package importers

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/pdftext"
)

// Разбор вёрстки PDF: строки → абзацы → блоки CDM (дополнение §15.1).
// В PDF нет абзацев, заголовков и списков — только строки с координатами и
// шрифтом; структура восстанавливается эвристиками. Значения порогов —
// в дополнении, тесты — в pdf_layout_test.go.

const (
	runningHeadZone   = 0.08 // доля высоты страницы сверху и снизу для колонтитулов
	minRunningRepeats = 3
	paragraphGap      = 1.4  // зазор больше 1,4 межстрочного шага — новый абзац
	codeBlockGap      = 4.0  // в коде пустые строки законны, рвём только на большом зазоре
	shortLineShare    = 0.15 // строка короче колонки на 15 % — кандидат в конец абзаца
	headingSizeRatio  = 1.2
	boldHeadingMax    = 80
	headingMaxRunes   = 200
	headingMaxLines   = 3
	maxBlankCodeLines = 3
	minPitchSamples   = 3
	minColumnLines    = 5 // меньше строк — границы колонки берутся по всему документу
)

// pdfLine — строка с вычисленными свойствами.
type pdfLine struct {
	pdftext.Line
	page int
	text string  // без краевых пробелов
	size float64 // кегль большинства символов
	bold bool    // все символы жирные
	mono bool    // все символы моноширинные
}

func newPDFLine(l pdftext.Line, page int) pdfLine {
	counts := map[float64]int{}
	var total, bold, mono int
	for _, s := range l.Spans {
		n := 0
		for _, r := range s.Text {
			if !unicode.IsSpace(r) && r != '\x02' {
				n++
			}
		}
		counts[s.Size] += n
		total += n
		if s.Bold {
			bold += n
		}
		if s.Mono {
			mono += n
		}
	}
	pl := pdfLine{Line: l, page: page, text: strings.TrimSpace(l.Text())}
	best := -1
	for size, n := range counts {
		if n > best || (n == best && size > pl.size) {
			pl.size, best = size, n
		}
	}
	pl.bold = total > 0 && bold == total
	pl.mono = total > 0 && mono == total
	return pl
}

var (
	digitsRe = regexp.MustCompile(`\d+`)
	// dotLeaderRe — строка печатного оглавления: отточие и номер страницы.
	dotLeaderRe  = regexp.MustCompile(`(?:\.\s*){4,}\d{1,4}$`)
	pageNumberRe = regexp.MustCompile(`(?i)^(?:(?:page|стр\.?|страница|с\.)\s*)?[-–—]?\s*(?:\d{1,5}|[ivxlcdm]{1,7})\s*[-–—]?` +
		`(?:\s*(?:/|of|из)\s*\d{1,5})?$`)
)

func runningHeadKey(text string) string {
	return digitsRe.ReplaceAllString(strings.ToLower(collapseSpaces(text)), "#")
}

func inRunningZone(p pdftext.Page, l pdftext.Line) bool {
	return p.Height > 0 && (l.Bottom > p.Height*(1-runningHeadZone) || l.Top < p.Height*runningHeadZone)
}

// pdfPageLines — строки страниц без колонтитулов: строк у верхнего и нижнего
// края, повторяющихся на половине страниц и больше (минимум на трёх), и
// голых номеров страниц. Повтор на двух страницах — ещё не колонтитул.
func pdfPageLines(pages []pdftext.Page) [][]pdfLine {
	seen := map[string]int{}
	for _, p := range pages {
		onPage := map[string]bool{}
		for _, l := range p.Lines {
			if inRunningZone(p, l) {
				onPage[runningHeadKey(l.Text())] = true
			}
		}
		for key := range onPage {
			seen[key]++
		}
	}
	minRepeats := max(minRunningRepeats, (len(pages)+1)/2)
	out := make([][]pdfLine, len(pages))
	for i, p := range pages {
		for _, l := range p.Lines {
			text := strings.TrimSpace(l.Text())
			// Печатное оглавление дублирует закладки и заголовки, а его
			// перевод не нужен никому.
			if dotLeaderRe.MatchString(text) {
				continue
			}
			if inRunningZone(p, l) && (pageNumberRe.MatchString(text) ||
				(len(pages) >= minRunningRepeats && seen[runningHeadKey(text)] >= minRepeats)) {
				continue
			}
			// PDFium отдаёт валидный UTF-8, но PostgreSQL не примет иной
			// текст в JSONB — защита не должна зависеть от движка.
			for k := range l.Spans {
				l.Spans[k].Text = strings.ToValidUTF8(l.Spans[k].Text, "\uFFFD")
			}
			out[i] = append(out[i], newPDFLine(l, p.Number))
		}
	}
	return out
}

