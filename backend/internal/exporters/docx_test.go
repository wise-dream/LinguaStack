package exporters

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func TestDOCXExportParts(t *testing.T) {
	files := zipFiles(t, export(t, "docx", sampleBook(ModeTranslation)))
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "docProps/core.xml", "word/document.xml",
		"word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels", "word/media/img0001.png"} {
		require.Contains(t, files, name)
	}
	core := string(files["docProps/core.xml"])
	require.Contains(t, core, "<dc:title>Книга</dc:title>")
	require.Contains(t, core, "<dc:creator>Автор</dc:creator>")
	require.Contains(t, core, "<dc:language>ru</dc:language>")
	styles := string(files["word/styles.xml"])
	for _, want := range []string{`w:styleId="Heading1"`, `<w:name w:val="heading 1"/>`, `<w:outlineLvl w:val="0"/>`,
		`w:styleId="Heading6"`, `w:styleId="Quote"`, `w:styleId="Caption"`, `w:styleId="SourceCode"`, `w:styleId="Original"`,
		`w:styleId="VerbatimChar"`, `<w:lang w:val="ru"/>`} {
		require.Contains(t, styles, want)
	}
	numbering := string(files["word/numbering.xml"])
	require.Contains(t, numbering, `<w:numFmt w:val="bullet"/>`)
	require.Contains(t, numbering, `<w:numFmt w:val="decimal"/>`)
	require.Contains(t, string(files["[Content_Types].xml"]), `Extension="png" ContentType="image/png"`)
}

func TestDOCXExportWellFormed(t *testing.T) {
	b := sampleBook(ModeBilingual)
	b.Title = "Книга & <ко>\x02"
	b.Chapters[0].Blocks = append(b.Chapters[0].Blocks, tr(cdm.Paragraph, 0, "a < b && \x02c", "а < б && \x02в"),
		raw(cdm.CodeBlock, "if a < b && c {\x0c\n\t}"))
	files := zipFiles(t, export(t, "docx", b))
	for name, body := range files {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		d := xml.NewDecoder(bytes.NewReader(body))
		d.Strict = true
		for {
			_, err := d.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err, "%s", name)
		}
	}
}

func TestDOCXExportInline(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = []Block{tr(cdm.Paragraph, 0, "x", "Обычный **жирный** *курсив* `код` [сайт](https://example.com/?a=1&b=2) [внутр](ch2.html)")}
	files := zipFiles(t, export(t, "docx", b))
	doc := string(files["word/document.xml"])
	require.Contains(t, doc, `<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">жирный</w:t></w:r>`)
	require.Contains(t, doc, `<w:r><w:rPr><w:i/></w:rPr><w:t xml:space="preserve">курсив</w:t></w:r>`)
	require.Contains(t, doc, `<w:rStyle w:val="VerbatimChar"/>`)
	require.Contains(t, doc, `<w:hyperlink r:id="`)
	require.Contains(t, string(files["word/_rels/document.xml.rels"]), `Target="https://example.com/?a=1&amp;b=2" TargetMode="External"`)
	require.Contains(t, doc, `<w:t xml:space="preserve">внутр</w:t>`)
}

func TestDOCXExportImage(t *testing.T) {
	files := zipFiles(t, export(t, "docx", sampleBook(ModeTranslation)))
	doc := string(files["word/document.xml"])
	require.Contains(t, doc, `<wp:extent cx="9525" cy="9525"/>`, "1×1 px при 96 dpi")
	require.Contains(t, doc, `descr="Схема"`)
	require.Contains(t, string(files["word/_rels/document.xml.rels"]), `Target="media/img0001.png"`)

	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = []Block{raw(cdm.Image, "", "src", "pic.webp", "alt", "Веб")}
	b.Assets = func(string) ([]byte, error) { return []byte("RIFF....WEBP"), nil }
	doc = string(zipFiles(t, export(t, "docx", b))["word/document.xml"])
	require.Contains(t, doc, "[Изображение: Веб]", "WebP Word не показывает — подпись вместо картинки")
}

func TestDOCXImageExtentLimited(t *testing.T) {
	cx, cy := imageExtent(3000, 1500)
	require.Equal(t, int64(5486400), cx, "не шире 6 дюймов")
	require.Equal(t, int64(2743200), cy)
}

func TestDOCXExportRoundTrip(t *testing.T) {
	data := export(t, "docx", sampleBook(ModeTranslation))
	res, err := importers.NewDOCX(importers.ArchiveLimits{MaxEntries: 100, MaxTotalBytes: 1 << 26, MaxRatio: 100}).Load(data, "book.docx")
	require.NoError(t, err)
	require.Empty(t, res.Issues)
	doc := res.Document
	require.Equal(t, "Книга", doc.Meta.Title)
	require.Equal(t, "Автор", doc.Meta.Author)
	require.Equal(t, []string{"Глава 1", "Без заголовка"}, titles(doc.Chapters))
	require.Equal(t, []string{"Раздел"}, titles(doc.Chapters[0].Children))
	var got []string
	for _, blk := range doc.Chapters[0].Blocks {
		got = append(got, string(blk.Type)+":"+blk.Text+metaString(blk.Meta))
	}
	require.Equal(t, []string{
		"heading:Глава 1", "paragraph:Текст с *курсивом* & .",
		"list_item:Один depth=1", "list_item:Вложенный depth=2", "list_item:Первый depth=1 ordered=true",
		"code_block:fmt.Println(\"```\")",
		"table_cell:Имя col=0 header=true row=0", "table_cell:Значение col=1 header=true row=0",
		"table_cell:а|б col=0 row=1", "table_cell:1 col=1 row=1",
		"image: alt=Схема src=word/media/img0001.png",
		"blockquote:Цитата", "paragraph:Untranslated", "paragraph:Строка один\nстрока два",
	}, got)
	require.Equal(t, 2, doc.Chapters[0].Children[0].Blocks[0].Level)
	require.Len(t, res.Assets, 1)
}

func metaString(meta map[string]string) string {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(" " + k + "=" + meta[k])
	}
	return b.String()
}
