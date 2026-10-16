package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/store"
	"github.com/vnovels/linguastack/internal/translation"
)

const (
	maxGlossaryEntries = 500
	maxGlossaryTerm    = 200
	maxModelID         = 200
	defaultSegmentPage = 200
	maxSegmentPage     = 1000
	sseKeepalive       = 15 * time.Second
)

var activeStatuses = []string{models.ProjectQueued, models.ProjectRunning, models.ProjectPaused}

type translationIn struct {
	SourceLang       string            `json:"source_lang"`
	TargetLang       string            `json:"target_lang"`
	ProviderConfigID string            `json:"provider_config_id"`
	ModelID          string            `json:"model_id"`
	Style            string            `json:"style"`
	Determinism      string            `json:"determinism"`
	Glossary         map[string]string `json:"glossary"`
}

type projectOut struct {
	ID                  uuid.UUID         `json:"id"`
	BookID              uuid.UUID         `json:"book_id"`
	ProviderConfigID    uuid.UUID         `json:"provider_config_id"`
	SourceLang          string            `json:"source_lang"`
	TargetLang          string            `json:"target_lang"`
	ModelID             string            `json:"model_id"`
	Style               string            `json:"style"`
	Determinism         string            `json:"determinism"`
	Glossary            map[string]string `json:"glossary"`
	Status              string            `json:"status"`
	SegmentsTotal       int               `json:"segments_total"`
	SegmentsDone        int               `json:"segments_done"`
	SegmentsFailed      int               `json:"segments_failed"`
	WarningsCount       int               `json:"warnings_count"`
	ProgressPercent     float64           `json:"progress_percent"`
	CostUSD             float64           `json:"cost_usd"`
	LastError           string            `json:"last_error,omitempty"`
	SendsDataExternally bool              `json:"sends_data_externally"`
	CreatedAt           time.Time         `json:"created_at"`
	StartedAt           *time.Time        `json:"started_at,omitempty"`
	FinishedAt          *time.Time        `json:"finished_at,omitempty"`
}

func projectResponse(p *models.TranslationProject, cfg *models.ProviderConfig) projectOut {
	glossary := p.Glossary.Data
	if glossary == nil {
		glossary = map[string]string{}
	}
	out := projectOut{
		ID: p.ID, BookID: p.BookID, ProviderConfigID: p.ProviderConfigID, SourceLang: p.SourceLang,
		TargetLang: p.TargetLang, ModelID: p.ModelID, Style: p.Style, Determinism: p.Determinism, Glossary: glossary,
		Status: p.Status, SegmentsTotal: p.SegmentsTotal, SegmentsDone: p.SegmentsDone, SegmentsFailed: p.SegmentsFailed,
		WarningsCount: p.WarningsCount, ProgressPercent: p.ProgressPercent(), CostUSD: p.CostUSD, LastError: p.LastError,
		CreatedAt: p.CreatedAt.UTC(), StartedAt: p.StartedAt, FinishedAt: p.FinishedAt,
	}
	if cfg != nil {
		out.SendsDataExternally = providers.SendsDataExternally(cfg.ProviderID, cfg.BaseURL)
	}
	return out
}

