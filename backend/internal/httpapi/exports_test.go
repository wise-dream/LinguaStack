package httpapi_test

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exports"
	"github.com/vnovels/linguastack/internal/storage"
)

type exportBody struct {
	ID            string `json:"id"`
	BookID        string `json:"book_id"`
	TranslationID string `json:"translation_id"`
	Format        string `json:"format"`
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	FileName      string `json:"file_name"`
	SizeBytes     int64  `json:"size_bytes"`
	Untranslated  int    `json:"untranslated"`
	Error         string `json:"error"`
	CreatedAt     string `json:"created_at"`
}

// translated — книга с завершённым переводом (Fake «prefix» → «RU:…»).
func (e *translationEnv) translated(t *testing.T) projectBody {
	t.Helper()
	p := e.project(t, e.book(t, "# Глава\n\nOne.\n\nTwo."))
	e.runWorker(t)
	return p
}

func (e *translationEnv) createExport(t *testing.T, c *apiClient, bookID string, body map[string]any) *response {
	t.Helper()
	return c.do(http.MethodPost, "/api/books/"+bookID+"/exports", body)
}

func (e *translationEnv) export(t *testing.T, p projectBody, format, mode string) exportBody {
	t.Helper()
	r := e.createExport(t, e.c, p.BookID, map[string]any{"translation_id": p.ID, "format": format, "mode": mode})
	require.Equal(t, http.StatusAccepted, r.Status, "тело: %s", r.Body)
	var out exportBody
	r.JSON(t, &out)
	return out
}

func (e *translationEnv) runExports(t *testing.T) {
	t.Helper()
	store, err := storage.New(e.storage, e.cfg.MaxUploadBytes)
	require.NoError(t, err)
	runner := exports.NewRunner(e.db, store, e.cfg)
	for range 20 {
		worked, err := runner.RunOnce(t.Context())
		require.NoError(t, err)
		if !worked {
			return
		}
	}
}

