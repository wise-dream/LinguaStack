package queue_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/metrics"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/testdb"
	"github.com/vnovels/linguastack/internal/translation"
)

// workerEnv — worker с Fake-провайдером поверх коммитящей базы пакета.
type workerEnv struct {
	t        *testing.T
	db       *gorm.DB
	cfg      config.Settings
	provider providers.Provider
	fake     *providers.Fake
	hook     func(project uuid.UUID, batch int)
	batches  int
}

func newWorkerEnv(t *testing.T) *workerEnv {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.MaxBatchSegments = 2 // несколько батчей даже на маленькой книге
	fake := providers.NewFake("prefix")
	return &workerEnv{t: t, db: testdb.Committing(t, "queue"), cfg: cfg, provider: fake, fake: fake}
}

func (e *workerEnv) worker(id string) *queue.Worker {
	w := queue.NewWorker(id, e.db, func(context.Context, *models.TranslationProject) (providers.Provider, error) {
		return e.provider, nil
	}, e.cfg)
	w.RetryBackoff = func(int) time.Duration { return 0 }
	w.AfterBatch = func(project uuid.UUID) {
		e.batches++
		if e.hook != nil {
			e.hook(project, e.batches)
		}
	}
	return w
}

func (e *workerEnv) projectFromMarkdown(md string) *models.TranslationProject {
	e.t.Helper()
	res, err := importers.NewMarkdown().Load([]byte(md), "book.md")
	require.NoError(e.t, err)
	project := testdb.MakeProject(e.t, e.db, &testdb.ProjectOptions{Document: &res.Document})
	_, err = queue.Enqueue(e.t.Context(), e.db, project, 0, e.cfg.MaxSegmentAttempts)
	require.NoError(e.t, err)
	return project
}

func paragraphs(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf("Para %d.", i)
	}
	return "# Ch\n\n" + strings.Join(parts, "\n\n")
}

// runUntilIdle выполняет задачи, пока очередь не опустеет.
func (e *workerEnv) runUntilIdle(w *queue.Worker) {
	e.t.Helper()
	for range 50 {
		worked, err := w.RunOnce(e.t.Context())
		require.NoError(e.t, err)
		if !worked {
			return
		}
	}
	e.t.Fatal("очередь не опустела за 50 итераций")
}

func (e *workerEnv) reload(p *models.TranslationProject) models.TranslationProject {
	e.t.Helper()
	var fresh models.TranslationProject
	require.NoError(e.t, e.db.First(&fresh, "id = ?", p.ID).Error)
	return fresh
}

func (e *workerEnv) segments(p *models.TranslationProject) []models.TranslationSegment {
	e.t.Helper()
	var segs []models.TranslationSegment
	require.NoError(e.t, e.db.Where("project_id = ?", p.ID).Order("chapter_index, ordinal").Find(&segs).Error)
	return segs
}

func (e *workerEnv) setStatus(p uuid.UUID, status string) {
	require.NoError(e.t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p).Update("status", status).Error)
}

func (e *workerEnv) requeue(p *models.TranslationProject) {
	e.t.Helper()
	e.setStatus(p.ID, models.ProjectQueued)
	fresh := e.reload(p)
	_, err := queue.Enqueue(e.t.Context(), e.db, &fresh, 0, e.cfg.MaxSegmentAttempts)
	require.NoError(e.t, err)
}

func TestWorkerTranslatesWholeBook(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.\n\nTwo.\n\n```go\ncode()\n```\n\nThree.")
	e.runUntilIdle(e.worker("w"))

	done := e.reload(project)
	require.Equal(t, models.ProjectCompleted, done.Status)
	require.Equal(t, 4, done.SegmentsTotal, "заголовок и три абзаца; код не переводится")
	require.Equal(t, 4, done.SegmentsDone)
	require.Zero(t, done.SegmentsFailed)
	require.NotNil(t, done.StartedAt)
	require.NotNil(t, done.FinishedAt)

	var texts []string
	for _, s := range e.segments(project) {
		require.Equal(t, models.SegmentCompleted, s.Status)
		texts = append(texts, *s.TranslatedText)
	}
	require.Equal(t, []string{"RU:Ch", "RU:One.", "RU:Two.", "RU:Three."}, texts)

	var revisions int64
	require.NoError(t, e.db.Model(&models.TranslationSegmentRevision{}).Where("origin = ?", models.OriginMachine).Count(&revisions).Error)
	require.EqualValues(t, 4, revisions, "каждая машинная версия сохранена (ТЗ §5.1)")

	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskCompleted, task.Status)
}