// validateTranslation нормализует запрос и возвращает текст ошибки или "".
func validateTranslation(in *translationIn, book *models.Book) string {
	if in.SourceLang == "" || in.SourceLang == "auto" {
		in.SourceLang = "auto"
		// Язык из метаданных книги (EPUB dc:language) надёжнее, чем
		// предложение модели определить его самой.
		if primary, _, _ := strings.Cut(strings.ToLower(book.Language), "-"); translation.IsKnownLanguage(primary) {
			in.SourceLang = primary
		}
	} else if !translation.IsKnownLanguage(in.SourceLang) {
		return "Неизвестный язык исходника"
	}
	if !translation.IsKnownLanguage(in.TargetLang) {
		return "Неизвестный язык перевода"
	}
	if in.Style == "" {
		in.Style = "neutral"
	}
	if !translation.IsKnownStyle(in.Style) {
		return "Неизвестный стиль перевода"
	}
	if in.Determinism == "" {
		in.Determinism = string(providers.Strict)
	}
	if !providers.Determinism(in.Determinism).Valid() {
		return "determinism: strict, balanced или creative"
	}
	if in.ModelID = strings.TrimSpace(in.ModelID); in.ModelID == "" || len(in.ModelID) > maxModelID {
		return "Не указана модель"
	}
	if len(in.Glossary) > maxGlossaryEntries {
		return fmt.Sprintf("Глоссарий не больше %d терминов", maxGlossaryEntries)
	}
	for term, translated := range in.Glossary {
		if utf8.RuneCountInString(term) > maxGlossaryTerm || utf8.RuneCountInString(translated) > maxGlossaryTerm {
			return fmt.Sprintf("Термины глоссария не длиннее %d символов", maxGlossaryTerm)
		}
	}
	return ""
}

func (s *server) createTranslation(w http.ResponseWriter, r *http.Request) {
	// Сначала владение книгой: чужая книга — 404 до любой проверки тела.
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var in translationIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := validateTranslation(&in, book); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	user := UserFromContext(r.Context())
	cfgID, err := uuid.Parse(in.ProviderConfigID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Конфигурация провайдера не найдена")
		return
	}
	cfg, err := store.Owned[models.ProviderConfig](r.Context(), s.DB, cfgID, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, "Конфигурация провайдера не найдена")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if providers.SendsDataExternally(cfg.ProviderID, cfg.BaseURL) && !cfg.AllowExternal {
		writeError(w, http.StatusBadRequest,
			"Текст книги будет отправлен внешнему сервису, но передача наружу запрещена в настройках провайдера (ТЗ §6.4)")
		return
	}

	var active int64
	if err := s.DB.WithContext(r.Context()).Model(&models.TranslationProject{}).
		Where("owner_id = ? AND status IN ?", user.ID, activeStatuses).Count(&active).Error; err != nil {
		internalError(w, r, err)
		return
	}
	if active >= int64(s.Cfg.MaxProjectsPerUser) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Не больше %d активных переводов одновременно", s.Cfg.MaxProjectsPerUser))
		return
	}

	glossary := in.Glossary
	if glossary == nil {
		glossary = map[string]string{}
	}
	project := models.TranslationProject{
		OwnerID: user.ID, BookID: book.ID, ProviderConfigID: cfg.ID, SourceLang: in.SourceLang, TargetLang: in.TargetLang,
		ModelID: in.ModelID, Style: in.Style, Determinism: in.Determinism, Glossary: models.NewJSON(glossary),
		Status: models.ProjectQueued,
	}
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		_, err := queue.Enqueue(r.Context(), tx, &project, 0, s.Cfg.MaxSegmentAttempts)
		return err
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectResponse(&project, cfg))
}

func (s *server) listTranslations(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	var list []models.TranslationProject
	if err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(user.ID)).Order("created_at DESC").Find(&list).Error; err != nil {
		internalError(w, r, err)
		return
	}
	var configs []models.ProviderConfig
	if err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(user.ID)).Find(&configs).Error; err != nil {
		internalError(w, r, err)
		return
	}
	byID := make(map[uuid.UUID]*models.ProviderConfig, len(configs))
	for i := range configs {
		byID[configs[i].ID] = &configs[i]
	}
	out := make([]projectOut, len(list))
	for i := range list {
		out[i] = projectResponse(&list[i], byID[list[i].ProviderConfigID])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) projectConfig(r *http.Request, p *models.TranslationProject) *models.ProviderConfig {
	cfg, err := store.Owned[models.ProviderConfig](r.Context(), s.DB, p.ProviderConfigID, p.OwnerID)
	if err != nil {
		return nil
	}
	return cfg
}

