package db

import (
	"context"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/migrations"
)

// Migrate накатывает все миграции из встроенного каталога.
//
// Session locker берёт advisory-lock Postgres на время наката: несколько
// процессов (реплики api, worker, параллельные пакеты тестов) могут
// стартовать одновременно, и мигрировать при этом должен ровно один.
func Migrate(ctx context.Context, gdb *gorm.DB) error {
	sqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("пул соединений: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("блокировка миграций: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS,
		goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("накат миграций: %w", err)
	}
	return nil
}
