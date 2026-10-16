package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Anthropic — модели Claude (ТЗ §6.3, дополнение §6). Три особенности
// ломают наивную реализацию:
//
//   - temperature/top_p/top_k не передаются никогда: Opus и Sonnet 5.5
//     отвечают 400. Намерение переводится в output_config.effort. Параметр
//     thinking тоже не передаётся: на Opus 5.5 рассуждения не отключаются.
//   - stop_reason "refusal" проверяется до чтения content: отказ приходит с
//     HTTP 200, и текст отказа нельзя принять за перевод.
//   - системный промпт помечается cache_control: он одинаков для всех
//     батчей книги, это основная экономия.
//
// Для Opus и Sonnet 5.5 включён серверный fallback ("default"): отказ
// классификатора на фрагменте художественного текста повторяется
// резервной моделью внутри того же запроса.
type Anthropic struct {
	client  anthropic.Client
	apiKey  string
	timeout time.Duration
}

const defaultAnthropicMaxTokens = 16000

// anthropicPrice — цены за 1M токенов (дополнение §6.2; кэш навыка
// claude-api от 2026-10-06). Чтение кэша дешевле входа; запись (TTL 5 минут) —
// 1.25 × вход.
type anthropicPrice struct {
	input, output, cacheRead float64
}

var anthropicPrices = map[string]anthropicPrice{
	"claude-opus-5-5":   {input: 4.00, output: 20.00, cacheRead: 0.20},
	"claude-sonnet-5-5": {input: 2.00, output: 10.00, cacheRead: 0.20},
	"claude-haiku-5-5":  {input: 0.10, output: 0.50, cacheRead: 0.01},
	// Не предлагаются пользователю, но могут ответить как резервные модели.
	"claude-opus-5":   {input: 5.00, output: 25.00, cacheRead: 0.50},
	"claude-opus-4-8": {input: 5.00, output: 25.00, cacheRead: 0.50},
}

const cacheWriteMultiplier = 1.25

type anthropicModel struct {
	id, name string
	fallback bool // поддерживает серверный fallback (у Haiku 5.5 его нет)
}

// anthropicCatalog — модели для выбора; первая — по умолчанию.
var anthropicCatalog = []anthropicModel{
	{id: "claude-opus-5-5", name: "Claude Opus 5.5", fallback: true},
	{id: "claude-sonnet-5-5", name: "Claude Sonnet 5.5", fallback: true},
	{id: "claude-haiku-5-5", name: "Claude Haiku 5.5"},
}

// NewAnthropic создаёт адаптер. opts — для тестов (адрес, HTTP-клиент).
func NewAnthropic(apiKey string, timeout time.Duration, opts ...option.RequestOption) *Anthropic {
	if timeout <= 0 {
		timeout = defaultModelTimeout
	}
	base := []option.RequestOption{
		option.WithAPIKey(apiKey),
		// Повторами с паузами и арендой управляет очередь; повторы SDK
		// умножили бы бюджет попыток.
		option.WithMaxRetries(0),
	}
	return &Anthropic{client: anthropic.NewClient(append(base, opts...)...), apiKey: apiKey, timeout: timeout}
}

func (a *Anthropic) ID() string                { return "anthropic" }
func (a *Anthropic) SendsDataExternally() bool { return true }

func catalogEntry(modelID string) (anthropicModel, bool) {
	for _, m := range anthropicCatalog {
		if m.id == modelID {
			return m, true
		}
	}
	return anthropicModel{}, false
}

func capabilitiesOf(m anthropicModel) ModelCapabilities {
	price := anthropicPrices[m.id]
	return ModelCapabilities{
		ModelID: m.id, DisplayName: m.name,
		ContextTokens: intPtr(1_000_000), MaxOutputTokens: intPtr(128_000),
		SupportsTemperature: false, SupportsSystemPrompt: true,
		InputCostPerMTok: &price.input, OutputCostPerMTok: &price.output,
	}
}

// AnthropicCatalog — модели для выбора с ценами; первая — по умолчанию.
// Каталог статичен, ключ для него не нужен.
func AnthropicCatalog() []ModelCapabilities {
	out := make([]ModelCapabilities, 0, len(anthropicCatalog))
	for _, m := range anthropicCatalog {
		out = append(out, capabilitiesOf(m))
	}
	return out
}

func (a *Anthropic) ListModels(context.Context) ([]ModelCapabilities, error) {
	return AnthropicCatalog(), nil
}

