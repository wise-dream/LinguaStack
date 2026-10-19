package importers

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/pdftext"
)

// PDF — импорт PDF с текстовым слоем (ТЗ §3.1: текст, порядок страниц,
// доступные структурные элементы; дополнение §15.1).
//
// Текст и шрифты извлекает pdftext (PDFium); абзацы, заголовки, списки и
// код восстанавливаются по вёрстке (pdf_layout.go), главы — по закладкам,
// а без них — по заголовкам. Сканы без текстового слоя распознаются, но
// не читаются: OCR — отдельный модуль после MVP.
type PDF struct {
	x pdftext.Extractor
}

// NewPDF создаёт импортёр PDF.
func NewPDF(x pdftext.Extractor) Importer { return PDF{x: x} }

func (PDF) FormatID() string { return "pdf" }

// Sniff: по спецификации заголовок «%PDF-» может стоять не в самом начале,
// а в первых 1024 байтах.
func (PDF) Sniff(data []byte, filename string) bool {
	return fileExt(filename) == ".pdf" || bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-"))
}

func (p PDF) Load(data []byte, filename string) (Result, error) {
	meta := cdm.BookMeta{Title: titleFromFilename(filename), SourceFormat: "pdf"}
	fatal := func(code, message string) (Result, error) {
		// Одной причины достаточно: «Документ пуст» рядом ничего не добавляет.
		return Result{Document: cdm.NewBuilder(meta).Build(),
			Issues: []Issue{{Severity: Error, Code: code, Message: message}}}, nil
	}
	doc, err := p.x.Extract(data)
	switch {
	case errors.Is(err, pdftext.ErrTooManyPages):
		return Result{}, fmt.Errorf("%w: %w", ErrDocumentLimits, err)
	case errors.Is(err, pdftext.ErrEncrypted):
		return fatal("encrypted", "PDF защищён паролем. Снимите защиту и загрузите файл снова.")
	case errors.Is(err, pdftext.ErrTimeout):
		return fatal("extract_timeout", "PDF не удалось разобрать за отведённое время: документ слишком сложный или повреждён.")
	case errors.Is(err, pdftext.ErrCorrupt):
		return fatal("corrupt_file", "Не удалось прочитать файл PDF: он повреждён.")
	case err != nil:
		return Result{}, err
	}

	var issues []Issue
	var scanned []string
	imagesOnText := 0
	for _, page := range doc.Pages {
		switch {
		case len(page.Lines) == 0 && page.Images > 0:
			scanned = append(scanned, strconv.Itoa(page.Number))
		case len(page.Lines) > 0:
			imagesOnText += page.Images
		}
	}
	if len(doc.Pages) > 0 && len(scanned) == len(doc.Pages) {
		return fatal("scanned_pdf", "В PDF нет текстового слоя — это сканы страниц. Распознавание текста (OCR) пока не поддерживается.")
	}
	if len(scanned) > 0 {
		issues = append(issues, Issue{Severity: Warning, Code: "scanned_pages",
			Message: "Страницы без текстового слоя (сканы) не импортированы: " + listNames(scanned)})
	}
	if imagesOnText > 0 {
		issues = append(issues, Issue{Severity: Info, Code: "pdf_images_skipped",
			Message: "Изображения и таблицы из PDF не переносятся — только текст и структура."})
	}

	pages := pdfPageLines(doc.Pages)
	metrics := measurePDF(pages)
	blocks := pdfBlocks(pdfParagraphs(pages, metrics), metrics)

	switch {
	case usablePDFTitle(doc.Title):
		meta.Title = strings.ToValidUTF8(doc.Title, "\uFFFD")
	default:
		// Заголовок первой страницы — титул; заголовок дальше в книге
		// («1 Введение») названием книги не бывает.
		for _, b := range blocks {
			if b.page > 1 {
				break
			}
			if b.kind == cdm.Heading {
				meta.Title = b.plain
				break
			}
		}
	}
	meta.Author = strings.ToValidUTF8(doc.Author, "\uFFFD")

	builder := cdm.NewBuilder(meta)
	if starts := outlineStarts(doc.Outline, blocks); len(starts) > 0 {
		emitByOutline(builder, blocks, starts)
	} else {
		emitByHeadings(builder, blocks)
	}
	return finish(builder.Build(), issues), nil
}

