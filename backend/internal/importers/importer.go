// Package importers приводит документы разных форматов к Canonical
// Document Model (ТЗ §4.2): по адаптеру на формат, ядро перевода о форматах
// не знает.
package importers

import (
	"errors"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
)

// Severity — важность замечания импорта.
type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

// Issue — замечание, которое пользователь видит до запуска перевода
// (ТЗ §3.1: о проблемах структуры, повреждённых файлах и отсутствующих
// ресурсах сообщается до перевода).
type Issue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	NodeID   string   `json:"node_id,omitempty"`
}

// Result — итог импорта.
//
// Два канала ошибок разделены намеренно. Issues — проблемы содержимого,
// которые пользователь проверяет и решает сам (пустой документ, угаданная
// кодировка, отсутствующая глава). error из Load — отказ принять файл
// вообще: нарушение безопасности или лимитов архива.
type Result struct {
	Document cdm.Document
	Issues   []Issue
}

// Importer — адаптер одного формата.
type Importer interface {
	FormatID() string
	// Sniff сообщает, похож ли файл на этот формат.
	Sniff(data []byte, filename string) bool
	Load(data []byte, filename string) (Result, error)
}

// ErrUnsupportedFormat — ни один импортёр не узнал файл.
var ErrUnsupportedFormat = errors.New("формат файла не поддерживается")

func extension(filename string) string {
	return strings.ToLower(path.Ext(strings.ReplaceAll(filename, "\\", "/")))
}

// titleFromFilename — название по умолчанию: имя файла без каталога и
// расширения. Метаданные формата, если есть, его перекрывают.
func titleFromFilename(filename string) string {
	base := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	return strings.TrimSuffix(base, path.Ext(base))
}

// sanitizeUTF8 заменяет невалидные последовательности: PostgreSQL не примет
// такой текст в JSONB, а молча выбросить байты — значит испортить книгу
// незаметно. Замена видна пользователю через Issue.
func sanitizeUTF8(s string) (string, []Issue) {
	if utf8.ValidString(s) {
		return s, nil
	}
	return strings.ToValidUTF8(s, "�"), []Issue{{
		Severity: Warning,
		Code:     "invalid_utf8",
		Message:  "В файле есть некорректные последовательности UTF-8; они заменены символом «�».",
	}}
}

func emptyDocumentIssue() Issue {
	return Issue{Severity: Error, Code: "empty_document", Message: "В документе нет текста для перевода."}
}
