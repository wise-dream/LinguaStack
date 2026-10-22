package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/httpapi"
)

// Матрица изоляции (ТЗ §2.2, §12): каждая операция с ресурсом проверяет
// права. Маршруты берутся из сервера, а не дублируются здесь: новый
// эндпоинт с параметром, не внесённый в матрицу, роняет тест.
//
// Общая библиотека (дополнение §9): книги и переводы читают и скачивают
// все — эти маршруты чужому отвечают, их проверяет shared_test.go.
// Остальное — только владельцу: чужому 404.
var sharedRoutes = map[string]bool{
	"GET /api/books/{id}":                  true,
	"GET /api/books/{id}/toc":              true,
	"GET /api/books/{id}/chapters/{index}": true,
	"GET /api/books/{id}/assets/{path...}": true,
	"GET /api/books/{id}/search":           true,
	"GET /api/books/{id}/position":         true,
	"PUT /api/books/{id}/position":         true,
	"POST /api/books/{id}/exports":         true,
	"GET /api/books/{id}/exports":          true,
	"GET /api/translations/{id}":           true,
	"GET /api/translations/{id}/segments":  true,
	"GET /api/translations/{id}/events":    true,
}

func TestSharedRoutesExist(t *testing.T) {
	// Опечатка в списке общих маршрутов тихо вывела бы маршрут из матрицы.
	all := map[string]bool{}
	for _, route := range httpapi.Routes() {
		all[route.Method+" "+route.Path] = true
	}
	for r := range sharedRoutes {
		require.True(t, all[r], "общий маршрут %s не зарегистрирован", r)
	}
}

type foreignResources struct {
	book, project, segment, config, export, user string
}

func setupForeign(t *testing.T) (*translationEnv, *apiClient, foreignResources) {
	t.Helper()
	owner := newTranslationEnv(t, nil)
	p := owner.project(t, owner.book(t, "# Ch\n\nOne."))
	owner.runWorker(t)
	seg := owner.segments(t, p.ID)[0]
	export := owner.export(t, p, "markdown", "translation")
	owner.runExports(t)
	intruder := owner.signedIn(t, "intruder@example.com")
	return owner, intruder, foreignResources{book: p.BookID, project: p.ID, segment: seg.ID,
		config: owner.ollamaConfig(t), export: export.ID, user: owner.c.meID(t)}
}

func (f foreignResources) url(t *testing.T, path string) (string, bool) {
	t.Helper()
	// Вторичные параметры — правдоподобными значениями: иначе маршрут мог
	// бы отвечать 404 из-за них, а не из-за чужого владельца.
	path = strings.NewReplacer("{index}", "0", "{path...}", "img/d.png").Replace(path)
	switch {
	case !strings.Contains(path, "{"):
		return path, true
	case strings.HasPrefix(path, "/api/providers/{kind}"):
		return "", false // каталог видов провайдеров, а не ресурс пользователя
	case strings.HasPrefix(path, "/api/books/{id}"):
		return strings.Replace(path, "{id}", f.book, 1), true
	case strings.HasPrefix(path, "/api/translations/{id}"):
		return strings.Replace(path, "{id}", f.project, 1), true
	case strings.HasPrefix(path, "/api/segments/{id}"):
		return strings.Replace(path, "{id}", f.segment, 1), true
	case strings.HasPrefix(path, "/api/provider-configs/{id}"):
		return strings.Replace(path, "{id}", f.config, 1), true
	case strings.HasPrefix(path, "/api/exports/{id}"):
		return strings.Replace(path, "{id}", f.export, 1), true
	case strings.HasPrefix(path, "/api/admin/users/{id}"):
		// Для не администратора админские маршруты не существуют.
		return strings.Replace(path, "{id}", f.user, 1), true
	}
	t.Errorf("маршрут %s не внесён в матрицу изоляции — добавьте его ресурс", path)
	return "", false
}

func TestEveryResourceEndpointIs404ForForeignUser(t *testing.T) {
	_, intruder, foreign := setupForeign(t)
	checked := 0
	for _, route := range httpapi.Routes() {
		if !route.Auth || !strings.Contains(route.Path, "{") || sharedRoutes[route.Method+" "+route.Path] {
			continue
		}
		url, ok := foreign.url(t, route.Path)
		if !ok {
			continue
		}
		r := intruder.do(route.Method, url, map[string]any{})
		require.Equal(t, http.StatusNotFound, r.Status, "%s %s пропускает чужой ресурс: %s", route.Method, route.Path, r.Body)
		checked++
	}
	require.GreaterOrEqual(t, checked, 15, "матрица должна покрывать все ресурсные маршруты")
}

func TestAdminHasNoContentAccess(t *testing.T) {
	// Роль admin не даёт прав сверх обычного пользователя (ТЗ §2.1,
	// дополнение §9): чужое — только общее чтение, остальное — тот же 404.
	owner, intruder, foreign := setupForeign(t)
	owner.promote(t, "intruder@example.com")
	require.Equal(t, http.StatusOK, intruder.do(http.MethodGet, "/api/admin/users", nil).Status, "чужак стал администратором")
	checked := 0
	for _, route := range httpapi.Routes() {
		if !route.Auth || !strings.Contains(route.Path, "{") || strings.HasPrefix(route.Path, "/api/admin/") ||
			sharedRoutes[route.Method+" "+route.Path] {
			continue
		}
		url, ok := foreign.url(t, route.Path)
		if !ok {
			continue
		}
		r := intruder.do(route.Method, url, map[string]any{})
		require.Equal(t, http.StatusNotFound, r.Status, "%s %s открывает администратору чужое: %s", route.Method, route.Path, r.Body)
		checked++
	}
	require.GreaterOrEqual(t, checked, 15)
}

func TestEveryProtectedEndpointRequiresAuthentication(t *testing.T) {
	_, _, foreign := setupForeign(t)
	anonymous := newTestEnv(t).client(t)
	for _, route := range httpapi.Routes() {
		if !route.Auth {
			continue
		}
		url, ok := foreign.url(t, route.Path)
		if !ok {
			url = strings.Replace(route.Path, "{kind}", "ollama", 1)
		}
		r := anonymous.do(route.Method, url, map[string]any{})
		require.Equal(t, http.StatusUnauthorized, r.Status, "%s %s", route.Method, route.Path)
	}
}

func TestPublicRoutesAreOnlyTheExpectedOnes(t *testing.T) {
	// Открытый без входа эндпоинт — осознанное решение, а не забытая обёртка.
	var public []string
	for _, route := range httpapi.Routes() {
		if !route.Auth {
			public = append(public, route.Method+" "+route.Path)
		}
	}
	require.ElementsMatch(t, []string{
		"GET /api/health", "POST /api/auth/register", "POST /api/auth/login", "POST /api/auth/logout",
		"POST /api/auth/password-reset", "POST /api/auth/password-reset/confirm",
	}, public)
}
