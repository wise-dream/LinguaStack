package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OpenAICompatible — любой сервис с API в форме OpenAI Chat Completions:
// OpenAI, OpenRouter, Groq, DeepSeek, vLLM, LM Studio (ТЗ §6.3). Адрес
// задаётся конфигурацией провайдера.
type OpenAICompatible struct {
	baseURL string
	apiKey  string
	client  *http.Client
	timeout time.Duration

	mu     sync.Mutex
	quirks map[string]openAIQuirks
}

// openAIQuirks — параметры, которые конкретная модель отвергает. Узнаются
// по первому 400 и запоминаются, чтобы не тратить лишний запрос на каждый
// батч: рассуждающие модели OpenAI не принимают temperature и требуют
// max_completion_tokens вместо max_tokens, а vLLM и LM Studio — наоборот.
type openAIQuirks struct {
	noTemperature       bool
	maxCompletionTokens bool
}

// NewOpenAICompatible создаёт адаптер. apiKey может быть пустым (локальные серверы).
func NewOpenAICompatible(baseURL, apiKey string, client *http.Client, timeout time.Duration) *OpenAICompatible {
	if client == nil {
		client = &http.Client{}
	}
	if timeout <= 0 {
		timeout = defaultModelTimeout
	}
	return &OpenAICompatible{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client, timeout: timeout,
		quirks: map[string]openAIQuirks{},
	}
}

func (o *OpenAICompatible) ID() string { return "openai_compatible" }

// SendsDataExternally — текст уходит с сервера, если адрес не loopback.
// Локальный vLLM или LM Studio предупреждения о передаче наружу (ТЗ §6.3)
// не заслуживает: это было бы неправдой.
func (o *OpenAICompatible) SendsDataExternally() bool {
	u, err := url.Parse(o.baseURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func (o *OpenAICompatible) call(method, path string, body any) httpCall {
	headers := map[string]string{}
	if o.apiKey != "" {
		headers["Authorization"] = "Bearer " + o.apiKey
	}
	return httpCall{client: o.client, method: method, url: o.baseURL + path, headers: headers, body: body, secret: o.apiKey}
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        *int `json:"prompt_tokens"`
		CompletionTokens    *int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens *int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

const maxQuirkRetries = 2

func (o *OpenAICompatible) Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error) {
	var messages []openAIMessage
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.User})

	var resp openAIResponse
	for attempt := 0; ; attempt++ {
		quirks := o.quirksFor(modelID)
		body := map[string]any{"model": modelID, "messages": messages}
		if !quirks.noTemperature {
			body["temperature"] = temperature(req.Determinism)
		}
		if req.MaxOutputTokens > 0 {
			if quirks.maxCompletionTokens {
				body["max_completion_tokens"] = req.MaxOutputTokens
			} else {
				body["max_tokens"] = req.MaxOutputTokens
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, o.timeout)
		err := o.call(http.MethodPost, "/chat/completions", body).do(ctx, callCtx, &resp)
		cancel()
		if err == nil {
			break
		}
		if attempt >= maxQuirkRetries || !o.learnQuirk(modelID, err) {
			return CompletionResult{}, err
		}
	}

	if len(resp.Choices) == 0 {
		return CompletionResult{}, fmt.Errorf("%w: ответ без вариантов (choices пуст)", ErrUnavailable)
	}
	choice := resp.Choices[0]
	if choice.FinishReason == "content_filter" {
		return CompletionResult{}, &RefusedError{Category: "content_filter", Explanation: "ответ заблокирован фильтром провайдера"}
	}
	return CompletionResult{
		Text: choice.Message.Content, ModelID: modelID, Truncated: choice.FinishReason == "length",
		InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens,
		CachedInputTokens: resp.Usage.PromptTokensDetails.CachedTokens,
	}, nil
}

func (o *OpenAICompatible) quirksFor(modelID string) openAIQuirks {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.quirks[modelID]
}

// learnQuirk узнаёт по тексту 400, какой параметр модель отвергла.
// Возвращает true, если стоит повторить запрос с исправлением.
func (o *OpenAICompatible) learnQuirk(modelID string, err error) bool {
	if !errors.Is(err, ErrBadRequest) {
		return false
	}
	msg := strings.ToLower(err.Error())
	o.mu.Lock()
	defer o.mu.Unlock()
	q := o.quirks[modelID]
	learned := false
	if strings.Contains(msg, "temperature") && !q.noTemperature {
		q.noTemperature, learned = true, true
	}
	if strings.Contains(msg, "max_tokens") && strings.Contains(msg, "max_completion_tokens") && !q.maxCompletionTokens {
		q.maxCompletionTokens, learned = true, true
	}
	o.quirks[modelID] = q
	return learned
}

func (o *OpenAICompatible) ListModels(ctx context.Context) ([]ModelCapabilities, error) {
	callCtx, cancel := context.WithTimeout(ctx, healthTimeout*10)
	defer cancel()
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := o.call(http.MethodGet, "/models", nil).do(ctx, callCtx, &resp); err != nil {
		return nil, err
	}
	out := make([]ModelCapabilities, 0, len(resp.Data))
	for _, m := range resp.Data {
		out = append(out, ModelCapabilities{ModelID: m.ID, DisplayName: m.ID, SupportsTemperature: true, SupportsSystemPrompt: true})
	}
	return out, nil
}

func (o *OpenAICompatible) Capabilities(_ context.Context, modelID string) (ModelCapabilities, error) {
	// Общий API не сообщает окно и цену модели; неизвестное — nil.
	return ModelCapabilities{
		ModelID: modelID, DisplayName: modelID, SupportsSystemPrompt: true,
		SupportsTemperature: !o.quirksFor(modelID).noTemperature,
	}, nil
}

func (o *OpenAICompatible) Health(ctx context.Context) Health {
	callCtx, cancel := context.WithTimeout(ctx, healthTimeout*3)
	defer cancel()
	if err := o.call(http.MethodGet, "/models", nil).do(ctx, callCtx, nil); err != nil {
		return Health{Available: false, Detail: err.Error()}
	}
	return Health{Available: true}
}
