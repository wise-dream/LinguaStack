package importers_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/pdftext"
)

var realPDF = importers.NewPDF(pdftext.NewPDFium(pdftext.Options{
	MaxPages: 100, Timeout: time.Minute, MemoryLimitMB: 512, Instances: 2}))

func loadPDFFixture(t *testing.T, name string) importers.Result {
	t.Helper()
	data, err := os.ReadFile("testdata/pdf/" + name)
	require.NoError(t, err)
	res, err := realPDF.Load(data, name)
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func requireFixtureBook(t *testing.T, doc cdm.Document) {
	t.Helper()
	require.Equal(t, "Пробная книга", doc.Meta.Title)
	require.Equal(t, []string{"Глава первая", "Chapter Two", "Глава третья"}, chapterTitles(doc.Chapters))
	require.Equal(t, []string{"Раздел 1.1"}, chapterTitles(doc.Chapters[0].Children))

	all := texts(doc.Blocks())
	require.Contains(t, all, "Первый абзац главы, достаточно длинный, чтобы перенестись на следующую строку и "+
		"проверить склейку строк в абзац при разборе документа.")
	require.Contains(t, all, "Переносы: интернационализация предоставляет возможность проверить мягкие переносы, "+
		"которые документ ставит в длинных словах: высокопроизводительность, самостоятельность, взаимодействие.")
	require.Contains(t, all, "English paragraph with **bold** text and more words to fill the line completely and wrap it.")
	for _, text := range all {
		require.NotContains(t, text, "колонтитул")
		require.NotContains(t, []string{"1", "2", "3"}, text, "номера страниц не попадают в текст")
	}

	section := doc.Chapters[0].Children[0].Blocks
	var code *cdm.Block
	for i := range section {
		if section[i].Type == cdm.CodeBlock {
			code = &section[i]
		}
	}
	require.NotNil(t, code, "блок кода: %v", texts(section))
	require.Equal(t, "func main() {\n    fmt.Println(\"код не переводится\")\n}", code.Text)

	var ordered []string
	for _, b := range doc.Chapters[1].Blocks {
		if b.Type == cdm.ListItem && b.Meta["ordered"] == "true" {
			ordered = append(ordered, b.Text)
		}
	}
	require.Equal(t, []string{"First item", "Second item"}, ordered)
}

func TestPDFImportOutlineFixture(t *testing.T) {
	res := loadPDFFixture(t, "outline.pdf")
	requireFixtureBook(t, res.Document)
	require.Equal(t, "pdf", res.Document.Meta.SourceFormat)
	require.Nil(t, issueWith(res.Issues, "scanned_pages"))
}

func TestPDFImportPlainFixture(t *testing.T) {
	requireFixtureBook(t, loadPDFFixture(t, "plain.pdf").Document)
}

func TestPDFImportEncrypted(t *testing.T) {
	res := loadPDFFixture(t, "encrypted.pdf")
	issue := issueWith(res.Issues, "encrypted")
	require.NotNil(t, issue)
	require.Equal(t, importers.Error, issue.Severity)
}

func TestPDFImportScannedFixture(t *testing.T) {
	res := loadPDFFixture(t, "scanned.pdf")
	require.NotNil(t, issueWith(res.Issues, "scanned_pdf"))
	require.Empty(t, res.Document.Blocks())
}

func TestPDFImportCorrupt(t *testing.T) {
	res, err := realPDF.Load([]byte("%PDF-1.7\nэто не PDF"), "broken.pdf")
	require.NoError(t, err)
	issue := issueWith(res.Issues, "corrupt_file")
	require.NotNil(t, issue)
	require.True(t, strings.Contains(issue.Message, "PDF"), issue.Message)
}

func TestPickPDF(t *testing.T) {
	imp, err := defaultRegistry(t).Pick([]byte("%PDF-1.4\n..."), "Book.PDF")
	require.NoError(t, err)
	require.Equal(t, "pdf", imp.FormatID())
}
