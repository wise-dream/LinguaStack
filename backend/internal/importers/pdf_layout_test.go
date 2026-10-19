package importers_test

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/pdftext"
)

// Синтетические страницы A4 (595×842 пт): колонка 72–523, основной кегль 10.

type fakePDF struct {
	doc *pdftext.Document
	err error
}

func (f fakePDF) Extract([]byte) (*pdftext.Document, error) { return f.doc, f.err }

const (
	colLeft  = 72.0
	colRight = 523.0
)

type lineOpts struct {
	size               float64
	x0                 float64
	full               bool    // строка до правого края колонки
	gap                float64 // дополнительный зазор перед строкой
	bold, italic, mono bool
	y                  float64 // явная базовая линия (колонтитулы)
}

type pageBuilder struct {
	page pdftext.Page
	y    float64
}

func newPage(number int) *pageBuilder {
	return &pageBuilder{page: pdftext.Page{Number: number, Width: 595, Height: 842}, y: 770}
}

func (p *pageBuilder) line(text string, o lineOpts) *pageBuilder {
	if o.size == 0 {
		o.size = 10
	}
	if o.x0 == 0 {
		o.x0 = colLeft
	}
	x1 := o.x0 + 0.5*o.size*float64(utf8.RuneCountInString(text))
	if o.full {
		x1 = colRight
	}
	p.y -= o.gap
	y := p.y
	if o.y != 0 {
		y = o.y
	} else {
		p.y -= o.size * 1.25
	}
	p.page.Lines = append(p.page.Lines, pdftext.Line{
		Spans: []pdftext.Span{{Text: text, Size: o.size, Bold: o.bold, Italic: o.italic, Mono: o.mono}},
		X0:    o.x0, X1: x1, Top: y + o.size*0.75, Bottom: y,
	})
	return p
}

// para — абзац: все строки, кроме последней, во всю ширину колонки.
func (p *pageBuilder) para(gap float64, lines ...string) *pageBuilder {
	for i, l := range lines {
		o := lineOpts{full: i < len(lines)-1}
		if i == 0 {
			o.gap = gap
		}
		p.line(l, o)
	}
	return p
}

func (p *pageBuilder) spans(spans []pdftext.Span, o lineOpts) *pageBuilder {
	p.line("", o)
	l := &p.page.Lines[len(p.page.Lines)-1]
	l.Spans = spans
	l.X1 = colRight
	return p
}

