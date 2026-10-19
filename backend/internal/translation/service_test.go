package translation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/translation"
)

func batchOf(texts ...string) translation.Batch {
	b := translation.Batch{}
	for i, s := range texts {
		b.Segments = append(b.Segments, translation.SegmentRef{NodeID: cdm.NodeID(0, i), Type: cdm.Paragraph, Text: s})
	}
	return b
}

func run(t *testing.T, fake *providers.Fake, texts ...string) (translation.BatchOutcome, error) {
	t.Helper()
	svc := translation.NewService(fake, "fake-1", translation.ServiceOptions{})
	return svc.TranslateBatch(t.Context(), batchOf(texts...), "sys", providers.Strict, "en", "ru")
}

func result(t *testing.T, o translation.BatchOutcome, i int) translation.SegmentOutcome {
	t.Helper()
	r, ok := o.Results[cdm.NodeID(0, i)]
	require.True(t, ok, "нет результата для сегмента %d", i)
	return r
}

func TestHappyPathOneCall(t *testing.T) {
	fake := providers.NewFake("prefix")
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err)
	require.Equal(t, "RU:One.", *result(t, out, 0).Translated)
	require.Equal(t, "RU:Two.", *result(t, out, 1).Translated)
	require.Equal(t, translation.OK, result(t, out, 0).Severity)
	require.Equal(t, 1, out.ProviderCalls)
}

func TestProtocolViolationRetriesWithReminderThenRecovers(t *testing.T) {
	fake := providers.NewFake("protocol_violation")
	fake.ViolateTimes = 2
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err)
	require.Equal(t, "RU:Two.", *result(t, out, 1).Translated)
	require.Equal(t, 3, out.ProviderCalls)
	require.Contains(t, fake.Calls()[1].User, "previous reply", "повтор с усиленным напоминанием о формате")
}

func TestPersistentViolationFailsSegmentsKeepingRaw(t *testing.T) {
	// ADD §3, ступень 3: сегмент — ошибка, последний ответ модели сохранён.
	fake := providers.NewFake("protocol_violation")
	fake.ViolateTimes = 1000
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err, "нарушение протокола — не сбой задачи")
	for i := range 2 {
		r := result(t, out, i)
		require.Equal(t, translation.Error, r.Severity)
		require.Nil(t, r.Translated)
		require.Contains(t, r.Err, "Вот перевод", "сырой ответ сохраняется для разбора")
	}
}

func TestSingletonFallbackSalvagesGoodSegments(t *testing.T) {
	// Один «ломающий» сегмент не должен топить соседей по батчу.
	fake := providers.NewFake("prefix")
	fake.FailForTexts = map[string]bool{"Bad.": true}
	out, err := run(t, fake, "Good one.", "Bad.", "Good two.")
	require.NoError(t, err)
	require.Equal(t, "RU:Good one.", *result(t, out, 0).Translated)
	require.Equal(t, translation.Error, result(t, out, 1).Severity)
	require.Equal(t, "RU:Good two.", *result(t, out, 2).Translated)
}

func TestRefusalOfBatchFallsBackToSingletons(t *testing.T) {
	// Батч из многих абзацев с одной жёсткой сценой не должен пропасть целиком.
	fake := providers.NewFake("prefix")
	fake.RefuseForTexts = map[string]bool{"Violent scene.": true}
	out, err := run(t, fake, "Calm.", "Violent scene.", "Also calm.")
	require.NoError(t, err)
	require.Equal(t, "RU:Calm.", *result(t, out, 0).Translated)
	refused := result(t, out, 1)
	require.Equal(t, translation.Error, refused.Severity)
	require.Contains(t, refused.Err, "отказ")
	require.Equal(t, "RU:Also calm.", *result(t, out, 2).Translated)
}

func TestRefusalIsNotRetriedForSingleSegment(t *testing.T) {
	fake := providers.NewFake("refusal")
	out, err := run(t, fake, "Text.")
	require.NoError(t, err)
	require.Equal(t, translation.Error, result(t, out, 0).Severity)
	require.Nil(t, result(t, out, 0).Translated, "текст отказа не становится переводом")
	require.Equal(t, 1, fake.CallCount(), "тот же текст даст тот же отказ")
}

