package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/notify"
	"github.com/vnovels/linguastack/internal/testdb"
)

// bookProject — книга из двух глав: в первой два абзаца, во второй один;
// сегменты созданы со статусами statuses (по порядку блоков).
func bookProject(t *testing.T, tx *gorm.DB, statuses ...string) (*models.TranslationProject, []string) {
	t.Helper()
	b := cdm.NewBuilder(cdm.BookMeta{Title: "Book", SourceFormat: "markdown"})
	b.StartChapter("One", 1)
	b.Add(cdm.Paragraph, "a", 0, nil)
	b.Add(cdm.Paragraph, "b", 0, nil)
	b.StartChapter("Two", 1)
	b.Add(cdm.Paragraph, "c", 0, nil)
	doc := b.Build()
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Document: &doc, Status: models.ProjectRunning})
	var nodes []string
	for i, blk := range doc.Blocks() {
		require.NoError(t, tx.Create(&models.TranslationSegment{ProjectID: p.ID, NodeID: blk.NodeID,
			ChapterIndex: chapterOf(doc, blk.NodeID), Ordinal: i, BlockType: string(blk.Type), SourceText: blk.Text,
			SourceHash: blk.SourceHash, Status: statuses[i], Severity: "ok"}).Error)
		nodes = append(nodes, blk.NodeID)
	}
	return p, nodes
}

func chapterOf(doc cdm.Document, nodeID string) int {
	for _, ch := range doc.Chapters {
		for _, b := range ch.Blocks {
			if b.NodeID == nodeID {
				return ch.Index
			}
		}
	}
	return -1
}

func bind(t *testing.T, tx *gorm.DB, p *models.TranslationProject, mutate func(*models.TelegramBinding)) {
	t.Helper()
	b := models.TelegramBinding{UserID: p.OwnerID, TelegramUserID: 5005, ChatID: 5005,
		NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}
	if mutate != nil {
		mutate(&b)
	}
	require.NoError(t, tx.Create(&b).Error)
}

func notifications(t *testing.T, tx *gorm.DB) []models.TelegramNotification {
	t.Helper()
	var out []models.TelegramNotification
	require.NoError(t, tx.Order("created_at, chapter_index").Find(&out).Error)
	return out
}

func TestChapterDoneOnceWhenAllSegmentsProcessed(t *testing.T) {
	tx := testdb.New(t)
	ctx := context.Background()
	p, nodes := bookProject(t, tx, models.SegmentCompleted, models.SegmentPending, models.SegmentCompleted)
	bind(t, tx, p, nil)

	require.NoError(t, notify.ChaptersDone(ctx, tx, p.ID, nodes))
	got := notifications(t, tx)
	require.Len(t, got, 1, "в первой главе ещё есть pending")
	require.Equal(t, models.NotifyChapter, got[0].Kind)
	require.Equal(t, 1, *got[0].ChapterIndex)
	require.Equal(t, p.OwnerID, got[0].UserID)
	require.Equal(t, models.NotificationPending, got[0].Status)

	require.NoError(t, tx.Model(&models.TranslationSegment{}).Where("node_id = ? AND project_id = ?", nodes[1], p.ID).
		Update("status", models.SegmentFailed).Error)
	require.NoError(t, notify.ChaptersDone(ctx, tx, p.ID, nodes[:2]))
	require.NoError(t, notify.ChaptersDone(ctx, tx, p.ID, nodes))
	got = notifications(t, tx)
	require.Len(t, got, 2, "глава с упавшим сегментом обработана; повтор события не дублирует")
	require.NoError(t, notify.ChaptersDone(ctx, tx, p.ID, nil))
}

func TestNoBindingNoNotification(t *testing.T) {
	tx := testdb.New(t)
	p, nodes := bookProject(t, tx, models.SegmentCompleted, models.SegmentCompleted, models.SegmentCompleted)
	require.NoError(t, notify.ChaptersDone(context.Background(), tx, p.ID, nodes))
	require.NoError(t, tx.Model(p).Update("status", models.ProjectCompleted).Error)
	require.NoError(t, notify.Completed(context.Background(), tx, p.ID))
	require.Empty(t, notifications(t, tx))
}

func TestSettingDisabledNoNotification(t *testing.T) {
	tx := testdb.New(t)
	ctx := context.Background()
	p, nodes := bookProject(t, tx, models.SegmentCompleted, models.SegmentCompleted, models.SegmentCompleted)
	bind(t, tx, p, func(b *models.TelegramBinding) {
		b.NotifyChapters, b.NotifyCompleted, b.NotifyErrors = false, false, false
	})
	require.NoError(t, notify.ChaptersDone(ctx, tx, p.ID, nodes))
	require.NoError(t, tx.Model(p).Update("status", models.ProjectCompleted).Error)
	require.NoError(t, notify.Completed(ctx, tx, p.ID))
	require.NoError(t, tx.Model(p).Updates(map[string]any{"status": models.ProjectFailed, "finished_at": time.Now()}).Error)
	require.NoError(t, notify.Failed(ctx, tx, p.ID))
	require.Empty(t, notifications(t, tx))
}

func TestCompletedAndFailed(t *testing.T) {
	tx := testdb.New(t)
	ctx := context.Background()
	p, _ := bookProject(t, tx, models.SegmentCompleted, models.SegmentCompleted, models.SegmentCompleted)
	bind(t, tx, p, nil)

	require.NoError(t, notify.Completed(ctx, tx, p.ID))
	require.Empty(t, notifications(t, tx), "проект ещё не завершён")

	failedAt := time.Now().Add(-time.Hour)
	require.NoError(t, tx.Model(p).Updates(map[string]any{"status": models.ProjectFailed, "finished_at": failedAt}).Error)
	require.NoError(t, notify.Failed(ctx, tx, p.ID))
	require.NoError(t, notify.Failed(ctx, tx, p.ID))
	require.Len(t, notifications(t, tx), 1)
	require.NoError(t, tx.Model(p).Update("finished_at", time.Now()).Error)
	require.NoError(t, notify.Failed(ctx, tx, p.ID))
	require.Len(t, notifications(t, tx), 2, "новый сбой — новое уведомление")

	require.NoError(t, tx.Model(p).Update("status", models.ProjectCompletedWithWarnings).Error)
	require.NoError(t, notify.Completed(ctx, tx, p.ID))
	require.NoError(t, notify.Completed(ctx, tx, p.ID))
	got := notifications(t, tx)
	require.Len(t, got, 3)
	require.Equal(t, models.NotifyCompleted, got[2].Kind)
}
