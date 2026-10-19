package exports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exporters"
	"github.com/vnovels/linguastack/internal/storage"
)

// Runner выполняет задачи экспорта в worker (дополнение §16).
//
// Захват — FOR UPDATE SKIP LOCKED с арендой на время сборки: несколько
// worker-ов не возьмут одну задачу, а задачу упавшего worker-а подберёт
// другой, когда аренда истечёт. Сборка идемпотентна: файл пишется во
// временный и переименовывается, повторный запуск его просто перезапишет.
type Runner struct {
	db      *gorm.DB
	store   *storage.FileStore
	timeout time.Duration

	// PollInterval — пауза, когда задач нет.
	PollInterval time.Duration
	// BeforeFinish вызывается перед записью результата (для тестов).
	BeforeFinish func()
}

// NewRunner создаёт исполнителя задач экспорта.
func NewRunner(db *gorm.DB, store *storage.FileStore, cfg config.Settings) *Runner {
	timeout := cfg.ExportTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Runner{db: db, store: store, timeout: timeout, PollInterval: time.Second}
}

// Run выполняет задачи до отмены ctx.
func (r *Runner) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		worked, err := r.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("экспорт: сбой цикла", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(r.PollInterval):
		}
	}
	return nil
}

const claimSQL = `
UPDATE export_jobs
   SET status = 'running', locked_until = now() + make_interval(secs => ?), updated_at = now()
 WHERE id = (
       SELECT id FROM export_jobs
        WHERE status = 'pending' OR (status = 'running' AND locked_until < now())
        ORDER BY created_at
        FOR UPDATE SKIP LOCKED
        LIMIT 1)
RETURNING *`

// RunOnce выполняет одну задачу. false — задач нет.
func (r *Runner) RunOnce(ctx context.Context) (bool, error) {
	var job models.ExportJob
	if err := r.db.WithContext(ctx).Raw(claimSQL, r.timeout.Seconds()).Scan(&job).Error; err != nil {
		return false, fmt.Errorf("захват задачи экспорта: %w", err)
	}
	if job.ID == uuid.Nil {
		return false, nil
	}
	return true, r.process(ctx, &job)
}

// errUserFacing — что видит пользователь при сбое сборки: подробности
// (пути, ошибки диска) — только в журнале.
const errUserFacing = "Не удалось собрать файл экспорта."

func (r *Runner) process(ctx context.Context, job *models.ExportJob) error {
	work, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var book models.Book
	err := r.db.WithContext(work).First(&book, "id = ? AND owner_id = ?", job.BookID, job.OwnerID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil // книгу удалили — задача ушла вместе с ней (каскад)
	}
	if err != nil {
		return fmt.Errorf("книга для экспорта: %w", err)
	}
	var project models.TranslationProject
	if err := r.db.WithContext(work).First(&project, "id = ? AND owner_id = ?", job.ProjectID, job.OwnerID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("перевод для экспорта: %w", err)
	}
	var segs []models.TranslationSegment
	if err := r.db.WithContext(work).Select("node_id", "translated_text").
		Where("project_id = ?", project.ID).Find(&segs).Error; err != nil {
		return fmt.Errorf("сегменты для экспорта: %w", err)
	}

	exporter, ok := exporters.Get(job.Format)
	if !ok {
		return r.fail(ctx, job, fmt.Errorf("неизвестный формат %q", job.Format))
	}
	doc, untranslated := Assemble(book.CDM.Data, book.SourceFormat, segs, exporters.Mode(job.Mode),
		project.SourceLang, project.TargetLang)
	doc.ID = job.ID.String()
	doc.Assets = func(name string) ([]byte, error) {
		f, err := r.store.OpenAsset(book.OwnerID, book.ID, name)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}
	out := exporter.Describe(doc)
	size, err := r.store.SaveExport(book.OwnerID, book.ID, job.ID, out.Ext, func(w io.Writer) error {
		return exporter.Write(ctxWriter{ctx: work, w: w}, doc)
	})
	if err != nil {
		return r.fail(ctx, job, err)
	}

	if r.BeforeFinish != nil {
		r.BeforeFinish()
	}
	res := r.db.WithContext(ctx).Model(&models.ExportJob{}).
		Where("id = ? AND status = ?", job.ID, models.ExportRunning).
		Updates(map[string]any{
			"status": models.ExportCompleted, "file_ext": out.Ext, "content_type": out.ContentType,
			"size_bytes": size, "untranslated": untranslated, "error": "",
			"locked_until": nil, "finished_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("завершение экспорта: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// Задачу удалили, пока файл собирался (вместе с книгой или
		// пользователем): файл никому не нужен.
		return r.store.DeleteExport(book.OwnerID, book.ID, job.ID, out.Ext)
	}
	slog.Info("экспорт готов", "export", job.ID, "format", job.Format, "size", size, "untranslated", untranslated)
	return nil
}

func (r *Runner) fail(ctx context.Context, job *models.ExportJob, cause error) error {
	slog.Error("экспорт не удался", "export", job.ID, "format", job.Format, "error", cause)
	err := r.db.WithContext(ctx).Model(&models.ExportJob{}).
		Where("id = ? AND status = ?", job.ID, models.ExportRunning).
		Updates(map[string]any{
			"status": models.ExportFailed, "error": errUserFacing, "locked_until": nil,
			"finished_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
		}).Error
	if err != nil {
		return fmt.Errorf("отметка сбоя экспорта: %w", err)
	}
	return nil
}

// ctxWriter прерывает сборку по тайм-ауту: экспортёры пишут часто, и
// следующая запись после отмены вернёт ошибку.
type ctxWriter struct {
	ctx context.Context
	w   io.Writer
}

func (c ctxWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.w.Write(p)
}