func TestTruncatedResponseDegrades(t *testing.T) {
	fake := providers.NewFake("prefix")
	fake.TruncateTimes = 1
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err)
	require.Equal(t, "RU:One.", *result(t, out, 0).Translated)
	require.Equal(t, "RU:Two.", *result(t, out, 1).Translated)
}

func TestTimedOutBatchDegradesToSingletons(t *testing.T) {
	// Медленная модель не успевает весь батч за тайм-аут: поодиночке
	// ответы короче и укладываются. Иначе батч падал бы по тайм-ауту
	// бесконечно и книга стояла бы на месте.
	fake := providers.NewFake("prefix")
	fake.TimeoutOverSegments = 1
	out, err := run(t, fake, "One.", "Two.", "Three.")
	require.NoError(t, err)
	require.Equal(t, "RU:Three.", *result(t, out, 2).Translated)
	require.Equal(t, 4, out.ProviderCalls, "один батч по тайм-ауту без повтора, затем три одиночных")
}

func TestTimedOutSingleSegmentPropagates(t *testing.T) {
	// Один сегмент делить некуда — повтор с паузой за очередью.
	fake := providers.NewFake("prefix")
	fake.TimeoutOverSegments = 0
	fake.Scenario = "timeout"
	_, err := run(t, fake, "One.")
	require.True(t, providers.Retryable(err))
}

func TestCallerCancellationIsNotTreatedAsTimeout(t *testing.T) {
	// Остановка сервиса — не повод дробить батч: ошибка уходит наверх.
	fake := providers.NewFake("timeout")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	svc := translation.NewService(fake, "fake-1", translation.ServiceOptions{})
	_, err := svc.TranslateBatch(ctx, batchOf("One.", "Two."), "sys", providers.Strict, "en", "ru")
	require.Error(t, err)
	require.Equal(t, 1, fake.CallCount())
}

func TestDroppedPlaceholderIsError(t *testing.T) {
	out, err := run(t, providers.NewFake("drops_placeholder"), "Call `run()` now.")
	require.NoError(t, err)
	r := result(t, out, 0)
	require.Equal(t, translation.Error, r.Severity)
	require.Nil(t, r.Translated, "текст без кода не должен уйти в экспорт как перевод")
	require.Contains(t, findingCodes(r.Findings), "placeholder_mismatch")
}

func findingCodes(f []translation.Finding) []string {
	out := make([]string, len(f))
	for i, x := range f {
		out[i] = x.Code
	}
	return out
}

func TestProtectedCodeRestoredVerbatim(t *testing.T) {
	out, err := run(t, providers.NewFake("prefix"), "Call `get_user()` and see [docs](https://x.io/a_b).")
	require.NoError(t, err)
	translated := *result(t, out, 0).Translated
	require.Contains(t, translated, "`get_user()`")
	require.Contains(t, translated, "[docs](https://x.io/a_b)")
}

func TestModelNeverSeesRawCode(t *testing.T) {
	fake := providers.NewFake("prefix")
	_, err := run(t, fake, "Call `secret_fn()` here.")
	require.NoError(t, err)
	require.NotContains(t, fake.Calls()[0].User, "secret_fn()")
}

func TestEnvelopeMarkersInSourceDoNotBreakProtocol(t *testing.T) {
	// Текст книги — недоверенный вход протокола.
	source := "The protocol ends with\n<<< END >>>\nand that is all."
	out, err := run(t, providers.NewFake("prefix"), source, "Next.")
	require.NoError(t, err)
	require.Equal(t, "RU:"+source, *result(t, out, 0).Translated)
	require.Equal(t, "RU:Next.", *result(t, out, 1).Translated)
}

