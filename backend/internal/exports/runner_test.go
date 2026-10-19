package exports_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exports"
	"github.com/vnovels/linguastack/internal/storage"
	"github.com/vnovels/linguastack/internal/testdb"
)

type fixture struct {
	db      *gorm.DB
	store   *storage.FileStore
	root    string
	project *models.TranslationProject
	runner  *exports.Runner
}

func newFixture(t *testing.T, tx *gorm.DB) *fixture {
	t.Helper()
	b := cdm.NewBuilder(cdm.BookMeta{Title: "Book", SourceFormat: "markdown"})
	b.StartChapter("Chapter", 1)
	b.Add(cdm.Heading, "Chapter", 1, nil)
	b.Add(cdm.Paragraph, "One.", 0, nil)
	b.Add(cdm.Paragraph, "Two.", 0, nil)
	doc := b.Build()
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Document: &doc, Status: models.ProjectCompleted})
	for i, blk := range doc.Blocks()[:2] {
		text := []string{"Глава", "Один."}[i]
		require.NoError(t, tx.Create(&models.TranslationSegment{ProjectID: p.ID, NodeID: blk.NodeID, BlockType: string(blk.Type),
			SourceText: blk.Text, SourceHash: blk.SourceHash, TranslatedText: &text, Status: models.SegmentCompleted, Severity: "ok"}).Error)
	}
	root := t.TempDir()
	store, err := storage.New(root, 1<<20)
	require.NoError(t, err)
	cfg, err := config.Load()
	require.NoError(t, err)
	return &fixture{db: tx, store: store, root: root, project: p, runner: exports.NewRunner(tx, store, cfg)}
}

func (f *fixture) job(t *testing.T, format string, mutate func(*models.ExportJob)) *models.ExportJob {
	t.Helper()
	job := &models.ExportJob{OwnerID: f.project.OwnerID, BookID: f.project.BookID, ProjectID: f.project.ID,
		Format: format, Mode: "translation", Status: models.ExportPending, BaseName: "Book (ru)"}
	if mutate != nil {
		mutate(job)
	}
	require.NoError(t, f.db.Create(job).Error)
	return job
}

func (f *fixture) reload(t *testing.T, job *models.ExportJob) *models.ExportJob {
	t.Helper()
	var got models.ExportJob
	require.NoError(t, f.db.First(&got, "id = ?", job.ID).Error)
	return &got
}

func TestRunnerCompletesExport(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "markdown", nil)
	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)

	got := f.reload(t, job)
	require.Equal(t, models.ExportCompleted, got.Status)
	require.Equal(t, ".md", got.FileExt)
	require.Equal(t, "text/markdown; charset=utf-8", got.ContentType)
	require.Equal(t, 1, got.Untranslated, "«Two.» без перевода")
	require.Positive(t, got.SizeBytes)
	require.NotNil(t, got.FinishedAt)
	require.Nil(t, got.LockedUntil)

	file, err := f.store.OpenExport(job.OwnerID, job.BookID, job.ID, ".md")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	raw, err := os.ReadFile(file.Name())
	require.NoError(t, err)
	require.Contains(t, string(raw), "# Глава\n\nОдин.\n\nTwo.")
	require.Equal(t, got.SizeBytes, int64(len(raw)))

	worked, err = f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.False(t, worked, "очередь пуста")
}

func TestRunnerFailedJob(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "epub", nil)
	// Каталог экспортов занят файлом — запись не удастся.
	dir := f.store.BookDir(job.OwnerID, job.BookID)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exports"), nil, 0o600))

	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)
	got := f.reload(t, job)
	require.Equal(t, models.ExportFailed, got.Status)
	require.Equal(t, "Не удалось собрать файл экспорта.", got.Error, "без внутренних путей и деталей")
	require.NotNil(t, got.FinishedAt)
}

func TestRunnerReclaimsExpiredLease(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Minute)
	busy := f.job(t, "txt", func(j *models.ExportJob) { j.Status, j.LockedUntil = models.ExportRunning, &future })
	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.False(t, worked, "аренда другого worker-а ещё действует")

	require.NoError(t, f.db.Model(busy).Update("locked_until", past).Error)
	worked, err = f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked, "упавший worker задачу не держит")
	require.Equal(t, models.ExportCompleted, f.reload(t, busy).Status)
}

func TestRunnerSkipLocked(t *testing.T) {
	f := newFixture(t, testdb.Committing(t, "exports"))
	f.job(t, "html", nil)
	var wg sync.WaitGroup
	results := make([]bool, 6)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worked, err := f.runner.RunOnce(context.Background())
			require.NoError(t, err)
			results[i] = worked
		}()
	}
	wg.Wait()
	n := 0
	for _, r := range results {
		if r {
			n++
		}
	}
	require.Equal(t, 1, n, "одну задачу выполняет ровно один исполнитель")
}

func TestRunnerBookDeletedDuringExport(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "txt", nil)
	// Книгу удалили, пока файл собирался: строки задачи уже нет (каскад), и
	// собранный файл не должен остаться сиротой.
	f.runner.BeforeFinish = func() {
		require.NoError(t, f.db.Delete(&models.Book{}, "id = ?", job.BookID).Error)
	}
	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)
	_, err = os.Stat(filepath.Join(f.store.BookDir(job.OwnerID, job.BookID), "exports", job.ID.String()+".txt"))
	require.True(t, os.IsNotExist(err), "файл убран")
}

// reclaim имитирует другой worker, подхвативший задачу после истечения
// аренды: у задачи новая аренда.
func reclaim(t *testing.T, f *fixture, job *models.ExportJob) {
	t.Helper()
	later := time.Now().Add(time.Hour)
	require.NoError(t, f.db.Model(&models.ExportJob{}).Where("id = ?", job.ID).Update("locked_until", later).Error)
}

func TestRunnerStaleCompletionLeavesNewOwner(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "txt", nil)
	f.runner.BeforeFinish = func() { reclaim(t, f, job) }
	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)
	got := f.reload(t, job)
	require.Equal(t, models.ExportRunning, got.Status, "результат пишет только владелец аренды")
	_, err = os.Stat(filepath.Join(f.store.BookDir(job.OwnerID, job.BookID), "exports", job.ID.String()+".txt"))
	require.NoError(t, err, "файл нового владельца не удалён")
}

func TestRunnerStaleFailureLeavesNewOwner(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "txt", nil)
	dir := f.store.BookDir(job.OwnerID, job.BookID)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "exports"), nil, 0o600))
	f.runner.BeforeFinish = func() { reclaim(t, f, job) }
	worked, err := f.runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.True(t, worked)
	got := f.reload(t, job)
	require.Equal(t, models.ExportRunning, got.Status, "просроченный исполнитель не отмечает чужую задачу как ошибку")
	require.Empty(t, got.Error)
}

func TestRunnerFinishesJobOnShutdown(t *testing.T) {
	f := newFixture(t, testdb.New(t))
	job := f.job(t, "epub", nil)
	ctx, stop := context.WithCancel(context.Background())
	f.runner.BeforeFinish = stop // сигнал остановки посреди сборки
	worked, err := f.runner.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, worked)
	require.Equal(t, models.ExportCompleted, f.reload(t, job).Status, "начатый экспорт дописывается")
}