func (s *server) getTranslation(w http.ResponseWriter, r *http.Request) {
	if p, ok := loadOwned[models.TranslationProject](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, projectResponse(p, s.projectConfig(r, p)))
	}
}

// transition переводит проект из одного из статусов from в to; иначе 409.
// Явная таблица переходов: возобновить отменённый или поставить на паузу
// завершённый проект нельзя.
func (s *server) transition(w http.ResponseWriter, r *http.Request, from []string, to string, after func(tx *gorm.DB, p *models.TranslationProject) error) {
	p, ok := loadOwned[models.TranslationProject](s, w, r, "id")
	if !ok {
		return
	}
	updates := map[string]any{"status": to, "updated_at": time.Now().UTC()}
	switch to {
	case models.ProjectCancelled:
		updates["finished_at"] = time.Now().UTC()
	case models.ProjectQueued:
		updates["finished_at"], updates["last_error"] = nil, ""
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.TranslationProject{}).Where("id = ? AND status IN ?", p.ID, from).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errConflict
		}
		p.Status = to
		return after(tx, p)
	})
	if errors.Is(err, errConflict) {
		writeError(w, http.StatusConflict, fmt.Sprintf("Из статуса «%s» это действие недоступно", p.Status))
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := s.DB.WithContext(r.Context()).First(p, "id = ?", p.ID).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectResponse(p, s.projectConfig(r, p)))
}

var errConflict = errors.New("недопустимый переход статуса")

// pause: выполняющуюся задачу worker завершит сам между батчами.
func (s *server) pauseTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, []string{models.ProjectQueued, models.ProjectRunning}, models.ProjectPaused,
		func(tx *gorm.DB, p *models.TranslationProject) error {
			return queue.CancelPending(r.Context(), tx, p.ID)
		})
}

// resume продолжает с места: и после паузы, и после сбоя провайдера.
func (s *server) resumeTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, []string{models.ProjectPaused, models.ProjectFailed}, models.ProjectQueued,
		func(tx *gorm.DB, p *models.TranslationProject) error {
			_, err := queue.Enqueue(r.Context(), tx, p, 0, s.Cfg.MaxSegmentAttempts)
			return err
		})
}

func (s *server) cancelTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, []string{models.ProjectQueued, models.ProjectRunning, models.ProjectPaused, models.ProjectFailed},
		models.ProjectCancelled,
		func(tx *gorm.DB, p *models.TranslationProject) error {
			return queue.CancelPending(r.Context(), tx, p.ID)
		})
}

