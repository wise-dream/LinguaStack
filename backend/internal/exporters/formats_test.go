package exporters

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func TestFormatsRegistered(t *testing.T) {
	require.Equal(t, []string{"epub", "markdown", "docx", "html", "txt"}, Formats())
	_, ok := Get("pdf")
	require.False(t, ok)
}

func TestMarkdownExport(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = slicesWithout(b.Chapters[0].Blocks, cdm.Image)
	x, _ := Get("markdown")
	require.Equal(t, Output{Ext: ".md", ContentType: "text/markdown; charset=utf-8"}, x.Describe(b))
	md := string(export(t, "markdown", b))

	require.True(t, strings.HasPrefix(md, "---\ntitle: \"Книга\"\nauthor: \"Автор\"\nlang: ru\n---\n\n"), md)
	require.Contains(t, md, "\n# Глава 1\n\nТекст с *курсивом* & <b>.\n\n- Один\n  - Вложенный\n1. Первый\n\n")
	require.Contains(t, md, "````go\nfmt.Println(\"```\")\n````\n", "ограждение длиннее серии ``` внутри")
	require.Contains(t, md, "| Имя | Значение |\n| --- | --- |\n| а\\|б | 1 |\n")
	require.Contains(t, md, "\n* * *\n")
	require.Contains(t, md, "\n> Цитата\n")
	require.Contains(t, md, "\nUntranslated\n", "непереведённый блок — оригиналом")
	require.Contains(t, md, "Строка один\\\nстрока два", "жёсткий перенос")
	require.Contains(t, md, "\n## Раздел\n\nВложенный текст.\n")
	require.Contains(t, md, "\n# Без заголовка\n\nГлава без заголовка.\n", "главе без заголовка — заголовок с её названием")
}

func slicesWithout(blocks []Block, t cdm.BlockType) []Block {
	var out []Block
	for _, b := range blocks {
		if b.Type != t {
			out = append(out, b)
		}
	}
	return out
}

func TestMarkdownSoftBreaksKept(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = slicesWithout(b.Chapters[0].Blocks, cdm.Image)
	b.HardBreaks = false
	require.Contains(t, string(export(t, "markdown", b)), "Строка один\nстрока два")
}

func TestMarkdownBilingual(t *testing.T) {
	b := sampleBook(ModeBilingual)
	b.Chapters[0].Blocks = slicesWithout(b.Chapters[0].Blocks, cdm.Image)
	md := string(export(t, "markdown", b))
	require.Contains(t, md, "\n# Глава 1\n\nChapter 1\n\nText with *em* & <b>.\n\nТекст с *курсивом* & <b>.\n")
	require.Contains(t, md, "- One\n- Один\n")
	require.Contains(t, md, "| Name<br>Имя | Value<br>Значение |")
	require.Contains(t, md, "\nUntranslated\n\nLine one", "без перевода — только оригинал")
}

func TestMarkdownWithImagesIsZip(t *testing.T) {
	b := sampleBook(ModeTranslation)
	x, _ := Get("markdown")
	require.Equal(t, Output{Ext: ".zip", ContentType: "application/zip"}, x.Describe(b))
	files := zipFiles(t, export(t, "markdown", b))
	require.Equal(t, tinyPNG, files["images/img0001.png"])
	require.Contains(t, string(files["book.md"]), "![Схема](images/img0001.png)")
}

func TestMarkdownRoundTrip(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = slicesWithout(b.Chapters[0].Blocks, cdm.Image)
	res, err := importers.NewMarkdown().Load(export(t, "markdown", b), "book.md")
	require.NoError(t, err)
	doc := res.Document
	require.Equal(t, "Книга", doc.Meta.Title)
	require.Equal(t, []string{"Глава 1", "Без заголовка"}, titles(doc.Chapters))
	require.Equal(t, []string{"Раздел"}, titles(doc.Chapters[0].Children))
	var cells []string
	for _, blk := range doc.Blocks() {
		if blk.Type == cdm.TableCell {
			cells = append(cells, blk.Text)
		}
	}
	require.Equal(t, []string{"Имя", "Значение", `а\|б`, "1"}, cells)
}

