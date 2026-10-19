package importers_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

// TestRealGutenbergAlice — настоящий EPUB с Project Gutenberg (см.
// testdata/real/README.md). Проверяются структурные свойства, а не точные
// числа: так тест переживает правки импорта, но не потерю текста.
func TestRealGutenbergAlice(t *testing.T) {
	data, err := os.ReadFile("testdata/real/alice.epub")
	require.NoError(t, err)
	res := loadEPUB(t, data)
	doc := res.Document

	require.Equal(t, "Alice's Adventures in Wonderland", doc.Meta.Title)
	require.Equal(t, "Lewis Carroll", doc.Meta.Author)
	require.Equal(t, "en", doc.Meta.Language)
	require.Nil(t, issueWith(res.Issues, "toc_missing"))

	var story []cdm.Chapter
	for _, ch := range doc.Chapters {
		if strings.HasPrefix(ch.Title, "CHAPTER ") {
			story = append(story, ch)
		}
	}
	require.Len(t, story, 12, "двенадцать глав повести")
	require.Equal(t, "CHAPTER I. Down the Rabbit-Hole", story[0].Title)
	for _, ch := range story {
		// В каждой главе Алисы — десятки абзацев; склейка давала один.
		require.Greater(t, len(ch.Blocks), 20, "глава %q", ch.Title)
		require.Equal(t, cdm.Heading, ch.Blocks[0].Type, "глава %q начинается с заголовка", ch.Title)
		for _, b := range ch.Blocks {
			require.Less(t, len(b.Text), 5000, "подозрительно длинный блок %s в %q", b.NodeID, ch.Title)
		}
	}
	require.Greater(t, len(doc.TranslatableBlocks()), 700, "в исходнике ~780 абзацев")

	first := story[0].Blocks[1]
	require.True(t, strings.HasPrefix(first.Text, "Alice was beginning to get very tired"), "первый абзац: %q", first.Text)
	require.Equal(t, "Cover", doc.Chapters[0].Title, "обложка названа по <title>, а не «wrap0000»")
}

// TestRealGutenbergAliceHTML — та же книга в HTML-версии Project Gutenberg
// (см. testdata/real/README.md): один h1 с названием, главы в h2,
// служебные шапка и подвал Gutenberg — <header>/<footer> страницы.
func TestRealGutenbergAliceHTML(t *testing.T) {
	data, err := os.ReadFile("testdata/real/alice.html")
	require.NoError(t, err)
	res, err := importers.NewHTML().Load(data, "pg11-images.html")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	doc := res.Document

	require.Equal(t, "Alice's Adventures in Wonderland", doc.Meta.Title)
	require.Equal(t, "Carroll, Lewis, 1832-1898", doc.Meta.Author)
	require.Equal(t, "en", doc.Meta.Language)

	var story []cdm.Chapter
	for _, ch := range doc.Chapters {
		if strings.HasPrefix(ch.Title, "CHAPTER ") {
			story = append(story, ch)
		}
	}
	require.Len(t, story, 12, "двенадцать глав повести — на верхнем уровне")
	require.Equal(t, "CHAPTER I. Down the Rabbit-Hole", story[0].Title)
	for _, ch := range story {
		require.Greater(t, len(ch.Blocks), 20, "глава %q", ch.Title)
	}
	first := story[0].Blocks[1]
	require.True(t, strings.HasPrefix(first.Text, "Alice was beginning to get very tired"), "первый абзац: %q", first.Text)
	for _, b := range doc.Blocks() {
		require.NotContains(t, b.Text, "Project Gutenberg License", "служебный подвал Gutenberg не импортируется")
	}
	require.NotNil(t, issueWith(res.Issues, "missing_resource"), "обложка лежит рядом с HTML, а не в нём")
}
