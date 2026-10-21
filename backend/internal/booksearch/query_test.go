package booksearch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
)

func TestParse(t *testing.T) {
	cases := []struct {
		raw  string
		want Query
	}{
		{"крыша Кошки", Query{Words: []string{"крыша", "кошки"}, Prefix: true}},
		{`"спали на крыше" пёс`, Query{Phrases: []string{"спали на крыше"}, Words: []string{"пёс"}, Prefix: true}},
		{`пёс "спали"`, Query{Phrases: []string{"спали"}, Words: []string{"пёс"}}},
		{"«спали на крыше»", Query{Phrases: []string{"спали на крыше"}}},
		{"fmt.Println(x)", Query{Words: []string{"fmt", "println", "x"}}},
		{`"спали на`, Query{Words: []string{"спали", "на"}, Prefix: true}},
		{"scan, ", Query{Words: []string{"scan"}}},
	}
	for _, c := range cases {
		got := Parse(c.raw)
		c.want.Raw = c.raw
		require.Equal(t, c.want, got, c.raw)
	}
}

func TestParseCJK(t *testing.T) {
	require.True(t, Parse("喜欢").CJK)
	require.True(t, Parse("ひらがな").CJK)
	require.True(t, Parse("한국어").CJK)
	require.False(t, Parse("кошка cat").CJK)
}

func TestOriginalLanguage(t *testing.T) {
	doc := func(texts ...string) *cdm.Document {
		var blocks []cdm.Block
		for _, s := range texts {
			blocks = append(blocks, cdm.Block{Text: s})
		}
		return &cdm.Document{Chapters: []cdm.Chapter{{Blocks: blocks}}}
	}
	russian := doc("Кошки спали на крыше, a cat.")
	require.Equal(t, "de", OriginalLanguage("de-AT", []string{"en"}, russian), "метаданные книги — первыми")
	require.Equal(t, "en", OriginalLanguage("", []string{"auto", "en"}, russian), "затем язык оригинала у перевода")
	require.Equal(t, "ru", OriginalLanguage("", nil, russian), "затем — по алфавиту")
	require.Equal(t, "en", OriginalLanguage("xx", []string{"auto"}, doc("The cat slept.")))
	require.Empty(t, OriginalLanguage("", nil, doc("我喜欢猫。")), "иначе — без словаря форм")
	require.Empty(t, OriginalLanguage("", nil, doc("")))
}

func TestUnmark(t *testing.T) {
	text := "Начало длинного абзаца. Кошки спали на крыше. Конец абзаца."
	snippet, words := unmark("Кошки спали на \ue000крыше\ue001.", text)
	require.Equal(t, "…Кошки спали на крыше.…", snippet, "обрезан с двух сторон")
	require.Equal(t, []string{"крыше"}, words)

	snippet, words = unmark("\ue000Кошки\ue001 и \ue000кошки\ue001, \ue000Кошки\ue001", "Кошки и кошки, Кошки")
	require.Equal(t, "Кошки и кошки, Кошки", snippet, "весь блок — без многоточий")
	require.Equal(t, []string{"Кошки", "кошки"}, words, "без повторов")
}