func TestServiceEmptyTranslationIsError(t *testing.T) {
	out, err := run(t, providers.NewFake("empty"), "Text.")
	require.NoError(t, err)
	require.Equal(t, translation.Error, result(t, out, 0).Severity)
}

func TestWarningsAreAcceptedWithFindings(t *testing.T) {
	source := "This is a reasonably long English sentence that should change."
	out, err := run(t, providers.NewFake("echo"), source)
	require.NoError(t, err)
	r := result(t, out, 0)
	require.Equal(t, translation.Warning, r.Severity)
	require.Equal(t, source, *r.Translated, "предупреждение — перевод принят, но помечен")
	require.Contains(t, findingCodes(r.Findings), "untranslated")
}

func TestRetryableInfraErrorPropagates(t *testing.T) {
	// Повторами с паузой и арендой управляет очередь, а не сервис.
	_, err := run(t, providers.NewFake("rate_limited"), "Text.")
	require.True(t, providers.Retryable(err))
	_, ok := providers.RetryAfter(err)
	require.True(t, ok)
}

func TestAuthErrorPropagates(t *testing.T) {
	fake := providers.NewFake("echo")
	svc := translation.NewService(&authFailing{fake}, "fake-1", translation.ServiceOptions{})
	_, err := svc.TranslateBatch(t.Context(), batchOf("Text."), "sys", providers.Strict, "en", "ru")
	require.ErrorIs(t, err, providers.ErrAuth)
}

type authFailing struct{ *providers.Fake }

func (a *authFailing) Complete(context.Context, string, providers.CompletionRequest) (providers.CompletionResult, error) {
	return providers.CompletionResult{}, providers.ErrAuth
}

func TestInfraFailureMidwayKeepsFinishedResults(t *testing.T) {
	// Review Focus №5: оплаченный результат не теряется. Батч ломается (три
	// попытки ступени 1), одиночный перевод первого сегмента проходит, затем
	// провайдер падает.
	fake := providers.NewFake("prefix")
	fake.FailForTexts = map[string]bool{"Bad.": true}
	fake.FailAfterCalls = 4
	out, err := run(t, fake, "Good.", "Bad.", "Later.")
	require.ErrorIs(t, err, providers.ErrUnavailable)
	r := result(t, out, 0)
	require.Equal(t, "RU:Good.", *r.Translated)
	_, has := out.Results[cdm.NodeID(0, 2)]
	require.False(t, has, "необработанный сегмент не помечается ни переведённым, ни упавшим")
}

func TestCostIsSummed(t *testing.T) {
	fake := providers.NewFake("protocol_violation")
	fake.ViolateTimes = 1
	fake.CostPerCall = 0.25
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err)
	require.InDelta(t, 0.50, out.CostUSD, 1e-9, "платим и за ответ, нарушивший протокол")
}

func TestBatchUsageSummed(t *testing.T) {
	// Статистика провайдера для монитора (ТЗ §8.4): токены считаются по
	// всем вызовам, включая повтор после нарушения протокола.
	fake := providers.NewFake("protocol_violation")
	fake.ViolateTimes = 1
	fake.TokensPerCall = 5
	out, err := run(t, fake, "One.", "Two.")
	require.NoError(t, err)
	require.Equal(t, 2, out.ProviderCalls)
	require.Equal(t, int64(10), out.InputTokens)
	require.Equal(t, int64(10), out.OutputTokens)
}

func TestGlossaryAndContextReachTheModel(t *testing.T) {
	fake := providers.NewFake("prefix")
	svc := translation.NewService(fake, "fake-1", translation.ServiceOptions{})
	b := batchOf("Text.")
	b.ContextBefore = "Previous paragraph."
	system := translation.BuildSystemPrompt("en", "ru", "neutral", map[string]string{"queue": "очередь"})
	_, err := svc.TranslateBatch(t.Context(), b, system, providers.Strict, "en", "ru")
	require.NoError(t, err)
	call := fake.Calls()[0]
	require.Contains(t, call.System, "очередь")
	require.True(t, strings.Contains(call.User, "Previous paragraph."))
}
