package exporters

import (
	"errors"
	"io"
)

// epubExporter — EPUB 3 (задача 6 плана этапа 5).
type epubExporter struct{}

func (epubExporter) Format() string { return "epub" }

func (epubExporter) Describe(*Book) Output {
	return Output{Ext: ".epub", ContentType: "application/epub+zip"}
}

func (epubExporter) Write(io.Writer, *Book) error {
	return errors.New("экспорт EPUB ещё не реализован")
}