func titles(chs []cdm.Chapter) []string {
	var out []string
	for _, ch := range chs {
		out = append(out, ch.Title)
	}
	return out
}

func TestHTMLExportSelfContained(t *testing.T) {
	page := string(export(t, "html", sampleBook(ModeBilingual)))
	require.True(t, strings.HasPrefix(page, "<!DOCTYPE html>"))
	require.Contains(t, page, `<html lang="ru">`)
	require.Contains(t, page, "<title>Книга</title>")
	require.Contains(t, page, `<a href="#c0">Глава 1</a>`)
	require.Contains(t, page, `<section id="c1">`)
	require.Contains(t, page, `src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(tinyPNG)+`"`)
	require.Contains(t, page, `<p class="original" lang="en">Text with <em>em</em> &amp; .</p>`)
	require.Contains(t, page, `<p class="translation">Текст с <em>курсивом</em> &amp; .</p>`)
	require.NotContains(t, page, "<script")
	require.NotContains(t, page, "<b>")
	_, err := html.Parse(strings.NewReader(page))
	require.NoError(t, err)
}

func TestHTMLExportReimports(t *testing.T) {
	data := export(t, "html", sampleBook(ModeTranslation))
	res, err := importers.NewHTML().Load(data, "book.html")
	require.NoError(t, err)
	require.Equal(t, "Книга", res.Document.Meta.Title)
	require.Equal(t, "ru", res.Document.Meta.Language)
	require.Equal(t, []string{"Глава 1", "Без заголовка"}, titles(res.Document.Chapters))
	require.Equal(t, 1, len(res.Assets), "картинка из data: вернулась ресурсом")
	var items []cdm.Block
	for _, blk := range res.Document.Blocks() {
		if blk.Type == cdm.ListItem {
			items = append(items, blk)
		}
	}
	require.Len(t, items, 3)
	require.Equal(t, "2", items[1].Meta["depth"])
	require.Equal(t, "true", items[2].Meta["ordered"])
}

func TestTXTExport(t *testing.T) {
	txt := export(t, "txt", sampleBook(ModeTranslation))
	require.False(t, bytes.HasPrefix(txt, []byte("\xef\xbb\xbf")), "без BOM")
	require.NotContains(t, string(txt), "\r")
	s := string(txt)
	require.True(t, strings.HasPrefix(s, "Книга\nАвтор\n\n"), s)
	require.Contains(t, s, "\nГлава 1\n\nТекст с курсивом & .\n\n- Один\n  - Вложенный\n1. Первый\n\n")
	require.Contains(t, s, "Имя | Значение\nа|б | 1\n")
	require.Contains(t, s, "[Изображение: Схема]")
	require.Contains(t, s, "fmt.Println(\"```\")")
	require.Contains(t, s, "Строка один\nстрока два")
}

func TestTXTBilingual(t *testing.T) {
	s := string(export(t, "txt", sampleBook(ModeBilingual)))
	require.Contains(t, s, "Text with em & .\nТекст с курсивом & .\n\n", "пара — оригинал и перевод подряд")
}

func TestImageWithoutAltCaption(t *testing.T) {
	b := sampleBook(ModeTranslation)
	b.Chapters[0].Blocks = []Block{raw(cdm.Image, "", "src", "gone.png")}
	require.Contains(t, string(export(t, "txt", b)), "\n[Изображение]\n")
	require.Contains(t, string(export(t, "html", b)), "[Изображение]</p>")
	require.Equal(t, "[Изображение]", inlinePlain("![](a.png)", false))
}

func TestZipEntriesDated(t *testing.T) {
	for _, format := range []string{"epub", "markdown", "docx"} {
		data := export(t, format, sampleBook(ModeTranslation))
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)
		for _, f := range r.File {
			require.Greater(t, f.Modified.Year(), 2020, "%s: %s датирован %v", format, f.Name, f.Modified)
		}
	}
}
