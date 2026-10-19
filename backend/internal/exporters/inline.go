package exporters

import (
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Встроенная разметка CDM — Markdown (импорт приводит к нему все форматы).
// Разбирается goldmark только парсером абзацев: текст блока — всегда
// абзац, и «- …», «# …», «1. …» или отступ остаются текстом, а не
// превращаются в список, заголовок или код. Определения ссылок («[1]: …»)
// тоже не распознаются: без трансформеров абзацев это обычный текст.
var inlineParser = parser.NewParser(
	parser.WithBlockParsers(util.Prioritized(parser.NewParagraphParser(), 100)),
	parser.WithInlineParsers(parser.DefaultInlineParsers()...),
)

func parseInline(s string) (ast.Node, []byte) {
	src := []byte(s)
	return inlineParser.Parse(text.NewReader(src)), src
}

// externalLinkRe — ссылки, которые остаются ссылками. Внутренние ссылки
// исходника (на другие файлы и якоря) в экспорте ведут в никуда — они
// становятся текстом; javascript: и прочие схемы — тем более.
var externalLinkRe = regexp.MustCompile(`(?i)^(https?|mailto):`)

// xmlSafe убирает символы, недопустимые в XML 1.0 (EPUB, DOCX), — в
// тексте из PDF это «\x02» и «\x0c».
func xmlSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r >= 0xd800 && r <= 0xdfff, r == 0xfffe, r == 0xffff:
			return -1
		}
		return r
	}, s)
}

// textValue — текст узла с разрешёнными экранированиями и сущностями
// (\* → *, &amp; → &), как это делает рендерер goldmark.
func textValue(v []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v))))
}

type inlineOptions struct {
	XHTML      bool
	HardBreaks bool
	// Image — адрес картинки в документе по её src; false — картинки нет,
	// выводится подпись.
	Image func(src string) (string, bool)
}

func (o inlineOptions) br() string {
	if o.XHTML {
		return "<br/>"
	}
	return "<br>"
}

// renderInlineHTML — встроенная разметка в безопасный HTML: текст
// экранирован, сырой HTML не выводится, ссылки — только внешние.
func renderInlineHTML(s string, o inlineOptions) string {
	doc, src := parseInline(xmlSafe(s))
	var b strings.Builder
	for p := doc.FirstChild(); p != nil; p = p.NextSibling() {
		if p != doc.FirstChild() {
			b.WriteString(o.br())
		}
		htmlChildren(&b, p, src, o)
	}
	return strings.TrimSpace(b.String())
}

func htmlChildren(b *strings.Builder, n ast.Node, src []byte, o inlineOptions) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		htmlNode(b, c, src, o)
	}
}

func htmlNode(b *strings.Builder, n ast.Node, src []byte, o inlineOptions) {
	switch n := n.(type) {
	case *ast.Text:
		b.WriteString(html.EscapeString(textValue(n.Segment.Value(src))))
		switch {
		case n.HardLineBreak() || (n.SoftLineBreak() && o.HardBreaks):
			b.WriteString(o.br())
		case n.SoftLineBreak():
			b.WriteString(" ")
		}
	case *ast.String:
		b.WriteString(html.EscapeString(string(n.Value)))
	case *ast.CodeSpan:
		b.WriteString("<code>" + html.EscapeString(codeSpanText(n, src)) + "</code>")
	case *ast.Emphasis:
		tag := "em"
		if n.Level >= 2 {
			tag = "strong"
		}
		b.WriteString("<" + tag + ">")
		htmlChildren(b, n, src, o)
		b.WriteString("</" + tag + ">")
	case *ast.Link:
		dest := string(n.Destination)
		if !externalLinkRe.MatchString(dest) {
			htmlChildren(b, n, src, o)
			return
		}
		b.WriteString(`<a href="` + html.EscapeString(string(util.URLEscape(n.Destination, true))) + `">`)
		htmlChildren(b, n, src, o)
		b.WriteString("</a>")
	case *ast.AutoLink:
		label := html.EscapeString(string(n.Label(src)))
		url := string(n.URL(src))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
			url = "mailto:" + url
		}
		if !externalLinkRe.MatchString(url) {
			b.WriteString(label)
			return
		}
		b.WriteString(`<a href="` + html.EscapeString(string(util.URLEscape([]byte(url), true))) + `">` + label + "</a>")
	case *ast.Image:
		alt := plainChildren(n, src, false)
		if o.Image != nil {
			if name, ok := o.Image(string(n.Destination)); ok {
				end := ">"
				if o.XHTML {
					end = "/>"
				}
				b.WriteString(`<img src="` + html.EscapeString(name) + `" alt="` + html.EscapeString(alt) + `"` + end)
				return
			}
		}
		b.WriteString(html.EscapeString(alt))
	case *ast.RawHTML:
		// сырой HTML в экспорт не попадает
	default:
		htmlChildren(b, n, src, o)
	}
}

func codeSpanText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		}
	}
	return b.String()
}

// inlinePlain — встроенная разметка простым текстом (TXT, названия).
func inlinePlain(s string, hardBreaks bool) string {
	doc, src := parseInline(xmlSafe(s))
	var b strings.Builder
	for p := doc.FirstChild(); p != nil; p = p.NextSibling() {
		if p != doc.FirstChild() {
			b.WriteString("\n")
		}
		b.WriteString(plainChildren(p, src, hardBreaks))
	}
	return strings.TrimSpace(b.String())
}

func plainChildren(n ast.Node, src []byte, hardBreaks bool) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		plainNode(&b, c, src, hardBreaks)
	}
	return b.String()
}

func plainNode(b *strings.Builder, n ast.Node, src []byte, hardBreaks bool) {
	switch n := n.(type) {
	case *ast.Text:
		b.WriteString(textValue(n.Segment.Value(src)))
		switch {
		case n.HardLineBreak() || (n.SoftLineBreak() && hardBreaks):
			b.WriteString("\n")
		case n.SoftLineBreak():
			b.WriteString(" ")
		}
	case *ast.String:
		b.Write(n.Value)
	case *ast.CodeSpan:
		b.WriteString(codeSpanText(n, src))
	case *ast.AutoLink:
		b.Write(n.Label(src))
	case *ast.Image:
		b.WriteString("[Изображение: " + plainChildren(n, src, false) + "]")
	case *ast.RawHTML:
	default:
		b.WriteString(plainChildren(n, src, hardBreaks))
	}
}

// PlainText — встроенная разметка простым текстом в одну строку: названия
// глав и книги.
func PlainText(s string) string { return strings.Join(strings.Fields(inlinePlain(s, false)), " ") }
