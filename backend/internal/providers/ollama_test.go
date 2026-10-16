package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

func ollamaServer(t *testing.T, handler http.HandlerFunc) *providers.Ollama {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return providers.NewOllama(srv.URL, srv.Client(), providers.OllamaOptions{NumCtx: 8192, Timeout: 5 * time.Second})
}

// waitForDisconnect имитирует долгую генерацию. Тело запроса читается до
// конца: net/http начинает замечать отключение клиента только после этого,
// иначе обработчик не узнает об отмене и закрытие сервера зависнет.
func waitForDisconnect(r *http.Request, limit time.Duration) {
	_, _ = io.Copy(io.Discard, r.Body)
	select {
	case <-r.Context().Done():
	case <-time.After(limit):
	}
}

func chatReply(w http.ResponseWriter, content, doneReason string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model": "qwen3:8b", "message": map[string]string{"role": "assistant", "content": content},
		"done": true, "done_reason": doneReason, "prompt_eval_count": 10, "eval_count": 3,
	})
}

func TestOllamaListModels(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tags", r.URL.Path)
		_, _ = io.WriteString(w, `{"models":[{"name":"qwen3:8b","details":{"parameter_size":"8.2B","family":"qwen3"}}]}`)
	})
	models, err := o.ListModels(t.Context())
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "qwen3:8b", models[0].ModelID)
	require.Nil(t, models[0].InputCostPerMTok, "локальная модель не имеет цены")
	require.True(t, models[0].SupportsTemperature)
}

func TestOllamaRequestShape(t *testing.T) {
	var got map[string]any
	o := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/chat", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		chatReply(w, "переведено", "stop")
	})
	res, err := o.Complete(t.Context(), "qwen3:8b", providers.CompletionRequest{
		System: "инструкция", User: "текст", Determinism: providers.Strict, MaxOutputTokens: 500,
	})
	require.NoError(t, err)
	require.Equal(t, "переведено", res.Text)
	require.Equal(t, 10, *res.InputTokens)
	require.Equal(t, 3, *res.OutputTokens)
	require.Nil(t, res.CostUSD)

	require.Equal(t, false, got["stream"])
	// Рассуждения qwen3 увеличивают выход в десятки раз и переводу не нужны.
	require.Equal(t, false, got["think"])
	opts := got["options"].(map[string]any)
	require.InDelta(t, 0.0, opts["temperature"], 1e-9)
	require.InDelta(t, 8192, opts["num_ctx"], 0, "num_ctx задаётся явно — иначе Ollama молча обрежет батч")
	require.InDelta(t, 500, opts["num_predict"], 0)
	messages := got["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Equal(t, "инструкция", messages[0].(map[string]any)["content"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestOllamaDeterminismMapsToTemperature(t *testing.T) {
	for determinism, want := range map[providers.Determinism]float64{
		providers.Strict: 0.0, providers.Balanced: 0.3, providers.Creative: 0.8,
	} {
		var got map[string]any
		o := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			chatReply(w, "x", "stop")
		})
		_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x", Determinism: determinism})
		require.NoError(t, err)
		require.InDelta(t, want, got["options"].(map[string]any)["temperature"], 1e-9, string(determinism))
	}
}

func TestOllamaStripsLeakedThinkTags(t *testing.T) {
	// Старые версии Ollama кладут рассуждения прямо в content.
	o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		chatReply(w, "<think>\nрассуждаю о переводе\n</think>\n\nКнига на столе.", "stop")
	})
	res, err := o.Complete(t.Context(), "qwen3:8b", providers.CompletionRequest{User: "x"})
	require.NoError(t, err)
	require.Equal(t, "Книга на столе.", res.Text)
}

func TestOllamaTruncatedFlag(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) { chatReply(w, "One, two", "length") })
	res, err := o.Complete(t.Context(), "qwen3:8b", providers.CompletionRequest{User: "x"})
	require.NoError(t, err)
	require.True(t, res.Truncated)
}

func TestOllamaErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"нет модели", http.StatusNotFound, `{"error":"model 'nope' not found"}`, providers.ErrModelNotAvailable},
		{"перегрузка", http.StatusServiceUnavailable, "overloaded", providers.ErrUnavailable},
		{"внутренняя ошибка", http.StatusInternalServerError, `{"error":"boom"}`, providers.ErrUnavailable},
		{"плохой запрос", http.StatusBadRequest, `{"error":"invalid options"}`, providers.ErrBadRequest},
	}
	for _, tc := range cases {
		o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		})
		_, err := o.Complete(t.Context(), "nope", providers.CompletionRequest{User: "x"})
		require.ErrorIs(t, err, tc.want, tc.name)
	}
}

