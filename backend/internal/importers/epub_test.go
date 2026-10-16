package importers_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/storage"
)

func loadEPUB(t *testing.T, data []byte) importers.Result {
	t.Helper()
	res, err := importers.NewEPUB(testLimits).Load(data, "book.epub")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func TestEPUBChaptersAndMetadata(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "Книга", author: "Автор", language: "ru", chapters: []epubChapter{
		{title: "Глава 1", body: "<h1>Глава 1</h1><p>Первый абзац.</p>"},
		{title: "Глава 2", body: "<h1>Глава 2</h1><p>Второй абзац.</p>"},
	}}))
	meta := res.Document.Meta
	require.Equal(t, "Книга", meta.Title)
	require.Equal(t, "Автор", meta.Author)
	require.Equal(t, "ru", meta.Language)
	require.Equal(t, "epub", meta.SourceFormat)
	require.Equal(t, []string{"Глава 1", "Глава 2"}, chapterTitles(res.Document.Chapters))
}

func TestEPUBHeadingIsBlockNotExtraChapter(t *testing.T) {
	// Глава задаётся оглавлением; <h1> внутри файла — её заголовок, а не
	// ещё одна глава.
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "Глава", body: "<h1>Глава</h1><h2>Подраздел</h2><p>Текст.</p>"},
	}}))
	require.Len(t, res.Document.Chapters, 1)
	headings := blocksOf(res.Document, cdm.Heading)
	require.Equal(t, []string{"Глава", "Подраздел"}, texts(headings))
	require.Equal(t, 2, headings[1].Level)
}

func TestEPUBParagraphsTranslatable(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: "<p>Абзац один.</p>\n  <p>Абзац   два\n  с переносом.</p>"},
	}}))
	require.Equal(t, []string{"Абзац один.", "Абзац два с переносом."}, texts(res.Document.TranslatableBlocks()),
		"пробелы HTML схлопываются, как в браузере")
}

func TestEPUBInlineMarkupBecomesMarkdown(t *testing.T) {
	// Единый встроенный синтаксис CDM — Markdown: маскирование и проверка
	// разметки работают одинаково для всех форматов.
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<p>Это <em>курсив</em>, <strong>жирный</strong>, <code>код()</code> и <a href="ch2.xhtml#n1">ссылка</a>.</p>`},
	}}))
	require.Equal(t, "Это *курсив*, **жирный**, `код()` и [ссылка](ch2.xhtml#n1).",
		res.Document.TranslatableBlocks()[0].Text)
}

func TestEPUBEscapesMarkdownSpecialChars(t *testing.T) {
	// Иначе «2*3*4» при экспорте превратилось бы в курсив.
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: "<p>2*3*4 и snake_case и [скобки]</p>"},
	}}))
	require.Equal(t, `2\*3\*4 и snake\_case и \[скобки\]`, res.Document.TranslatableBlocks()[0].Text)
}

func TestEPUBDecodesHTMLEntities(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: "<p>&laquo;Текст&raquo; &amp; &#8212; тире</p>"},
	}}))
	require.Equal(t, "«Текст» & — тире", res.Document.TranslatableBlocks()[0].Text)
}

func TestEPUBPreIsCodeBlock(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: "<pre><code>if x {\n    print(1)\n}</code></pre>"},
	}}))
	code := blocksOf(res.Document, cdm.CodeBlock)
	require.Len(t, code, 1)
	require.Equal(t, "if x {\n    print(1)\n}", code[0].Text, "в коде пробелы и переносы значимы")
}

func TestEPUBListsAndTables(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<ol><li>раз</li><li>два<ul><li>вложенный</li></ul></li></ol>
<table><tr><th>Имя</th><th>Значение</th></tr><tr><td>a</td><td>b</td></tr></table>`},
	}}))
	items := blocksOf(res.Document, cdm.ListItem)
	require.Equal(t, []string{"раз", "два", "вложенный"}, texts(items))
	require.Equal(t, "true", items[0].Meta["ordered"])
	require.Equal(t, "2", items[2].Meta["depth"])

	cells := blocksOf(res.Document, cdm.TableCell)
	require.Equal(t, []string{"Имя", "Значение", "a", "b"}, texts(cells))
	require.Equal(t, "true", cells[0].Meta["header"])
	require.Equal(t, "1", cells[3].Meta["row"])
}

