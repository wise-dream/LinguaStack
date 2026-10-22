package translation_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/translation"
)

func TestEstimate(t *testing.T) {
	text := strings.Repeat("Alice was beginning to get very tired. ", 25)
	doc := cdm.Document{Chapters: []cdm.Chapter{{Index: 0, Blocks: []cdm.Block{
		{NodeID: "a", Type: cdm.Paragraph, Text: text},
		{NodeID: "code", Type: cdm.CodeBlock, Text: "fmt.Println()"},
		{NodeID: "b", Type: cdm.Paragraph, Text: text},
		{NodeID: "c", Type: cdm.Paragraph, Text: text},
	}}}}
	source := 3 * utf8.RuneCountInString(text)

	ru := translation.Estimate(doc, "en", "ru", 2, 6000)
	require.Equal(t, source, ru.SourceChars, "код не переводится и не считается")
	// Вход — настоящие запросы: по 2 блока в пачке — 2 запроса, инструкция
	// уходит в каждом.
	system := utf8.RuneCountInString(translation.BuildSystemPrompt("en", "ru", "neutral", nil))
	require.GreaterOrEqual(t, ru.InputTokens, (2*system+source)/4)
	require.Less(t, ru.InputTokens, (2*system+source)/2)

	// Выход — по объёму исходника и письменности языка перевода: русский у
	// Claude в токенах в разы дороже английского (дополнение §20).
	en := translation.Estimate(doc, "ru", "en", 2, 6000)
	require.Greater(t, ru.OutputTokens, 3*en.OutputTokens)
	require.Greater(t, ru.OutputTokens, source)
}
