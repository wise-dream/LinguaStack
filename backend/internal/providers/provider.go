// Package providers — единый интерфейс к языковым моделям (ТЗ §6).
//
// Модуль перевода не зависит от конкретного API (ТЗ §6.1). Поэтому
// интерфейс выражает намерение (Determinism), а не параметры генерации:
// у Claude Opus/Sonnet 5.5 temperature вызывает HTTP 400, у Ollama и
// OpenAI-совместимых — обязателен. Каждый адаптер сам переводит намерение
// в свои параметры (дополнение §6).
package providers

import "context"

// Determinism — желаемая степень предсказуемости перевода.
type Determinism string

const (
	Strict   Determinism = "strict"
	Balanced Determinism = "balanced"
	Creative Determinism = "creative"
)

// Valid сообщает, известно ли значение.
func (d Determinism) Valid() bool {
	switch d {
	case Strict, Balanced, Creative:
		return true
	}
	return false
}

// ModelCapabilities — возможности модели. nil означает «неизвестно»:
// нельзя считать, что все модели поддерживают одинаковый набор языков
// (ТЗ §5.2), а локальная модель не сообщает стоимость.
type ModelCapabilities struct {
	ModelID              string   `json:"model_id"`
	DisplayName          string   `json:"display_name,omitempty"`
	ContextTokens        *int     `json:"context_tokens,omitempty"`
	MaxOutputTokens      *int     `json:"max_output_tokens,omitempty"`
	SupportsTemperature  bool     `json:"supports_temperature"`
	SupportsSystemPrompt bool     `json:"supports_system_prompt"`
	SupportedLanguages   []string `json:"supported_languages,omitempty"`
	InputCostPerMTok     *float64 `json:"input_cost_per_mtok,omitempty"`
	OutputCostPerMTok    *float64 `json:"output_cost_per_mtok,omitempty"`
}

// CompletionRequest — запрос к модели.
type CompletionRequest struct {
	System          string
	User            string
	Determinism     Determinism
	MaxOutputTokens int // 0 — значение адаптера по умолчанию
}

// CompletionResult — ответ модели и метрики (ТЗ §6.1).
type CompletionResult struct {
	Text              string
	ModelID           string
	InputTokens       *int
	OutputTokens      *int
	CachedInputTokens *int
	CostUSD           *float64
}

// Health — состояние провайдера для интерфейса (ТЗ §6.2).
type Health struct {
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

// Provider — адаптер поставщика моделей.
type Provider interface {
	ID() string
	// SendsDataExternally — текст книги покидает сервер. Интерфейс обязан
	// явно предупреждать об этом пользователя (ТЗ §6.3).
	SendsDataExternally() bool
	ListModels(ctx context.Context) ([]ModelCapabilities, error)
	Health(ctx context.Context) Health
	Capabilities(ctx context.Context, modelID string) (ModelCapabilities, error)
	Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error)
}

func intPtr(v int) *int { return &v }