func TestWorkerAccumulatesUsage(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(3)) // 4 сегмента — 2 батча
	e.fake.TokensPerCall = 3
	e.fake.CostPerCall = 0.25
	e.runUntilIdle(e.worker("w"))

	done := e.reload(project)
	require.Equal(t, 2, done.ProviderCalls)
	require.Equal(t, int64(6), done.InputTokens)
	require.Equal(t, int64(6), done.OutputTokens)
	require.InDelta(t, 0.5, done.CostUSD, 1e-9)
}

func TestRerunDoesNotRetranslateCompleted(t *testing.T) {
	// ТЗ §7, §17: идемпотентность — повторный прогон пропускает готовое.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.\n\nTwo.")
	e.runUntilIdle(e.worker("w"))
	calls := e.fake.CallCount()

	e.requeue(project)
	e.runUntilIdle(e.worker("w"))
	require.Equal(t, calls, e.fake.CallCount(), "модель не вызывается снова")
	require.Equal(t, models.ProjectCompleted, e.reload(project).Status)
}

func TestProgressSurvivesWorkerCrash(t *testing.T) {
	// ТЗ §15.8: перевод продолжается после перезапуска сервера.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(6)) // 7 сегментов — 4 батча
	e.hook = func(_ uuid.UUID, batch int) {
		if batch == 2 {
			panic("kill -9")
		}
	}
	func() {
		defer func() { require.NotNil(t, recover()) }()
		_, _ = e.worker("doomed").RunOnce(t.Context())
	}()
	partial := e.reload(project)
	require.Equal(t, 4, partial.SegmentsDone, "два батча по два сегмента сохранены до падения")

	// Аренда упавшего worker-а истекает — задачу берёт новый.
	e.hook = nil
	require.NoError(t, e.db.Model(&models.TranslationTask{}).Where("project_id = ?", project.ID).
		Update("locked_until", time.Now().Add(-time.Second)).Error)
	e.runUntilIdle(e.worker("survivor"))

	done := e.reload(project)
	require.Equal(t, models.ProjectCompleted, done.Status)
	require.Equal(t, 7, done.SegmentsDone)
	require.Equal(t, 7, e.fake.TranslatedCount(), "переведённое до падения не переводится снова")
}

func TestPauseStopsBetweenBatchesKeepingProgress(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(9))
	e.hook = func(p uuid.UUID, batch int) {
		if batch == 1 {
			e.setStatus(p, models.ProjectPaused) // пользователь нажал «Пауза»
		}
	}
	e.runUntilIdle(e.worker("w"))

	paused := e.reload(project)
	require.Equal(t, models.ProjectPaused, paused.Status)
	require.Equal(t, 2, paused.SegmentsDone, "сделанное не теряется")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskCancelled, task.Status, "пауза живёт на проекте, задача завершается")
}

func TestResumeContinuesWithoutRetranslating(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(9))
	e.hook = func(p uuid.UUID, batch int) {
		if batch == 1 {
			e.setStatus(p, models.ProjectPaused)
		}
	}
	e.runUntilIdle(e.worker("w"))
	e.hook = nil

	e.requeue(project)
	e.runUntilIdle(e.worker("w"))
	done := e.reload(project)
	require.Equal(t, models.ProjectCompleted, done.Status)
	require.Equal(t, 10, done.SegmentsDone)
	require.Equal(t, 10, e.fake.TranslatedCount())
}

// cancellingProvider отменяет проект, пока запрос к модели «в полёте».
type cancellingProvider struct {
	*providers.Fake
	onCall func(n int)
	n      int
}

func (c *cancellingProvider) Complete(ctx context.Context, model string, req providers.CompletionRequest) (providers.CompletionResult, error) {
	c.n++
	c.onCall(c.n)
	return c.Fake.Complete(ctx, model, req)
}

func TestCancelDuringBatchKeepsPaidResult(t *testing.T) {
	// Review Focus №5: ответ модели уже оплачен — его нельзя потерять,
	// но и отменённый проект не должен ожить.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(7))
	e.fake.CostPerCall = 0.5
	e.provider = &cancellingProvider{Fake: e.fake, onCall: func(n int) {
		if n == 2 {
			e.setStatus(project.ID, models.ProjectCancelled)
		}
	}}
	e.runUntilIdle(e.worker("w"))

	cancelled := e.reload(project)
	require.Equal(t, models.ProjectCancelled, cancelled.Status, "запись результата не воскрешает проект")
	require.Equal(t, 4, cancelled.SegmentsDone, "батч, отменённый в полёте, сохранён")
	require.InDelta(t, 1.0, cancelled.CostUSD, 1e-9, "его стоимость учтена")
	require.Equal(t, 2, e.fake.CallCount(), "после отмены новых запросов нет")
}

