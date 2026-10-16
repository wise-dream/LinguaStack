package importers

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/storage"
)

// ErrArchiveLimits — архив превышает лимиты (дополнение §10). Это отказ
// принять файл, а не замечание: такой архив — либо ошибка, либо атака.
var ErrArchiveLimits = errors.New("архив превышает допустимые пределы")

// ratioCheckFloor — сжатие проверяется только у крупных записей: маленький
// файл из повторяющихся символов законно сжимается в сотни раз, а угрозы
// не представляет.
const ratioCheckFloor = 1 << 20

// ArchiveLimits — пределы распаковки (ТЗ §13).
type ArchiveLimits struct {
	MaxEntries    int
	MaxTotalBytes int64
	MaxRatio      int
}

// LimitsFrom берёт пределы из настроек.
func LimitsFrom(cfg config.Settings) ArchiveLimits {
	return ArchiveLimits{MaxEntries: cfg.MaxZipEntries, MaxTotalBytes: cfg.MaxZipTotalBytes, MaxRatio: cfg.MaxZipRatio}
}

// Asset — ресурс из архива (картинка), который сохраняется вместе с книгой.
// Содержимое не копируется в память при импорте: Open распаковывает запись
// по требованию.
type Asset struct {
	Name string
	Size int64
	Open func() ([]byte, error)
}

// archiveEntry — проверенная запись архива.
type archiveEntry struct {
	name string // путь от корня книги, со снятой общей корневой папкой
	file *zip.File
}

// openArchive открывает ZIP и проверяет его целиком до чтения содержимого:
// имена (path traversal), символические ссылки, число записей, суммарный
// объявленный размер и степень сжатия. Ошибка — отказ принять файл.
// corrupt == true — архив не читается вовсе; это замечание, а не отказ.
func openArchive(data []byte, limits ArchiveLimits) (entries []archiveEntry, corrupt bool, err error) {
	reader, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zerr != nil {
		return nil, true, nil
	}
	if limits.MaxEntries > 0 && len(reader.File) > limits.MaxEntries {
		return nil, false, fmt.Errorf("%w: записей %d при лимите %d (entries)", ErrArchiveLimits, len(reader.File), limits.MaxEntries)
	}

	var total int64
	for _, f := range reader.File {
		isDir := strings.HasSuffix(f.Name, "/")
		name := strings.TrimSuffix(f.Name, "/")
		if _, err := storage.ValidateMemberName(name); err != nil {
			return nil, false, err
		}
		// Ссылок мы не создаём, но и принимать не станем: дополнение §10.
		if f.Mode()&fs.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("%w: символическая ссылка %q в архиве", storage.ErrUnsafePath, f.Name)
		}
		if isDir {
			continue
		}
		size, err := entrySize(f)
		if err != nil {
			return nil, false, err
		}
		if limits.MaxTotalBytes > 0 && size > limits.MaxTotalBytes {
			return nil, false, fmt.Errorf("%w: запись %q больше %d байт (size)", ErrArchiveLimits, f.Name, limits.MaxTotalBytes)
		}
		if total > math.MaxInt64-size {
			return nil, false, fmt.Errorf("%w: суммарный размер записей переполняет счётчик (size)", ErrArchiveLimits)
		}
		total += size
		if limits.MaxTotalBytes > 0 && total > limits.MaxTotalBytes {
			return nil, false, fmt.Errorf("%w: распакованный размер больше %d байт (size)", ErrArchiveLimits, limits.MaxTotalBytes)
		}
		if limits.MaxRatio > 0 && f.UncompressedSize64 > ratioCheckFloor && f.CompressedSize64 > 0 &&
			f.UncompressedSize64/f.CompressedSize64 > uint64(limits.MaxRatio) {
			return nil, false, fmt.Errorf("%w: запись %q сжата в %d раз (ratio)", ErrArchiveLimits, f.Name,
				f.UncompressedSize64/f.CompressedSize64)
		}
		entries = append(entries, archiveEntry{name: name, file: f})
	}
	return stripCommonRoot(entries), false, nil
}

// readEntry читает запись не больше объявленного размера: заголовок ZIP
// может лгать, и лимит по объявленным размерам иначе обходится.
//
// archive/zip сам обрывает чтение сверх объявленного размера (ErrFormat),
// поэтому проверка длины ниже — вторая линия защиты на случай смены
// реализации чтения.
func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("чтение %q: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	size, err := entrySize(f)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(rc, size+1))
	if err != nil {
		return nil, fmt.Errorf("чтение %q: %w", f.Name, err)
	}
	if int64(len(data)) > size {
		return nil, fmt.Errorf("%w: запись %q больше объявленного размера", ErrArchiveLimits, f.Name)
	}
	return data, nil
}

// isJunk — служебные записи, не относящиеся к книге: метаданные macOS и
// скрытые файлы (.git, .DS_Store). Их имена всё равно проверены выше.
// entrySize — объявленный размер записи как int64. Заголовок с размером
// больше 2^63 — заведомо злонамеренный: при преобразовании он стал бы
// отрицательным и сломал бы любой лимит.
func entrySize(f *zip.File) (int64, error) {
	if f.UncompressedSize64 > math.MaxInt64 {
		return 0, fmt.Errorf("%w: недопустимый размер записи %q", ErrArchiveLimits, f.Name)
	}
	return int64(f.UncompressedSize64), nil
}

func isJunk(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "__MACOSX" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// stripCommonRoot снимает единственную общую корневую папку: архив
// репозитория с GitHub целиком лежит в «repo-main/», и без этого SUMMARY.md
// в корне книги не находится.
func stripCommonRoot(entries []archiveEntry) []archiveEntry {
	var root string
	for _, e := range entries {
		if isJunk(e.name) {
			continue
		}
		first, _, found := strings.Cut(e.name, "/")
		if !found || (root != "" && first != root) {
			return entries
		}
		root = first
	}
	if root == "" {
		return entries
	}
	out := make([]archiveEntry, 0, len(entries))
	for _, e := range entries {
		if rest, ok := strings.CutPrefix(e.name, root+"/"); ok {
			e.name = rest
		}
		out = append(out, e)
	}
	return out
}

func corruptIssue(format string) Issue {
	return Issue{Severity: Error, Code: "corrupt_file", Message: "Не удалось прочитать файл " + format + ": архив повреждён."}
}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".bmp": true, ".avif": true,
}

// collectAssets превращает используемые картинки в ресурсы книги и
// возвращает отсутствующие (ТЗ §3.1, §4.3: о них сообщается до перевода).
func collectAssets(files map[string]*zip.File, images []string) (assets []Asset, missing []string) {
	seen := map[string]bool{}
	for _, name := range images {
		if seen[name] {
			continue
		}
		seen[name] = true
		f, ok := files[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !imageExtensions[fileExt(name)] {
			continue
		}
		size, err := entrySize(f)
		if err != nil {
			continue // уже отсечено в openArchive; повторная проверка дешевле допущения
		}
		assets = append(assets, Asset{Name: name, Size: size, Open: func() ([]byte, error) { return readEntry(f) }})
	}
	return assets, missing
}

func missingResourceIssue(missing []string) Issue {
	return Issue{Severity: Warning, Code: "missing_resource", Message: "Не найдены изображения: " + listNames(missing)}
}
