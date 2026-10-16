package importers

import (
	"fmt"

	"github.com/vnovels/linguastack/internal/config"
)

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
	return NewRegistry(NewZipRepo(LimitsFrom(cfg)), NewMarkdown(), NewTXT())
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
