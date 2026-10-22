package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
)

type searchBody struct {
	Results []struct {
		ChapterIndex int      `json:"chapter_index"`
		NodeID       string   `json:"node_id"`
		In           string   `json:"in"`
		Snippet      string   `json:"snippet"`
		Hits         []string `json:"hits"`
	} `json:"results"`
	Total      int    `json:"total"`
	Truncated  bool   `json:"truncated"`
	Suggestion string `json:"suggestion"`
}

// snippets — отрывки результатов с одной стороны (original или translation).
func (b searchBody) snippets(in string) []string {
	var out []string
	for _, r := range b.Results {
		if r.In == in {
			out = append(out, r.Snippet)
		}
	}
	return out
}

func q(s string) string { return "q=" + url.QueryEscape(s) }

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

func TestSearchLimit100CountsAll(t *testing.T) {
	e := newTranslationEnv(t, nil)
	var b strings.Builder
	b.WriteString("# T\n\n")
	for i := range 120 {
		fmt.Fprintf(&b, "Кот номер %d.\n\n", i)
	}
	bookID := e.book(t, b.String())
	_, body := search(t, e.c, bookID, q("кот"))
	require.Len(t, body.Results, 100)
	require.Equal(t, 120, body.Total, "общее число — все совпадения, не только показанные")
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

	// Позиция своя у каждого читателя общей книги.
	other := e.signedIn(t, "other@example.com")
	r = other.do(http.MethodGet, "/api/books/"+bookID+"/position", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.JSONEq(t, "null", string(r.Body), "у другого читателя позиции ещё нет")
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

const cats = "# Глава\n\nКошки спали на крыше.\n\nПёс лаял во дворе."

func TestSearchWordForms(t *testing.T) {
	// Формы слова: язык оригинала без метаданных угадан по алфавиту.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, cats)
	_, body := search(t, e.c, bookID, q("кошка"))
	require.Equal(t, []string{"Кошки спали на крыше."}, body.snippets("original"))
	require.Equal(t, 1, body.Total)
	require.Equal(t, []string{"Кошки"}, body.Results[0].Hits, "найденное слово — для подсветки")
}

func TestSearchWordsAnyOrderAndPhrase(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, cats)
	found := func(query string) []string {
		status, body := search(t, e.c, bookID, q(query))
		require.Equal(t, http.StatusOK, status, query)
		return body.snippets("original")
	}
	require.Equal(t, []string{"Кошки спали на крыше."}, found("крыша кошки"), "слова в любом порядке")
	require.Empty(t, found("кошки двор"), "все слова — в одном блоке")
	require.Equal(t, []string{"Кошки спали на крыше."}, found(`"спали на крыше"`), "фраза в кавычках")
	require.Empty(t, found(`"крыше спали"`), "фраза — в своём порядке")
	require.Equal(t, []string{"Кошки спали на крыше."}, found("«спали на крыше»"))
	require.Equal(t, []string{"Пёс лаял во дворе."}, found("лая"), "последнее слово — начало слова")
}

func TestSearchTranslationUsesTargetLanguage(t *testing.T) {
	// Перевод ищется по формам языка перевода, оригинал — по своему
	// языку (здесь украинский: без словаря форм, только точные слова).
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, cats)
	r := e.create(t, bookID, map[string]any{"provider_config_id": e.ollamaConfig(t), "source_lang": "uk"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	var p projectBody
	r.JSON(t, &p)
	e.runWorker(t)

	_, body := search(t, e.c, bookID, q("кошку")+"&translation_id="+p.ID)
	require.Equal(t, []string{"RU:Кошки спали на крыше."}, body.snippets("translation"))
	require.Empty(t, body.snippets("original"))
}

func TestSearchSuggestsOnTypo(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, cats)
	_, body := search(t, e.c, bookID, q("кошкм спали"))
	require.Zero(t, body.Total)
	require.Equal(t, "кошки спали", body.Suggestion)

	_, body = search(t, e.c, bookID, q("зебра"))
	require.Empty(t, body.Suggestion, "непохожему слову подсказки нет")
}

func TestSearchFallsBackToSubstring(t *testing.T) {
	// Кусок слова или кода, которого нет среди слов, ищется подстрокой.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# T\n\nCall fmt.Println(x) twice.\n\nNothing here.")
	_, body := search(t, e.c, bookID, q("rintln("))
	require.Equal(t, []string{"Call fmt.Println(x) twice."}, body.snippets("original"))
	require.Equal(t, []string{"rintln("}, body.Results[0].Hits)
}

func TestSearchCJKBySubstring(t *testing.T) {
	// В китайском и японском слова не разделены пробелами: только подстрока.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# T\n\n我喜欢猫。\n\n狗在跑。")
	_, body := search(t, e.c, bookID, q("喜欢"))
	require.Equal(t, []string{"我喜欢猫。"}, body.snippets("original"))
}

func TestSegmentsFilteredByText(t *testing.T) {
	// Найти фрагмент перевода по тексту оригинала или перевода — по тем же
	// правилам, что поиск в читалке; вместе с фильтрами ошибок.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# Глава\n\nКошки спали на крыше.\n\nПёс лаял во дворе.\n\nCall fmt.Println(x) twice.")
	r := e.create(t, bookID, map[string]any{"provider_config_id": e.ollamaConfig(t), "source_lang": "uk"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	var p projectBody
	r.JSON(t, &p)
	e.runWorker(t)

	found := func(query string) []string {
		t.Helper()
		var segs []segmentBody
		resp := e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?"+query, nil)
		require.Equal(t, http.StatusOK, resp.Status, "%s: %s", query, resp.Body)
		resp.JSON(t, &segs)
		out := make([]string, len(segs))
		for i, s := range segs {
			out[i] = s.SourceText
		}
		return out
	}
	require.Equal(t, []string{"Кошки спали на крыше."}, found(q("кошку")), "формы слов перевода")
	require.Equal(t, []string{"Пёс лаял во дворе."}, found(q("лаял")))
	require.Equal(t, []string{"Call fmt.Println(x) twice."}, found(q("rintln(")), "кусок кода — подстрокой")
	require.Empty(t, found(q("зебра")))

	segs := e.segments(t, p.ID)
	require.NoError(t, e.db.Model(&models.TranslationSegment{}).Where("id = ?", segs[2].ID).Update("severity", "warning").Error)
	require.Equal(t, []string{"Пёс лаял во дворе."}, found(q("лаял")+"&severity=warning"))
	require.Empty(t, found(q("кошку")+"&severity=warning"))

	require.Equal(t, http.StatusUnprocessableEntity, e.c.do(http.MethodGet, "/api/translations/"+p.ID+"/segments?q=a", nil).Status)
}
