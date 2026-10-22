package httpapi

import (
	"context"
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

	"github.com/vnovels/linguastack/internal/booksearch"
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
	// Owned — перевод того, кто спрашивает: только он им управляет.
	Owned            bool              `json:"owned"`
	ID               uuid.UUID         `json:"id"`
	BookID           uuid.UUID         `json:"book_id"`
	ProviderConfigID uuid.UUID         `json:"provider_config_id"`
	SourceLang       string            `json:"source_lang"`
	TargetLang       string            `json:"target_lang"`
	ModelID          string            `json:"model_id"`
	Style            string            `json:"style"`
	Determinism      string            `json:"determinism"`
	Glossary         map[string]string `json:"glossary"`
	Status           string            `json:"status"`
	SegmentsTotal    int               `json:"segments_total"`
	SegmentsDone     int               `json:"segments_done"`
	SegmentsFailed   int               `json:"segments_failed"`
	WarningsCount    int               `json:"warnings_count"`
	ProgressPercent  float64           `json:"progress_percent"`
	CostUSD          float64           `json:"cost_usd"`
	usageOut
	LastError           string     `json:"last_error,omitempty"`
	BookTitle           string     `json:"book_title"`
	SendsDataExternally bool       `json:"sends_data_externally"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

// usageOut — статистика провайдера для монитора (ТЗ §8.4).
type usageOut struct {
	InputTokens   int64 `json:"input_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	ProviderCalls int   `json:"provider_calls"`
}

func usageResponse(p *models.TranslationProject) usageOut {
	return usageOut{InputTokens: p.InputTokens, OutputTokens: p.OutputTokens, ProviderCalls: p.ProviderCalls}
}

func projectResponse(p *models.TranslationProject, cfg *models.ProviderConfig, bookTitle string) projectOut {
	glossary := p.Glossary.Data
	if glossary == nil {
		glossary = map[string]string{}
	}
	out := projectOut{
		ID: p.ID, BookID: p.BookID, ProviderConfigID: p.ProviderConfigID, SourceLang: p.SourceLang,
		TargetLang: p.TargetLang, ModelID: p.ModelID, Style: p.Style, Determinism: p.Determinism, Glossary: glossary,
		Status: p.Status, SegmentsTotal: p.SegmentsTotal, SegmentsDone: p.SegmentsDone, SegmentsFailed: p.SegmentsFailed,
		WarningsCount: p.WarningsCount, ProgressPercent: p.ProgressPercent(), CostUSD: p.CostUSD, usageOut: usageResponse(p),
		LastError: p.LastError, BookTitle: bookTitle,
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
	if cfg.ProviderID == "ollama" && !s.serverOllamaFor(user) {
		writeError(w, http.StatusForbidden, ollamaAdminOnlyMessage)
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
	limit := s.Cfg.MaxProjectsPerUser
	if user.MaxActiveProjects != nil {
		limit = *user.MaxActiveProjects
	}
	if active >= int64(limit) {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Не больше %d активных переводов одновременно", limit))
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
	out := projectResponse(&project, cfg, book.Title)
	out.Owned = true
	writeJSON(w, http.StatusCreated, out)
}

// translationOptions — списки для формы настройки перевода. Языки, которые
// понимает конкретная модель, сообщает её ModelCapabilities (ТЗ §5.2).
func (s *server) translationOptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"languages": translation.Languages(),
		"styles":    translation.Styles(),
		"determinism": []providers.Determinism{
			providers.Strict, providers.Balanced, providers.Creative,
		},
	})
}

// listTranslations — свои переводы; с book_id — переводы этой книги, чья
// бы она ни была (общая библиотека, дополнение §9): их делает только её
// владелец.
func (s *server) listTranslations(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	owner := user.ID
	var bookID *uuid.UUID
	if raw := r.URL.Query().Get("book_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "book_id должен быть UUID")
			return
		}
		book, err := store.Shared[models.Book](r.Context(), s.DB, id)
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, []projectOut{})
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		owner, bookID = book.OwnerID, &book.ID
	}
	q := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(owner))
	if bookID != nil {
		q = q.Where("book_id = ?", *bookID)
	}
	var list []models.TranslationProject
	if err := q.Order("created_at DESC").Find(&list).Error; err != nil {
		internalError(w, r, err)
		return
	}
	var books []models.Book
	if err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(owner)).Select("id", "title").Find(&books).Error; err != nil {
		internalError(w, r, err)
		return
	}
	titles := make(map[uuid.UUID]string, len(books))
	for _, b := range books {
		titles[b.ID] = b.Title
	}
	var configs []models.ProviderConfig
	if err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(owner)).Find(&configs).Error; err != nil {
		internalError(w, r, err)
		return
	}
	byID := make(map[uuid.UUID]*models.ProviderConfig, len(configs))
	for i := range configs {
		byID[configs[i].ID] = &configs[i]
	}
	out := make([]projectOut, len(list))
	for i := range list {
		out[i] = projectResponse(&list[i], byID[list[i].ProviderConfigID], titles[list[i].BookID])
		out[i].Owned = list[i].OwnerID == user.ID
	}
	writeJSON(w, http.StatusOK, out)
}

