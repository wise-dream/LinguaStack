package httpapi_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
)

type projectBody struct {
	ID                  string  `json:"id"`
	BookID              string  `json:"book_id"`
	TargetLang          string  `json:"target_lang"`
	SourceLang          string  `json:"source_lang"`
	Status              string  `json:"status"`
	SegmentsTotal       int     `json:"segments_total"`
	SegmentsDone        int     `json:"segments_done"`
	SegmentsFailed      int     `json:"segments_failed"`
	ProgressPercent     float64 `json:"progress_percent"`
	SendsDataExternally bool    `json:"sends_data_externally"`
	LastError           string  `json:"last_error"`
	BookTitle           string  `json:"book_title"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	ProviderCalls       int     `json:"provider_calls"`
}

type segmentBody struct {
	ID             string  `json:"id"`
	NodeID         string  `json:"node_id"`
	SourceText     string  `json:"source_text"`
	TranslatedText *string `json:"translated_text"`
	Status         string  `json:"status"`
	Severity       string  `json:"severity"`
}

// translationEnv — API и worker с Fake-провайдером в одной транзакции теста.
type translationEnv struct {
	*testEnv
	fake *providers.Fake
	c    *apiClient
}

func newTranslationEnv(t *testing.T, adjust func(*httpapi.Deps)) *translationEnv {
	t.Helper()
	fake := providers.NewFake("prefix")
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.BuildProvider = func(models.ProviderConfig, string) (providers.Provider, error) { return fake, nil }
		d.SSEInterval = 10 * time.Millisecond
		if adjust != nil {
			adjust(d)
		}
	})
	return &translationEnv{testEnv: env, fake: fake, c: env.signedIn(t, "u@example.com")}
}

func (e *translationEnv) book(t *testing.T, md string) string {
	t.Helper()
	return uploadBook(t, e.c, "book.md", md).ID
}

func (e *translationEnv) ollamaConfig(t *testing.T) string {
	t.Helper()
	_, cfg := createConfig(t, e.c, map[string]any{"provider_id": "ollama"})
	return cfg.ID
}

func (e *translationEnv) create(t *testing.T, bookID string, body map[string]any) *response {
	t.Helper()
	full := map[string]any{"source_lang": "en", "target_lang": "ru", "model_id": "fake-1"}
	for k, v := range body {
		full[k] = v
	}
	return e.c.do(http.MethodPost, "/api/books/"+bookID+"/translations", full)
}

func (e *translationEnv) project(t *testing.T, bookID string) projectBody {
	t.Helper()
	r := e.create(t, bookID, map[string]any{"provider_config_id": e.ollamaConfig(t)})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	var p projectBody
	r.JSON(t, &p)
	return p
}

// runWorker прогоняет очередь настоящим worker-ом с Fake-провайдером.
func (e *translationEnv) runWorker(t *testing.T) {
	t.Helper()
	w := queue.NewWorker("test-worker", e.db, func(context.Context, *models.TranslationProject) (providers.Provider, error) {
		return e.fake, nil
	}, e.cfg)
	w.RetryBackoff = func(int) time.Duration { return 0 }
	for range 20 {
		worked, err := w.RunOnce(t.Context())
		require.NoError(t, err)
		if !worked {
			return
		}
	}
}

func (e *translationEnv) get(t *testing.T, id string) projectBody {
	t.Helper()
	var p projectBody
	e.c.do(http.MethodGet, "/api/translations/"+id, nil).JSON(t, &p)
	return p
}

func (e *translationEnv) segments(t *testing.T, id string) []segmentBody {
	t.Helper()
	var segs []segmentBody
	r := e.c.do(http.MethodGet, "/api/translations/"+id+"/segments", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &segs)
	return segs
}

func TestCreateTranslationEnqueues(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	require.Equal(t, "queued", p.Status)
	require.False(t, p.SendsDataExternally)

	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", p.ID).Error)
	require.Equal(t, models.TaskPending, task.Status)
}

func TestTranslationRunsEndToEndWithWorker(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne.\n\n```go\ncode()\n```\n\nTwo."))
	e.runWorker(t)

	done := e.get(t, p.ID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, 3, done.SegmentsDone)
	require.InDelta(t, 100, done.ProgressPercent, 0)
	var texts []string
	for _, s := range e.segments(t, p.ID) {
		texts = append(texts, *s.TranslatedText)
	}
	require.Equal(t, []string{"RU:Ch", "RU:One.", "RU:Two."}, texts)
}

func TestCreateTranslationValidates(t *testing.T) {
	e := newTranslationEnv(t, nil)
	book := e.book(t, "# Ch\n\nOne.")
	cfg := e.ollamaConfig(t)
	cases := map[string]map[string]any{
		"неизвестный язык перевода":  {"provider_config_id": cfg, "target_lang": "klingon"},
		"неизвестный язык исходника": {"provider_config_id": cfg, "source_lang": "xx"},
		"неизвестный стиль":          {"provider_config_id": cfg, "style": "pirate"},
		"неизвестная детерминизм":    {"provider_config_id": cfg, "determinism": "chaotic"},
		"нет модели":                 {"provider_config_id": cfg, "model_id": ""},
		"нет конфигурации":           {"provider_config_id": "00000000-0000-0000-0000-000000000000"},
		"не UUID":                    {"provider_config_id": "nope"},
		"огромный глоссарий":         {"provider_config_id": cfg, "glossary": hugeGlossary()},
	}
	for name, body := range cases {
		require.Equal(t, http.StatusUnprocessableEntity, e.create(t, book, body).Status, name)
	}
}

func hugeGlossary() map[string]string {
	g := map[string]string{}
	for i := range 600 {
		g[strings.Repeat("t", 3)+string(rune('a'+i%26))+strings.Repeat("x", i%40)] = "перевод"
	}
	return g
}

func TestAutoSourceLanguageUsesBookMetadataWhenKnown(t *testing.T) {
	e := newTranslationEnv(t, nil)
	r := e.create(t, e.book(t, "# Ch\n\nOne."), map[string]any{"provider_config_id": e.ollamaConfig(t), "source_lang": "auto"})
	require.Equal(t, http.StatusCreated, r.Status)
	var p projectBody
	r.JSON(t, &p)
	require.Equal(t, "auto", p.SourceLang, "у Markdown язык в метаданных неизвестен")
}

func TestForeignConfigCannotBeUsed(t *testing.T) {
	e := newTranslationEnv(t, nil)
	other := e.signedIn(t, "other@example.com")
	_, foreign := createConfig(t, other, map[string]any{"provider_id": "ollama"})
	r := e.create(t, e.book(t, "# Ch\n\nOne."), map[string]any{"provider_config_id": foreign.ID})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

func TestExternalProviderRejectedWhenEgressForbidden(t *testing.T) {
	// ТЗ §6.4: пользователь может запретить передачу текста внешним сервисам.
	e := newTranslationEnv(t, nil)
	book := e.book(t, "# Ch\n\nOne.")
	_, forbidden := createConfig(t, e.c, map[string]any{"provider_id": "anthropic", "api_key": secretKey, "allow_external": false})
	r := e.create(t, book, map[string]any{"provider_config_id": forbidden.ID, "model_id": "claude-opus-5-5"})
	require.Equal(t, http.StatusBadRequest, r.Status)
	require.Contains(t, strings.ToLower(string(r.Body)), "внешн")

	_, allowed := createConfig(t, e.c, map[string]any{"provider_id": "anthropic", "api_key": secretKey, "allow_external": true})
	r = e.create(t, book, map[string]any{"provider_config_id": allowed.ID, "model_id": "claude-opus-5-5"})
	require.Equal(t, http.StatusCreated, r.Status)
	var p projectBody
	r.JSON(t, &p)
	require.True(t, p.SendsDataExternally, "интерфейс обязан предупредить (ТЗ §6.3)")
}

func TestTooManyActiveProjectsIs429(t *testing.T) {
	// ТЗ §13: ограничение количества активных задач.
	e := newTranslationEnv(t, func(d *httpapi.Deps) { d.Cfg.MaxProjectsPerUser = 2 })
	book := e.book(t, "# Ch\n\nOne.")
	cfg := e.ollamaConfig(t)
	var codes []int
	for _, lang := range []string{"ru", "de", "fr"} {
		codes = append(codes, e.create(t, book, map[string]any{"provider_config_id": cfg, "target_lang": lang}).Status)
	}
	require.Equal(t, []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests}, codes)
}

func TestTwoProjectsOnOneBookTranslateIndependently(t *testing.T) {
	// ТЗ §5.1, Review Focus №4.
	e := newTranslationEnv(t, nil)
	book := e.book(t, "# Ch\n\nOne.")
	cfg := e.ollamaConfig(t)
	var ru, de projectBody
	e.create(t, book, map[string]any{"provider_config_id": cfg, "target_lang": "ru"}).JSON(t, &ru)
	e.create(t, book, map[string]any{"provider_config_id": cfg, "target_lang": "de"}).JSON(t, &de)
	e.runWorker(t)
	require.Equal(t, "completed", e.get(t, ru.ID).Status)
	require.Equal(t, "completed", e.get(t, de.ID).Status)
	require.Len(t, e.segments(t, ru.ID), 2)
	require.Len(t, e.segments(t, de.ID), 2)
}

func TestPauseResumeCancelTransitions(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	act := func(action string) int {
		return e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/"+action, nil).Status
	}
	require.Equal(t, http.StatusOK, act("pause"))
	require.Equal(t, "paused", e.get(t, p.ID).Status)
	var pending int64
	require.NoError(t, e.db.Model(&models.TranslationTask{}).Where("project_id = ? AND status = 'pending'", p.ID).Count(&pending).Error)
	require.Zero(t, pending, "ожидающая задача снята с очереди")

	require.Equal(t, http.StatusOK, act("resume"))
	require.Equal(t, "queued", e.get(t, p.ID).Status)
	require.Equal(t, http.StatusOK, act("cancel"))
	require.Equal(t, "cancelled", e.get(t, p.ID).Status)

	require.Equal(t, http.StatusConflict, act("resume"), "отменённый проект не возобновляется")
	require.Equal(t, http.StatusConflict, act("pause"))
	require.Equal(t, http.StatusConflict, act("cancel"))
}

func TestResumeFailedProjectContinues(t *testing.T) {
	// Провайдер был недоступен до исчерпания попыток — после «Продолжить»
	// перевод идёт дальше с места.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.fake.SetUnavailable(true)
	e.runWorker(t)
	require.Equal(t, "failed", e.get(t, p.ID).Status)

	e.fake.SetUnavailable(false)
	require.Equal(t, http.StatusOK, e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/resume", nil).Status)
	e.runWorker(t)
	require.Equal(t, "completed", e.get(t, p.ID).Status)
}

func TestResumeRetriesFailedSegments(t *testing.T) {
	// Модель во всех попытках портила защищённый фрагмент — проект
	// «ошибка». «Продолжить» (в вебе и в Telegram — один код) переводит
	// такие фрагменты заново, а не завершается сразу с той же ошибкой.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nCall `run()` now.\n\nPlain text."))
	e.fake.Scenario = "drops_placeholder"
	e.runWorker(t)
	require.Equal(t, "failed", e.get(t, p.ID).Status)

	e.fake.Scenario = "prefix"
	require.Equal(t, http.StatusOK, e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/resume", nil).Status)
	for _, seg := range e.segments(t, p.ID) {
		require.NotEqual(t, "failed", seg.Status, "упавший фрагмент снова ждёт перевода: %s", seg.NodeID)
	}
	e.runWorker(t)
	require.Equal(t, "completed", e.get(t, p.ID).Status)
	for _, seg := range e.segments(t, p.ID) {
		require.Equal(t, "completed", seg.Status, seg.NodeID)
	}
}

// problemProject — переведённая книга из трёх фрагментов: заголовок,
// первый абзац «упал», второй принят с предупреждением.
func problemProject(t *testing.T, e *translationEnv) (projectBody, []segmentBody) {
	t.Helper()
	p := e.project(t, e.book(t, "# Ch\n\nOne.\n\nTwo."))
	e.runWorker(t)
	segs := e.segments(t, p.ID)
	require.Len(t, segs, 3)
	require.NoError(t, e.db.Model(&models.TranslationSegment{}).Where("id = ?", segs[1].ID).
		Updates(map[string]any{"status": "failed", "severity": "error", "translated_text": nil}).Error)
	require.NoError(t, e.db.Model(&models.TranslationSegment{}).Where("id = ?", segs[2].ID).
		Update("severity", "warning").Error)
	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", "failed").Error)
	return p, segs
}

func TestRetryProblemSegments(t *testing.T) {
	// Одной кнопкой — заново все фрагменты с ошибками и предупреждениями;
	// нормальные не трогаются.
	e := newTranslationEnv(t, nil)
	p, segs := problemProject(t, e)

	r := e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/retry-problems", nil)
	require.Equal(t, http.StatusAccepted, r.Status, "тело: %s", r.Body)
	var out struct {
		Queued  int         `json:"queued"`
		Project projectBody `json:"project"`
	}
	r.JSON(t, &out)
	require.Equal(t, 2, out.Queued)
	require.Equal(t, "queued", out.Project.Status)
	status := map[string]string{}
	for _, s := range e.segments(t, p.ID) {
		status[s.ID] = s.Status
	}
	require.Equal(t, "completed", status[segs[0].ID], "нормальный фрагмент не трогается")
	require.Equal(t, "pending", status[segs[1].ID])
	require.Equal(t, "pending", status[segs[2].ID])

	e.runWorker(t)
	require.Equal(t, "completed", e.get(t, p.ID).Status)
	for _, s := range e.segments(t, p.ID) {
		require.Equal(t, "completed", s.Status, s.NodeID)
		require.Equal(t, "ok", s.Severity, s.NodeID)
	}
	require.Equal(t, http.StatusConflict, e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/retry-problems", nil).Status,
		"повторять нечего")
}

func TestRetryProblemSegmentsRespectsState(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p, segs := problemProject(t, e)
	retry := func() int { return e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/retry-problems", nil).Status }

	// Идёт перевод — worker уже взял список фрагментов: позже.
	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", "running").Error)
	require.Equal(t, http.StatusConflict, retry())

	// На паузе — фрагменты ждут «Продолжить», задача не ставится.
	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", "paused").Error)
	require.Equal(t, http.StatusAccepted, retry())
	require.Equal(t, "paused", e.get(t, p.ID).Status)
	var tasks int64
	require.NoError(t, e.db.Model(&models.TranslationTask{}).Where("project_id = ? AND status = 'pending'", p.ID).Count(&tasks).Error)
	require.Zero(t, tasks)
	for _, s := range e.segments(t, p.ID) {
		if s.ID == segs[1].ID {
			require.Equal(t, "pending", s.Status)
		}
	}

	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", "cancelled").Error)
	require.Equal(t, http.StatusConflict, retry())
}

func TestCompletedProjectCannotBeCancelledOrPaused(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	for _, action := range []string{"pause", "cancel", "resume"} {
		require.Equal(t, http.StatusConflict, e.c.do(http.MethodPost, "/api/translations/"+p.ID+"/"+action, nil).Status, action)
	}
}

func TestListTranslationsOnlyOwn(t *testing.T) {
	e := newTranslationEnv(t, nil)
	e.project(t, e.book(t, "# Ch\n\nOne."))
	other := &translationEnv{testEnv: e.testEnv, fake: e.fake, c: e.signedIn(t, "other@example.com")}
	other.project(t, other.book(t, "# Чужая\n\nx."))

	var mine []projectBody
	e.c.do(http.MethodGet, "/api/translations", nil).JSON(t, &mine)
	require.Len(t, mine, 1)
}

func TestTranslationOptions(t *testing.T) {
	// Списки для формы настройки перевода (ТЗ §8.1 п.5).
	e := newTranslationEnv(t, nil)
	var opts struct {
		Languages []struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"languages"`
		Styles      []string `json:"styles"`
		Determinism []string `json:"determinism"`
	}
	r := e.c.do(http.MethodGet, "/api/translation-options", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &opts)
	codes := map[string]string{}
	for _, l := range opts.Languages {
		codes[l.Code] = l.Name
	}
	require.Equal(t, "English", codes["en"])
	require.Equal(t, "Russian", codes["ru"])
	require.True(t, sort.SliceIsSorted(opts.Languages, func(i, j int) bool { return opts.Languages[i].Code < opts.Languages[j].Code }))
	require.Equal(t, []string{"literary", "neutral", "simplified", "technical"}, opts.Styles)
	require.Equal(t, []string{"strict", "balanced", "creative"}, opts.Determinism)
}

