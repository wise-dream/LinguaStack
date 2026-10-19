package exporters

import (
	"io"
	"strconv"
	"strings"

	"github.com/vnovels/linguastack/internal/cdm"
)

// txtExporter — простой текст UTF-8 без BOM, строки через LF.
type txtExporter struct{}

func (txtExporter) Format() string { return "txt" }

func (txtExporter) Describe(*Book) Output {
	return Output{Ext: ".txt", ContentType: "text/plain; charset=utf-8"}
}

func (txtExporter) Write(w io.Writer, b *Book) error {
	parts := []string{b.Title}
	if b.Author != "" {
		parts[0] += "\n" + b.Author
	}
	t := &txtWriter{book: b}
	var walk func([]Chapter, int)
	walk = func(chs []Chapter, depth int) {
		for i := range chs {
			for _, group := range groupBlocks(displayBlocks(&chs[i], depth)) {
				if text := t.group(group); text != "" {
					parts = append(parts, text)
				}
			}
			walk(chs[i].Children, depth+1)
		}
	}
	walk(b.Chapters, 1)
	_, err := io.WriteString(w, xmlSafe(strings.Join(parts, "\n\n"))+"\n")
	return err
}

type txtWriter struct{ book *Book }

func (t *txtWriter) plain(s string) string { return inlinePlain(s, t.book.HardBreaks) }

// texts — оригинал и перевод строками подряд в двуязычном режиме.
func (t *txtWriter) texts(blk Block) string {
	if t.book.bilingual(blk) {
		return t.plain(blk.Original) + "\n" + t.plain(blk.Translation)
	}
	return t.plain(blk.Text())
}

func (t *txtWriter) group(blocks []Block) string {
	switch blocks[0].Type {
	case cdm.ListItem:
		return t.list(blocks)
	case cdm.TableCell:
		rows, _ := tableGrid(blocks)
		lines := make([]string, len(rows))
		for i, row := range rows {
			values := make([]string, len(row))
			for j, c := range row {
				if c != nil {
					values[j] = strings.ReplaceAll(t.texts(*c), "\n", " / ")
				}
			}
			lines[i] = strings.Join(values, " | ")
		}
		return strings.Join(lines, "\n")
	}
	blk := blocks[0]
	switch blk.Type {
	case cdm.CodeBlock, cdm.MathBlock:
		return blk.Original
	case cdm.Image:
		return "[Изображение: " + blk.Meta["alt"] + "]"
	case cdm.ThematicBreak:
		return "* * *"
	case cdm.RawHTML:
		return rawHTMLText(blk.Original)
	case cdm.Blockquote:
		return prefixLines(t.texts(blk), "> ", ">")
	case cdm.Heading:
		if t.book.bilingual(blk) {
			return t.plain(blk.Translation) + "\n" + t.plain(blk.Original)
		}
	}
	return t.texts(blk)
}

// list — пункты с отступом по глубине; нумерация считается заново в
// каждой серии пунктов одного уровня.
func (t *txtWriter) list(items []Block) string {
	counters := map[int]int{}
	var lines []string
	for _, it := range items {
		depth := listDepth(it)
		for d := range counters {
			if d > depth {
				delete(counters, d)
			}
		}
		marker := "- "
		if ordered(it) {
			counters[depth]++
			marker = strconv.Itoa(counters[depth]) + ". "
		}
		indent := strings.Repeat("  ", depth-1)
		lines = append(lines, indent+marker+
			strings.ReplaceAll(t.texts(it), "\n", "\n"+indent+strings.Repeat(" ", len(marker))))
	}
	return strings.Join(lines, "\n")
}
