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
// maxPerUser worker-ов; задачи с истёкшей арендой лимит не занимают.
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
              AND r.locked_until > now() AND r.id <> t.id) < ?
    ORDER BY t.priority DESC, t.created_at, t.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1)
RETURNING *`

// Enqueue ставит проект в очередь. Идемпотентно: если у проекта уже есть
// активная задача (уникальный частичный индекс), возвращается она.
func Enqueue(ctx context.Context, db *gorm.DB, project *models.TranslationProject, priority, maxAttempts int) (*models.TranslationTask, error) {
	task := models.TranslationTask{
		ProjectID: project.ID, OwnerID: project.OwnerID, Kind: "translate", Status: models.TaskPending,
		Priority: priority, MaxAttempts: maxAttempts, AvailableAt: time.Now().UTC(),
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

// ClaimNext берёт задачу в аренду. nil, nil — задач нет.
func ClaimNext(ctx context.Context, db *gorm.DB, workerID string, lease time.Duration, maxPerUser int) (*models.TranslationTask, error) {
	var task models.TranslationTask
	if err := db.WithContext(ctx).Raw(claimSQL, workerID, lease.Seconds(), maxPerUser).Scan(&task).Error; err != nil {
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
