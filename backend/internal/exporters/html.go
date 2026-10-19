package exporters

import (
	"encoding/base64"
	"html"
	"io"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/vnovels/linguastack/internal/cdm"
)

// htmlRenderer выводит блоки глав в HTML — один и тот же код для HTML-файла
// и XHTML глав EPUB (xhtml: самозакрывающиеся теги).
type htmlRenderer struct {
	book  *Book
	xhtml bool
	image func(src string) (string, bool)
}

func (r *htmlRenderer) inline(text string) string {
	return renderInlineHTML(text, inlineOptions{XHTML: r.xhtml, HardBreaks: r.book.HardBreaks, Image: r.image})
}

func (r *htmlRenderer) void(tag string) string {
	if r.xhtml {
		return "<" + tag + "/>"
	}
	return "<" + tag + ">"
}

func (r *htmlRenderer) srcLang() string {
	if r.book.SourceLang == "" {
		return ""
	}
	return ` lang="` + html.EscapeString(r.book.SourceLang) + `"`
}

// paragraphs — блок абзацами тега tag: в двуязычном режиме оригинал (с его
// языком) и перевод; непереведённый блок в режиме перевода — оригинал со
// своим lang.
func (r *htmlRenderer) paragraphs(blk Block, tag, class string) string {
	cls := func(extra string) string {
		names := strings.TrimSpace(class + " " + extra)
		if names == "" {
			return ""
		}
		return ` class="` + names + `"`
	}
	switch {
	case r.book.bilingual(blk):
		return "<" + tag + cls("original") + r.srcLang() + ">" + r.inline(blk.Original) + "</" + tag + ">\n" +
			"<" + tag + cls("translation") + ">" + r.inline(blk.Translation) + "</" + tag + ">"
	case !blk.Translated && cdm.IsTranslatable(blk.Type) && r.book.Mode == ModeTranslation && blk.Original != "":
		return "<" + tag + cls("") + r.srcLang() + ">" + r.inline(blk.Original) + "</" + tag + ">"
	}
	return "<" + tag + cls("") + ">" + r.inline(blk.Text()) + "</" + tag + ">"
}

// chapter — глава секцией с якорем c{index}; подглавы — вложенными
// секциями.
func (r *htmlRenderer) chapter(b *strings.Builder, ch *Chapter, depth int) {
	b.WriteString(`<section id="c` + strconv.Itoa(ch.Index) + `">` + "\n")
	b.WriteString(r.blocks(displayBlocks(ch, depth)))
	for i := range ch.Children {
		r.chapter(b, &ch.Children[i], depth+1)
	}
	b.WriteString("</section>\n")
}

