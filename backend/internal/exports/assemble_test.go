package exports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exporters"
	"github.com/vnovels/linguastack/internal/exports"
)

func sampleDoc() cdm.Document {
	b := cdm.NewBuilder(cdm.BookMeta{Title: "Alice", Author: "Carroll", SourceFormat: "epub"})
	b.StartChapter("Alice", 1)
	b.Add(cdm.Heading, "Alice", 1, nil)
	b.StartChapter("Chapter 1", 1)
	b.Add(cdm.Heading, "Chapter *1*", 1, nil)
	b.Add(cdm.Paragraph, "One.", 0, nil)
	b.Add(cdm.CodeBlock, "x := 1", 0, nil)
	b.Add(cdm.Image, "", 0, map[string]string{"src": "a.png"})
	b.Add(cdm.Paragraph, "Two.", 0, nil)
	b.StartChapter("Nav label", 1)
	b.Add(cdm.Heading, "Different heading", 1, nil)
	b.Add(cdm.Paragraph, "Three.", 0, nil)
	return b.Build()
}

func seg(nodeID, text string) models.TranslationSegment {
	return models.TranslationSegment{NodeID: nodeID, TranslatedText: &text, Status: models.SegmentCompleted}
}

func sampleSegments(doc cdm.Document) []models.TranslationSegment {
	blocks := doc.Blocks()
	empty := ""
	return []models.TranslationSegment{
		seg(blocks[0].NodeID, "Алиса"),
		seg(blocks[1].NodeID, "Глава *1*"),
		seg(blocks[2].NodeID, "Один."),
		// blocks[5] «Two.» — сегмент упал, перевода нет
		{NodeID: blocks[5].NodeID, Status: models.SegmentFailed},
		seg(blocks[6].NodeID, "Другой заголовок"),
		{NodeID: blocks[7].NodeID, TranslatedText: &empty, Status: models.SegmentCompleted},
	}
}

func TestAssembleTranslationMode(t *testing.T) {
	doc := sampleDoc()
	book, untranslated := exports.Assemble(doc, "epub", sampleSegments(doc), exporters.ModeTranslation, "en", "ru")
	require.Equal(t, 2, untranslated, "«Two.» упал, «Three.» пустой")
	require.Equal(t, "Алиса", book.Title, "первый заголовок совпадает с названием книги")
	require.Equal(t, "Carroll", book.Author)
	require.Equal(t, "ru", book.Lang)
	require.Equal(t, "en", book.SourceLang)
	require.Equal(t, exporters.ModeTranslation, book.Mode)
	require.True(t, book.HardBreaks)

	ch := book.Chapters[1]
	require.Equal(t, "Глава 1", ch.Title, "название главы — перевод её первого заголовка без разметки")
	require.Equal(t, "Глава *1*", ch.Blocks[0].Text())
	require.True(t, ch.Blocks[1].Translated)
	require.Equal(t, "One.", ch.Blocks[1].Original)
	require.False(t, ch.Blocks[2].Translated, "код не переводится")
	require.Equal(t, "x := 1", ch.Blocks[2].Text())
	require.Equal(t, "a.png", ch.Blocks[3].Meta["src"])
	require.Equal(t, "Two.", ch.Blocks[4].Text(), "непереведённый — оригиналом")
	require.Equal(t, "Nav label", book.Chapters[2].Title, "заголовок не совпадает с названием главы — название не трогаем")
}

func TestAssembleKeepsTOCOrder(t *testing.T) {
	doc := sampleDoc()
	doc.Chapters[1], doc.Chapters[2] = doc.Chapters[2], doc.Chapters[1]
	book, _ := exports.Assemble(doc, "epub", sampleSegments(doc), exporters.ModeBilingual, "en", "ru")
	require.Equal(t, []int{0, 2, 1}, []int{book.Chapters[0].Index, book.Chapters[1].Index, book.Chapters[2].Index})
	require.Equal(t, exporters.ModeBilingual, book.Mode)
}

func TestAssembleHardBreaks(t *testing.T) {
	doc := sampleDoc()
	for format, hard := range map[string]bool{"markdown": false, "zip_repo": false, "epub": true, "html": true,
		"docx": true, "pdf": true, "txt": true} {
		book, _ := exports.Assemble(doc, format, nil, exporters.ModeTranslation, "en", "ru")
		require.Equal(t, hard, book.HardBreaks, format)
	}
}

func TestAssembleWithoutSegments(t *testing.T) {
	doc := sampleDoc()
	book, untranslated := exports.Assemble(doc, "epub", nil, exporters.ModeTranslation, "en", "ru")
	require.Equal(t, len(doc.TranslatableBlocks()), untranslated)
	require.Equal(t, "Alice", book.Title)
}
