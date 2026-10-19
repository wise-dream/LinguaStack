package importers_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func loadDOCX(t *testing.T, data []byte) importers.Result {
	t.Helper()
	res, err := importers.NewDOCX(testLimits).Load(data, "book.docx")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func TestDOCXHeadingsFromStyles(t *testing.T) {
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: wp("", wr("Вступление.", "")) +
		wp("Heading1", wr("Глава 1", "")) + wp("", wr("Первый абзац.", "")) +
		wp("Heading2", wr("Раздел 1.1", "")) + wp("", wr("Вложенный.", "")) +
		wp("Heading1", wr("Глава 2", "")) + wp("", wr("Второй.", ""))}))
	doc := res.Document
	require.Equal(t, "docx", doc.Meta.SourceFormat)
	require.Equal(t, []string{"book", "Глава 1", "Глава 2"}, chapterTitles(doc.Chapters))
	require.Equal(t, []string{"Раздел 1.1"}, chapterTitles(doc.Chapters[1].Children))
	h := doc.Chapters[1].Blocks[0]
	require.Equal(t, cdm.Heading, h.Type)
	require.Equal(t, 1, h.Level)
	require.Equal(t, "Глава 1", h.Text, "жирность стиля заголовка — не разметка текста")
	require.Equal(t, 2, doc.Chapters[1].Children[0].Blocks[0].Level)
}

func TestDOCXLocalizedHeadingStyle(t *testing.T) {
	// Русский Word: styleId="1" с именем "heading 1".
	styles := `<w:style w:type="paragraph" w:styleId="1"><w:name w:val="heading 1"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="a3"><w:name w:val="Title"/></w:style>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{styles: styles,
		body: wp("a3", wr("Книга", "")) + wp("1", wr("Глава", "")) + wp("", wr("Текст.", ""))}))
	require.Equal(t, []string{"Книга", "Глава"}, chapterTitles(res.Document.Chapters))
	require.Equal(t, "Книга", res.Document.Meta.Title, "без docProps — название из абзаца стиля Title")
}

func TestDOCXOutlineLevel(t *testing.T) {
	body := `<w:p><w:pPr><w:outlineLvl w:val="1"/></w:pPr>` + wr("Раздел", "") + `</w:p>` + wp("", wr("Текст.", ""))
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body}))
	blocks := res.Document.Blocks()
	require.Equal(t, cdm.Heading, blocks[0].Type)
	require.Equal(t, 2, blocks[0].Level)
}

func TestDOCXInlineFormatting(t *testing.T) {
	body := wp("", wr("Обычный ", "")+wr("жирный", "<w:b/>")+wr(" и ", "")+wr("курсив", "<w:i/>")+
		wr(" и ", "")+wr("код", `<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`)+
		wr(" и ", "")+wr("стиль", `<w:rStyle w:val="VerbatimChar"/>`)+wr(", ", "")+
		wr("раз", "<w:b/>")+wr("два", "<w:b/>")+wr(" не жирный", `<w:b w:val="0"/>`)+wr(" 2*3", ""))
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body}))
	require.Equal(t, []string{"Обычный **жирный** и *курсив* и `код` и `стиль`, **раздва** не жирный 2\\*3"},
		texts(res.Document.Blocks()))
}

func TestDOCXHyperlink(t *testing.T) {
	body := wp("", wr("См. ", "")+`<w:hyperlink r:id="rId1">`+wr("сайт", "")+`</w:hyperlink>`+
		wr(" и ", "")+`<w:hyperlink w:anchor="_Toc1">`+wr("раздел", "")+`</w:hyperlink>`+wr(".", ""))
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body, rels: map[string]string{"rId1": "ext:https://example.com/a?b=1"}}))
	require.Equal(t, []string{"См. [сайт](https://example.com/a?b=1) и раздел."}, texts(res.Document.Blocks()))
}

const testNumbering = `<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl>` +
	`<w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>` +
	`<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>` +
	`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>`

func listPara(numID, ilvl, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` +
		numID + `"/></w:numPr></w:pPr>` + wr(text, "") + `</w:p>`
}

