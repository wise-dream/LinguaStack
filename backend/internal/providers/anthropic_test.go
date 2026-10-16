package providers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

const testAnthropicKey = "sk-ant-test-key-0123456789"

type anthropicCapture struct {
	body    map[string]any
	headers http.Header
	calls   atomic.Int32
}

func anthropicServer(t *testing.T, reply func(w http.ResponseWriter)) (*providers.Anthropic, *anthropicCapture) {
	t.Helper()
	capture := &anthropicCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.calls.Add(1)
		capture.headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		capture.body = nil
		_ = json.Unmarshal(raw, &capture.body)
		w.Header().Set("Content-Type", "application/json")
		reply(w)
	}))
	t.Cleanup(srv.Close)
	a := providers.NewAnthropic(testAnthropicKey, time.Minute,
		option.WithBaseURL(srv.URL), option.WithHTTPClient(srv.Client()))
	return a, capture
}

func messageJSON(model, text, stopReason string, usage map[string]any) string {
	if usage == nil {
		usage = map[string]any{"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0}
	}
	raw, _ := json.Marshal(map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": model,
		"content": []map[string]any{
			{"type": "thinking", "thinking": "", "signature": "sig"},
			{"type": "text", "text": text},
		},
		"stop_reason": stopReason, "stop_details": nil, "usage": usage,
	})
	return string(raw)
}

func okReply(text string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-opus-5-5", text, "end_turn", nil))
	}
}

func complete(t *testing.T, a *providers.Anthropic, model string, req providers.CompletionRequest) (providers.CompletionResult, error) {
	t.Helper()
	return a.Complete(t.Context(), model, req)
}

func TestAnthropicNeverSendsSamplingOrThinkingParams(t *testing.T) {
	// Opus/Sonnet 5.5 отвечают 400 на temperature/top_p/top_k, а
	// отключение рассуждений на Opus 5.5 — тоже 400 (дополнение §6).
	a, c := anthropicServer(t, okReply("перевод"))
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{System: "s", User: "u", Determinism: providers.Strict})
	require.NoError(t, err)
	require.Equal(t, "перевод", res.Text)
	for _, key := range []string{"temperature", "top_p", "top_k", "thinking"} {
		require.NotContains(t, c.body, key)
	}
	require.Equal(t, testAnthropicKey, c.headers.Get("X-Api-Key"))
}

func TestAnthropicDeterminismMapsToEffort(t *testing.T) {
	for determinism, effort := range map[providers.Determinism]string{
		providers.Strict: "low", providers.Balanced: "medium", providers.Creative: "high",
	} {
		a, c := anthropicServer(t, okReply("x"))
		_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u", Determinism: determinism})
		require.NoError(t, err)
		require.Equal(t, effort, c.body["output_config"].(map[string]any)["effort"], string(determinism))
	}
}

func TestAnthropicSystemPromptCacheControlled(t *testing.T) {
	// Системный промпт одинаков для всех батчей книги — основная экономия (ADD §6.2).
	a, c := anthropicServer(t, okReply("x"))
	_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{System: "инструкция", User: "u"})
	require.NoError(t, err)
	system := c.body["system"].([]any)[0].(map[string]any)
	require.Equal(t, "инструкция", system["text"])
	require.Equal(t, "ephemeral", system["cache_control"].(map[string]any)["type"])
}

func TestAnthropicNoSystemWhenEmpty(t *testing.T) {
	a, c := anthropicServer(t, okReply("x"))
	_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.NotContains(t, c.body, "system")
}

func TestAnthropicFallbacksDefaultForOpusAndSonnet(t *testing.T) {
	// Художественный текст может задеть классификаторы безопасности:
	// отказ пересылается резервной модели на стороне API.
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5"} {
		a, c := anthropicServer(t, okReply("x"))
		_, err := complete(t, a, model, providers.CompletionRequest{User: "u"})
		require.NoError(t, err)
		require.Equal(t, "default", c.body["fallbacks"], model)
		require.Contains(t, c.headers.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01", model)
	}
}