func TestTranslationResponseHasUsage(t *testing.T) {
	// ТЗ §8.4: монитор показывает доступную статистику провайдера.
	e := newTranslationEnv(t, nil)
	e.fake.TokensPerCall = 7
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	done := e.get(t, p.ID)
	require.Equal(t, e.fake.CallCount(), done.ProviderCalls)
	require.Equal(t, int64(7*done.ProviderCalls), done.InputTokens)
	require.Equal(t, int64(7*done.ProviderCalls), done.OutputTokens)

	events := readSSE(t, e.c, t.Context(), "/api/translations/"+p.ID+"/events", 1)
	require.Contains(t, events[0], fmt.Sprintf(`"provider_calls":%d`, done.ProviderCalls))
	require.Contains(t, events[0], `"input_tokens":`)
}

func TestTranslationHasBookTitle(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Глава\n\nOne."))
	var list []projectBody
	e.c.do(http.MethodGet, "/api/translations", nil).JSON(t, &list)
	require.Len(t, list, 1)
	require.Equal(t, e.get(t, p.ID).BookTitle, list[0].BookTitle)
	require.NotEmpty(t, list[0].BookTitle)
}

func TestTranslationsFilterByBook(t *testing.T) {
	e := newTranslationEnv(t, nil)
	first := e.book(t, "# A\n\nOne.")
	second := e.book(t, "# B\n\nTwo.")
	p := e.project(t, first)
	e.project(t, second)

	var list []projectBody
	e.c.do(http.MethodGet, "/api/translations?book_id="+first, nil).JSON(t, &list)
	require.Len(t, list, 1)
	require.Equal(t, p.ID, list[0].ID)

	// Чужая книга — пустой список, а не 404: фильтр не раскрывает,
	// существует ли книга.
	other := e.signedIn(t, "other@example.com")
	r := other.do(http.MethodGet, "/api/translations?book_id="+first, nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.JSONEq(t, "[]", string(r.Body))

	require.Equal(t, http.StatusUnprocessableEntity, e.c.do(http.MethodGet, "/api/translations?book_id=nope", nil).Status)
}

func TestSegmentsListIsPaged(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nA.\n\nB.\n\nC.\n\nD."))
	e.runWorker(t)
	var page []segmentBody
	e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?offset=1&limit=2", nil).JSON(t, &page)
	require.Len(t, page, 2)
	require.Equal(t, "A.", page[0].SourceText, "порядок чтения: заголовок, затем абзацы")
	require.Equal(t, http.StatusUnprocessableEntity,
		e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?limit=99999", nil).Status)
}

func TestSegmentsFilterByChapter(t *testing.T) {
	// Читалка грузит сегменты главы, а не всей книги.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# A\n\nOne.\n\n# B\n\nTwo.\n\nThree."))
	e.runWorker(t)
	var first, second []segmentBody
	e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?chapter=0", nil).JSON(t, &first)
	e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?chapter=1", nil).JSON(t, &second)
	require.Len(t, first, 2, "заголовок и абзац главы A")
	require.Equal(t, "One.", first[1].SourceText)
	require.Len(t, second, 3)
	for _, bad := range []string{"chapter=-1", "chapter=abc"} {
		require.Equal(t, http.StatusUnprocessableEntity,
			e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?"+bad, nil).Status, bad)
	}
}

func TestSegmentsFilterByStatusAndSeverity(t *testing.T) {
	// ТЗ §5.3: не прошедший проверку результат должен находиться и
	// обрабатываться повторно — среди сотен сегментов книги, а не перебором.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nGood.\n\nBad."))
	e.fake.FailForTexts = map[string]bool{"Bad.": true}
	e.runWorker(t)

	list := func(query string) []string {
		var page []segmentBody
		r := e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?"+query, nil)
		require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
		r.JSON(t, &page)
		texts := []string{}
		for _, s := range page {
			texts = append(texts, s.SourceText)
		}
		return texts
	}
	require.Equal(t, []string{"Bad."}, list("status=failed"))
	require.Equal(t, []string{"Bad."}, list("severity=error"))
	require.Equal(t, []string{"Ch", "Good."}, list("status=completed"))
	require.Empty(t, list("status=completed&severity=error"))
	require.Equal(t, []string{"Good."}, list("status=completed&offset=1"), "пагинация — по отфильтрованному")

	for _, bad := range []string{"status=done", "severity=fatal"} {
		require.Equal(t, http.StatusUnprocessableEntity,
			e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?"+bad, nil).Status, bad)
	}
}

func TestPatchSegmentRecordsManualRevision(t *testing.T) {
	// ТЗ §8.3: ручная правка перевода; ТЗ §5.1: прежняя версия сохраняется.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	seg := e.segments(t, p.ID)[1]

	r := e.c.do(http.MethodPatch, "/api/segments/"+seg.ID, map[string]string{"translated_text": "Мой вариант"})
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var updated segmentBody
	r.JSON(t, &updated)
	require.Equal(t, "Мой вариант", *updated.TranslatedText)

	var revisions []struct {
		TranslatedText string `json:"translated_text"`
		Origin         string `json:"origin"`
	}
	e.c.do(http.MethodGet, "/api/segments/"+seg.ID+"/revisions", nil).JSON(t, &revisions)
	require.Len(t, revisions, 2)
	require.Equal(t, "machine", revisions[0].Origin)
	require.Equal(t, "RU:One.", revisions[0].TranslatedText)
	require.Equal(t, "manual", revisions[1].Origin)

	require.Equal(t, http.StatusUnprocessableEntity,
		e.c.do(http.MethodPatch, "/api/segments/"+seg.ID, map[string]string{"translated_text": "   "}).Status)
}

func TestRetrySegmentRequeuesOnlyIt(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nGood.\n\nBad."))
	e.fake.FailForTexts = map[string]bool{"Bad.": true}
	e.runWorker(t)
	require.Equal(t, "failed", e.get(t, p.ID).Status)

	var bad segmentBody
	for _, s := range e.segments(t, p.ID) {
		if s.SourceText == "Bad." {
			bad = s
		}
	}
	require.Equal(t, "failed", bad.Status)
	calls := e.fake.CallCount()

	r := e.c.do(http.MethodPost, "/api/segments/"+bad.ID+"/retry", nil)
	require.Equal(t, http.StatusAccepted, r.Status, "тело: %s", r.Body)
	var retried segmentBody
	r.JSON(t, &retried)
	require.Equal(t, "pending", retried.Status)
	require.Equal(t, "queued", e.get(t, p.ID).Status)

	e.fake.FailForTexts = nil
	e.runWorker(t)
	done := e.get(t, p.ID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, 3, done.SegmentsDone)
	require.Equal(t, calls+1, e.fake.CallCount(), "переводится только повторённый сегмент")
}

func TestRetryWhileRunningIs409(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	seg := e.segments(t, p.ID)[0]
	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", "running").Error)
	require.Equal(t, http.StatusConflict, e.c.do(http.MethodPost, "/api/segments/"+seg.ID+"/retry", nil).Status)
}

func readSSE(t *testing.T, c *apiClient, ctx context.Context, path string, max int) []string {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	require.NoError(t, err)
	resp, err := c.http.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"))
	var events []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "data:") {
			events = append(events, line)
			if len(events) >= max {
				break
			}
		}
	}
	return events
}

func TestSSEEmitsProgressAndClosesOnTerminalStatus(t *testing.T) {
	// ADD §13, ТЗ §8.4: прогресс без перезагрузки страницы.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	events := readSSE(t, e.c, t.Context(), "/api/translations/"+p.ID+"/events", 10)
	require.Len(t, events, 1, "завершённый проект: одно событие, и поток закрывается")
	require.Contains(t, events[0], `"status":"completed"`)
	require.Contains(t, events[0], `"progress_percent":100`)
}

func TestSSEClosesWhenServerStops(t *testing.T) {
	// http.Server.Shutdown не отменяет контексты запросов и ждёт их конца,
	// а поток сам не закрывается: без сигнала остановки каждый перезапуск
	// api висел бы до тайм-аута и завершался ошибкой.
	stopping, stop := context.WithCancel(t.Context())
	e := newTranslationEnv(t, func(d *httpapi.Deps) { d.Stopping = stopping })
	p := e.project(t, e.book(t, "# Ch\n\nOne.")) // queued — поток сам не закроется
	time.AfterFunc(100*time.Millisecond, stop)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	events := readSSE(t, e.c, ctx, "/api/translations/"+p.ID+"/events", 1000)
	require.NotEmpty(t, events)
	require.NoError(t, ctx.Err(), "поток закрыл сервер, а не тайм-аут клиента")
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestSSEStopsWhenClientDisconnects(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne.")) // queued — поток сам не закроется
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	events := readSSE(t, e.c, ctx, "/api/translations/"+p.ID+"/events", 1)
	require.Len(t, events, 1)
	require.Contains(t, events[0], `"status":"queued"`)
	// Закрытие сервера в Cleanup зависло бы, если обработчик не заметил отключения.
}
