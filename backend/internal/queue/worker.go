package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/translation"
)

// Worker выполняет задачи перевода.
//
// Два контекста. Сигнал остановки (SIGTERM) проверяется только между
// батчами: начатый батч дописывается, иначе уже отправленный запрос к
// платной модели пропал бы. Работу с моделью и БД обрывает только потеря
// аренды — тогда задачу уже ведёт другой worker.
type Worker struct {
	id      string
	db      *gorm.DB
	resolve ProviderResolver
	cfg     config.Settings

	// PollInterval — пауза, когда очередь пуста.
	PollInterval time.Duration
	// RetryBackoff — пауза перед повтором после сбоя инфраструктуры;
	// тесты обнуляют её.
	RetryBackoff func(attempt int) time.Duration
	// AfterBatch вызывается после записи каждого батча (для тестов).
	AfterBatch func(project uuid.UUID)
}

var errStopping = errors.New("worker останавливается")

// NewWorker создаёт worker.
func NewWorker(id string, db *gorm.DB, resolve ProviderResolver, cfg config.Settings) *Worker {
	return &Worker{id: id, db: db, resolve: resolve, cfg: cfg, PollInterval: time.Second, RetryBackoff: defaultBackoff}
}

// defaultBackoff: 30 с, 1 мин, 2 мин … не больше 10 минут.
func defaultBackoff(attempt int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempt && d < 10*time.Minute; i++ {
		d *= 2
	}
	return min(d, 10*time.Minute)
}

// Run выполняет задачи до отмены ctx.
func (w *Worker) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, err := w.RunOnce(ctx)
		if err != nil {
			slog.Error("worker: сбой цикла", "worker", w.id, "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(w.PollInterval):
		}
	}
}

// RunOnce выполняет одну задачу. false — задач нет.
func (w *Worker) RunOnce(stop context.Context) (bool, error) {
	task, err := ClaimNext(stop, w.db, w.id, w.cfg.TaskLease, w.cfg.MaxTasksPerUser)
	if err != nil {
		if stop.Err() != nil {
			return false, nil
		}
		return false, err
	}
	if task == nil {
		return false, nil
	}

	work, cancelWork := context.WithCancel(context.WithoutCancel(stop))
	defer cancelWork()
	leaseLost := make(chan struct{})
	go w.heartbeat(work, task.ID, cancelWork, leaseLost)

	status, perr := w.process(stop, work, task)
	select {
	case <-leaseLost:
		slog.Warn("worker: аренда потеряна, задачу ведёт другой worker", "worker", w.id, "task", task.ID)
		return true, nil
	default:
	}
	return true, w.settle(work, task, status, perr)
}

func (w *Worker) heartbeat(ctx context.Context, taskID uuid.UUID, cancel context.CancelFunc, lost chan<- struct{}) {
	ticker := time.NewTicker(max(w.cfg.TaskLease/3, time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := Heartbeat(ctx, w.db, taskID, w.id, w.cfg.TaskLease)
			if err == nil && !ok {
				close(lost)
				cancel()
				return
			}
		}
	}
}

// settle фиксирует итог задачи в очереди и в проекте.
func (w *Worker) settle(ctx context.Context, task *models.TranslationTask, status string, err error) error {
	switch {
	case err == nil:
		return ignoreLeaseLost(Finish(ctx, w.db, task.ID, w.id, status, ""))
	case errors.Is(err, errStopping):
		return ignoreLeaseLost(Release(ctx, w.db, task.ID, w.id))
	case providers.Retryable(err):
		retryIn := w.RetryBackoff(task.Attempts)
		if d, ok := providers.RetryAfter(err); ok {
			retryIn = max(retryIn, d)
		}
		terminal, ferr := Fail(ctx, w.db, task.ID, w.id, err.Error(), retryIn)
		if ferr != nil {
			return ignoreLeaseLost(ferr)
		}
		if terminal {
			return w.failProject(ctx, task.ProjectID, err)
		}
		return nil
	default:
		// Неверный ключ, нет модели, запрет передачи наружу: повтор не
		// поможет, задача завершается сразу и с причиной.
		if ferr := Finish(ctx, w.db, task.ID, w.id, models.TaskFailed, err.Error()); ferr != nil {
			return ignoreLeaseLost(ferr)
		}
		return w.failProject(ctx, task.ProjectID, err)
	}
}

func ignoreLeaseLost(err error) error {
	if errors.Is(err, ErrLeaseLost) {
		return nil
	}
	return err
}

func (w *Worker) failProject(ctx context.Context, projectID uuid.UUID, cause error) error {
	return w.db.WithContext(ctx).Exec(`
UPDATE translation_projects SET status = 'failed', last_error = ?, finished_at = now(), updated_at = now()
 WHERE id = ? AND status IN ('queued', 'running')`, clip(cause.Error()), projectID).Error
}