func TestCancelledBeforeStartDoesNotRun(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	e.setStatus(project.ID, models.ProjectCancelled)
	e.runUntilIdle(e.worker("w"))
	require.Equal(t, models.ProjectCancelled, e.reload(project).Status)
	require.Zero(t, e.fake.CallCount())
}

func TestBookWithoutTranslatableBlocksCompletes(t *testing.T) {
	// Review Focus №1: глава из одного кода — 0/0, статус completed, 100%.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("```python\nprint(1)\n```\n")
	e.runUntilIdle(e.worker("w"))
	done := e.reload(project)
	require.Equal(t, models.ProjectCompleted, done.Status)
	require.Zero(t, done.SegmentsTotal)
	require.InDelta(t, 100.0, done.ProgressPercent(), 0)
	require.Zero(t, e.fake.CallCount())
}

func TestUnavailableProviderFailsTaskAfterAttempts(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	e.fake.SetUnavailable(true)
	e.runUntilIdle(e.worker("w"))

	failed := e.reload(project)
	require.Equal(t, models.ProjectFailed, failed.Status)
	require.Contains(t, failed.LastError, "недоступен")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskFailed, task.Status)
	require.Equal(t, e.cfg.MaxSegmentAttempts, task.Attempts)
}

// flakyProvider падает на каждом чётном вызове — сбои перемежаются
// успешными батчами.
type flakyProvider struct {
	*providers.Fake
	n int
}

func (f *flakyProvider) Complete(ctx context.Context, model string, req providers.CompletionRequest) (providers.CompletionResult, error) {
	f.n++
	if f.n%2 == 0 {
		return providers.CompletionResult{}, fmt.Errorf("%w: сбой %d", providers.ErrUnavailable, f.n)
	}
	return f.Fake.Complete(ctx, model, req)
}

func TestProgressResetsAttemptBudget(t *testing.T) {
	// Smoke на «Алисе»: книга перевела 477 сегментов за три часа и упала
	// после трёх сбоев — счётчик попыток копил сбои за всё время. Попытки
	// считают сбои подряд без прогресса: задача, которая двигается, не
	// должна падать из-за редких сбоев, а застрявшая — должна.
	e := newWorkerEnv(t)
	e.cfg.MaxSegmentAttempts = 2
	project := e.projectFromMarkdown(paragraphs(7)) // 4 батча по 2 сегмента
	e.provider = &flakyProvider{Fake: e.fake}
	e.runUntilIdle(e.worker("w"))

	done := e.reload(project)
	require.Equal(t, models.ProjectCompleted, done.Status, "ошибка: %s", done.LastError)
	require.Equal(t, 8, done.SegmentsDone)
}

func TestAuthErrorFailsImmediately(t *testing.T) {
	// Неверный ключ — повторять бессмысленно; попытки не тратятся впустую.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	w := queue.NewWorker("w", e.db, func(context.Context, *models.TranslationProject) (providers.Provider, error) {
		return nil, fmt.Errorf("%w: ключ отозван", providers.ErrAuth)
	}, e.cfg)
	e.runUntilIdle(w)
	failed := e.reload(project)
	require.Equal(t, models.ProjectFailed, failed.Status)
	require.Contains(t, failed.LastError, "ключ отозван")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, 1, task.Attempts)
}

func TestOneBadSegmentDoesNotStopBook(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nGood.\n\nBad.\n\nAlso good.")
	e.fake.FailForTexts = map[string]bool{"Bad.": true}
	e.runUntilIdle(e.worker("w"))

	done := e.reload(project)
	require.Equal(t, models.ProjectFailed, done.Status, "есть сегмент, исчерпавший попытки (ADD §5)")
	require.Equal(t, 3, done.SegmentsDone, "остальные сегменты переведены")
	require.Equal(t, 1, done.SegmentsFailed)
	for _, s := range e.segments(project) {
		if s.SourceText == "Bad." {
			require.Equal(t, models.SegmentFailed, s.Status)
			require.Nil(t, s.TranslatedText)
			require.NotEmpty(t, s.LastError, "последний ответ модели сохранён для разбора")
		}
	}
}

