package queue_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/testdb"
)

const lease = time.Minute

// Тесты пакета коммитят по-настоящему (testdb.Committing): SKIP LOCKED и
// аренды проверяются только на разных соединениях. t.Parallel() здесь нельзя.

func enqueueFor(t *testing.T, gdb *gorm.DB, owner *uuid.UUID, priority int) *models.TranslationTask {
	t.Helper()
	project := testdb.MakeProject(t, gdb, &testdb.ProjectOptions{Owner: owner})
	task, err := queue.Enqueue(t.Context(), gdb, project, priority, 3)
	require.NoError(t, err)
	return task
}

func claim(t *testing.T, gdb *gorm.DB, worker string, maxPerUser int) *models.TranslationTask {
	t.Helper()
	task, err := queue.ClaimNext(t.Context(), gdb, worker, lease, maxPerUser)
	require.NoError(t, err)
	return task
}

func reload(t *testing.T, gdb *gorm.DB, id uuid.UUID) models.TranslationTask {
	t.Helper()
	var task models.TranslationTask
	require.NoError(t, gdb.First(&task, "id = ?", id).Error)
	return task
}

func expire(t *testing.T, gdb *gorm.DB, id uuid.UUID) {
	t.Helper()
	require.NoError(t, gdb.Model(&models.TranslationTask{}).Where("id = ?", id).
		Update("locked_until", time.Now().Add(-time.Second)).Error)
}

func TestClaimMarksRunning(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	queued := enqueueFor(t, gdb, nil, 0)

	task := claim(t, gdb, "worker-a", 3)
	require.NotNil(t, task)
	require.Equal(t, queued.ID, task.ID)
	require.Equal(t, models.TaskRunning, task.Status)
	require.Equal(t, "worker-a", *task.LockedBy)
	require.True(t, task.LockedUntil.After(time.Now()))
	require.Equal(t, 1, task.Attempts)
	require.NotNil(t, task.StartedAt)
}

func TestClaimReturnsNilWhenQueueEmpty(t *testing.T) {
	require.Nil(t, claim(t, testdb.Committing(t, "queue"), "w", 3))
}

func TestClaimedTaskNotGivenToSecondWorker(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	require.NotNil(t, claim(t, gdb, "worker-a", 3))
	require.Nil(t, claim(t, gdb, "worker-b", 3))
}

