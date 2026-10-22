// Package queue — фоновая очередь перевода на арендах в PostgreSQL
// (ТЗ §7, дополнение §7): без отдельного брокера, с честностью между
// пользователями и восстановлением после перезапуска.
package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// ErrLeaseLost — задачу держит не этот worker (аренда истекла и её забрал
// другой). Результат такого worker-а писать в задачу нельзя.
var ErrLeaseLost = errors.New("аренда задачи потеряна")

const maxLastError = 4000

// claimSQL — дополнение §7. Берётся либо ожидающая задача, срок которой
// наступил, либо выполняющаяся с истёкшей арендой — её worker упал.
// FOR UPDATE SKIP LOCKED: несколько worker-ов не берут одну задачу.
// Подзапрос count — честность: пользователь не занимает больше
// maxPerUser worker-ов (или своей квоты из users, дополнение §18); задачи
// с истёкшей арендой лимит не занимают. Ollama сервера — один процессор:
// её задача не берётся, пока идёт другая задача Ollama.
const claimSQL = `
UPDATE translation_tasks
   SET status = 'running', locked_by = ?, locked_until = now() + make_interval(secs => ?),
       attempts = attempts + 1, started_at = COALESCE(started_at, now())
 WHERE id = (
   SELECT t.id FROM translation_tasks t
    WHERE ((t.status = 'pending' AND t.available_at <= now())
        OR (t.status = 'running' AND t.locked_until < now()))
      AND (SELECT count(*) FROM translation_tasks r
            WHERE r.owner_id = t.owner_id AND r.status = 'running'
              AND r.locked_until > now() AND r.id <> t.id)
          < COALESCE((SELECT u.max_concurrent_tasks FROM users u WHERE u.id = t.owner_id), ?)
      AND NOT (
        EXISTS (SELECT 1 FROM translation_projects p JOIN provider_configs c ON c.id = p.provider_config_id
                 WHERE p.id = t.project_id AND c.provider_id = 'ollama')
        AND EXISTS (SELECT 1 FROM translation_tasks r
                      JOIN translation_projects rp ON rp.id = r.project_id
                      JOIN provider_configs rc ON rc.id = rp.provider_config_id
                     WHERE r.status = 'running' AND r.locked_until > now() AND r.id <> t.id
                       AND rc.provider_id = 'ollama'))
    ORDER BY t.priority DESC, t.created_at, t.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1)
RETURNING *`

// Enqueue ставит проект в очередь. Идемпотентно: если у проекта уже есть
// активная задача (уникальный частичный индекс), возвращается она.
func Enqueue(ctx context.Context, db *gorm.DB, project *models.TranslationProject, priority, maxAttempts int) (*models.TranslationTask, error) {
	// available_at не задаётся: его ставит БД (DEFAULT now()). Очередь
	// считает время только по часам базы — иначе расхождение часов
	// приложения и БД откладывало бы задачи «на будущее».
	task := models.TranslationTask{
		ProjectID: project.ID, OwnerID: project.OwnerID, Kind: "translate", Status: models.TaskPending,
		Priority: priority, MaxAttempts: maxAttempts,
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tx.Create(&task).Error })
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		var existing models.TranslationTask
		if err := db.WithContext(ctx).Where("project_id = ? AND status IN ?", project.ID,
			[]string{models.TaskPending, models.TaskRunning}).First(&existing).Error; err != nil {
			return nil, fmt.Errorf("поиск активной задачи: %w", err)
		}
		return &existing, nil
	}
	if err != nil {
		return nil, fmt.Errorf("постановка в очередь: %w", err)
	}
	return &task, nil
}

// claimLock — ключ рекомендательной блокировки захвата. Лимиты захвата
// считаются по выполняющимся задачам: два worker-а, захватывающие
// одновременно, оба увидели бы «ещё есть место» и вместе превысили бы лимит
// пользователя или запустили бы две задачи Ollama. Захват — миллисекунды,
// поэтому очередь из worker-ов за блокировкой не заметна.
const claimLock = 0x4c53_636c_6169_6d // «LSclaim»

