package providers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

type selfRetryable struct{ retry bool }

func (e selfRetryable) Error() string   { return "своя ошибка" }
func (e selfRetryable) Retryable() bool { return e.retry }

func TestRetryableMatchesSpec(t *testing.T) {
	// Дополнение §6.1: очередь решает про повтор, не зная провайдера.
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"нет ошибки", nil, false},
		{"недоступен", providers.ErrUnavailable, true},
		{"недоступен, обёрнуто", fmt.Errorf("ollama: %w: connection refused", providers.ErrUnavailable), true},
		{"лимит частоты", &providers.RateLimitedError{RetryAfter: time.Second}, true},
		{"лимит, обёрнуто", fmt.Errorf("batch: %w", &providers.RateLimitedError{}), true},
		{"неверный ключ", providers.ErrAuth, false},
		{"нет модели", providers.ErrModelNotAvailable, false},
		{"плохой запрос", providers.ErrBadRequest, false},
		{"отказ по политике", &providers.RefusedError{Category: "cyber"}, false},
		{"наш таймаут", fmt.Errorf("запрос: %w", context.DeadlineExceeded), true},
		{"остановка сервиса", context.Canceled, false},
		{"ошибка сама объявляет повтор", selfRetryable{retry: true}, true},
		{"ошибка сама запрещает повтор", fmt.Errorf("x: %w", selfRetryable{retry: false}), false},
		{"неизвестная ошибка", errors.New("что-то"), false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, providers.Retryable(tc.err), tc.name)
	}
}

func TestRetryAfterExtracted(t *testing.T) {
	d, ok := providers.RetryAfter(fmt.Errorf("x: %w", &providers.RateLimitedError{RetryAfter: 7 * time.Second}))
	require.True(t, ok)
	require.Equal(t, 7*time.Second, d)

	_, ok = providers.RetryAfter(providers.ErrUnavailable)
	require.False(t, ok)
}

func TestErrorTextsCarryDetail(t *testing.T) {
	err := &providers.RefusedError{Category: "cyber", Explanation: "declined"}
	require.Contains(t, err.Error(), "cyber")
	require.Contains(t, (&providers.RateLimitedError{RetryAfter: 3 * time.Second}).Error(), "3s")
}

func TestDeterminismValid(t *testing.T) {
	for _, d := range []providers.Determinism{providers.Strict, providers.Balanced, providers.Creative} {
		require.True(t, d.Valid(), string(d))
	}
	require.False(t, providers.Determinism("chaotic").Valid())
	require.False(t, providers.Determinism("").Valid())
}
