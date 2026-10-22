package httpapi_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/httpapi"
)

const newPassword = "new-correct-horse-battery"

func TestChangePassword(t *testing.T) {
	env := newTestEnv(t)
	tab := env.signedIn(t, "pw@example.com")
	other := env.client(t)
	require.Equal(t, http.StatusOK, other.login("pw@example.com", testPassword).Status)

	change := func(current, next string) int {
		return tab.do(http.MethodPost, "/api/me/password",
			map[string]string{"current_password": current, "new_password": next}).Status
	}
	require.Equal(t, http.StatusForbidden, change("wrong-password-123", newPassword), "403, не 401: сессия жива")
	require.Equal(t, http.StatusOK, tab.do(http.MethodGet, "/api/me", nil).Status)
	require.Equal(t, http.StatusUnprocessableEntity, change(testPassword, "short"))

	require.Equal(t, http.StatusNoContent, change(testPassword, newPassword))
	require.Equal(t, http.StatusOK, tab.do(http.MethodGet, "/api/me", nil).Status, "текущая сессия жива")
	require.Equal(t, http.StatusUnauthorized, other.do(http.MethodGet, "/api/me", nil).Status, "остальные погашены")
	require.Equal(t, http.StatusUnauthorized, env.client(t).login("pw@example.com", testPassword).Status)
	require.Equal(t, http.StatusOK, env.client(t).login("pw@example.com", newPassword).Status)
}

func TestChangePasswordGuessingLimited(t *testing.T) {
	// Украденная сессия не даёт подбирать пароль: неудачи идут в тот же
	// журнал, что и вход.
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.LoginMaxAttempts = 2 })
	c := env.signedIn(t, "guess@example.com")
	body := func(current string) map[string]string {
		return map[string]string{"current_password": current, "new_password": newPassword}
	}
	for range 2 {
		require.Equal(t, http.StatusForbidden, c.do(http.MethodPost, "/api/me/password", body("guess-guess-guess")).Status)
	}
	require.Equal(t, http.StatusTooManyRequests, c.do(http.MethodPost, "/api/me/password", body(testPassword)).Status)
	require.Equal(t, http.StatusTooManyRequests, c.do(http.MethodPatch, "/api/me",
		map[string]string{"email": "x@example.com", "current_password": testPassword}).Status)
}

func TestChangeEmail(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "old@example.com")
	env.signedIn(t, "taken@example.com")
	patch := func(email, password string) *response {
		return c.do(http.MethodPatch, "/api/me", map[string]string{"email": email, "current_password": password})
	}
	require.Equal(t, http.StatusForbidden, patch("new@example.com", "wrong-password-123").Status)
	require.Equal(t, http.StatusUnprocessableEntity, patch("not-an-email", testPassword).Status)
	require.Equal(t, http.StatusConflict, patch("Taken@Example.com", testPassword).Status)

	r := patch(" New@Example.com ", testPassword)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var me struct {
		Email string `json:"email"`
	}
	r.JSON(t, &me)
	require.Equal(t, "new@example.com", me.Email)
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/me", nil).Status, "сессия жива")
	require.Equal(t, http.StatusOK, env.client(t).login("new@example.com", testPassword).Status)
	require.Equal(t, http.StatusUnauthorized, env.client(t).login("old@example.com", testPassword).Status)
}

func TestDeleteAccountRemovesAllData(t *testing.T) {
	// GDPR: человек удаляет свой аккаунт сам — с книгами, переводами,
	// экспортами, подключениями и файлами; чужие данные не задеты.
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	e.runWorker(t)
	e.export(t, p, "markdown", "translation")
	e.runExports(t)
	userID := e.c.meID(t)
	userDir := filepath.Join(e.storage, userID)
	require.DirExists(t, userDir)
	other := e.signedIn(t, "other@example.com")
	uploadBook(t, other, "other.md", "# Чужая\n\nКнига.")

	r := e.c.do(http.MethodDelete, "/api/me", map[string]string{"current_password": "not my password"})
	require.Equal(t, http.StatusForbidden, r.Status, "без пароля аккаунт не удаляется")
	require.DirExists(t, userDir)

	r = e.c.do(http.MethodDelete, "/api/me", map[string]string{"current_password": testPassword})
	require.Equal(t, http.StatusNoContent, r.Status, "тело: %s", r.Body)
	require.Contains(t, strings.ToLower(r.Header.Get("Set-Cookie")), "max-age=0", "cookie сессии стёрта")
	require.Equal(t, http.StatusUnauthorized, e.c.do(http.MethodGet, "/api/me", nil).Status)
	require.Equal(t, http.StatusUnauthorized, e.client(t).login("u@example.com", testPassword).Status)
	require.NoDirExists(t, userDir)
	for _, table := range []string{"users", "books", "translation_projects", "provider_configs", "export_jobs", "user_sessions"} {
		var n int64
		column := "owner_id"
		switch table {
		case "users":
			column = "id"
		case "user_sessions":
			column = "user_id"
		}
		require.NoError(t, e.db.Table(table).Where(column+" = ?", userID).Count(&n).Error)
		require.Zero(t, n, table)
	}

	var books []map[string]any
	other.do(http.MethodGet, "/api/books", nil).JSON(t, &books)
	require.Len(t, books, 1, "чужая библиотека цела")
}

func TestLastAdminCannotDeleteAccount(t *testing.T) {
	// Без администратора некому управлять пользователями и квотами.
	e := newTestEnv(t)
	admin := e.signedIn(t, "admin@example.com")
	e.promote(t, "admin@example.com")
	r := admin.do(http.MethodDelete, "/api/me", map[string]string{"current_password": testPassword})
	require.Equal(t, http.StatusConflict, r.Status)
	require.Contains(t, string(r.Body), "единственный администратор")

	e.signedIn(t, "second@example.com")
	e.promote(t, "second@example.com")
	require.Equal(t, http.StatusNoContent, admin.do(http.MethodDelete, "/api/me", map[string]string{"current_password": testPassword}).Status)
}
