package importers_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func texts(blocks []cdm.Block) []string {
	out := make([]string, len(blocks))
	for i, b := range blocks {
		out[i] = b.Text
	}
	return out
}

func issueCodes(issues []importers.Issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.Code
	}
	return out
}

func loadTXT(t *testing.T, data []byte) importers.Result {
	t.Helper()
	res, err := importers.NewTXT().Load(data, "book.txt")
	require.NoError(t, err)
	return res
}

func TestTXTSplitsParagraphsOnBlankLines(t *testing.T) {
	res := loadTXT(t, []byte("Первый абзац.\n\nВторой абзац.\n\n\nТретий."))
	require.Equal(t, []string{"Первый абзац.", "Второй абзац.", "Третий."}, texts(res.Document.TranslatableBlocks()))
	for _, b := range res.Document.Blocks() {
		require.Equal(t, cdm.Paragraph, b.Type)
	}
}

func TestTXTJoinsHardWrappedLines(t *testing.T) {
	// Жёсткие переносы посреди предложения — вёрстка, а не структура.
	res := loadTXT(t, []byte("Это длинное предложение,\nсвёрстанное по ширине\nстроки.\n\nНовый абзац."))
	require.Equal(t, []string{"Это длинное предложение, свёрстанное по ширине строки.", "Новый абзац."},
		texts(res.Document.Blocks()))
}

func TestTXTStripsUTF8BOM(t *testing.T) {
	res := loadTXT(t, []byte("\ufeffТекст с BOM."))
	require.Equal(t, []string{"Текст с BOM."}, texts(res.Document.Blocks()))
}

func TestTXTHandlesCRLFAndCR(t *testing.T) {
	require.Equal(t, []string{"A", "B"}, texts(loadTXT(t, []byte("A\r\n\r\nB")).Document.Blocks()))
	require.Equal(t, []string{"A", "B"}, texts(loadTXT(t, []byte("A\r\rB")).Document.Blocks()))
}

func TestTXTBlankLineWithSpacesSeparatesParagraphs(t *testing.T) {
	require.Equal(t, []string{"A", "B"}, texts(loadTXT(t, []byte("A\n   \t\nB")).Document.Blocks()))
}

func TestTXTDecodesCP1251WithIssue(t *testing.T) {
	// ТЗ §3.1: TXT обязан обрабатывать кодировки, а не ломаться на них.
	raw, err := charmap.Windows1251.NewEncoder().Bytes([]byte("Привет, мир"))
	require.NoError(t, err)
	res := loadTXT(t, raw)
	require.Equal(t, []string{"Привет, мир"}, texts(res.Document.Blocks()))
	require.Contains(t, issueCodes(res.Issues), "encoding_guessed")
}

func TestTXTDecodesUTF16WithBOM(t *testing.T) {
	// «Unicode»-текст из Блокнота Windows — UTF-16 LE с BOM.
	raw, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte("Абзац один.\r\n\r\nАбзац два."))
	require.NoError(t, err)
	res := loadTXT(t, raw)
	require.Equal(t, []string{"Абзац один.", "Абзац два."}, texts(res.Document.Blocks()))
	require.NotContains(t, issueCodes(res.Issues), "encoding_guessed", "BOM однозначно задаёт кодировку")
}

func TestTXTEmptyFileIsIssueNotError(t *testing.T) {
	for _, data := range [][]byte{nil, []byte(""), []byte("  \n\n\t \n"), []byte("\ufeff")} {
		res := loadTXT(t, data)
		require.Empty(t, res.Document.Blocks())
		require.Contains(t, issueCodes(res.Issues), "empty_document")
		require.Equal(t, importers.Error, res.Issues[0].Severity)
	}
}

func TestTXTBinaryContentIsIssue(t *testing.T) {
	// Двоичный файл с расширением .txt не должен уйти в перевод мусором.
	res := loadTXT(t, []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff"))
	require.Empty(t, res.Document.Blocks())
	require.Contains(t, issueCodes(res.Issues), "binary_content")
}

func TestTXTTitleFromFilename(t *testing.T) {
	res, err := importers.NewTXT().Load([]byte("Текст."), "uploads/Война и мир.txt")
	require.NoError(t, err)
	require.Equal(t, "Война и мир", res.Document.Meta.Title)
	require.Equal(t, "txt", res.Document.Meta.SourceFormat)
}

func TestTXTBlocksHaveIDsAndHashes(t *testing.T) {
	res := loadTXT(t, []byte("Один.\n\nДва."))
	blocks := res.Document.Blocks()
	require.Equal(t, "ch0000/b0000", blocks[0].NodeID)
	require.Equal(t, cdm.ContentHash("Два."), blocks[1].SourceHash)
	require.NoError(t, res.Document.Validate())
}