type segmentOut struct {
	ID             uuid.UUID             `json:"id"`
	NodeID         string                `json:"node_id"`
	ChapterIndex   int                   `json:"chapter_index"`
	Ordinal        int                   `json:"ordinal"`
	BlockType      string                `json:"block_type"`
	SourceText     string                `json:"source_text"`
	TranslatedText *string               `json:"translated_text"`
	Status         string                `json:"status"`
	Severity       string                `json:"severity"`
	Findings       []translation.Finding `json:"findings"`
	Attempts       int                   `json:"attempts"`
	LastError      string                `json:"last_error,omitempty"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

func segmentResponse(seg *models.TranslationSegment) segmentOut {
	findings := seg.Findings.Data
	if findings == nil {
		findings = []translation.Finding{}
	}
	return segmentOut{
		ID: seg.ID, NodeID: seg.NodeID, ChapterIndex: seg.ChapterIndex, Ordinal: seg.Ordinal, BlockType: seg.BlockType,
		SourceText: seg.SourceText, TranslatedText: seg.TranslatedText, Status: seg.Status, Severity: seg.Severity,
		Findings: findings, Attempts: seg.Attempts, LastError: seg.LastError, UpdatedAt: seg.UpdatedAt.UTC(),
	}
}

var (
	segmentStatuses = []string{
		models.SegmentPending, models.SegmentRunning, models.SegmentCompleted, models.SegmentFailed,
	}
	segmentSeverities = []string{string(translation.OK), string(translation.Warning), string(translation.Error)}
)

func queryInt(r *http.Request, name string, def int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil
}

func (s *server) listSegments(w http.ResponseWriter, r *http.Request) {
	p, ok := loadOwned[models.TranslationProject](s, w, r, "id")
	if !ok {
		return
	}
	offset, okOffset := queryInt(r, "offset", 0)
	limit, okLimit := queryInt(r, "limit", defaultSegmentPage)
	if !okOffset || !okLimit || offset < 0 || limit < 1 || limit > maxSegmentPage {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("offset ≥ 0, limit от 1 до %d", maxSegmentPage))
		return
	}
	q := s.DB.WithContext(r.Context()).Where("project_id = ?", p.ID)
	// Фильтры — чтобы найти не прошедшие проверку сегменты среди сотен
	// (ТЗ §5.3) без перебора всех страниц.
	if status := r.URL.Query().Get("status"); status != "" {
		if !slices.Contains(segmentStatuses, status) {
			writeError(w, http.StatusUnprocessableEntity, "status: "+strings.Join(segmentStatuses, ", "))
			return
		}
		q = q.Where("status = ?", status)
	}
	if severity := r.URL.Query().Get("severity"); severity != "" {
		if !slices.Contains(segmentSeverities, severity) {
			writeError(w, http.StatusUnprocessableEntity, "severity: "+strings.Join(segmentSeverities, ", "))
			return
		}
		q = q.Where("severity = ?", severity)
	}
	var segs []models.TranslationSegment
	if err := q.Order("chapter_index, ordinal").Offset(offset).Limit(limit).Find(&segs).Error; err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]segmentOut, len(segs))
	for i := range segs {
		out[i] = segmentResponse(&segs[i])
	}
	writeJSON(w, http.StatusOK, out)
}

type progressEvent struct {
	Status          string  `json:"status"`
	SegmentsTotal   int     `json:"segments_total"`
	SegmentsDone    int     `json:"segments_done"`
	SegmentsFailed  int     `json:"segments_failed"`
	WarningsCount   int     `json:"warnings_count"`
	ProgressPercent float64 `json:"progress_percent"`
	CostUSD         float64 `json:"cost_usd"`
	LastError       string  `json:"last_error,omitempty"`
}

// translationEvents — поток прогресса (ТЗ §8.4, дополнение §13): событие
// при каждом изменении, комментарий-«пинг» для прокси, закрытие на
// терминальном статусе, при отключении клиента и при остановке сервера
// (клиент EventSource переподключится сам).
func (s *server) translationEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := loadOwned[models.TranslationProject](s, w, r, "id")
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Потоковая передача недоступна")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx не должен буферизовать поток
	w.WriteHeader(http.StatusOK)

	ticker := time.NewTicker(s.SSEInterval)
	defer ticker.Stop()
	last, lastSent := "", time.Now()
	for {
		if err := s.DB.WithContext(r.Context()).First(p, "id = ?", p.ID).Error; err != nil {
			return // проект удалён или клиент ушёл
		}
		raw, _ := json.Marshal(progressEvent{
			Status: p.Status, SegmentsTotal: p.SegmentsTotal, SegmentsDone: p.SegmentsDone, SegmentsFailed: p.SegmentsFailed,
			WarningsCount: p.WarningsCount, ProgressPercent: p.ProgressPercent(), CostUSD: p.CostUSD, LastError: p.LastError,
		})
		switch {
		case string(raw) != last:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
			last, lastSent = string(raw), time.Now()
			flusher.Flush()
		case time.Since(lastSent) > sseKeepalive:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			lastSent = time.Now()
			flusher.Flush()
		}
		if p.IsTerminal() {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.Stopping.Done():
			return
		case <-ticker.C:
		}
	}
}
