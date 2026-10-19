package importers

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/vnovels/linguastack/internal/cdm"
)

// EPUB — импорт EPUB 2 и 3 на стандартной библиотеке (ТЗ §3.1: главы,
// оглавление, текст, ссылки, изображения).
//
// EPUB — это ZIP с OPF-описанием в XML. Порядок чтения задаёт spine;
// оглавление (nav в EPUB 3, иначе NCX) даёт названия глав и вложенность.
// Встроенная HTML-разметка приводится к Markdown-синтаксису — так во всём
// CDM один встроенный синтаксис, и маскирование, проверка разметки и
// экспорт работают одинаково для всех форматов.
type EPUB struct {
	limits ArchiveLimits
}

// NewEPUB создаёт импортёр EPUB.
func NewEPUB(limits ArchiveLimits) Importer { return EPUB{limits: limits} }

func (EPUB) FormatID() string { return "epub" }

// Sniff узнаёт EPUB и по расширению, и по содержимому: по спецификации OCF
// первая запись архива — несжатый mimetype, так что EPUB с расширением .zip
// не уйдёт в импорт репозитория.
func (EPUB) Sniff(data []byte, filename string) bool {
	return fileExt(filename) == ".epub" || isEPUBContent(data)
}

func isEPUBContent(data []byte) bool {
	const header = 30
	if len(data) < header || !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return false
	}
	nameLen := int(binary.LittleEndian.Uint16(data[26:28]))
	extraLen := int(binary.LittleEndian.Uint16(data[28:30]))
	start := header + nameLen + extraLen
	return len(data) >= start && string(data[header:header+nameLen]) == "mimetype" &&
		bytes.HasPrefix(data[start:], []byte("application/epub+zip"))
}

// --- структуры OCF/OPF/NCX ---
//
// encoding/xml не загружает внешние сущности и DTD (XXE) и не разворачивает
// пользовательские сущности — «billion laughs» ему не грозит.

type ocfContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opfPackage struct {
	Metadata struct {
		Titles    []string `xml:"title"`
		Creators  []string `xml:"creator"`
		Languages []string `xml:"language"`
	} `xml:"metadata"`
	Manifest []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		MediaType  string `xml:"media-type,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine struct {
		TOC   string `xml:"toc,attr"`
		Items []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type ncxPoint struct {
	Label   string `xml:"navLabel>text"`
	Content struct {
		Src string `xml:"src,attr"`
	} `xml:"content"`
	Children []ncxPoint `xml:"navPoint"`
}

type ncxDoc struct {
	Points []ncxPoint `xml:"navMap>navPoint"`
}

type tocLabel struct {
	label string
	depth int
}

func decodeXML(data []byte, v any) error {
	d := xml.NewDecoder(bytes.NewReader(data))
	// Нестрогий режим и HTML-сущности: в реальных NCX встречаются &nbsp;.
	// HTMLAutoClose здесь недопустим: в OPF есть <meta property="…">текст</meta>,
	// а для HTML meta — пустой элемент; непарный </meta> разваливал разбор
	// всего описания книги (найдено на реальном EPUB с Project Gutenberg).
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charset.NewReaderLabel
	return d.Decode(v)
}

// --- загрузка ---

type epubBook struct {
	files  map[string]*zip.File
	issues []Issue
}

func (e EPUB) Load(data []byte, filename string) (Result, error) {
	fallback := cdm.BookMeta{Title: titleFromFilename(filename), SourceFormat: "epub"}
	corrupt := func(reason string) (Result, error) {
		issue := corruptIssue("EPUB")
		if reason != "" {
			issue.Message += " " + reason
		}
		return finish(cdm.NewBuilder(fallback).Build(), []Issue{issue}), nil
	}

	archive, err := openArchive(data, e.limits)
	if err != nil {
		return Result{}, err
	}
	if archive.corrupt {
		return corrupt("")
	}
	book := &epubBook{files: map[string]*zip.File{}, issues: archive.issues}
	for _, entry := range archive.entries {
		if _, seen := book.files[entry.name]; !seen {
			book.files[entry.name] = entry.file
		}
	}

	var container ocfContainer
	raw, ok, err := book.read("META-INF/container.xml")
	if err != nil {
		return Result{}, err
	}
	if !ok || decodeXML(raw, &container) != nil || len(container.Rootfiles) == 0 {
		return corrupt("Нет описания книги (META-INF/container.xml).")
	}
	opfPath := cleanHref("", container.Rootfiles[0].FullPath)
	var opf opfPackage
	raw, ok, err = book.read(opfPath)
	if err != nil {
		return Result{}, err
	}
	if !ok || decodeXML(raw, &opf) != nil {
		return corrupt("Не читается OPF-описание " + opfPath + ".")
	}
	opfDir := path.Dir(opfPath)

	type manifestItem struct{ href, mediaType string }
	manifest := map[string]manifestItem{}
	var navHref, ncxHref string
	for _, item := range opf.Manifest {
		href := cleanHref(opfDir, item.Href)
		manifest[item.ID] = manifestItem{href: href, mediaType: item.MediaType}
		if strings.Contains(" "+item.Properties+" ", " nav ") {
			navHref = href
		}
		if item.MediaType == "application/x-dtbncx+xml" && (ncxHref == "" || item.ID == opf.Spine.TOC) {
			ncxHref = href
		}
	}

	toc, err := book.tableOfContents(navHref, ncxHref)
	if err != nil {
		return Result{}, err
	}

	meta := fallback
	if t := firstNonEmpty(opf.Metadata.Titles); t != "" {
		meta.Title = t
	}
	meta.Author = strings.Join(trimAll(opf.Metadata.Creators), ", ")
	meta.Language = firstNonEmpty(opf.Metadata.Languages)
	builder := cdm.NewBuilder(meta)

	var images []string
	depth := 1
	for _, ref := range opf.Spine.Items {
		item, ok := manifest[ref.IDRef]
		if !ok || (item.mediaType != "application/xhtml+xml" && item.mediaType != "text/html") {
			continue
		}
		raw, ok, err := book.read(item.href)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		text, issues := sanitizeUTF8(string(raw))
		book.issues = append(book.issues, issues...)
		doc, err := html.Parse(strings.NewReader(text))
		if err != nil {
			book.issues = append(book.issues, Issue{Severity: Warning, Code: "corrupt_entry",
				Message: fmt.Sprintf("Не разобрать главу %q; она пропущена.", item.href)})
			continue
		}
		body := findElement(doc, atom.Body)
		if body == nil {
			body = doc
		}

		label := ""
		if entry, ok := toc[item.href]; ok {
			label, depth = entry.label, entry.depth
		}
		if label == "" {
			if h := firstHeadingNode(body); h != nil {
				label = collapseSpaces(textContent(h))
			}
		}
		if label == "" {
			// Страницы вне оглавления без заголовков — обложка, титул:
			// у Project Gutenberg это <title>"Cover"</title>.
			if title := findElement(doc, atom.Title); title != nil {
				label = strings.Trim(collapseSpaces(textContent(title)), `"'«»“”`)
			}
		}
		if label == "" {
			label = titleFromFilename(item.href)
		}
		builder.StartChapter(label, depth)
		em := &htmlEmitter{b: builder, dir: path.Dir(item.href), blockish: map[*html.Node]bool{}}
		em.markBlockish(body)
		em.blocks(body, htmlContext{})
		images = append(images, em.images...)
	}

	result := finish(builder.Build(), book.issues)
	var missing []string
	result.Assets, missing = collectAssets(book.files, images)
	if len(missing) > 0 {
		result.Issues = append(result.Issues, missingResourceIssue(missing))
	}
	return result, nil
}

func (b *epubBook) read(name string) ([]byte, bool, error) {
	f, ok := b.files[name]
	if !ok {
		return nil, false, nil
	}
	data, err := readEntry(f)
	if errors.Is(err, ErrArchiveLimits) {
		return nil, false, err
	}
	if err != nil {
		b.issues = append(b.issues, Issue{Severity: Warning, Code: "corrupt_entry",
			Message: fmt.Sprintf("Не удалось прочитать %q из архива; файл пропущен.", name)})
		return nil, false, nil
	}
	return data, true, nil
}

