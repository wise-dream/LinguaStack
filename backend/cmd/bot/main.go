// Команда bot — Telegram-бот LinguaStack (ТЗ §9; дополнение §17): команды
// пользователей (long polling) и доставка уведомлений. Один процесс на
// развёртывание: Telegram отдаёт обновления только одному опрашивающему.
// Без LINGUASTACK_TELEGRAM_BOT_TOKEN бот выключен и ждёт сигнала
// остановки.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/vnovels/linguastack/internal/bot"
	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db"
	"github.com/vnovels/linguastack/internal/logging"
	"github.com/vnovels/linguastack/internal/notify"
	"github.com/vnovels/linguastack/internal/telegram"
)

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Stderr); err != nil {
		slog.Error("бот остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logOut io.Writer) error {
	// Фильтр секретов: токен стоит в адресах запросов к Bot API.
	slog.SetDefault(slog.New(logging.NewRedactingHandler(slog.NewTextHandler(logOut, nil))))
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.TelegramBotToken == "" {
		slog.Info("Telegram-бот выключен: LINGUASTACK_TELEGRAM_BOT_TOKEN не задан")
		<-ctx.Done()
		return nil
	}

	gdb, err := db.Open(ctx, cfg.DatabaseURL, io.Discard)
	if err != nil {
		return err
	}
	if err := db.Migrate(ctx, gdb); err != nil {
		return err
	}
	tg := telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil)
	me, err := tg.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("токен бота не принят Telegram: %w", err)
	}
	switch {
	case cfg.TelegramBotUsername == "":
		slog.Warn("LINGUASTACK_TELEGRAM_BOT_USERNAME не задан: веб-приложение не выдаст ссылку привязки", "bot", me.Username)
	case !strings.EqualFold(strings.TrimPrefix(cfg.TelegramBotUsername, "@"), me.Username):
		slog.Warn("LINGUASTACK_TELEGRAM_BOT_USERNAME не совпадает с именем бота: ссылки привязки поведут не туда",
			"configured", cfg.TelegramBotUsername, "bot", me.Username)
	}
	if err := tg.SetCommands(ctx, bot.Commands); err != nil {
		slog.Warn("меню команд не задано", "error", err)
	}
	if cfg.PublicURL == "" {
		slog.Warn("LINGUASTACK_PUBLIC_URL не задан: в сообщениях не будет ссылок на сайт")
	}

	b := bot.New(gdb, tg, cfg)
	dispatcher := notify.NewDispatcher(gdb, tg, cfg)
	slog.Info("бот запущен", "bot", me.Username)
	done := make(chan error, 1)
	go func() { done <- dispatcher.Run(ctx) }()
	err = b.Run(ctx)
	if derr := <-done; err == nil {
		err = derr
	}
	slog.Info("бот остановлен")
	return err
}
