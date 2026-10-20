package main

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/queue"
)

func TestServeMetrics(t *testing.T) {
	w := &queue.Worker{}
	addr, stop, err := serveMetrics(t.Context(), config.Settings{WorkerMetricsAddr: "127.0.0.1:0"}, w)
	require.NoError(t, err)
	stop()
	require.Empty(t, addr, "без токена метрики не отдаются")
	require.Nil(t, w.Metrics)

	addr, stop, err = serveMetrics(t.Context(), config.Settings{WorkerMetricsAddr: "127.0.0.1:0", MetricsToken: "tok"}, w)
	require.NoError(t, err)
	t.Cleanup(stop)
	require.NotNil(t, w.Metrics)

	get := func(auth string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/metrics", nil)
		require.NoError(t, err)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	code, _ := get("")
	require.Equal(t, http.StatusUnauthorized, code)
	code, body := get("Bearer tok")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, "linguastack_worker_batch_duration_seconds")

	_, _, err = serveMetrics(t.Context(), config.Settings{WorkerMetricsAddr: addr, MetricsToken: "tok"}, &queue.Worker{})
	require.Error(t, err, "занятый адрес — ошибка запуска")
}