var (
	wordTitleRe    = regexp.MustCompile(`(?i)^microsoft (word|powerpoint|excel) - `)
	fileLikeNameRe = regexp.MustCompile(`(?i)\.(docx?|pdf|tex|dvi|indd|odt|rtf|pages)$`)
)

// usablePDFTitle: Word пишет в метаданные «Microsoft Word - файл.docx», а
// многие генераторы — имя файла или «Untitled»; это не название книги.
func usablePDFTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	return t != "" && t != "untitled" && !wordTitleRe.MatchString(title) && !fileLikeNameRe.MatchString(title)
}

// --- главы ---

type outlineEntry struct {
	title string
	page  int
	depth int
	start int // индекс блока, с которого глава начинается
}

func flattenOutline(items []pdftext.OutlineItem, depth int, out []outlineEntry) []outlineEntry {
	for _, it := range items {
		if title := strings.ToValidUTF8(it.Title, "\uFFFD"); it.Page > 0 && title != "" {
			out = append(out, outlineEntry{title: title, page: it.Page, depth: depth})
		}
		out = flattenOutline(it.Children, depth+1, out)
	}
	return out
}

// sectionNumberRe — номер раздела в начале заголовка: «1», «2.3.», «IV».
var sectionNumberRe = regexp.MustCompile(`^(?:\d+(?:\.\d+)*\.?|[IVXLCDM]+\.)\s+`)

// normTitle — название для сравнения закладки со строкой: без регистра,
// пунктуации и номера раздела (в закладке LaTeX «Overview», в тексте
// «1 Overview»).
func normTitle(s string) string {
	s = sectionNumberRe.ReplaceAllString(strings.TrimSpace(s), "")
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// outlineStarts находит, с какого блока начинается каждая закладка: первая
// строка её страницы, совпадающая с названием, иначе верх страницы. Начала
// не убывают — закладка «назад» начинается там же, где предыдущая.
func outlineStarts(outline []pdftext.OutlineItem, blocks []pdfBlock) []outlineEntry {
	entries := flattenOutline(outline, 1, nil)
	cursor := 0
	for i := range entries {
		e := &entries[i]
		first := cursor
		for first < len(blocks) && blocks[first].page < e.page {
			first++
		}
		e.start = first
		want := normTitle(e.title)
		for j := first; j < len(blocks) && blocks[j].page == e.page; j++ {
			got := normTitle(blocks[j].plain)
			if got == want || (blocks[j].kind == cdm.Heading && got != "" && want != "" &&
				(strings.HasPrefix(got, want) || strings.HasPrefix(want, got))) {
				e.start = j
				// Строка — название главы: заголовок уровня закладки.
				blocks[j].kind, blocks[j].text, blocks[j].meta = cdm.Heading, escapeMarkdown(blocks[j].plain), nil
				blocks[j].level = min(e.depth, 6)
				break
			}
		}
		cursor = e.start
	}
	return entries
}

func emitByOutline(b *cdm.Builder, blocks []pdfBlock, entries []outlineEntry) {
	next := 0
	for i, block := range blocks {
		for ; next < len(entries) && entries[next].start == i; next++ {
			b.StartChapter(entries[next].title, entries[next].depth)
		}
		addPDFBlock(b, block)
	}
	for ; next < len(entries); next++ {
		b.StartChapter(entries[next].title, entries[next].depth)
	}
}

// emitByHeadings — главы по заголовкам, как в Markdown; единственный
// заголовок верхнего уровня — название книги, главы — уровнем ниже.
func emitByHeadings(b *cdm.Builder, blocks []pdfBlock) {
	top, deeper := 0, false
	for _, block := range blocks {
		if block.kind == cdm.Heading {
			if block.level == 1 {
				top++
			} else {
				deeper = true
			}
		}
	}
	shift := 0
	if top == 1 && deeper {
		shift = 1
	}
	for _, block := range blocks {
		if block.kind == cdm.Heading {
			b.StartChapter(block.plain, max(1, block.level-shift))
		}
		addPDFBlock(b, block)
	}
}

func addPDFBlock(b *cdm.Builder, block pdfBlock) {
	meta := map[string]string{"page": strconv.Itoa(block.page)}
	for k, v := range block.meta {
		meta[k] = v
	}
	level := 0
	if block.kind == cdm.Heading {
		level = block.level
	}
	b.Add(block.kind, block.text, level, meta)
}

func itoa(n int) string { return strconv.Itoa(n) }
