package exporters

import (
	"archive/zip"
	"bytes"
	"html"
	"image"
	_ "image/gif"  // размеры картинок GIF
	_ "image/jpeg" // и JPEG для wp:extent
	_ "image/png"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"

	"github.com/vnovels/linguastack/internal/cdm"
)

// docxExporter — DOCX: минимальный набор частей OOXML со своими стилями
// заголовков, списков, кода, цитат и оригинала (дополнение §16). Стили
// названы так же, как у Word («heading 1», «Source Code»), — документ
// открывается Word и LibreOffice и снова импортируется нашим DOCX.
type docxExporter struct{}

func (docxExporter) Format() string { return "docx" }

func (docxExporter) Describe(*Book) Output {
	return Output{Ext: ".docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
}

// docxImageTypes — картинки, которые Word показывает и размер которых
// умеет прочитать image.DecodeConfig.
var docxImageTypes = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

const (
	emuPerPixel = 9525    // 914400 EMU на дюйм / 96 точек
	maxImageEMU = 5486400 // 6 дюймов — ширина полосы набора A4
)

// imageExtent — размер картинки в EMU, не шире полосы набора.
func imageExtent(width, height int) (cx, cy int64) {
	cx, cy = int64(width)*emuPerPixel, int64(height)*emuPerPixel
	if cx > maxImageEMU {
		cy = cy * maxImageEMU / cx
		cx = maxImageEMU
	}
	return cx, cy
}

type docxImage struct {
	rel    string
	name   string
	cx, cy int64
}

type docxWriter struct {
	book     *Book
	body     strings.Builder
	rels     []string // связи документа после стилей и нумерации
	images   map[string]*docxImage
	nums     []string // элементы w:num: нумерованные списки начинаются с 1
	drawings int
}

func (x *docxWriter) rel(kind, target string, external bool) string {
	id := "rId" + strconv.Itoa(len(x.rels)+3)
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	x.rels = append(x.rels, `<Relationship Id="`+id+`" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/`+
		kind+`" Target="`+html.EscapeString(target)+`"`+mode+`/>`)
	return id
}

func (docxExporter) Write(w io.Writer, b *Book) error {
	x := &docxWriter{book: b, images: map[string]*docxImage{}}
	zw := zip.NewWriter(w)
	put := func(name string, data []byte) error {
		f, err := createZipEntry(zw, name)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}

	order, names := bookImages(b)
	for _, src := range order {
		if !docxImageTypes[extOf(names[src])] {
			continue
		}
		data, err := b.Assets(src)
		if err != nil {
			continue
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
			continue
		}
		if err := put("word/media/"+names[src], data); err != nil {
			return err
		}
		cx, cy := imageExtent(cfg.Width, cfg.Height)
		x.images[src] = &docxImage{rel: x.rel("image", "media/"+names[src], false), name: names[src], cx: cx, cy: cy}
	}

	var walk func([]Chapter, int)
	walk = func(chs []Chapter, depth int) {
		for i := range chs {
			for _, group := range groupBlocks(displayBlocks(&chs[i], depth)) {
				x.group(group)
			}
			walk(chs[i].Children, depth+1)
		}
	}
	walk(b.Chapters, 1)

	lang := html.EscapeString(b.Lang)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", docxPackageRels},
		{"docProps/core.xml", x.core()},
		{"word/document.xml", docxDocumentStart + x.body.String() + docxSection + "</w:body></w:document>"},
		{"word/styles.xml", strings.ReplaceAll(docxStyles, "{lang}", lang)},
		{"word/numbering.xml", docxNumberingStart + strings.Join(x.nums, "") + "</w:numbering>"},
		{"word/_rels/document.xml.rels", x.documentRels()},
	}
	for _, p := range parts {
		if err := put(p.name, []byte(p.body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

func (x *docxWriter) core() string {
	esc := func(s string) string { return html.EscapeString(xmlSafe(s)) }
	author := ""
	if x.book.Author != "" {
		author = "<dc:creator>" + esc(x.book.Author) + "</dc:creator>"
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
		"<dc:title>" + esc(x.book.Title) + "</dc:title>" + author +
		"<dc:language>" + esc(x.book.Lang) + "</dc:language>" +
		`<dcterms:created xsi:type="dcterms:W3CDTF">` + time.Now().UTC().Format("2006-01-02T15:04:05Z") + "</dcterms:created>" +
		"</cp:coreProperties>"
}

func (x *docxWriter) documentRels() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
		strings.Join(x.rels, "") + "</Relationships>"
}

// --- блоки ---

func (x *docxWriter) para(style string, extra string, runs string) {
	ppr := ""
	if style != "" || extra != "" {
		ppr = "<w:pPr>"
		if style != "" {
			ppr += `<w:pStyle w:val="` + style + `"/>`
		}
		ppr += extra + "</w:pPr>"
	}
	x.body.WriteString("<w:p>" + ppr + runs + "</w:p>")
}

// texts — абзацы блока: в двуязычном режиме оригинал стилем Original,
// затем перевод стилем блока.
func (x *docxWriter) texts(blk Block, style, extra string) {
	if x.book.bilingual(blk) {
		x.para("Original", extra, x.runs(blk.Original))
		x.para(style, extra, x.runs(blk.Translation))
		return
	}
	x.para(style, extra, x.runs(blk.Text()))
}

func (x *docxWriter) group(blocks []Block) {
	switch blocks[0].Type {
	case cdm.ListItem:
		x.list(blocks)
		return
	case cdm.TableCell:
		x.table(blocks)
		return
	}
	blk := blocks[0]
	switch blk.Type {
	case cdm.Heading:
		style := "Heading" + strconv.Itoa(min(max(blk.Level, 1), 6))
		if x.book.bilingual(blk) {
			x.para(style, "", x.runs(blk.Translation))
			x.para("Original", "", x.runs(blk.Original))
			return
		}
		x.para(style, "", x.runs(blk.Text()))
	case cdm.CodeBlock, cdm.MathBlock:
		for _, line := range strings.Split(xmlSafe(blk.Original), "\n") {
			x.para("SourceCode", "", codeRuns(line))
		}
	case cdm.Image:
		if img, ok := x.images[blk.Meta["src"]]; ok {
			x.para("", `<w:jc w:val="center"/>`, x.drawing(img, blk.Meta["alt"]))
			return
		}
		x.para("", "", textRun(imageCaption(blk.Meta["alt"]), ""))
	case cdm.ThematicBreak:
		x.para("", `<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="auto"/></w:pBdr>`, "")
	case cdm.Blockquote:
		x.texts(blk, "Quote", "")
	case cdm.Caption:
		x.texts(blk, "Caption", "")
	case cdm.RawHTML:
		if text := rawHTMLText(blk.Original); text != "" {
			x.para("", "", textRun(text, ""))
		}
	default:
		x.texts(blk, "", "")
	}
}

// list — пункты с нумерацией: маркированные — общая нумерация 1,
// каждый нумерованный список — своя (иначе Word продолжит счёт с
// предыдущего списка).
func (x *docxWriter) list(items []Block) {
	numbered := ""
	for i, it := range items {
		numID := "1"
		if ordered(it) {
			if numbered == "" || (i > 0 && !ordered(items[i-1]) && listDepth(it) == 1) {
				numbered = strconv.Itoa(len(x.nums) + 2)
				x.nums = append(x.nums, `<w:num w:numId="`+numbered+`"><w:abstractNumId w:val="1"/></w:num>`)
			}
			numID = numbered
		}
		numPr := `<w:numPr><w:ilvl w:val="` + strconv.Itoa(listDepth(it)-1) + `"/><w:numId w:val="` + numID + `"/></w:numPr>`
		x.texts(it, "ListParagraph", numPr)
	}
}

func (x *docxWriter) table(cells []Block) {
	rows, header := tableGrid(cells)
	cols := len(rows[0])
	x.body.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:left w:val="single" w:sz="4" w:space="0" w:color="999999"/>` +
		`<w:bottom w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:right w:val="single" w:sz="4" w:space="0" w:color="999999"/>` +
		`<w:insideH w:val="single" w:sz="4" w:space="0" w:color="999999"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="999999"/>` +
		`</w:tblBorders></w:tblPr><w:tblGrid>` + strings.Repeat("<w:gridCol/>", cols) + "</w:tblGrid>")
	for i, row := range rows {
		x.body.WriteString("<w:tr>")
		if i == 0 && header {
			x.body.WriteString("<w:trPr><w:tblHeader/></w:trPr>")
		}
		for _, c := range row {
			x.body.WriteString("<w:tc>")
			switch {
			case c == nil:
				x.body.WriteString("<w:p/>")
			case x.book.bilingual(*c):
				x.para("Original", "", x.runs(c.Original))
				x.para("", "", x.runs(c.Translation))
			default:
				x.para("", "", x.runs(c.Text()))
			}
			x.body.WriteString("</w:tc>")
		}
		x.body.WriteString("</w:tr>")
	}
	x.body.WriteString("</w:tbl>")
}

func (x *docxWriter) drawing(img *docxImage, alt string) string {
	x.drawings++
	n := strconv.Itoa(x.drawings)
	cx, cy := strconv.FormatInt(img.cx, 10), strconv.FormatInt(img.cy, 10)
	return `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="` + cx + `" cy="` + cy + `"/>` +
		`<wp:docPr id="` + n + `" name="Picture ` + n + `" descr="` + html.EscapeString(xmlSafe(alt)) + `"/>` +
		`<wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
		`<pic:nvPicPr><pic:cNvPr id="` + n + `" name="` + img.name + `"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="` + img.rel + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>` +
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + cx + `" cy="` + cy + `"/></a:xfrm>` +
		`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}

// --- прогоны ---

// textRun — прогон текста; rPr — содержимое свойств.
func textRun(text, rPr string) string {
	if rPr != "" {
		rPr = "<w:rPr>" + rPr + "</w:rPr>"
	}
	return "<w:r>" + rPr + `<w:t xml:space="preserve">` + html.EscapeString(xmlSafe(text)) + "</w:t></w:r>"
}

// codeRuns — строка кода: табуляции — w:tab.
func codeRuns(line string) string {
	var b strings.Builder
	for i, part := range strings.Split(line, "\t") {
		if i > 0 {
			b.WriteString("<w:r><w:tab/></w:r>")
		}
		if part != "" {
			b.WriteString(textRun(part, ""))
		}
	}
	return b.String()
}

type runFormat struct {
	bold, italic, code, link bool
}

func (f runFormat) rPr() string {
	var b strings.Builder
	switch {
	case f.code:
		b.WriteString(`<w:rStyle w:val="VerbatimChar"/>`)
	case f.link:
		b.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
	}
	if f.bold {
		b.WriteString("<w:b/>")
	}
	if f.italic {
		b.WriteString("<w:i/>")
	}
	return b.String()
}

// runs — встроенная разметка прогонами Word.
func (x *docxWriter) runs(text string) string {
	doc, src := parseInline(xmlSafe(text))
	var b strings.Builder
	for p := doc.FirstChild(); p != nil; p = p.NextSibling() {
		if p != doc.FirstChild() {
			b.WriteString("<w:r><w:br/></w:r>")
		}
		x.inlineChildren(&b, p, src, runFormat{})
	}
	return b.String()
}

func (x *docxWriter) inlineChildren(b *strings.Builder, n ast.Node, src []byte, f runFormat) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		x.inlineNode(b, c, src, f)
	}
}

func (x *docxWriter) inlineNode(b *strings.Builder, n ast.Node, src []byte, f runFormat) {
	switch n := n.(type) {
	case *ast.Text:
		b.WriteString(textRun(textValue(n.Segment.Value(src)), f.rPr()))
		switch {
		case n.HardLineBreak() || (n.SoftLineBreak() && x.book.HardBreaks):
			b.WriteString("<w:r><w:br/></w:r>")
		case n.SoftLineBreak():
			b.WriteString(textRun(" ", f.rPr()))
		}
	case *ast.String:
		b.WriteString(textRun(string(n.Value), f.rPr()))
	case *ast.CodeSpan:
		code := f
		code.code = true
		b.WriteString(textRun(codeSpanText(n, src), code.rPr()))
	case *ast.Emphasis:
		inner := f
		if n.Level >= 2 {
			inner.bold = true
		} else {
			inner.italic = true
		}
		x.inlineChildren(b, n, src, inner)
	case *ast.Link:
		dest := string(n.Destination)
		if !externalLinkRe.MatchString(dest) {
			x.inlineChildren(b, n, src, f)
			return
		}
		link := f
		link.link = true
		b.WriteString(`<w:hyperlink r:id="` + x.rel("hyperlink", string(util.URLEscape(n.Destination, true)), true) + `">`)
		x.inlineChildren(b, n, src, link)
		b.WriteString("</w:hyperlink>")
	case *ast.AutoLink:
		url := string(n.URL(src))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
			url = "mailto:" + url
		}
		label := string(n.Label(src))
		if !externalLinkRe.MatchString(url) {
			b.WriteString(textRun(label, f.rPr()))
			return
		}
		link := f
		link.link = true
		b.WriteString(`<w:hyperlink r:id="` + x.rel("hyperlink", string(util.URLEscape([]byte(url), true)), true) + `">` +
			textRun(label, link.rPr()) + "</w:hyperlink>")
	case *ast.Image:
		alt := plainChildren(n, src, false)
		if img, ok := x.images[string(n.Destination)]; ok {
			b.WriteString(x.drawing(img, alt))
			return
		}
		b.WriteString(textRun(alt, f.rPr()))
	case *ast.RawHTML:
	default:
		x.inlineChildren(b, n, src, f)
	}
}

// --- постоянные части ---

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/><Default Extension="jpg" ContentType="image/jpeg"/>` +
	`<Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
	`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
	`</Types>`

const docxPackageRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
	`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
	`</Relationships>`

const docxDocumentStart = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>`

// docxSection — A4, поля 2,5 см (1418 twip).
const docxSection = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
	`<w:pgMar w:top="1418" w:right="1418" w:bottom="1418" w:left="1418" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr>`

func docxHeadingStyles() string {
	sizes := []int{36, 32, 28, 26, 24, 22}
	var b strings.Builder
	for i, size := range sizes {
		n := strconv.Itoa(i + 1)
		b.WriteString(`<w:style w:type="paragraph" w:styleId="Heading` + n + `"><w:name w:val="heading ` + n + `"/>` +
			`<w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/>` +
			`<w:outlineLvl w:val="` + strconv.Itoa(i) + `"/></w:pPr><w:rPr><w:b/><w:sz w:val="` + strconv.Itoa(size) + `"/></w:rPr></w:style>`)
	}
	return b.String()
}

var docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Georgia" w:hAnsi="Georgia" w:cs="Georgia" w:eastAsia="Georgia"/>` +
	`<w:sz w:val="22"/><w:lang w:val="{lang}"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="300" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>` +
	docxHeadingStyles() +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:ind w:left="720" w:right="720"/></w:pPr><w:rPr><w:i/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:jc w:val="center"/></w:pPr><w:rPr><w:sz w:val="18"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="SourceCode"><w:name w:val="Source Code"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/></w:pPr>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Original"><w:name w:val="Original"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:spacing w:after="40"/></w:pPr><w:rPr><w:i/><w:color w:val="595959"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/>` +
	`<w:pPr><w:spacing w:after="40"/></w:pPr></w:style>` +
	`<w:style w:type="character" w:styleId="VerbatimChar"><w:name w:val="Verbatim Char"/>` +
	`<w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/></w:rPr></w:style>` +
	`<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/>` +
	`<w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>` +
	`</w:styles>`

func docxAbstractNum(id, format string) string {
	var b strings.Builder
	b.WriteString(`<w:abstractNum w:abstractNumId="` + id + `"><w:multiLevelType w:val="hybridMultilevel"/>`)
	bullets := []string{"•", "◦", "▪"}
	for lvl := range 9 {
		l := strconv.Itoa(lvl)
		text := "%" + strconv.Itoa(lvl+1) + "."
		if format == "bullet" {
			text = bullets[lvl%len(bullets)]
		}
		indent := strconv.Itoa(720 * (lvl + 1))
		b.WriteString(`<w:lvl w:ilvl="` + l + `"><w:start w:val="1"/><w:numFmt w:val="` + format + `"/>` +
			`<w:lvlText w:val="` + text + `"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="` + indent + `" w:hanging="360"/></w:pPr></w:lvl>`)
	}
	b.WriteString("</w:abstractNum>")
	return b.String()
}

var docxNumberingStart = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	docxAbstractNum("0", "bullet") + docxAbstractNum("1", "decimal") +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>`