type libraryTranslation struct {
	ID            uuid.UUID `json:"id"`
	SourceLang    string    `json:"source_lang"`
	TargetLang    string    `json:"target_lang"`
	ModelID       string    `json:"model_id"`
	Status        string    `json:"status"`
	SegmentsDone  int       `json:"segments_done"`
	SegmentsTotal int       `json:"segments_total"`
}

type libraryBook struct {
	ID           uuid.UUID            `json:"id"`
	Title        string               `json:"title"`
	Author       string               `json:"author,omitempty"`
	Language     string               `json:"language,omitempty"`
	SourceFormat string               `json:"source_format"`
	CreatedAt    time.Time            `json:"created_at"`
	Owned        bool                 `json:"owned"`
	Translations []libraryTranslation `json:"translations"`
}

// library — общая библиотека (дополнение §9): все книги всех пользователей
// с их переводами, новые сверху. Владельцы не называются.
func (s *server) library(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	var books []models.Book
	err := s.DB.WithContext(r.Context()).Select("id", "owner_id", "title", "author", "language", "source_format", "created_at").
		Order("created_at DESC, id").Find(&books).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	var projects []models.TranslationProject
	err = s.DB.WithContext(r.Context()).Select("id", "book_id", "source_lang", "target_lang", "model_id", "status",
		"segments_done", "segments_total", "created_at").Order("created_at DESC, id").Find(&projects).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	byBook := map[uuid.UUID][]libraryTranslation{}
	for _, p := range projects {
		byBook[p.BookID] = append(byBook[p.BookID], libraryTranslation{ID: p.ID, SourceLang: p.SourceLang,
			TargetLang: p.TargetLang, ModelID: p.ModelID, Status: p.Status,
			SegmentsDone: p.SegmentsDone, SegmentsTotal: p.SegmentsTotal})
	}
	out := make([]libraryBook, len(books))
	for i, b := range books {
		tr := byBook[b.ID]
		if tr == nil {
			tr = []libraryTranslation{}
		}
		out[i] = libraryBook{ID: b.ID, Title: b.Title, Author: b.Author, Language: b.Language, SourceFormat: b.SourceFormat,
			CreatedAt: b.CreatedAt.UTC(), Owned: b.OwnerID == user.ID, Translations: tr}
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

// projectFor — ответ по одному проекту: конфигурация и название книги
// дочитываются отдельно.
func (s *server) projectFor(r *http.Request, p *models.TranslationProject) projectOut {
	var title string
	_ = s.DB.WithContext(r.Context()).Model(&models.Book{}).
		Where("id = ? AND owner_id = ?", p.BookID, p.OwnerID).Pluck("title", &title).Error
	out := projectResponse(p, s.projectConfig(r, p), title)
	out.Owned = p.OwnerID == UserFromContext(r.Context()).ID
	return out
}

func (s *server) getTranslation(w http.ResponseWriter, r *http.Request) {
	if p, ok := loadShared[models.TranslationProject](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, s.projectFor(r, p))
	}
}

// transition переводит проект из одного из статусов from в to; иначе 409.
// Явная таблица переходов: возобновить отменённый или поставить на паузу
// завершённый проект нельзя.
// transition — пауза, возобновление или отмена (общий код с ботом —
// queue.Pause/Resume/Cancel): чужой проект — 404, недопустимый переход — 409.
func (s *server) transition(w http.ResponseWriter, r *http.Request,
	action func(ctx context.Context, projectID, ownerID uuid.UUID) (*models.TranslationProject, error)) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	p, err := action(r.Context(), id, UserFromContext(r.Context()).ID)
	var bad *queue.TransitionError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	case errors.As(err, &bad):
		writeError(w, http.StatusConflict, fmt.Sprintf("Из статуса «%s» это действие недоступно", bad.From))
		return
	case err != nil:
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.projectFor(r, p))
}

// pause: выполняющуюся задачу worker завершит сам между батчами.
func (s *server) pauseTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(ctx context.Context, id, owner uuid.UUID) (*models.TranslationProject, error) {
		return queue.Pause(ctx, s.DB, id, owner)
	})
}

// resume продолжает с места: и после паузы, и после сбоя провайдера.
func (s *server) resumeTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(ctx context.Context, id, owner uuid.UUID) (*models.TranslationProject, error) {
		return queue.Resume(ctx, s.DB, id, owner, s.Cfg.MaxSegmentAttempts)
	})
}

// translationEstimate — примерный объём перевода книги в токенах
// (дополнение §20). Цену по нему считает интерфейс: цены моделей у него
// есть в списке моделей подключения.
func (s *server) translationEstimate(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	tgt := r.URL.Query().Get("target_lang")
	if !translation.IsKnownLanguage(tgt) {
		writeError(w, http.StatusUnprocessableEntity, "Неизвестный язык перевода")
		return
	}
	src := r.URL.Query().Get("source_lang")
	if src == "" {
		src = "auto"
	}
	writeJSON(w, http.StatusOK, translation.Estimate(book.CDM.Data, src, tgt, s.Cfg.MaxBatchSegments, s.Cfg.MaxBatchChars))
}