func TestEPUBBlockquoteAndCaption(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<blockquote><p>Цитата.</p></blockquote><figure><img src="a.png" alt="А"/><figcaption>Подпись</figcaption></figure>`},
	}, extra: map[string]string{"OEBPS/a.png": "\x89PNG"}}))
	require.Equal(t, []string{"Цитата."}, texts(blocksOf(res.Document, cdm.Blockquote)))
	require.Equal(t, []string{"Подпись"}, texts(blocksOf(res.Document, cdm.Caption)))
}

func TestEPUBImagesRecordSourceAltAndAssets(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<p><img src="images/x.png" alt="Схема"/></p>`},
	}, extra: map[string]string{"OEBPS/images/x.png": "\x89PNG\r\n\x1a\n"}}))
	images := blocksOf(res.Document, cdm.Image)
	require.Len(t, images, 1)
	require.Equal(t, "OEBPS/images/x.png", images[0].Meta["src"], "путь от корня книги")
	require.Equal(t, "Схема", images[0].Meta["alt"])
	require.Len(t, res.Assets, 1)
	require.Equal(t, "OEBPS/images/x.png", res.Assets[0].Name)
}

func TestEPUBMissingImageIsIssue(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<p>Текст</p><p><img src="gone.png" alt=""/></p>`},
	}}))
	missing := issueWith(res.Issues, "missing_resource")
	require.NotNil(t, missing)
	require.Contains(t, missing.Message, "gone.png")
}

func TestEPUBNestedTOCBecomesNesting(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "Часть I", body: "<h1>Часть I</h1>", depth: 1},
		{title: "Глава 1", body: "<p>а</p>", depth: 2},
		{title: "Глава 2", body: "<p>б</p>", depth: 2},
		{title: "Часть II", body: "<h1>Часть II</h1>", depth: 1},
	}}))
	require.Equal(t, []string{"Часть I", "Часть II"}, chapterTitles(res.Document.Chapters))
	require.Equal(t, []string{"Глава 1", "Глава 2"}, chapterTitles(res.Document.Chapters[0].Children))
}

func TestEPUB3NavPreferredOverNCX(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "Из NCX", body: "<p>а</p>"},
	}, nav: map[string]string{"ch1.xhtml": "Из nav"}}))
	require.Equal(t, []string{"Из nav"}, chapterTitles(res.Document.Chapters))
}

func TestEPUBSpineIsReadingOrder(t *testing.T) {
	// Оглавление даёт названия, а порядок чтения задаёт spine.
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "Первая по TOC", body: "<p>а</p>"},
		{title: "Вторая по TOC", body: "<p>б</p>"},
	}, spine: []string{"ch2.xhtml", "ch1.xhtml"}}))
	require.Equal(t, []string{"Вторая по TOC", "Первая по TOC"}, chapterTitles(res.Document.Chapters))
}

func TestEPUBWithoutTOCFallsBackToSpine(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", noTOC: true, chapters: []epubChapter{
		{body: "<h1>Начало</h1><p>а</p>"},
		{body: "<p>Без заголовка.</p>"},
	}}))
	require.Equal(t, []string{"Начало", "ch2"}, chapterTitles(res.Document.Chapters),
		"название — из первого заголовка, иначе из имени файла")
	require.NotNil(t, issueWith(res.Issues, "toc_missing"))
}

func TestEPUBURLEncodedHrefs(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{file: "chapter%201.xhtml", title: "Глава с пробелом", body: "<p>Текст.</p>"},
	}}))
	require.Equal(t, []string{"Глава с пробелом"}, chapterTitles(res.Document.Chapters))
	require.Equal(t, []string{"Текст."}, texts(res.Document.TranslatableBlocks()))
}

func TestEPUBSkipsScriptStyleAndHead(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "Заголовок окна", chapters: []epubChapter{
		{title: "C", body: `<script>alert("x")</script><p>Текст.</p><style>p{}</style>`},
	}}))
	require.Equal(t, []string{"Текст."}, texts(res.Document.Blocks()))
}

func TestEPUBInlineTextInDivBecomesParagraph(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<div>Текст прямо в div.<p>Абзац.</p>Хвост.</div>`},
	}}))
	require.Equal(t, []string{"Текст прямо в div.", "Абзац.", "Хвост."}, texts(res.Document.Blocks()))
}

