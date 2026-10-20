// Команда api запускает HTTP-сервер LinguaStack.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/logging"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/storage"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("сервер остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run() error {
	migrateOnly := flag.Bool("migrate", false, "накатить миграции и выйти")
	healthcheck := flag.Bool("healthcheck", false, "проверить, что запущенный сервер отвечает, и выйти")
	roleSpec := flag.String("set-role", "", "назначить роль пользователю (адрес=admin или адрес=user) и выйти")
	flag.Parse()
	// Фильтр секретов на всех логах: ключ мог оказаться в тексте ошибки
	// провайдера (ТЗ §13).
	slog.SetDefault(slog.New(logging.NewRedactingHandler(slog.NewTextHandler(os.Stderr, nil))))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *healthcheck {
		url, err := healthURL(cfg.HTTPAddr)
		if err != nil {
			return err
		}
		return probeHealth(context.Background(), url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gdb, err := db.Open(ctx, cfg.DatabaseURL, os.Stderr)
	if err != nil {
		return err
	}
	// Миграции накатываются и при обычном старте: забытый накат означает
	// неработающий сервис, а advisory-lock goose делает одновременный старт
	// нескольких процессов безопасным.
	if err := db.Migrate(ctx, gdb); err != nil {
		return err
	}
	if *migrateOnly {
		slog.Info("миграции накатаны")
		return nil
	}
	if *roleSpec != "" {
		return setRole(ctx, gdb, *roleSpec)
	}

	if err := httpapi.CheckWebRoot(cfg.WebRoot); err != nil {
		return err
	}
	if cfg.E2EFakeProvider {
		slog.Warn("включён тестовый провайдер fake (LINGUASTACK_E2E_FAKE_PROVIDER): только для сквозных тестов")
	}
	fileStore, err := storage.New(cfg.StorageRoot, cfg.MaxUploadBytes)
	if err != nil {
		return err
	}
	secrets, err := security.BoxFromKey(cfg.SecretKey, cfg.RequireSecretKey)
	if err != nil {
		return err
	}
	if secrets == nil {
		slog.Warn("LINGUASTACK_SECRET_KEY не задан: конфигурации провайдеров с ключами API недоступны")
	}

	stopping, stopStreams := context.WithCancel(context.Background())
	defer stopStreams()
	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewServer(httpapi.Deps{
			Cfg: cfg, DB: gdb, Store: fileStore, Importers: importers.Default(cfg), Secrets: secrets,
			Stopping: stopping,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Shutdown ждёт завершения запросов, но потоки событий сами не
	// кончаются — их надо закрыть в начале остановки.
	server.RegisterOnShutdown(stopStreams)

	errs := make(chan error, 1)
	go func() {
		slog.Info("сервер запущен", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
		close(errs)
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	// Корректное завершение: дать текущим запросам закрыться, а не обрывать
	// их на полуслове при перезапуске сервиса; потоки SSE закрываются сразу.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	slog.Info("останавливаю сервер")
	return server.Shutdown(shutdownCtx)
}
