// Package httpapi собирает HTTP-слой: маршруты, обработчики, middleware.
package httpapi

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
)

// Deps — всё, что нужно обработчикам. Собирается в main и в тестах.
type Deps struct {
	Cfg config.Settings
	DB  *gorm.DB
}

// NewServer возвращает маршрутизатор со всеми эндпоинтами.
//
// Используется http.ServeMux из стандартной библиотеки: с Go 1.22 он
// понимает метод и параметры в шаблоне ("GET /api/books/{id}"), поэтому
// отдельный роутер как зависимость не нужен.
func NewServer(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
