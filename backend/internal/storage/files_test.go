package storage_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/storage"
)

var (
	userID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	bookID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func newStore(t *testing.T, maxBytes int64) *storage.FileStore {
	t.Helper()
	s, err := storage.New(t.TempDir(), maxBytes)
	require.NoError(t, err)
	return s
}

func TestPathNeverContainsUserSuppliedName(t *testing.T) {
	s := newStore(t, 1024)
	path, err := s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("hello"))
	require.NoError(t, err)
	// ТЗ §2.2: имена на диске непрогнозируемы и не зависят от имени,
	// которое задал пользователь.
	require.Equal(t, "original.txt", filepath.Base(path))
	require.Contains(t, path, userID.String())
	require.Contains(t, path, bookID.String())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello", string(raw))
}

func TestOriginalCannotBeOverwritten(t *testing.T) {
	// ТЗ §4.1: оригинал хранится неизменным.
	s := newStore(t, 1024)
	_, err := s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("first"))
	require.NoError(t, err)
	_, err = s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("second"))
	require.ErrorIs(t, err, storage.ErrOriginalExists)

	f, err := s.OpenOriginal(userID, bookID, ".txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	require.NoError(t, err)
	require.Equal(t, "first", string(raw))
}

func TestValidateMemberNameRejects(t *testing.T) {
	for _, member := range []string{
		"../escape.md",
		"/absolute/path.md",
		"nested/../../escape.md",
		"a/b/../../../escape.md",
		"./../escape.md",
		`..\..\escape.md`,
		`nested\file.md`,
		"",
		"dir/",
		"bad\x00name.md",
		"C:/windows/system32.md",
	} {
		_, err := storage.ValidateMemberName(member)
		require.ErrorIs(t, err, storage.ErrUnsafePath, "имя %q должно отвергаться", member)
	}
}

func TestValidateMemberNameAccepts(t *testing.T) {
	cases := map[string]string{
		"ch1.md":            "ch1.md",
		"nested/ch2.md":     "nested/ch2.md",
		"a/b/c/ch3.md":      "a/b/c/ch3.md",
		"./intro.md":        "intro.md",
		"a//b.md":           "a/b.md",
		"img/диаграмма.png": "img/диаграмма.png",
	}
	for member, want := range cases {
		got, err := storage.ValidateMemberName(member)
		require.NoError(t, err, "имя %q", member)
		require.Equal(t, want, got)
	}
}

func TestSaveAssetStaysInsideBook(t *testing.T) {
	s := newStore(t, 1024)
	path, err := s.SaveAsset(userID, bookID, "img/d.png", bytes.NewReader([]byte("PNG")))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(path, s.BookDir(userID, bookID)))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "PNG", string(raw))
}

func TestSaveAssetRejectsSymlinkEscape(t *testing.T) {
	// Имя записи безобидно, но каталог внутри книги — симлинк наружу.
	// Проверка строки этого не видит; os.Root не пускает на уровне ОС.
	s := newStore(t, 1024)
	_, err := s.SaveAsset(userID, bookID, "seed.txt", strings.NewReader("x"))
	require.NoError(t, err)

	outside := t.TempDir()
	assets := filepath.Join(s.BookDir(userID, bookID), "assets")
	require.NoError(t, os.Symlink(outside, filepath.Join(assets, "link")))

	_, err = s.SaveAsset(userID, bookID, "link/escape.txt", strings.NewReader("pwned"))
	require.ErrorIs(t, err, storage.ErrUnsafePath)
	_, statErr := os.Stat(filepath.Join(outside, "escape.txt"))
	require.True(t, errors.Is(statErr, os.ErrNotExist), "файл не должен появиться снаружи")
}

func TestSaveAssetRejectsTraversalName(t *testing.T) {
	s := newStore(t, 1024)
	_, err := s.SaveAsset(userID, bookID, "../../escape.txt", strings.NewReader("x"))
	require.ErrorIs(t, err, storage.ErrUnsafePath)
}

func TestOversizedStreamRejectedAndNotLeftBehind(t *testing.T) {
	// Лимит должен срабатывать по ходу чтения, а не после исчерпания диска.
	s := newStore(t, 10)
	_, err := s.SaveOriginal(userID, bookID, ".txt", bytes.NewReader(bytes.Repeat([]byte("y"), 5000)))
	require.ErrorIs(t, err, storage.ErrFileTooLarge)

	entries, err := os.ReadDir(s.BookDir(userID, bookID))
	if err == nil {
		require.Empty(t, entries, "частично записанный файл не должен остаться")
	}
}

func TestOversizedAssetRejected(t *testing.T) {
	s := newStore(t, 10)
	_, err := s.SaveAsset(userID, bookID, "big.png", bytes.NewReader(bytes.Repeat([]byte("y"), 100)))
	require.ErrorIs(t, err, storage.ErrFileTooLarge)
}

func TestExactLimitAccepted(t *testing.T) {
	s := newStore(t, 10)
	_, err := s.SaveOriginal(userID, bookID, ".txt", bytes.NewReader(bytes.Repeat([]byte("y"), 10)))
	require.NoError(t, err, "файл ровно на пределе допустим")
}

func TestUnknownExtensionRejected(t *testing.T) {
	s := newStore(t, 1024)
	_, err := s.SaveOriginal(userID, bookID, ".exe", strings.NewReader("MZ"))
	require.ErrorIs(t, err, storage.ErrDisallowedExtension)
}

func TestExtensionCaseInsensitive(t *testing.T) {
	s := newStore(t, 1024)
	path, err := s.SaveOriginal(userID, bookID, ".EPUB", strings.NewReader("data"))
	require.NoError(t, err)
	require.Equal(t, "original.epub", filepath.Base(path))
}

func TestNormalizedSavedAsJSON(t *testing.T) {
	s := newStore(t, 1024)
	path, err := s.SaveNormalized(userID, bookID, map[string]any{"meta": map[string]string{"title": "Т"}})
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.JSONEq(t, `{"meta":{"title":"Т"}}`, string(raw))
}

func TestDeleteBookRemovesDirectory(t *testing.T) {
	s := newStore(t, 1024)
	_, err := s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("data"))
	require.NoError(t, err)
	require.NoError(t, s.DeleteBook(userID, bookID))
	_, err = os.Stat(s.BookDir(userID, bookID))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestDeleteBookIdempotent(t *testing.T) {
	// Иначе повторная чистка срывается на первой уже удалённой книге.
	s := newStore(t, 1024)
	require.NoError(t, s.DeleteBook(userID, bookID))
	require.NoError(t, s.DeleteBook(userID, bookID))
}

func TestDeleteBookDoesNotTouchOtherBooks(t *testing.T) {
	s := newStore(t, 1024)
	other := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	_, err := s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("a"))
	require.NoError(t, err)
	_, err = s.SaveOriginal(userID, other, ".txt", strings.NewReader("b"))
	require.NoError(t, err)

	require.NoError(t, s.DeleteBook(userID, bookID))
	_, err = os.Stat(s.BookDir(userID, other))
	require.NoError(t, err)
}

func TestFilesNotWorldReadable(t *testing.T) {
	// На общем сервере тексты чужих книг не должны читаться другими
	// системными пользователями.
	s := newStore(t, 1024)
	path, err := s.SaveOriginal(userID, bookID, ".txt", strings.NewReader("private"))
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o007, "права %v", info.Mode().Perm())
}
