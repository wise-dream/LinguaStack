package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
)

type searchBody struct {
	Results []struct {
		ChapterIndex int    `json:"chapter_index"`
		NodeID       string `json:"node_id"`
		In           string `json:"in"`
		Snippet      string `json:"snippet"`
	} `json:"results"`
	Truncated bool `json:"truncated"`
}

func search(t *testing.T, c *apiClient, bookID, query string) (int, searchBody) {
	t.Helper()
	r := c.do(http.MethodGet, "/api/books/"+bookID+"/search?"+query, nil)
	var body searchBody
	if r.Status == http.StatusOK {
		r.JSON(t, &body)
	}
	return r.Status, body
}

func TestSearchOriginalCaseInsensitive(t *testing.T) {
	// ТЗ §8.3: поиск по тексту.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# Глава\n\nКот спал.\n\nАлиса пошла в сад.")
	status, body := search(t, e.c, bookID, "q=%D0%90%D0%9B%D0%98%D0%A1%D0%90") // «АЛИСА»
	require.Equal(t, http.StatusOK, status)
	require.Len(t, body.Results, 1)
	got := body.Results[0]
	require.Equal(t, "original", got.In)
	require.NotEmpty(t, got.NodeID)
	require.Contains(t, got.Snippet, "Алиса пошла")
	require.False(t, body.Truncated)
}

func TestSearchTranslationEscapesLike(t *testing.T) {
	// % и _ — символы шаблона LIKE: в запросе они ищутся буквально.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# T\n\nGot 100% right.\n\nGot 1000 right.\n\nKeep a_b.\n\nKeep axb.")
	p := e.project(t, bookID)
	e.runWorker(t)

	translated := func(q string) []string {
		status, body := search(t, e.c, bookID, "q="+q+"&translation_id="+p.ID)
		require.Equal(t, http.StatusOK, status)
		var out []string
		for _, r := range body.Results {
			if r.In == "translation" {
				out = append(out, r.Snippet)
			}
		}
		return out
	}
	require.Equal(t, []string{"RU:Got 100% right."}, translated("0%25"))
	require.Equal(t, []string{"RU:Keep a_b."}, translated("a_b"))
}

func TestSearchQueryLength(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# T\n\nText.")
	for _, q := range []string{"q=a", "q=%20%20", "q=" + strings.Repeat("a", 201), ""} {
		status, _ := search(t, e.c, bookID, q)
		require.Equal(t, http.StatusUnprocessableEntity, status, q)
	}
}

func TestSearchLimit50SetsTruncated(t *testing.T) {
	e := newTranslationEnv(t, nil)
	var b strings.Builder
	b.WriteString("# T\n\n")
	for i := range 60 {
		fmt.Fprintf(&b, "Кот номер %d.\n\n", i)
	}
	bookID := e.book(t, b.String())
	_, body := search(t, e.c, bookID, "q=%D0%BA%D0%BE%D1%82") // «кот»
	require.Len(t, body.Results, 50)
	require.True(t, body.Truncated)
	require.Contains(t, body.Results[0].Snippet, "номер 0.", "порядок — порядок чтения")
}

func TestSearchForeignTranslation404(t *testing.T) {
	e := newTranslationEnv(t, nil)
	first := e.book(t, "# A\n\nOne.")
	second := e.book(t, "# B\n\nTwo.")
	otherBook := e.project(t, second)
	for _, id := range []string{otherBook.ID, "nope", "00000000-0000-0000-0000-000000000001"} {
		status, _ := search(t, e.c, first, "q=One&translation_id="+id)
		require.Equal(t, http.StatusNotFound, status, id)
	}
}

type positionBody struct {
	ChapterIndex  int     `json:"chapter_index"`
	NodeID        string  `json:"node_id"`
	TranslationID *string `json:"translation_id"`
	Mode          string  `json:"mode"`
}

func TestPositionRoundTrip(t *testing.T) {
	// ТЗ §8.3: сохранение позиции чтения — на сервере, чтобы продолжить
	// с другого устройства.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# A\n\nOne.\n\n# B\n\nTwo.")
	p := e.project(t, bookID)
	r := e.c.do(http.MethodGet, "/api/books/"+bookID+"/position", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.JSONEq(t, "null", string(r.Body), "позиции ещё нет")

	put := map[string]any{"chapter_index": 1, "node_id": "ch0001/b0001", "translation_id": p.ID, "mode": "translation"}
	require.Equal(t, http.StatusNoContent, e.c.do(http.MethodPut, "/api/books/"+bookID+"/position", put).Status)
	var got positionBody
	e.c.do(http.MethodGet, "/api/books/"+bookID+"/position", nil).JSON(t, &got)
	require.Equal(t, 1, got.ChapterIndex)
	require.Equal(t, "ch0001/b0001", got.NodeID)
	require.Equal(t, p.ID, *got.TranslationID)
	require.Equal(t, "translation", got.Mode)

	// Повторное сохранение перезаписывает.
	put = map[string]any{"chapter_index": 0, "node_id": "", "translation_id": nil, "mode": "parallel"}
	require.Equal(t, http.StatusNoContent, e.c.do(http.MethodPut, "/api/books/"+bookID+"/position", put).Status)
	e.c.do(http.MethodGet, "/api/books/"+bookID+"/position", nil).JSON(t, &got)
	require.Equal(t, 0, got.ChapterIndex)
	require.Nil(t, got.TranslationID)

	// Позиция своя у каждого пользователя.
	other := e.signedIn(t, "other@example.com")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/books/"+bookID+"/position", nil).Status)
}

func TestPositionUnknownChapter422(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# A\n\nOne.")
	foreign := e.project(t, e.book(t, "# B\n\nTwo."))
	cases := []map[string]any{
		{"chapter_index": 99, "mode": "parallel"},
		{"chapter_index": 0, "mode": "sideways"},
		{"chapter_index": 0, "mode": "parallel", "translation_id": foreign.ID},
		{"chapter_index": 0, "mode": "parallel", "node_id": strings.Repeat("x", 65)},
	}
	for _, body := range cases {
		require.Equal(t, http.StatusUnprocessableEntity,
			e.c.do(http.MethodPut, "/api/books/"+bookID+"/position", body).Status, "%v", body)
	}
}

func TestPositionDeletedWithBook(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# A\n\nOne.")
	require.Equal(t, http.StatusNoContent, e.c.do(http.MethodPut, "/api/books/"+bookID+"/position",
		map[string]any{"chapter_index": 0, "mode": "original"}).Status)
	require.Equal(t, http.StatusNoContent, e.c.do(http.MethodDelete, "/api/books/"+bookID, nil).Status)
	var count int64
	require.NoError(t, e.db.Model(&models.ReadingPosition{}).Where("book_id = ?", bookID).Count(&count).Error)
	require.Zero(t, count)
}

func TestPositionTranslationDeletedBecomesNull(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# A\n\nOne.")
	p := e.project(t, bookID)
	require.Equal(t, http.StatusNoContent, e.c.do(http.MethodPut, "/api/books/"+bookID+"/position",
		map[string]any{"chapter_index": 0, "mode": "parallel", "translation_id": p.ID}).Status)
	require.NoError(t, e.db.Exec("DELETE FROM translation_projects WHERE id = ?", p.ID).Error)
	var got positionBody
	e.c.do(http.MethodGet, "/api/books/"+bookID+"/position", nil).JSON(t, &got)
	require.Nil(t, got.TranslationID, "удалённый перевод не ломает позицию")
	require.Equal(t, "parallel", got.Mode)
}