func TestConcurrentClaimsNeverDuplicate(t *testing.T) {
	// FOR UPDATE SKIP LOCKED: двадцать worker-ов на десять задач — каждая
	// задача выдана ровно одному.
	gdb := testdb.Committing(t, "queue")
	for range 10 {
		enqueueFor(t, gdb, nil, 0)
	}
	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := queue.ClaimNext(t.Context(), gdb, "w"+string(rune('a'+i)), lease, 100)
			if err == nil && task != nil {
				mu.Lock()
				seen[task.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Len(t, seen, 10)
	for id, n := range seen {
		require.Equal(t, 1, n, "задача %s выдана %d раз", id, n)
	}
}

func TestExpiredLeaseReclaimable(t *testing.T) {
	// ТЗ §7: восстановление после перезапуска. Упавший worker оставил задачу
	// в running — по истечении аренды её берёт другой.
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	dead := claim(t, gdb, "dead-worker", 3)
	require.Nil(t, claim(t, gdb, "live-worker", 3), "аренда ещё действует")

	expire(t, gdb, dead.ID)
	reclaimed := claim(t, gdb, "live-worker", 3)
	require.NotNil(t, reclaimed)
	require.Equal(t, dead.ID, reclaimed.ID)
	require.Equal(t, "live-worker", *reclaimed.LockedBy)
	require.Equal(t, 2, reclaimed.Attempts, "падение worker-а — тоже попытка")
}

func TestPerUserLimit(t *testing.T) {
	// ТЗ §7: один пользователь не занимает все ресурсы сервера.
	gdb := testdb.Committing(t, "queue")
	owner := testdb.MakeUser(t, gdb).ID
	for range 5 {
		enqueueFor(t, gdb, &owner, 0)
	}
	claimed := 0
	for i := range 5 {
		if claim(t, gdb, "w"+string(rune('a'+i)), 2) != nil {
			claimed++
		}
	}
	require.Equal(t, 2, claimed)
}

func TestExpiredRunningDoesNotCountTowardUserLimit(t *testing.T) {
	// Задача упавшего worker-а не должна навсегда съедать лимит пользователя.
	gdb := testdb.Committing(t, "queue")
	owner := testdb.MakeUser(t, gdb).ID
	enqueueFor(t, gdb, &owner, 0)
	enqueueFor(t, gdb, &owner, 0)
	stuck := claim(t, gdb, "dead", 1)
	expire(t, gdb, stuck.ID)
	require.NotNil(t, claim(t, gdb, "live", 1))
}

func TestBusyUserDoesNotBlockOthers(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	hog := testdb.MakeUser(t, gdb).ID
	for range 3 {
		enqueueFor(t, gdb, &hog, 10) // даже с высоким приоритетом
	}
	polite := testdb.MakeUser(t, gdb).ID
	enqueueFor(t, gdb, &polite, 0)

	var owners []uuid.UUID
	for i := range 4 {
		if task := claim(t, gdb, "w"+string(rune('a'+i)), 2); task != nil {
			owners = append(owners, task.OwnerID)
		}
	}
	require.Contains(t, owners, polite)
	require.Len(t, owners, 3, "двое от занятого пользователя и один от вежливого")
}

func TestPriorityThenFIFO(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	first := enqueueFor(t, gdb, nil, 0)
	second := enqueueFor(t, gdb, nil, 0)
	urgent := enqueueFor(t, gdb, nil, 10)
	require.Equal(t, urgent.ID, claim(t, gdb, "w1", 9).ID)
	require.Equal(t, first.ID, claim(t, gdb, "w2", 9).ID)
	require.Equal(t, second.ID, claim(t, gdb, "w3", 9).ID)
}

func TestHeartbeatOnlyForHolder(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	task := claim(t, gdb, "holder", 3)
	before := *task.LockedUntil

	ok, err := queue.Heartbeat(t.Context(), gdb, task.ID, "holder", 10*time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, reload(t, gdb, task.ID).LockedUntil.After(before))

	ok, err = queue.Heartbeat(t.Context(), gdb, task.ID, "intruder", 10*time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "чужая аренда не продлевается")
}

func TestCompleteAndNeverReclaimed(t *testing.T) {
	// ТЗ §7: завершённая задача не выполняется повторно.
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	task := claim(t, gdb, "w", 3)
	require.NoError(t, queue.Finish(t.Context(), gdb, task.ID, "w", models.TaskCompleted))
	done := reload(t, gdb, task.ID)
	require.Equal(t, models.TaskCompleted, done.Status)
	require.NotNil(t, done.FinishedAt)
	require.Nil(t, done.LockedBy)

	expire(t, gdb, task.ID)
	require.Nil(t, claim(t, gdb, "w2", 3))
}

func TestFinishByNonHolderIsLeaseLost(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	task := claim(t, gdb, "holder", 3)
	err := queue.Finish(t.Context(), gdb, task.ID, "intruder", models.TaskCompleted)
	require.True(t, errors.Is(err, queue.ErrLeaseLost))
	require.Equal(t, models.TaskRunning, reload(t, gdb, task.ID).Status)
}

func TestFailWithRetryDelaysAvailability(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	task := claim(t, gdb, "w", 3)
	terminal, err := queue.Fail(t.Context(), gdb, task.ID, "w", "провайдер недоступен", time.Hour)
	require.NoError(t, err)
	require.False(t, terminal)
	failed := reload(t, gdb, task.ID)
	require.Equal(t, models.TaskPending, failed.Status)
	require.Equal(t, "провайдер недоступен", failed.LastError)
	require.Nil(t, claim(t, gdb, "w", 3), "пауза перед повтором соблюдается")

	require.NoError(t, gdb.Model(&models.TranslationTask{}).Where("id = ?", task.ID).
		Update("available_at", time.Now().Add(-time.Second)).Error)
	require.NotNil(t, claim(t, gdb, "w", 3))
}

func TestFailBeyondMaxAttemptsIsTerminal(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	var terminal bool
	for range 3 {
		task := claim(t, gdb, "w", 3)
		require.NotNil(t, task)
		var err error
		terminal, err = queue.Fail(t.Context(), gdb, task.ID, "w", "boom", 0)
		require.NoError(t, err)
	}
	require.True(t, terminal)
	require.Nil(t, claim(t, gdb, "w", 3))
}

func TestReleaseReturnsTaskWithoutSpendingAttempt(t *testing.T) {
	// Остановка сервиса — не сбой: попытка не расходуется.
	gdb := testdb.Committing(t, "queue")
	enqueueFor(t, gdb, nil, 0)
	task := claim(t, gdb, "w", 3)
	require.NoError(t, queue.Release(t.Context(), gdb, task.ID, "w"))
	released := reload(t, gdb, task.ID)
	require.Equal(t, models.TaskPending, released.Status)
	require.Equal(t, 0, released.Attempts)
	require.NotNil(t, claim(t, gdb, "w2", 3))
}

func TestEnqueueIsIdempotent(t *testing.T) {
	// Двойное «Перевести» возвращает ту же активную задачу.
	gdb := testdb.Committing(t, "queue")
	project := testdb.MakeProject(t, gdb, nil)
	a, err := queue.Enqueue(t.Context(), gdb, project, 0, 3)
	require.NoError(t, err)
	b, err := queue.Enqueue(t.Context(), gdb, project, 0, 3)
	require.NoError(t, err)
	require.Equal(t, a.ID, b.ID)
}

func TestCancelPendingForProject(t *testing.T) {
	gdb := testdb.Committing(t, "queue")
	project := testdb.MakeProject(t, gdb, nil)
	task, err := queue.Enqueue(t.Context(), gdb, project, 0, 3)
	require.NoError(t, err)
	require.NoError(t, queue.CancelPending(t.Context(), gdb, project.ID))
	require.Equal(t, models.TaskCancelled, reload(t, gdb, task.ID).Status)
	require.Nil(t, claim(t, gdb, "w", 3))
}
