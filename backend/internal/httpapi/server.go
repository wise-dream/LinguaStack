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
	"github.com/vnovels/linguastack/internal/mail"
	"github.com/vnovels/linguastack/internal/metrics"
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
	// Mailer — отправка писем (сброс пароля); nil — почта не настроена.
	Mailer mail.Sender
	// Metrics — реестр метрик api; nil — создаётся по DB.
	Metrics *metrics.API
	// Stopping отменяется в начале остановки сервера: потоки событий по
	// нему закрываются. http.Server.Shutdown контексты запросов не
	// отменяет и ждёт их завершения, а поток сам не кончается. nil —
	// сервер не останавливается (тесты).
	Stopping context.Context
}

type server struct {
	Deps
	limits limits
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

func (rt *router) public(pattern string, h http.HandlerFunc) { rt.add(pattern, false, h) }

// protected — маршрут со входом и лимитом частоты на пользователя.
func (rt *router) protected(pattern string, h http.HandlerFunc) {
	rt.add(pattern, true, limited(rt.s.limits.user, byUser, tooManyRequests, h))
}

// admin — маршрут администратора; остальным его нет (404).
func (rt *router) admin(pattern string, h http.HandlerFunc) {
	rt.protected(pattern, rt.s.requireAdmin(h))
}

// stream — маршрут со входом без лимита частоты: поток событий открыт,
// пока открыта вкладка, и переподключается сам; картинки книги глава
// грузит десятками разом, а отдать файл дёшево.
func (rt *router) stream(pattern string, h http.HandlerFunc) { rt.add(pattern, true, h) }

// NewServer возвращает маршрутизатор со всеми эндпоинтами.
//
// Используется http.ServeMux из стандартной библиотеки: с Go 1.22 он
// понимает метод и параметры в шаблоне ("GET /api/books/{id}"), поэтому
// отдельный роутер как зависимость не нужен.
func NewServer(d Deps) http.Handler {
	s := &server{Deps: d, limits: newLimits(d.Cfg)}
	if s.BuildProvider == nil {
		s.BuildProvider = s.defaultBuildProvider
	}
	if s.SSEInterval <= 0 {
		s.SSEInterval = time.Second
	}
	if s.Stopping == nil {
		s.Stopping = context.Background()
	}
	if s.Metrics == nil {
		s.Metrics = metrics.NewAPI(s.DB)
	}
	mux := buildRoutes(s).mux
	// Вне Routes(): у метрик свой токен, а не сессия пользователя.
	mux.Handle("GET /metrics", s.Metrics.Handler(s.Cfg.MetricsToken))
	mountWeb(mux, s.Cfg.WebRoot)
	return s.observe(mux)
}

// Routes — все маршруты сервера. По ним матрица изоляции в тестах
// проверяет каждый эндпоинт: новый маршрут без записи в матрице роняет тест.
func Routes() []Route {
	return buildRoutes(&server{}).routes
}

func buildRoutes(s *server) *router {
	rt := &router{mux: http.NewServeMux(), s: s}

	rt.public("GET /api/health", s.health)
	rt.public("POST /api/auth/register", limited(s.limits.register, s.byIP, tooManyRegisters, s.register))
	rt.public("POST /api/auth/login", s.login)
	rt.public("POST /api/auth/logout", s.logout)
	rt.public("POST /api/auth/password-reset", limited(s.limits.reset, s.byIP, tooManyResets, s.requestPasswordReset))
	rt.public("POST /api/auth/password-reset/confirm", s.confirmPasswordReset)
	rt.protected("GET /api/me", s.me)
	rt.protected("PATCH /api/me", s.changeEmail)
	rt.protected("DELETE /api/me", s.deleteAccount)
	rt.protected("POST /api/me/password", s.changePassword)

	rt.protected("POST /api/books", limited(s.limits.uploads, byUser, tooManyUploads, s.uploadBook))
	rt.protected("GET /api/books", s.listBooks)
	rt.protected("GET /api/books/{id}", s.getBook)
	rt.protected("DELETE /api/books/{id}", s.deleteBook)
	rt.protected("GET /api/books/{id}/toc", s.getTOC)
	rt.protected("GET /api/books/{id}/chapters/{index}", s.getChapter)
	rt.stream("GET /api/books/{id}/assets/{path...}", s.getAsset)
	rt.protected("GET /api/books/{id}/search", s.searchBook)
	rt.protected("GET /api/books/{id}/position", s.getPosition)
	rt.protected("PUT /api/books/{id}/position", s.putPosition)
	rt.protected("PUT /api/books/{id}/toc", s.putTOC)
	rt.protected("POST /api/books/{id}/translations", s.createTranslation)
	rt.protected("POST /api/books/{id}/exports", s.createExport)
	rt.protected("GET /api/books/{id}/exports", s.listBookExports)
	rt.protected("GET /api/exports/{id}", s.getExport)
	rt.protected("GET /api/exports/{id}/file", s.downloadExport)
	rt.protected("DELETE /api/exports/{id}", s.deleteExport)

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
	rt.protected("POST /api/translations/{id}/retry-problems", s.retryProblems)
	rt.protected("GET /api/translations/{id}/segments", s.listSegments)
	rt.stream("GET /api/translations/{id}/events", s.translationEvents)

	rt.protected("GET /api/telegram", s.getTelegram)
	rt.protected("POST /api/telegram/link", s.linkTelegram)
	rt.protected("PATCH /api/telegram", s.patchTelegram)
	rt.protected("DELETE /api/telegram", s.unlinkTelegram)

	rt.admin("GET /api/admin/overview", s.adminOverview)
	rt.admin("GET /api/admin/users", s.adminListUsers)
	rt.admin("PATCH /api/admin/users/{id}", s.adminPatchUser)
	rt.admin("POST /api/admin/users/{id}/password-reset", s.adminResetLink)

	rt.protected("PATCH /api/segments/{id}", s.patchSegment)
	rt.protected("POST /api/segments/{id}/retry", s.retrySegment)
	rt.protected("GET /api/segments/{id}/revisions", s.segmentRevisions)
	return rt
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
