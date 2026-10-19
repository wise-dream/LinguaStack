package providers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fake — детерминированный провайдер для тестов (дополнение §14): все
// тесты очереди, валидации, повторов и деградации проходят без сети.
//
// Конверт <<<SEG n>>> Fake разбирает собственной реализацией, а не
// парсером пакета перевода: тестовый двойник не должен разделять ошибки
// с тем кодом, который им проверяют.
type Fake struct {
	// Scenario: echo, prefix, protocol_violation, drops_placeholder,
	// rate_limited, timeout, refusal, empty.
	Scenario string
	Prefix   string // для prefix и protocol_violation после восстановления

	FailTimes      int             // первые N вызовов — ErrUnavailable
	FailAfterCalls int             // после N успешных вызовов — ErrUnavailable (0 — выключено)
	ViolateTimes   int             // protocol_violation: сколько раз ломать конверт
	FailForTexts   map[string]bool // сегмент с таким текстом ломает протокол всего ответа
	RefuseForTexts map[string]bool // сегмент с таким текстом вызывает отказ по политике
	TruncateTimes  int             // первые N ответов на конверт помечены как оборванные
	CostPerCall    float64         // стоимость каждого успешного вызова
	TokensPerCall  int             // токенов входа и выхода у каждого успешного вызова
	Unavailable    bool            // всегда ErrUnavailable
	// TimeoutOverSegments > 0 — конверт больше N сегментов «не успевает»:
	// тайм-аут адаптера (медленная локальная модель).
	TimeoutOverSegments int

	mu         sync.Mutex
	calls      []CompletionRequest
	failed     int
	succeeded  int
	violated   int
	truncated  int
	translated int
}

// NewFake создаёт тестовый провайдер со сценарием.
func NewFake(scenario string) *Fake {
	return &Fake{Scenario: scenario, Prefix: "RU:"}
}

var fakeSegRe = regexp.MustCompile(`(?m)^<<<SEG (\d+)>>>[ \t]*$`)

type fakeSegment struct {
	n    int
	text string
}

// parseRequest выделяет сегменты из запроса; контекст до первого маркера
// и всё после <<<END>>> игнорируются.
func parseRequest(user string) ([]fakeSegment, bool) {
	locs := fakeSegRe.FindAllStringSubmatchIndex(user, -1)
	if len(locs) == 0 {
		return nil, false
	}
	out := make([]fakeSegment, 0, len(locs))
	for i, loc := range locs {
		n, _ := strconv.Atoi(user[loc[2]:loc[3]])
		end := len(user)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		} else if j := strings.Index(user[loc[1]:], "<<<END>>>"); j >= 0 {
			end = loc[1] + j
		}
		out = append(out, fakeSegment{n: n, text: strings.Trim(user[loc[1]:end], "\n")})
	}
	return out, true
}

func render(segs []fakeSegment) string {
	var b strings.Builder
	for _, s := range segs {
		fmt.Fprintf(&b, "<<<SEG %d>>>\n%s\n", s.n, s.text)
	}
	b.WriteString("<<<END>>>")
	return b.String()
}

var fakeMarkerRe = regexp.MustCompile(`⟦\d+⟧`)

func (f *Fake) Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error) {
	res, err := f.complete(ctx, modelID, req)
	if err == nil && f.TokensPerCall > 0 {
		res.InputTokens, res.OutputTokens = intPtr(f.TokensPerCall), intPtr(f.TokensPerCall)
	}
	return res, err
}

