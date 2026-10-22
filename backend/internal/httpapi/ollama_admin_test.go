package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
)

// Ollama сервера — общий CPU: при LINGUASTACK_OLLAMA_ADMIN_ONLY ею
// пользуется только администратор, остальные подключают свои сервисы.
func newOllamaAdminOnlyEnv(t *testing.T) *translationEnv {
	t.Helper()
	return newTranslationEnv(t, func(d *httpapi.Deps) { d.Cfg.OllamaAdminOnly = true })
}

func TestOllamaHiddenFromRegularUsers(t *testing.T) {
	e := newOllamaAdminOnlyEnv(t)
	require.NotContains(t, kindIDs(t, e.c), "ollama")
	require.Contains(t, kindIDs(t, e.c), "anthropic")

	r, _ := createConfig(t, e.c, map[string]any{"provider_id": "ollama"})
	require.Equal(t, http.StatusForbidden, r.Status)
	require.Contains(t, string(r.Body), "только администратору")
	require.Equal(t, http.StatusNotFound, e.c.do(http.MethodGet, "/api/providers/ollama/models", nil).Status)
}

func TestOllamaAvailableToAdmin(t *testing.T) {
	e := newOllamaAdminOnlyEnv(t)
	e.promote(t, "u@example.com")
	require.Contains(t, kindIDs(t, e.c), "ollama")
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	require.Equal(t, "completed", e.get(t, p.ID).Status)
}

func TestExistingOllamaConfigOfRegularUserIsBlocked(t *testing.T) {
	// Подключение создано раньше (или пользователя разжаловали): Ollama
	// сервера ему всё равно недоступна — ни проверка, ни новый перевод.
	e := newOllamaAdminOnlyEnv(t)
	e.promote(t, "u@example.com")
	cfgID := e.ollamaConfig(t)
	bookID := e.book(t, "# Ch\n\nOne.")
	require.NoError(t, e.db.Model(&models.User{}).Where("email = ?", "u@example.com").Update("role", models.RoleUser).Error)

	require.Equal(t, http.StatusForbidden, e.c.do(http.MethodGet, "/api/provider-configs/"+cfgID+"/models", nil).Status)
	r := e.create(t, bookID, map[string]any{"provider_config_id": cfgID})
	require.Equal(t, http.StatusForbidden, r.Status, "тело: %s", r.Body)
}