// process переводит книгу проекта батч за батчем. Возвращает статус
// задачи: completed — книга пройдена, cancelled — проект приостановлен
// или отменён (сделанное сохранено).
func (w *Worker) process(stop, work context.Context, task *models.TranslationTask) (string, error) {
	var project models.TranslationProject
	if err := w.db.WithContext(work).First(&project, "id = ?", task.ProjectID).Error; err != nil {
		return "", fmt.Errorf("проект задачи: %w", err)
	}
	var book models.Book
	if err := w.db.WithContext(work).First(&book, "id = ? AND owner_id = ?", project.BookID, project.OwnerID).Error; err != nil {
		return "", fmt.Errorf("книга проекта: %w", err)
	}

	refs := translation.Segment(book.CDM.Data)
	if err := w.materialize(work, project.ID, refs); err != nil {
		return "", err
	}
	active, err := w.markRunning(work, project.ID)
	if err != nil || !active {
		return models.TaskCancelled, err
	}

	pending, err := w.pendingNodes(work, project.ID)
	if err != nil {
		return "", err
	}
	batches, oversized := translation.BuildBatches(refs, w.cfg.MaxBatchSegments, w.cfg.MaxBatchChars)
	if err := w.failOversized(work, project.ID, oversized, pending); err != nil {
		return "", err
	}

	system := translation.BuildSystemPrompt(project.SourceLang, project.TargetLang, project.Style, project.Glossary.Data)
	for _, batch := range batches {
		batch.Segments = filterPending(batch.Segments, pending)
		if len(batch.Segments) == 0 {
			continue
		}
		if stop.Err() != nil {
			return "", errStopping
		}
		if active, err := w.stillRunning(work, project.ID); err != nil || !active {
			return models.TaskCancelled, err
		}

		provider, err := w.resolve(work, &project)
		if err != nil {
			return "", err
		}
		svc := translation.NewService(provider, project.ModelID, translation.ServiceOptions{})
		outcome, terr := svc.TranslateBatch(work, batch, system, providers.Determinism(project.Determinism),
			project.SourceLang, project.TargetLang)
		// Результаты пишутся и при сбое: ответы модели уже оплачены
		// (Review Focus №5).
		if err := w.persist(work, project.ID, outcome); err != nil {
			return "", err
		}
		if w.AfterBatch != nil {
			w.AfterBatch(project.ID)
		}
		if terr != nil {
			return "", terr
		}
	}
	return models.TaskCompleted, w.finalize(work, project.ID)
}

// materialize создаёт строки сегментов. Идемпотентно: существующие
// сегменты (и их переводы) не трогаются.
func (w *Worker) materialize(ctx context.Context, projectID uuid.UUID, refs []translation.SegmentRef) error {
	rows := make([]models.TranslationSegment, 0, len(refs))
	for _, r := range refs {
		rows = append(rows, models.TranslationSegment{
			ProjectID: projectID, NodeID: r.NodeID, ChapterIndex: r.ChapterIndex, Ordinal: r.Ordinal,
			BlockType: string(r.Type), Level: r.Level, SourceText: r.Text, SourceHash: r.SourceHash,
			Status: models.SegmentPending, Severity: string(translation.OK),
			Findings: models.NewJSON([]translation.Finding{}),
		})
	}
	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&rows, 500).Error; err != nil {
				return fmt.Errorf("сегменты проекта: %w", err)
			}
		}
		return tx.Exec(`UPDATE translation_projects SET segments_total =
			(SELECT count(*) FROM translation_segments WHERE project_id = ?) WHERE id = ?`, projectID, projectID).Error
	})
}

func (w *Worker) markRunning(ctx context.Context, projectID uuid.UUID) (bool, error) {
	res := w.db.WithContext(ctx).Exec(`
UPDATE translation_projects SET status = 'running', started_at = COALESCE(started_at, now()), updated_at = now()
 WHERE id = ? AND status IN ('queued', 'running')`, projectID)
	return res.RowsAffected == 1, res.Error
}

// stillRunning — проверка паузы и отмены между батчами.
func (w *Worker) stillRunning(ctx context.Context, projectID uuid.UUID) (bool, error) {
	var status string
	err := w.db.WithContext(ctx).Model(&models.TranslationProject{}).Where("id = ?", projectID).Pluck("status", &status).Error
	return status == models.ProjectRunning, err
}

func (w *Worker) pendingNodes(ctx context.Context, projectID uuid.UUID) (map[string]bool, error) {
	var nodes []string
	err := w.db.WithContext(ctx).Model(&models.TranslationSegment{}).
		Where("project_id = ? AND status IN ?", projectID, []string{models.SegmentPending, models.SegmentRunning}).
		Pluck("node_id", &nodes).Error
	set := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		set[n] = true
	}
	return set, err
}

func filterPending(segs []translation.SegmentRef, pending map[string]bool) []translation.SegmentRef {
	out := segs[:0:0]
	for _, s := range segs {
		if pending[s.NodeID] {
			out = append(out, s)
		}
	}
	return out
}

