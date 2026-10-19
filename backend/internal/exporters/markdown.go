package exporters

import (
	"archive/zip"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vnovels/linguastack/internal/cdm"
)

// markdownExporter — Markdown (дополнение §16). Встроенная разметка CDM уже
// Markdown и выводится как есть. Книга с картинками — ZIP: book.md и
// каталог images/; без картинок — один .md.
type markdownExporter struct{}

func (markdownExporter) Format() string { return "markdown" }

func (markdownExporter) Describe(b *Book) Output {
	if order, _ := bookImages(b); len(order) > 0 {
		return Output{Ext: ".zip", ContentType: "application/zip"}
	}
	return Output{Ext: ".md", ContentType: "text/markdown; charset=utf-8"}
}

func (markdownExporter) Write(w io.Writer, b *Book) error {
	order, names := bookImages(b)
	if len(order) == 0 {
		_, err := io.WriteString(w, renderMarkdown(b, func(string) (string, bool) { return "", false }))
		return err
	}
	// Картинки — первыми: какие из них нашлись, решает текст книги.
	zw := zip.NewWriter(w)
	packed := map[string]string{}
	for _, src := range order {
		data, err := b.Assets(src)
		if err != nil {
			continue
		}
		name := "images/" + names[src]
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
		packed[src] = name
	}
	f, err := zw.Create("book.md")
	if err != nil {
		return err
	}
	image := func(src string) (string, bool) {
		name, ok := packed[src]
		return name, ok
	}
	if _, err := io.WriteString(f, renderMarkdown(b, image)); err != nil {
		return err
	}
	return zw.Close()
}

func renderMarkdown(b *Book, image func(string) (string, bool)) string {
	var parts []string
	front := "---\ntitle: " + strconv.Quote(b.Title) + "\n"
	if b.Author != "" {
		front += "author: " + strconv.Quote(b.Author) + "\n"
	}
	if b.Lang != "" {
		front += "lang: " + b.Lang + "\n"
	}
	parts = append(parts, front+"---")

	md := &mdWriter{book: b, image: image}
	var walk func([]Chapter, int)
	walk = func(chs []Chapter, depth int) {
		for i := range chs {
			for _, group := range groupBlocks(displayBlocks(&chs[i], depth)) {
				parts = append(parts, md.group(group)...)
			}
			walk(chs[i].Children, depth+1)
		}
	}
	walk(b.Chapters, 1)
	return strings.Join(parts, "\n\n") + "\n"
}

type mdWriter struct {
	book  *Book
	image func(string) (string, bool)
}

// texts — тексты блока: оригинал и перевод в двуязычном режиме, иначе один.
func (m *mdWriter) texts(blk Block) []string {
	var out []string
	if m.book.bilingual(blk) {
		out = []string{blk.Original, blk.Translation}
	} else {
		out = []string{blk.Text()}
	}
	for i, t := range out {
		out[i] = m.inline(t)
	}
	return out
}

var inlineImageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

// inline — разметка как есть, но картинки — с адресами внутри документа, а
// переносы строк — явными, если «\n» в книге значит <br>.
func (m *mdWriter) inline(text string) string {
	text = inlineImageRe.ReplaceAllStringFunc(text, func(s string) string {
		sub := inlineImageRe.FindStringSubmatch(s)
		if name, ok := m.image(sub[2]); ok {
			return "![" + sub[1] + "](" + name + ")"
		}
		return sub[1]
	})
	if m.book.HardBreaks {
		text = strings.ReplaceAll(text, "\n", "\\\n")
	}
	return text
}

func (m *mdWriter) group(blocks []Block) []string {
	switch blocks[0].Type {
	case cdm.ListItem:
		return []string{m.list(blocks)}
	case cdm.TableCell:
		return []string{m.table(blocks)}
	}
	blk := blocks[0]
	switch blk.Type {
	case cdm.Heading:
		level := strings.Repeat("#", min(max(blk.Level, 1), 6))
		texts := m.texts(blk)
		// Заголовок — перевод, оригинал под ним — абзацем: два заголовка
		// подряд дали бы две главы при обратном импорте.
		heading := level + " " + oneLine(texts[len(texts)-1])
		if len(texts) == 2 {
			return []string{heading, texts[0]}
		}
		return []string{heading}
	case cdm.CodeBlock:
		fence := strings.Repeat("`", max(3, longestRun(blk.Original, '`')+1))
		return []string{fence + blk.Meta["language"] + "\n" + blk.Original + "\n" + fence}
	case cdm.MathBlock, cdm.RawHTML:
		return []string{blk.Original}
	case cdm.Image:
		alt := blk.Meta["alt"]
		if name, ok := m.image(blk.Meta["src"]); ok {
			return []string{"![" + escapeAlt(alt) + "](" + name + ")"}
		}
		return []string{"\\[Изображение: " + escapeAlt(alt) + "\\]"}
	case cdm.ThematicBreak:
		return []string{"* * *"}
	case cdm.Blockquote:
		return []string{prefixLines(strings.Join(m.texts(blk), "\n\n"), "> ", ">")}
	}
	return m.texts(blk)
}

func (m *mdWriter) list(items []Block) string {
	var lines []string
	for _, it := range items {
		indent := strings.Repeat("  ", listDepth(it)-1)
		marker := "- "
		if ordered(it) {
			marker = "1. "
		}
		for _, text := range m.texts(it) {
			first, rest, _ := strings.Cut(text, "\n")
			line := indent + marker + first
			if rest != "" {
				line += "\n" + prefixLines(rest, indent+strings.Repeat(" ", len(marker)), "")
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

var unescapedPipeRe = regexp.MustCompile(`(^|[^\\])\|`)

func (m *mdWriter) table(cells []Block) string {
	rows, _ := tableGrid(cells)
	cell := func(c *Block) string {
		if c == nil {
			return ""
		}
		text := strings.Join(m.texts(*c), "<br>")
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\\\n", "<br>"), "\n", "<br>")
		return unescapedPipeRe.ReplaceAllString(unescapedPipeRe.ReplaceAllString(text, `$1\|`), `$1\|`)
	}
	var lines []string
	for i, row := range rows {
		values := make([]string, len(row))
		for j, c := range row {
			values[j] = cell(c)
		}
		lines = append(lines, "| "+strings.Join(values, " | ")+" |")
		if i == 0 {
			lines = append(lines, "|"+strings.Repeat(" --- |", len(row)))
		}
	}
	return strings.Join(lines, "\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\\\n", " ")), " ")
}

func longestRun(s string, r rune) int {
	best, cur := 0, 0
	for _, c := range s {
		if c == r {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

var altEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)

func escapeAlt(s string) string { return altEscaper.Replace(s) }

// prefixLines добавляет префикс к каждой строке; пустые строки получают
// blank (у цитаты — «>», чтобы абзацы остались внутри неё).
func prefixLines(s, prefix, blank string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = blank
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
