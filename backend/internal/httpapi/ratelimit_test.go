package httpapi_test

import (
	"net/http"
	"net/netip"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/httpapi"
)

// Пополнение настолько медленное, что за время теста токен не набежит.
const glacial = 1e-6

func TestAPIRateLimitedPerUser(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Cfg.RateLimitRPS, d.Cfg.RateLimitBurst = glacial, 3
	})
	a := env.signedIn(t, "a@example.com")
	b := env.signedIn(t, "b@example.com")
	for range 3 {
		require.Equal(t, http.StatusOK, a.do(http.MethodGet, "/api/books", nil).Status)
	}
	r := a.do(http.MethodGet, "/api/books", nil)
	require.Equal(t, http.StatusTooManyRequests, r.Status, "сверх всплеска — 429")
	retry, err := strconv.Atoi(r.Header.Get("Retry-After"))
	require.NoError(t, err, "Retry-After — целые секунды: %q", r.Header.Get("Retry-After"))
	require.Positive(t, retry)
	require.Contains(t, string(r.Body), "Слишком много запросов")

	require.Equal(t, http.StatusOK, b.do(http.MethodGet, "/api/books", nil).Status, "другой пользователь не задет")

	// Поток событий не ограничивается: долгий и один на вкладку.
	events := a.do(http.MethodGet, "/api/translations/"+uuid.NewString()+"/events", nil)
	require.Equal(t, http.StatusNotFound, events.Status)
}

func TestRegisterRateLimitedPerIP(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.RegisterPerHour = 2 })
	c := env.client(t)
	require.Equal(t, http.StatusCreated, c.register("r1@example.com").Status)
	require.Equal(t, http.StatusCreated, c.register("r2@example.com").Status)
	// Без доверенного прокси X-Forwarded-For не в счёт: подставленный
	// адрес лимит не обходит.
	r := c.do(http.MethodPost, "/api/auth/register",
		map[string]string{"email": "r3@example.com", "password": testPassword},
		[2]string{"X-Forwarded-For", "198.51.100.77"})
	require.Equal(t, http.StatusTooManyRequests, r.Status)
	require.NotEmpty(t, r.Header.Get("Retry-After"))
	require.Equal(t, http.StatusUnauthorized, c.login("r3@example.com", testPassword).Status, "аккаунт не создан")
}

func TestRegisterBehindTrustedProxy(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Cfg.RegisterPerHour = 1
		d.Cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	})
	c := env.client(t)
	reg := func(email, xff string) int {
		return c.do(http.MethodPost, "/api/auth/register",
			map[string]string{"email": email, "password": testPassword},
			[2]string{"X-Forwarded-For", xff}).Status
	}
	require.Equal(t, http.StatusCreated, reg("p1@example.com", "198.51.100.1"))
	require.Equal(t, http.StatusTooManyRequests, reg("p2@example.com", "198.51.100.1"))
	// Левый адрес цепочки клиент дописывает сам — не помогает.
	require.Equal(t, http.StatusTooManyRequests, reg("p3@example.com", "203.0.113.9, 198.51.100.1"))
	require.Equal(t, http.StatusCreated, reg("p4@example.com", "198.51.100.2"), "у другого клиента своя корзина")
}

func TestUploadRateLimited(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.UploadsPerHour = 1 })
	c := env.signedIn(t, "u@example.com")
	uploadBook(t, c, "one.txt", "Первая книга.")
	r := c.upload("two.txt", []byte("Вторая книга."))
	require.Equal(t, http.StatusTooManyRequests, r.Status)
	require.Contains(t, string(r.Body), "загрузок")
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/books", nil).Status, "остальные запросы идут")
}

func TestHealthNotLimited(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Cfg.RateLimitRPS, d.Cfg.RateLimitBurst = glacial, 1
		d.Cfg.RegisterPerHour, d.Cfg.UploadsPerHour = 1, 1
	})
	c := env.client(t)
	for range 20 {
		require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/health", nil).Status)
	}
}
