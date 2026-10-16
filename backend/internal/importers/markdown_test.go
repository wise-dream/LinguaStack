package importers_test

import (
	"os"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func loadMD(t *testing.T, src string) importers.Result {
	t.Helper()
	res, err := importers.NewMarkdown().Load([]byte(src), "book.md")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func loadSample(t *testing.T) cdm.Document {
	t.Helper()
	raw, err := os.ReadFile("testdata/sample.md")
	require.NoError(t, err)
	return loadMD(t, string(raw)).Document
}

func blocksOf(doc cdm.Document, bt cdm.BlockType) []cdm.Block {
	var out []cdm.Block
	for _, b := range doc.Blocks() {
		if b.Type == bt {
			out = append(out, b)
		}
	}
	return out
}

func TestMarkdownChaptersSplitOnHeadings(t *testing.T) {
	doc := loadSample(t)
	require.Len(t, doc.Chapters, 2)
	require.Equal(t, "Глава первая", doc.Chapters[0].Title, "в оглавлении — текст без разметки")
	require.Equal(t, "Глава вторая", doc.Chapters[1].Title)
}

func TestMarkdownSubsectionBecomesChild(t *testing.T) {
	doc := loadSample(t)
	require.Len(t, doc.Chapters[0].Children, 1)
	require.Equal(t, "Раздел 1.1", doc.Chapters[0].Children[0].Title)
}

func TestMarkdownHeadingKeepsMarkupAndLevel(t *testing.T) {
	headings := blocksOf(loadSample(t), cdm.Heading)
	require.Equal(t, "Глава *первая*", headings[0].Text, "блок хранит исходный Markdown")
	require.Equal(t, 1, headings[0].Level)
	require.Equal(t, 2, headings[1].Level)
}

func TestMarkdownInlineMarkupPreserved(t *testing.T) {
	paragraphs := blocksOf(loadSample(t), cdm.Paragraph)
	require.Equal(t, "Вступительный **абзац** со ссылкой на [документацию](https://example.com).", paragraphs[0].Text)
}

func TestMarkdownCodeBlockPresentButNotTranslatable(t *testing.T) {
	doc := loadSample(t)
	code := blocksOf(doc, cdm.CodeBlock)
	require.Len(t, code, 1)
	require.Contains(t, code[0].Text, `print("этот код не переводится")`)
	require.Equal(t, "python", code[0].Meta["language"])
	for _, b := range doc.TranslatableBlocks() {
		require.NotEqual(t, cdm.CodeBlock, b.Type)
	}
}

func TestMarkdownListItemsAreSeparateBlocks(t *testing.T) {
	items := blocksOf(loadSample(t), cdm.ListItem)
	require.Equal(t, []string{"первый пункт", "второй пункт", "вложенный пункт"}, texts(items))
	require.Equal(t, "1", items[0].Meta["depth"])
	require.Equal(t, "2", items[2].Meta["depth"], "вложенность сохраняется для экспорта")
}

func TestMarkdownOrderedListMarked(t *testing.T) {
	items := blocksOf(loadMD(t, "1. раз\n2. два\n").Document, cdm.ListItem)
	require.Len(t, items, 2)
	require.Equal(t, "true", items[0].Meta["ordered"])
}

func TestMarkdownTableCellsAreBlocks(t *testing.T) {
	cells := blocksOf(loadSample(t), cdm.TableCell)
	require.Equal(t, []string{"Колонка", "Значение", "a **b**", "c"}, texts(cells))
	require.Equal(t, "true", cells[0].Meta["header"])
	require.Equal(t, "1", cells[2].Meta["row"])
	require.Equal(t, "1", cells[3].Meta["col"])
}

func TestMarkdownBlockquote(t *testing.T) {
	quotes := blocksOf(loadSample(t), cdm.Blockquote)
	require.Equal(t, []string{"Цитата в разделе."}, texts(quotes))
}

func TestMarkdownStandaloneImageBecomesImageBlock(t *testing.T) {
	images := blocksOf(loadSample(t), cdm.Image)
	require.Len(t, images, 1)
	require.Equal(t, "img/d.png", images[0].Meta["src"])
	require.Equal(t, "диаграмма", images[0].Meta["alt"])
}

func TestMarkdownThematicBreakAndHTMLNotTranslatable(t *testing.T) {
	doc := loadMD(t, "Текст.\n\n---\n\n<div>сырой html</div>\n").Document
	require.Len(t, blocksOf(doc, cdm.ThematicBreak), 1)
	require.Len(t, blocksOf(doc, cdm.RawHTML), 1)
	require.Equal(t, []string{"Текст."}, texts(doc.TranslatableBlocks()))
}

func TestMarkdownMathBlockDetected(t *testing.T) {
	doc := loadMD(t, "Формула:\n\n$$\nE = mc^2\n$$\n").Document
	require.Len(t, blocksOf(doc, cdm.MathBlock), 1)
	require.Equal(t, []string{"Формула:"}, texts(doc.TranslatableBlocks()))
}

func TestMarkdownWithoutHeadingsYieldsOneChapter(t *testing.T) {
	doc := loadMD(t, "Просто абзац.\n\nИ ещё один.").Document
	require.Len(t, doc.Chapters, 1)
	require.Len(t, doc.Chapters[0].Blocks, 2)
}

func TestMarkdownTextBeforeFirstHeadingKept(t *testing.T) {
	doc := loadMD(t, "Предисловие.\n\n# Глава\n\nТекст.").Document
	require.Len(t, doc.Chapters, 2)
	require.Equal(t, "Предисловие.", doc.Chapters[0].Blocks[0].Text)
}

func TestMarkdownSetextHeading(t *testing.T) {
	doc := loadMD(t, "Заголовок\n=========\n\nТекст.").Document
	require.Equal(t, "Заголовок", doc.Chapters[0].Title)
}

func TestMarkdownFrontMatterStripped(t *testing.T) {
	// Без обработки «title: …» + «---» стал бы заголовком второго уровня
	// и фальшивой главой.
	res := loadMD(t, "---\ntitle: Настоящее название\nauthor: Автор\n---\n\n# Глава\n\nТекст.")
	require.Equal(t, "Настоящее название", res.Document.Meta.Title)
	require.Equal(t, "Автор", res.Document.Meta.Author)
	require.Len(t, res.Document.Chapters, 1)
	require.Equal(t, "Глава", res.Document.Chapters[0].Title)
	for _, b := range res.Document.Blocks() {
		require.NotContains(t, b.Text, "title:")
	}
}

func TestMarkdownMultilineParagraphKeepsLines(t *testing.T) {
	doc := loadMD(t, "Строка один\nстрока два.").Document
	require.Equal(t, "Строка один\nстрока два.", doc.Blocks()[0].Text)
}

func TestMarkdownEmptyIsIssue(t *testing.T) {
	res := loadMD(t, "   \n\n")
	require.Contains(t, issueCodes(res.Issues), "empty_document")
}

func TestMarkdownOnlyCodeIsWarnedNotError(t *testing.T) {
	// Review Focus №1 со стороны импорта: книга из одного кода законна,
	// но пользователь должен знать, что переводить нечего.
	res := loadMD(t, "```go\nfunc main() {}\n```\n")
	require.Contains(t, issueCodes(res.Issues), "nothing_to_translate")
	require.NotContains(t, issueCodes(res.Issues), "empty_document")
}

func TestMarkdownInvalidUTF8Sanitized(t *testing.T) {
	res, err := importers.NewMarkdown().Load([]byte("Текст \xff\xfe с мусором."), "b.md")
	require.NoError(t, err)
	require.Contains(t, issueCodes(res.Issues), "invalid_utf8")
	require.Contains(t, res.Document.Blocks()[0].Text, "с мусором")
}

func TestMarkdownTitleFromFirstHeadingWhenNoFrontMatter(t *testing.T) {
	res := loadMD(t, "# Моя книга\n\nТекст.")
	require.Equal(t, "Моя книга", res.Document.Meta.Title)
}

func TestPickMarkdown(t *testing.T) {
	for _, name := range []string{"a.md", "a.markdown", "A.MD"} {
		imp, err := defaultRegistry(t).Pick([]byte("# x"), name)
		require.NoError(t, err)
		require.Equal(t, "markdown", imp.FormatID(), name)
	}
}

func FuzzMarkdownLoad(f *testing.F) {
	raw, err := os.ReadFile("testdata/sample.md")
	if err == nil {
		f.Add(string(raw))
	}
	for _, seed := range []string{
		"# A\n## B\n### C\n# D", "---\ntitle: x\n---\n# y", "- a\n  - b\n    - c",
		"| a |\n|---|\n| b |", "> q\n> - l\n> ```\n> c\n> ```", "![i](a.png)", "$$\nx\n$$",
		"Текст \xff\xfe", "\r\n# CRLF\r\n\r\nтекст\r\n", "####### не заголовок", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		res, err := importers.NewMarkdown().Load([]byte(src), "fuzz.md")
		if err != nil {
			t.Fatalf("Markdown не должен отказывать в загрузке: %v", err)
		}
		if err := res.Document.Validate(); err != nil {
			t.Fatalf("документ нарушает инварианты: %v", err)
		}
		for _, b := range res.Document.Blocks() {
			if !utf8.ValidString(b.Text) {
				t.Fatalf("невалидный UTF-8 в блоке %s: %q", b.NodeID, b.Text)
			}
		}
		if !utf8.ValidString(res.Document.Meta.Title) {
			t.Fatalf("невалидный UTF-8 в названии: %q", res.Document.Meta.Title)
		}
	})
}
