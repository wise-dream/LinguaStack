package importers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vnovels/linguastack/internal/cdm"
)

// DOCX — импорт документов Word (ТЗ §3.1: заголовки, абзацы, списки,
// таблицы, изображения; дополнение §15.2).
//
// DOCX — это ZIP с частями OOXML. Тело (word/document.xml) читается
// потоково, по элементу верхнего уровня; стили, нумерация, связи и сноски
// — небольшие части, они разбираются целиком. Оформление абзаца стилем
// (жирный заголовок) — вёрстка, а не разметка текста: маркеры ** и *
// ставятся только по прямому оформлению прогонов и символьным стилям.
type DOCX struct {
	limits ArchiveLimits
}

// NewDOCX создаёт импортёр DOCX.
func NewDOCX(limits ArchiveLimits) Importer { return DOCX{limits: limits} }

func (DOCX) FormatID() string { return "docx" }

// Sniff узнаёт DOCX и по содержимому: документ с расширением .zip не должен
// уйти в импорт репозитория. Читается только центральный каталог архива.
func (DOCX) Sniff(data []byte, filename string) bool {
	return fileExt(filename) == ".docx" || isDOCXContent(data)
}

func isDOCXContent(data []byte) bool {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return false
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			return true
		}
	}
	return false
}

// unsupportedImageExtensions — форматы картинок Word, которые браузер не
// показывает: в книгу они не попадают, пользователь видит замечание.
var unsupportedImageExtensions = map[string]bool{
	".emf": true, ".wmf": true, ".emz": true, ".wmz": true, ".tif": true, ".tiff": true,
}

func (x DOCX) Load(data []byte, filename string) (Result, error) {
	fallback := cdm.BookMeta{Title: titleFromFilename(filename), SourceFormat: "docx"}
	corrupt := func() (Result, error) {
		return finish(cdm.NewBuilder(fallback).Build(), []Issue{corruptIssue("DOCX")}), nil
	}
	archive, err := openArchive(data, x.limits)
	if err != nil {
		return Result{}, err
	}
	if archive.corrupt {
		return corrupt()
	}
	pkg := newArchiveBook(archive)
	body, ok, err := pkg.read("word/document.xml")
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return corrupt()
	}
	part := func(name string) (*xmlNode, error) {
		raw, ok, err := pkg.read(name)
		if err != nil || !ok {
			return nil, err
		}
		n, perr := parseXMLNode(raw)
		if perr != nil {
			pkg.issues = append(pkg.issues, Issue{Severity: Warning, Code: "corrupt_entry",
				Message: fmt.Sprintf("Не разобрать %q; документ прочитан без этой части.", name)})
			return nil, nil
		}
		return n, nil
	}
	c := &docxConverter{notes: map[string]*xmlNode{}}
	var styleLang string
	parts := map[string]func(*xmlNode){
		"word/styles.xml":              func(n *xmlNode) { c.styles, styleLang = parseDocxStyles(n) },
		"word/numbering.xml":           func(n *xmlNode) { c.numbering = parseDocxNumbering(n) },
		"word/_rels/document.xml.rels": func(n *xmlNode) { c.rels = parseDocxRels(n) },
		"word/footnotes.xml":           func(n *xmlNode) { c.addNotes(n, "footnote") },
		"word/endnotes.xml":            func(n *xmlNode) { c.addNotes(n, "endnote") },
	}
	for _, name := range []string{"word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels",
		"word/footnotes.xml", "word/endnotes.xml"} {
		n, err := part(name)
		if err != nil {
			return Result{}, err
		}
		if n != nil {
			parts[name](n)
		}
	}
	core, err := part("docProps/core.xml")
	if err != nil {
		return Result{}, err
	}

	meta := fallback
	coreTitle := false
	for _, n := range core.childrenOrNil() {
		switch value := collapseSpaces(n.Text); n.local() {
		case "title":
			if value != "" {
				meta.Title, coreTitle = value, true
			}
		case "creator":
			meta.Author = value
		case "language":
			meta.Language = value
		}
	}
	if meta.Language == "" {
		meta.Language = styleLang
	}

	c.b = cdm.NewBuilder(meta)
	if err := streamBody(body, c.bodyElement); err != nil {
		pkg.issues = append(pkg.issues, Issue{Severity: Warning, Code: "corrupt_entry",
			Message: "Документ повреждён: прочитана часть до места повреждения."})
	}
	c.flushCode()
	c.flushNotes()

	doc := c.b.Build()
	if !coreTitle && c.title != "" {
		doc.Meta.Title = c.title
	}
	result := finish(doc, pkg.issues)
	var missing []string
	result.Assets, missing = collectAssets(pkg.files, c.images)
	if len(missing) > 0 {
		result.Issues = append(result.Issues, missingResourceIssue(missing))
	}
	if len(c.unsupported) > 0 {
		result.Issues = append(result.Issues, Issue{Severity: Warning, Code: "unsupported_image",
			Message: "Изображения в форматах, которые браузер не показывает (EMF, WMF, TIFF), пропущены: " +
				listNames(uniqueStrings(c.unsupported))})
	}
	if c.remote > 0 {
		result.Issues = append(result.Issues, Issue{Severity: Info, Code: "remote_images_skipped",
			Message: fmt.Sprintf("Внешние изображения не загружаются (%d): сервер не обращается к адресам из загруженных файлов.", c.remote)})
	}
	return result, nil
}

