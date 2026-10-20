package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/metrics"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/security"
)

// Админ-панель (ТЗ §2.1, §8.1 №12; дополнение §18). Ответы — метаданные и
// счётчики: ни названий книг, ни текстов (дополнение §9). К содержимому
// администратор обращается как любой чужак — и получает 404.

const (
	adminUsersLimit  = 500
	maxQuotaValue    = 10000
	overviewTimeout  = 5 * time.Second
	lastSeenInterval = 5 * time.Minute
)

// requireAdmin — после requireUser. Не администратору админского маршрута
// нет: 404, как у чужого ресурса, а не 403 — по ответу не видно, что он
// существует.
func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()).Role != models.RoleAdmin {
			writeError(w, http.StatusNotFound, "Не найдено")
			return
		}
		next(w, r)
	}
}

// touchLastSeen отмечает визит не чаще раза в lastSeenInterval: запись на
// каждый запрос нагрузила бы БД без пользы для панели.
func (s *server) touchLastSeen(ctx context.Context, user *models.User) {
	now := time.Now().UTC()
	if user.LastSeenAt != nil && now.Sub(*user.LastSeenAt) < lastSeenInterval {
		return
	}
	err := s.DB.WithContext(ctx).Model(&models.User{}).Where("id = ?", user.ID).UpdateColumn("last_seen_at", now).Error
	if err != nil {
		slog.Warn("не удалось отметить визит", "error", err)
		return
	}
	user.LastSeenAt = &now
}

type providerStatus struct {
	ID        string   `json:"id"`
	BaseURL   string   `json:"base_url"`
	Available bool     `json:"available"`
	Detail    string   `json:"detail,omitempty"`
	Models    []string `json:"models"`
}

type overviewOut struct {
	Metrics   metrics.Snapshot `json:"metrics"`
	Providers []providerStatus `json:"providers"`
}

func (s *server) adminOverview(w http.ResponseWriter, r *http.Request) {
	snap, err := metrics.TakeSnapshot(r.Context(), s.DB)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overviewOut{Metrics: snap, Providers: []providerStatus{s.serverOllama(r.Context())}})
}

// serverOllama — Ollama сервера: доступен ли и какие модели есть.
func (s *server) serverOllama(ctx context.Context) providerStatus {
	ctx, cancel := context.WithTimeout(ctx, overviewTimeout)
	defer cancel()
	st := providerStatus{ID: "ollama", BaseURL: s.Cfg.OllamaBaseURL, Models: []string{}}
	p, err := providers.Build("ollama", "", "", s.buildOptions())
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	h := p.Health(ctx)
	st.Available, st.Detail = h.Available, h.Detail
	if !h.Available {
		return st
	}
	list, err := p.ListModels(ctx)
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	for _, m := range list {
		st.Models = append(st.Models, m.ModelID)
	}
	return st
}