// pdfMetrics — основной кегль и межстрочный шаг документа.
type pdfMetrics struct {
	body  float64
	pitch float64 // для основного кегля
}

func (m pdfMetrics) pitchFor(size float64) float64 { return m.pitch * size / m.body }

func measurePDF(pages [][]pdfLine) pdfMetrics {
	weights := map[float64]int{}
	for _, lines := range pages {
		for _, l := range lines {
			if !l.mono {
				weights[l.size] += utf8.RuneCountInString(l.text)
			}
		}
	}
	m := pdfMetrics{body: 10}
	best := 0
	for size, n := range weights {
		if n > best || (n == best && size < m.body) {
			m.body, best = size, n
		}
	}
	if best == 0 {
		m.body = 10
	}
	var steps []float64
	for _, lines := range pages {
		for i := 1; i < len(lines); i++ {
			a, b := lines[i-1], lines[i]
			step := a.Bottom - b.Bottom
			if !a.mono && !b.mono && math.Abs(a.size-m.body) <= 0.5 && math.Abs(b.size-m.body) <= 0.5 &&
				step > 0 && step < 3*m.body {
				steps = append(steps, step)
			}
		}
	}
	// Нижний квартиль, а не медиана: шаги внутри абзацев — большинство,
	// зазоры между абзацами — выбросы вверх. По двум-трём строкам шаг не
	// оценить — тогда типичный 1,2 кегля.
	m.pitch = m.body * 1.2
	if len(steps) >= minPitchSamples {
		sort.Float64s(steps)
		m.pitch = steps[len(steps)/4]
	}
	return m
}

// pdfColumn — границы колонки страницы по строкам основного текста.
type pdfColumn struct{ left, right float64 }

func (c pdfColumn) width() float64 { return c.right - c.left }

func pageColumn(lines []pdfLine, m pdfMetrics) pdfColumn {
	col := pdfColumn{left: math.Inf(1), right: math.Inf(-1)}
	for _, pass := range []bool{true, false} {
		for _, l := range lines {
			if pass && (l.mono || math.Abs(l.size-m.body) > 0.5) {
				continue
			}
			col.left, col.right = math.Min(col.left, l.X0), math.Max(col.right, l.X1)
		}
		if !math.IsInf(col.left, 1) {
			break
		}
	}
	return col
}

var sentenceEndRe = regexp.MustCompile(`[.!?…:;»"”)\]]$`)

// nextWordFits: первое слово следующей строки поместилось бы на этой —
// значит, перенос был намеренным, а не вёрсткой.
func nextWordFits(a, b pdfLine, col pdfColumn) bool {
	fields := strings.Fields(b.text)
	runes := utf8.RuneCountInString(b.text)
	if len(fields) == 0 || runes == 0 {
		return false
	}
	width := (b.X1 - b.X0) * float64(utf8.RuneCountInString(fields[0])+1) / float64(runes)
	return a.X1+width < col.right
}

func (a pdfLine) short(col pdfColumn) bool {
	return a.X1 < col.right-shortLineShare*col.width()
}

