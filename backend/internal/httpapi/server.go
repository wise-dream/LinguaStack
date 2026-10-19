// Package httpapi собирает HTTP-слой: маршруты, обработчики, middleware.
package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/storage"
)

// Deps — всё, что нужно обработчикам. Собирается в main и в тестах.
type Deps struct {
	Cfg       config.Settings
	DB        *gorm.DB
	Store     *storage.FileStore
	Importers *importers.Registry
	// Secrets — nil, если не задан LINGUASTACK_SECRET_KEY: сервер работает,
	// но конфигурации с ключами API недоступны.
	Secrets *security.SecretBox
	// BuildProvider собирает адаптер по конфигурации; nil — сборка по
	// настройкам сервера. Тесты подставляют Fake.
	BuildProvider func(cfg models.ProviderConfig, apiKey string) (providers.Provider, error)
	// SSEInterval — частота опроса прогресса для потока событий.
	SSEInterval time.Duration
	// Stopping отменяется в начале остановки сервера: потоки событий по
	// нему закрываются. http.Server.Shutdown контексты запросов не
	// отменяет и ждёт их завершения, а поток сам не кончается. nil —
	// сервер не останавливается (тесты).
	Stopping context.Context
}

type server struct {
	Deps
}

// Route — зарегистрированный маршрут. Auth — нужен вход.
type Route struct {
	Method string
	Path   string
	Auth   bool
}

type router struct {
	mux    *http.ServeMux
	routes []Route
	s      *server
}

func (rt *router) add(pattern string, auth bool, h http.HandlerFunc) {
	method, path, _ := strings.Cut(pattern, " ")
	rt.routes = append(rt.routes, Route{Method: method, Path: path, Auth: auth})
	if auth {
		h = rt.s.requireUser(h)
	}
	rt.mux.HandleFunc(pattern, h)
}

func (rt *router) public(pattern string, h http.HandlerFunc)    { rt.add(pattern, false, h) }
func (rt *router) protected(pattern string, h http.HandlerFunc) { rt.add(pattern, true, h) }

// NewServer возвращает маршрутизатор со всеми эндпоинтами.
//
// Используется http.ServeMux из стандартной библиотеки: с Go 1.22 он
// понимает метод и параметры в шаблоне ("GET /api/books/{id}"), поэтому
// отдельный роутер как зависимость не нужен.
func NewServer(d Deps) http.Handler {
	s := &server{Deps: d}
	if s.BuildProvider == nil {
		s.BuildProvider = s.defaultBuildProvider
	}
	if s.SSEInterval <= 0 {
		s.SSEInterval = time.Second
	}
	if s.Stopping == nil {
		s.Stopping = context.Background()
	}
	return buildRoutes(s).mux
}

// Routes — все маршруты сервера. По ним матрица изоляции в тестах
// проверяет каждый эндпоинт: новый маршрут без записи в матрице роняет тест.
func Routes() []Route {
	return buildRoutes(&server{}).routes
}

func buildRoutes(s *server) *router {
	rt := &router{mux: http.NewServeMux(), s: s}

	rt.public("GET /api/health", s.health)
	rt.public("POST /api/auth/register", s.register)
	rt.public("POST /api/auth/login", s.login)
	rt.public("POST /api/auth/logout", s.logout)
	rt.protected("GET /api/me", s.me)

	rt.protected("POST /api/books", s.uploadBook)
	rt.protected("GET /api/books", s.listBooks)
	rt.protected("GET /api/books/{id}", s.getBook)
	rt.protected("DELETE /api/books/{id}", s.deleteBook)
	rt.protected("GET /api/books/{id}/toc", s.getTOC)
	rt.protected("PUT /api/books/{id}/toc", s.putTOC)
	rt.protected("POST /api/books/{id}/translations", s.createTranslation)

	rt.protected("GET /api/providers", s.listKinds)
	rt.protected("GET /api/providers/{kind}/models", s.kindModels)
	rt.protected("POST /api/provider-configs", s.createProviderConfig)
	rt.protected("GET /api/provider-configs", s.listProviderConfigs)
	rt.protected("GET /api/provider-configs/{id}", s.getProviderConfig)
	rt.protected("PATCH /api/provider-configs/{id}", s.patchProviderConfig)
	rt.protected("DELETE /api/provider-configs/{id}", s.deleteProviderConfig)
	rt.protected("GET /api/provider-configs/{id}/models", s.configModels)
	rt.protected("GET /api/provider-configs/{id}/health", s.configHealth)

	rt.protected("GET /api/translation-options", s.translationOptions)
	rt.protected("GET /api/translations", s.listTranslations)
	rt.protected("GET /api/translations/{id}", s.getTranslation)
	rt.protected("POST /api/translations/{id}/pause", s.pauseTranslation)
	rt.protected("POST /api/translations/{id}/resume", s.resumeTranslation)
	rt.protected("POST /api/translations/{id}/cancel", s.cancelTranslation)
	rt.protected("GET /api/translations/{id}/segments", s.listSegments)
	rt.protected("GET /api/translations/{id}/events", s.translationEvents)

	rt.protected("PATCH /api/segments/{id}", s.patchSegment)
	rt.protected("POST /api/segments/{id}/retry", s.retrySegment)
	rt.protected("GET /api/segments/{id}/revisions", s.segmentRevisions)
	return rt
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
