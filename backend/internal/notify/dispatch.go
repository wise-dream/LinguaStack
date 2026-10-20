package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/telegram"
)

// Sender отправляет сообщение в Telegram.
type Sender interface {
	SendMessage(ctx context.Context, m telegram.Message) (int64, error)
}

// Dispatcher доставляет уведомления из «исходящих» (дополнение §17):
// склеивает главы одного перевода, соблюдает лимит Telegram (429 —
// повтор через retry_after без траты попытки), повторяет временные сбои
// и не повторяет бесконечно.
type Dispatcher struct {
	db     *gorm.DB
	sender Sender
	base   string

	// PollInterval — пауза, когда доставлять нечего.
	PollInterval time.Duration
	// SendGap — пауза между сообщениями: Telegram принимает ~30 сообщений
	// в секунду на бота.
	SendGap     time.Duration
	BatchSize   int
	MaxAttempts int
	// Backoff — когда повторить после attempts неудачных попыток.
	Backoff func(attempts int) time.Duration
	// Lease — на сколько уведомление занято доставкой: упавший процесс не
	// держит его дольше.
	Lease time.Duration
	// ChapterInterval — не чаще одного сообщения о главах одного перевода:
	// быстрая модель заканчивает главы каждые несколько секунд, и книга из
	// 200 глав дала бы 200 сообщений. Главы за интервал копятся и приходят
	// одним сообщением; «готово» и ошибки интервала не ждут.
	ChapterInterval time.Duration
}

// NewDispatcher создаёт доставщика.
func NewDispatcher(db *gorm.DB, sender Sender, cfg config.Settings) *Dispatcher {
	return &Dispatcher{
		db: db, sender: sender, base: strings.TrimRight(cfg.PublicURL, "/"),
		PollInterval: 2 * time.Second, SendGap: 40 * time.Millisecond, BatchSize: 100, MaxAttempts: 5,
		Backoff: func(attempts int) time.Duration { return 30 * time.Second << max(attempts-1, 0) },
		Lease:   5 * time.Minute, ChapterInterval: 5 * time.Minute,
	}
}

// Run доставляет уведомления до отмены ctx.
func (d *Dispatcher) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		sent, err := d.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("уведомления: сбой доставки", "error", err)
		}
		if sent > 0 && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(d.PollInterval):
		}
	}
	return nil
}

// Время — по часам базы (clock_timestamp: настоящее время и внутри
// транзакции), как у очереди переводов.
const claimSQL = `
UPDATE telegram_notifications SET available_at = clock_timestamp() + make_interval(secs => ?)
 WHERE id IN (SELECT id FROM telegram_notifications
               WHERE status = 'pending' AND available_at <= clock_timestamp()
               ORDER BY created_at, chapter_index
               FOR UPDATE SKIP LOCKED
               LIMIT ?)
RETURNING *`

type projectGroup struct {
	completed, failed, chapters []models.TelegramNotification
}