// tableOfContents: nav (EPUB 3) приоритетнее NCX; без обоих — порядок spine
// и замечание toc_missing. Для файла берётся первое упоминание: главы,
// заданные якорями внутри одного файла, в MVP не разделяются.
func (b *epubBook) tableOfContents(navHref, ncxHref string) (map[string]tocLabel, error) {
	toc := map[string]tocLabel{}
	add := func(href, label string, depth int) {
		if href == "" {
			return
		}
		if _, seen := toc[href]; !seen {
			toc[href] = tocLabel{label: collapseSpaces(label), depth: depth}
		}
	}

	if navHref != "" {
		raw, ok, err := b.read(navHref)
		if err != nil {
			return nil, err
		}
		if ok {
			if doc, perr := html.Parse(bytes.NewReader(raw)); perr == nil {
				if nav := findTOCNav(doc); nav != nil {
					walkNavList(nav, path.Dir(navHref), 0, add)
				}
			}
		}
	}
	if len(toc) == 0 && ncxHref != "" {
		raw, ok, err := b.read(ncxHref)
		if err != nil {
			return nil, err
		}
		var ncx ncxDoc
		if ok && decodeXML(raw, &ncx) == nil {
			var walk func([]ncxPoint, int)
			walk = func(points []ncxPoint, depth int) {
				for _, p := range points {
					add(cleanHref(path.Dir(ncxHref), p.Content.Src), p.Label, depth)
					walk(p.Children, depth+1)
				}
			}
			walk(ncx.Points, 1)
		}
	}
	if len(toc) == 0 {
		b.issues = append(b.issues, Issue{Severity: Warning, Code: "toc_missing",
			Message: "В книге нет оглавления; главы взяты в порядке чтения, названия — из заголовков."})
	}
	return toc, nil
}

func findTOCNav(doc *html.Node) *html.Node {
	var first, toc *html.Node
	walkNodes(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.DataAtom != atom.Nav {
			return true
		}
		if first == nil {
			first = n
		}
		for _, a := range n.Attr {
			if (a.Key == "epub:type" || (a.Namespace == "epub" && a.Key == "type") || a.Key == "role") &&
				(strings.Contains(a.Val, "toc")) {
				toc = n
				return false
			}
		}
		return true
	})
	if toc != nil {
		return toc
	}
	return first
}

func walkNavList(n *html.Node, dir string, depth int, add func(href, label string, depth int)) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.Ol, atom.Ul:
			walkNavList(c, dir, depth+1, add)
		case atom.Li:
			if a := findElement(c, atom.A); a != nil {
				add(cleanHref(dir, attr(a, "href")), textContent(a), max(depth, 1))
			}
			walkNavList(c, dir, depth, add)
		default:
			walkNavList(c, dir, depth, add)
		}
	}
}

// --- HTML главы → блоки CDM ---

type htmlContext struct {
	listDepth int
	ordered   bool
	inQuote   bool
}

type htmlEmitter struct {
	b      *cdm.Builder
	dir    string
	images []string
	// blockish: элемент блочный сам или содержит блочных потомков.
	// Встроенный элемент может обернуть абзацы: HTML5-разбор восстанавливает
	// незакрытый <a id="…"/> из заголовка вокруг всех следующих <p>
	// (так устроены главы Project Gutenberg). Решать «блок или встроенный»
	// по одному имени тега нельзя — вся глава склеилась бы в один абзац.
	blockish map[*html.Node]bool
	// splitChapters — заголовок открывает главу глубины level-minLevel+1
	// (импорт HTML). В EPUB главы задаёт spine, и заголовок — просто блок.
	splitChapters bool
	minLevel      int
	// imageSrc разрешает адрес картинки в имя ресурса книги; false —
	// картинка пропускается. nil — путь относительно dir.
	imageSrc func(raw string) (string, bool)
}

// resolveImage — имя ресурса для адреса картинки из разметки.
func (e *htmlEmitter) resolveImage(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if e.imageSrc != nil {
		return e.imageSrc(raw)
	}
	return cleanHref(e.dir, raw), true
}

