package exporters

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func TestEPUBStructure(t *testing.T) {
	data := export(t, "epub", sampleBook(ModeTranslation))
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	require.Equal(t, "mimetype", r.File[0].Name, "mimetype — первой записью")
	require.Equal(t, zip.Store, r.File[0].Method, "без сжатия")
	require.Zero(t, r.File[0].Flags&0x8, "без дескриптора данных")

	files := zipFiles(t, data)
	require.Equal(t, "application/epub+zip", string(files["mimetype"]))
	require.Contains(t, string(files["META-INF/container.xml"]), `full-path="OEBPS/content.opf"`)
	opf := string(files["OEBPS/content.opf"])
	for _, want := range []string{
		`<dc:identifier id="bookid">urn:uuid:00000000-0000-0000-0000-000000000001</dc:identifier>`,
		"<dc:title>Книга</dc:title>", "<dc:creator>Автор</dc:creator>", "<dc:language>ru</dc:language>",
		`<meta property="dcterms:modified">`,
		`href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"`,
		`href="toc.ncx" media-type="application/x-dtbncx+xml"`,
		`href="images/img0001.png" media-type="image/png"`,
		`<spine toc="ncx"><itemref idref="ch0000"/><itemref idref="ch0001"/><itemref idref="ch0002"/></spine>`,
	} {
		require.Contains(t, opf, want)
	}
	nav := string(files["OEBPS/nav.xhtml"])
	require.Contains(t, nav, `epub:type="toc"`)
	require.Contains(t, nav, `<li><a href="text/ch0000.xhtml">Глава 1</a><ol><li><a href="text/ch0001.xhtml">Раздел</a></li></ol></li>`)
	require.Contains(t, string(files["OEBPS/toc.ncx"]), `<content src="text/ch0001.xhtml"/>`)
	require.Equal(t, tinyPNG, files["OEBPS/images/img0001.png"])
	ch := string(files["OEBPS/text/ch0000.xhtml"])
	require.Contains(t, ch, `<img src="../images/img0001.png" alt="Схема"/>`)
	require.NotContains(t, ch, "Вложенный текст", "подглава — своим файлом")
	require.Contains(t, string(files["OEBPS/text/ch0001.xhtml"]), "Вложенный текст")
}

func TestEPUBWellFormedXML(t *testing.T) {
	b := sampleBook(ModeBilingual)
	b.Title = "Книга & <ко>\x02"
	b.Chapters[0].Title = "Глава & \x0cодин"
	b.Chapters[0].Blocks = append(b.Chapters[0].Blocks, tr(cdm.Paragraph, 0, "a < b && \x02c", "а < б && \x02в"),
		raw(cdm.CodeBlock, "if a < b && c {\x0c}"), raw(cdm.RawHTML, "<div onclick=\"x()\">сырой <b>html</b></div>"))
	files := zipFiles(t, export(t, "epub", b))
	checked := 0
	for name, body := range files {
		if !strings.HasSuffix(name, ".xhtml") && !strings.HasSuffix(name, ".opf") && !strings.HasSuffix(name, ".ncx") &&
			!strings.HasSuffix(name, ".xml") {
			continue
		}
		d := xml.NewDecoder(bytes.NewReader(body))
		d.Strict = true
		for {
			_, err := d.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err, "%s:\n%s", name, body)
		}
		checked++
	}
	require.GreaterOrEqual(t, checked, 7)
	ch := string(files["OEBPS/text/ch0000.xhtml"])
	require.NotContains(t, ch, "onclick")
	require.Contains(t, ch, "сырой html")
}

func TestEPUBMissingImageIsAltText(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Assets = func(string) ([]byte, error) { return nil, io.EOF }
	files := zipFiles(t, export(t, "epub", b))
	require.NotContains(t, string(files["OEBPS/content.opf"]), "images/")
	require.Contains(t, string(files["OEBPS/text/ch0000.xhtml"]), "[Изображение: Схема]")
}

func TestEPUBBilingual(t *testing.T) {
	ch := string(zipFiles(t, export(t, "epub", sampleBook(ModeBilingual)))["OEBPS/text/ch0000.xhtml"])
	require.Contains(t, ch, `<p class="original" lang="en">Text with <em>em</em> &amp; .</p>`)
	require.Contains(t, ch, `<p class="translation">Текст с <em>курсивом</em> &amp; .</p>`)
	require.Contains(t, ch, `<h1>Глава 1</h1>`)
}

