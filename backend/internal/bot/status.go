package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/store"
	"github.com/vnovels/linguastack/internal/telegram"
)

const statusLimit = 10

var statusLabels = map[string]string{
	models.ProjectDraft: "Черновик", models.ProjectQueued: "В очереди", models.ProjectRunning: "Переводится",
	models.ProjectPaused: "Приостановлен", models.ProjectCompleted: "Готово",
	models.ProjectCompletedWithWarnings: "Готово, есть замечания", models.ProjectFailed: "Ошибка",
	models.ProjectCancelled: "Отменён",
}

func (b *Bot) status(ctx context.Context, m *telegram.IncomingMessage) error {
	bnd, err := b.binding(ctx, m.From.ID)
	if err != nil {
		return err
	}
	if bnd == nil {
		return b.reply(ctx, m.Chat.ID, "Telegram не привязан к аккаунту LinguaStack. Чтобы видеть переводы, "+b.settingsHint(), nil)
	}
	text, keyboard, err := b.statusView(ctx, bnd)
	if err != nil {
		return err
	}
	return b.reply(ctx, m.Chat.ID, text, keyboard)
}

// statusView — список переводов: идущие и ждущие решения первыми, затем
// недавние; кнопки паузы и продолжения — по номерам из списка.
func (b *Bot) statusView(ctx context.Context, bnd *models.TelegramBinding) (string, [][]telegram.Button, error) {
	var projects []models.TranslationProject
	err := b.db.WithContext(ctx).Where("owner_id = ?", bnd.UserID).
		Order("CASE WHEN status IN ('running', 'queued', 'paused', 'failed') THEN 0 ELSE 1 END, updated_at DESC, id").
		Limit(statusLimit + 1).Find(&projects).Error
	if err != nil {
		return "", nil, err
	}
	if len(projects) == 0 {
		return "Переводов пока нет. Начать перевод можно на сайте.", nil, nil
	}
	var total int64
	if err := b.db.WithContext(ctx).Model(&models.TranslationProject{}).Where("owner_id = ?", bnd.UserID).Count(&total).Error; err != nil {
		return "", nil, err
	}
	if len(projects) > statusLimit {
		projects = projects[:statusLimit]
	}
	titles, err := b.bookTitles(ctx, projects)
	if err != nil {
		return "", nil, err
	}

	var text strings.Builder
	text.WriteString("<b>Ваши переводы</b>\n")
	var keyboard [][]telegram.Button
	for i, p := range projects {
		n := strconv.Itoa(i + 1)
		fmt.Fprintf(&text, "\n%s. <b>%s</b> → %s\n%s", n, html.EscapeString(titles[p.BookID]),
			html.EscapeString(telegram.LanguageName(p.TargetLang)), progressLine(&p))
		if p.Status == models.ProjectFailed && p.LastError != "" {
			text.WriteString("\n" + html.EscapeString(clip(p.LastError, 200)))
		}
		if url := b.page("/translations/" + p.ID.String()); url != "" {
			text.WriteString("\n" + telegram.Link(url, "Открыть"))
		}
		text.WriteString("\n")
		switch p.Status {
		case models.ProjectQueued, models.ProjectRunning:
			keyboard = append(keyboard, []telegram.Button{{Text: "Приостановить " + n, CallbackData: "p:" + p.ID.String()}})
		case models.ProjectPaused, models.ProjectFailed:
			keyboard = append(keyboard, []telegram.Button{{Text: "Продолжить " + n, CallbackData: "r:" + p.ID.String()}})
		}
	}
	if rest := int(total) - len(projects); rest > 0 {
		fmt.Fprintf(&text, "\n…и ещё %d — на сайте.", rest)
	}
	return text.String(), keyboard, nil
}

func (b *Bot) bookTitles(ctx context.Context, projects []models.TranslationProject) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		ids[i] = p.BookID
	}
	var books []models.Book
	if err := b.db.WithContext(ctx).Select("id", "title").Where("id IN ?", ids).Find(&books).Error; err != nil {
		return nil, err
	}
	titles := make(map[uuid.UUID]string, len(books))
	for _, bk := range books {
		titles[bk.ID] = bk.Title
	}
	return titles, nil
}

// progressLine — «Переводится: 45% (390 из 873)».
func progressLine(p *models.TranslationProject) string {
	label := statusLabels[p.Status]
	if label == "" {
		label = p.Status
	}
	if p.SegmentsTotal == 0 {
		return label
	}
	return fmt.Sprintf("%s: %d%% (%d из %d)", label, int(math.Round(p.ProgressPercent())), p.SegmentsDone, p.SegmentsTotal)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// callback — нажатие «Приостановить» или «Продолжить». Данные кнопки
// приходят от клиента и могут быть подделаны: владелец проверяется
// заново (queue.Pause/Resume — тот же код, что у HTTP API).
func (b *Bot) callback(ctx context.Context, q *telegram.CallbackQuery) error {
	answer := func(text string) error { return b.tg.AnswerCallback(ctx, q.ID, text) }
	if q.Message == nil || q.Message.Chat.Type != "private" {
		return answer("Только в личном чате")
	}
	bnd, err := b.binding(ctx, q.From.ID)
	if err != nil {
		return err
	}
	if bnd == nil {
		return answer("Telegram не привязан")
	}
	action, rawID, _ := strings.Cut(q.Data, ":")
	id, perr := uuid.Parse(rawID)
	var done string
	switch {
	case perr != nil:
		return answer("Перевод не найден")
	case action == "p":
		_, err = queue.Pause(ctx, b.db, id, bnd.UserID)
		done = "Перевод приостановлен"
	case action == "r":
		_, err = queue.Resume(ctx, b.db, id, bnd.UserID, b.cfg.MaxSegmentAttempts)
		done = "Перевод продолжается"
	default:
		return answer("Перевод не найден")
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return answer("Перевод не найден")
	case errors.Is(err, queue.ErrBadTransition):
		return answer("Сейчас это действие недоступно")
	case err != nil:
		return err
	}
	if err := answer(done); err != nil {
		return err
	}
	text, keyboard, err := b.statusView(ctx, bnd)
	if err != nil {
		return err
	}
	// Не обновилось (то же содержимое, сообщение старое) — не беда:
	// ответ на нажатие уже показан.
	_ = b.tg.EditMessage(ctx, q.Message.Chat.ID, q.Message.MessageID, text, keyboard)
	return nil
}
