package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/storage"
	"github.com/vnovels/linguastack/internal/store"
)

// multipartOverhead — запас на заголовки multipart поверх лимита файла.
const multipartOverhead = 1 << 20

const maxFilenameBytes = 255

type bookOut struct {
	ID               uuid.UUID         `json:"id"`
	Title            string            `json:"title"`
	Author           string            `json:"author,omitempty"`
	Language         string            `json:"language,omitempty"`
	SourceFormat     string            `json:"source_format"`
	OriginalFilename string            `json:"original_filename"`
	OriginalSize     int64             `json:"original_size"`
	Status           string            `json:"status"`
	Chapters         int               `json:"chapters"`
	Blocks           int               `json:"blocks"`
	Translatable     int               `json:"translatable"`
	Issues           []importers.Issue `json:"issues"`
	CreatedAt        time.Time         `json:"created_at"`
}

func bookResponse(b *models.Book) bookOut {
	issues := b.Issues.Data
	if issues == nil {
		issues = []importers.Issue{}
	}
	return bookOut{
		ID: b.ID, Title: b.Title, Author: b.Author, Language: b.Language, SourceFormat: b.SourceFormat,
		OriginalFilename: b.OriginalFilename, OriginalSize: b.OriginalSize, Status: b.Status,
		Chapters: b.ChaptersCount, Blocks: b.BlocksCount, Translatable: b.TranslatableCount,
		Issues: issues, CreatedAt: b.CreatedAt.UTC(),
	}
}

// loadOwned — ресурс пользователя по параметру пути, иначе 404.
// Некорректный UUID — тоже 404: для клиента это просто несуществующий ресурс.
func loadOwned[T any](s *server, w http.ResponseWriter, r *http.Request, param string) (*T, bool) {
	id, err := uuid.Parse(r.PathValue(param))
	if err != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return nil, false
	}
	row, err := store.Owned[T](r.Context(), s.DB, id, UserFromContext(r.Context()).ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return nil, false
	}
	if err != nil {
		internalError(w, r, err)
		return nil, false
	}
	return row, true
}

// readUpload читает поле file из multipart-формы целиком, не больше лимита.
// Импортёрам нужен весь файл (ZIP читается с конца), а лимит не даёт
// исчерпать память.
func (s *server) readUpload(w http.ResponseWriter, r *http.Request) (filename string, data []byte, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxUploadBytes+multipartOverhead)
	tooLarge := func(err error) bool {
		var maxErr *http.MaxBytesError
		return errors.As(err, &maxErr)
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Ожидается multipart/form-data с полем file")
		return "", nil, false
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if tooLarge(err) {
				writeError(w, http.StatusRequestEntityTooLarge, "Файл слишком большой")
			} else {
				writeError(w, http.StatusUnprocessableEntity, "Некорректная multipart-форма")
			}
			return "", nil, false
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		filename = part.FileName()
		data, err = io.ReadAll(io.LimitReader(part, s.Cfg.MaxUploadBytes+1))
		_ = part.Close()
		if tooLarge(err) || int64(len(data)) > s.Cfg.MaxUploadBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "Файл слишком большой")
			return "", nil, false
		}
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "Не удалось прочитать файл")
			return "", nil, false
		}
		break
	}
	if filename == "" {
		writeError(w, http.StatusUnprocessableEntity, "Ожидается multipart/form-data с полем file")
		return "", nil, false
	}
	return filename, data, true
}

// safeFilename — базовое имя файла для метаданных (на диск оно не попадает).
func safeFilename(name string) string {
	base := path.Base(strings.ReplaceAll(name, "\\", "/"))
	for len(base) > maxFilenameBytes {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}
	return base
}

func (s *server) uploadBook(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	rawName, data, ok := s.readUpload(w, r)
	if !ok {
		return
	}
	filename := safeFilename(rawName)
	suffix, err := storage.NormalizeSuffix(path.Ext(filename))
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "Формат файла не поддерживается")
		return
	}
	importer, err := s.Importers.Pick(data, filename)
	if err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "Формат файла не поддерживается")
		return
	}
	result, err := importer.Load(data, filename)
	switch {
	case errors.Is(err, storage.ErrUnsafePath):
		writeError(w, http.StatusBadRequest, "Архив содержит небезопасные пути и не может быть принят")
		return
	case errors.Is(err, importers.ErrArchiveLimits):
		writeError(w, http.StatusRequestEntityTooLarge, "Архив превышает допустимые пределы")
		return
	case err != nil:
		internalError(w, r, err)
		return
	}

	doc := result.Document
	book := models.Book{
		OwnerID: user.ID, Title: doc.Meta.Title, Author: doc.Meta.Author, Language: doc.Meta.Language,
		SourceFormat: importer.FormatID(), OriginalFilename: filename, OriginalSuffix: suffix,
		OriginalSize: int64(len(data)), Status: models.BookStatusImported,
		ChaptersCount: chapterCount(doc.Chapters), BlocksCount: len(doc.Blocks()),
		TranslatableCount: len(doc.TranslatableBlocks()),
		CDM:               models.NewJSON(doc),
	}
	book.ID = uuid.New()

	// Сначала файлы, затем строка в БД: при сбое на любом шаге убираем
	// файлы, так что книга либо есть целиком, либо её нет.
	cleanup := func() { _ = s.Store.DeleteBook(user.ID, book.ID) }
	if _, err := s.Store.SaveOriginal(user.ID, book.ID, suffix, bytes.NewReader(data)); err != nil {
		cleanup()
		internalError(w, r, err)
		return
	}
	issues := result.Issues
	for _, asset := range result.Assets {
		issue, err := s.saveAsset(user.ID, book.ID, asset)
		if err != nil {
			cleanup()
			internalError(w, r, err)
			return
		}
		if issue != nil {
			issues = append(issues, *issue)
		}
	}
	book.Issues = models.NewJSON(issues)

	if err := s.DB.WithContext(r.Context()).Create(&book).Error; err != nil {
		cleanup()
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, bookResponse(&book))
}

