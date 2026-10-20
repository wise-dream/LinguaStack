package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/httpapi"
)

func scrape(t *testing.T, c *apiClient, auth string) *response {
	t.Helper()
	if auth == "" {
		return c.do(http.MethodGet, "/metrics", nil)
	}
	return c.do(http.MethodGet, "/metrics", nil, [2]string{"Authorization", auth})
}

func TestMetricsRequireToken(t *testing.T) {
	off := newTestEnv(t)
	require.Equal(t, http.StatusNotFound, scrape(t, off.client(t), "Bearer x").Status, "токен не задан — маршрута нет")

	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.MetricsToken = "metrics-token-for-tests" })
	c := env.client(t)
	require.Equal(t, http.StatusUnauthorized, scrape(t, c, "").Status)
	require.Equal(t, http.StatusUnauthorized, scrape(t, c, "Bearer wrong").Status)
	r := scrape(t, c, "Bearer metrics-token-for-tests")
	require.Equal(t, http.StatusOK, r.Status)
	require.Contains(t, string(r.Body), "linguastack_users ")
	require.Contains(t, string(r.Body), "go_goroutines")
}

func TestHTTPMetricsByRoutePattern(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.MetricsToken = "tok" })
	c := env.signedIn(t, "m@example.com")
	b := uploadBook(t, c, "book.txt", "Текст книги.")
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/books/"+b.ID, nil).Status)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/nope", nil).Status)

	body := string(scrape(t, c, "Bearer tok").Body)
	require.Contains(t, body, `linguastack_http_requests_total{method="GET",route="/api/books/{id}",status="200"} 1`)
	require.Contains(t, body, `linguastack_http_requests_total{method="POST",route="/api/books",status="201"} 1`)
	require.Contains(t, body, `route="unmatched",status="404"`)
	require.NotContains(t, body, b.ID, "настоящий идентификатор не попадает в метки")
}

// logBuffer — потокобезопасный приёмник журнала.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		out = append(out, rec)
	}
	return out
}

func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	buf := &logBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestAccessLogAndRequestID(t *testing.T) {
	logs := captureLog(t)
	env := newTestEnv(t)
	c := env.signedIn(t, "log@example.com")
	b := uploadBook(t, c, "book.txt", "Текст книги.")

	r := c.do(http.MethodGet, "/api/books/"+b.ID+"?q=secret-query", nil, [2]string{"X-Request-ID", "req-from-proxy-42"})
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, "req-from-proxy-42", r.Header.Get("X-Request-ID"), "входящий ID возвращается")

	fresh := c.do(http.MethodGet, "/api/books", nil)
	require.Len(t, fresh.Header.Get("X-Request-ID"), 32, "без входящего — новый")
	bad := c.do(http.MethodGet, "/api/books", nil, [2]string{"X-Request-ID", "плохой id <script>"})
	require.Len(t, bad.Header.Get("X-Request-ID"), 32, "негодный ID заменяется")

	var me struct {
		ID string `json:"id"`
	}
	c.do(http.MethodGet, "/api/me", nil).JSON(t, &me)

	var found map[string]any
	for _, rec := range logs.records(t) {
		if rec["request_id"] == "req-from-proxy-42" && rec["msg"] == "запрос" {
			found = rec
		}
	}
	require.NotNil(t, found, "строка журнала с ID запроса")
	require.Equal(t, "GET", found["method"])
	require.Equal(t, "/api/books/{id}", found["route"])
	require.EqualValues(t, 200, found["status"])
	require.Equal(t, me.ID, found["user"])
	require.Contains(t, found, "duration_ms")

	raw := func() string {
		logs.mu.Lock()
		defer logs.mu.Unlock()
		return logs.buf.String()
	}()
	require.NotContains(t, raw, "secret-query", "строка запроса не журналируется")
	require.NotContains(t, raw, c.cookie("linguastack_session"), "cookie сессии не журналируется")
	require.NotContains(t, raw, testPassword)
}
