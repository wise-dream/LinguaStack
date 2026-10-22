package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Поиск и порции общей библиотеки и своей (дополнение §9).

type libraryPage struct {
	Books []libraryBody `json:"books"`
	Total int           `json:"total"`
	Next  string        `json:"next"`
}

func getLibrary(t *testing.T, c *apiClient, query url.Values) libraryPage {
	t.Helper()
	r := c.do(http.MethodGet, "/api/library?"+query.Encode(), nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var page libraryPage
	r.JSON(t, &page)
	return page
}

func titles(page libraryPage) []string {
	out := make([]string, len(page.Books))
	for i, b := range page.Books {
		out[i] = b.Title
	}
	return out
}

func bookWithMeta(t *testing.T, c *apiClient, title, author string) bookBody {
	t.Helper()
	return uploadBook(t, c, "book.md", "---\ntitle: "+title+"\nauthor: "+author+"\n---\n\n# Глава\n\nТекст.")
}

func TestLibrarySearchByTitleAndAuthor(t *testing.T) {
	e := newTestEnv(t)
	owner := e.signedIn(t, "owner@example.com")
	reader := e.signedIn(t, "reader@example.com")
	bookWithMeta(t, owner, "Crafting Interpreters", "Robert Nystrom")
	bookWithMeta(t, owner, "Good Arguments", "Bo Seo")
	bookWithMeta(t, owner, "Ёжик в тумане", "Сергей Козлов")
	bookWithMeta(t, owner, "Скидка 100% навсегда", "Отдел продаж")

	search := func(q string) []string {
		return titles(getLibrary(t, reader, url.Values{"q": {q}}))
	}
	require.Equal(t, []string{"Crafting Interpreters"}, search("INTERP"), "регистр не важен")
	require.Equal(t, []string{"Good Arguments"}, search("seo"), "по автору")
	require.Equal(t, []string{"Crafting Interpreters"}, search("nystrom crafting"), "каждое слово — в названии или авторе")
	require.Equal(t, []string{"Ёжик в тумане"}, search("ежик"), "ё и е не различаются")
	require.Equal(t, []string{"Скидка 100% навсегда"}, search("100%"), "% — обычный символ, а не шаблон")
	require.Empty(t, search("_"), "_ — обычный символ")
	require.Len(t, search("  "), 4, "пустой запрос — все книги")

	page := getLibrary(t, reader, url.Values{"q": {"good"}})
	require.Equal(t, 1, page.Total, "total — по запросу, а не по всей библиотеке")
}

func TestLibraryMineOnly(t *testing.T) {
	e := newTestEnv(t)
	owner := e.signedIn(t, "owner@example.com")
	reader := e.signedIn(t, "reader@example.com")
	bookWithMeta(t, owner, "Чужая", "А")
	mine := bookWithMeta(t, reader, "Своя", "Б")

	page := getLibrary(t, reader, url.Values{"mine": {"1"}})
	require.Equal(t, []string{"Своя"}, titles(page))
	require.Equal(t, mine.ID, page.Books[0].ID)
	require.True(t, page.Books[0].Owned)
	require.Equal(t, 1, page.Total)
	require.Len(t, getLibrary(t, reader, nil).Books, 2, "без mine — общая библиотека")
}

func TestLibraryPagesByCursor(t *testing.T) {
	e := newTestEnv(t)
	c := e.signedIn(t, "owner@example.com")
	for _, title := range []string{"Первая", "Вторая", "Третья", "Четвёртая", "Пятая"} {
		bookWithMeta(t, c, title, "Автор")
	}

	first := getLibrary(t, c, url.Values{"limit": {"2"}})
	require.Equal(t, []string{"Пятая", "Четвёртая"}, titles(first), "новые сверху")
	require.Equal(t, 5, first.Total)
	require.NotEmpty(t, first.Next)

	// Книга, загруженная между порциями, не сдвигает следующую: ни повтора,
	// ни пропуска.
	bookWithMeta(t, c, "Шестая", "Автор")
	second := getLibrary(t, c, url.Values{"limit": {"2"}, "after": {first.Next}})
	require.Equal(t, []string{"Третья", "Вторая"}, titles(second))
	last := getLibrary(t, c, url.Values{"limit": {"2"}, "after": {second.Next}})
	require.Equal(t, []string{"Первая"}, titles(last))
	require.Empty(t, last.Next, "дальше книг нет")

	searched := getLibrary(t, c, url.Values{"limit": {"1"}, "q": {"автор"}})
	require.Equal(t, 6, searched.Total)
	require.Equal(t, []string{"Пятая"}, titles(getLibrary(t, c, url.Values{"limit": {"1"}, "q": {"автор"}, "after": {searched.Next}})),
		"курсор работает и вместе с поиском")
}

func TestLibraryRejectsBadParams(t *testing.T) {
	e := newTestEnv(t)
	c := e.signedIn(t, "owner@example.com")
	for _, q := range []string{"after=garbage", "limit=0", "limit=abc", "limit=101"} {
		require.Equal(t, http.StatusBadRequest, c.do(http.MethodGet, "/api/library?"+q, nil).Status, q)
	}
}