// ClaimNext берёт задачу в аренду. nil, nil — задач нет.
func ClaimNext(ctx context.Context, db *gorm.DB, workerID string, lease time.Duration, maxPerUser int) (*models.TranslationTask, error) {
	var task models.TranslationTask
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(?)`, int64(claimLock)).Error; err != nil {
			return err
		}
		return tx.Raw(claimSQL, workerID, lease.Seconds(), maxPerUser).Scan(&task).Error
	})
	if err != nil {
		return nil, fmt.Errorf("захват задачи: %w", err)
	}
	if task.ID == [16]byte{} {
		return nil, nil
	}
	return &task, nil
}

// Heartbeat продлевает аренду, пока задача реально выполняется. false —
// аренды у этого worker-а больше нет.
func Heartbeat(ctx context.Context, db *gorm.DB, taskID [16]byte, workerID string, lease time.Duration) (bool, error) {
	res := db.WithContext(ctx).Exec(`
UPDATE translation_tasks SET locked_until = now() + make_interval(secs => ?)
 WHERE id = ? AND locked_by = ? AND status = 'running'`, lease.Seconds(), taskID, workerID)
	if res.Error != nil {
		return false, fmt.Errorf("продление аренды: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// Finish завершает задачу статусом completed, failed или cancelled.
// errText — причина для failed (неверный ключ, нет модели): такие сбои
// повторять бессмысленно, задача завершается сразу.
func Finish(ctx context.Context, db *gorm.DB, taskID [16]byte, workerID, status, errText string) error {
	res := db.WithContext(ctx).Exec(`
UPDATE translation_tasks
   SET status = ?, last_error = ?, finished_at = now(), locked_by = NULL, locked_until = NULL
 WHERE id = ? AND locked_by = ? AND status = 'running'`, status, clip(errText), taskID, workerID)
	if res.Error != nil {
		return fmt.Errorf("завершение задачи: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Fail фиксирует неудачную попытку. Пока попытки не исчерпаны, задача
// возвращается в очередь не раньше retryIn; иначе — failed. terminal
// сообщает, что попыток больше не будет.
func Fail(ctx context.Context, db *gorm.DB, taskID [16]byte, workerID, errText string, retryIn time.Duration) (terminal bool, err error) {
	var row struct{ Status string }
	res := db.WithContext(ctx).Raw(`
UPDATE translation_tasks
   SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
       finished_at = CASE WHEN attempts >= max_attempts THEN now() ELSE NULL END,
       available_at = now() + make_interval(secs => ?),
       last_error = ?, locked_by = NULL, locked_until = NULL
 WHERE id = ? AND locked_by = ? AND status = 'running'
RETURNING status`, retryIn.Seconds(), clip(errText), taskID, workerID).Scan(&row)
	if res.Error != nil {
		return false, fmt.Errorf("неудачная попытка: %w", res.Error)
	}
	if row.Status == "" {
		return false, ErrLeaseLost
	}
	return row.Status == models.TaskFailed, nil
}

// MarkProgress отмечает, что задача продвинулась: счётчик попыток снова
// учитывает только текущий захват. Попытки — это сбои подряд без
// прогресса: книга, которая переводится часами, не должна падать из-за
// редких сбоев за это время, а застрявшая на одном месте — должна.
func MarkProgress(ctx context.Context, db *gorm.DB, taskID [16]byte, workerID string) error {
	err := db.WithContext(ctx).Exec(`
UPDATE translation_tasks SET attempts = 1
 WHERE id = ? AND locked_by = ? AND status = 'running' AND attempts > 1`, taskID, workerID).Error
	if err != nil {
		return fmt.Errorf("отметка прогресса задачи: %w", err)
	}
	return nil
}

// Release возвращает задачу в очередь при остановке сервиса. Остановка —
// не сбой, поэтому попытка не расходуется.
func Release(ctx context.Context, db *gorm.DB, taskID [16]byte, workerID string) error {
	res := db.WithContext(ctx).Exec(`
UPDATE translation_tasks
   SET status = 'pending', attempts = GREATEST(attempts - 1, 0), available_at = now(),
       locked_by = NULL, locked_until = NULL
 WHERE id = ? AND locked_by = ? AND status = 'running'`, taskID, workerID)
	if res.Error != nil {
		return fmt.Errorf("возврат задачи: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// CancelPending отменяет ожидающие задачи проекта. Выполняющуюся задачу
// worker заметит сам — между батчами он проверяет статус проекта.
func CancelPending(ctx context.Context, db *gorm.DB, projectID [16]byte) error {
	err := db.WithContext(ctx).Exec(`
UPDATE translation_tasks SET status = 'cancelled', finished_at = now()
 WHERE project_id = ? AND status = 'pending'`, projectID).Error
	if err != nil {
		return fmt.Errorf("отмена задач проекта: %w", err)
	}
	return nil
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxLastError {
		return s
	}
	return string([]rune(s)[:maxLastError]) + "…"
}
