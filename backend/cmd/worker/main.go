// Команда worker выполняет фоновые задачи: перевод (ТЗ §7) и экспорт
// (дополнение §16).
//
// Worker-ов может быть несколько: задачи распределяются через аренды в
// PostgreSQL (FOR UPDATE SKIP LOCKED). По SIGTERM текущий батч дописывается,
// задача возвращается в очередь без траты попытки.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db"
	"github.com/vnovels/linguastack/internal/exports"
	"github.com/vnovels/linguastack/internal/logging"
	"github.com/vnovels/linguastack/internal/metrics"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Флагов нет, но разбор нужен: `worker -h` или опечатка во флаге не
	// должны молча запускать ещё один рабочий процесс.
	flag.Parse()
	slog.SetDefault(slog.New(logging.NewRedactingHandler(slog.NewTextHandler(os.Stderr, nil))))
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
	if err := db.Migrate(ctx, gdb); err != nil {
		return err
	}
	secrets, err := security.BoxFromKey(cfg.SecretKey, cfg.RequireSecretKey)
	if err != nil {
		return err
	}

	resolver := queue.ConfigResolver(gdb, secrets, providers.BuildOptions{
		OllamaBaseURL: cfg.OllamaBaseURL, OllamaNumCtx: cfg.OllamaNumCtx,
		Timeout: cfg.ProviderTimeout, AllowPrivateURLs: cfg.AllowPrivateProviderURLs,
		Fake: cfg.E2EFakeProvider, FakeDelay: cfg.E2EFakeDelay,
	})
	if cfg.E2EFakeProvider {
		slog.Warn("включён тестовый провайдер fake (LINGUASTACK_E2E_FAKE_PROVIDER): только для сквозных тестов")
	}
	// Файлы экспорта пишутся в то же хранилище, откуда их отдаёт api.
	store, err := storage.New(cfg.StorageRoot, cfg.MaxUploadBytes)
	if err != nil {
		return err
	}
	runner := exports.NewRunner(gdb, store, cfg)
	exportsDone := make(chan error, 1)
	go func() { exportsDone <- runner.Run(ctx) }()

	worker := queue.NewWorker(workerID(), gdb, resolver, cfg)
	_, stopMetrics, err := serveMetrics(ctx, cfg, worker)
	if err != nil {
		return err
	}
	defer stopMetrics()
	slog.Info("worker запущен", "max_tasks_per_user", cfg.MaxTasksPerUser, "lease", cfg.TaskLease,
		"export_timeout", cfg.ExportTimeout)
	err = worker.Run(ctx)
	if exportErr := <-exportsDone; err == nil {
		err = exportErr
	}
	slog.Info("worker остановлен")
	return err
}

// serveMetrics отдаёт счётчики worker-а на LINGUASTACK_WORKER_METRICS_ADDR
// (дополнение §18) с тем же токеном, что /metrics у api. Адрес занят —
// ошибка запуска, а не молча пропавшие метрики.
func serveMetrics(ctx context.Context, cfg config.Settings, worker *queue.Worker) (string, func(), error) {
	if cfg.WorkerMetricsAddr == "" {
		return "", func() {}, nil
	}
	if cfg.MetricsToken == "" {
		slog.Warn("LINGUASTACK_WORKER_METRICS_ADDR задан без LINGUASTACK_METRICS_TOKEN — метрики worker-а выключены")
		return "", func() {}, nil
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.WorkerMetricsAddr)
	if err != nil {
		return "", nil, fmt.Errorf("адрес метрик worker-а: %w", err)
	}
	reg := metrics.NewRegistry()
	worker.Metrics = metrics.NewWorker(reg)
	srv := &http.Server{Handler: metrics.Handler(reg, cfg.MetricsToken), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("сервер метрик worker-а остановлен", "error", err)
		}
	}()
	slog.Info("метрики worker-а", "addr", ln.Addr().String())
	return ln.Addr().String(), func() { _ = srv.Close() }, nil
}

// workerID — уникальное имя для аренд: хост, процесс и случайный хвост
// (после перезапуска контейнера pid может совпасть).
func workerID() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.NewString()[:8])
}