func loadFakePDF(t *testing.T, doc *pdftext.Document) importers.Result {
	t.Helper()
	res, err := importers.NewPDF(fakePDF{doc: doc}).Load([]byte("%PDF-1.7"), "book.pdf")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func pdfOf(pages ...*pageBuilder) *pdftext.Document {
	doc := &pdftext.Document{}
	for _, p := range pages {
		doc.Pages = append(doc.Pages, p.page)
	}
	return doc
}

func TestPDFLinesJoinIntoParagraph(t *testing.T) {
	res := loadFakePDF(t, pdfOf(newPage(1).para(0, "Первая строка абзаца,", "вторая строка", "и третья.")))
	require.Equal(t, []string{"Первая строка абзаца, вторая строка и третья."}, texts(res.Document.Blocks()))
}

func TestPDFSoftHyphenJoins(t *testing.T) {
	res := loadFakePDF(t, pdfOf(newPage(1).
		para(0, "Это интернацио\x02", "нализация и PDF\x02", "документ, well-", "known.")))
	require.Equal(t, []string{"Это интернационализация и PDF-документ, well-known."}, texts(res.Document.Blocks()),
		"перенос в слове снимается; дефис после заглавной и обычный дефис остаются")
}

func TestPDFParagraphBreakByGap(t *testing.T) {
	res := loadFakePDF(t, pdfOf(newPage(1).para(0, "Первый абзац во всю", "ширину.").para(8, "Второй абзац.")))
	require.Equal(t, []string{"Первый абзац во всю ширину.", "Второй абзац."}, texts(res.Document.Blocks()))
}

func TestPDFParagraphBreakByIndent(t *testing.T) {
	// Книжная вёрстка: без зазоров, абзац — отступ первой строки.
	p := newPage(1).
		line("Первый абзац, первая строка во всю ширину", lineOpts{x0: colLeft + 15, full: true}).
		line("и его продолжение, тоже во всю ширину колонки", lineOpts{full: true}).
		line("Второй абзац с отступом первой строки", lineOpts{x0: colLeft + 15, full: true}).
		line("и продолжение.", lineOpts{})
	res := loadFakePDF(t, pdfOf(p))
	require.Equal(t, []string{
		"Первый абзац, первая строка во всю ширину и его продолжение, тоже во всю ширину колонки",
		"Второй абзац с отступом первой строки и продолжение.",
	}, texts(res.Document.Blocks()))
}

func TestPDFParagraphBreakShortLine(t *testing.T) {
	// Строка кончилась задолго до края, а следующее слово поместилось бы —
	// перенос был намеренным (конец абзаца или <br>).
	p := newPage(1).
		line("Пункт один", lineOpts{x0: colLeft + 30}).
		line("Пункт два", lineOpts{x0: colLeft + 30}).
		line("Строка во всю ширину, после которой", lineOpts{full: true}).
		line("продолжение того же абзаца.", lineOpts{})
	res := loadFakePDF(t, pdfOf(p))
	require.Equal(t, []string{"Пункт один", "Пункт два", "Строка во всю ширину, после которой продолжение того же абзаца."},
		texts(res.Document.Blocks()))
}

func TestPDFHeadingBySize(t *testing.T) {
	p := newPage(1).
		line("Часть первая", lineOpts{size: 24}).
		line("Глава 1", lineOpts{size: 18, gap: 10}).
		para(10, "Текст главы во всю ширину колонки", "и дальше.").
		line("Раздел 1.1", lineOpts{size: 14, gap: 10}).
		para(10, "Текст раздела.")
	res := loadFakePDF(t, pdfOf(p))
	var headings []cdm.Block
	for _, b := range res.Document.Blocks() {
		if b.Type == cdm.Heading {
			headings = append(headings, b)
		}
	}
	require.Equal(t, []string{"Часть первая", "Глава 1", "Раздел 1.1"}, texts(headings))
	require.Equal(t, []int{1, 2, 3}, []int{headings[0].Level, headings[1].Level, headings[2].Level})
}

func TestPDFBoldLineHeading(t *testing.T) {
	p := newPage(1).
		line("3. Персональные данные", lineOpts{bold: true}).
		para(4, "Компания принимает все меры для защиты данных.").
		spans([]pdftext.Span{{Text: "Внимание: ", Size: 10, Bold: true}, {Text: "жирное начало абзаца не заголовок", Size: 10}},
			lineOpts{gap: 8})
	res := loadFakePDF(t, pdfOf(p))
	blocks := res.Document.Blocks()
	require.Equal(t, cdm.Heading, blocks[0].Type)
	require.Equal(t, "3. Персональные данные", blocks[0].Text)
	require.Equal(t, cdm.Paragraph, blocks[2].Type)
	require.Equal(t, "**Внимание:** жирное начало абзаца не заголовок", blocks[2].Text)
}

func TestPDFInlineSpans(t *testing.T) {
	p := newPage(1).spans([]pdftext.Span{
		{Text: "Обычный ", Size: 10}, {Text: "курсив", Size: 10, Italic: true}, {Text: " и ", Size: 10},
		{Text: "run()", Size: 10, Mono: true}, {Text: " и 2*3.", Size: 10},
	}, lineOpts{})
	res := loadFakePDF(t, pdfOf(p))
	require.Equal(t, []string{"Обычный *курсив* и `run()` и 2\\*3."}, texts(res.Document.Blocks()))
}

func TestPDFHeadersFooters(t *testing.T) {
	var pages []*pageBuilder
	for i := 1; i <= 5; i++ {
		p := newPage(i)
		if i != 3 {
			p.line("Название книги — Глава 2", lineOpts{size: 8, y: 810})
		}
		p.line([]string{"", "1", "2", "iii", "— 4 —", "Page 5"}[i], lineOpts{size: 8, y: 30})
		p.para(0, "Текст страницы во всю ширину колонки", "и конец.")
		if i == 2 {
			p.para(10, "Название книги — Глава 2")
		}
		pages = append(pages, p)
	}
	res := loadFakePDF(t, pdfOf(pages...))
	got := texts(res.Document.Blocks())
	page := "Текст страницы во всю ширину колонки и конец."
	require.Equal(t, []string{page, page, "Название книги — Глава 2", page, page, page}, got,
		"колонтитул и номера страниц удалены; тот же текст посреди страницы — нет")
}

func TestPDFFewPagesKeepRepeatedText(t *testing.T) {
	p1 := newPage(1).line("Шапка", lineOpts{size: 8, y: 810}).para(0, "Один.")
	p2 := newPage(2).line("Шапка", lineOpts{size: 8, y: 810}).para(0, "Два.")
	res := loadFakePDF(t, pdfOf(p1, p2))
	require.Contains(t, texts(res.Document.Blocks()), "Шапка", "на двух страницах повтор — не доказательство колонтитула")
}

func TestPDFListItems(t *testing.T) {
	p := newPage(1).
		para(0, "Перед списком.").
		line("• первый пункт", lineOpts{gap: 4}).
		line("◦ вложенный", lineOpts{x0: colLeft + 20}).
		line("1) нумерованный", lineOpts{}).
		line("2. второй", lineOpts{}).
		para(8, "После списка.")
	res := loadFakePDF(t, pdfOf(p))
	blocks := res.Document.Blocks()
	require.Equal(t, []string{"Перед списком.", "первый пункт", "вложенный", "нумерованный", "второй", "После списка."}, texts(blocks))
	require.Equal(t, cdm.ListItem, blocks[1].Type)
	require.Equal(t, map[string]string{"depth": "1", "page": "1"}, blocks[1].Meta)
	require.Equal(t, map[string]string{"depth": "2", "page": "1"}, blocks[2].Meta)
	require.Equal(t, map[string]string{"depth": "1", "ordered": "true", "page": "1"}, blocks[3].Meta)
	require.Equal(t, cdm.ListItem, blocks[4].Type)
}

func TestPDFDialogueDashIsNotList(t *testing.T) {
	// В русской прозе тире открывает реплику, а не пункт списка.
	p := newPage(1).para(0, "— Привет, — сказал он.").para(8, "– Здравствуй.")
	blocks := loadFakePDF(t, pdfOf(p)).Document.Blocks()
	require.Equal(t, []string{"— Привет, — сказал он.", "– Здравствуй."}, texts(blocks))
	require.Equal(t, cdm.Paragraph, blocks[0].Type)
	require.Equal(t, cdm.Paragraph, blocks[1].Type)
}

func TestPDFMonoLinesCodeBlock(t *testing.T) {
	p := newPage(1).
		para(0, "До кода.").
		line("func main() {", lineOpts{mono: true, size: 9, gap: 6}).
		line(` fmt.Println("*x*")`, lineOpts{mono: true, size: 9, x0: colLeft + 4*0.5*9}).
		line("", lineOpts{mono: true, size: 9}).
		line("}", lineOpts{mono: true, size: 9}).
		para(8, "После кода.")
	// Пустая строка не попадает в Lines — её место показывает зазор.
	lines := p.page.Lines[:0]
	for _, l := range p.page.Lines {
		if l.Text() != "" {
			lines = append(lines, l)
		}
	}
	p.page.Lines = lines
	res := loadFakePDF(t, pdfOf(p))
	blocks := res.Document.Blocks()
	require.Len(t, blocks, 3)
	require.Equal(t, cdm.CodeBlock, blocks[1].Type)
	require.Equal(t, "func main() {\n    fmt.Println(\"*x*\")\n\n}", blocks[1].Text)
}

func TestPDFChaptersFromOutline(t *testing.T) {
	p1 := newPage(1).para(0, "Титульная страница.")
	p2 := newPage(2).para(0, "Конец введения на странице главы.").
		line("Глава 1", lineOpts{size: 18, gap: 20}).para(8, "Текст главы 1.")
	p3 := newPage(3).para(0, "Продолжение главы 1.").line("1.1 Раздел", lineOpts{size: 14, gap: 12}).para(6, "Текст раздела.")
	p4 := newPage(4).line("Глава 2", lineOpts{size: 18}).para(8, "Текст главы 2.")
	doc := pdfOf(p1, p2, p3, p4)
	doc.Title = "Книга"
	doc.Outline = []pdftext.OutlineItem{
		{Title: "Введение", Page: 1},
		{Title: "Глава 1", Page: 2, Children: []pdftext.OutlineItem{{Title: "1.1 Раздел", Page: 3}}},
		{Title: "Глава 2", Page: 4},
		{Title: "Без страницы"},
	}
	res := loadFakePDF(t, doc)
	chs := res.Document.Chapters
	require.Equal(t, []string{"Введение", "Глава 1", "Глава 2"}, chapterTitles(chs))
	require.Equal(t, []string{"Титульная страница.", "Конец введения на странице главы."}, texts(chs[0].Blocks))
	require.Equal(t, []string{"Глава 1", "Текст главы 1.", "Продолжение главы 1."}, texts(chs[1].Blocks))
	require.Equal(t, cdm.Heading, chs[1].Blocks[0].Type)
	require.Equal(t, []string{"1.1 Раздел"}, chapterTitles(chs[1].Children))
	require.Equal(t, []string{"1.1 Раздел", "Текст раздела."}, texts(chs[1].Children[0].Blocks))
}

func TestPDFChaptersFromHeadings(t *testing.T) {
	p := newPage(1).para(0, "До глав.").
		line("Глава 1", lineOpts{size: 18, gap: 10}).para(6, "Один.").
		line("Раздел", lineOpts{size: 14, gap: 10}).para(6, "Вложенный.").
		line("Глава 2", lineOpts{size: 18, gap: 10}).para(6, "Два.")
	doc := pdfOf(p)
	doc.Title = "Книга"
	res := loadFakePDF(t, doc)
	chs := res.Document.Chapters
	require.Equal(t, []string{"Книга", "Глава 1", "Глава 2"}, chapterTitles(chs), "текст до первой главы — глава с названием книги")
	require.Equal(t, []string{"Раздел"}, chapterTitles(chs[1].Children))
}

func TestPDFPageMeta(t *testing.T) {
	res := loadFakePDF(t, pdfOf(newPage(1).para(0, "Один."), newPage(2).para(0, "Два.")))
	blocks := res.Document.Blocks()
	require.Equal(t, "1", blocks[0].Meta["page"])
	require.Equal(t, "2", blocks[1].Meta["page"])
}

func TestPDFParagraphContinuesAcrossPages(t *testing.T) {
	p1 := newPage(1).para(0, "Абзац начинается на одной странице и", "продолжается во всю ширину без точки")
	p1.page.Lines[1].X1 = colRight
	p2 := newPage(2).para(0, "на следующей странице.")
	res := loadFakePDF(t, pdfOf(p1, p2))
	require.Equal(t, []string{"Абзац начинается на одной странице и продолжается во всю ширину без точки на следующей странице."},
		texts(res.Document.Blocks()))
}

func TestPDFScanned(t *testing.T) {
	scan := func(n int) *pageBuilder {
		p := newPage(n)
		p.page.Images = 1
		return p
	}
	res := loadFakePDF(t, pdfOf(scan(1), scan(2)))
	issue := issueWith(res.Issues, "scanned_pdf")
	require.NotNil(t, issue)
	require.Equal(t, importers.Error, issue.Severity)
	require.Nil(t, issueWith(res.Issues, "empty_document"), "одной причины достаточно")

	text := newPage(1).para(0, "Текст.")
	text.page.Images = 2
	res = loadFakePDF(t, pdfOf(text, scan(2), newPage(3).para(0, "Ещё.")))
	issue = issueWith(res.Issues, "scanned_pages")
	require.NotNil(t, issue)
	require.Equal(t, importers.Warning, issue.Severity)
	require.Contains(t, issue.Message, "2")
	require.NotNil(t, issueWith(res.Issues, "pdf_images_skipped"))
}

func TestPDFTitle(t *testing.T) {
	doc := pdfOf(newPage(1).line("Годовой отчёт", lineOpts{size: 20}).para(10, "Текст отчёта: основного кегля в документе больше всего."))
	doc.Title = "Microsoft Word - отчёт.docx"
	require.Equal(t, "Годовой отчёт", loadFakePDF(t, doc).Document.Meta.Title)

	doc.Title = "Настоящее название"
	doc.Author = "Автор"
	meta := loadFakePDF(t, doc).Document.Meta
	require.Equal(t, "Настоящее название", meta.Title)
	require.Equal(t, "Автор", meta.Author)
	require.Equal(t, "pdf", meta.SourceFormat)

	require.Equal(t, "book", loadFakePDF(t, pdfOf(newPage(1).para(0, "Без заголовков."))).Document.Meta.Title)
}

func TestPDFOutlineMatchesNumberedHeading(t *testing.T) {
	// LaTeX: в закладке «Overview», в тексте — «1 Overview».
	p1 := newPage(1).para(0, "Титул книги во всю ширину колонки,", "и конец.")
	p2 := newPage(2).line("1 Overview", lineOpts{size: 16}).para(8, "Текст обзора во всю ширину колонки,", "и конец.").
		line("1.1 Components", lineOpts{size: 13, gap: 8}).para(8, "Текст раздела.")
	doc := pdfOf(p1, p2)
	doc.Outline = []pdftext.OutlineItem{{Title: "Overview", Page: 2, Children: []pdftext.OutlineItem{{Title: "Components", Page: 2}}}}
	chs := loadFakePDF(t, doc).Document.Chapters
	require.Equal(t, []string{"book", "Overview"}, chapterTitles(chs))
	require.Equal(t, []string{"1 Overview", "Текст обзора во всю ширину колонки, и конец."}, texts(chs[1].Blocks))
	require.Equal(t, []string{"1.1 Components", "Текст раздела."}, texts(chs[1].Children[0].Blocks))
}

func TestPDFHangingIndentContinues(t *testing.T) {
	// Список определений: термин слева, продолжение с отступом.
	p := newPage(1).
		line("Vec Provides the vector operations required for setting up", lineOpts{full: true}).
		line("problems and includes parallel scatter and gather operations", lineOpts{x0: colLeft + 20, full: true}).
		line("for regular data structures.", lineOpts{x0: colLeft + 20}).
		line("Mat A large suite of data structures for sparse matrices", lineOpts{full: true, gap: 4}).
		line("and four parallel matrix formats.", lineOpts{x0: colLeft + 20})
	res := loadFakePDF(t, pdfOf(p))
	require.Equal(t, []string{
		"Vec Provides the vector operations required for setting up problems and includes parallel scatter and gather operations for regular data structures.",
		"Mat A large suite of data structures for sparse matrices and four parallel matrix formats.",
	}, texts(res.Document.Blocks()))
}

func TestPDFPrintedTOCLinesDropped(t *testing.T) {
	p := newPage(1).
		line("Contents", lineOpts{size: 16}).
		line("1.1 Components . . . . . . . . . . . . . . . 2", lineOpts{gap: 6, full: true}).
		line("2.1 Using pip.................................3", lineOpts{full: true}).
		para(8, "Обычный абзац с точкой в конце... и числом 3")
	res := loadFakePDF(t, pdfOf(p))
	require.Equal(t, []string{"Contents", "Обычный абзац с точкой в конце... и числом 3"}, texts(res.Document.Blocks()))
}

// FuzzPDFLayout — произвольные строки, координаты и кегли не роняют разбор
// вёрстки, а документ остаётся корректным CDM.
func FuzzPDFLayout(f *testing.F) {
	f.Add([]byte("Глава\x00\x10текст абзаца\x02продолжение\x05• пункт"), uint8(3))
	f.Add([]byte{0, 0, 0, 0}, uint8(1))
	f.Fuzz(func(t *testing.T, data []byte, pages uint8) {
		doc := &pdftext.Document{Outline: []pdftext.OutlineItem{{Title: string(data[:min(len(data), 8)]), Page: 1}}}
		for p := 1; p <= int(pages%5)+1; p++ {
			page := pdftext.Page{Number: p, Width: 595, Height: 842}
			for i := 0; i+3 < len(data); i += 4 {
				size := float64(data[i]%30) + 1
				top := float64(data[i+1]) * 3.3
				x0 := float64(data[i+2]) * 2
				text := string(data[i : i+4])
				page.Lines = append(page.Lines, pdftext.Line{
					Spans: []pdftext.Span{{Text: text, Size: size, Bold: data[i+3]%2 == 0, Mono: data[i+3]%5 == 0}},
					X0:    x0, X1: x0 + float64(data[i+3]), Top: top, Bottom: top - size,
				})
			}
			doc.Pages = append(doc.Pages, page)
		}
		res, err := importers.NewPDF(fakePDF{doc: doc}).Load(nil, "f.pdf")
		require.NoError(t, err)
		require.NoError(t, res.Document.Validate())
		for _, b := range res.Document.Blocks() {
			require.True(t, utf8.ValidString(b.Text))
		}
	})
}
