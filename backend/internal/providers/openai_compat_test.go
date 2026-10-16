package providers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

func openAIServer(t *testing.T, key string, handler http.HandlerFunc) *providers.OpenAICompatible {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return providers.NewOpenAICompatible(srv.URL+"/v1/", key, srv.Client(), time.Minute)
}

func choiceReply(w http.ResponseWriter, content, finish string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   "gpt-x",
		"choices": []map[string]any{{"message": map[string]string{"content": content}, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 7, "completion_tokens": 2, "prompt_tokens_details": map[string]int{"cached_tokens": 5}},
	})
}

func TestOpenAISendsBearerAndParsesChoice(t *testing.T) {
	var got map[string]any
	o := openAIServer(t, "sk-test-key", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path, "лишний слеш в base_url не ломает путь")
		require.Equal(t, "Bearer sk-test-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		choiceReply(w, "перевод", "stop")
	})
	res, err := o.Complete(t.Context(), "gpt-x", providers.CompletionRequest{System: "инструкция", User: "текст", MaxOutputTokens: 300})
	require.NoError(t, err)
	require.Equal(t, "перевод", res.Text)
	require.Equal(t, 7, *res.InputTokens)
	require.Equal(t, 2, *res.OutputTokens)
	require.Equal(t, 5, *res.CachedInputTokens)
	require.False(t, res.Truncated)

	messages := got["messages"].([]any)
	require.Equal(t, "system", messages[0].(map[string]any)["role"])
	require.Equal(t, "user", messages[1].(map[string]any)["role"])
	require.InDelta(t, 300, got["max_tokens"], 0)
}

func TestOpenAINoAuthHeaderWithoutKey(t *testing.T) {
	// Локальный vLLM или LM Studio часто работает без ключа.
	o := openAIServer(t, "", func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		choiceReply(w, "ok", "stop")
	})
	_, err := o.Complete(t.Context(), "local", providers.CompletionRequest{User: "x"})
	require.NoError(t, err)
}

func TestOpenAIDeterminismMapsToTemperature(t *testing.T) {
	for determinism, want := range map[providers.Determinism]float64{
		providers.Strict: 0.0, providers.Balanced: 0.3, providers.Creative: 0.8,
	} {
		var got map[string]any
		o := openAIServer(t, "k", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			choiceReply(w, "x", "stop")
		})
		_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x", Determinism: determinism})
		require.NoError(t, err)
		require.InDelta(t, want, got["temperature"], 1e-9, string(determinism))
	}
}

func TestOpenAIErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, providers.ErrAuth},
		{http.StatusForbidden, providers.ErrAuth},
		{http.StatusNotFound, providers.ErrModelNotAvailable},
		{http.StatusBadGateway, providers.ErrUnavailable},
		{http.StatusUnprocessableEntity, providers.ErrBadRequest},
	}
	for _, tc := range cases {
		o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"error":{"message":"nope"}}`)
		})
		_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
		require.ErrorIs(t, err, tc.want, "HTTP %d", tc.status)
		require.Contains(t, err.Error(), "nope", "сообщение провайдера сохраняется для диагностики")
	}
	require.False(t, providers.Retryable(providers.ErrAuth))
}

func TestOpenAI429CarriesRetryAfter(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	d, ok := providers.RetryAfter(err)
	require.True(t, ok)
	require.Equal(t, 7*time.Second, d)
}

func TestOpenAIRetryAfterHTTPDateParsed(t *testing.T) {
	// Заголовок бывает и секундами, и датой (RFC 9110).
	at := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", at)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	d, ok := providers.RetryAfter(err)
	require.True(t, ok)
	require.InDelta(t, 90, d.Seconds(), 3)
}

func TestOpenAIKeyAbsentFromErrorText(t *testing.T) {
	// ТЗ §13: текст ошибки уходит в логи и в last_error задачи. Сервер,
	// эхом повторяющий заголовок авторизации, не должен раскрыть ключ.
	const key = "sk-super-secret-value-9876"
	o := openAIServer(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"bad auth header `+r.Header.Get("Authorization")+`"}}`)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), key)
	require.Contains(t, err.Error(), "***")
}

func TestOpenAIEmptyChoicesIsUnavailable(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[],"usage":{}}`)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.ErrorIs(t, err, providers.ErrUnavailable)
}

func TestOpenAIContentFilterIsRefusal(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) { choiceReply(w, "", "content_filter") })
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	var refused *providers.RefusedError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, "content_filter", refused.Category)
}

func TestOpenAITruncatedFlag(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) { choiceReply(w, "обрыв", "length") })
	res, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.NoError(t, err)
	require.True(t, res.Truncated)
}

func TestOpenAIDropsTemperatureWhenModelRejectsIt(t *testing.T) {
	// Рассуждающие модели OpenAI отвергают temperature. Один повтор без
	// параметра — и модель запоминается, чтобы не тратить запрос на каждый батч.
	var calls atomic.Int32
	o := openAIServer(t, "k", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if _, has := got["temperature"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model.","param":"temperature"}}`)
			return
		}
		choiceReply(w, "ок", "stop")
	})
	res, err := o.Complete(t.Context(), "o-reasoner", providers.CompletionRequest{User: "x"})
	require.NoError(t, err)
	require.Equal(t, "ок", res.Text)
	require.EqualValues(t, 2, calls.Load())

	_, err = o.Complete(t.Context(), "o-reasoner", providers.CompletionRequest{User: "y"})
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load(), "второй запрос — сразу без temperature")
}

func TestOpenAISwitchesToMaxCompletionTokens(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if _, has := got["max_tokens"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`)
			return
		}
		require.InDelta(t, 100, got["max_completion_tokens"], 0)
		choiceReply(w, "ок", "stop")
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x", MaxOutputTokens: 100})
	require.NoError(t, err)
}

func TestOpenAIUnrelated400IsNotRetried(t *testing.T) {
	var calls atomic.Int32
	o := openAIServer(t, "k", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"context_length_exceeded"}}`)
	})
	_, err := o.Complete(t.Context(), "m", providers.CompletionRequest{User: "x"})
	require.ErrorIs(t, err, providers.ErrBadRequest)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAIListModelsAndHealth(t *testing.T) {
	o := openAIServer(t, "k", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-x"},{"id":"gpt-y"}]}`)
	})
	models, err := o.ListModels(t.Context())
	require.NoError(t, err)
	require.Equal(t, "gpt-x", models[0].ModelID)
	require.Nil(t, models[0].InputCostPerMTok)
	require.True(t, o.Health(t.Context()).Available)
}

func TestOpenAISendsDataExternallyDependsOnHost(t *testing.T) {
	// Локальный vLLM — текст не покидает сервер; предупреждать о передаче
	// наружу (ТЗ §6.3) было бы неправдой.
	cases := map[string]bool{
		"https://api.openai.com/v1":    true,
		"https://openrouter.ai/api/v1": true,
		"http://10.0.0.5:8000/v1":      true,
		"http://localhost:1234/v1":     false,
		"http://127.0.0.1:8000/v1":     false,
		"http://[::1]:8000/v1":         false,
		"http://LOCALHOST/v1":          false,
	}
	for base, want := range cases {
		o := providers.NewOpenAICompatible(base, "", nil, time.Minute)
		require.Equal(t, want, o.SendsDataExternally(), base)
	}
	require.Equal(t, "openai_compatible", providers.NewOpenAICompatible("http://x", "", nil, 0).ID())
	require.True(t, strings.HasPrefix("openai_compatible", "openai"))
}