// paragraphRight — правый край абзаца, к которому прибавляется строка b
// после строк lines. Узкий абзац, выровненный по ширине (цитата, врезка),
// — сам себе колонка: иначе каждая его строка выглядела бы короткой.
func paragraphRight(lines []pdfLine, b pdfLine, col pdfColumn) pdfColumn {
	right := math.Inf(-1)
	for _, l := range lines {
		right = math.Max(right, l.X1)
	}
	a := lines[len(lines)-1]
	if len(lines) == 1 && math.Abs(a.X1-b.X1) > 1 {
		return col
	}
	return pdfColumn{left: col.left, right: right}
}

// breaksParagraph — начинает ли строка b новый абзац после строк lines;
// next — строка после b (nil — b последняя на странице).
func breaksParagraph(lines []pdfLine, b pdfLine, next *pdfLine, col pdfColumn, m pdfMetrics) bool {
	a := lines[len(lines)-1]
	if a.mono != b.mono {
		return true
	}
	gap := a.Bottom - b.Bottom
	if a.mono {
		return gap <= 0 || gap > codeBlockGap*m.pitchFor(a.size)
	}
	switch {
	case math.Abs(a.size-b.size) > 0.5:
		return true
	case gap <= 0 || gap > paragraphGap*m.pitchFor(b.size): // зазор или переход в другую колонку
		return true
	case b.X0 > a.X0+b.size && indentStartsParagraph(a, b, next):
		return true
	case listMarker(b.text) != "":
		return true
	case a.short(paragraphRight(lines, b, col)) && nextWordFits(a, b, col):
		return true
	}
	return false
}

// indentStartsParagraph отличает отступ первой строки от висячего отступа
// (список определений: термин слева, продолжение с отступом). После первой
// строки абзаца текст возвращается к левому краю, при висячем отступе —
// остаётся с отступом.
func indentStartsParagraph(a, b pdfLine, next *pdfLine) bool {
	if next == nil {
		return sentenceEndRe.MatchString(a.text)
	}
	return math.Abs(next.X0-b.X0) > b.size/2
}

// continuesAcrossPages — абзац продолжается на следующей странице: строка
// до края колонки без концевой пунктуации, а новая страница начинается
// без отступа тем же кеглем.
func continuesAcrossPages(a, b pdfLine, colA, colB pdfColumn, m pdfMetrics) bool {
	return !a.mono && !b.mono && math.Abs(a.size-m.body) <= 0.5 && math.Abs(a.size-b.size) <= 0.5 &&
		!a.short(colA) && !sentenceEndRe.MatchString(a.text) && b.X0 <= colB.left+0.5*b.size &&
		listMarker(b.text) == ""
}

// pdfPara — строки одного абзаца.
type pdfPara struct {
	lines []pdfLine
}

func (p *pdfPara) first() pdfLine { return p.lines[0] }

func (p *pdfPara) mono() bool { return p.first().mono }

func pdfParagraphs(pages [][]pdfLine, m pdfMetrics) []*pdfPara {
	var all []pdfLine
	for _, lines := range pages {
		all = append(all, lines...)
	}
	docCol := pageColumn(all, m)
	var paras []*pdfPara
	var prevCol pdfColumn
	for _, lines := range pages {
		if len(lines) == 0 {
			continue
		}
		// По нескольким строкам край колонки не виден: последняя строка
		// абзаца сама окажется «краем».
		col := docCol
		if len(lines) >= minColumnLines {
			col = pageColumn(lines, m)
		}
		var cur *pdfPara
		for i, l := range lines {
			var next *pdfLine
			if i+1 < len(lines) {
				next = &lines[i+1]
			}
			switch {
			case i == 0 && len(paras) > 0 && continuesAcrossPages(last(paras[len(paras)-1].lines), l, prevCol, col, m):
				cur = paras[len(paras)-1]
				cur.lines = append(cur.lines, l)
			case cur == nil || breaksParagraph(cur.lines, l, next, col, m):
				cur = &pdfPara{lines: []pdfLine{l}}
				paras = append(paras, cur)
			default:
				cur.lines = append(cur.lines, l)
			}
		}
		prevCol = col
	}
	return paras
}

