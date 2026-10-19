package exporters

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
)

// tinyPNG — корректный PNG 1×1.
var tinyPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4" +
	"\x89\x00\x00\x00\rIDATx\xdac\xfc\xcf\xc0P\x0f\x00\x04\x85\x01\x80\x84\xa9\x8c!\x00\x00\x00\x00IEND\xaeB`\x82")

func tr(t cdm.BlockType, level int, original, translation string, meta ...string) Block {
	b := Block{Type: t, Level: level, Original: original, Translation: translation, Translated: translation != ""}
	if len(meta) > 0 {
		b.Meta = map[string]string{}
		for i := 0; i+1 < len(meta); i += 2 {
			b.Meta[meta[i]] = meta[i+1]
		}
	}
	return b
}

func raw(t cdm.BlockType, text string, meta ...string) Block {
	b := tr(t, 0, text, "", meta...)
	return b
}

// sampleBook — книга со всеми типами блоков.
func sampleBook(mode Mode) *Book {
	return &Book{
		ID: "urn:uuid:00000000-0000-0000-0000-000000000001", Title: "Книга", Author: "Автор",
		Lang: "ru", SourceLang: "en", Mode: mode, HardBreaks: true,
		Chapters: []Chapter{
			{Index: 0, Title: "Глава 1", Blocks: []Block{
				tr(cdm.Heading, 1, "Chapter 1", "Глава 1"),
				tr(cdm.Paragraph, 0, "Text with *em* & <b>.", "Текст с *курсивом* & <b>."),
				tr(cdm.ListItem, 0, "One", "Один", "depth", "1"),
				tr(cdm.ListItem, 0, "Nested", "Вложенный", "depth", "2"),
				tr(cdm.ListItem, 0, "First", "Первый", "depth", "1", "ordered", "true"),
				raw(cdm.CodeBlock, "fmt.Println(\"```\")", "language", "go"),
				tr(cdm.TableCell, 0, "Name", "Имя", "row", "0", "col", "0", "header", "true"),
				tr(cdm.TableCell, 0, "Value", "Значение", "row", "0", "col", "1", "header", "true"),
				tr(cdm.TableCell, 0, "a|b", "а|б", "row", "1", "col", "0"),
				tr(cdm.TableCell, 0, "1", "1", "row", "1", "col", "1"),
				raw(cdm.Image, "", "src", "img/a.png", "alt", "Схема"),
				raw(cdm.ThematicBreak, ""),
				tr(cdm.Blockquote, 0, "Quote", "Цитата"),
				tr(cdm.Paragraph, 0, "Untranslated", ""),
				tr(cdm.Paragraph, 0, "Line one\nline two", "Строка один\nстрока два"),
			}, Children: []Chapter{
				{Index: 1, Title: "Раздел", Blocks: []Block{
					tr(cdm.Heading, 2, "Section", "Раздел"),
					tr(cdm.Paragraph, 0, "Nested text.", "Вложенный текст."),
				}},
			}},
			{Index: 2, Title: "Без заголовка", Blocks: []Block{
				tr(cdm.Paragraph, 0, "Chapter without heading.", "Глава без заголовка."),
			}},
		},
		Assets: func(name string) ([]byte, error) {
			if name == "img/a.png" {
				return tinyPNG, nil
			}
			return nil, io.ErrUnexpectedEOF
		},
	}
}

func export(t *testing.T, format string, b *Book) []byte {
	t.Helper()
	x, ok := Get(format)
	require.True(t, ok, format)
	var buf bytes.Buffer
	require.NoError(t, x.Write(&buf, b))
	return buf.Bytes()
}

func zipFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	out := map[string][]byte{}
	for _, f := range r.File {
		rc, err := f.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		out[f.Name] = body
	}
	return out
}
