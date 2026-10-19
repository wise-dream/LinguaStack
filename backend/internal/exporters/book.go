// Package exporters собирает из перевода документ в формате пользователя
// (ТЗ §3.2, §4.2; дополнение §16): по адаптеру на формат, ядро перевода о
// форматах не знает. Вход — Book: блоки CDM с оригиналом и переводом;
// откуда он взялся (БД, сегменты), решает пакет exports.
package exporters

import (
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/vnovels/linguastack/internal/cdm"
)

// Mode — что попадает в документ.
type Mode string

const (
	// ModeTranslation — только перевод; непереведённые блоки — оригиналом.
	ModeTranslation Mode = "translation"
	// ModeBilingual — оригинал, затем перевод (привычный порядок для
	// изучающих язык).
	ModeBilingual Mode = "bilingual"
)

// Block — блок CDM с переводом.
type Block struct {
	Type        cdm.BlockType
	Level       int
	Meta        map[string]string
	Original    string
	Translation string
	Translated  bool
}

// Text — перевод, если он есть, иначе оригинал.
func (b Block) Text() string {
	if b.Translated {
		return b.Translation
	}
	return b.Original
}

// Chapter — глава; Title уже на языке документа (см. exports.Assemble).
type Chapter struct {
	Index    int
	Title    string
	Blocks   []Block
	Children []Chapter
}

// Book — документ для экспорта.
type Book struct {
	ID         string // идентификатор публикации (EPUB): UUID задачи экспорта
	Title      string
	Author     string
	Lang       string // язык документа — язык перевода
	SourceLang string // язык оригинала: lang у абзацев оригинала
	Mode       Mode
	// HardBreaks — «\n» внутри блока означает перенос строки (<br>), а не
	// мягкий перенос исходника Markdown (дополнение §16).
	HardBreaks bool
	Chapters   []Chapter
	// Assets читает ресурс книги (картинку) по имени из meta.src.
	Assets func(name string) ([]byte, error)
}

// Output — что получится: расширение файла и тип для скачивания.
type Output struct {
	Ext         string
	ContentType string
}

// Exporter — адаптер одного формата.
type Exporter interface {
	Format() string
	Describe(b *Book) Output
	Write(w io.Writer, b *Book) error
}

var registry = []Exporter{epubExporter{}, markdownExporter{}, docxExporter{}, htmlExporter{}, txtExporter{}}

// Formats — поддерживаемые форматы в порядке показа пользователю.
func Formats() []string {
	out := make([]string, len(registry))
	for i, x := range registry {
		out[i] = x.Format()
	}
	return out
}

// Get — адаптер формата.
func Get(format string) (Exporter, bool) {
	for _, x := range registry {
		if x.Format() == format {
			return x, true
		}
	}
	return nil, false
}

// bilingual — показывать ли у блока и оригинал, и перевод.
func (b *Book) bilingual(blk Block) bool {
	return b.Mode == ModeBilingual && blk.Translated && cdm.IsTranslatable(blk.Type)
}

// displayBlocks — блоки главы для сплошных форматов (всё, кроме EPUB, где
// глава — отдельный файл с названием в оглавлении): глава без заголовка
// получает заголовок со своим названием, иначе её граница пропадёт.
func displayBlocks(ch *Chapter, depth int) []Block {
	if ch.Title == "" || (len(ch.Blocks) > 0 && ch.Blocks[0].Type == cdm.Heading) {
		return ch.Blocks
	}
	heading := Block{Type: cdm.Heading, Level: min(depth, 6), Original: ch.Title}
	return append([]Block{heading}, ch.Blocks...)
}

// groupBlocks — подряд идущие пункты списка и ячейки одной таблицы
// выводятся вместе; остальное — по одному. Новая таблица начинается, когда
// номер строки идёт назад.
func groupBlocks(blocks []Block) [][]Block {
	var groups [][]Block
	for i := 0; i < len(blocks); {
		j := i + 1
		switch blocks[i].Type {
		case cdm.ListItem:
			for j < len(blocks) && blocks[j].Type == cdm.ListItem {
				j++
			}
		case cdm.TableCell:
			for j < len(blocks) && blocks[j].Type == cdm.TableCell &&
				metaInt(blocks[j], "row", 0) >= metaInt(blocks[j-1], "row", 0) {
				j++
			}
		}
		groups = append(groups, blocks[i:j])
		i = j
	}
	return groups
}

func metaInt(b Block, key string, fallback int) int {
	if n, err := strconv.Atoi(b.Meta[key]); err == nil && n >= 0 {
		return n
	}
	return fallback
}

func listDepth(b Block) int { return min(max(metaInt(b, "depth", 1), 1), 6) }

func ordered(b Block) bool { return b.Meta["ordered"] == "true" }

// tableGrid раскладывает ячейки по строкам и столбцам; header — у первой
// строки есть ячейки-заголовки.
func tableGrid(cells []Block) (rows [][]*Block, header bool) {
	maxRow, maxCol := 0, 0
	for _, c := range cells {
		maxRow, maxCol = max(maxRow, metaInt(c, "row", 0)), max(maxCol, metaInt(c, "col", 0))
	}
	rows = make([][]*Block, maxRow+1)
	for i := range rows {
		rows[i] = make([]*Block, maxCol+1)
	}
	for i := range cells {
		c := &cells[i]
		rows[metaInt(*c, "row", 0)][metaInt(*c, "col", 0)] = c
		if metaInt(*c, "row", 0) == 0 && c.Meta["header"] == "true" {
			header = true
		}
	}
	return rows, header
}

// imageTypes — картинки, которые попадают в документ (их показывают и
// браузеры, и читалки), и их типы.
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".avif": "image/avif",
}

// bookImages — картинки книги в порядке появления: блоки image и картинки
// внутри текста. Имя в документе — по номеру: имена ресурсов исходника
// бывают любыми (пробелы, кириллица), а читалкам EPUB это не нравится.
func bookImages(b *Book) (order []string, names map[string]string) {
	names = map[string]string{}
	add := func(src string) {
		ext := strings.ToLower(path.Ext(src))
		if _, ok := imageTypes[ext]; !ok || names[src] != "" {
			return
		}
		order = append(order, src)
		names[src] = "img" + leftPad(len(order)) + ext
	}
	var walk func([]Chapter)
	walk = func(chs []Chapter) {
		for _, ch := range chs {
			for _, blk := range ch.Blocks {
				if blk.Type == cdm.Image {
					add(blk.Meta["src"])
					continue
				}
				for _, text := range []string{blk.Original, blk.Translation} {
					for _, src := range inlineImages(text) {
						add(src)
					}
				}
			}
			walk(ch.Children)
		}
	}
	walk(b.Chapters)
	return order, names
}

func leftPad(n int) string {
	s := strconv.Itoa(n)
	return strings.Repeat("0", max(0, 4-len(s))) + s
}

// inlineImages — адреса картинок внутри встроенной разметки.
func inlineImages(text string) []string {
	if !strings.Contains(text, "![") {
		return nil
	}
	doc, _ := parseInline(text)
	var out []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := n.(*ast.Image); ok && entering {
			out = append(out, string(img.Destination))
		}
		return ast.WalkContinue, nil
	})
	return out
}
