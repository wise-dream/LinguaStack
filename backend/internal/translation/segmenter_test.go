package translation_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/translation"
)

func docOf(build func(b *cdm.Builder)) cdm.Document {
	b := cdm.NewBuilder(cdm.BookMeta{Title: "T", SourceFormat: "markdown"})
	build(b)
	return b.Build()
}

func segTexts(segs []translation.SegmentRef) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.Text
	}
	return out
}

func TestOneSegmentPerTranslatableBlock(t *testing.T) {
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("C", 1)
		b.Add(cdm.Paragraph, "Первый.", 0, nil)
		b.Add(cdm.CodeBlock, "print(1)", 0, nil)
		b.Add(cdm.Image, "", 0, map[string]string{"src": "a.png"})
		b.Add(cdm.Paragraph, "Второй.", 0, nil)
	})
	segs := translation.Segment(doc)
	require.Equal(t, []string{"Первый.", "Второй."}, segTexts(segs))
	require.Equal(t, "ch0000/b0003", segs[1].NodeID)
	require.Equal(t, 3, segs[1].Ordinal, "позиция блока в главе")
	require.Equal(t, cdm.ContentHash("Второй."), segs[1].SourceHash)
}

func TestSegmentsFollowReadingOrderAcrossNestedChapters(t *testing.T) {
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("A", 1)
		b.Add(cdm.Heading, "A", 1, nil)
		b.StartChapter("A.1", 2)
		b.Add(cdm.Paragraph, "a1", 0, nil)
		b.StartChapter("B", 1)
		b.Add(cdm.Paragraph, "b", 0, nil)
	})
	segs := translation.Segment(doc)
	require.Equal(t, []string{"A", "a1", "b"}, segTexts(segs))
	require.Equal(t, []int{0, 1, 2}, []int{segs[0].ChapterIndex, segs[1].ChapterIndex, segs[2].ChapterIndex})
	require.Equal(t, 1, segs[0].Level)
}

func TestBlankBlocksSkipped(t *testing.T) {
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("C", 1)
		b.Add(cdm.Paragraph, "   ", 0, nil)
		b.Add(cdm.TableCell, "", 0, nil)
		b.Add(cdm.Paragraph, "Текст.", 0, nil)
	})
	require.Equal(t, []string{"Текст."}, segTexts(translation.Segment(doc)))
}

func manyParagraphs(n int, text func(i int) string) cdm.Document {
	return docOf(func(b *cdm.Builder) {
		b.StartChapter("C", 1)
		for i := range n {
			b.Add(cdm.Paragraph, text(i), 0, nil)
		}
	})
}

func TestBatchesRespectSegmentLimit(t *testing.T) {
	segs := translation.Segment(manyParagraphs(25, func(i int) string { return "Абзац." }))
	batches, oversized := translation.BuildBatches(segs, 10, 10_000)
	require.Empty(t, oversized)
	var sizes []int
	for _, b := range batches {
		sizes = append(sizes, len(b.Segments))
	}
	require.Equal(t, []int{10, 10, 5}, sizes)
}

func TestBatchesRespectCharLimit(t *testing.T) {
	segs := translation.Segment(manyParagraphs(10, func(int) string { return strings.Repeat("x", 400) }))
	batches, oversized := translation.BuildBatches(segs, 100, 1000)
	require.Empty(t, oversized)
	require.Greater(t, len(batches), 1)
	for _, b := range batches {
		total := 0
		for _, s := range b.Segments {
			total += utf8.RuneCountInString(s.Text)
		}
		require.LessOrEqual(t, total, 1000)
	}
}

func TestCharLimitCountsRunesNotBytes(t *testing.T) {
	// Кириллица — 2 байта на символ: лимит в байтах вдвое урезал бы батчи.
	segs := translation.Segment(manyParagraphs(2, func(int) string { return strings.Repeat("я", 400) }))
	batches, oversized := translation.BuildBatches(segs, 100, 800)
	require.Empty(t, oversized)
	require.Len(t, batches, 1, "800 символов кириллицы — ровно в лимите")
}

