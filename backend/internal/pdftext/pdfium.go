package pdftext

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	pdfiumerrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// PDFium (движок Chrome), собранный в WebAssembly и исполняемый wazero:
// без CGO, а разбор недоверенного файла идёт в песочнице wasm
// (дополнение §15.1).

const (
	maxOutlineDepth = 16
	maxPageObjects  = 100000
	// minInstanceWait — сколько ждать свободный экземпляр, когда все
	// заняты другими загрузками.
	minInstanceWait = 30 * time.Second
	// killWait — сколько ждать горутину разбора после уничтожения
	// экземпляра: вызов wasm прерывается по отмене контекста.
	killWait = 10 * time.Second
)

// Пул один на процесс: компиляция модуля — несколько секунд и сотни
// мегабайт, держать два пула незачем. Создаётся при первом PDF, а не при
// старте: процесс без PDF не платит за PDFium.
var (
	poolOnce sync.Once
	pool     pdfium.Pool
	poolErr  error
)

// memoryLimitPages — предел памяти в страницах wasm (64 КиБ); больше 4 ГиБ
// 32-битному модулю не адресовать.
func memoryLimitPages(mb int) uint32 {
	const maxPages = 65536
	return uint32(min(max(mb, 1)*16, maxPages)) //nolint:gosec // G115: значение ограничено выше
}

// poolConfig — конфигурация песочницы. FSConfig задан явно и пуст: иначе
// go-pdfium монтирует в модуль корень файловой системы хоста. Документ
// передаётся байтами, файлы модулю не нужны.
func poolConfig(o Options) webassembly.Config {
	instances := max(o.Instances, 1)
	runtime := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
		WithCloseOnContextDone(true)
	if o.MemoryLimitMB > 0 {
		runtime = runtime.WithMemoryLimitPages(memoryLimitPages(o.MemoryLimitMB))
	}
	return webassembly.Config{
		MinIdle: 0, MaxIdle: instances, MaxTotal: instances,
		FSConfig:      wazero.NewFSConfig(),
		RuntimeConfig: runtime,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	}
}

func sharedPool(o Options) (pdfium.Pool, error) {
	poolOnce.Do(func() { pool, poolErr = webassembly.Init(poolConfig(o)) })
	return pool, poolErr
}

type pdfiumExtractor struct {
	opts Options
}

// NewPDFium — извлечение через PDFium. Пределы памяти и число экземпляров
// берутся из первого созданного извлекателя (пул общий), страницы и
// тайм-аут — свои у каждого.
func NewPDFium(o Options) Extractor { return &pdfiumExtractor{opts: o} }

type outcome struct {
	doc *Document
	err error
}

func (x *pdfiumExtractor) Extract(data []byte) (*Document, error) {
	p, err := sharedPool(x.opts)
	if err != nil {
		return nil, fmt.Errorf("запуск PDFium: %w", err)
	}
	inst, err := p.GetInstance(max(x.opts.Timeout, minInstanceWait))
	if err != nil {
		return nil, fmt.Errorf("%w: нет свободного экземпляра PDFium: %w", ErrTimeout, err)
	}

	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("%w: сбой разбора: %v", ErrCorrupt, r)}
			}
		}()
		doc, err := extract(inst, data, x.opts.MaxPages)
		done <- outcome{doc: doc, err: err}
	}()

	timeout := x.opts.Timeout
	if timeout <= 0 {
		timeout = time.Hour
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case o := <-done:
		_ = inst.Close()
		return o.doc, o.err
	case <-timer.C:
		// Kill отменяет контекст экземпляра — вызов wasm прерывается
		// (WithCloseOnContextDone), и экземпляр в пул не возвращается.
		_ = inst.Kill()
		select {
		case <-done:
		case <-time.After(killWait):
		}
		return nil, ErrTimeout
	}
}

func extract(inst pdfium.Pdfium, data []byte, maxPages int) (*Document, error) {
	opened, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		if errors.Is(err, pdfiumerrors.ErrPassword) || errors.Is(err, pdfiumerrors.ErrSecurity) {
			return nil, ErrEncrypted
		}
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	ref := opened.Document
	defer func() { _, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: ref}) }()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: ref})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if maxPages > 0 && count.PageCount > maxPages {
		return nil, fmt.Errorf("%w: %d при пределе %d", ErrTooManyPages, count.PageCount, maxPages)
	}

	doc := &Document{Title: metaText(inst, ref, "Title"), Author: metaText(inst, ref, "Author")}
	if bm, err := inst.GetBookmarks(&requests.GetBookmarks{Document: ref}); err == nil {
		doc.Outline = outline(bm.Bookmarks, 0)
	}
	for i := range count.PageCount {
		page, err := extractPage(inst, ref, i)
		if err != nil {
			return nil, fmt.Errorf("%w: страница %d: %w", ErrCorrupt, i+1, err)
		}
		doc.Pages = append(doc.Pages, page)
	}
	return doc, nil
}

