package importers_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// docxOptions — части тестового DOCX. Пустая строка — части нет совсем:
// так проверяется, что импорт переживает минимальный документ.
type docxOptions struct {
	body      string // содержимое w:body
	styles    string // содержимое w:styles; noStyles — без части
	noStyles  bool
	numbering string            // содержимое w:numbering
	rels      map[string]string // rId → Target; "ext:" в начале — внешняя связь
	footnotes string            // w:footnote элементы
	core      string            // содержимое cp:coreProperties
	media     map[string][]byte // имя внутри word/ → байты
	raw       map[string]string // произвольные записи архива
}

const docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml"`

// defaultDocxStyles — стили, которые пишет Word: id и имя различаются.
const defaultDocxStyles = `
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Heading1"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/></w:style>
<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/></w:style>
<w:style w:type="paragraph" w:styleId="SourceCode"><w:name w:val="Source Code"/></w:style>
<w:style w:type="paragraph" w:styleId="TOC1"><w:name w:val="toc 1"/></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/></w:style>
<w:style w:type="character" w:styleId="VerbatimChar"><w:name w:val="Verbatim Char"/><w:rPr><w:rFonts w:ascii="Consolas"/></w:rPr></w:style>
<w:style w:type="character" w:styleId="Strong"><w:name w:val="Strong"/><w:rPr><w:b/></w:rPr></w:style>`

func buildDOCX(t *testing.T, o docxOptions) []byte {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="application/xml"/></Types>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ` + docxNS + `><w:body>` +
			o.body + `<w:sectPr/></w:body></w:document>`,
	}
	if !o.noStyles {
		styles := o.styles
		if styles == "" {
			styles = defaultDocxStyles
		}
		parts["word/styles.xml"] = `<?xml version="1.0" encoding="UTF-8"?><w:styles ` + docxNS + `>` + styles + `</w:styles>`
	}
	if o.numbering != "" {
		parts["word/numbering.xml"] = `<?xml version="1.0" encoding="UTF-8"?><w:numbering ` + docxNS + `>` + o.numbering + `</w:numbering>`
	}
	if len(o.rels) > 0 {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
		ids := make([]string, 0, len(o.rels))
		for id := range o.rels {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			target := o.rels[id]
			mode := ""
			if rest, ok := strings.CutPrefix(target, "ext:"); ok {
				target, mode = rest, ` TargetMode="External"`
			}
			fmt.Fprintf(&b, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/x" Target="%s"%s/>`,
				id, html.EscapeString(target), mode)
		}
		b.WriteString(`</Relationships>`)
		parts["word/_rels/document.xml.rels"] = b.String()
	}
	if o.footnotes != "" {
		parts["word/footnotes.xml"] = `<?xml version="1.0" encoding="UTF-8"?><w:footnotes ` + docxNS + `>` +
			`<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
			o.footnotes + `</w:footnotes>`
	}
	if o.core != "" {
		parts["docProps/core.xml"] = `<?xml version="1.0" encoding="UTF-8"?><cp:coreProperties ` +
			`xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
			`xmlns:dc="http://purl.org/dc/elements/1.1/">` + o.core + `</cp:coreProperties>`
	}
	for name, body := range o.raw {
		parts[name] = body
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	write := func(name string, data []byte) {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write(data)
		require.NoError(t, err)
	}
	for _, name := range names {
		write(name, []byte(parts[name]))
	}
	for name, data := range o.media {
		write("word/"+name, data)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// wp — абзац со стилем (пустой стиль — без w:pStyle) и содержимым.
func wp(style, inner string) string {
	if style == "" {
		return "<w:p>" + inner + "</w:p>"
	}
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>` + inner + "</w:p>"
}

// wr — прогон с текстом и свойствами (содержимое w:rPr).
func wr(text, props string) string {
	rpr := ""
	if props != "" {
		rpr = "<w:rPr>" + props + "</w:rPr>"
	}
	return "<w:r>" + rpr + `<w:t xml:space="preserve">` + html.EscapeString(text) + "</w:t></w:r>"
}

// wimage — встроенная картинка DrawingML со связью rID и описанием.
func wimage(rID, descr string) string {
	return `<w:r><w:drawing><wp:inline><wp:extent cx="914400" cy="914400"/><wp:docPr id="1" name="Picture 1" descr="` +
		html.EscapeString(descr) + `"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
		`<pic:pic><pic:blipFill><a:blip r:embed="` + rID + `"/></pic:blipFill></pic:pic></a:graphicData></a:graphic>` +
		`</wp:inline></w:drawing></w:r>`
}
