package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/store"
	"github.com/vnovels/linguastack/internal/translation"
)

func (s *server) loadSegment(w http.ResponseWriter, r *http.Request) (*models.TranslationSegment, *models.TranslationProject, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return nil, nil, false
	}
	seg, project, err := store.OwnedSegment(r.Context(), s.DB, id, UserFromContext(r.Context()).ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return nil, nil, false
	}
	if err != nil {
		internalError(w, r, err)
		return nil, nil, false
	}
	return seg, project, true
}

type segmentPatch struct {
	TranslatedText string `json:"translated_text"`
}

// patchSegment — ручная правка перевода (ТЗ §8.3). Прежняя версия остаётся
// в истории (ТЗ §5.1); проверка модели к ручному тексту не применяется —
// здесь решает человек. Worker такой сегмент не перезапишет.
func (s *server) patchSegment(w http.ResponseWriter, r *http.Request) {
	seg, project, ok := s.loadSegment(w, r)
	if !ok {
		return
	}
	var in segmentPatch
	if !decodeJSON(w, r, &in) {
		return
	}
	text := strings.TrimSpace(in.TranslatedText)
	if text == "" {
		writeError(w, http.StatusUnprocessableEntity, "Перевод не может быть пустым")
		return
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(seg).Updates(map[string]any{
			"translated_text": text, "status": models.SegmentCompleted, "severity": string(translation.OK),
			"findings": models.NewJSON([]translation.Finding{}), "last_error": "", "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.TranslationSegmentRevision{SegmentID: seg.ID, TranslatedText: text, Origin: models.OriginManual}).Error; err != nil {
			return err
		}
		if err := queue.RefreshCounters(r.Context(), tx, project.ID, 0); err != nil {
			return err
		}
		// Правка последнего упавшего сегмента завершает проект.
		return queue.SettleStatus(r.Context(), tx, project.ID,
			models.ProjectFailed, models.ProjectCompleted, models.ProjectCompletedWithWarnings)
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := s.DB.WithContext(r.Context()).First(seg, "id = ?", seg.ID).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, segmentResponse(seg))
}

// retrySegment ставит сегмент на повторный перевод (ТЗ §12). Во время
// выполнения проекта — 409: worker берёт список ожидающих сегментов в
// начале задачи и поставленный посреди прогона сегмент не увидел бы, а
// проект завершился бы «готовым» с непереведённым сегментом.
func (s *server) retrySegment(w http.ResponseWriter, r *http.Request) {
	seg, project, ok := s.loadSegment(w, r)
	if !ok {
		return
	}
	switch project.Status {
	case models.ProjectRunning:
		writeError(w, http.StatusConflict, "Проект переводится — поставьте его на паузу или дождитесь окончания")
		return
	case models.ProjectCancelled:
		writeError(w, http.StatusConflict, "Проект отменён")
		return
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(seg).Updates(map[string]any{
			"status": models.SegmentPending, "attempts": 0, "last_error": "", "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if err := queue.RefreshCounters(r.Context(), tx, project.ID, 0); err != nil {
			return err
		}
		if project.Status == models.ProjectPaused {
			return nil // переведётся после «Продолжить»
		}
		res := tx.Model(&models.TranslationProject{}).Where("id = ?", project.ID).
			Updates(map[string]any{"status": models.ProjectQueued, "finished_at": nil, "last_error": "", "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		project.Status = models.ProjectQueued
		_, err := queue.Enqueue(r.Context(), tx, project, 0, s.Cfg.MaxSegmentAttempts)
		return err
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := s.DB.WithContext(r.Context()).First(seg, "id = ?", seg.ID).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, segmentResponse(seg))
}

type revisionOut struct {
	ID             uuid.UUID `json:"id"`
	TranslatedText string    `json:"translated_text"`
	Origin         string    `json:"origin"`
	CreatedAt      time.Time `json:"created_at"`
}

func (s *server) segmentRevisions(w http.ResponseWriter, r *http.Request) {
	seg, _, ok := s.loadSegment(w, r)
	if !ok {
		return
	}
	var revs []models.TranslationSegmentRevision
	if err := s.DB.WithContext(r.Context()).Where("segment_id = ?", seg.ID).Order("created_at, id").Find(&revs).Error; err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]revisionOut, len(revs))
	for i, rev := range revs {
		out[i] = revisionOut{ID: rev.ID, TranslatedText: rev.TranslatedText, Origin: rev.Origin, CreatedAt: rev.CreatedAt.UTC()}
	}
	writeJSON(w, http.StatusOK, out)
}