// saveAsset сохраняет ресурс. Повреждённый или слишком большой ресурс —
// замечание к книге, а не отказ всей загрузки.
func (s *server) saveAsset(userID, bookID uuid.UUID, asset importers.Asset) (*importers.Issue, error) {
	raw, err := asset.Open()
	if err != nil {
		return &importers.Issue{Severity: importers.Warning, Code: "corrupt_resource",
			Message: "Не удалось прочитать изображение " + asset.Name}, nil
	}
	_, err = s.Store.SaveAsset(userID, bookID, asset.Name, bytes.NewReader(raw))
	switch {
	case errors.Is(err, storage.ErrFileTooLarge):
		return &importers.Issue{Severity: importers.Warning, Code: "resource_too_large",
			Message: "Изображение слишком большое и не сохранено: " + asset.Name}, nil
	case err != nil:
		return nil, err
	}
	return nil, nil
}

func chapterCount(chapters []cdm.Chapter) int {
	n := len(chapters)
	for _, ch := range chapters {
		n += chapterCount(ch.Children)
	}
	return n
}

func (s *server) listBooks(w http.ResponseWriter, r *http.Request) {
	var books []models.Book
	err := s.DB.WithContext(r.Context()).
		Scopes(store.OwnedScope(UserFromContext(r.Context()).ID)).
		Omit("cdm"). // библиотека не читает документы целиком
		Order("created_at DESC").
		Find(&books).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]bookOut, len(books))
	for i := range books {
		out[i] = bookResponse(&books[i])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getBook(w http.ResponseWriter, r *http.Request) {
	if book, ok := loadOwned[models.Book](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, bookResponse(book))
	}
}

type tocOut struct {
	Chapters []cdm.TOCEntry `json:"chapters"`
}

func tocResponse(doc cdm.Document) tocOut {
	toc := doc.TOC()
	if toc == nil {
		toc = []cdm.TOCEntry{}
	}
	return tocOut{Chapters: toc}
}

func (s *server) getTOC(w http.ResponseWriter, r *http.Request) {
	if book, ok := loadOwned[models.Book](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, tocResponse(book.CDM.Data))
	}
}

type tocOrderIn struct {
	Order []int `json:"order"`
}

// putTOC переставляет главы верхнего уровня (ТЗ §4.3). order — новая
// последовательность прежних позиций: [2, 0, 1] ставит третью главу первой.
//
// Переставляется только срез глав, их Index не меняется, поэтому
// идентификаторы блоков (ch0003/b0017) и ссылки сегментов перевода на них
// остаются прежними.
func (s *server) putTOC(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var in tocOrderIn
	if !decodeJSON(w, r, &in) {
		return
	}
	doc := book.CDM.Data
	if !isPermutation(in.Order, len(doc.Chapters)) {
		writeError(w, http.StatusUnprocessableEntity, "order должен быть перестановкой номеров глав верхнего уровня")
		return
	}
	reordered := make([]cdm.Chapter, len(doc.Chapters))
	for i, from := range in.Order {
		reordered[i] = doc.Chapters[from]
	}
	doc.Chapters = reordered

	err := s.DB.WithContext(r.Context()).Model(book).
		Updates(map[string]any{"cdm": models.NewJSON(doc), "updated_at": time.Now().UTC()}).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tocResponse(doc))
}

func isPermutation(order []int, n int) bool {
	if len(order) != n {
		return false
	}
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	for i, v := range sorted {
		if v != i {
			return false
		}
	}
	return true
}

func (s *server) deleteBook(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	// Строка и файлы удаляются вместе (ТЗ §13): если файлы не удалились,
	// откатывается и удаление строки.
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.Book{}, "id = ?", book.ID).Error; err != nil {
			return err
		}
		return s.Store.DeleteBook(book.OwnerID, book.ID)
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