// RunOnce доставляет готовые к отправке уведомления; возвращает число
// отправленных сообщений.
func (d *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	var claimed []models.TelegramNotification
	if err := d.db.WithContext(ctx).Raw(claimSQL, d.Lease.Seconds(), d.BatchSize).Scan(&claimed).Error; err != nil {
		return 0, fmt.Errorf("выборка уведомлений: %w", err)
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	byUser := map[uuid.UUID]map[uuid.UUID]*projectGroup{}
	var users []uuid.UUID
	for _, n := range claimed {
		if byUser[n.UserID] == nil {
			byUser[n.UserID] = map[uuid.UUID]*projectGroup{}
			users = append(users, n.UserID)
		}
		g := byUser[n.UserID][n.ProjectID]
		if g == nil {
			g = &projectGroup{}
			byUser[n.UserID][n.ProjectID] = g
		}
		switch n.Kind {
		case models.NotifyCompleted:
			g.completed = append(g.completed, n)
		case models.NotifyFailed:
			g.failed = append(g.failed, n)
		default:
			g.chapters = append(g.chapters, n)
		}
	}

	sent := 0
	for _, userID := range users {
		var bnd models.TelegramBinding
		err := d.db.WithContext(ctx).First(&bnd, "user_id = ?", userID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			for _, g := range byUser[userID] {
				d.finish(ctx, all(g), models.NotificationSkipped, "Telegram не привязан")
			}
			continue
		}
		if err != nil {
			return sent, err
		}
		for projectID, g := range byUser[userID] {
			n, stop, err := d.project(ctx, &bnd, projectID, g)
			sent += n
			if err != nil || stop {
				return sent, err
			}
		}
	}
	return sent, nil
}

func all(g *projectGroup) []models.TelegramNotification {
	return append(append(append([]models.TelegramNotification(nil), g.completed...), g.failed...), g.chapters...)
}

// project доставляет уведомления одного перевода. stop — Telegram просит
// подождать (429): дальше в этот раз не отправляем.
func (d *Dispatcher) project(ctx context.Context, bnd *models.TelegramBinding, projectID uuid.UUID, g *projectGroup) (int, bool, error) {
	info := &projectInfo{base: d.base}
	if err := d.db.WithContext(ctx).First(&info.project, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			d.finish(ctx, all(g), models.NotificationSkipped, "перевод удалён")
			return 0, false, nil
		}
		return 0, false, err
	}
	var book models.Book
	query := d.db.WithContext(ctx).Select("id", "title")
	if len(g.chapters) > 0 && len(g.completed) == 0 {
		query = d.db.WithContext(ctx).Select("id", "title", "cdm")
	}
	if err := query.First(&book, "id = ?", info.project.BookID).Error; err != nil {
		return 0, false, err
	}
	info.title = book.Title
	if book.CDM.Data.Chapters != nil {
		info.doc = &book.CDM.Data
	}

	type message struct {
		text    string
		rows    []models.TelegramNotification
		enabled bool
	}
	var messages []message
	if len(g.completed) > 0 {
		// Перевод готов — отдельные главы уже не новость, в том числе
		// отложенные интервалом.
		d.finish(ctx, g.chapters, models.NotificationSkipped, "заменено уведомлением о завершении")
		if err := d.db.WithContext(ctx).Model(&models.TelegramNotification{}).
			Where("project_id = ? AND kind = ? AND status = ?", projectID, models.NotifyChapter, models.NotificationPending).
			Updates(map[string]any{"status": models.NotificationSkipped, "last_error": "заменено уведомлением о завершении"}).Error; err != nil {
			return 0, false, err
		}
		messages = append(messages, message{completedText(info), g.completed, bnd.NotifyCompleted})
	} else if len(g.chapters) > 0 && bnd.NotifyChapters {
		wait, err := d.chapterWait(ctx, projectID)
		if err != nil {
			return 0, false, err
		}
		if wait > 0 {
			d.later(ctx, g.chapters, wait, false, errChapterInterval)
			g.chapters = nil
		}
	}
	if len(g.completed) == 0 && len(g.chapters) > 0 {
		indexes := make([]int, 0, len(g.chapters))
		for _, n := range g.chapters {
			if n.ChapterIndex != nil {
				indexes = append(indexes, *n.ChapterIndex)
			}
		}
		sort.Ints(indexes)
		if len(indexes) > 0 {
			messages = append(messages, message{chaptersText(info, indexes), g.chapters, bnd.NotifyChapters})
		}
	}
	if len(g.failed) > 0 {
		messages = append(messages, message{failedText(info), g.failed, bnd.NotifyErrors})
	}

	sent := 0
	for _, m := range messages {
		if !m.enabled {
			d.finish(ctx, m.rows, models.NotificationSkipped, "уведомления этого вида выключены")
			continue
		}
		if sent > 0 && d.SendGap > 0 {
			time.Sleep(d.SendGap)
		}
		_, err := d.sender.SendMessage(ctx, telegram.Message{ChatID: bnd.ChatID, Text: m.text})
		var apiErr *telegram.APIError
		switch {
		case err == nil:
			d.finish(ctx, m.rows, models.NotificationSent, "")
			sent++
		case errors.As(err, &apiErr) && apiErr.Code == 429:
			d.later(ctx, m.rows, max(apiErr.RetryAfter, time.Second), false, err)
			return sent, true, nil
		case errors.As(err, &apiErr) && !apiErr.Temporary():
			// 403 — бот заблокирован, 400 — чат не найден: повтор не поможет.
			d.finish(ctx, m.rows, models.NotificationSkipped, err.Error())
		default:
			d.later(ctx, m.rows, 0, true, err)
		}
	}
	return sent, false, nil
}

var errChapterInterval = errors.New("ждёт интервала между сообщениями о главах")

// chapterWait — сколько ещё ждать до следующего сообщения о главах
// перевода; время — по часам базы.
func (d *Dispatcher) chapterWait(ctx context.Context, projectID uuid.UUID) (time.Duration, error) {
	var seconds *float64
	err := d.db.WithContext(ctx).Raw(`
SELECT extract(epoch FROM max(sent_at) + make_interval(secs => ?) - clock_timestamp())
  FROM telegram_notifications WHERE project_id = ? AND kind = 'chapter' AND status = 'sent'`,
		d.ChapterInterval.Seconds(), projectID).Scan(&seconds).Error
	if err != nil || seconds == nil || *seconds <= 0 {
		return 0, err
	}
	return time.Duration(*seconds * float64(time.Second)), nil
}

func ids(rows []models.TelegramNotification) []uuid.UUID {
	out := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func (d *Dispatcher) finish(ctx context.Context, rows []models.TelegramNotification, status, reason string) {
	if len(rows) == 0 {
		return
	}
	updates := map[string]any{"status": status, "last_error": reason}
	if status == models.NotificationSent {
		updates["sent_at"] = gorm.Expr("clock_timestamp()") // по часам базы, как интервал глав
	}
	if err := d.db.WithContext(ctx).Model(&models.TelegramNotification{}).Where("id IN ?", ids(rows)).Updates(updates).Error; err != nil {
		slog.Error("уведомления: статус не записан", "error", err)
	}
}

// later откладывает доставку. failure — неудачная попытка (считается);
// лимит Telegram попыткой не считается.
func (d *Dispatcher) later(ctx context.Context, rows []models.TelegramNotification, wait time.Duration, failure bool, cause error) {
	for _, r := range rows {
		attempts := r.Attempts
		if failure {
			attempts++
			if attempts >= d.MaxAttempts {
				slog.Warn("уведомления: доставка не удалась", "notification", r.ID, "attempts", attempts, "error", cause)
				if err := d.db.WithContext(ctx).Model(&r).Updates(map[string]any{"status": models.NotificationFailed,
					"attempts": attempts, "last_error": cause.Error()}).Error; err != nil {
					slog.Error("уведомления: статус не записан", "error", err)
				}
				continue
			}
			wait = d.Backoff(attempts)
		}
		err := d.db.WithContext(ctx).Exec(`
UPDATE telegram_notifications SET attempts = ?, last_error = ?, available_at = clock_timestamp() + make_interval(secs => ?)
 WHERE id = ?`, attempts, cause.Error(), wait.Seconds(), r.ID).Error
		if err != nil {
			slog.Error("уведомления: повтор не записан", "error", err)
		}
	}
}
