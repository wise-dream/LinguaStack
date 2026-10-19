package importers

import (
	"fmt"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/pdftext"
)

// pdfInstances — одновременных разборов PDF на процесс: каждый экземпляр
// PDFium — это своя память модуля wasm.
const pdfInstances = 2

// Registry выбирает импортёр для загруженного файла.
type Registry struct {
	importers []Importer
}

// NewRegistry собирает реестр. Порядок важен: специфичные форматы
// проверяются раньше общих (EPUB — это тоже ZIP).
func NewRegistry(importers ...Importer) *Registry {
	return &Registry{importers: importers}
}

// Default — реестр со всеми поддерживаемыми форматами.
func Default(cfg config.Settings) *Registry {
	limits := LimitsFrom(cfg)
	pdf := pdftext.NewPDFium(pdftext.Options{MaxPages: cfg.PDFMaxPages, Timeout: cfg.PDFTimeout,
		MemoryLimitMB: cfg.PDFMemoryMB, Instances: pdfInstances})
	// PDF — последним: он узнаётся и по содержимому.
	return NewRegistry(NewEPUB(limits), NewDOCX(limits), NewZipRepo(limits), NewMarkdown(), NewHTML(), NewTXT(), NewPDF(pdf))
}

// Pick возвращает первый импортёр, узнавший файл.
func (r *Registry) Pick(data []byte, filename string) (Importer, error) {
	for _, imp := range r.importers {
		if imp.Sniff(data, filename) {
			return imp, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, filename)
}
