package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
)

func TestStorageQuotaRefusesUpload(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.StorageQuotaBytes = 100 })
	c := env.signedIn(t, "quota@example.com")
	uploadBook(t, c, "first.txt", strings.Repeat("а", 30)) // 60 байт UTF-8

	r := c.upload("second.txt", []byte(strings.Repeat("б", 25))) // ещё 50 — сверх 100
	require.Equal(t, http.StatusRequestEntityTooLarge, r.Status)
	require.Contains(t, string(r.Body), "Превышена квота хранилища")

	var books int64
	require.NoError(t, env.db.Model(&models.Book{}).Count(&books).Error)
	require.EqualValues(t, 1, books)
	owners, err := os.ReadDir(env.storage)
	require.NoError(t, err)
	var stored int
	for _, d := range owners {
		inner, _ := os.ReadDir(filepath.Join(env.storage, d.Name()))
		stored += len(inner)
	}
	require.Equal(t, 1, stored, "файлов отклонённой книги не осталось")

	// Квота пользователя перекрывает настройку: 0 — без предела.
	require.NoError(t, env.db.Model(&models.User{}).Where("email = ?", "quota@example.com").
		Update("storage_quota_bytes", 0).Error)
	uploadBook(t, c, "second.txt", strings.Repeat("б", 25))
}

func TestUserQuotaOverridesDefault(t *testing.T) {
	te := newTranslationEnv(t, nil)
	require.NoError(t, te.db.Model(&models.User{}).Where("email = ?", "u@example.com").
		Update("max_active_projects", 1).Error)
	book := te.book(t, "# Ch\n\nOne.")
	te.project(t, book)
	r := te.create(t, book, map[string]any{"target_lang": "de", "provider_config_id": te.ollamaConfig(t)})
	require.Equal(t, http.StatusTooManyRequests, r.Status, "тело: %s", r.Body)
	require.Contains(t, string(r.Body), "Не больше 1 активных")
}