func TestWarningsMakeCompletedWithWarnings(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("This is a reasonably long English sentence that should change.")
	e.fake.Scenario = "echo"
	e.runUntilIdle(e.worker("w"))
	done := e.reload(project)
	require.Equal(t, models.ProjectCompletedWithWarnings, done.Status)
	require.Equal(t, 1, done.WarningsCount)
}

func TestOversizedBlockFailsAloneWithClearMessage(t *testing.T) {
	// Review Focus №3: слишком длинный абзац — понятная ошибка, книга идёт дальше.
	e := newWorkerEnv(t)
	e.cfg.MaxBatchChars = 100
	project := e.projectFromMarkdown("# Ch\n\n" + strings.Repeat("x", 500) + "\n\nShort.")
	e.runUntilIdle(e.worker("w"))
	done := e.reload(project)
	require.Equal(t, 2, done.SegmentsDone)
	require.Equal(t, 1, done.SegmentsFailed)
	for _, s := range e.segments(project) {
		if s.Status == models.SegmentFailed {
			require.Contains(t, s.LastError, "длиннее лимита")
		}
	}
}

func TestExternalEgressRevokedMidwayIsRespected(t *testing.T) {
	// ТЗ §6.4: если пользователь запретил передачу наружу, следующий батч
	// к внешнему сервису не уходит.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	w := queue.NewWorker("w", e.db, queue.ConfigResolver(e.db, nil, providers.BuildOptions{}, false), e.cfg)
	require.NoError(t, e.db.Model(&models.ProviderConfig{}).Where("id = ?", project.ProviderConfigID).
		Updates(map[string]any{"provider_id": "openai_compatible", "base_url": "https://api.example.com/v1", "allow_external": false}).Error)
	e.runUntilIdle(w)
	failed := e.reload(project)
	require.Equal(t, models.ProjectFailed, failed.Status)
	require.Contains(t, failed.LastError, "запрещ")
}

func TestServerOllamaOnlyForAdmins(t *testing.T) {
	// Подключение к Ollama сервера осталось у обычного пользователя (создано
	// до ограничения или его разжаловали): перевод не занимает общий CPU, а
	// падает с понятной причиной.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	require.NoError(t, e.db.Model(&models.ProviderConfig{}).Where("id = ?", project.ProviderConfigID).
		Update("provider_id", "ollama").Error)
	w := queue.NewWorker("w", e.db, queue.ConfigResolver(e.db, nil, providers.BuildOptions{}, true), e.cfg)
	e.runUntilIdle(w)
	failed := e.reload(project)
	require.Equal(t, models.ProjectFailed, failed.Status)
	require.Contains(t, failed.LastError, "только администратору")
}

// gate держит вызовы модели, пока их не наберётся n одновременно: так
// видно, что задачи действительно идут параллельно.
type gate struct {
	*providers.Fake
	started chan struct{}
	release chan struct{}
}

func (g gate) Complete(ctx context.Context, model string, req providers.CompletionRequest) (providers.CompletionResult, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
	case <-ctx.Done():
		return providers.CompletionResult{}, ctx.Err()
	}
	return g.Fake.Complete(ctx, model, req)
}