// errNothingToRetry — в проекте нет сегментов с ошибками и предупреждениями.
var errNothingToRetry = errors.New("нечего переводить заново")

// retryProblems ставит заново все сегменты с ошибками и с предупреждениями
// (ТЗ §12, дополнение §5). Правила — как у повтора одного сегмента: во
// время перевода — 409 (worker уже взял список ожидающих), на паузе —
// сегменты ждут «Продолжить». Прежний перевод сегмента с предупреждением
// останется, если повтор не удастся (queue.Worker.persist).
func (s *server) retryProblems(w http.ResponseWriter, r *http.Request) {
	project, ok := loadOwned[models.TranslationProject](s, w, r, "id")
	if !ok {
		return
	}
	switch project.Status {
	case models.ProjectRunning:
		writeError(w, http.StatusConflict, "Перевод идёт — поставьте его на паузу или дождитесь окончания")
		return
	case models.ProjectCancelled:
		writeError(w, http.StatusConflict, "Перевод отменён")
		return
	}
	var queued int64
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.TranslationSegment{}).
			Where("project_id = ? AND (status = ? OR (status = ? AND severity = ?))",
				project.ID, models.SegmentFailed, models.SegmentCompleted, string(translation.Warning)).
			Updates(map[string]any{"status": models.SegmentPending, "attempts": 0, "last_error": "", "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		if queued = res.RowsAffected; queued == 0 {
			return errNothingToRetry
		}
		if err := queue.RefreshCounters(r.Context(), tx, project.ID, queue.Usage{}); err != nil {
			return err
		}
		if project.Status == models.ProjectPaused {
			return nil // переведутся после «Продолжить»
		}
		res = tx.Model(&models.TranslationProject{}).Where("id = ?", project.ID).
			Updates(map[string]any{"status": models.ProjectQueued, "finished_at": nil, "last_error": "", "updated_at": time.Now().UTC()})
		if res.Error != nil {
			return res.Error
		}
		_, err := queue.Enqueue(r.Context(), tx, project, 0, s.Cfg.MaxSegmentAttempts)
		return err
	})
	if errors.Is(err, errNothingToRetry) {
		writeError(w, http.StatusConflict, "Фрагментов с ошибками и предупреждениями нет")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if err := s.DB.WithContext(r.Context()).First(project, "id = ?", project.ID).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": queued, "project": s.projectFor(r, project)})
}

func (s *server) cancelTranslation(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(ctx context.Context, id, owner uuid.UUID) (*models.TranslationProject, error) {
		return queue.Cancel(ctx, s.DB, id, owner)
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
	p, ok := loadShared[models.TranslationProject](s, w, r, "id")
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
	if raw := r.URL.Query().Get("chapter"); raw != "" {
		chapter, err := strconv.Atoi(raw)
		if err != nil || chapter < 0 {
			writeError(w, http.StatusUnprocessableEntity, "chapter — номер главы, 0 и больше")
			return
		}
		q = q.Where("chapter_index = ?", chapter)
	}
	if severity := r.URL.Query().Get("severity"); severity != "" {
		if !slices.Contains(segmentSeverities, severity) {
			writeError(w, http.StatusUnprocessableEntity, "severity: "+strings.Join(segmentSeverities, ", "))
			return
		}
		q = q.Where("severity = ?", severity)
	}
	// Поиск по тексту оригинала и перевода — те же правила, что в читалке
	// (дополнение §19): найти фрагмент, чтобы поправить или повторить его.
	if text := strings.TrimSpace(r.URL.Query().Get("q")); text != "" {
		if n := utf8.RuneCountInString(text); n < searchMinRunes || n > searchMaxRunes {
			writeError(w, http.StatusUnprocessableEntity, "Запрос — от 2 до 200 символов")
			return
		}
		book, err := store.Owned[models.Book](r.Context(), s.DB, p.BookID, p.OwnerID)
		if err != nil {
			internalError(w, r, err)
			return
		}
		nodes, err := booksearch.Nodes(r.Context(), s.DB, booksearch.Params{Book: book, Project: p, Query: text})
		if err != nil {
			internalError(w, r, err)
			return
		}
		if len(nodes) == 0 {
			writeJSON(w, http.StatusOK, []segmentOut{})
			return
		}
		q = q.Where("node_id IN ?", nodes)
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
	usageOut
	LastError string `json:"last_error,omitempty"`
}

// translationEvents — поток прогресса (ТЗ §8.4, дополнение §13): событие
// при каждом изменении, комментарий-«пинг» для прокси, закрытие на
// терминальном статусе, при отключении клиента и при остановке сервера
// (клиент EventSource переподключится сам).
func (s *server) translationEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := loadShared[models.TranslationProject](s, w, r, "id")
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
			WarningsCount: p.WarningsCount, ProgressPercent: p.ProgressPercent(), CostUSD: p.CostUSD,
			usageOut: usageResponse(p), LastError: p.LastError,
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
