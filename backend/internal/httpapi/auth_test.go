package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/security"
)

func TestRegisterCreatesAccount(t *testing.T) {
	c := newTestEnv(t).client(t)
	r := c.register("u@example.com")
	require.Equal(t, http.StatusCreated, r.Status)

	var body map[string]any
	r.JSON(t, &body)
	require.Equal(t, "u@example.com", body["email"])
	require.Equal(t, "user", body["role"])
	require.NotEmpty(t, body["id"])
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	c := newTestEnv(t).client(t)
	require.Equal(t, http.StatusCreated, c.register("dup@example.com").Status)
	require.Equal(t, http.StatusConflict, c.register("dup@example.com").Status)
}

func TestEmailIsCaseInsensitive(t *testing.T) {
	// Иначе «A@x.com» и «a@x.com» — два аккаунта одного человека.
	c := newTestEnv(t).client(t)
	require.Equal(t, http.StatusCreated, c.register("Mixed@Example.com").Status)
	require.Equal(t, http.StatusConflict, c.register("mixed@example.com").Status)
	require.Equal(t, http.StatusOK, c.login("MIXED@example.COM", testPassword).Status)
}

func TestRegisterValidatesInput(t *testing.T) {
	c := newTestEnv(t).client(t)
	cases := []struct {
		name string
		body any
		want int
	}{
		{"короткий пароль", map[string]string{"email": "s@example.com", "password": "short"}, http.StatusUnprocessableEntity},
		{"невалидный email", map[string]string{"email": "not-an-email", "password": testPassword}, http.StatusUnprocessableEntity},
		{"email с именем", map[string]string{"email": "Bob <b@example.com>", "password": testPassword}, http.StatusUnprocessableEntity},
		{"неизвестное поле", map[string]string{"email": "x@example.com", "password": testPassword, "role": "admin"}, http.StatusUnprocessableEntity},
		{"не JSON", []byte("{oops"), http.StatusUnprocessableEntity},
		{"слишком большое тело", map[string]string{"email": "big@example.com", "password": strings.Repeat("x", 200_000)}, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, c.do(http.MethodPost, "/api/auth/register", tc.body).Status)
		})
	}
}

