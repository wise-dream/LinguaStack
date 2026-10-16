package providers

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrUnknownProvider — такого вида провайдера нет.
var ErrUnknownProvider = errors.New("неизвестный провайдер")

// Kind — вид провайдера для интерфейса настройки (ТЗ §8.1, п. 8).
type Kind struct {
	ID                  string `json:"provider_id"`
	Name                string `json:"name"`
	SendsDataExternally bool   `json:"sends_data_externally"`
	RequiresAPIKey      bool   `json:"requires_api_key"`
	RequiresBaseURL     bool   `json:"requires_base_url"`
	// UserBaseURL — адрес задаёт пользователь. У Ollama его задаёт
	// администратор: модель работает на собственном сервере (ТЗ §6.2), а
	// пользовательский адрес открыл бы путь к SSRF.
	UserBaseURL bool `json:"user_base_url"`
}

var kinds = []Kind{
	{ID: "ollama", Name: "Ollama (локальные модели)"},
	{ID: "anthropic", Name: "Anthropic Claude", SendsDataExternally: true, RequiresAPIKey: true},
	{ID: "openai_compatible", Name: "OpenAI-совместимый API", SendsDataExternally: true,
		RequiresBaseURL: true, UserBaseURL: true},
}

// Kinds — поддерживаемые виды провайдеров.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// LookupKind ищет вид по идентификатору.
func LookupKind(id string) (Kind, bool) {
	for _, k := range kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// BuildOptions — серверные настройки, нужные адаптерам.
type BuildOptions struct {
	OllamaBaseURL string
	OllamaNumCtx  int
	Timeout       time.Duration
	// AllowPrivateURLs — администратор разрешил частные адреса для
	// OpenAI-совместимых сервисов (локальный vLLM, LM Studio).
	AllowPrivateURLs bool
}

// Build собирает адаптер. Ключ передаётся уже расшифрованным — он живёт
// в памяти только на время работы адаптера.
func Build(kind, baseURL, apiKey string, opts BuildOptions) (Provider, error) {
	switch kind {
	case "ollama":
		return NewOllama(opts.OllamaBaseURL, &http.Client{}, OllamaOptions{NumCtx: opts.OllamaNumCtx, Timeout: opts.Timeout}), nil
	case "anthropic":
		if apiKey == "" {
			return nil, fmt.Errorf("%w: для Anthropic нужен ключ API", ErrAuth)
		}
		return NewAnthropic(apiKey, opts.Timeout), nil
	case "openai_compatible":
		if err := ValidateBaseURL(baseURL, opts.AllowPrivateURLs); err != nil {
			return nil, err
		}
		return NewOpenAICompatible(baseURL, apiKey, GuardedHTTPClient(opts.AllowPrivateURLs), opts.Timeout), nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, kind)
}

// SendsDataExternally — уходит ли текст с сервера для провайдера этого вида
// с этим адресом (без сетевых запросов).
func SendsDataExternally(kind, baseURL string) bool {
	if kind == "openai_compatible" {
		return NewOpenAICompatible(baseURL, "", nil, 0).SendsDataExternally()
	}
	k, ok := LookupKind(kind)
	return !ok || k.SendsDataExternally
}