func TestBatchNeverSpansChapters(t *testing.T) {
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("A", 1)
		b.Add(cdm.Paragraph, "A1.", 0, nil)
		b.Add(cdm.Paragraph, "A2.", 0, nil)
		b.StartChapter("B", 1)
		b.Add(cdm.Paragraph, "B1.", 0, nil)
	})
	batches, oversized := translation.BuildBatches(translation.Segment(doc), 100, 10_000)
	require.Empty(t, oversized)
	require.Len(t, batches, 2)
	for _, b := range batches {
		chapters := map[int]bool{}
		for _, s := range b.Segments {
			chapters[s.ChapterIndex] = true
		}
		require.Len(t, chapters, 1)
	}
}

func TestContextCarriesNeighbours(t *testing.T) {
	segs := translation.Segment(manyParagraphs(3, func(i int) string { return []string{"Нулевой.", "Первый.", "Второй."}[i] }))
	batches, oversized := translation.BuildBatches(segs, 1, 10_000)
	require.Empty(t, oversized)
	require.Empty(t, batches[0].ContextBefore)
	require.Equal(t, "Первый.", batches[0].ContextAfter)
	require.Equal(t, "Нулевой.", batches[1].ContextBefore)
	require.Equal(t, "Второй.", batches[1].ContextAfter)
	require.Empty(t, batches[2].ContextAfter)
}

func TestContextDoesNotCrossChapterBoundary(t *testing.T) {
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("A", 1)
		b.Add(cdm.Paragraph, "Конец главы A.", 0, nil)
		b.StartChapter("B", 1)
		b.Add(cdm.Paragraph, "Начало главы B.", 0, nil)
	})
	batches, oversized := translation.BuildBatches(translation.Segment(doc), 10, 10_000)
	require.Empty(t, oversized)
	require.Empty(t, batches[0].ContextAfter)
	require.Empty(t, batches[1].ContextBefore)
}

func TestContextIsTrimmedToItsEdge(t *testing.T) {
	// Длинный соседний абзац удвоил бы запрос; для связности хватает края.
	long := strings.Repeat("слово ", 1000) + "КОНЕЦ"
	segs := translation.Segment(manyParagraphs(2, func(i int) string { return []string{long, "Следующий."}[i] }))
	batches, oversized := translation.BuildBatches(segs, 1, 10_000)
	require.Empty(t, oversized)
	before := batches[1].ContextBefore
	require.Less(t, utf8.RuneCountInString(before), utf8.RuneCountInString(long))
	require.True(t, strings.HasSuffix(before, "КОНЕЦ"), "сохраняется ближайший к сегменту край")
}

func TestChapterWithOnlyCodeYieldsNoSegments(t *testing.T) {
	// Review Focus №1: глава целиком из кода не должна ломать прогресс.
	doc := docOf(func(b *cdm.Builder) {
		b.StartChapter("C", 1)
		b.Add(cdm.CodeBlock, "print(1)", 0, nil)
		b.Add(cdm.CodeBlock, "print(2)", 0, nil)
	})
	require.Empty(t, translation.Segment(doc))
	batches, oversized := translation.BuildBatches(nil, 10, 1000)
	require.Empty(t, oversized)
	require.Empty(t, batches)
}

func TestOversizedBlockIsNamedButDoesNotStopTheBook(t *testing.T) {
	// Review Focus №3: блок больше лимита — явная ошибка с именем блока, а
	// не молчаливое обрезание. Но остальная книга переводится.
	segs := translation.Segment(manyParagraphs(3, func(i int) string {
		return []string{"До.", strings.Repeat("x", 50_000), "После."}[i]
	}))
	batches, oversized := translation.BuildBatches(segs, 10, 6000)
	require.Len(t, oversized, 1)
	require.Equal(t, "ch0000/b0001", oversized[0].NodeID)
	require.Equal(t, 50_000, oversized[0].Chars)
	require.Equal(t, 6000, oversized[0].Limit)
	require.Contains(t, oversized[0].Error(), "ch0000/b0001")

	var translated []string
	for _, b := range batches {
		translated = append(translated, segTexts(b.Segments)...)
	}
	require.Equal(t, []string{"До.", "После."}, translated)
}