func metaText(inst pdfium.Pdfium, ref references.FPDF_DOCUMENT, tag string) string {
	m, err := inst.FPDF_GetMetaText(&requests.FPDF_GetMetaText{Document: ref, Tag: tag})
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(cleanText(m.Value)), " ")
}

func outline(items []responses.GetBookmarksBookmark, depth int) []OutlineItem {
	if depth >= maxOutlineDepth || len(items) == 0 {
		return nil
	}
	out := make([]OutlineItem, 0, len(items))
	for _, b := range items {
		item := OutlineItem{Title: strings.Join(strings.Fields(cleanText(b.Title)), " "),
			Children: outline(b.Children, depth+1)}
		if b.DestInfo != nil && b.DestInfo.PageIndex >= 0 {
			item.Page = b.DestInfo.PageIndex + 1
		}
		out = append(out, item)
	}
	return out
}

func extractPage(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int) (Page, error) {
	loaded, err := inst.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: doc, Index: index})
	if err != nil {
		return Page{}, err
	}
	defer func() { _, _ = inst.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: loaded.Page}) }()
	ref := requests.Page{ByReference: &loaded.Page}

	page := Page{Number: index + 1}
	if w, err := inst.FPDF_GetPageWidthF(&requests.FPDF_GetPageWidthF{Page: ref}); err == nil {
		page.Width = float64(w.PageWidth)
	}
	if h, err := inst.FPDF_GetPageHeightF(&requests.FPDF_GetPageHeightF{Page: ref}); err == nil {
		page.Height = float64(h.PageHeight)
	}
	text, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
		Page: ref, Mode: requests.GetPageTextStructuredModeChars, CollectFontInformation: true,
	})
	if err != nil {
		return Page{}, err
	}
	page.Lines = buildLines(text.Chars)

	objects, err := inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: ref})
	if err == nil {
		for i := range min(objects.Count, maxPageObjects) {
			obj, err := inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: ref, Index: i})
			if err != nil {
				continue
			}
			kind, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
			if err == nil && kind.Type == enums.FPDF_PAGEOBJ_IMAGE {
				page.Images++
			}
		}
	}
	return page, nil
}

type spanStyle struct {
	size               float64
	bold, italic, mono bool
}

func styleOf(fi *responses.FontInformation) spanStyle {
	if fi == nil {
		return spanStyle{}
	}
	name := strings.ToLower(fi.Name)
	if _, rest, ok := strings.Cut(name, "+"); ok { // подмножество шрифта: «ABCDEF+DejaVuSerif-Bold»
		name = rest
	}
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(name, w) {
				return true
			}
		}
		return false
	}
	size := fi.RenderedSize
	if size <= 0 {
		size = fi.Size
	}
	return spanStyle{
		size:   math.Round(size*2) / 2,
		bold:   fi.Weight >= 600 || has("bold", "black", "heavy", "semibold", "demibold"),
		italic: fi.Flags&0x40 != 0 || has("italic", "oblique"),
		mono:   fi.Flags&0x1 != 0 || has("mono", "courier", "consol", "code"),
	}
}

// buildLines собирает строки из символов PDFium. Концы строк («\r\n») и
// пробелы между словами PDFium вставляет сам по геометрии; пробел
// продолжает текущий кусок, чтобы стиль не дробился на каждом слове.
func buildLines(chars []*responses.GetPageTextStructuredChar) []Line {
	var lines []Line
	var cur *Line
	var style spanStyle
	flush := func() {
		if cur != nil && strings.TrimSpace(cleanText(cur.Text())) != "" {
			last := &cur.Spans[len(cur.Spans)-1]
			last.Text = markLineHyphen(last.Text)
			for i := range cur.Spans {
				cur.Spans[i].Text = cleanText(cur.Spans[i].Text)
			}
			lines = append(lines, *cur)
		}
		cur = nil
	}
	for _, ch := range chars {
		if ch == nil {
			continue
		}
		if ch.Text == "\r" || ch.Text == "\n" || ch.Text == "\r\n" {
			flush()
			continue
		}
		// Мягкий перенос пока остаётся: в конце строки он — метка переноса.
		text := ch.Text
		if cleanText(text) == "" && text != "\u00ad" {
			continue
		}
		space := strings.TrimSpace(text) == ""
		if cur == nil {
			cur = &Line{X0: math.Inf(1), X1: math.Inf(-1), Top: math.Inf(-1), Bottom: math.Inf(1)}
		}
		st := style
		if !space || len(cur.Spans) == 0 {
			st = styleOf(ch.FontInformation)
		}
		if len(cur.Spans) == 0 || st != style {
			cur.Spans = append(cur.Spans, Span{Size: st.size, Bold: st.bold, Italic: st.italic, Mono: st.mono})
			style = st
		}
		cur.Spans[len(cur.Spans)-1].Text += text
		if !space {
			pos := ch.PointPosition
			cur.X0, cur.X1 = math.Min(cur.X0, pos.Left), math.Max(cur.X1, pos.Right)
			cur.Top, cur.Bottom = math.Max(cur.Top, pos.Top), math.Min(cur.Bottom, pos.Bottom)
		}
	}
	flush()
	return lines
}