// markBlockish вычисляет blockish снизу вверх один раз на документ, чтобы
// проверка «есть ли блочные потомки» не была квадратичной.
func (e *htmlEmitter) markBlockish(n *html.Node) bool {
	if n.Type == html.ElementNode && skippedTags[n.DataAtom] {
		return false
	}
	has := n.Type == html.ElementNode &&
		(blockTags[n.DataAtom] || (n.DataAtom == atom.Math && attr(n, "display") == "block"))
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if e.markBlockish(c) {
			has = true
		}
	}
	e.blockish[n] = has
	return has
}

func (e *htmlEmitter) hasBlockChild(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if e.blockish[c] {
			return true
		}
	}
	return false
}

var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Header: true, atom.Footer: true,
	atom.Main: true, atom.Aside: true, atom.Nav: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.H5: true, atom.H6: true, atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Blockquote: true, atom.Pre: true,
	atom.Table: true, atom.Figure: true, atom.Figcaption: true, atom.Hr: true, atom.Dl: true, atom.Dt: true,
	atom.Dd: true, atom.Address: true, atom.Details: true, atom.Summary: true, atom.Center: true, atom.Body: true,
	atom.Html: true, atom.Caption: true,
}

var skippedTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Head: true, atom.Template: true, atom.Noscript: true,
}

func (e *htmlEmitter) blocks(n *html.Node, ctx htmlContext) {
	var inline []*html.Node
	flush := func() {
		if len(inline) > 0 {
			e.inlineBlock(inline, ctx)
			inline = nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && skippedTags[c.DataAtom]:
		case c.Type == html.ElementNode && e.blockish[c]:
			flush()
			e.block(c, ctx)
		case c.Type == html.TextNode || c.Type == html.ElementNode:
			inline = append(inline, c)
		}
	}
	flush()
}

func (e *htmlEmitter) block(n *html.Node, ctx htmlContext) {
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		if text := e.inlineText(childNodes(n)); text != "" {
			level := int(n.Data[1] - '0')
			if e.splitChapters {
				e.b.StartChapter(collapseSpaces(textContent(n)), max(1, level-e.minLevel+1))
			}
			e.b.Add(cdm.Heading, text, level, nil)
		}
	case atom.P, atom.Dt, atom.Dd, atom.Summary, atom.Address:
		// В режиме quirks (XHTML без DOCTYPE) таблица не закрывает <p>,
		// так что абзац может содержать блоки.
		if e.hasBlockChild(n) {
			e.blocks(n, ctx)
		} else {
			e.inlineBlock(childNodes(n), ctx)
		}
	case atom.Pre:
		var meta map[string]string
		if code := findElement(n, atom.Code); code != nil {
			if lang := languageFromClass(attr(code, "class")); lang != "" {
				meta = map[string]string{"language": lang}
			}
		}
		e.b.Add(cdm.CodeBlock, strings.TrimSuffix(textContent(n), "\n"), 0, meta)
	case atom.Ul, atom.Ol:
		inner := htmlContext{listDepth: ctx.listDepth + 1, ordered: n.DataAtom == atom.Ol, inQuote: ctx.inQuote}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Li {
				e.blocks(c, inner)
			}
		}
	case atom.Li:
		e.blocks(n, htmlContext{listDepth: max(ctx.listDepth, 1), ordered: ctx.ordered, inQuote: ctx.inQuote})
	case atom.Blockquote:
		e.blocks(n, htmlContext{listDepth: ctx.listDepth, ordered: ctx.ordered, inQuote: true})
	case atom.Table:
		e.table(n)
	case atom.Hr:
		e.b.Add(cdm.ThematicBreak, "", 0, nil)
	case atom.Figcaption, atom.Caption:
		if text := e.inlineText(childNodes(n)); text != "" {
			e.b.Add(cdm.Caption, text, 0, nil)
		}
	case atom.Math:
		var buf bytes.Buffer
		_ = html.Render(&buf, n)
		e.b.Add(cdm.MathBlock, buf.String(), 0, nil)
	default:
		e.blocks(n, ctx)
	}
}

