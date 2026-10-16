package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Ollama — локальные модели (ТЗ §6.2). Текст книги не покидает сервер.
type Ollama struct {
	baseURL string
	client  *http.Client
	opts    OllamaOptions

	mu   sync.Mutex
	caps map[string]ModelCapabilities
}

// OllamaOptions — настройки адаптера.
type OllamaOptions struct {
	// NumCtx — контекстное окно запроса. Задаётся явно: значение сервера по
	// умолчанию меньше батча, и Ollama молча обрезает запрос.
	NumCtx  int
	Timeout time.Duration
}

const (
	defaultOllamaNumCtx = 8192
	defaultModelTimeout = 10 * time.Minute
	healthTimeout       = 3 * time.Second
)

// NewOllama создаёт адаптер. client == nil — клиент по умолчанию.
func NewOllama(baseURL string, client *http.Client, opts OllamaOptions) *Ollama {
	if client == nil {
		client = &http.Client{}
	}
	if opts.NumCtx <= 0 {
		opts.NumCtx = defaultOllamaNumCtx
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultModelTimeout
	}
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), client: client, opts: opts, caps: map[string]ModelCapabilities{}}
}

func (o *Ollama) ID() string                { return "ollama" }
func (o *Ollama) SendsDataExternally() bool { return false }

func (o *Ollama) call(method, path string, body any) httpCall {
	return httpCall{client: o.client, method: method, url: o.baseURL + path, body: body}
}

// temperature — перевод намерения в параметр Ollama (дополнение §6).
func temperature(d Determinism) float64 {
	switch d {
	case Balanced:
		return 0.3
	case Creative:
		return 0.8
	default:
		return 0.0
	}
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// thinkRe — рассуждения, которые старые версии Ollama кладут прямо в
// content (новые возвращают их отдельным полем).
var thinkRe = regexp.MustCompile(`(?s)^\s*<think>.*?</think>\s*`)

// outputBudget — лимит выхода по умолчанию: 2 токена на символ запроса
// с запасом, но не больше окна. Перевод не длиннее исходника в токенах
// даже для пар вроде китайский → русский, а без лимита зациклившаяся
// модель генерирует бесконечно: Ollama сдвигает окно и продолжает.
func (o *Ollama) outputBudget(user string) int {
	return min(o.opts.NumCtx, 2*utf8.RuneCountInString(user)+256)
}

// ollamaChunk — событие потока /api/chat; последнее несёт done и метрики.
type ollamaChunk struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount *int   `json:"prompt_eval_count"`
	EvalCount       *int   `json:"eval_count"`
	Error           string `json:"error"`
}

// Complete запрашивает перевод потоком: тайм-аут адаптера означает «модель
// молчит дольше Timeout», а не «ответ занял дольше Timeout».
func (o *Ollama) Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error) {
	var messages []ollamaMessage
	if req.System != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: req.User})
	numPredict := req.MaxOutputTokens
	if numPredict <= 0 {
		numPredict = o.outputBudget(req.User)
	}
	options := map[string]any{
		"temperature": temperature(req.Determinism), "num_ctx": o.opts.NumCtx, "num_predict": numPredict,
	}
	body := map[string]any{
		"model": modelID, "messages": messages, "stream": true, "options": options,
		// Рассуждения «думающих» моделей (qwen3) увеличивают выход в десятки
		// раз, а переводу не нужны. Модели без рассуждений поле принимают.
		"think": false,
	}

	var text strings.Builder
	var last ollamaChunk
	err := o.call(http.MethodPost, "/api/chat", body).stream(ctx, o.opts.Timeout, func(line []byte) error {
		var chunk ollamaChunk
		if err := json.Unmarshal(line, &chunk); err != nil {
			return fmt.Errorf("%w: некорректное событие потока: %s", ErrUnavailable, truncate(string(line)))
		}
		if chunk.Error != "" {
			return fmt.Errorf("%w: %s", ErrUnavailable, truncate(chunk.Error))
		}
		text.WriteString(chunk.Message.Content)
		if chunk.Done {
			last = chunk
		}
		return nil
	})
	if err != nil {
		return CompletionResult{}, err
	}
	if !last.Done {
		return CompletionResult{}, fmt.Errorf("%w: поток ответа оборван до завершения", ErrUnavailable)
	}
	return CompletionResult{
		Text:         thinkRe.ReplaceAllString(text.String(), ""),
		ModelID:      modelID,
		Truncated:    last.DoneReason == "length",
		InputTokens:  last.PromptEvalCount,
		OutputTokens: last.EvalCount,
	}, nil
}

func (o *Ollama) ListModels(ctx context.Context) ([]ModelCapabilities, error) {
	callCtx, cancel := context.WithTimeout(ctx, healthTimeout*10)
	defer cancel()
	var resp struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := o.call(http.MethodGet, "/api/tags", nil).do(ctx, callCtx, &resp); err != nil {
		return nil, err
	}
	out := make([]ModelCapabilities, 0, len(resp.Models))
	for _, m := range resp.Models {
		display := m.Name
		if m.Details.ParameterSize != "" {
			display += " (" + m.Details.ParameterSize + ")"
		}
		out = append(out, ModelCapabilities{
			ModelID: m.Name, DisplayName: display, SupportsTemperature: true, SupportsSystemPrompt: true,
		})
	}
	return out, nil
}

// Capabilities читает окно модели из /api/show. Действующее окно — меньшее
// из окна модели и num_ctx: батчер рассчитывает именно на него.
func (o *Ollama) Capabilities(ctx context.Context, modelID string) (ModelCapabilities, error) {
	o.mu.Lock()
	cached, ok := o.caps[modelID]
	o.mu.Unlock()
	if ok {
		return cached, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, healthTimeout*10)
	defer cancel()
	var resp struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := o.call(http.MethodPost, "/api/show", map[string]string{"model": modelID}).do(ctx, callCtx, &resp); err != nil {
		return ModelCapabilities{}, err
	}
	window := o.opts.NumCtx
	for key, value := range resp.ModelInfo {
		if n, ok := value.(float64); ok && strings.HasSuffix(key, ".context_length") && n > 0 && int(n) < window {
			window = int(n)
		}
	}
	caps := ModelCapabilities{
		ModelID: modelID, DisplayName: modelID, ContextTokens: intPtr(window),
		SupportsTemperature: true, SupportsSystemPrompt: true,
	}
	o.mu.Lock()
	o.caps[modelID] = caps
	o.mu.Unlock()
	return caps, nil
}

func (o *Ollama) Health(ctx context.Context) Health {
	callCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	var resp struct {
		Version string `json:"version"`
	}
	if err := o.call(http.MethodGet, "/api/version", nil).do(ctx, callCtx, &resp); err != nil {
		return Health{Available: false, Detail: err.Error()}
	}
	return Health{Available: true, Detail: fmt.Sprintf("Ollama %s", resp.Version)}
}
