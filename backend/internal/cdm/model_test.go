package cdm_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
)

func meta() cdm.BookMeta {
	return cdm.BookMeta{Title: "Книга", Language: "en", SourceFormat: "markdown"}
}

func nodeIDs(blocks []cdm.Block) []string {
	ids := make([]string, len(blocks))
	for i, b := range blocks {
		ids[i] = b.NodeID
	}
	return ids
}

func TestTranslatableBlocksExcludeCodeAndImages(t *testing.T) {
	b := cdm.NewBuilder(meta())
	b.StartChapter("Глава", 1)
	b.Add(cdm.Paragraph, "Hello", 0, nil)
	b.Add(cdm.CodeBlock, "print(1)", 0, nil)
	b.Add(cdm.Image, "", 0, map[string]string{"src": "x.png"})
	b.Add(cdm.ListItem, "Пункт", 0, nil)
	doc := b.Build()

	var texts []string
	for _, blk := range doc.TranslatableBlocks() {
		texts = append(texts, blk.Text)
	}
	require.Equal(t, []string{"Hello", "Пункт"}, texts)
	require.Len(t, doc.Blocks(), 4, "непереводимые блоки остаются в документе")
}

func TestIsTranslatable(t *testing.T) {
	for _, bt := range []cdm.BlockType{cdm.Heading, cdm.Paragraph, cdm.ListItem, cdm.TableCell, cdm.Caption, cdm.Blockquote} {
		require.True(t, cdm.IsTranslatable(bt), string(bt))
	}
	for _, bt := range []cdm.BlockType{cdm.CodeBlock, cdm.MathBlock, cdm.Image, cdm.ThematicBreak, cdm.RawHTML} {
		require.False(t, cdm.IsTranslatable(bt), string(bt))
	}
}

func TestBuilderAssignsIDsAndHashes(t *testing.T) {
	b := cdm.NewBuilder(meta())
	b.StartChapter("A", 1)
	first := b.Add(cdm.Paragraph, "a1", 0, nil)
	b.Add(cdm.Paragraph, "a2", 0, nil)
	b.StartChapter("B", 1)
	b.Add(cdm.Paragraph, "b1", 0, nil)
	doc := b.Build()

	require.Equal(t, "ch0000/b0000", first.NodeID)
	require.Equal(t, cdm.ContentHash("a1"), first.SourceHash)
	require.Equal(t, []string{"ch0000/b0000", "ch0000/b0001", "ch0001/b0000"}, nodeIDs(doc.Blocks()))
	require.NoError(t, doc.Validate())
}

func TestBuilderNestsChaptersByDepth(t *testing.T) {
	b := cdm.NewBuilder(meta())
	b.StartChapter("Часть 1", 1)
	b.StartChapter("Глава 1.1", 2)
	b.StartChapter("Раздел 1.1.1", 3)
	b.StartChapter("Глава 1.2", 2)
	b.StartChapter("Часть 2", 1)
	doc := b.Build()

	require.Len(t, doc.Chapters, 2)
	require.Equal(t, "Часть 1", doc.Chapters[0].Title)
	require.Equal(t, []string{"Глава 1.1", "Глава 1.2"},
		[]string{doc.Chapters[0].Children[0].Title, doc.Chapters[0].Children[1].Title})
	require.Equal(t, "Раздел 1.1.1", doc.Chapters[0].Children[0].Children[0].Title)

	// Индексы глобальные и в порядке чтения — от них зависят node_id.
	var indexes []int
	var walk func([]cdm.Chapter)
	walk = func(chs []cdm.Chapter) {
		for _, ch := range chs {
			indexes = append(indexes, ch.Index)
			walk(ch.Children)
		}
	}
	walk(doc.Chapters)
	require.Equal(t, []int{0, 1, 2, 3, 4}, indexes)
}

func TestBuilderSkippedDepthStillNests(t *testing.T) {
	// Заголовок h3 сразу после h1 — частый случай в реальных документах.
	b := cdm.NewBuilder(meta())
	b.StartChapter("Верх", 1)
	b.StartChapter("Глубже", 3)
	doc := b.Build()
	require.Len(t, doc.Chapters, 1)
	require.Equal(t, "Глубже", doc.Chapters[0].Children[0].Title)
}

func TestBlocksBeforeFirstChapterGetImplicitChapter(t *testing.T) {
	b := cdm.NewBuilder(meta())
	b.Add(cdm.Paragraph, "Предисловие без заголовка", 0, nil)
	b.StartChapter("Глава 1", 1)
	b.Add(cdm.Paragraph, "Текст", 0, nil)
	doc := b.Build()

	require.Len(t, doc.Chapters, 2)
	require.Equal(t, "Книга", doc.Chapters[0].Title, "неявная глава получает название книги")
	require.Equal(t, "Предисловие без заголовка", doc.Chapters[0].Blocks[0].Text)
}

func TestBlocksFollowReadingOrder(t *testing.T) {
	// Собственные блоки главы идут раньше блоков её подглав.
	b := cdm.NewBuilder(meta())
	b.StartChapter("Глава", 1)
	b.Add(cdm.Paragraph, "intro", 0, nil)
	b.StartChapter("Подглава", 2)
	b.Add(cdm.Paragraph, "body", 0, nil)
	b.StartChapter("Следующая", 1)
	b.Add(cdm.Paragraph, "next", 0, nil)
	doc := b.Build()

	var texts []string
	for _, blk := range doc.Blocks() {
		texts = append(texts, blk.Text)
	}
	require.Equal(t, []string{"intro", "body", "next"}, texts)
}

func TestValidateDetectsDuplicateNodeIDs(t *testing.T) {
	doc := cdm.Document{Meta: meta(), Chapters: []cdm.Chapter{
		{Index: 0, Title: "A", Blocks: []cdm.Block{{NodeID: "ch0000/b0000", Type: cdm.Paragraph}}},
		{Index: 1, Title: "B", Blocks: []cdm.Block{{NodeID: "ch0000/b0000", Type: cdm.Paragraph}}},
	}}
	require.Error(t, doc.Validate())
}

func TestTOCPreservesOrderAndNesting(t *testing.T) {
	b := cdm.NewBuilder(meta())
	b.StartChapter("Часть", 1)
	b.StartChapter("Подглава", 2)
	b.StartChapter("Вторая", 1)
	toc := b.Build().TOC()

	require.Len(t, toc, 2)
	require.Equal(t, "Часть", toc[0].Title)
	require.Equal(t, "Подглава", toc[0].Children[0].Title)
	require.Equal(t, 1, toc[0].Children[0].Index)
	require.Equal(t, "Вторая", toc[1].Title)
}

func TestDocumentJSONRoundTrip(t *testing.T) {
	// CDM хранится в JSONB — сериализация обязана быть без потерь.
	b := cdm.NewBuilder(meta())
	b.StartChapter("Глава", 1)
	b.Add(cdm.Heading, "Глава", 1, nil)
	b.Add(cdm.Image, "", 0, map[string]string{"src": "a.png", "alt": "Схема"})
	b.StartChapter("Пустая подглава", 2)
	doc := b.Build()

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	var back cdm.Document
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, doc, back)
}

func TestEmptyDocument(t *testing.T) {
	doc := cdm.NewBuilder(meta()).Build()
	require.Empty(t, doc.Blocks())
	require.Empty(t, doc.TranslatableBlocks())
	require.Empty(t, doc.TOC())
	require.NoError(t, doc.Validate())
}