func TestPoolRunsTasksInParallel(t *testing.T) {
	// Несколько worker-ов в процессе (LINGUASTACK_WORKER_CONCURRENCY):
	// большая книга одного пользователя не держит очередь остальных.
	e := newWorkerEnv(t)
	a := e.projectFromMarkdown("# A\n\nOne.")
	b := e.projectFromMarkdown("# B\n\nTwo.")
	g := gate{Fake: e.fake, started: make(chan struct{}, 8), release: make(chan struct{})}
	pool := make([]*queue.Worker, 2)
	for i := range pool {
		pool[i] = queue.NewWorker(fmt.Sprintf("w%d", i), e.db, func(context.Context, *models.TranslationProject) (providers.Provider, error) {
			return g, nil
		}, e.cfg)
		pool[i].PollInterval = 10 * time.Millisecond
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- queue.RunPool(ctx, pool) }()

	for range 2 {
		select {
		case <-g.started:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("вторая задача не началась, пока идёт первая")
		}
	}
	close(g.release)
	require.Eventually(t, func() bool {
		return e.reload(a).Status == models.ProjectCompleted && e.reload(b).Status == models.ProjectCompleted
	}, 10*time.Second, 20*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
}

func TestFailedRetryKeepsPreviousTranslation(t *testing.T) {
	// Повтор фрагмента с предупреждением не удался — прежний перевод
	// остаётся: приемлемый текст лучше пустого места в книге.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nCall `run()` now.")
	e.runUntilIdle(e.worker("w"))
	seg := e.segments(project)[1]
	require.Equal(t, "RU:Call `run()` now.", *seg.TranslatedText)
	require.NoError(t, e.db.Model(&models.TranslationSegment{}).Where("id = ?", seg.ID).Updates(map[string]any{
		"status": models.SegmentPending, "severity": "warning",
		"findings": models.NewJSON([]translation.Finding{{Code: "length_ratio", Severity: "warning", Message: "короче"}}),
	}).Error)
	e.requeue(project)

	e.fake.Scenario = "drops_placeholder"
	e.runUntilIdle(e.worker("w"))
	got := e.segments(project)[1]
	require.Equal(t, models.SegmentCompleted, got.Status)
	require.Equal(t, "RU:Call `run()` now.", *got.TranslatedText)
	require.Equal(t, "warning", got.Severity)
	require.Equal(t, "length_ratio", got.Findings.Data[0].Code, "замечания прежнего перевода на месте")
	require.Equal(t, models.ProjectCompletedWithWarnings, e.reload(project).Status)
}

func TestWorkerStopsGracefullyFinishingCurrentBatch(t *testing.T) {
	// SIGTERM: текущий батч дописывается, задача отпускается без траты попытки.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(7))
	ctx, stop := context.WithCancel(t.Context())
	e.hook = func(_ uuid.UUID, batch int) {
		if batch == 1 {
			stop()
		}
	}
	require.NoError(t, e.worker("w").Run(ctx))

	stopped := e.reload(project)
	require.Equal(t, 2, stopped.SegmentsDone, "батч, начатый до остановки, дописан")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskPending, task.Status)
	require.Zero(t, task.Attempts, "остановка сервиса — не сбой")

	e.hook = nil
	e.runUntilIdle(e.worker("w2"))
	require.Equal(t, models.ProjectCompleted, e.reload(project).Status)
}

func TestStopDuringCallWaitsForBatchWithinGrace(t *testing.T) {
	// Сигнал пришёл, пока запрос «в полёте»: ответ укладывается в grace и
	// сохраняется — он уже оплачен.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown(paragraphs(7))
	ctx, stop := context.WithCancel(t.Context())
	e.provider = &cancellingProvider{Fake: e.fake, onCall: func(n int) {
		if n == 1 {
			stop()
		}
	}}
	require.NoError(t, e.worker("w").Run(ctx))
	require.Equal(t, 2, e.reload(project).SegmentsDone)
	require.Equal(t, 1, e.fake.CallCount(), "после остановки новых запросов нет")
}

// hangingProvider на вызове hangOn подаёт сигнал остановки и «зависает»
// до отмены контекста — как локальная модель, ушедшая в бесконечную
// генерацию.
type hangingProvider struct {
	*providers.Fake
	hangOn int
	onHang func()
	n      int
}

func (h *hangingProvider) Complete(ctx context.Context, model string, req providers.CompletionRequest) (providers.CompletionResult, error) {
	h.n++
	if h.n == h.hangOn {
		h.onHang()
		<-ctx.Done()
		return providers.CompletionResult{}, fmt.Errorf("запрос к провайдеру прерван: %w", ctx.Err())
	}
	return h.Fake.Complete(ctx, model, req)
}

func TestStopAbortsHungCallAfterGrace(t *testing.T) {
	// Зависший вызов не держит остановку сервиса до тайм-аута провайдера
	// (минуты): после grace он обрывается, сделанное сохранено, задача
	// возвращается в очередь без траты попытки, а проект не помечается
	// упавшим.
	e := newWorkerEnv(t)
	e.cfg.WorkerShutdownGrace = 50 * time.Millisecond
	project := e.projectFromMarkdown(paragraphs(7))
	ctx, stop := context.WithCancel(t.Context())
	e.provider = &hangingProvider{Fake: e.fake, hangOn: 2, onHang: stop}

	done := make(chan error, 1)
	go func() { done <- e.worker("w").Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("worker не остановился после grace")
	}

	stopped := e.reload(project)
	require.Equal(t, models.ProjectRunning, stopped.Status, "остановка сервиса — не сбой проекта")
	require.Empty(t, stopped.LastError)
	require.Equal(t, 2, stopped.SegmentsDone, "первый батч сохранён")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskPending, task.Status)
	require.Zero(t, task.Attempts)

	e.provider = e.fake
	e.runUntilIdle(e.worker("w2"))
	require.Equal(t, models.ProjectCompleted, e.reload(project).Status)
}

