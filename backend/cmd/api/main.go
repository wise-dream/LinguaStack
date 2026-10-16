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
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
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

	fileStore, err := storage.New(cfg.StorageRoot, cfg.MaxUploadBytes)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewServer(httpapi.Deps{
			Cfg: cfg, DB: gdb, Store: fileStore, Importers: importers.Default(cfg),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

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

	// Корректное завершение: дать текущим запросам (в том числе SSE) закрыться,
	// а не обрывать их на полуслове при перезапуске сервиса.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	slog.Info("останавливаю сервер")
	return server.Shutdown(shutdownCtx)
}
