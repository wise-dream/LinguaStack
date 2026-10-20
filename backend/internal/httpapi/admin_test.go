package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
)

// noOllama — адрес, где Ollama заведомо нет: тесты не зависят от
// запущенного на машине сервера.
const noOllama = "http://127.0.0.1:9"

func newAdminEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.OllamaBaseURL = noOllama })
}

// promote делает пользователя администратором напрямую в БД — так же,
// как команда api -set-role.
func (e *testEnv) promote(t *testing.T, email string) {
	t.Helper()
	require.NoError(t, e.db.Model(&models.User{}).Where("email = ?", email).Update("role", models.RoleAdmin).Error)
}

func (c *apiClient) meID(t *testing.T) string {
	t.Helper()
	var me struct {
		ID string `json:"id"`
	}
	r := c.do(http.MethodGet, "/api/me", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &me)
	return me.ID
}

type adminUser struct {
	ID                 string  `json:"id"`
	Email              string  `json:"email"`
	Role               string  `json:"role"`
	IsActive           bool    `json:"is_active"`
	CreatedAt          string  `json:"created_at"`
	LastSeenAt         *string `json:"last_seen_at"`
	Books              int64   `json:"books"`
	Translations       int64   `json:"translations"`
	StorageBytes       int64   `json:"storage_bytes"`
	MaxActiveProjects  *int    `json:"max_active_projects"`
	MaxConcurrentTasks *int    `json:"max_concurrent_tasks"`
	StorageQuotaBytes  *int64  `json:"storage_quota_bytes"`
}

type adminUsers struct {
	Users    []adminUser `json:"users"`
	Defaults struct {
		MaxActiveProjects  int   `json:"max_active_projects"`
		MaxConcurrentTasks int   `json:"max_concurrent_tasks"`
		StorageQuotaBytes  int64 `json:"storage_quota_bytes"`
	} `json:"defaults"`
}

func (c *apiClient) adminUser(t *testing.T, email string) adminUser {
	t.Helper()
	r := c.do(http.MethodGet, "/api/admin/users", nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var list adminUsers
	r.JSON(t, &list)
	for _, u := range list.Users {
		if u.Email == email {
			return u
		}
	}
	t.Fatalf("пользователя %s нет в списке", email)
	return adminUser{}
}

func TestAdminRoutesHiddenFromUsers(t *testing.T) {
	env := newAdminEnv(t)
	user := env.signedIn(t, "user@example.com")
	id := user.meID(t)
	checked := 0
	for _, route := range httpapi.Routes() {
		if !strings.HasPrefix(route.Path, "/api/admin/") {
			continue
		}
		path := strings.Replace(route.Path, "{id}", id, 1)
		r := user.do(route.Method, path, map[string]any{"is_active": false})
		require.Equal(t, http.StatusNotFound, r.Status, "%s %s виден не администратору", route.Method, route.Path)
		checked++
	}
	require.GreaterOrEqual(t, checked, 3)
	var me models.User
	require.NoError(t, env.db.First(&me, "email = ?", "user@example.com").Error)
	require.True(t, me.IsActive, "запрос не администратора ничего не изменил")
}

func TestAdminOverview(t *testing.T) {
	env := newAdminEnv(t)
	admin := env.signedIn(t, "admin@example.com")
	env.signedIn(t, "user@example.com")
	env.promote(t, "admin@example.com")

	r := admin.do(http.MethodGet, "/api/admin/overview", nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var out struct {
		Metrics struct {
			Users    int64            `json:"users"`
			Projects map[string]int64 `json:"projects"`
		} `json:"metrics"`
		Providers []struct {
			ID        string   `json:"id"`
			BaseURL   string   `json:"base_url"`
			Available bool     `json:"available"`
			Models    []string `json:"models"`
		} `json:"providers"`
	}
	r.JSON(t, &out)
	require.GreaterOrEqual(t, out.Metrics.Users, int64(2))
	require.Contains(t, out.Metrics.Projects, models.ProjectRunning)
	require.Len(t, out.Providers, 1)
	require.Equal(t, "ollama", out.Providers[0].ID)
	require.Equal(t, noOllama, out.Providers[0].BaseURL)
	require.False(t, out.Providers[0].Available)
	require.NotNil(t, out.Providers[0].Models)
}

func TestAdminUsersListNoContent(t *testing.T) {
	env := newAdminEnv(t)
	admin := env.signedIn(t, "admin@example.com")
	env.promote(t, "admin@example.com")
	user := env.signedIn(t, "reader@example.com")
	content := "# Секретная книга\n\nТайный текст главы."
	uploadBook(t, user, "private-diary.md", content)

	r := admin.do(http.MethodGet, "/api/admin/users", nil)
	require.Equal(t, http.StatusOK, r.Status)
	for _, secret := range []string{"Секретная", "Тайный", "private-diary", "password"} {
		require.NotContains(t, string(r.Body), secret, "админ видит только метаданные и счётчики")
	}
	u := admin.adminUser(t, "reader@example.com")
	require.Equal(t, models.RoleUser, u.Role)
	require.True(t, u.IsActive)
	require.EqualValues(t, 1, u.Books)
	require.EqualValues(t, 0, u.Translations)
	require.EqualValues(t, len(content), u.StorageBytes)
	require.NotNil(t, u.LastSeenAt, "вход отмечает визит")
	require.Nil(t, u.MaxActiveProjects, "квота не задана — действует значение по умолчанию")

	var list adminUsers
	r.JSON(t, &list)
	require.Equal(t, env.cfg.MaxProjectsPerUser, list.Defaults.MaxActiveProjects)
	require.Equal(t, env.cfg.MaxTasksPerUser, list.Defaults.MaxConcurrentTasks)
	require.Equal(t, int64(2<<30), list.Defaults.StorageQuotaBytes)
}

func TestAdminDeactivate(t *testing.T) {
	te := newTranslationEnv(t, nil)
	admin := te.signedIn(t, "admin@example.com")
	te.promote(t, "admin@example.com")
	p := te.project(t, te.book(t, "# Ch\n\nOne.")) // в очереди, worker не запускался
	userID := te.c.meID(t)

	r := admin.do(http.MethodPatch, "/api/admin/users/"+userID, map[string]any{"is_active": false})
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)

	require.Equal(t, http.StatusUnauthorized, te.c.do(http.MethodGet, "/api/books", nil).Status, "открытая вкладка теряет сессию")
	require.Equal(t, http.StatusUnauthorized, te.client(t).login("u@example.com", testPassword).Status, "вход отклонён")
	var project models.TranslationProject
	require.NoError(t, te.db.First(&project, "id = ?", p.ID).Error)
	require.Equal(t, models.ProjectPaused, project.Status, "переводы встали на паузу")
	var pending int64
	require.NoError(t, te.db.Model(&models.TranslationTask{}).
		Where("project_id = ? AND status = ?", p.ID, models.TaskPending).Count(&pending).Error)
	require.Zero(t, pending, "worker не возьмёт задачу отключённого")
	var sessions int64
	require.NoError(t, te.db.Model(&models.UserSession{}).Where("user_id = ?", userID).Count(&sessions).Error)
	require.Zero(t, sessions)
	require.False(t, admin.adminUser(t, "u@example.com").IsActive)

	r = admin.do(http.MethodPatch, "/api/admin/users/"+userID, map[string]any{"is_active": true})
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, http.StatusOK, te.client(t).login("u@example.com", testPassword).Status, "включённый снова входит")
}