// inlineBlock — абзац из встроенного содержимого. Абзац из одной картинки
// становится блоком Image.
func (e *htmlEmitter) inlineBlock(nodes []*html.Node, ctx htmlContext) {
	if src, alt, ok := e.soleImage(nodes); ok {
		e.images = append(e.images, src)
		e.b.Add(cdm.Image, "", 0, map[string]string{"src": src, "alt": alt})
		return
	}
	text := e.inlineText(nodes)
	if text == "" {
		return
	}
	switch {
	case ctx.listDepth > 0:
		meta := map[string]string{"depth": strconv.Itoa(ctx.listDepth)}
		if ctx.ordered {
			meta["ordered"] = "true"
		}
		e.b.Add(cdm.ListItem, text, 0, meta)
	case ctx.inQuote:
		e.b.Add(cdm.Blockquote, text, 0, nil)
	default:
		e.b.Add(cdm.Paragraph, text, 0, nil)
	}
}

func (e *htmlEmitter) table(t *html.Node) {
	row := 0
	walkNodes(t, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.DataAtom != atom.Tr {
			return n == t || n.DataAtom != atom.Table // вложенные таблицы не смешиваем с внешней
		}
		col := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || (c.DataAtom != atom.Td && c.DataAtom != atom.Th) {
				continue
			}
			meta := map[string]string{"row": strconv.Itoa(row), "col": strconv.Itoa(col)}
			if c.DataAtom == atom.Th {
				meta["header"] = "true"
			}
			e.b.Add(cdm.TableCell, e.inlineText(childNodes(c)), 0, meta)
			col++
		}
		row++
		return false
	})
}

var (
	spaceRunRe   = regexp.MustCompile(`[ \t\r\n\f]+`)
	multiSpaceRe = regexp.MustCompile(` {2,}`)
	lineEdgeRe   = regexp.MustCompile(` *\n *`)
)

// inlineText — встроенное содержимое в Markdown-синтаксисе. Пробелы
// схлопываются, как в браузере; переносы <br> сохраняются.
func (e *htmlEmitter) inlineText(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		e.inline(&b, n)
	}
	s := multiSpaceRe.ReplaceAllString(b.String(), " ")
	s = lineEdgeRe.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}

func (e *htmlEmitter) inline(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(escapeMarkdown(spaceRunRe.ReplaceAllString(n.Data, " ")))
		return
	case html.ElementNode:
	default:
		return
	}
	children := func() string {
		var inner strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			e.inline(&inner, c)
		}
		return inner.String()
	}
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Template:
	case atom.Em, atom.I, atom.Cite, atom.Dfn, atom.Var:
		wrapMarker(b, children(), "*")
	case atom.Strong, atom.B:
		wrapMarker(b, children(), "**")
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		if code := spaceRunRe.ReplaceAllString(textContent(n), " "); strings.TrimSpace(code) != "" {
			b.WriteString("`" + strings.ReplaceAll(code, "`", "'") + "`")
		}
	case atom.A:
		inner := children()
		href := attr(n, "href")
		if href == "" || strings.TrimSpace(inner) == "" {
			b.WriteString(inner) // якорь-метка вроде <a id="page12"/>
			return
		}
		b.WriteString("[" + strings.TrimSpace(inner) + "](" + href + ")")
	case atom.Br:
		b.WriteString("\n")
	case atom.Img:
		if resolved, ok := e.resolveImage(attr(n, "src")); ok {
			e.images = append(e.images, resolved)
			b.WriteString("![" + escapeMarkdown(attr(n, "alt")) + "](" + resolved + ")")
		}
	case atom.Svg:
		if src := e.svgImage(n); src != "" {
			e.images = append(e.images, src)
			b.WriteString("![](" + src + ")")
		}
	default:
		inner := children()
		if blockTags[n.DataAtom] {
			inner += "\n" // блочный элемент внутри ячейки или подписи
		}
		b.WriteString(inner)
	}
}

// wrapMarker обрамляет текст маркером разметки, вынося пробелы наружу:
// «* x *» — не курсив в Markdown, а «*x*» — курсив.
func wrapMarker(b *strings.Builder, inner, marker string) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		b.WriteString(inner)
		return
	}
	if strings.HasPrefix(inner, " ") {
		b.WriteString(" ")
	}
	b.WriteString(marker + trimmed + marker)
	if strings.HasSuffix(inner, " ") {
		b.WriteString(" ")
	}
}

