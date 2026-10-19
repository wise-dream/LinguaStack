package queue_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/store"
	"github.com/vnovels/linguastack/internal/testdb"
)

func TestPauseResumeCancel(t *testing.T) {
	tx := testdb.New(t)
	ctx := context.Background()
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Status: models.ProjectQueued})
	_, err := queue.Enqueue(ctx, tx, p, 0, 3)
	require.NoError(t, err)

	paused, err := queue.Pause(ctx, tx, p.ID, p.OwnerID)
	require.NoError(t, err)
	require.Equal(t, models.ProjectPaused, paused.Status)
	var pending int64
	require.NoError(t, tx.Model(&models.TranslationTask{}).Where("project_id = ? AND status = ?", p.ID, models.TaskPending).Count(&pending).Error)
	require.Zero(t, pending, "ожидающая задача снята")

	resumed, err := queue.Resume(ctx, tx, p.ID, p.OwnerID, 3)
	require.NoError(t, err)
	require.Equal(t, models.ProjectQueued, resumed.Status)
	require.NoError(t, tx.Model(&models.TranslationTask{}).Where("project_id = ? AND status = ?", p.ID, models.TaskPending).Count(&pending).Error)
	require.Equal(t, int64(1), pending)

	cancelled, err := queue.Cancel(ctx, tx, p.ID, p.OwnerID)
	require.NoError(t, err)
	require.Equal(t, models.ProjectCancelled, cancelled.Status)
	require.NotNil(t, cancelled.FinishedAt)
}

func TestControlForeignProjectNotFound(t *testing.T) {
	tx := testdb.New(t)
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Status: models.ProjectRunning})
	stranger := testdb.MakeUser(t, tx)
	for _, act := range []func() (*models.TranslationProject, error){
		func() (*models.TranslationProject, error) {
			return queue.Pause(context.Background(), tx, p.ID, stranger.ID)
		},
		func() (*models.TranslationProject, error) {
			return queue.Resume(context.Background(), tx, p.ID, stranger.ID, 3)
		},
		func() (*models.TranslationProject, error) {
			return queue.Cancel(context.Background(), tx, uuid.New(), p.OwnerID)
		},
	} {
		_, err := act()
		require.ErrorIs(t, err, store.ErrNotFound)
	}
	var got models.TranslationProject
	require.NoError(t, tx.First(&got, "id = ?", p.ID).Error)
	require.Equal(t, models.ProjectRunning, got.Status)
}

func TestBadTransition(t *testing.T) {
	tx := testdb.New(t)
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Status: models.ProjectCompleted})
	_, err := queue.Pause(context.Background(), tx, p.ID, p.OwnerID)
	var te *queue.TransitionError
	require.True(t, errors.As(err, &te), "err = %v", err)
	require.Equal(t, models.ProjectCompleted, te.From)
	require.ErrorIs(t, err, queue.ErrBadTransition)
}