func TestAnthropicNoFallbacksForHaiku(t *testing.T) {
	// У Haiku 5.5 серверного fallback нет — параметр дал бы 400.
	a, c := anthropicServer(t, okReply("x"))
	_, err := complete(t, a, "claude-haiku-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.NotContains(t, c.body, "fallbacks")
	require.NotContains(t, c.headers.Get("Anthropic-Beta"), "server-side-fallback")
}

func TestAnthropicRefusalRaisedBeforeContent(t *testing.T) {
	// stop_reason "refusal" приходит с HTTP 200 — принять его за перевод нельзя.
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		raw := strings.Replace(messageJSON("claude-opus-5-5", "Я не могу помочь с этим.", "refusal", nil),
			`"stop_details":null`, `"stop_details":{"type":"refusal","category":"cyber","explanation":"declined"}`, 1)
		_, _ = io.WriteString(w, raw)
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	var refused *providers.RefusedError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, "cyber", refused.Category)
	require.False(t, providers.Retryable(err))
	require.Empty(t, res.Text, "текст отказа не должен стать переводом")
}

func TestAnthropicIgnoresThinkingBlocks(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		raw := strings.Replace(messageJSON("claude-opus-5-5", "перевод", "end_turn", nil),
			`"thinking":""`, `"thinking":"скрытые рассуждения"`, 1)
		_, _ = io.WriteString(w, raw)
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.Equal(t, "перевод", res.Text)
}

func TestAnthropicTruncatedOnMaxTokens(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-opus-5-5", "обрыв", "max_tokens", nil))
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.True(t, res.Truncated)
}

func TestAnthropicMaxTokensAlwaysSent(t *testing.T) {
	a, c := anthropicServer(t, okReply("x"))
	_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, c.body["max_tokens"], float64(4096))

	_, err = complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u", MaxOutputTokens: 9000})
	require.NoError(t, err)
	require.InDelta(t, 9000, c.body["max_tokens"], 0)
}

func TestAnthropicCostFromCatalog(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-opus-5-5", "x", "end_turn", map[string]any{
			"input_tokens": 1_000_000, "output_tokens": 1_000_000, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0,
		}))
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	// Дополнение §6.2: $4.00 за 1M входных + $20.00 за 1M выходных.
	require.InDelta(t, 24.0, *res.CostUSD, 1e-9)
}

func TestAnthropicCostCountsCacheReadsAndWrites(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-opus-5-5", "x", "end_turn", map[string]any{
			"input_tokens": 0, "output_tokens": 0, "cache_read_input_tokens": 1_000_000, "cache_creation_input_tokens": 1_000_000,
		}))
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.Equal(t, 1_000_000, *res.CachedInputTokens)
	// Чтение кэша $0.20/MTok, запись — 1.25 × цены входа ($5.00).
	require.InDelta(t, 0.20+5.00, *res.CostUSD, 1e-9)
}

func TestAnthropicCostUsesServingModelsOnFallback(t *testing.T) {
	// Отказ пересылается резервной модели: каждый шаг тарифицируется по
	// ценам своей модели (usage.iterations).
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-opus-4-8", "x", "end_turn", map[string]any{
			"input_tokens": 2_000_000, "output_tokens": 1_000_000, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0,
			"iterations": []map[string]any{
				{"type": "fallback_message", "model": "claude-opus-5-5", "input_tokens": 1_000_000, "output_tokens": 0, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0},
				{"type": "message", "model": "claude-opus-4-8", "input_tokens": 1_000_000, "output_tokens": 1_000_000, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0},
			},
		}))
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4-8", res.ModelID, "в результате — модель, которая реально ответила")
	require.InDelta(t, 4.00+5.00+25.00, *res.CostUSD, 1e-9)
}