func (e *translationEnv) getExport(t *testing.T, id string) exportBody {
	t.Helper()
	var out exportBody
	r := e.c.do(http.MethodGet, "/api/exports/"+id, nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	r.JSON(t, &out)
	return out
}

func TestCreateExportQueued(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	job := env.export(t, p, "epub", "bilingual")
	require.Equal(t, "pending", job.Status)
	require.Equal(t, p.BookID, job.BookID)
	require.Equal(t, p.ID, job.TranslationID)
	require.Equal(t, "epub", job.Format)
	require.Equal(t, "bilingual", job.Mode)
	require.Empty(t, job.FileName, "имя известно, когда файл готов")
}

func TestCreateExportValidation(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	other := env.project(t, env.book(t, "# Другая\n\nText."))
	for name, body := range map[string]map[string]any{
		"формат":       {"translation_id": p.ID, "format": "pdf", "mode": "translation"},
		"режим":        {"translation_id": p.ID, "format": "epub", "mode": "original"},
		"без перевода": {"format": "epub", "mode": "translation"},
		"не UUID":      {"translation_id": "abc", "format": "epub", "mode": "translation"},
		"другая книга": {"translation_id": other.ID, "format": "epub", "mode": "translation"},
	} {
		r := env.createExport(t, env.c, p.BookID, body)
		require.Equal(t, http.StatusUnprocessableEntity, r.Status, "%s: %s", name, r.Body)
	}

	intruder := env.signedIn(t, "intruder@example.com")
	own := uploadBook(t, intruder, "own.md", "# Своя\n\nText.")
	r := env.createExport(t, intruder, own.ID, map[string]any{"translation_id": p.ID, "format": "epub", "mode": "translation"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, "чужой перевод — как несуществующий, как и чужая конфигурация в createTranslation")
	require.Contains(t, string(r.Body), "Перевод не найден")
}

func TestCreateExportLimit(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	for range env.cfg.MaxActiveExportsPerUser {
		env.export(t, p, "txt", "translation")
	}
	r := env.createExport(t, env.c, p.BookID, map[string]any{"translation_id": p.ID, "format": "txt", "mode": "translation"})
	require.Equal(t, http.StatusTooManyRequests, r.Status, "тело: %s", r.Body)
	env.runExports(t)
	env.export(t, p, "txt", "translation")
}

func TestDownloadExport(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	job := env.export(t, p, "markdown", "translation")
	env.runExports(t)

	done := env.getExport(t, job.ID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, "Глава (ru).md", done.FileName)
	require.Positive(t, done.SizeBytes)
	require.Zero(t, done.Untranslated)

	r := env.c.do(http.MethodGet, "/api/exports/"+job.ID+"/file", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, "text/markdown; charset=utf-8", r.Header.Get("Content-Type"))
	require.Equal(t, `attachment; filename="_____ (ru).md"; filename*=UTF-8''%D0%93%D0%BB%D0%B0%D0%B2%D0%B0%20%28ru%29.md`,
		r.Header.Get("Content-Disposition"))
	require.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
	require.Contains(t, r.Header.Get("Content-Security-Policy"), "sandbox")
	require.Equal(t, "private, no-store", r.Header.Get("Cache-Control"))
	require.Contains(t, string(r.Body), "RU:One.")
	require.Equal(t, done.SizeBytes, int64(len(r.Body)))
}

func TestDownloadNotReady(t *testing.T) {
	env := newTranslationEnv(t, nil)
	job := env.export(t, env.translated(t), "txt", "translation")
	r := env.c.do(http.MethodGet, "/api/exports/"+job.ID+"/file", nil)
	require.Equal(t, http.StatusConflict, r.Status)
}

func TestDownloadFilenameSanitized(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	require.NoError(t, env.db.Model(&models.Book{}).Where("id = ?", p.BookID).
		Update("title", "Книга \"1\"\r\nX/Y:*?<>|\\").Error)
	job := env.export(t, p, "txt", "translation")
	env.runExports(t)
	require.Equal(t, "Книга 1 X Y (ru).txt", env.getExport(t, job.ID).FileName)

	header := env.c.do(http.MethodGet, "/api/exports/"+job.ID+"/file", nil).Header.Get("Content-Disposition")
	require.NotContains(t, header, "\r")
	require.NotContains(t, header, "\n")
	_, encoded, ok := strings.Cut(header, "filename*=UTF-8''")
	require.True(t, ok, header)
	decoded, err := url.PathUnescape(encoded)
	require.NoError(t, err)
	require.Equal(t, "Книга 1 X Y (ru).txt", decoded)
}

func TestDeleteExport(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	job := env.export(t, p, "html", "translation")
	env.runExports(t)
	path := filepath.Join(env.storage, "*", p.BookID, "exports", job.ID+".html")
	matches, err := filepath.Glob(path)
	require.NoError(t, err)
	require.Len(t, matches, 1)

	require.Equal(t, http.StatusNoContent, env.c.do(http.MethodDelete, "/api/exports/"+job.ID, nil).Status)
	require.Equal(t, http.StatusNotFound, env.c.do(http.MethodGet, "/api/exports/"+job.ID, nil).Status)
	_, err = os.Stat(matches[0])
	require.True(t, os.IsNotExist(err), "файл удалён")

	running := env.export(t, p, "txt", "translation")
	require.NoError(t, env.db.Model(&models.ExportJob{}).Where("id = ?", running.ID).Update("status", "running").Error)
	require.Equal(t, http.StatusConflict, env.c.do(http.MethodDelete, "/api/exports/"+running.ID, nil).Status)
}

func TestBookDeleteRemovesExports(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	job := env.export(t, p, "epub", "translation")
	env.runExports(t)
	require.Equal(t, http.StatusNoContent, env.c.do(http.MethodDelete, "/api/books/"+p.BookID, nil).Status)
	require.Equal(t, http.StatusNotFound, env.c.do(http.MethodGet, "/api/exports/"+job.ID, nil).Status)
	for _, f := range filesUnder(t, env.storage) {
		require.NotContains(t, f, p.BookID)
	}
}

func TestListBookExports(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	first := env.export(t, p, "epub", "translation")
	second := env.export(t, p, "markdown", "bilingual")
	var list []exportBody
	r := env.c.do(http.MethodGet, "/api/books/"+p.BookID+"/exports", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &list)
	require.Len(t, list, 2)
	require.Equal(t, []string{second.ID, first.ID}, []string{list[0].ID, list[1].ID}, "новые первыми")
}
