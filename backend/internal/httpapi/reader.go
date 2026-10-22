package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/booksearch"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/store"
)

const (
	searchMinRunes   = 2
	searchMaxRunes   = 200
	searchMaxResults = 100
	maxNodeIDLength  = 64
)

var readModes = []string{models.ReadParallel, models.ReadOriginal, models.ReadTranslation}

// searchBook — поиск по тексту книги и одного её перевода (ТЗ §8.3,
// дополнение §19). Порядок результатов — порядок чтения.
func (s *server) searchBook(w http.ResponseWriter, r *http.Request) {
	book, ok := loadShared[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < searchMinRunes || n > searchMaxRunes {
		writeError(w, http.StatusUnprocessableEntity, "Запрос — от 2 до 200 символов")
		return
	}
	var project *models.TranslationProject
	if raw := r.URL.Query().Get("translation_id"); raw != "" {
		p, found := s.bookProject(r, book, raw)
		if !found {
			writeError(w, http.StatusNotFound, "Перевод не найден")
			return
		}
		project = p
	}
	res, err := booksearch.Search(r.Context(), s.DB, booksearch.Params{Book: book, Project: project, Query: q, Limit: searchMaxResults})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// bookProject — перевод этой книги (их делает только владелец книги).
func (s *server) bookProject(r *http.Request, book *models.Book, raw string) (*models.TranslationProject, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, false
	}
	p, err := store.Owned[models.TranslationProject](r.Context(), s.DB, id, book.OwnerID)
	if err != nil || p.BookID != book.ID {
		return nil, false
	}
	return p, true
}

type positionIn struct {
	ChapterIndex  int     `json:"chapter_index"`
	NodeID        string  `json:"node_id"`
	TranslationID *string `json:"translation_id"`
	Mode          string  `json:"mode"`
}

type positionOut struct {
	ChapterIndex  int        `json:"chapter_index"`
	NodeID        string     `json:"node_id"`
	TranslationID *uuid.UUID `json:"translation_id"`
	Mode          string     `json:"mode"`
}

// getPosition — позиция чтения или null, если книгу ещё не открывали.
func (s *server) getPosition(w http.ResponseWriter, r *http.Request) {
	book, ok := loadShared[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var pos models.ReadingPosition
	err := s.DB.WithContext(r.Context()).
		First(&pos, "user_id = ? AND book_id = ?", UserFromContext(r.Context()).ID, book.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, positionOut{ChapterIndex: pos.ChapterIndex, NodeID: pos.NodeID,
		TranslationID: pos.TranslationID, Mode: pos.Mode})
}

func (s *server) putPosition(w http.ResponseWriter, r *http.Request) {
	book, ok := loadShared[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var in positionIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, found := findChapter(book.CDM.Data.Chapters, in.ChapterIndex); !found {
		writeError(w, http.StatusUnprocessableEntity, "Такой главы в книге нет")
		return
	}
	if !slices.Contains(readModes, in.Mode) {
		writeError(w, http.StatusUnprocessableEntity, "mode: parallel, original или translation")
		return
	}
	if len(in.NodeID) > maxNodeIDLength {
		writeError(w, http.StatusUnprocessableEntity, "Слишком длинный node_id")
		return
	}
	pos := models.ReadingPosition{UserID: UserFromContext(r.Context()).ID, BookID: book.ID,
		ChapterIndex: in.ChapterIndex, NodeID: in.NodeID, Mode: in.Mode}
	if in.TranslationID != nil {
		// Чужой и несуществующий перевод неразличимы: одна и та же 422.
		p, found := s.bookProject(r, book, *in.TranslationID)
		if !found {
			writeError(w, http.StatusUnprocessableEntity, "Перевод не найден")
			return
		}
		pos.TranslationID = &p.ID
	}
	err := s.DB.WithContext(r.Context()).Exec(`
INSERT INTO reading_positions (user_id, book_id, translation_id, chapter_index, node_id, mode, updated_at)
VALUES (?, ?, ?, ?, ?, ?, now())
ON CONFLICT (user_id, book_id) DO UPDATE
   SET translation_id = EXCLUDED.translation_id, chapter_index = EXCLUDED.chapter_index,
       node_id = EXCLUDED.node_id, mode = EXCLUDED.mode, updated_at = now()`,
		pos.UserID, pos.BookID, pos.TranslationID, pos.ChapterIndex, pos.NodeID, pos.Mode).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
