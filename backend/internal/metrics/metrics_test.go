package metrics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/metrics"
	"github.com/vnovels/linguastack/internal/testdb"
)

func TestSnapshotCounts(t *testing.T) {
	tx := testdb.New(t)
	ctx := t.Context()
	// Общая тестовая база может содержать чужие строки — сравнивается прирост.
	before, err := metrics.TakeSnapshot(ctx, tx)
	require.NoError(t, err)

	p1 := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Status: models.ProjectRunning})
	testdb.MakeProject(t, tx, &testdb.ProjectOptions{Status: models.ProjectCompleted})
	require.NoError(t, tx.Model(p1).Updates(map[string]any{"cost_usd": 0.25, "input_tokens": 100, "output_tokens": 40}).Error)
	require.NoError(t, tx.Create(&models.TranslationTask{ProjectID: p1.ID, OwnerID: p1.OwnerID, Status: models.TaskPending, MaxAttempts: 3}).Error)
	require.NoError(t, tx.Create(&models.ExportJob{OwnerID: p1.OwnerID, BookID: p1.BookID, ProjectID: p1.ID,
		Format: "epub", Mode: "translation", Status: models.ExportFailed, BaseName: "b", FileExt: "epub"}).Error)
	require.NoError(t, tx.Create(&models.TelegramNotification{UserID: p1.OwnerID, ProjectID: p1.ID, Kind: models.NotifyCompleted,
		DedupeKey: "k", Status: models.NotificationSent, AvailableAt: time.Now()}).Error)

	after, err := metrics.TakeSnapshot(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, int64(2), after.Users-before.Users)
	require.Equal(t, int64(2), after.Books-before.Books)
	require.Equal(t, int64(1), after.Projects[models.ProjectRunning]-before.Projects[models.ProjectRunning])
	require.Equal(t, int64(1), after.Projects[models.ProjectCompleted]-before.Projects[models.ProjectCompleted])
	require.Equal(t, int64(1), after.Tasks[models.TaskPending]-before.Tasks[models.TaskPending])
	require.Equal(t, int64(1), after.Exports[models.ExportFailed]-before.Exports[models.ExportFailed])
	require.Equal(t, int64(1), after.Notifications[models.NotificationSent]-before.Notifications[models.NotificationSent])
	require.InDelta(t, 0.25, after.CostUSD-before.CostUSD, 1e-9)
	require.Equal(t, int64(100), after.InputTokens-before.InputTokens)
	require.Equal(t, int64(40), after.OutputTokens-before.OutputTokens)
	// Известные статусы присутствуют и с нулём: ряды метрик не пропадают.
	_, ok := after.Projects[models.ProjectCancelled]
	require.True(t, ok)
}

// sqlRecorder запоминает выполненные запросы.
type sqlRecorder struct {
	logger.Interface
	mu  sync.Mutex
	sql []string
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	s, _ := fc()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sql = append(r.sql, s)
}

func TestSnapshotSkipsSegments(t *testing.T) {
	// Сегментов — миллионы; опрос метрик раз в 15 секунд не должен их
	// перебирать (Review Focus №5).
	rec := &sqlRecorder{Interface: logger.Discard}
	tx := testdb.New(t).Session(&gorm.Session{Logger: rec})
	_, err := metrics.TakeSnapshot(t.Context(), tx)
	require.NoError(t, err)
	require.NotEmpty(t, rec.sql)
	for _, q := range rec.sql {
		require.NotContains(t, q, "translation_segments")
	}
}

func get(t *testing.T, h http.Handler, auth string) (int, string) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func TestHandlerRequiresToken(t *testing.T) {
	m := metrics.NewAPI(testdb.New(t))
	m.HTTP.Observe(http.MethodGet, "/api/books/{id}", http.StatusOK, 30*time.Millisecond)

	code, _ := get(t, m.Handler(""), "Bearer ")
	require.Equal(t, http.StatusNotFound, code, "токен не задан — метрик нет")

	h := m.Handler("s3cret-metrics-token")
	code, _ = get(t, h, "")
	require.Equal(t, http.StatusUnauthorized, code)
	code, body := get(t, h, "Bearer wrong")
	require.Equal(t, http.StatusUnauthorized, code)
	require.NotContains(t, body, "s3cret")

	code, body = get(t, h, "Bearer s3cret-metrics-token")
	require.Equal(t, http.StatusOK, code)
	for _, name := range []string{
		`linguastack_http_requests_total{method="GET",route="/api/books/{id}",status="200"} 1`,
		"linguastack_http_request_duration_seconds_bucket",
		"linguastack_users ", "linguastack_books ",
		`linguastack_projects{status="running"}`, `linguastack_tasks{status="pending"}`,
		`linguastack_exports{status="failed"}`, `linguastack_notifications{status="sent"}`,
		"linguastack_provider_cost_usd_total", `linguastack_provider_tokens_total{direction="input"}`,
		"go_goroutines", "process_",
	} {
		require.Contains(t, body, name)
	}
}

func TestWorkerMetrics(t *testing.T) {
	reg := metrics.NewRegistry()
	w := metrics.NewWorker(reg)
	w.Batch(metrics.BatchResult{Translated: 18, Failed: 2, Calls: 1, Duration: 3 * time.Second})
	w.Batch(metrics.BatchResult{Failed: 5, Calls: 2, Err: true, Duration: time.Second})
	var nilWorker *metrics.Worker
	nilWorker.Batch(metrics.BatchResult{Translated: 1}) // без метрик — ничего не делает

	code, body := get(t, metrics.Handler(reg, "tok"), "Bearer tok")
	require.Equal(t, http.StatusOK, code)
	for _, line := range []string{
		`linguastack_worker_batches_total{result="ok"} 1`,
		`linguastack_worker_batches_total{result="error"} 1`,
		`linguastack_worker_segments_total{result="translated"} 18`,
		`linguastack_worker_segments_total{result="failed"} 7`,
		`linguastack_worker_provider_calls_total 3`,
		"linguastack_worker_batch_duration_seconds_count 2",
	} {
		require.True(t, strings.Contains(body, line), "нет %q в\n%s", line, body)
	}
}