func last[T any](s []T) T { return s[len(s)-1] }

// --- текст абзаца ---

// segments — встроенное содержимое абзаца: строки склеиваются пробелом,
// перенос в слове («интернацио\x02» + «нализация») — без пробела и дефиса,
// дефис после заглавной («PDF\x02» + «документ») и обычный дефис в конце
// строки сохраняются без пробела.
func (p *pdfPara) segments() []docxSeg {
	var segs []docxSeg
	for i, l := range p.lines {
		line := lineSegments(l)
		if len(line) == 0 {
			continue
		}
		if i > 0 && len(segs) > 0 {
			prev := &segs[len(segs)-1]
			next, _ := utf8.DecodeRuneInString(l.text)
			switch {
			case strings.HasSuffix(prev.text, pdftextHyphen):
				prev.text = strings.TrimSuffix(prev.text, pdftextHyphen)
				before, _ := utf8.DecodeLastRuneInString(prev.text)
				if !unicode.IsLower(before) || !unicode.IsLower(next) {
					prev.text += "-"
				}
			case strings.HasSuffix(prev.text, "-"):
			default:
				space := *prev
				space.text = " "
				segs = append(segs, space)
			}
		}
		segs = append(segs, line...)
	}
	for i := range segs {
		segs[i].text = strings.ReplaceAll(segs[i].text, pdftextHyphen, "-")
	}
	return segs
}

const pdftextHyphen = "\x02"

func lineSegments(l pdfLine) []docxSeg {
	var segs []docxSeg
	for _, s := range l.Spans {
		if s.Text != "" {
			segs = append(segs, docxSeg{text: s.Text, bold: s.Bold, italic: s.Italic, code: s.Mono})
		}
	}
	if len(segs) > 0 {
		segs[0].text = strings.TrimLeft(segs[0].text, " ")
		segs[len(segs)-1].text = strings.TrimRight(segs[len(segs)-1].text, " ")
	}
	return segs
}

// codeText — строки кода с отступами по координатам: PDFium сжимает
// ведущие пробелы, а ширина символа моноширинного шрифта постоянна.
func (p *pdfPara) codeText(m pdfMetrics) string {
	var widths []float64
	minX := math.Inf(1)
	for _, l := range p.lines {
		minX = math.Min(minX, l.X0)
		if n := utf8.RuneCountInString(l.text); n > 0 && l.X1 > l.X0 {
			widths = append(widths, (l.X1-l.X0)/float64(n))
		}
	}
	charWidth := p.first().size * 0.6
	if len(widths) > 0 {
		sort.Float64s(widths)
		charWidth = widths[len(widths)/2]
	}
	var out []string
	for i, l := range p.lines {
		if i > 0 {
			gap := p.lines[i-1].Bottom - l.Bottom
			blanks := int(math.Round(gap/m.pitchFor(l.size))) - 1
			for range min(blanks, maxBlankCodeLines) {
				out = append(out, "")
			}
		}
		indent := int(math.Round((l.X0 - minX) / charWidth))
		out = append(out, strings.Repeat(" ", max(indent, 0))+strings.ReplaceAll(l.text, pdftextHyphen, "-"))
	}
	return strings.Join(out, "\n")
}

// --- классификация ---

var (
	bulletRe  = regexp.MustCompile(`^[•◦▪▫●○■□‣⁃∙·*]\s+`)
	orderedRe = regexp.MustCompile(`^(?:\d{1,3}[.)]|[a-zа-яё]\))\s+`)
)

// listMarker — маркер пункта списка в начале текста. Тире не маркер: в
// русской прозе им начинается реплика диалога.
func listMarker(text string) string {
	if m := bulletRe.FindString(text); m != "" {
		return m
	}
	return orderedRe.FindString(text)
}

// pdfBlock — блок, готовый к записи в CDM.
type pdfBlock struct {
	kind  cdm.BlockType
	text  string
	plain string // текст без разметки — название главы
	level int
	page  int
	meta  map[string]string
	x0    float64
}