func (f *Fake) complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	switch {
	case f.Unavailable:
		f.mu.Unlock()
		return CompletionResult{}, fmt.Errorf("%w: fake всегда недоступен", ErrUnavailable)
	case f.failed < f.FailTimes:
		f.failed++
		f.mu.Unlock()
		return CompletionResult{}, fmt.Errorf("%w: fake сбой %d", ErrUnavailable, f.failed)
	case f.FailAfterCalls > 0 && f.succeeded >= f.FailAfterCalls:
		f.mu.Unlock()
		return CompletionResult{}, fmt.Errorf("%w: fake упал после %d вызовов", ErrUnavailable, f.succeeded)
	}
	f.mu.Unlock()

	switch f.Scenario {
	case "rate_limited":
		return CompletionResult{}, &RateLimitedError{RetryAfter: time.Second}
	case "refusal":
		return CompletionResult{}, &RefusedError{Category: "cyber", Explanation: "fake отказ"}
	case "timeout":
		if _, ok := ctx.Deadline(); ok {
			<-ctx.Done()
			return CompletionResult{}, ctx.Err()
		}
		return CompletionResult{}, fmt.Errorf("%w: %w", ErrUnavailable, context.DeadlineExceeded)
	}

	segs, isEnvelope := parseRequest(req.User)
	if !isEnvelope {
		f.mu.Lock()
		f.succeeded++
		f.mu.Unlock()
		text := req.User
		if f.Scenario == "prefix" {
			text = f.Prefix + text
		}
		return CompletionResult{Text: text, ModelID: modelID}, nil
	}

	if f.TimeoutOverSegments > 0 && len(segs) > f.TimeoutOverSegments {
		return CompletionResult{}, fmt.Errorf("%w: fake тайм-аут на %d сегментах: %w", ErrUnavailable, len(segs), context.DeadlineExceeded)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range segs {
		if f.RefuseForTexts[s.text] {
			return CompletionResult{}, &RefusedError{Category: "violence", Explanation: "fake отказ на сегменте"}
		}
	}
	f.succeeded++
	var cost *float64
	if f.CostPerCall > 0 {
		c := f.CostPerCall
		cost = &c
	}
	if f.truncated < f.TruncateTimes {
		f.truncated++
		return CompletionResult{Text: render(segs[:len(segs)-1]), ModelID: modelID, Truncated: true, CostUSD: cost}, nil
	}
	for _, s := range segs {
		if f.FailForTexts[s.text] {
			// Ответ без этого сегмента: сервис увидит нарушение протокола.
			return CompletionResult{Text: render(without(segs, s.n)), ModelID: modelID, CostUSD: cost}, nil
		}
	}
	if f.Scenario == "protocol_violation" && f.violated < f.ViolateTimes {
		f.violated++
		return CompletionResult{Text: "Вот перевод:\n" + render(segs[:len(segs)-1]), ModelID: modelID, CostUSD: cost}, nil
	}

	out := make([]fakeSegment, len(segs))
	for i, s := range segs {
		text := s.text
		switch f.Scenario {
		case "prefix", "protocol_violation":
			text = f.Prefix + text
		case "drops_placeholder":
			text = f.Prefix + fakeMarkerRe.ReplaceAllString(text, "")
		case "empty":
			text = ""
		}
		out[i] = fakeSegment{n: s.n, text: text}
	}
	if f.Scenario != "empty" {
		f.translated += len(segs)
	}
	return CompletionResult{
		Text: render(out), ModelID: modelID, CostUSD: cost,
		InputTokens: intPtr(len(req.User)), OutputTokens: intPtr(len(segs)),
	}, nil
}

func without(segs []fakeSegment, n int) []fakeSegment {
	out := make([]fakeSegment, 0, len(segs))
	for _, s := range segs {
		if s.n != n {
			out = append(out, s)
		}
	}
	return out
}

// Calls — копия записанных запросов.
func (f *Fake) Calls() []CompletionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]CompletionRequest(nil), f.calls...)
}

// CallCount — число вызовов Complete.
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// TranslatedCount — сколько сегментов переведено успешно.
func (f *Fake) TranslatedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.translated
}

// SetUnavailable включает и выключает постоянную недоступность.
func (f *Fake) SetUnavailable(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Unavailable = v
}

// SetFailAfterCalls задаёт падение после N успешных вызовов (0 — выключить)
// и сбрасывает счётчик.
func (f *Fake) SetFailAfterCalls(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FailAfterCalls = n
	f.succeeded = 0
}

func (f *Fake) ID() string                { return "fake" }
func (f *Fake) SendsDataExternally() bool { return false }

func (f *Fake) Health(context.Context) Health { return Health{Available: true} }

var fakeModel = ModelCapabilities{
	ModelID: "fake-1", DisplayName: "Fake", SupportsTemperature: true, SupportsSystemPrompt: true,
	ContextTokens: intPtr(32000), MaxOutputTokens: intPtr(8000),
}

func (f *Fake) ListModels(context.Context) ([]ModelCapabilities, error) {
	return []ModelCapabilities{fakeModel}, nil
}

func (f *Fake) Capabilities(_ context.Context, modelID string) (ModelCapabilities, error) {
	if modelID != fakeModel.ModelID {
		return ModelCapabilities{}, fmt.Errorf("%w: %s", ErrModelNotAvailable, modelID)
	}
	return fakeModel, nil
}