func TestDOCXLists(t *testing.T) {
	body := listPara("1", "0", "Пункт") + listPara("1", "1", "Вложенный") + listPara("2", "0", "Первый") +
		`<w:p><w:pPr><w:numPr><w:numId w:val="0"/></w:numPr></w:pPr>` + wr("Не список", "") + `</w:p>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body, numbering: testNumbering}))
	blocks := res.Document.Blocks()
	require.Len(t, blocks, 4)
	require.Equal(t, cdm.ListItem, blocks[0].Type)
	require.Equal(t, map[string]string{"depth": "1"}, blocks[0].Meta)
	require.Equal(t, map[string]string{"depth": "2"}, blocks[1].Meta)
	require.Equal(t, map[string]string{"depth": "1", "ordered": "true"}, blocks[2].Meta)
	require.Equal(t, cdm.Paragraph, blocks[3].Type, "numId=0 снимает нумерацию")
}

func TestDOCXListsWithoutNumberingPart(t *testing.T) {
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: listPara("5", "0", "Пункт")}))
	blocks := res.Document.Blocks()
	require.Equal(t, cdm.ListItem, blocks[0].Type)
	require.Equal(t, map[string]string{"depth": "1"}, blocks[0].Meta, "без numbering.xml — маркированный")
}

func TestDOCXTable(t *testing.T) {
	cell := func(text, props string) string { return "<w:tc>" + props + wp("", wr(text, "")) + "</w:tc>" }
	body := `<w:tbl><w:tblPr/><w:tblGrid/>` +
		`<w:tr><w:trPr><w:tblHeader/></w:trPr>` + cell("Имя", "") + cell("Значение", "") + `</w:tr>` +
		`<w:tr>` + cell("a", `<w:tcPr><w:vMerge w:val="restart"/></w:tcPr>`) +
		`<w:tc>` + wp("", wr("1", "")) + wp("", wr("строка 2", "")) + `</w:tc></w:tr>` +
		`<w:tr>` + cell("", `<w:tcPr><w:vMerge/></w:tcPr>`) + cell("2", "") + `</w:tr></w:tbl>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body}))
	cells := blocksOf(res.Document, cdm.TableCell)
	require.Equal(t, []string{"Имя", "Значение", "a", "1\nстрока 2", "2"}, texts(cells))
	require.Equal(t, map[string]string{"row": "0", "col": "0", "header": "true"}, cells[0].Meta)
	require.Equal(t, map[string]string{"row": "1", "col": "1"}, cells[3].Meta)
	require.Equal(t, map[string]string{"row": "2", "col": "1"}, cells[4].Meta, "продолжение объединения пропущено, колонка — по сетке")
}

func TestDOCXImage(t *testing.T) {
	body := wp("", wr("Текст.", "")) + wp("", wimage("rId1", "Схема")) +
		wp("", wr("Рядом ", "")+wimage("rId1", "мини")) + wp("", wimage("rId2", "")) + wp("", wimage("rId3", ""))
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body,
		rels:  map[string]string{"rId1": "media/image1.png", "rId2": "media/image2.emf", "rId3": "media/gone.png"},
		media: map[string][]byte{"media/image1.png": tinyPNG, "media/image2.emf": []byte("emf")}}))
	images := blocksOf(res.Document, cdm.Image)
	require.Len(t, images, 2, "PNG и отсутствующий PNG; EMF не показывается")
	require.Equal(t, map[string]string{"src": "word/media/image1.png", "alt": "Схема"}, images[0].Meta)
	require.Contains(t, texts(res.Document.Blocks()), "Рядом ![мини](word/media/image1.png)")
	require.Len(t, res.Assets, 1)
	require.Equal(t, "word/media/image1.png", res.Assets[0].Name)
	unsupported := issueWith(res.Issues, "unsupported_image")
	require.NotNil(t, unsupported)
	require.Contains(t, unsupported.Message, "word/media/image2.emf")
	missing := issueWith(res.Issues, "missing_resource")
	require.NotNil(t, missing)
	require.Contains(t, missing.Message, "word/media/gone.png")
}

func TestDOCXCodeStyle(t *testing.T) {
	code := func(text string) string { return wp("SourceCode", wr(text, "")) }
	body := wp("", wr("До.", "")) + code("func main() {") + `<w:p><w:pPr><w:pStyle w:val="SourceCode"/></w:pPr>` +
		`<w:r><w:tab/><w:t>fmt.Println("*x*")</w:t></w:r></w:p>` + code("}") + wp("", wr("После.", ""))
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body}))
	blocks := res.Document.Blocks()
	require.Len(t, blocks, 3)
	require.Equal(t, cdm.CodeBlock, blocks[1].Type)
	require.Equal(t, "func main() {\n\tfmt.Println(\"*x*\")\n}", blocks[1].Text, "код без экранирования разметки")
}

func TestDOCXQuoteAndCaption(t *testing.T) {
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: wp("Quote", wr("Цитата.", "")) + wp("Caption", wr("Рис. 1", ""))}))
	blocks := res.Document.Blocks()
	require.Equal(t, cdm.Blockquote, blocks[0].Type)
	require.Equal(t, cdm.Caption, blocks[1].Type)
}