var markdownEscaper = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, "`", "\\`", `[`, `\[`, `]`, `\]`)

// escapeMarkdown экранирует символы разметки в тексте: иначе «2*3*4» при
// экспорте превратилось бы в курсив.
func escapeMarkdown(s string) string { return markdownEscaper.Replace(s) }

func (e *htmlEmitter) soleImage(nodes []*html.Node) (src, alt string, ok bool) {
	var found *html.Node
	for _, n := range nodes {
		switch {
		case n.Type == html.TextNode && strings.TrimSpace(n.Data) == "":
		case n.Type == html.ElementNode && (n.DataAtom == atom.Img || n.DataAtom == atom.Svg) && found == nil:
			found = n
		default:
			return "", "", false
		}
	}
	if found == nil {
		return "", "", false
	}
	if found.DataAtom == atom.Svg {
		if s := e.svgImage(found); s != "" {
			return s, "", true
		}
		return "", "", false
	}
	if s, ok := e.resolveImage(attr(found, "src")); ok {
		return s, attr(found, "alt"), true
	}
	return "", "", false
}

// svgImage — картинка обложки вида <svg><image xlink:href="cover.jpg"/></svg>.
func (e *htmlEmitter) svgImage(n *html.Node) string {
	var src string
	walkNodes(n, func(c *html.Node) bool {
		if c.Type == html.ElementNode && c.Data == "image" {
			src = attr(c, "href")
			return false
		}
		return src == ""
	})
	resolved, _ := e.resolveImage(src)
	return resolved
}

// --- помощники DOM ---

func childNodes(n *html.Node) []*html.Node {
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
	}
	return out
}

// attr ищет атрибут по локальному имени в любом пространстве имён
// (href и xlink:href у SVG).
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key || strings.HasSuffix(a.Key, ":"+key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func walkNodes(n *html.Node, visit func(*html.Node) bool) {
	if !visit(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkNodes(c, visit)
	}
}

func findElement(n *html.Node, a atom.Atom) *html.Node {
	var found *html.Node
	walkNodes(n, func(c *html.Node) bool {
		if found != nil {
			return false
		}
		if c.Type == html.ElementNode && c.DataAtom == a {
			found = c
			return false
		}
		return true
	})
	return found
}

func firstHeadingNode(n *html.Node) *html.Node {
	var found *html.Node
	walkNodes(n, func(c *html.Node) bool {
		if found != nil {
			return false
		}
		if c.Type == html.ElementNode {
			switch c.DataAtom {
			case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
				found = c
				return false
			}
		}
		return true
	})
	return found
}

func textContent(n *html.Node) string {
	var b strings.Builder
	walkNodes(n, func(c *html.Node) bool {
		if c.Type == html.ElementNode && skippedTags[c.DataAtom] {
			return false
		}
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		return true
	})
	return b.String()
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

var languageClassRe = regexp.MustCompile(`(?:^|\s)(?:language|lang)-([\w+#-]+)`)

func languageFromClass(class string) string {
	if m := languageClassRe.FindStringSubmatch(class); m != nil {
		return m[1]
	}
	return ""
}

// cleanHref разрешает ссылку из файла в каталоге dir в путь от корня книги:
// без фрагмента, с раскодированными %XX (href в OPF и NCX — это URL).
func cleanHref(dir, href string) string {
	if i := strings.IndexAny(href, "#?"); i >= 0 {
		href = href[:i]
	}
	if href == "" || schemeRe.MatchString(href) {
		return href
	}
	if unescaped, err := url.PathUnescape(href); err == nil {
		href = unescaped
	}
	return path.Join(dir, href)
}

func firstNonEmpty(values []string) string {
	for _, v := range values {
		if v = collapseSpaces(v); v != "" {
			return v
		}
	}
	return ""
}

func trimAll(values []string) []string {
	var out []string
	for _, v := range values {
		if v = collapseSpaces(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
