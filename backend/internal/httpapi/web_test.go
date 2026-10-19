package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/httpapi"
)

// webEnv — сервер, раздающий собранное веб-приложение из временного каталога.
func webEnv(t *testing.T) *apiClient {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><title>LinguaStack</title>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "assets", "app-1a2b.js"), []byte("console.log(1)"), 0o600))
	secret := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("top secret"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "leak.txt")))
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.WebRoot = root })
	return env.client(t)
}

func TestSPAFallbackServesIndex(t *testing.T) {
	// Маршруты SPA (/books/123) существуют только в браузере: сервер на
	// любой из них отдаёт index.html, и роутер Vue открывает страницу.
	c := webEnv(t)
	for _, p := range []string{"/", "/books/123", "/read/abc?ch=2"} {
		r := c.do(http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, r.Status, p)
		require.Contains(t, string(r.Body), "<title>LinguaStack</title>", p)
		require.Equal(t, "no-cache", r.Header.Get("Cache-Control"), p)
		require.True(t, strings.HasPrefix(r.Header.Get("Content-Type"), "text/html"), p)
	}
}

func TestSPAAssetsImmutable(t *testing.T) {
	// Имена собранных файлов содержат хэш: их можно кэшировать навсегда.
	c := webEnv(t)
	r := c.do(http.MethodGet, "/assets/app-1a2b.js", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, "console.log(1)", string(r.Body))
	require.Equal(t, "public, max-age=31536000, immutable", r.Header.Get("Cache-Control"))
	require.Contains(t, r.Header.Get("Content-Type"), "javascript")

	// Отсутствующий файл сборки — 404, а не index.html: иначе браузер
	// попытался бы выполнить HTML как скрипт.
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/assets/old-9z9z.js", nil).Status)
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	c := webEnv(t)
	r := c.do(http.MethodGet, "/api/nope", nil)
	require.Equal(t, http.StatusNotFound, r.Status)
	require.JSONEq(t, `{"error":"Не найдено"}`, string(r.Body))
}

func TestSPASecurityHeaders(t *testing.T) {
	c := webEnv(t)
	r := c.do(http.MethodGet, "/", nil)
	csp := r.Header.Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'", "connect-src 'self'"} {
		require.Contains(t, csp, directive)
	}
	require.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
	require.Equal(t, "same-origin", r.Header.Get("Referrer-Policy"))
}

func TestSPANoDirectoryListing(t *testing.T) {
	c := webEnv(t)
	r := c.do(http.MethodGet, "/assets/", nil)
	require.NotContains(t, string(r.Body), "app-1a2b.js", "содержимое каталога не раскрывается")
	require.Equal(t, http.StatusNotFound, r.Status)
}

func TestSPASymlinkOutsideRootNotServed(t *testing.T) {
	c := webEnv(t)
	r := c.do(http.MethodGet, "/leak.txt", nil)
	require.NotContains(t, string(r.Body), "top secret")
}

func TestWebRootDisabledByDefault(t *testing.T) {
	c := newTestEnv(t).client(t)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/", nil).Status)
}
