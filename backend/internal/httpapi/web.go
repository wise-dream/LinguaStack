package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
)

// pageCSP — политика для страниц веб-приложения: всё только со своего
// origin. 'unsafe-inline' в style-src нужен атрибутам style, которые
// выставляет Vue; скриптов со стороны и встроенных скриптов нет.
const pageCSP = "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; " +
	"frame-ancestors 'none'"

// webApp раздаёт собранное веб-приложение (frontend/dist). Маршруты SPA
// существуют только в браузере, поэтому любой путь вне /api и /assets
// получает index.html. Файлы читаются через os.Root: ссылка или «..» не
// выведут за каталог сборки.
type webApp struct {
	root *os.Root
}

func newWebApp(dir string) (*webApp, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("каталог веб-приложения %q: %w", dir, err)
	}
	if _, err := root.Stat("index.html"); err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("в каталоге веб-приложения %q нет index.html: %w", dir, err)
	}
	return &webApp{root: root}, nil
}

func (a *webApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", pageCSP)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")

	clean := path.Clean("/" + r.URL.Path)
	switch {
	case clean == "/api" || strings.HasPrefix(clean, "/api/"):
		writeError(w, http.StatusNotFound, "Не найдено")
	case clean == "/assets" || strings.HasPrefix(clean, "/assets/"):
		// Имена собранных файлов содержат хэш содержимого. Отсутствующий
		// файл — 404, а не index.html: HTML вместо скрипта сломал бы
		// страницу молча.
		if !a.serveFile(w, r, strings.TrimPrefix(clean, "/"), "public, max-age=31536000, immutable") {
			http.NotFound(w, r)
		}
	default:
		if clean != "/" && a.serveFile(w, r, strings.TrimPrefix(clean, "/"), "public, max-age=3600") {
			return
		}
		if !a.serveFile(w, r, "index.html", "no-cache") {
			http.NotFound(w, r)
		}
	}
}

// serveFile отдаёт обычный файл каталога сборки; false — такого нет.
func (a *webApp) serveFile(w http.ResponseWriter, r *http.Request, name, cacheControl string) bool {
	f, err := a.root.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	return true
}

// CheckWebRoot проверяет каталог сборки при старте: ошибка в
// LINGUASTACK_WEB_ROOT должна остановить сервис, а не превратиться в 404
// на каждой странице.
func CheckWebRoot(dir string) error {
	if dir == "" {
		return nil
	}
	app, err := newWebApp(dir)
	if err != nil {
		return err
	}
	return app.root.Close()
}

// mountWeb подключает раздачу веб-приложения, если задан каталог сборки.
func mountWeb(mux *http.ServeMux, dir string) {
	if dir == "" {
		return
	}
	app, err := newWebApp(dir)
	if err != nil {
		slog.Error("веб-приложение не подключено", "error", err)
		return
	}
	mux.Handle("GET /", app)
}