// blocks — блоки подряд, списки и таблицы — группами.
func (r *htmlRenderer) blocks(blocks []Block) string {
	var b strings.Builder
	for _, group := range groupBlocks(blocks) {
		if out := r.group(group); out != "" {
			b.WriteString(out)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (r *htmlRenderer) group(blocks []Block) string {
	switch blocks[0].Type {
	case cdm.ListItem:
		return r.list(blocks)
	case cdm.TableCell:
		return r.table(blocks)
	}
	blk := blocks[0]
	switch blk.Type {
	case cdm.Heading:
		tag := "h" + strconv.Itoa(min(max(blk.Level, 1), 6))
		if r.book.bilingual(blk) {
			// Заголовок — перевод; оригинал — подписью под ним, иначе
			// обратный импорт и оглавления читалок увидели бы две главы.
			return "<" + tag + ">" + r.inline(blk.Translation) + "</" + tag + ">\n" +
				`<p class="original heading-original"` + r.srcLang() + ">" + r.inline(blk.Original) + "</p>"
		}
		return r.paragraphs(blk, tag, "")
	case cdm.Paragraph:
		return r.paragraphs(blk, "p", "")
	case cdm.Caption:
		return r.paragraphs(blk, "p", "caption")
	case cdm.Blockquote:
		return "<blockquote>" + r.paragraphs(blk, "p", "") + "</blockquote>"
	case cdm.CodeBlock:
		class := ""
		if lang := blk.Meta["language"]; lang != "" {
			class = ` class="language-` + html.EscapeString(lang) + `"`
		}
		return "<pre><code" + class + ">" + html.EscapeString(xmlSafe(blk.Original)) + "</code></pre>"
	case cdm.MathBlock:
		return `<pre class="math">` + html.EscapeString(xmlSafe(blk.Original)) + "</pre>"
	case cdm.Image:
		alt := html.EscapeString(xmlSafe(blk.Meta["alt"]))
		if name, ok := r.image(blk.Meta["src"]); ok {
			end := ">"
			if r.xhtml {
				end = "/>"
			}
			return `<figure><img src="` + html.EscapeString(name) + `" alt="` + alt + `"` + end + "</figure>"
		}
		return `<p class="image-missing">` + html.EscapeString(imageCaption(xmlSafe(blk.Meta["alt"]))) + "</p>"
	case cdm.ThematicBreak:
		return r.void("hr")
	case cdm.RawHTML:
		if text := rawHTMLText(blk.Original); text != "" {
			return "<p>" + html.EscapeString(xmlSafe(text)) + "</p>"
		}
		return ""
	}
	return r.paragraphs(blk, "p", "")
}

// list — пункты вложенными списками по глубине.
func (r *htmlRenderer) list(items []Block) string {
	type open struct {
		depth int
		tag   string
	}
	var b strings.Builder
	var stack []open
	closeTop := func() {
		b.WriteString("</li></" + stack[len(stack)-1].tag + ">")
		stack = stack[:len(stack)-1]
	}
	for _, it := range items {
		depth, tag := listDepth(it), "ul"
		if ordered(it) {
			tag = "ol"
		}
		for len(stack) > 0 && stack[len(stack)-1].depth > depth {
			closeTop()
		}
		if len(stack) > 0 && stack[len(stack)-1].depth == depth {
			if stack[len(stack)-1].tag == tag {
				b.WriteString("</li>")
			} else {
				closeTop()
			}
		}
		if len(stack) == 0 || stack[len(stack)-1].depth < depth {
			b.WriteString("<" + tag + ">")
			stack = append(stack, open{depth: depth, tag: tag})
		}
		b.WriteString("<li>")
		if r.book.bilingual(it) {
			b.WriteString(r.paragraphs(it, "p", ""))
		} else {
			b.WriteString(r.inline(it.Text()))
		}
	}
	for len(stack) > 0 {
		closeTop()
	}
	return b.String()
}

func (r *htmlRenderer) table(cells []Block) string {
	rows, header := tableGrid(cells)
	var b strings.Builder
	b.WriteString("<table><tbody>")
	for i, row := range rows {
		b.WriteString("<tr>")
		tag := "td"
		if i == 0 && header {
			tag = "th"
		}
		for _, c := range row {
			b.WriteString("<" + tag + ">")
			if c != nil {
				if r.book.bilingual(*c) {
					b.WriteString(r.paragraphs(*c, "p", ""))
				} else {
					b.WriteString(r.inline(c.Text()))
				}
			}
			b.WriteString("</" + tag + ">")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
	return b.String()
}

// rawHTMLText — текст сырого HTML из Markdown: сам HTML в экспорт не
// попадает (дополнение §16), а его текст — да.
func rawHTMLText(s string) string {
	nodes, err := xhtml.ParseFragment(strings.NewReader(s), nil)
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// tocList — оглавление вложенными <ol>; href — адрес главы.
func tocList(chs []Chapter, href func(ch *Chapter) string) string {
	if len(chs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<ol>")
	for i := range chs {
		ch := &chs[i]
		b.WriteString(`<li><a href="` + html.EscapeString(href(ch)) + `">` + html.EscapeString(xmlSafe(ch.Title)) + "</a>")
		b.WriteString(tocList(ch.Children, href))
		b.WriteString("</li>")
	}
	b.WriteString("</ol>")
	return b.String()
}

// bookCSS — типографика экспорта: без внешних шрифтов, оригинал в
// двуязычном режиме тише перевода.
const bookCSS = `body { font-family: Georgia, "Times New Roman", serif; line-height: 1.6; margin: 0 auto; max-width: 40em; padding: 1em; }
h1, h2, h3, h4, h5, h6 { line-height: 1.25; }
pre { white-space: pre-wrap; font-family: Menlo, Consolas, monospace; font-size: 0.9em; }
img { max-width: 100%; height: auto; }
figure { margin: 1em 0; text-align: center; }
table { border-collapse: collapse; }
td, th { border: 1px solid #999; padding: 0.25em 0.5em; vertical-align: top; }
blockquote { margin-left: 1.5em; font-style: italic; }
.original { color: #555; margin-bottom: 0.2em; }
.heading-original { font-style: italic; margin-top: -0.5em; }
.caption { text-align: center; font-size: 0.9em; }
`

// htmlExporter — один самодостаточный HTML-файл: картинки — в data:.
type htmlExporter struct{}

func (htmlExporter) Format() string { return "html" }

func (htmlExporter) Describe(*Book) Output {
	return Output{Ext: ".html", ContentType: "text/html; charset=utf-8"}
}

func (htmlExporter) Write(w io.Writer, b *Book) error {
	_, names := bookImages(b)
	uris := map[string]string{}
	image := func(src string) (string, bool) {
		if uri, ok := uris[src]; ok {
			return uri, uri != ""
		}
		uri := ""
		if names[src] != "" {
			if data, err := b.Assets(src); err == nil {
				uri = "data:" + imageTypes[strings.ToLower(extOf(names[src]))] + ";base64," + base64.StdEncoding.EncodeToString(data)
			}
		}
		uris[src] = uri
		return uri, uri != ""
	}
	r := &htmlRenderer{book: b, image: image}
	var page strings.Builder
	page.WriteString("<!DOCTYPE html>\n<html lang=\"" + html.EscapeString(b.Lang) + "\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + html.EscapeString(xmlSafe(b.Title)) + "</title>\n")
	if b.Author != "" {
		page.WriteString(`<meta name="author" content="` + html.EscapeString(xmlSafe(b.Author)) + "\">\n")
	}
	page.WriteString("<style>\n" + bookCSS + "</style>\n</head>\n<body>\n")
	page.WriteString(`<nav class="toc">` + tocList(b.Chapters, func(ch *Chapter) string { return "#c" + strconv.Itoa(ch.Index) }) + "</nav>\n<main>\n")
	for i := range b.Chapters {
		r.chapter(&page, &b.Chapters[i], 1)
	}
	page.WriteString("</main>\n</body>\n</html>\n")
	_, err := io.WriteString(w, page.String())
	return err
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}
