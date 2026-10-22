package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/store"
)

// Управление переводом: пауза, возобновление, отмена. Общий код HTTP API и
// Telegram-бота (дополнение §17) — проверка владельца и допустимых
// переходов в одном месте.

// ErrBadTransition — из текущего статуса действие недоступно.
var ErrBadTransition = errors.New("недопустимый переход статуса")

// TransitionError уточняет ErrBadTransition статусом, из которого просили.
type TransitionError struct{ From string }

func (e *TransitionError) Error() string {
	return fmt.Sprintf("из статуса %q это действие недоступно", e.From)
}

func (e *TransitionError) Unwrap() error { return ErrBadTransition }

// Pause приостанавливает перевод: выполняющуюся задачу worker завершит сам
// между батчами, ожидающая снимается.
func Pause(ctx context.Context, db *gorm.DB, projectID, ownerID uuid.UUID) (*models.TranslationProject, error) {
	return transition(ctx, db, projectID, ownerID, []string{models.ProjectQueued, models.ProjectRunning},
		models.ProjectPaused, func(tx *gorm.DB, p *models.TranslationProject) error {
			return CancelPending(ctx, tx, p.ID)
		})
}

// Resume доводит перевод до конца: продолжает с места после паузы и сбоя
// провайдера и заново переводит упавшие сегменты — те, что исчерпали
// попытки модели. Без этого проект «ошибка» из-за упавших сегментов
// завершался бы сразу с той же ошибкой: worker берёт только ожидающие.
func Resume(ctx context.Context, db *gorm.DB, projectID, ownerID uuid.UUID, maxAttempts int) (*models.TranslationProject, error) {
	return transition(ctx, db, projectID, ownerID, []string{models.ProjectPaused, models.ProjectFailed},
		models.ProjectQueued, func(tx *gorm.DB, p *models.TranslationProject) error {
			if err := RetryFailed(ctx, tx, p.ID); err != nil {
				return err
			}
			_, err := Enqueue(ctx, tx, p, 0, maxAttempts)
			return err
		})
}

// RetryFailed возвращает упавшие сегменты проекта в ожидание со свежим
// счётом попыток, как «Перевести заново» у одного сегмента.
func RetryFailed(ctx context.Context, tx *gorm.DB, projectID uuid.UUID) error {
	err := tx.WithContext(ctx).Model(&models.TranslationSegment{}).
		Where("project_id = ? AND status = ?", projectID, models.SegmentFailed).
		Updates(map[string]any{"status": models.SegmentPending, "attempts": 0, "last_error": "", "updated_at": time.Now().UTC()}).Error
	if err != nil {
		return fmt.Errorf("повтор упавших сегментов: %w", err)
	}
	return RefreshCounters(ctx, tx, projectID, Usage{})
}

// Cancel отменяет перевод; сделанное сохраняется.
func Cancel(ctx context.Context, db *gorm.DB, projectID, ownerID uuid.UUID) (*models.TranslationProject, error) {
	return transition(ctx, db, projectID, ownerID,
		[]string{models.ProjectQueued, models.ProjectRunning, models.ProjectPaused, models.ProjectFailed},
		models.ProjectCancelled, func(tx *gorm.DB, p *models.TranslationProject) error {
			return CancelPending(ctx, tx, p.ID)
		})
}

// transition меняет статус, только если проект принадлежит ownerID
// (иначе store.ErrNotFound) и находится в одном из from.
func transition(ctx context.Context, db *gorm.DB, projectID, ownerID uuid.UUID, from []string, to string,
	after func(tx *gorm.DB, p *models.TranslationProject) error) (*models.TranslationProject, error) {
	p, err := store.Owned[models.TranslationProject](ctx, db, projectID, ownerID)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{"status": to, "updated_at": time.Now().UTC()}
	switch to {
	case models.ProjectCancelled:
		updates["finished_at"] = time.Now().UTC()
	case models.ProjectQueued:
		updates["finished_at"], updates["last_error"] = nil, ""
	}
	before := p.Status
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.TranslationProject{}).Where("id = ? AND status IN ?", p.ID, from).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &TransitionError{From: before}
		}
		p.Status = to
		return after(tx, p)
	})
	if err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).First(p, "id = ?", p.ID).Error; err != nil {
		return nil, fmt.Errorf("проект после перехода: %w", err)
	}
	return p, nil
}
