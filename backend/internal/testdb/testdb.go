// Package testdb даёт тестам изолированную PostgreSQL-базу.
//
// База физически отдельная (linguastack_test) и собирается теми же
// миграциями goose, что и рабочая, — так каждый прогон тестов заодно
// проверяет миграции. Каждый тест работает в транзакции, которая
// откатывается после него, поэтому тесты не зависят от порядка запуска.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db"
)

const setupTimeout = 30 * time.Second

// pgDuplicateDatabase — SQLSTATE 42P04: базу уже создал параллельный пакет.
const pgDuplicateDatabase = "42P04"

// errUnreachable отличает «база не запущена» от прочих сбоев подготовки:
// подсказка про docker compose уместна только в первом случае.
var errUnreachable = errors.New("PostgreSQL недоступен")

var (
	once     sync.Once
	shared   *gorm.DB
	setupErr error
)

// Shared возвращает общее подключение к тестовой базе без транзакции.
// Нужно тестам, которым транзакция мешает (миграции, конкурентные захваты).
func Shared(t testing.TB) *gorm.DB {
	t.Helper()
	once.Do(func() { shared, setupErr = setup() })
	if setupErr != nil {
		// Именно Fatal, а не Skip: пропуск без базы превратил бы половину
		// сюиты в ложное «ok».
		if errors.Is(setupErr, errUnreachable) {
			t.Fatalf("подготовка тестовой БД: %v\nзапустите: docker compose up -d db", setupErr)
		}
		t.Fatalf("подготовка тестовой БД: %v", setupErr)
	}
	return shared
}

// New возвращает транзакцию, которая откатывается после теста.
func New(t testing.TB) *gorm.DB {
	t.Helper()
	tx := Shared(t).Begin()
	if tx.Error != nil {
		t.Fatalf("начало транзакции: %v", tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func setup() (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureDatabase(ctx, cfg.TestDatabaseURL); err != nil {
		return nil, err
	}
	gdb, err := db.Open(ctx, cfg.TestDatabaseURL, io.Discard)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, gdb); err != nil {
		return nil, err
	}
	return gdb, nil
}

// ensureDatabase создаёт тестовую базу, если её нет.
func ensureDatabase(ctx context.Context, dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("разбор URL тестовой БД: %w", err)
	}
	name := parsed.Path[1:]
	parsed.Path = "/postgres"

	conn, err := pgx.Connect(ctx, parsed.String())
	if err != nil {
		return fmt.Errorf("%w: %w", errUnreachable, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgDuplicateDatabase {
		return nil // базу уже создал параллельный пакет тестов
	}
	return fmt.Errorf("создание тестовой БД: %w", err)
}