func TestAnthropicUnknownServingModelCostIsNil(t *testing.T) {
	// Неизвестная цена — честный nil, а не догадка.
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, messageJSON("claude-future-9", "x", "end_turn", nil))
	})
	res, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.NoError(t, err)
	require.Nil(t, res.CostUSD)
}

func TestAnthropicErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusUnauthorized, providers.ErrAuth},
		{http.StatusForbidden, providers.ErrAuth},
		{http.StatusNotFound, providers.ErrModelNotAvailable},
		{http.StatusBadRequest, providers.ErrBadRequest},
		{http.StatusInternalServerError, providers.ErrUnavailable},
		{529, providers.ErrUnavailable},
	}
	for _, tc := range cases {
		a, c := anthropicServer(t, func(w http.ResponseWriter) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"some_error","message":"описание"}}`)
		})
		_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
		require.ErrorIs(t, err, tc.want, "HTTP %d", tc.status)
		// Повторами управляет очередь; повторы SDK умножили бы бюджет попыток.
		require.EqualValues(t, 1, c.calls.Load(), "HTTP %d: SDK не должен повторять сам", tc.status)
	}
}

func TestAnthropicRateLimitedCarriesRetryAfter(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	})
	_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	d, ok := providers.RetryAfter(err)
	require.True(t, ok, "получено: %v", err)
	require.Equal(t, 12*time.Second, d)
}

func TestAnthropicKeyAbsentFromErrorText(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key `+testAnthropicKey+`"}}`)
	})
	_, err := complete(t, a, "claude-opus-5-5", providers.CompletionRequest{User: "u"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), testAnthropicKey)
}

func TestAnthropicCatalog(t *testing.T) {
	a := providers.NewAnthropic(testAnthropicKey, time.Minute)
	models, err := a.ListModels(t.Context())
	require.NoError(t, err)
	var ids []string
	for _, m := range models {
		ids = append(ids, m.ModelID)
		require.False(t, m.SupportsTemperature, "%s: temperature вызывает 400", m.ModelID)
		require.NotNil(t, m.InputCostPerMTok, m.ModelID)
	}
	require.Equal(t, []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}, ids, "по умолчанию — Opus 5.5")
	dated := regexp.MustCompile(`-\d{8}$`)
	for _, id := range ids {
		require.False(t, dated.MatchString(id), "суффиксы даты запрещены: %s", id)
	}

	_, err = a.Capabilities(t.Context(), "claude-2")
	require.ErrorIs(t, err, providers.ErrModelNotAvailable)
	require.True(t, a.SendsDataExternally())
	require.Equal(t, "anthropic", a.ID())
}

func TestAnthropicHealthValidatesKey(t *testing.T) {
	a, _ := anthropicServer(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	})
	h := a.Health(t.Context())
	require.False(t, h.Available)
	require.NotContains(t, h.Detail, testAnthropicKey)
}

// TestLiveAnthropicTranslatesSentence — живой запрос к API. Тратит деньги,
// поэтому выполняется только явно: LINGUASTACK_LIVE_ANTHROPIC=1 и ключ в
// ANTHROPIC_API_KEY.
func TestLiveAnthropicTranslatesSentence(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if os.Getenv("LINGUASTACK_LIVE_ANTHROPIC") != "1" || key == "" {
		t.Skip("живой Anthropic не включён (LINGUASTACK_LIVE_ANTHROPIC=1 и ANTHROPIC_API_KEY)")
	}
	a := providers.NewAnthropic(key, 2*time.Minute)
	require.True(t, a.Health(t.Context()).Available)
	res, err := a.Complete(t.Context(), "claude-haiku-5-5", providers.CompletionRequest{
		System: "Translate English to Russian. Reply with the translation only.",
		User:   "The book is on the table.", Determinism: providers.Strict, MaxOutputTokens: 1024,
	})
	require.NoError(t, err)
	require.Contains(t, res.Text, "Книга")
	require.NotNil(t, res.CostUSD)
}