func TestEPUBRoundTrip(t *testing.T) {
	data := export(t, "epub", sampleBook(ModeTranslation))
	res, err := importers.NewEPUB(importers.ArchiveLimits{MaxEntries: 100, MaxTotalBytes: 1 << 26, MaxRatio: 100}).Load(data, "book.epub")
	require.NoError(t, err)
	require.Empty(t, res.Issues)
	doc := res.Document
	require.Equal(t, "Книга", doc.Meta.Title)
	require.Equal(t, "Автор", doc.Meta.Author)
	require.Equal(t, "ru", doc.Meta.Language)
	require.Equal(t, []string{"Глава 1", "Без заголовка"}, titles(doc.Chapters))
	require.Equal(t, []string{"Раздел"}, titles(doc.Chapters[0].Children))
	var got []string
	for _, blk := range doc.Chapters[0].Blocks {
		got = append(got, string(blk.Type)+":"+blk.Text)
	}
	require.Equal(t, []string{
		"heading:Глава 1", "paragraph:Текст с *курсивом* & .",
		"list_item:Один", "list_item:Вложенный", "list_item:Первый",
		"code_block:fmt.Println(\"```\")",
		"table_cell:Имя", "table_cell:Значение", "table_cell:а|б", "table_cell:1",
		"image:", "thematic_break:", "blockquote:Цитата", "paragraph:Untranslated", "paragraph:Строка один\nстрока два",
	}, got)
	require.Len(t, res.Assets, 1)
	require.Equal(t, "Глава без заголовка.", doc.Chapters[1].Blocks[0].Text, "в EPUB глава без заголовка остаётся без него")
}

// bookFromDoc — книга без перевода (режим перевода выводит оригинал).
func bookFromDoc(doc cdm.Document, assets []importers.Asset) *Book {
	var chapters func([]cdm.Chapter) []Chapter
	chapters = func(src []cdm.Chapter) []Chapter {
		var out []Chapter
		for _, ch := range src {
			c := Chapter{Index: ch.Index, Title: ch.Title, Children: chapters(ch.Children)}
			for _, b := range ch.Blocks {
				c.Blocks = append(c.Blocks, Block{Type: b.Type, Level: b.Level, Meta: b.Meta, Original: b.Text})
			}
			out = append(out, c)
		}
		return out
	}
	byName := map[string]importers.Asset{}
	for _, a := range assets {
		byName[a.Name] = a
	}
	return &Book{ID: "x", Title: doc.Meta.Title, Author: doc.Meta.Author, Lang: "en", Mode: ModeTranslation, HardBreaks: true,
		Chapters: chapters(doc.Chapters),
		Assets: func(name string) ([]byte, error) {
			if a, ok := byName[name]; ok {
				return a.Open()
			}
			return nil, io.EOF
		}}
}

func countBlocks(doc cdm.Document) map[cdm.BlockType]int {
	out := map[cdm.BlockType]int{}
	for _, b := range doc.Blocks() {
		out[b.Type]++
	}
	return out
}

func TestEPUBRealAliceRoundTrip(t *testing.T) {
	data, err := os.ReadFile("../importers/testdata/real/alice.epub")
	require.NoError(t, err)
	limits := importers.ArchiveLimits{MaxEntries: 2000, MaxTotalBytes: 1 << 28, MaxRatio: 100}
	src, err := importers.NewEPUB(limits).Load(data, "alice.epub")
	require.NoError(t, err)

	out := export(t, "epub", bookFromDoc(src.Document, src.Assets))
	back, err := importers.NewEPUB(limits).Load(out, "alice-export.epub")
	require.NoError(t, err)
	require.Empty(t, back.Issues)
	require.Equal(t, titles(src.Document.Chapters), titles(back.Document.Chapters))
	require.Equal(t, countBlocks(src.Document), countBlocks(back.Document))
	require.Equal(t, len(src.Assets), len(back.Assets), "все иллюстрации на месте")
	srcText, backText := src.Document.TranslatableBlocks(), back.Document.TranslatableBlocks()
	for i := range srcText {
		// Внутренние ссылки исходника в экспорте — текст (дополнение §16).
		require.Equal(t, PlainText(srcText[i].Text), PlainText(backText[i].Text), "блок %d", i)
	}
}