func TestEPUBSelfClosingAnchorDoesNotSwallowText(t *testing.T) {
	// <a id="page12"/> — частая метка страницы. HTML5-разбор считает её
	// незакрытой ссылкой; текст после неё не должен пропасть или стать ссылкой.
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: `<p><a id="page12"/>Текст после метки.</p><p>Следующий абзац.</p>`},
	}}))
	require.Equal(t, []string{"Текст после метки.", "Следующий абзац."}, texts(res.Document.TranslatableBlocks()))
}

func TestCorruptEPUBIsIssueNotPanic(t *testing.T) {
	res, err := importers.NewEPUB(testLimits).Load([]byte("not an epub at all"), "b.epub")
	require.NoError(t, err)
	corrupt := issueWith(res.Issues, "corrupt_file")
	require.NotNil(t, corrupt)
	require.Equal(t, importers.Error, corrupt.Severity)
}

func TestEPUBWithoutContainerIsCorrupt(t *testing.T) {
	res, err := importers.NewEPUB(testLimits).Load(makeZip(t, files("mimetype", "application/epub+zip")...), "b.epub")
	require.NoError(t, err)
	require.NotNil(t, issueWith(res.Issues, "corrupt_file"))
}

func TestEPUBRejectsZipSlip(t *testing.T) {
	data := buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{{title: "C", body: "<p>x</p>"}},
		extra: map[string]string{"../evil.txt": "x"}})
	_, err := importers.NewEPUB(testLimits).Load(data, "b.epub")
	require.ErrorIs(t, err, storage.ErrUnsafePath)
}

func TestEPUBTextIsValidUTF8(t *testing.T) {
	res := loadEPUB(t, buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{
		{title: "C", body: "<p>Битые \xff\xfe байты.</p>"},
	}}))
	for _, b := range res.Document.Blocks() {
		require.True(t, utf8.ValidString(b.Text))
	}
}

func TestPickEPUBByExtensionAndByContent(t *testing.T) {
	data := buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{{title: "C", body: "<p>x</p>"}}})
	for _, name := range []string{"book.epub", "book.zip"} {
		imp, err := defaultRegistry(t).Pick(data, name)
		require.NoError(t, err)
		require.Equal(t, "epub", imp.FormatID(), "файл %s: EPUB — это тоже ZIP, но формат определяет mimetype", name)
	}
}

// FuzzEPUBChapterHTML подаёт произвольную разметку главы внутри корректного
// EPUB: мутации самого ZIP почти всегда ломают архив и до разбора HTML не
// доходят.
func FuzzEPUBChapterHTML(f *testing.F) {
	for _, seed := range []string{
		"<p>Текст <em>курсив</em> <a href='x.xhtml'>ссылка</a></p>",
		"<ol><li>а<ul><li>б</li></ul></li></ol>", "<table><tr><th>a</th><td><p>b</p><p>c</p></td></tr></table>",
		"<pre><code class='language-go'>x</code></pre>", "<a id='p'/>текст<p>абзац</p>",
		"<figure><svg><image xlink:href='c.jpg'/></svg><figcaption>п</figcaption></figure>",
		"<math display='block'><mi>x</mi></math>", "<div>a<div>b<p>c</p>d</div></div>", "<em> </em><strong></strong>",
		"2*3_4`5[6]\\", "<script>x</script><style>y</style>", "<h7>не заголовок</h7>", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		data := buildEPUB(t, epubOptions{title: "T", chapters: []epubChapter{{title: "C", body: body}}})
		res, err := importers.NewEPUB(testLimits).Load(data, "fuzz.epub")
		if err != nil {
			t.Fatalf("корректный EPUB отклонён: %v", err)
		}
		if err := res.Document.Validate(); err != nil {
			t.Fatalf("документ нарушает инварианты: %v", err)
		}
		for _, b := range res.Document.Blocks() {
			if !utf8.ValidString(b.Text) {
				t.Fatalf("невалидный UTF-8 в %s", b.NodeID)
			}
			if b.Type != cdm.MathBlock && b.Type != cdm.CodeBlock && strings.Contains(strings.ToLower(b.Text), "<script") {
				t.Fatalf("содержимое <script> попало в текст: %q", b.Text)
			}
		}
	})
}