type adminUserOut struct {
	ID                 uuid.UUID  `json:"id"`
	Email              string     `json:"email"`
	Role               string     `json:"role"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          time.Time  `json:"created_at"`
	LastSeenAt         *time.Time `json:"last_seen_at"`
	Books              int64      `json:"books"`
	Translations       int64      `json:"translations"`
	StorageBytes       int64      `json:"storage_bytes"`
	MaxActiveProjects  *int       `json:"max_active_projects"`
	MaxConcurrentTasks *int       `json:"max_concurrent_tasks"`
	StorageQuotaBytes  *int64     `json:"storage_quota_bytes"`
}

type quotaDefaults struct {
	MaxActiveProjects  int   `json:"max_active_projects"`
	MaxConcurrentTasks int   `json:"max_concurrent_tasks"`
	StorageQuotaBytes  int64 `json:"storage_quota_bytes"`
}

type adminUsersOut struct {
	Users    []adminUserOut `json:"users"`
	Defaults quotaDefaults  `json:"defaults"`
}

// adminUsersQuery — пользователи со счётчиками. Подзапросы идут по
// индексам владельца книг и проектов.
const adminUsersQuery = `
SELECT u.id, u.email, u.role, u.is_active, u.created_at, u.last_seen_at,
       u.max_active_projects, u.max_concurrent_tasks, u.storage_quota_bytes,
       (SELECT count(*) FROM books b WHERE b.owner_id = u.id) AS books,
       (SELECT coalesce(sum(b.original_size), 0) FROM books b WHERE b.owner_id = u.id) AS storage_bytes,
       (SELECT count(*) FROM translation_projects p WHERE p.owner_id = u.id) AS translations
  FROM users u`

func (s *server) loadAdminUsers(ctx context.Context, where string, args ...any) ([]adminUserOut, error) {
	rows := []adminUserOut{}
	err := s.DB.WithContext(ctx).Raw(adminUsersQuery+" WHERE "+where, args...).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("пользователи для админ-панели: %w", err)
	}
	return rows, nil
}

func (s *server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
	users, err := s.loadAdminUsers(r.Context(), "u.email LIKE ? ORDER BY u.created_at DESC LIMIT ?", pattern, adminUsersLimit)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, adminUsersOut{Users: users, Defaults: quotaDefaults{
		MaxActiveProjects: s.Cfg.MaxProjectsPerUser, MaxConcurrentTasks: s.Cfg.MaxTasksPerUser,
		StorageQuotaBytes: s.Cfg.StorageQuotaBytes,
	}})
}

// optional различает «поле не передано» и «null»: null возвращает квоте
// значение по умолчанию, отсутствие поля её не трогает.
type optional[T any] struct {
	set   bool
	value *T
}

func (o *optional[T]) UnmarshalJSON(b []byte) error {
	o.set = true
	if string(b) == "null" {
		o.value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.value = &v
	return nil
}

type adminUserPatch struct {
	IsActive           *bool           `json:"is_active"`
	Role               *string         `json:"role"`
	MaxActiveProjects  optional[int]   `json:"max_active_projects"`
	MaxConcurrentTasks optional[int]   `json:"max_concurrent_tasks"`
	StorageQuotaBytes  optional[int64] `json:"storage_quota_bytes"`
}

func (in adminUserPatch) validate() string {
	if in.Role != nil && *in.Role != models.RoleUser && *in.Role != models.RoleAdmin {
		return "Роль — user или admin"
	}
	for _, q := range []optional[int]{in.MaxActiveProjects, in.MaxConcurrentTasks} {
		if q.value != nil && (*q.value < 1 || *q.value > maxQuotaValue) {
			return fmt.Sprintf("Квота переводов и задач — от 1 до %d", maxQuotaValue)
		}
	}
	if v := in.StorageQuotaBytes.value; v != nil && *v < 0 {
		return "Квота хранилища — не меньше 0 (0 — без предела)"
	}
	return ""
}

func (in adminUserPatch) changes() map[string]any {
	set := map[string]any{}
	if in.IsActive != nil {
		set["is_active"] = *in.IsActive
	}
	if in.Role != nil {
		set["role"] = *in.Role
	}
	if in.MaxActiveProjects.set {
		set["max_active_projects"] = in.MaxActiveProjects.value
	}
	if in.MaxConcurrentTasks.set {
		set["max_concurrent_tasks"] = in.MaxConcurrentTasks.value
	}
	if in.StorageQuotaBytes.set {
		set["storage_quota_bytes"] = in.StorageQuotaBytes.value
	}
	return set
}

var errAdminUserNotFound = errors.New("пользователь не найден")

func (s *server) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	var in adminUserPatch
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	me := UserFromContext(r.Context())
	// Себя не отключить и не разжаловать: иначе система может остаться без
	// администратора, а вернуть его можно только командой на сервере.
	if id == me.ID && ((in.IsActive != nil && !*in.IsActive) || (in.Role != nil && *in.Role != models.RoleAdmin)) {
		writeError(w, http.StatusConflict, "Нельзя отключить или разжаловать себя")
		return
	}

	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var target models.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errAdminUserNotFound
		}
		if err != nil {
			return err
		}
		if set := in.changes(); len(set) > 0 {
			if err := tx.Model(&models.User{}).Where("id = ?", id).Updates(set).Error; err != nil {
				return err
			}
		}
		if target.IsActive && in.IsActive != nil && !*in.IsActive {
			return deactivate(r.Context(), tx, id)
		}
		return nil
	})
	if errors.Is(err, errAdminUserNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	users, err := s.loadAdminUsers(r.Context(), "u.id = ?", id)
	if err != nil || len(users) != 1 {
		internalError(w, r, fmt.Errorf("пользователь после изменения: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, users[0])
}

// deactivate гасит сессии отключённого и ставит его переводы на паузу:
// открытая вкладка теряет доступ сразу, worker не тратит ресурсы на его
// книги (Review Focus №3). Включение обратно переводы не возобновляет —
// это решает сам пользователь.
func deactivate(ctx context.Context, tx *gorm.DB, userID uuid.UUID) error {
	if err := security.RevokeUserSessions(ctx, tx, userID, ""); err != nil {
		return err
	}
	var active []uuid.UUID
	err := tx.Model(&models.TranslationProject{}).
		Where("owner_id = ? AND status IN ?", userID, []string{models.ProjectQueued, models.ProjectRunning}).
		Pluck("id", &active).Error
	if err != nil {
		return fmt.Errorf("активные переводы: %w", err)
	}
	for _, projectID := range active {
		_, err := queue.Pause(ctx, tx, projectID, userID)
		var bad *queue.TransitionError
		if err != nil && !errors.As(err, &bad) {
			return err
		}
	}
	return nil
}
