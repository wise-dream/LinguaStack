package translation

import (
	"math"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
)

// BookEstimate — примерный объём перевода книги в токенах: по нему
// интерфейс показывает цену до запуска (дополнение §20).
type BookEstimate struct {
	SourceChars  int `json:"source_chars"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Коэффициенты сняты с беты 2026-10-22 (Claude Haiku 5.5, en→ru): запрос —
// английская инструкция и текст, около 4 символов на токен; русский ответ
// — около 1,4 токена на символ, а символов в нём примерно столько же,
// сколько в исходнике.
const promptCharsPerToken = 4.0

// Estimate считает вход по настоящим запросам — те же пачки, инструкция в
// каждой, соседний текст для связности, — а выход по объёму исходника и
// письменности языка перевода. Повторы при сбоях не учитываются: это
// нижняя оценка. Блоки длиннее пачки не переводятся и не считаются.
func Estimate(doc cdm.Document, src, tgt string, maxSegments, maxChars int) BookEstimate {
	batches, _ := BuildBatches(Segment(doc), maxSegments, maxChars)
	system := utf8.RuneCountInString(BuildSystemPrompt(src, tgt, "neutral", nil))
	var e BookEstimate
	prompt := 0
	for _, b := range batches {
		prompt += system + utf8.RuneCountInString(BuildUserPrompt(b))
		for _, s := range b.Segments {
			e.SourceChars += utf8.RuneCountInString(s.Text)
		}
	}
	e.InputTokens = int(math.Ceil(float64(prompt) / promptCharsPerToken))
	e.OutputTokens = int(math.Ceil(float64(e.SourceChars) * outputTokensPerSourceChar(tgt)))
	return e
}

// outputTokensPerSourceChar — токенов ответа на символ исходника по
// письменности языка перевода. Кириллица и прочие нелатинские алфавиты в
// токенах у Claude в разы дороже латиницы; иероглифы короче по символам.
func outputTokensPerSourceChar(tgt string) float64 {
	switch tgt {
	case "ru", "uk", "bg", "sr", "kk":
		return 1.45
	case "zh", "ja", "ko":
		return 0.6
	case "el", "hy", "ka", "he", "ar", "fa", "hi", "th":
		return 1.5
	default:
		return 0.32
	}
}
