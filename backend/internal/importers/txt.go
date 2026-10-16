package importers

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/vnovels/linguastack/internal/cdm"
)

// TXT — импорт обычного текста (ТЗ §3.1: кодировки, абзацы, текст).
type TXT struct{}

// NewTXT создаёт импортёр TXT.
func NewTXT() Importer { return TXT{} }

func (TXT) FormatID() string { return "txt" }

func (TXT) Sniff(_ []byte, filename string) bool {
	return fileExt(filename) == ".txt"
}

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

func (TXT) Load(data []byte, filename string) (Result, error) {
	builder := cdm.NewBuilder(cdm.BookMeta{Title: titleFromFilename(filename), SourceFormat: "txt"})

	text, issues := decodeText(data)
	if strings.ContainsRune(text, 0) {
		issues = append(issues, Issue{
			Severity: Error, Code: "binary_content",
			Message: "Файл похож на двоичный, а не на текст.",
		})
		return Result{Document: builder.Build(), Issues: issues}, nil
	}

	for _, para := range splitParagraphs(text) {
		builder.Add(cdm.Paragraph, para, 0, nil)
	}
	return finish(builder.Build(), issues), nil
}

// decodeText определяет кодировку: BOM однозначен; без него — UTF-8, если
// байты валидны, иначе windows-1251 как самая вероятная унаследованная
// кодировка для русскоязычных текстов, с предупреждением пользователю.
func decodeText(data []byte) (string, []Issue) {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		return sanitizeUTF8(string(data[len(bomUTF8):]))
	case bytes.HasPrefix(data, bomUTF16LE), bytes.HasPrefix(data, bomUTF16BE):
		decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(data)
		if err != nil {
			return "", []Issue{{Severity: Error, Code: "decode_failed", Message: "Не удалось декодировать UTF-16."}}
		}
		return sanitizeUTF8(string(decoded))
	case utf8.Valid(data):
		return string(data), nil
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(data)
	if err != nil {
		return sanitizeUTF8(string(data))
	}
	return string(decoded), []Issue{{
		Severity: Warning, Code: "encoding_guessed",
		Message: "Файл не в UTF-8; кодировка определена как windows-1251. Проверьте текст перед переводом.",
	}}
}

// splitParagraphs делит текст по пустым строкам. Строки внутри абзаца
// склеиваются пробелом: жёсткие переносы посреди предложения — вёрстка
// исходника, а не его структура, и мешают и переводу, и читалке.
func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var paragraphs, current []string
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, strings.Join(current, " "))
			current = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return paragraphs
}
