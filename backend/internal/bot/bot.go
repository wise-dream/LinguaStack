// Package bot — Telegram-бот LinguaStack (ТЗ §9; дополнение §17):
// привязка аккаунта, /status с паузой и возобновлением кнопками, /help,
// /unlink. Только личные чаты: в группе ход переводов увидели бы другие.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/telegram"
)

// Bot обрабатывает обновления Telegram.
type Bot struct {
	db  *gorm.DB
	tg  *telegram.Client
	cfg config.Settings

	// PollTimeout — сколько Telegram держит запрос getUpdates без событий.
	PollTimeout time.Duration
	// RetryDelay — пауза после сбоя getUpdates (кроме 429 с retry_after).
	RetryDelay time.Duration
}

// New создаёт бота.
func New(db *gorm.DB, tg *telegram.Client, cfg config.Settings) *Bot {
	return &Bot{db: db, tg: tg, cfg: cfg, PollTimeout: 30 * time.Second, RetryDelay: 5 * time.Second}
}

// Commands — меню команд бота.
var Commands = []telegram.Command{
	{Command: "status", Description: "Мои переводы и их ход"},
	{Command: "help", Description: "Что умеет бот"},
	{Command: "unlink", Description: "Отвязать Telegram от аккаунта"},
}

// Run опрашивает Telegram до отмены ctx. Обновление подтверждается
// следующим запросом (offset): упавший процесс получит необработанные
// события снова.
func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset, b.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			delay := b.RetryDelay
			var apiErr *telegram.APIError
			if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
				delay = apiErr.RetryAfter
			}
			slog.Warn("бот: getUpdates не удался", "error", err, "retry_in", delay)
			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
			continue
		}
		for _, u := range updates {
			if err := b.HandleUpdate(ctx, u); err != nil && ctx.Err() == nil {
				slog.Error("бот: обновление не обработано", "update", u.UpdateID, "error", err)
			}
			offset = u.UpdateID + 1
		}
	}
	return nil
}

// HandleUpdate обрабатывает одно обновление.
func (b *Bot) HandleUpdate(ctx context.Context, u telegram.Update) error {
	switch {
	case u.Message != nil:
		return b.message(ctx, u.Message)
	case u.Callback != nil:
		return b.callback(ctx, u.Callback)
	}
	return nil
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string, keyboard [][]telegram.Button) error {
	_, err := b.tg.SendMessage(ctx, telegram.Message{ChatID: chatID, Text: text, Keyboard: keyboard})
	return err
}

func (b *Bot) message(ctx context.Context, m *telegram.IncomingMessage) error {
	if m.Chat.Type != "private" {
		return b.reply(ctx, m.Chat.ID, "Бот работает только в личном чате: в группе ход ваших переводов увидели бы другие.", nil)
	}
	command, arg := parseCommand(m.Text)
	switch command {
	case "/start":
		if arg != "" {
			return b.link(ctx, m, arg)
		}
		return b.start(ctx, m)
	case "/status":
		return b.status(ctx, m)
	case "/help":
		return b.reply(ctx, m.Chat.ID, b.helpText(), nil)
	case "/unlink":
		return b.unlink(ctx, m)
	}
	return b.reply(ctx, m.Chat.ID, "Не понимаю. /help — что умеет бот.", nil)
}

// parseCommand: «/start@linguastack_bot код» → «/start», «код».
func parseCommand(text string) (command, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	command, arg, _ = strings.Cut(text, " ")
	command, _, _ = strings.Cut(command, "@")
	return strings.ToLower(command), strings.TrimSpace(arg)
}

// page — ссылка на страницу веб-приложения; без LINGUASTACK_PUBLIC_URL
// ссылок нет.
func (b *Bot) page(path string) string {
	if b.cfg.PublicURL == "" {
		return ""
	}
	return strings.TrimRight(b.cfg.PublicURL, "/") + path
}

// settingsHint — как привязать аккаунт, со ссылкой на настройки.
func (b *Bot) settingsHint() string {
	hint := "на сайте LinguaStack откройте «Настройки» и нажмите «Привязать Telegram»"
	if url := b.page("/settings"); url != "" {
		hint += ": " + telegram.Link(url, "открыть настройки")
	}
	return hint + "."
}

func (b *Bot) helpText() string {
	return "LinguaStack присылает сюда уведомления о переводах книг: глава готова, книга переведена, перевод остановился из-за ошибки.\n\n" +
		"/status — мои переводы и их ход, пауза и продолжение\n" +
		"/unlink — отвязать Telegram от аккаунта\n" +
		"/help — эта справка\n\n" +
		"Привязать аккаунт: " + b.settingsHint()
}
