package metrics

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// Snapshot — счётчики данных и очереди: для /metrics и обзора в
// админ-панели.
type Snapshot struct {
	Users         int64            `json:"users"`
	Books         int64            `json:"books"`
	Projects      map[string]int64 `json:"projects"`
	Tasks         map[string]int64 `json:"tasks"`
	Exports       map[string]int64 `json:"exports"`
	Notifications map[string]int64 `json:"notifications"`
	CostUSD       float64          `json:"cost_usd"`
	InputTokens   int64            `json:"input_tokens"`
	OutputTokens  int64            `json:"output_tokens"`
}

// Известные статусы — с нулём, даже когда строк нет: иначе ряд метрики
// пропадает, а панель путает «нет данных» с «ноль».
var (
	projectStatuses = []string{models.ProjectDraft, models.ProjectQueued, models.ProjectRunning, models.ProjectPaused,
		models.ProjectCompleted, models.ProjectCompletedWithWarnings, models.ProjectFailed, models.ProjectCancelled}
	taskStatuses         = []string{models.TaskPending, models.TaskRunning, models.TaskCompleted, models.TaskFailed, models.TaskCancelled}
	exportStatuses       = []string{models.ExportPending, models.ExportRunning, models.ExportCompleted, models.ExportFailed}
	notificationStatuses = []string{models.NotificationPending, models.NotificationSent, models.NotificationSkipped, models.NotificationFailed}
)

// TakeSnapshot считает строки дешёвыми count(*) … GROUP BY status. Таблица
// сегментов не трогается: она на порядки больше остальных (Review Focus №5).
func TakeSnapshot(ctx context.Context, db *gorm.DB) (Snapshot, error) {
	db = db.WithContext(ctx)
	s := Snapshot{}
	if err := db.Model(&models.User{}).Count(&s.Users).Error; err != nil {
		return s, fmt.Errorf("счётчик пользователей: %w", err)
	}
	if err := db.Model(&models.Book{}).Count(&s.Books).Error; err != nil {
		return s, fmt.Errorf("счётчик книг: %w", err)
	}
	var err error
	if s.Projects, err = byStatus(db, "translation_projects", projectStatuses); err != nil {
		return s, err
	}
	if s.Tasks, err = byStatus(db, "translation_tasks", taskStatuses); err != nil {
		return s, err
	}
	if s.Exports, err = byStatus(db, "export_jobs", exportStatuses); err != nil {
		return s, err
	}
	if s.Notifications, err = byStatus(db, "telegram_notifications", notificationStatuses); err != nil {
		return s, err
	}
	var usage struct {
		Cost   float64
		Input  int64
		Output int64
	}
	err = db.Raw(`SELECT coalesce(sum(cost_usd), 0) AS cost, coalesce(sum(input_tokens), 0) AS input,
	                     coalesce(sum(output_tokens), 0) AS output FROM translation_projects`).Scan(&usage).Error
	if err != nil {
		return s, fmt.Errorf("расход провайдеров: %w", err)
	}
	s.CostUSD, s.InputTokens, s.OutputTokens = usage.Cost, usage.Input, usage.Output
	return s, nil
}

// byStatus — число строк таблицы по статусу; table — константа из кода.
func byStatus(db *gorm.DB, table string, known []string) (map[string]int64, error) {
	var rows []struct {
		Status string
		N      int64
	}
	if err := db.Table(table).Select("status, count(*) AS n").Group("status").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("счётчики %s: %w", table, err)
	}
	out := make(map[string]int64, len(known))
	for _, k := range known {
		out[k] = 0
	}
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, nil
}
