// Package notify — уведомления Telegram через таблицу «исходящих»
// (дополнение §17): worker пишет события, процесс bot доставляет их с
// повторами и склейкой. Запись — только если у владельца есть привязка с
// включённым видом уведомлений; повтор события отсекает dedupe_key.
package notify

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChaptersDone записывает «глава готова» для глав сегментов nodeIDs, в
// которых не осталось ожидающих и выполняющихся сегментов. Вызывается
// после записи батча.
func ChaptersDone(ctx context.Context, db *gorm.DB, projectID uuid.UUID, nodeIDs []string) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	err := db.WithContext(ctx).Exec(`
INSERT INTO telegram_notifications (id, user_id, project_id, kind, chapter_index, dedupe_key)
SELECT gen_random_uuid(), p.owner_id, p.id, 'chapter', c.chapter_index, 'chapter:' || p.id || ':' || c.chapter_index
  FROM translation_projects p
  JOIN telegram_bindings b ON b.user_id = p.owner_id AND b.notify_chapters
  JOIN (SELECT DISTINCT chapter_index FROM translation_segments WHERE project_id = ? AND node_id IN ?) c ON true
 WHERE p.id = ?
   AND NOT EXISTS (SELECT 1 FROM translation_segments s
                    WHERE s.project_id = p.id AND s.chapter_index = c.chapter_index AND s.status IN ('pending', 'running'))
ON CONFLICT (dedupe_key) DO NOTHING`, projectID, nodeIDs, projectID).Error
	if err != nil {
		return fmt.Errorf("уведомление о главе: %w", err)
	}
	return nil
}

// Completed записывает «перевод готов», если проект завершён. Одно на
// проект.
func Completed(ctx context.Context, db *gorm.DB, projectID uuid.UUID) error {
	err := db.WithContext(ctx).Exec(`
INSERT INTO telegram_notifications (id, user_id, project_id, kind, dedupe_key)
SELECT gen_random_uuid(), p.owner_id, p.id, 'completed', 'completed:' || p.id
  FROM translation_projects p
  JOIN telegram_bindings b ON b.user_id = p.owner_id AND b.notify_completed
 WHERE p.id = ? AND p.status IN ('completed', 'completed_with_warnings')
ON CONFLICT (dedupe_key) DO NOTHING`, projectID).Error
	if err != nil {
		return fmt.Errorf("уведомление о завершении: %w", err)
	}
	return nil
}

// Failed записывает «перевод остановлен», если проект упал. Каждый сбой
// (время finished_at) — своё уведомление: после «Продолжить» перевод может
// упасть снова.
func Failed(ctx context.Context, db *gorm.DB, projectID uuid.UUID) error {
	err := db.WithContext(ctx).Exec(`
INSERT INTO telegram_notifications (id, user_id, project_id, kind, dedupe_key)
SELECT gen_random_uuid(), p.owner_id, p.id, 'failed',
       'failed:' || p.id || ':' || COALESCE((extract(epoch FROM p.finished_at) * 1000000)::bigint, 0)
  FROM translation_projects p
  JOIN telegram_bindings b ON b.user_id = p.owner_id AND b.notify_errors
 WHERE p.id = ? AND p.status = 'failed'
ON CONFLICT (dedupe_key) DO NOTHING`, projectID).Error
	if err != nil {
		return fmt.Errorf("уведомление об ошибке: %w", err)
	}
	return nil
}