func (a *Anthropic) Capabilities(_ context.Context, modelID string) (ModelCapabilities, error) {
	m, ok := catalogEntry(modelID)
	if !ok {
		return ModelCapabilities{}, fmt.Errorf("%w: %s нет в каталоге", ErrModelNotAvailable, modelID)
	}
	return capabilitiesOf(m), nil
}

func effortFor(d Determinism) anthropic.BetaOutputConfigEffort {
	switch d {
	case Balanced:
		return anthropic.BetaOutputConfigEffortMedium
	case Creative:
		return anthropic.BetaOutputConfigEffortHigh
	default:
		return anthropic.BetaOutputConfigEffortLow
	}
}

func (a *Anthropic) Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error) {
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	params := anthropic.BetaMessageNewParams{
		Model:        anthropic.Model(modelID),
		MaxTokens:    int64(maxTokens),
		Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.User))},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: effortFor(req.Determinism)},
	}
	if req.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{
			Text: req.System, CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		}}
	}
	if m, ok := catalogEntry(modelID); ok && m.fallback {
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}

	callCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	resp, err := a.client.Beta.Messages.New(callCtx, params)
	if err != nil {
		return CompletionResult{}, a.mapError(ctx, callCtx, err)
	}

	// Отказ проверяется ДО чтения content: это HTTP 200, и текст отказа
	// нельзя принять за перевод.
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return CompletionResult{}, &RefusedError{
			Category:    string(resp.StopDetails.Category),
			Explanation: resp.StopDetails.Explanation,
		}
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(tb.Text)
		}
	}
	usage := resp.Usage
	return CompletionResult{
		Text:              text.String(),
		ModelID:           string(resp.Model),
		Truncated:         resp.StopReason == anthropic.BetaStopReasonMaxTokens,
		InputTokens:       intPtr(int(usage.InputTokens)),
		OutputTokens:      intPtr(int(usage.OutputTokens)),
		CachedInputTokens: intPtr(int(usage.CacheReadInputTokens)),
		CostUSD:           cost(resp),
	}, nil
}

// cost считает стоимость по ценам моделей, которые реально отвечали:
// при сработавшем fallback каждый шаг (usage.iterations) — по своей
// модели. Неизвестная цена хотя бы одного шага — nil, а не догадка.
func cost(resp *anthropic.BetaMessage) *float64 {
	type hop struct {
		model                                string
		input, output, cacheRead, cacheWrite int64
	}
	var hops []hop
	for _, it := range resp.Usage.Iterations {
		hops = append(hops, hop{string(it.Model), it.InputTokens, it.OutputTokens, it.CacheReadInputTokens, it.CacheCreationInputTokens})
	}
	if len(hops) == 0 {
		u := resp.Usage
		hops = []hop{{string(resp.Model), u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens}}
	}
	total := 0.0
	for _, h := range hops {
		price, ok := anthropicPrices[h.model]
		if !ok {
			return nil
		}
		total += (float64(h.input)*price.input +
			float64(h.output)*price.output +
			float64(h.cacheRead)*price.cacheRead +
			float64(h.cacheWrite)*price.input*cacheWriteMultiplier) / 1_000_000
	}
	return &total
}

func (a *Anthropic) mapError(parent, ctx context.Context, err error) error {
	scrub := httpCall{secret: a.apiKey}.scrub
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		detail := scrub(truncate(apiErr.Error()))
		if apiErr.RequestID != "" {
			detail += " (request-id " + apiErr.RequestID + ")"
		}
		var header map[string][]string
		if apiErr.Response != nil {
			header = apiErr.Response.Header
		}
		return statusError(apiErr.StatusCode, header, detail)
	}
	return httpCall{secret: a.apiKey}.transportError(parent, ctx, errors.New(scrub(err.Error())))
}

// Health проверяет и доступность API, и валидность ключа (ТЗ §6.3).
func (a *Anthropic) Health(ctx context.Context) Health {
	callCtx, cancel := context.WithTimeout(ctx, healthTimeout*3)
	defer cancel()
	if _, err := a.client.Models.Get(callCtx, anthropicCatalog[len(anthropicCatalog)-1].id, anthropic.ModelGetParams{}); err != nil {
		return Health{Available: false, Detail: a.mapError(ctx, callCtx, err).Error()}
	}
	return Health{Available: true}
}