// failOversized помечает слишком длинные блоки ошибкой с понятным текстом
// (Review Focus №3): книга продолжает переводиться.
func (w *Worker) failOversized(ctx context.Context, projectID uuid.UUID, oversized []*translation.SegmentTooLargeError, pending map[string]bool) error {
	outcome := translation.BatchOutcome{Results: map[string]translation.SegmentOutcome{}}
	for _, o := range oversized {
		if pending[o.NodeID] {
			outcome.Results[o.NodeID] = translation.SegmentOutcome{NodeID: o.NodeID, Severity: translation.Error,
				Err: o.Error() + ". Разделите абзац или увеличьте LINGUASTACK_MAX_BATCH_CHARS."}
		}
	}
	if len(outcome.Results) == 0 {
		return nil
	}
	return w.persist(ctx, projectID, outcome)
}

// persist записывает итоги батча. Запись идемпотентна: переведённый
// сегмент не перезаписывается (status <> 'completed'), поэтому повторный
// прогон и гонка двух worker-ов не портят результат. Счётчики проекта
// пересчитываются из сегментов, а не увеличиваются, — так они не могут
// разойтись с данными. Статус проекта здесь не меняется: запись после
// отмены не «воскрешает» проект (Review Focus №5).
func (w *Worker) persist(ctx context.Context, projectID uuid.UUID, outcome translation.BatchOutcome) error {
	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for nodeID, r := range outcome.Results {
			findings := r.Findings
			if findings == nil {
				findings = []translation.Finding{}
			}
			if r.Translated != nil {
				var ids []uuid.UUID
				err := tx.Raw(`
UPDATE translation_segments
   SET translated_text = ?, status = 'completed', severity = ?, findings = ?, attempts = attempts + 1,
       last_error = '', updated_at = now()
 WHERE project_id = ? AND node_id = ? AND status <> 'completed'
RETURNING id`, *r.Translated, string(r.Severity), models.NewJSON(findings), projectID, nodeID).Scan(&ids).Error
				if err != nil {
					return fmt.Errorf("запись перевода: %w", err)
				}
				for _, id := range ids {
					rev := models.TranslationSegmentRevision{SegmentID: id, TranslatedText: *r.Translated, Origin: models.OriginMachine}
					if err := tx.Create(&rev).Error; err != nil {
						return fmt.Errorf("версия перевода: %w", err)
					}
				}
				continue
			}
			err := tx.Exec(`
UPDATE translation_segments
   SET status = 'failed', severity = 'error', findings = ?, last_error = ?, attempts = attempts + 1, updated_at = now()
 WHERE project_id = ? AND node_id = ? AND status <> 'completed'`,
				models.NewJSON(findings), clip(r.Err), projectID, nodeID).Error
			if err != nil {
				return fmt.Errorf("запись ошибки сегмента: %w", err)
			}
		}
		return RefreshCounters(ctx, tx, projectID, outcome.CostUSD)
	})
}

// RefreshCounters пересчитывает счётчики проекта из сегментов, а не
// увеличивает их: повторный прогон и гонка двух worker-ов не могут их
// рассинхронизировать с данными.
func RefreshCounters(ctx context.Context, db *gorm.DB, projectID uuid.UUID, addCost float64) error {
	return db.WithContext(ctx).Exec(`
UPDATE translation_projects p
   SET segments_done = s.done, segments_failed = s.failed, warnings_count = s.warnings,
       cost_usd = p.cost_usd + ?, updated_at = now()
  FROM (SELECT count(*) FILTER (WHERE status = 'completed') AS done,
               count(*) FILTER (WHERE status = 'failed') AS failed,
               count(*) FILTER (WHERE severity = 'warning') AS warnings
          FROM translation_segments WHERE project_id = ?) s
 WHERE p.id = ?`, addCost, projectID, projectID).Error
}

// finalize ставит итоговый статус (дополнение §5), только если проект всё
// ещё выполняется: приостановленный или отменённый остаётся таким.
func (w *Worker) finalize(ctx context.Context, projectID uuid.UUID) error {
	return SettleStatus(ctx, w.db, projectID, models.ProjectRunning)
}

// SettleStatus ставит итоговый статус по счётчикам, если текущий статус —
// один из from. Ручная правка последнего упавшего сегмента так переводит
// проект из failed в completed.
func SettleStatus(ctx context.Context, db *gorm.DB, projectID uuid.UUID, from ...string) error {
	return db.WithContext(ctx).Exec(`
UPDATE translation_projects
   SET status = CASE WHEN segments_failed > 0 THEN 'failed'
                     WHEN warnings_count > 0 THEN 'completed_with_warnings'
                     ELSE 'completed' END,
       last_error = CASE WHEN segments_failed > 0
                         THEN segments_failed || ' сегм. не переведено — см. ошибки сегментов' ELSE '' END,
       finished_at = now(), updated_at = now()
 WHERE id = ? AND status IN ?`, projectID, from).Error
}
