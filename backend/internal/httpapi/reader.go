package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/store"
)

const (
	searchMinRunes   = 2
	searchMaxRunes   = 200
	searchMaxResults = 50
	snippetRadius    = 60
	maxNodeIDLength  = 64
)

var readModes = []string{models.ReadParallel, models.ReadOriginal, models.ReadTranslation}

type searchResult struct {
	ChapterIndex int    `json:"chapter_index"`
	NodeID       string `json:"node_id"`
	In           string `json:"in"`
	Snippet      string `json:"snippet"`

	order, ordinal int // порядок чтения: глава в оглавлении, затем блок
}

// searchBook — поиск по тексту книги и одного её перевода (ТЗ §8.3).
// Порядок результатов — порядок чтения: после ручной перестановки глав
// index главы с ним не совпадает.
func (s *server) searchBook(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
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

	order := map[int]int{}
	var results []searchResult
	needle := foldRunes(q)
	n := 0
	walk(book.CDM.Data.Chapters, func(ch *cdm.Chapter) {
		order[ch.Index] = n
		n++
		for i, b := range ch.Blocks {
			if snippet, hit := matchSnippet(b.Text, needle); hit && len(results) <= searchMaxResults {
				results = append(results, searchResult{ChapterIndex: ch.Index, NodeID: b.NodeID, In: "original",
					Snippet: snippet, order: order[ch.Index], ordinal: i})
			}
		}
	})
	if project != nil {
		var segs []models.TranslationSegment
		err := s.DB.WithContext(r.Context()).
			Where("project_id = ? AND translated_text ILIKE ? ESCAPE '\\'", project.ID, "%"+escapeLike(q)+"%").
			Order("chapter_index, ordinal").Limit(searchMaxResults + 1).Find(&segs).Error
		if err != nil {
			internalError(w, r, err)
			return
		}
		for _, seg := range segs {
			snippet, _ := matchSnippet(*seg.TranslatedText, needle)
			results = append(results, searchResult{ChapterIndex: seg.ChapterIndex, NodeID: seg.NodeID, In: "translation",
				Snippet: snippet, order: order[seg.ChapterIndex], ordinal: seg.Ordinal})
		}
	}
	slices.SortStableFunc(results, func(a, b searchResult) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return a.ordinal - b.ordinal
	})
	truncated := len(results) > searchMaxResults
	if truncated {
		results = results[:searchMaxResults]
	}
	if results == nil {
		results = []searchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "truncated": truncated})
}

// bookProject — перевод пользователя, принадлежащий этой книге.
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

func walk(chapters []cdm.Chapter, visit func(*cdm.Chapter)) {
	for i := range chapters {
		visit(&chapters[i])
		walk(chapters[i].Children, visit)
	}
}

// foldRunes — строчные буквы по одной на символ: так позиция совпадения в
// свёрнутом тексте совпадает с позицией в исходном (strings.ToLower может
// менять длину).
func foldRunes(s string) []rune {
	out := []rune(s)
	for i, r := range out {
		out[i] = unicode.ToLower(r)
	}
	return out
}

// matchSnippet ищет needle без учёта регистра и возвращает фрагмент вокруг
// первого совпадения.
func matchSnippet(text string, needle []rune) (string, bool) {
	runes := []rune(text)
	folded := foldRunes(text)
	at := -1
	for i := 0; i+len(needle) <= len(folded); i++ {
		if slices.Equal(folded[i:i+len(needle)], needle) {
			at = i
			break
		}
	}
	if at < 0 {
		return "", false
	}
	start, end := max(0, at-snippetRadius), min(len(runes), at+len(needle)+snippetRadius)
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet, true
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }

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
	book, ok := loadOwned[models.Book](s, w, r, "id")
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
	book, ok := loadOwned[models.Book](s, w, r, "id")
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