func pdfBlocks(paras []*pdfPara, m pdfMetrics) []pdfBlock {
	// Уровни заголовков — по убыванию кегля; жирные заголовки основного
	// кегля — следующий уровень.
	var sizes []float64
	for _, p := range paras {
		if s := p.first().size; !p.mono() && isSizeHeading(p, m) && !slices.Contains(sizes, s) {
			sizes = append(sizes, s)
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(sizes)))

	var blocks []pdfBlock
	for _, p := range paras {
		first := p.first()
		if p.mono() {
			blocks = append(blocks, pdfBlock{kind: cdm.CodeBlock, text: p.codeText(m), page: first.page})
			continue
		}
		segs := p.segments()
		plain := collapseSpaces(plainSegments(segs))
		if plain == "" {
			continue
		}
		b := pdfBlock{page: first.page, plain: plain, x0: first.X0}
		switch {
		case isSizeHeading(p, m):
			b.kind, b.text = cdm.Heading, escapeMarkdown(plain)
			b.level = min(slices.Index(sizes, first.size)+1, 6)
		case isBoldHeading(p, plain, m):
			b.kind, b.text = cdm.Heading, escapeMarkdown(plain)
			b.level = min(len(sizes)+1, 6)
		default:
			if marker := listMarker(plain); marker != "" {
				b.kind = cdm.ListItem
				b.meta = map[string]string{}
				if orderedRe.MatchString(plain) {
					b.meta["ordered"] = "true"
				}
				segs = trimSegments(segs, utf8.RuneCountInString(marker))
			} else {
				b.kind = cdm.Paragraph
			}
			b.text = renderSegments(segs)
		}
		blocks = append(blocks, b)
	}
	setListDepths(blocks)
	return blocks
}

func isSizeHeading(p *pdfPara, m pdfMetrics) bool {
	if len(p.lines) > headingMaxLines || p.first().size < m.body*headingSizeRatio {
		return false
	}
	n := 0
	for _, l := range p.lines {
		n += utf8.RuneCountInString(l.text)
	}
	return n <= headingMaxRunes
}

func isBoldHeading(p *pdfPara, plain string, m pdfMetrics) bool {
	l := p.first()
	return len(p.lines) == 1 && l.bold && l.size >= m.body-0.5 && utf8.RuneCountInString(plain) <= boldHeadingMax &&
		!strings.ContainsAny(plain[len(plain)-1:], ".,;:")
}

// trimSegments отрезает n первых символов (маркер списка).
func trimSegments(segs []docxSeg, n int) []docxSeg {
	for len(segs) > 0 && n > 0 {
		runes := []rune(segs[0].text)
		if len(runes) > n {
			segs[0].text = string(runes[n:])
			break
		}
		n -= len(runes)
		segs = segs[1:]
	}
	if len(segs) > 0 {
		segs[0].text = strings.TrimLeft(segs[0].text, " ")
	}
	return segs
}

// setListDepths — глубина пункта по отступу внутри серии подряд идущих
// пунктов: каждый новый отступ (с допуском 4 пт) — уровень глубже.
func setListDepths(blocks []pdfBlock) {
	for i := 0; i < len(blocks); {
		if blocks[i].kind != cdm.ListItem {
			i++
			continue
		}
		j := i
		var indents []float64
		for ; j < len(blocks) && blocks[j].kind == cdm.ListItem; j++ {
			if !slices.ContainsFunc(indents, func(x float64) bool { return math.Abs(x-blocks[j].x0) <= 4 }) {
				indents = append(indents, blocks[j].x0)
			}
		}
		sort.Float64s(indents)
		for k := i; k < j; k++ {
			depth := slices.IndexFunc(indents, func(x float64) bool { return math.Abs(x-blocks[k].x0) <= 4 }) + 1
			blocks[k].meta["depth"] = itoa(min(depth, 6))
		}
		i = j
	}
}
