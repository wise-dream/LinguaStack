// Package httpapi собирает HTTP-слой: маршруты, обработчики, middleware.
package httpapi

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/storage"
)

// Deps — всё, что нужно обработчикам. Собирается в main и в тестах.
type Deps struct {
	Cfg       config.Settings
	DB        *gorm.DB
	Store     *storage.FileStore
	Importers *importers.Registry
}

type server struct {
	Deps
}

// NewServer возвращает маршрутизатор со всеми эндпоинтами.
//
// Используется http.ServeMux из стандартной библиотеки: с Go 1.22 он
// понимает метод и параметры в шаблоне ("GET /api/books/{id}"), поэтому
// отдельный роутер как зависимость не нужен.
func NewServer(d Deps) http.Handler {
	s := &server{Deps: d}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.health)

	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.requireUser(s.me))

	mux.HandleFunc("POST /api/books", s.requireUser(s.uploadBook))
	mux.HandleFunc("GET /api/books", s.requireUser(s.listBooks))
	mux.HandleFunc("GET /api/books/{id}", s.requireUser(s.getBook))
	mux.HandleFunc("DELETE /api/books/{id}", s.requireUser(s.deleteBook))
	mux.HandleFunc("GET /api/books/{id}/toc", s.requireUser(s.getTOC))
	mux.HandleFunc("PUT /api/books/{id}/toc", s.requireUser(s.putTOC))

	return mux
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