func TestRegisterCannotSelfAssignAdmin(t *testing.T) {
	// Неизвестные поля отвергаются целиком — "role" нельзя протащить в запросе.
	c := newTestEnv(t).client(t)
	r := c.do(http.MethodPost, "/api/auth/register",
		map[string]string{"email": "evil@example.com", "password": testPassword, "role": "admin"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

func TestLoginSetsHttpOnlySameSiteCookie(t *testing.T) {
	c := newTestEnv(t).client(t)
	c.register("c@example.com")
	r := c.login("c@example.com", testPassword)
	require.Equal(t, http.StatusOK, r.Status)

	// Атрибуты cookie регистронезависимы (RFC 6265).
	setCookie := strings.ToLower(r.Header.Get("Set-Cookie"))
	require.Contains(t, setCookie, "httponly", "токен не должен быть доступен JS")
	require.Contains(t, setCookie, "samesite=lax", "защита от CSRF со сторонних сайтов")
}

func TestMeRequiresAuthentication(t *testing.T) {
	require.Equal(t, http.StatusUnauthorized, newTestEnv(t).client(t).do(http.MethodGet, "/api/me", nil).Status)
}

func TestMeReturnsCurrentUser(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "m@example.com")
	r := c.do(http.MethodGet, "/api/me", nil)
	require.Equal(t, http.StatusOK, r.Status)
	var body map[string]any
	r.JSON(t, &body)
	require.Equal(t, "m@example.com", body["email"])
}

func TestPasswordNeverEchoed(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	reg := c.register("p@example.com")
	c.login("p@example.com", testPassword)
	me := c.do(http.MethodGet, "/api/me", nil)
	for _, r := range []*response{reg, me} {
		require.NotContains(t, string(r.Body), testPassword)
		require.NotContains(t, string(r.Body), "password")
		require.NotContains(t, string(r.Body), "$argon2id$")
	}
}

func TestLoginFailureDoesNotRevealAccountExistence(t *testing.T) {
	c := newTestEnv(t).client(t)
	missing := c.login("nobody@example.com", testPassword)
	c.register("real@example.com")
	wrong := c.login("real@example.com", "wrong-password-x")

	require.Equal(t, http.StatusUnauthorized, missing.Status)
	require.Equal(t, missing.Status, wrong.Status)
	require.JSONEq(t, string(missing.Body), string(wrong.Body),
		"разные ответы позволяют перечислять зарегистрированные адреса")
}

func TestLoginThrottledAfterLimit(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.register("t@example.com")
	for range env.cfg.LoginMaxAttempts {
		require.Equal(t, http.StatusUnauthorized, c.login("t@example.com", "bad-password-x").Status)
	}
	// Даже с верным паролем: иначе лимит обходится угадыванием (ТЗ §13).
	require.Equal(t, http.StatusTooManyRequests, c.login("t@example.com", testPassword).Status)
}

func TestThrottleScopedPerEmail(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.register("a1@example.com")
	c.register("a2@example.com")
	for range env.cfg.LoginMaxAttempts {
		c.login("a1@example.com", "bad-password-x")
	}
	require.Equal(t, http.StatusOK, c.login("a2@example.com", testPassword).Status,
		"перебор одного адреса не должен блокировать другой")
}

func TestSuccessfulLoginClearsFailures(t *testing.T) {
	env := newTestEnv(t)
	c := env.client(t)
	c.register("s@example.com")
	for range env.cfg.LoginMaxAttempts - 1 {
		c.login("s@example.com", "bad-password-x")
	}
	require.Equal(t, http.StatusOK, c.login("s@example.com", testPassword).Status)
	for range env.cfg.LoginMaxAttempts - 1 {
		c.login("s@example.com", "bad-password-x")
	}
	require.Equal(t, http.StatusOK, c.login("s@example.com", testPassword).Status,
		"законный пользователь не должен оказаться заблокирован задним числом")
}

func TestLogoutInvalidatesSession(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "l@example.com")
	token := c.cookie(security.SessionCookie)
	require.NotEmpty(t, token)

	require.Equal(t, http.StatusNoContent, c.do(http.MethodPost, "/api/auth/logout", nil).Status)
	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/me", nil).Status)

	// Клиент, сохранивший старую cookie, тоже не должен пройти.
	stale := env.client(t)
	r := stale.do(http.MethodGet, "/api/me", nil, [2]string{"Cookie", security.SessionCookie + "=" + token})
	require.Equal(t, http.StatusUnauthorized, r.Status)
}

func TestSessionTokenStoredHashed(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "h@example.com")
	token := c.cookie(security.SessionCookie)

	var hashes []string
	require.NoError(t, env.db.Model(&models.UserSession{}).Pluck("token_hash", &hashes).Error)
	require.NotEmpty(t, hashes)
	require.NotContains(t, hashes, token, "утечка дампа БД не должна давать готовые сессии")
	require.Contains(t, hashes, security.HashToken(token))
}

func TestUnknownCookieRejected(t *testing.T) {
	c := newTestEnv(t).client(t)
	r := c.do(http.MethodGet, "/api/me", nil, [2]string{"Cookie", security.SessionCookie + "=made-up-token"})
	require.Equal(t, http.StatusUnauthorized, r.Status)
}

func TestDeactivatedUserCannotAuthenticate(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "d@example.com")
	require.NoError(t, env.db.Model(&models.User{}).Where("email = ?", "d@example.com").
		Update("is_active", false).Error)

	require.Equal(t, http.StatusUnauthorized, c.do(http.MethodGet, "/api/me", nil).Status,
		"ранее выданная сессия должна перестать работать")
	require.Equal(t, http.StatusUnauthorized, c.login("d@example.com", testPassword).Status)
}

func TestRegistrationAllowlist(t *testing.T) {
	// Бета по приглашению: регистрируются только адреса и домены из списка.
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Cfg.RegistrationAllowlist = " Boss@Example.com, @team.example "
	})
	c := env.client(t)
	require.Equal(t, http.StatusCreated, c.register("boss@example.com").Status)
	require.Equal(t, http.StatusCreated, c.register("Anna@Team.Example").Status)
	r := c.register("stranger@example.com")
	require.Equal(t, http.StatusForbidden, r.Status)
	require.Contains(t, string(r.Body), "по приглашению")
	require.Equal(t, http.StatusForbidden, c.register("x@evilteam.example").Status, "домен — целиком, не суффикс")
	require.Equal(t, http.StatusUnauthorized, c.login("stranger@example.com", testPassword).Status, "аккаунт не создан")
}