func TestCancelDuringLastBatchDoesNotRevive(t *testing.T) {
	// Review Focus №5: отмена пришла во время последнего батча — после него
	// цикл кончается, и итоговый статус не должен затереть отмену.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOnly.")
	e.provider = &cancellingProvider{Fake: e.fake, onCall: func(int) { e.setStatus(project.ID, models.ProjectCancelled) }}
	e.runUntilIdle(e.worker("w"))
	cancelled := e.reload(project)
	require.Equal(t, models.ProjectCancelled, cancelled.Status)
	require.Equal(t, 2, cancelled.SegmentsDone, "и результат последнего батча сохранён")
}

func TestPartialBatchResultsSavedOnInfraFailure(t *testing.T) {
	// Review Focus №5 на уровне worker-а: сервис вернул часть переводов и
	// сбой провайдера — переводы записаны, остальное ждёт повтора.
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("Good.\n\nBad.")
	e.fake.FailForTexts = map[string]bool{"Bad.": true}
	e.fake.FailAfterCalls = 4 // 3 попытки батча, одиночный «Good.», затем сбой
	worked, err := e.worker("w").RunOnce(t.Context())
	require.NoError(t, err)
	require.True(t, worked)

	partial := e.reload(project)
	require.Equal(t, 1, partial.SegmentsDone)
	require.Zero(t, partial.SegmentsFailed, "непереведённый из-за сбоя сегмент — не ошибка, он ждёт повтора")
	var task models.TranslationTask
	require.NoError(t, e.db.First(&task, "project_id = ?", project.ID).Error)
	require.Equal(t, models.TaskPending, task.Status)
}

func (e *workerEnv) bindTelegram(project *models.TranslationProject) {
	e.t.Helper()
	require.NoError(e.t, e.db.Create(&models.TelegramBinding{UserID: project.OwnerID, TelegramUserID: int64(project.OwnerID.ID()),
		ChatID: 1, NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}).Error)
}

func (e *workerEnv) notifications(project *models.TranslationProject) []string {
	e.t.Helper()
	var rows []models.TelegramNotification
	require.NoError(e.t, e.db.Where("project_id = ?", project.ID).Order("created_at, chapter_index").Find(&rows).Error)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
		if r.ChapterIndex != nil {
			out[i] += fmt.Sprintf(":%d", *r.ChapterIndex)
		}
	}
	return out
}

func TestWorkerEnqueuesChapterAndCompleted(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# One\n\nA.\n\nB.\n\nC.\n\n# Two\n\nD.")
	e.bindTelegram(project)
	e.runUntilIdle(e.worker("w"))
	require.Equal(t, models.ProjectCompleted, e.reload(project).Status)
	require.Equal(t, []string{"chapter:0", "chapter:1", "completed"}, e.notifications(project))
}

func TestWorkerEnqueuesFailed(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# Ch\n\nOne.")
	e.bindTelegram(project)
	w := queue.NewWorker("w", e.db, func(context.Context, *models.TranslationProject) (providers.Provider, error) {
		return nil, fmt.Errorf("%w: ключ отозван", providers.ErrAuth)
	}, e.cfg)
	e.runUntilIdle(w)
	require.Equal(t, []string{"failed"}, e.notifications(project))
}

func TestWorkerWithoutBindingEnqueuesNothing(t *testing.T) {
	e := newWorkerEnv(t)
	project := e.projectFromMarkdown("# One\n\nA.")
	e.runUntilIdle(e.worker("w"))
	require.Empty(t, e.notifications(project))
}

func TestWorkerReportsBatchMetrics(t *testing.T) {
	e := newWorkerEnv(t)
	e.projectFromMarkdown(paragraphs(3)) // 4 сегмента — 2 батча
	reg := metrics.NewRegistry()
	w := e.worker("w")
	w.Metrics = metrics.NewWorker(reg)
	e.runUntilIdle(w)

	families, err := reg.Gather()
	require.NoError(t, err)
	values := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := f.GetName()
			for _, l := range m.GetLabel() {
				key += "/" + l.GetValue()
			}
			if c := m.GetCounter(); c != nil {
				values[key] = c.GetValue()
			}
		}
	}
	require.InDelta(t, 2, values["linguastack_worker_batches_total/ok"], 0)
	require.InDelta(t, 4, values["linguastack_worker_segments_total/translated"], 0)
	require.InDelta(t, 2, values["linguastack_worker_provider_calls_total"], 0)
}