func TestDOCXFootnotes(t *testing.T) {
	body := wp("Heading1", wr("Глава", "")) + wp("", wr("Текст", "")+`<w:r><w:footnoteReference w:id="2"/></w:r>`+wr(" дальше.", "")) +
		wp("", wr("Второй абзац.", "")) + wp("Heading1", wr("Следующая", "")) + wp("", wr("Без сносок.", ""))
	notes := `<w:footnote w:id="2"><w:p><w:r><w:footnoteRef/></w:r>` + wr(" Пояснение.", "") + `</w:p></w:footnote>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body, footnotes: notes}))
	ch := res.Document.Chapters[0]
	require.Equal(t, []string{"Глава", "Текст\\[1\\] дальше.", "Второй абзац.", "\\[1\\] Пояснение."}, texts(ch.Blocks),
		"сноска — в конце своей главы")
	require.Equal(t, map[string]string{"note": "1"}, ch.Blocks[3].Meta)
	require.Equal(t, []string{"Следующая", "Без сносок."}, texts(res.Document.Chapters[1].Blocks))
}

func TestDOCXSkipsWordTOC(t *testing.T) {
	sdt := `<w:sdt><w:sdtPr><w:docPartObj><w:docPartGallery w:val="Table of Contents"/></w:docPartObj></w:sdtPr>` +
		`<w:sdtContent>` + wp("", wr("Оглавление", "")) + wp("TOC1", wr("Глава 1 ..... 3", "")) + `</w:sdtContent></w:sdt>`
	other := `<w:sdt><w:sdtPr/><w:sdtContent>` + wp("", wr("Поле формы.", "")) + `</w:sdtContent></w:sdt>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: sdt + wp("TOC1", wr("Глава 2 ..... 5", "")) + other +
		wp("Heading1", wr("Глава 1", ""))}))
	require.Equal(t, []string{"Поле формы.", "Глава 1"}, texts(res.Document.Blocks()))
}

func TestDOCXAlternateContent(t *testing.T) {
	box := func(text string) string {
		return `<w:txbxContent>` + wp("", wr(text, "")) + `</w:txbxContent>`
	}
	body := wp("", wr("До поля. ", "")+`<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:anchor><a:graphic><a:graphicData>`+
		`<wps:wsp><wps:txbx>`+box("Текст в поле.")+`</wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:anchor></w:drawing></mc:Choice>`+
		`<mc:Fallback><w:pict><v:shape><v:textbox>`+box("Текст в поле.")+`</v:textbox></v:shape></w:pict></mc:Fallback>`+
		`</mc:AlternateContent></w:r>`) + `<w:del><w:r><w:delText>Удалённое</w:delText></w:r></w:del>`
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: body}))
	require.Equal(t, []string{"До поля.", "Текст в поле."}, texts(res.Document.Blocks()))
}

func TestDOCXCoreProperties(t *testing.T) {
	res := loadDOCX(t, buildDOCX(t, docxOptions{body: wp("", wr("Текст.", "")),
		core: `<dc:title>Отчёт</dc:title><dc:creator>Автор</dc:creator><dc:language>ru-RU</dc:language>`}))
	meta := res.Document.Meta
	require.Equal(t, "Отчёт", meta.Title)
	require.Equal(t, "Автор", meta.Author)
	require.Equal(t, "ru-RU", meta.Language)
}

func TestDOCXLanguageFromStyleDefaults(t *testing.T) {
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:lang w:val="en-US"/></w:rPr></w:rPrDefault></w:docDefaults>` + defaultDocxStyles
	res := loadDOCX(t, buildDOCX(t, docxOptions{styles: styles, body: wp("", wr("Text.", ""))}))
	require.Equal(t, "en-US", res.Document.Meta.Language)
}

func TestDOCXWithoutStylesAndNumbering(t *testing.T) {
	res := loadDOCX(t, buildDOCX(t, docxOptions{noStyles: true, body: wp("Heading1", wr("Глава", "")) + wp("", wr("Текст.", ""))}))
	require.Equal(t, []string{"Глава", "Текст."}, texts(res.Document.Blocks()))
	require.Empty(t, res.Issues)
}

func TestDOCXCorrupt(t *testing.T) {
	res := loadDOCX(t, []byte("not a zip"))
	require.NotNil(t, issueWith(res.Issues, "corrupt_file"))

	res = loadDOCX(t, makeZip(t, files("readme.txt", "x")...))
	require.NotNil(t, issueWith(res.Issues, "corrupt_file"))

	res = loadDOCX(t, buildDOCX(t, docxOptions{raw: map[string]string{
		"word/document.xml": `<w:document ` + docxNS + `><w:body>` + wp("", wr("Целый абзац.", "")) + `<w:p><w:r>`,
	}}))
	require.Equal(t, []string{"Целый абзац."}, texts(res.Document.Blocks()), "прочитанное до повреждения сохраняется")
	require.NotNil(t, issueWith(res.Issues, "corrupt_entry"))
}

func TestDOCXArchiveLimits(t *testing.T) {
	data := buildDOCX(t, docxOptions{body: wp("", wr("Текст.", ""))})
	_, err := importers.NewDOCX(importers.ArchiveLimits{MaxEntries: 1}).Load(data, "book.docx")
	require.True(t, errors.Is(err, importers.ErrArchiveLimits), "err = %v", err)
}

func TestDOCXRenamedZipSniffed(t *testing.T) {
	data := buildDOCX(t, docxOptions{body: wp("", wr("Текст.", ""))})
	imp, err := defaultRegistry(t).Pick(data, "отчёт.zip")
	require.NoError(t, err)
	require.Equal(t, "docx", imp.FormatID())

	imp, err = defaultRegistry(t).Pick(makeZip(t, files("a.md", "# A")...), "repo.zip")
	require.NoError(t, err)
	require.Equal(t, "zip_repo", imp.FormatID())
}
