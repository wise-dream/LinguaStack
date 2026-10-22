package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Общая библиотека (дополнение §9): все книги и их переводы видят все
// пользователи — читают и скачивают; править, переводить и удалять может
// только тот, кто загрузил.

type libraryBody struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Owned        bool   `json:"owned"`
	Translations []struct {
		ID           string `json:"id"`
		TargetLang   string `json:"target_lang"`
		Status       string `json:"status"`
		SegmentsDone int    `json:"segments_done"`
	} `json:"translations"`
}

// sharedSetup — книга с готовым переводом у владельца и второй
// пользователь, который её читает.
func sharedSetup(t *testing.T) (owner *translationEnv, reader *apiClient, p projectBody) {
	t.Helper()
	owner = newTranslationEnv(t, nil)
	p = owner.project(t, owner.book(t, "# Глава\n\nКошки спали на крыше.\n\nПёс лаял во дворе."))
	owner.runWorker(t)
	return owner, owner.signedIn(t, "reader@example.com"), p
}

func TestSharedLibraryListsEveryonesBooks(t *testing.T) {
	owner, reader, p := sharedSetup(t)
	mine := uploadBook(t, reader, "mine.md", "# Своя\n\nТекст.")

	lib := getLibrary(t, reader, nil)
	byID := map[string]libraryBody{}
	for _, b := range lib.Books {
		byID[b.ID] = b
	}
	require.Contains(t, byID, p.BookID)
	require.False(t, byID[p.BookID].Owned)
	require.Len(t, byID[p.BookID].Translations, 1)
	require.Equal(t, "completed", byID[p.BookID].Translations[0].Status)
	require.True(t, byID[mine.ID].Owned, "своя книга тоже в общей библиотеке")

	// Владелец видит книгу читателя так же.
	require.Len(t, getLibrary(t, owner.c, nil).Books, 2)
}

func TestSharedBookReadableByOthers(t *testing.T) {
	_, reader, p := sharedSetup(t)
	ok := func(method, path string, body any) *response {
		t.Helper()
		r := reader.do(method, path, body)
		require.Equal(t, http.StatusOK, r.Status, "%s %s: %s", method, path, r.Body)
		return r
	}
	var book struct {
		Title string `json:"title"`
		Owned bool   `json:"owned"`
	}
	ok(http.MethodGet, "/api/books/"+p.BookID, nil).JSON(t, &book)
	require.False(t, book.Owned)
	ok(http.MethodGet, "/api/books/"+p.BookID+"/toc", nil)
	ok(http.MethodGet, "/api/books/"+p.BookID+"/chapters/0", nil)
	ok(http.MethodGet, "/api/books/"+p.BookID+"/search?"+q("кошки")+"&translation_id="+p.ID, nil)

	var list []struct {
		ID    string `json:"id"`
		Owned bool   `json:"owned"`
	}
	ok(http.MethodGet, "/api/translations?book_id="+p.BookID, nil).JSON(t, &list)
	require.Len(t, list, 1)
	require.Equal(t, p.ID, list[0].ID)
	require.False(t, list[0].Owned)
	ok(http.MethodGet, "/api/translations/"+p.ID, nil)
	var segs []segmentBody
	ok(http.MethodGet, "/api/translations/"+p.ID+"/segments", nil).JSON(t, &segs)
	require.Len(t, segs, 3)

	// Место в книге — своё у каждого читателя.
	require.Equal(t, http.StatusNoContent, reader.do(http.MethodPut, "/api/books/"+p.BookID+"/position",
		map[string]any{"chapter_index": 0, "node_id": "", "translation_id": p.ID, "mode": "parallel"}).Status)
	ok(http.MethodGet, "/api/books/"+p.BookID+"/position", nil)
}

func TestSharedBookChangesOnlyByOwner(t *testing.T) {
	owner, reader, p := sharedSetup(t)
	seg := owner.segments(t, p.ID)[1]
	cfg := owner.ollamaConfig(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodDelete, "/api/books/" + p.BookID},
		{http.MethodPut, "/api/books/" + p.BookID + "/toc"},
		{http.MethodPost, "/api/books/" + p.BookID + "/translations"},
		{http.MethodGet, "/api/books/" + p.BookID + "/estimate?target_lang=ru"},
		{http.MethodPost, "/api/translations/" + p.ID + "/pause"},
		{http.MethodPost, "/api/translations/" + p.ID + "/resume"},
		{http.MethodPost, "/api/translations/" + p.ID + "/cancel"},
		{http.MethodPost, "/api/translations/" + p.ID + "/retry-problems"},
		{http.MethodPatch, "/api/segments/" + seg.ID},
		{http.MethodPost, "/api/segments/" + seg.ID + "/retry"},
		{http.MethodGet, "/api/segments/" + seg.ID + "/revisions"},
		{http.MethodGet, "/api/provider-configs/" + cfg},
	} {
		body := map[string]any{"translated_text": "чужая правка", "order": []int{0}}
		require.Equal(t, http.StatusNotFound, reader.do(c.method, c.path, body).Status, "%s %s", c.method, c.path)
	}
	require.Equal(t, http.StatusOK, owner.c.do(http.MethodGet, "/api/books/"+p.BookID, nil).Status, "книга цела")
}

func TestSharedBookExportByReader(t *testing.T) {
	// Скачать чужую книгу: задание экспорта — читателя, файл — в его
	// каталоге; владелец удаляет книгу — файлы читателя уходят вместе с ней.
	owner, reader, p := sharedSetup(t)
	r := reader.do(http.MethodPost, "/api/books/"+p.BookID+"/exports",
		map[string]any{"translation_id": p.ID, "format": "markdown", "mode": "translation"})
	require.Equal(t, http.StatusAccepted, r.Status, "тело: %s", r.Body)
	var job exportBody
	r.JSON(t, &job)
	owner.runExports(t)

	var got exportBody
	reader.do(http.MethodGet, "/api/exports/"+job.ID, nil).JSON(t, &got)
	require.Equal(t, "completed", got.Status, got.Error)
	file := reader.do(http.MethodGet, "/api/exports/"+job.ID+"/file", nil)
	require.Equal(t, http.StatusOK, file.Status)
	require.Contains(t, string(file.Body), "RU:Кошки спали на крыше.")

	var ownerJobs, readerJobs []exportBody
	owner.c.do(http.MethodGet, "/api/books/"+p.BookID+"/exports", nil).JSON(t, &ownerJobs)
	reader.do(http.MethodGet, "/api/books/"+p.BookID+"/exports", nil).JSON(t, &readerJobs)
	require.Empty(t, ownerJobs, "чужие задания экспорта владелец не видит")
	require.Len(t, readerJobs, 1)
	require.Equal(t, http.StatusNotFound, owner.c.do(http.MethodGet, "/api/exports/"+job.ID, nil).Status)

	readerDir := filepath.Join(owner.storage, reader.meID(t), p.BookID)
	require.DirExists(t, readerDir)
	require.Equal(t, http.StatusNoContent, owner.c.do(http.MethodDelete, "/api/books/"+p.BookID, nil).Status)
	_, err := os.Stat(readerDir)
	require.True(t, os.IsNotExist(err), "файлы экспорта читателя удалены вместе с книгой")
}
