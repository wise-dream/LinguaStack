// Package storage хранит оригиналы, нормализованные документы и ресурсы книг.
//
// Раскладка: {root}/{user_uuid}/{book_uuid}/original{ext}, normalized.json,
// assets/… Два свойства существенны (ТЗ §2.2, §13, дополнение §11):
//
//   - имя файла, заданное пользователем, никогда не попадает в путь на
//     диске — только UUID; идентификаторы принимаются типом uuid.UUID,
//     поэтому обход каталога через идентификатор невозможен по построению;
//   - пути из внешних данных (имена записей архивов) проверяются, а запись
//     ресурсов идёт через os.Root, который на уровне ОС не выпускает за
//     пределы каталога книги.
package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	dirPerm  = 0o750
	filePerm = 0o600
)

var (
	ErrUnsafePath          = errors.New("небезопасный путь")
	ErrFileTooLarge        = errors.New("файл превышает допустимый размер")
	ErrDisallowedExtension = errors.New("расширение файла не поддерживается")
	ErrOriginalExists      = errors.New("оригинал книги уже сохранён")
)

var allowedSuffixes = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".epub": true, ".zip": true,
	".pdf": true, ".docx": true, ".html": true, ".htm": true,
}

// NormalizeSuffix приводит расширение к нижнему регистру и сверяет с allow-list.
func NormalizeSuffix(suffix string) (string, error) {
	ext := strings.ToLower(suffix)
	if !allowedSuffixes[ext] {
		return "", fmt.Errorf("%w: %q", ErrDisallowedExtension, suffix)
	}
	return ext, nil
}

// ValidateMemberName проверяет имя записи архива и возвращает его в
// каноническом виде (прямые слеши, без "." и повторных разделителей).
//
// Отвергаются: пустые имена и каталоги, абсолютные пути, буквы дисков,
// компоненты "..", обратные слеши (разделитель Windows не должен обходить
// проверку на POSIX) и NUL.
func ValidateMemberName(member string) (string, error) {
	unsafe := func() (string, error) { return "", fmt.Errorf("%w: %q", ErrUnsafePath, member) }

	if member == "" || strings.HasSuffix(member, "/") || strings.ContainsAny(member, "\\\x00") {
		return unsafe()
	}
	if strings.HasPrefix(member, "/") || (len(member) >= 2 && member[1] == ':') {
		return unsafe()
	}
	for _, part := range strings.Split(member, "/") {
		if part == ".." {
			return unsafe()
		}
	}
	clean := path.Clean(member)
	if !fs.ValidPath(clean) || clean == "." {
		return unsafe()
	}
	return clean, nil
}

// FileStore — файловое хранилище книг.
type FileStore struct {
	root     string
	maxBytes int64
}

// New создаёт хранилище в каталоге root.
func New(root string, maxBytes int64) (*FileStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("каталог хранилища: %w", err)
	}
	if err := os.MkdirAll(abs, dirPerm); err != nil {
		return nil, fmt.Errorf("каталог хранилища: %w", err)
	}
	return &FileStore{root: abs, maxBytes: maxBytes}, nil
}

// BookDir — каталог книги.
func (s *FileStore) BookDir(userID, bookID uuid.UUID) string {
	return filepath.Join(s.root, userID.String(), bookID.String())
}

func (s *FileStore) ensureBookDir(userID, bookID uuid.UUID) (string, error) {
	dir := s.BookDir(userID, bookID)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("каталог книги: %w", err)
	}
	return dir, nil
}

// SaveOriginal сохраняет исходный файл книги.
//
// Запись идёт во временный файл, который затем жёстко связывается с
// итоговым именем: os.Link атомарен и отказывает, если файл уже есть, —
// так оригинал нельзя перезаписать (ТЗ §4.1), а оборванная загрузка не
// оставляет полуфайл под итоговым именем.
func (s *FileStore) SaveOriginal(userID, bookID uuid.UUID, suffix string, r io.Reader) (string, error) {
	ext, err := NormalizeSuffix(suffix)
	if err != nil {
		return "", err
	}
	dir, err := s.ensureBookDir(userID, bookID)
	if err != nil {
		return "", err
	}
	final := filepath.Join(dir, "original"+ext)
	if _, err := os.Lstat(final); err == nil {
		return "", ErrOriginalExists
	}

	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("временный файл: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := s.copyLimited(tmp, r); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("запись оригинала: %w", err)
	}
	if err := os.Link(tmp.Name(), final); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", ErrOriginalExists
		}
		return "", fmt.Errorf("сохранение оригинала: %w", err)
	}
	return final, nil
}

