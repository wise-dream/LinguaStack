package exporters

import (
	"errors"
	"io"
)

// docxExporter — DOCX (задача 7 плана этапа 5).
type docxExporter struct{}

func (docxExporter) Format() string { return "docx" }

func (docxExporter) Describe(*Book) Output {
	return Output{Ext: ".docx", ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
}

func (docxExporter) Write(io.Writer, *Book) error {
	return errors.New("экспорт DOCX ещё не реализован")
}