func TestAdminCannotDemoteSelf(t *testing.T) {
	env := newAdminEnv(t)
	admin := env.signedIn(t, "admin@example.com")
	env.promote(t, "admin@example.com")
	other := env.signedIn(t, "second@example.com")
	env.promote(t, "second@example.com")
	self := admin.meID(t)

	for _, body := range []map[string]any{{"is_active": false}, {"role": models.RoleUser}} {
		r := admin.do(http.MethodPatch, "/api/admin/users/"+self, body)
		require.Equal(t, http.StatusConflict, r.Status, "%v", body)
	}
	require.Equal(t, models.RoleAdmin, admin.adminUser(t, "admin@example.com").Role)

	r := admin.do(http.MethodPatch, "/api/admin/users/"+other.meID(t), map[string]any{"role": models.RoleUser})
	require.Equal(t, http.StatusOK, r.Status, "другого администратора разжаловать можно")
	require.Equal(t, http.StatusNotFound, other.do(http.MethodGet, "/api/admin/users", nil).Status)
}

func TestAdminPatchQuotas(t *testing.T) {
	env := newAdminEnv(t)
	admin := env.signedIn(t, "admin@example.com")
	env.promote(t, "admin@example.com")
	user := env.signedIn(t, "q@example.com")
	path := "/api/admin/users/" + user.meID(t)

	r := admin.do(http.MethodPatch, path, map[string]any{
		"max_active_projects": 2, "max_concurrent_tasks": 1, "storage_quota_bytes": 0,
	})
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	u := admin.adminUser(t, "q@example.com")
	require.Equal(t, 2, *u.MaxActiveProjects)
	require.Equal(t, 1, *u.MaxConcurrentTasks)
	require.Equal(t, int64(0), *u.StorageQuotaBytes, "0 — без предела")

	// Поле не передано — не меняется; null — снова по умолчанию.
	r = admin.do(http.MethodPatch, path, map[string]any{"max_active_projects": nil})
	require.Equal(t, http.StatusOK, r.Status)
	u = admin.adminUser(t, "q@example.com")
	require.Nil(t, u.MaxActiveProjects)
	require.Equal(t, 1, *u.MaxConcurrentTasks)

	for _, bad := range []map[string]any{
		{"max_active_projects": 0}, {"max_concurrent_tasks": -1}, {"storage_quota_bytes": -5},
		{"role": "root"}, {"email": "x@example.com"},
	} {
		require.Equal(t, http.StatusUnprocessableEntity, admin.do(http.MethodPatch, path, bad).Status, "%v", bad)
	}
	require.Equal(t, http.StatusNotFound,
		admin.do(http.MethodPatch, "/api/admin/users/00000000-0000-0000-0000-000000000000", map[string]any{"is_active": true}).Status)
	require.Equal(t, http.StatusNotFound, admin.do(http.MethodPatch, "/api/admin/users/nope", map[string]any{"is_active": true}).Status)
}