func TestOllamaRateLimited(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "4")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	d, ok := providers.RetryAfter(err)
	require.True(t, ok)
	require.Equal(t, 4*time.Second, d)
}

func TestOllamaConnectionRefusedIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	o := providers.NewOllama(url, http.DefaultClient, providers.OllamaOptions{NumCtx: 8192, Timeout: time.Second})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.ErrorIs(t, err, providers.ErrUnavailable)
	require.True(t, providers.Retryable(err))
	require.False(t, o.Health(t.Context()).Available)
}

func TestOllamaTimeoutIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		waitForDisconnect(r, 2*time.Second)
	}))
	t.Cleanup(srv.Close)
	o := providers.NewOllama(srv.URL, srv.Client(), providers.OllamaOptions{NumCtx: 8192, Timeout: 50 * time.Millisecond})
	start := time.Now()
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.True(t, providers.Retryable(err), "получено: %v", err)
	require.Less(t, time.Since(start), time.Second)
}

func TestOllamaCallerCancellationIsNotRetryable(t *testing.T) {
	// Остановка сервиса — не сбой провайдера: задачу не надо считать упавшей.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		waitForDisconnect(r, 2*time.Second)
	}))
	t.Cleanup(srv.Close)
	o := providers.NewOllama(srv.URL, srv.Client(), providers.OllamaOptions{NumCtx: 8192, Timeout: 5 * time.Second})
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(30*time.Millisecond, cancel)
	_, err := o.Complete(ctx, "m", providers.CompletionRequest{User: "x"})
	require.True(t, errors.Is(err, context.Canceled), "получено: %v", err)
	require.False(t, providers.Retryable(err))
}

func TestOllamaCapabilitiesFromShow(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/show", r.URL.Path)
		_, _ = io.WriteString(w, `{"model_info":{"qwen3.context_length":40960},"capabilities":["completion","thinking"]}`)
	})
	caps, err := o.Capabilities(t.Context(), "qwen3:8b")
	require.NoError(t, err)
	// Действующее окно — то, что мы задаём в num_ctx, а не максимум модели.
	require.Equal(t, 8192, *caps.ContextTokens)
	require.Nil(t, caps.SupportedLanguages)
}

func TestOllamaCapabilitiesSmallModelWindow(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model_info":{"llama.context_length":4096}}`)
	})
	caps, err := o.Capabilities(t.Context(), "small")
	require.NoError(t, err)
	require.Equal(t, 4096, *caps.ContextTokens, "окно модели меньше num_ctx — берётся оно")
}

func TestOllamaCapabilitiesMissingModel(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"model 'x' not found"}`)
	})
	_, err := o.Capabilities(t.Context(), "x")
	require.ErrorIs(t, err, providers.ErrModelNotAvailable)
}

func TestOllamaHealth(t *testing.T) {
	o := ollamaServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/version", r.URL.Path)
		_, _ = io.WriteString(w, `{"version":"0.18.2"}`)
	})
	h := o.Health(t.Context())
	require.True(t, h.Available)
	require.Contains(t, h.Detail, "0.18.2")
}

func TestOllamaDescribesItself(t *testing.T) {
	o := providers.NewOllama("http://localhost:1", nil, providers.OllamaOptions{})
	require.Equal(t, "ollama", o.ID())
	require.False(t, o.SendsDataExternally(), "локальная модель: текст не покидает сервер")
}

func TestLiveOllamaTranslatesSentence(t *testing.T) {
	if os.Getenv("LINGUASTACK_LIVE_OLLAMA") != "1" {
		t.Skip("живой Ollama не включён (LINGUASTACK_LIVE_OLLAMA=1)")
	}
	o := providers.NewOllama("http://localhost:11434", http.DefaultClient,
		providers.OllamaOptions{NumCtx: 8192, Timeout: 3 * time.Minute})
	require.True(t, o.Health(t.Context()).Available)
	caps, err := o.Capabilities(t.Context(), "qwen3:8b")
	require.NoError(t, err)
	require.Equal(t, 8192, *caps.ContextTokens)

	res, err := o.Complete(t.Context(), "qwen3:8b", providers.CompletionRequest{
		System: "Translate English to Russian. Reply with the translation only.",
		User:   "The book is on the table.", Determinism: providers.Strict,
	})
	require.NoError(t, err)
	require.Contains(t, res.Text, "Книга")
	require.NotContains(t, res.Text, "<think>")
	require.Less(t, *res.OutputTokens, 50, "рассуждения выключены")
}