// OpenOriginal открывает оригинал на чтение.
func (s *FileStore) OpenOriginal(userID, bookID uuid.UUID, suffix string) (*os.File, error) {
	ext, err := NormalizeSuffix(suffix)
	if err != nil {
		return nil, err
	}
	// Путь собран из uuid.UUID и расширения из allow-list — пользователь на
	// него не влияет.
	return os.Open(filepath.Join(s.BookDir(userID, bookID), "original"+ext)) //nolint:gosec // G304: см. выше
}

// SaveNormalized записывает нормализованный документ в JSON.
func (s *FileStore) SaveNormalized(userID, bookID uuid.UUID, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("сериализация документа: %w", err)
	}
	dir, err := s.ensureBookDir(userID, bookID)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".normalized-*")
	if err != nil {
		return "", fmt.Errorf("временный файл: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("запись документа: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("запись документа: %w", err)
	}
	final := filepath.Join(dir, "normalized.json")
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", fmt.Errorf("запись документа: %w", err)
	}
	return final, nil
}

// SaveAsset сохраняет ресурс книги (изображение из EPUB или ZIP) под
// именем записи архива внутри assets/ (ТЗ §3.1, §4.3).
func (s *FileStore) SaveAsset(userID, bookID uuid.UUID, member string, r io.Reader) (string, error) {
	name, err := ValidateMemberName(member)
	if err != nil {
		return "", err
	}
	dir, err := s.ensureBookDir(userID, bookID)
	if err != nil {
		return "", err
	}
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, dirPerm); err != nil {
		return "", fmt.Errorf("каталог ресурсов: %w", err)
	}

	root, err := os.OpenRoot(assets)
	if err != nil {
		return "", fmt.Errorf("каталог ресурсов: %w", err)
	}
	defer func() { _ = root.Close() }()

	if err := rejectSymlinks(root, name); err != nil {
		return "", err
	}
	if parent := path.Dir(name); parent != "." {
		if err := root.MkdirAll(parent, dirPerm); err != nil {
			return "", fmt.Errorf("каталог ресурса: %w", err)
		}
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return "", fmt.Errorf("создание ресурса %q: %w", name, err)
	}
	if err := s.copyLimited(f, r); err != nil {
		_ = f.Close()
		_ = root.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("запись ресурса: %w", err)
	}
	return filepath.Join(assets, filepath.FromSlash(name)), nil
}

// rejectSymlinks отвергает путь, если любой его существующий компонент —
// символическая ссылка. Каталог ресурсов создаём только мы и ссылок туда
// не кладём, поэтому ссылка означает подмену. os.Root остаётся второй
// линией защиты — от подмены между этой проверкой и записью.
func rejectSymlinks(root *os.Root, name string) error {
	parts := strings.Split(name, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		info, err := root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // дальше по пути ничего не существует
		}
		if err != nil {
			return fmt.Errorf("%w: %q: %w", ErrUnsafePath, name, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: символическая ссылка в пути %q", ErrUnsafePath, name)
		}
	}
	return nil
}

// copyLimited копирует не больше maxBytes; превышение — ErrFileTooLarge.
// Лимит срабатывает по ходу чтения, а не после исчерпания диска.
func (s *FileStore) copyLimited(dst io.Writer, src io.Reader) error {
	n, err := io.Copy(dst, io.LimitReader(src, s.maxBytes+1))
	if err != nil {
		return fmt.Errorf("запись файла: %w", err)
	}
	if n > s.maxBytes {
		return fmt.Errorf("%w: больше %d байт", ErrFileTooLarge, s.maxBytes)
	}
	return nil
}

// DeleteBook удаляет все файлы книги (ТЗ §13). Идемпотентно: отсутствие
// каталога — не ошибка, иначе повторная чистка срывается.
func (s *FileStore) DeleteBook(userID, bookID uuid.UUID) error {
	if err := os.RemoveAll(s.BookDir(userID, bookID)); err != nil {
		return fmt.Errorf("удаление файлов книги: %w", err)
	}
	return nil
}
