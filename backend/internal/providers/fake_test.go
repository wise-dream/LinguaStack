package providers_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

func req(user string) providers.CompletionRequest {
	return providers.CompletionRequest{System: "sys", User: user, Determinism: providers.Strict}
}

const envelope = "<<<SEG 1>>>\nПервый\n<<<SEG 2>>>\nВторой\n<<<END>>>"

func TestFakeEcho(t *testing.T) {
	f := providers.NewFake("echo")
	res, err := f.Complete(t.Context(), "fake-1", req("hello"))
	require.NoError(t, err)
	require.Equal(t, "hello", res.Text)
	require.Equal(t, "fake-1", res.ModelID)
}

func TestFakePrefixTranslatesEachSegment(t *testing.T) {
	f := providers.NewFake("prefix")
	f.Prefix = "RU:"
	res, err := f.Complete(t.Context(), "fake-1", req("Контекст до.\n"+envelope))
	require.NoError(t, err)
	require.Contains(t, res.Text, "<<<SEG 1>>>\nRU:Первый")
	require.Contains(t, res.Text, "<<<SEG 2>>>\nRU:Второй")
	require.Contains(t, res.Text, "<<<END>>>")
	require.NotContains(t, res.Text, "Контекст", "контекст не переводится")
	require.Equal(t, 2, f.TranslatedCount())
}

func TestFakeProtocolViolationThenRecovers(t *testing.T) {
	f := providers.NewFake("protocol_violation")
	f.ViolateTimes = 1
	bad, err := f.Complete(t.Context(), "fake-1", req(envelope))
	require.NoError(t, err)
	require.NotContains(t, bad.Text, "<<<SEG 2>>>", "нарушение протокола: сегмента не хватает")
	good, err := f.Complete(t.Context(), "fake-1", req(envelope))
	require.NoError(t, err)
	require.Contains(t, good.Text, "<<<SEG 2>>>")
}

func TestFakeDropsPlaceholder(t *testing.T) {
	f := providers.NewFake("drops_placeholder")
	res, err := f.Complete(t.Context(), "fake-1", req("<<<SEG 1>>>\nКод ⟦1⟧ здесь\n<<<END>>>"))
	require.NoError(t, err)
	require.NotContains(t, res.Text, "⟦1⟧")
}

func TestFakeFailTimesThenSucceeds(t *testing.T) {
	f := providers.NewFake("echo")
	f.FailTimes = 2
	for range 2 {
		_, err := f.Complete(t.Context(), "fake-1", req("x"))
		require.ErrorIs(t, err, providers.ErrUnavailable)
	}
	res, err := f.Complete(t.Context(), "fake-1", req("x"))
	require.NoError(t, err)
	require.Equal(t, "x", res.Text)
}

func TestFakeFailAfterCalls(t *testing.T) {
	// Нужен для имитации падения посреди книги.
	f := providers.NewFake("echo")
	f.FailAfterCalls = 1
	_, err := f.Complete(t.Context(), "fake-1", req("a"))
	require.NoError(t, err)
	_, err = f.Complete(t.Context(), "fake-1", req("b"))
	require.ErrorIs(t, err, providers.ErrUnavailable)
}

func TestFakeFailForTexts(t *testing.T) {
	f := providers.NewFake("prefix")
	f.FailForTexts = map[string]bool{"Плохой.": true}
	res, err := f.Complete(t.Context(), "fake-1", req("<<<SEG 1>>>\nПлохой.\n<<<SEG 2>>>\nХороший.\n<<<END>>>"))
	require.NoError(t, err)
	require.NotContains(t, res.Text, "<<<SEG 1>>>", "сегмент с плохим текстом ломает протокол")
}

func TestFakeRateLimitedCarriesRetryAfter(t *testing.T) {
	_, err := providers.NewFake("rate_limited").Complete(t.Context(), "fake-1", req("x"))
	var rl *providers.RateLimitedError
	require.True(t, errors.As(err, &rl))
	require.Equal(t, time.Second, rl.RetryAfter)
	require.True(t, providers.Retryable(err))
}

func TestFakeRefusalIsNotRetryable(t *testing.T) {
	_, err := providers.NewFake("refusal").Complete(t.Context(), "fake-1", req("x"))
	var refused *providers.RefusedError
	require.True(t, errors.As(err, &refused))
	require.False(t, providers.Retryable(err))
}

func TestFakeTimeoutRespectsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := providers.NewFake("timeout").Complete(ctx, "fake-1", req("x"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, providers.Retryable(err))
	require.Less(t, time.Since(start), time.Second)
}

func TestFakeEmptyReturnsBlankSegments(t *testing.T) {
	res, err := providers.NewFake("empty").Complete(t.Context(), "fake-1", req(envelope))
	require.NoError(t, err)
	require.Contains(t, res.Text, "<<<SEG 1>>>\n\n<<<SEG 2>>>")
}

func TestFakeRecordsCalls(t *testing.T) {
	f := providers.NewFake("echo")
	_, _ = f.Complete(t.Context(), "fake-1", req("первый"))
	_, _ = f.Complete(t.Context(), "fake-1", req("второй"))
	require.Equal(t, 2, f.CallCount())
	require.Equal(t, "второй", f.Calls()[1].User)
}

func TestFakeIsConcurrencySafe(t *testing.T) {
	// Fake зовёт worker из нескольких горутин; тест имеет смысл с -race.
	f := providers.NewFake("prefix")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Complete(context.Background(), "fake-1", req(envelope))
		}()
	}
	wg.Wait()
	require.Equal(t, 20, f.CallCount())
	require.Equal(t, 40, f.TranslatedCount())
}

func TestFakeDescribesItself(t *testing.T) {
	f := providers.NewFake("echo")
	require.Equal(t, "fake", f.ID())
	require.False(t, f.SendsDataExternally())
	require.True(t, f.Health(t.Context()).Available)
	models, err := f.ListModels(t.Context())
	require.NoError(t, err)
	require.Equal(t, "fake-1", models[0].ModelID)
	caps, err := f.Capabilities(t.Context(), "fake-1")
	require.NoError(t, err)
	require.Nil(t, caps.SupportedLanguages, "ТЗ §5.2: набор языков может быть неизвестен")
	_, err = f.Capabilities(t.Context(), "nope")
	require.ErrorIs(t, err, providers.ErrModelNotAvailable)
	require.True(t, strings.HasPrefix(caps.ModelID, "fake"))
}