func (n *xmlNode) childrenOrNil() []*xmlNode {
	if n == nil {
		return nil
	}
	out := make([]*xmlNode, len(n.Nodes))
	for i := range n.Nodes {
		out[i] = &n.Nodes[i]
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// --- стили, нумерация, связи ---

type docxStyle struct {
	name    string // нижний регистр: «heading 1», «source code»
	basedOn string
	pPr     *xmlNode
	rPr     *xmlNode
}

type docxStyles map[string]*docxStyle

func parseDocxStyles(root *xmlNode) (docxStyles, string) {
	styles := docxStyles{}
	for _, st := range root.children("style") {
		styles[st.attr("styleId")] = &docxStyle{
			name:    strings.ToLower(collapseSpaces(st.child("name").attr("val"))),
			basedOn: st.child("basedOn").attr("val"),
			pPr:     st.child("pPr"),
			rPr:     st.child("rPr"),
		}
	}
	return styles, root.path("docDefaults", "rPrDefault", "rPr", "lang").attr("val")
}

// chain — стиль и его предки по basedOn (с защитой от циклов).
func (s docxStyles) chain(id string) []*docxStyle {
	var out []*docxStyle
	seen := map[string]bool{}
	for id != "" && !seen[id] && len(out) < 16 {
		seen[id] = true
		st := s[id]
		if st == nil {
			break
		}
		out = append(out, st)
		id = st.basedOn
	}
	return out
}

// names — имена стиля по цепочке. Неизвестный стиль (документ без
// styles.xml) узнаётся по самому идентификатору: «Heading1», «SourceCode».
func (s docxStyles) names(id string) []string {
	chain := s.chain(id)
	if len(chain) == 0 {
		if id == "" {
			return nil
		}
		return []string{strings.ToLower(id)}
	}
	out := make([]string, len(chain))
	for i, st := range chain {
		out[i] = st.name
	}
	return out
}

type docxKind struct {
	heading                   int
	toc, code, quote, caption bool
}

var (
	headingStyleRe = regexp.MustCompile(`^heading\s*([1-9])$`)
	tocStyleRe     = regexp.MustCompile(`^toc\s*[1-9]$`)
)

var (
	codeStyleNames  = map[string]bool{"source code": true, "html preformatted": true, "plain text": true, "preformatted text": true}
	quoteStyleNames = map[string]bool{"quote": true, "intense quote": true, "block text": true}
)

func outlineLevel(pPr *xmlNode) (int, bool) {
	v := pPr.child("outlineLvl").attr("val")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 8 {
		return 0, false // 9 — «основной текст»
	}
	return n + 1, true
}

// paragraphKind — роль абзаца по его стилю (дополнение §15.2).
func (s docxStyles) paragraphKind(styleID string, pPr *xmlNode) docxKind {
	var k docxKind
	if level, ok := outlineLevel(pPr); ok {
		k.heading = level
	}
	for _, name := range s.names(styleID) {
		if m := headingStyleRe.FindStringSubmatch(name); m != nil {
			if k.heading == 0 {
				k.heading = int(m[1][0] - '0')
			}
			return k
		}
		switch {
		case name == "title":
			if k.heading == 0 {
				k.heading = 1
			}
			return k
		case tocStyleRe.MatchString(name) || name == "toc heading" || name == "table of figures":
			k.toc = true
			return k
		case codeStyleNames[name] || (strings.Contains(name, "code") && !strings.Contains(name, "char")):
			k.code = true
			return k
		case quoteStyleNames[name]:
			k.quote = true
			return k
		case name == "caption":
			k.caption = true
			return k
		}
	}
	if k.heading == 0 {
		for _, st := range s.chain(styleID) {
			if level, ok := outlineLevel(st.pPr); ok {
				k.heading = level
				break
			}
		}
	}
	return k
}

// numPr — нумерация абзаца: своя или из стиля. numId "0" снимает
// нумерацию, унаследованную от стиля.
func (s docxStyles) numPr(styleID string, pPr *xmlNode) (numID, ilvl string) {
	if np := pPr.child("numPr"); np != nil {
		numID, ilvl = np.child("numId").attr("val"), np.child("ilvl").attr("val")
	}
	if numID == "" {
		for _, st := range s.chain(styleID) {
			if np := st.pPr.child("numPr"); np != nil {
				numID = np.child("numId").attr("val")
				if ilvl == "" {
					ilvl = np.child("ilvl").attr("val")
				}
				break
			}
		}
	}
	if ilvl == "" {
		ilvl = "0"
	}
	return numID, ilvl
}

// docxNumbering — формат нумерации: numId → ilvl → numFmt.
type docxNumbering map[string]map[string]string

func parseDocxNumbering(root *xmlNode) docxNumbering {
	levels := func(n *xmlNode, into map[string]string) {
		for _, lvl := range n.children("lvl") {
			if f := lvl.child("numFmt").attr("val"); f != "" {
				into[lvl.attr("ilvl")] = f
			}
		}
	}
	abstract := map[string]map[string]string{}
	for _, a := range root.children("abstractNum") {
		formats := map[string]string{}
		levels(a, formats)
		abstract[a.attr("abstractNumId")] = formats
	}
	out := docxNumbering{}
	for _, num := range root.children("num") {
		formats := map[string]string{}
		for k, v := range abstract[num.child("abstractNumId").attr("val")] {
			formats[k] = v
		}
		for _, o := range num.children("lvlOverride") {
			levels(o, formats)
		}
		out[num.attr("numId")] = formats
	}
	return out
}

// ordered: без numbering.xml формат неизвестен — маркированный список.
func (n docxNumbering) ordered(numID, ilvl string) bool {
	switch f := n[numID][ilvl]; f {
	case "", "bullet", "none":
		return false
	}
	return true
}

type docxRel struct {
	target   string
	external bool
}

func parseDocxRels(root *xmlNode) map[string]docxRel {
	out := map[string]docxRel{}
	for _, r := range root.children("Relationship") {
		out[r.attr("Id")] = docxRel{target: r.attr("Target"), external: strings.EqualFold(r.attr("TargetMode"), "External")}
	}
	return out
}

// --- тело документа ---

type docxConverter struct {
	b           *cdm.Builder
	styles      docxStyles
	numbering   docxNumbering
	rels        map[string]docxRel
	notes       map[string]*xmlNode // "footnote:2" → сноска
	noteSeq     int
	pending     []docxNote // сноски текущей главы
	code        []string   // строки накапливаемого блока кода
	title       string     // текст первого абзаца стиля Title
	images      []string
	unsupported []string
	remote      int
}

type docxNote struct {
	number int
	node   *xmlNode
}

func (c *docxConverter) addNotes(root *xmlNode, kind string) {
	for _, n := range root.children(kind) {
		switch n.attr("type") {
		case "separator", "continuationSeparator", "continuationNotice":
			continue
		}
		c.notes[kind+":"+n.attr("id")] = n
	}
}

func (c *docxConverter) bodyElement(n *xmlNode) {
	switch n.local() {
	case "p":
		c.paragraph(n)
	case "tbl":
		c.flushCode()
		c.table(n)
	case "sdt":
		if isTOCBlock(n) {
			return
		}
		c.bodyChildren(n.child("sdtContent"))
	case "customXml", "ins", "moveTo":
		c.bodyChildren(n)
	case "AlternateContent":
		c.bodyChildren(n.child("Choice"))
	}
}

func (c *docxConverter) bodyChildren(n *xmlNode) {
	for _, ch := range n.childrenOrNil() {
		c.bodyElement(ch)
	}
}

// isTOCBlock — оглавление, которое Word собирает полем: сгенерированный
// текст, а не содержимое книги.
func isTOCBlock(sdt *xmlNode) bool {
	gallery := sdt.path("sdtPr", "docPartObj", "docPartGallery").attr("val")
	return strings.EqualFold(gallery, "Table of Contents")
}

func (c *docxConverter) paragraph(p *xmlNode) {
	pPr := p.child("pPr")
	styleID := pPr.child("pStyle").attr("val")
	kind := c.styles.paragraphKind(styleID, pPr)
	if kind.toc {
		return
	}
	notesBefore := len(c.pending)
	var boxes []*xmlNode
	segs := c.inline(p, "", nil, &boxes)
	if kind.code {
		c.code = append(c.code, plainSegments(segs))
		c.bodyBoxes(boxes)
		return
	}
	c.flushCode()

	text := renderSegments(segs)
	switch {
	case imagesOnly(segs):
		for _, s := range segs {
			if s.image != "" {
				c.b.Add(cdm.Image, "", 0, map[string]string{"src": s.image, "alt": s.alt})
			}
		}
	case text == "":
	case kind.heading > 0:
		level := min(kind.heading, 6)
		// Сноски самого заголовка относятся к новой главе.
		carried := append([]docxNote(nil), c.pending[notesBefore:]...)
		c.pending = c.pending[:notesBefore]
		c.flushNotes()
		title := collapseSpaces(plainSegments(segs))
		if c.title == "" && c.isTitleStyle(styleID) {
			c.title = title
		}
		c.b.StartChapter(title, level)
		c.b.Add(cdm.Heading, text, level, nil)
		c.pending = append(c.pending, carried...)
	default:
		c.textBlock(kind, styleID, pPr, text)
	}
	c.bodyBoxes(boxes)
}

func (c *docxConverter) isTitleStyle(styleID string) bool {
	names := c.styles.names(styleID)
	return len(names) > 0 && names[0] == "title"
}

func (c *docxConverter) textBlock(kind docxKind, styleID string, pPr *xmlNode, text string) {
	if numID, ilvl := c.styles.numPr(styleID, pPr); numID != "" && numID != "0" {
		depth := 1
		if n, err := strconv.Atoi(ilvl); err == nil && n >= 0 && n < 9 {
			depth = n + 1
		}
		meta := map[string]string{"depth": strconv.Itoa(depth)}
		if c.numbering.ordered(numID, ilvl) {
			meta["ordered"] = "true"
		}
		c.b.Add(cdm.ListItem, text, 0, meta)
		return
	}
	switch {
	case kind.quote:
		c.b.Add(cdm.Blockquote, text, 0, nil)
	case kind.caption:
		c.b.Add(cdm.Caption, text, 0, nil)
	default:
		c.b.Add(cdm.Paragraph, text, 0, nil)
	}
}

// bodyBoxes — абзацы текстовых полей идут отдельными блоками после
// абзаца, к которому поле привязано.
func (c *docxConverter) bodyBoxes(boxes []*xmlNode) {
	for _, box := range boxes {
		c.bodyChildren(box)
	}
}

func (c *docxConverter) flushCode() {
	if len(c.code) == 0 {
		return
	}
	c.b.Add(cdm.CodeBlock, strings.Join(c.code, "\n"), 0, nil)
	c.code = nil
}

// flushNotes добавляет сноски главы её последними абзацами.
func (c *docxConverter) flushNotes() {
	c.flushCode()
	for _, note := range c.pending {
		label := fmt.Sprintf("[%d] ", note.number)
		segs := []docxSeg{{text: label}}
		for _, p := range note.node.children("p") {
			segs = append(segs, docxSeg{text: " "})
			segs = c.inline(p, "", segs, nil)
		}
		if text := renderSegments(segs); text != "" {
			c.b.Add(cdm.Paragraph, text, 0, map[string]string{"note": strconv.Itoa(note.number)})
		}
	}
	c.pending = nil
}

func (c *docxConverter) table(tbl *xmlNode) {
	row := 0
	for _, tr := range tbl.children("tr") {
		_, header := tr.path("trPr", "tblHeader").onOff()
		col := 0
		for _, tc := range tr.children("tc") {
			span := 1
			if n, err := strconv.Atoi(tc.path("tcPr", "gridSpan").attr("val")); err == nil && n > 1 {
				span = n
			}
			vMerge := tc.path("tcPr", "vMerge")
			if vMerge == nil || vMerge.attr("val") == "restart" {
				meta := map[string]string{"row": strconv.Itoa(row), "col": strconv.Itoa(col)}
				if header {
					meta["header"] = "true"
				}
				c.b.Add(cdm.TableCell, c.cellText(tc), 0, meta)
			}
			col += span
		}
		row++
	}
}

// cellText — абзацы ячейки строками; вложенная таблица — строками своих
// ячеек.
func (c *docxConverter) cellText(cell *xmlNode) string {
	var lines []string
	for _, ch := range cell.childrenOrNil() {
		switch ch.local() {
		case "p":
			if text := renderSegments(c.inline(ch, "", nil, nil)); text != "" {
				lines = append(lines, text)
			}
		case "tbl":
			for _, tr := range ch.children("tr") {
				for _, tc := range tr.children("tc") {
					if text := c.cellText(tc); text != "" {
						lines = append(lines, text)
					}
				}
			}
		case "sdt":
			if text := c.cellText(ch.child("sdtContent")); text != "" {
				lines = append(lines, text)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// --- прогоны ---

// docxSeg — кусок встроенного содержимого абзаца.
type docxSeg struct {
	text               string
	bold, italic, code bool
	link               string
	image, alt         string // image != "" — картинка
}

func (s docxSeg) sameFormat(o docxSeg) bool {
	return s.bold == o.bold && s.italic == o.italic && s.code == o.code && s.link == o.link
}

// inline собирает содержимое абзаца. Удалённое при рецензировании (w:del,
// w:moveFrom) и запасной вариант mc:Fallback, дублирующий mc:Choice,
// пропускаются.
func (c *docxConverter) inline(n *xmlNode, link string, out []docxSeg, boxes *[]*xmlNode) []docxSeg {
	for _, ch := range n.childrenOrNil() {
		switch ch.local() {
		case "r":
			out = c.run(ch, ch, link, out, boxes)
		case "hyperlink":
			target := ""
			if rel, ok := c.rels[ch.attr("id")]; ok && rel.external && isExternalLink(rel.target) {
				target = rel.target
			}
			out = c.inline(ch, target, out, boxes)
		case "ins", "moveTo", "smartTag", "fldSimple", "customXml", "bdo", "dir":
			out = c.inline(ch, link, out, boxes)
		case "sdt":
			out = c.inline(ch.child("sdtContent"), link, out, boxes)
		case "AlternateContent":
			out = c.inline(ch.child("Choice"), link, out, boxes)
		}
	}
	return out
}

var externalLinkRe = regexp.MustCompile(`(?i)^(https?|mailto):`)

func isExternalLink(href string) bool { return externalLinkRe.MatchString(href) }

// run — содержимое прогона. content — сам прогон или mc:Choice внутри него;
// оформление берётся из прогона r.
func (c *docxConverter) run(r, content *xmlNode, link string, out []docxSeg, boxes *[]*xmlNode) []docxSeg {
	format := c.runFormat(r.child("rPr"))
	format.link = link
	add := func(text string) {
		s := format
		s.text = text
		out = append(out, s)
	}
	for _, ch := range content.childrenOrNil() {
		switch ch.local() {
		case "t":
			add(ch.Text)
		case "tab":
			add("\t")
		case "br", "cr":
			if t := ch.attr("type"); t != "page" && t != "column" {
				add("\n")
			}
		case "noBreakHyphen":
			add("-")
		case "footnoteReference", "endnoteReference":
			c.noteSeq++
			kind := strings.TrimSuffix(ch.local(), "Reference")
			if node, ok := c.notes[kind+":"+ch.attr("id")]; ok {
				c.pending = append(c.pending, docxNote{number: c.noteSeq, node: node})
			}
			plain := docxSeg{text: fmt.Sprintf("[%d]", c.noteSeq), link: link}
			out = append(out, plain)
		case "drawing", "pict", "object":
			out = c.graphic(ch, out, boxes)
		case "AlternateContent":
			out = c.run(r, ch.child("Choice"), link, out, boxes)
		}
	}
	return out
}

// graphic — картинка (DrawingML a:blip или VML v:imagedata) и текстовые
// поля внутри рисунка.
func (c *docxConverter) graphic(g *xmlNode, out []docxSeg, boxes *[]*xmlNode) []docxSeg {
	alt := ""
	if pr := g.find("docPr"); pr != nil {
		alt = collapseSpaces(pr.attr("descr"))
		if alt == "" {
			alt = collapseSpaces(pr.attr("title"))
		}
	}
	if blip := g.find("blip"); blip != nil {
		if id := blip.attr("embed"); id != "" {
			out = c.image(id, alt, out)
		} else if blip.attr("link") != "" {
			c.remote++
		}
	} else if img := g.find("imagedata"); img != nil {
		out = c.image(img.attr("id"), alt, out)
	}
	if boxes != nil {
		g.walk(func(n *xmlNode) bool {
			if n.local() == "txbxContent" {
				*boxes = append(*boxes, n)
				return false
			}
			return true
		})
	}
	return out
}

func (c *docxConverter) image(relID, alt string, out []docxSeg) []docxSeg {
	rel, ok := c.rels[relID]
	if !ok || rel.target == "" {
		return out
	}
	if rel.external {
		c.remote++
		return out
	}
	var name string
	if strings.HasPrefix(rel.target, "/") {
		name = cleanHref("", strings.TrimPrefix(rel.target, "/"))
	} else {
		name = cleanHref("word", rel.target)
	}
	if unsupportedImageExtensions[fileExt(name)] {
		c.unsupported = append(c.unsupported, name)
		return out
	}
	c.images = append(c.images, name)
	return append(out, docxSeg{image: name, alt: alt})
}

// monospaceFonts — шрифты кода; плюс любой с «mono» в названии.
var monospaceFonts = map[string]bool{
	"courier": true, "courier new": true, "consolas": true, "menlo": true, "monaco": true, "lucida console": true,
	"source code pro": true, "fira code": true, "cascadia code": true, "andale mono": true,
}

func isMonospaceFont(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return monospaceFonts[name] || strings.Contains(name, "mono")
}

// runFormat — прямое оформление прогона поверх символьного стиля.
func (c *docxConverter) runFormat(rPr *xmlNode) docxSeg {
	var f docxSeg
	apply := func(props *xmlNode) {
		if present, v := props.child("b").onOff(); present {
			f.bold = v
		}
		if present, v := props.child("i").onOff(); present {
			f.italic = v
		}
		if fonts := props.child("rFonts"); fonts != nil && (isMonospaceFont(fonts.attr("ascii")) || isMonospaceFont(fonts.attr("hAnsi"))) {
			f.code = true
		}
	}
	chain := c.styles.chain(rPr.child("rStyle").attr("val"))
	for i := len(chain) - 1; i >= 0; i-- {
		apply(chain[i].rPr)
		if n := chain[i].name; strings.Contains(n, "code") || strings.Contains(n, "verbatim") || strings.Contains(n, "teletype") {
			f.code = true
		}
	}
	apply(rPr)
	return f
}

// --- сборка текста ---

func imagesOnly(segs []docxSeg) bool {
	found := false
	for _, s := range segs {
		switch {
		case s.image != "":
			found = true
		case strings.TrimSpace(s.text) != "":
			return false
		}
	}
	return found
}

// plainSegments — текст без разметки: код и названия глав.
func plainSegments(segs []docxSeg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}

// renderSegments — встроенное содержимое в Markdown-синтаксисе: соседние
// прогоны с одинаковым оформлением сливаются (иначе «**раз****два**»).
func renderSegments(segs []docxSeg) string {
	var b strings.Builder
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.image != "" {
			b.WriteString("![" + escapeMarkdown(s.alt) + "](" + s.image + ")")
			i++
			continue
		}
		var text strings.Builder
		j := i
		for ; j < len(segs) && segs[j].image == "" && segs[j].sameFormat(s); j++ {
			text.WriteString(segs[j].text)
		}
		b.WriteString(formatSegment(strings.ReplaceAll(text.String(), "\t", " "), s))
		i = j
	}
	out := multiSpaceRe.ReplaceAllString(b.String(), " ")
	out = lineEdgeRe.ReplaceAllString(out, "\n")
	return strings.TrimSpace(out)
}

func formatSegment(text string, f docxSeg) string {
	var b strings.Builder
	switch {
	case f.code:
		if strings.TrimSpace(text) == "" {
			b.WriteString(text)
		} else {
			wrapMarker(&b, strings.ReplaceAll(text, "`", "'"), "`")
		}
	case f.bold && f.italic:
		wrapMarker(&b, escapeMarkdown(text), "***")
	case f.bold:
		wrapMarker(&b, escapeMarkdown(text), "**")
	case f.italic:
		wrapMarker(&b, escapeMarkdown(text), "*")
	default:
		b.WriteString(escapeMarkdown(text))
	}
	out := b.String()
	if f.link == "" || strings.TrimSpace(out) == "" {
		return out
	}
	lead := out[:len(out)-len(strings.TrimLeft(out, " "))]
	trail := out[len(strings.TrimRight(out, " ")):]
	return lead + "[" + strings.TrimSpace(out) + "](" + f.link + ")" + trail
}
