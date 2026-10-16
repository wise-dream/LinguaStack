// Package httpapi собирает HTTP-слой: маршруты, обработчики, middleware.
package httpapi

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
)

// Deps — всё, что нужно обработчикам. Собирается в main и в тестах.
type Deps struct {
	Cfg config.Settings
	DB  *gorm.DB
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

	return mux
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
