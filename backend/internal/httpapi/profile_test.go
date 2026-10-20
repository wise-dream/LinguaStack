package httpapi_test

import (
	"net/http"
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
