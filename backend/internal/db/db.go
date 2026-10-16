// Package db открывает соединение с PostgreSQL через GORM и накатывает миграции.
package db

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const slowQueryThreshold = 500 * time.Millisecond

// Open подключается к базе и проверяет соединение.
func Open(ctx context.Context, url string, w io.Writer) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		// Ошибки драйвера переводятся в gorm.ErrDuplicatedKey и т.п.:
		// обработчики различают «уже есть» и сбой через errors.Is.
		TranslateError: true,
		Logger:         NewLogger(w, logger.Warn),
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("открытие БД: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("пул соединений: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("БД недоступна: %w", err)
	}
	return gdb, nil
}

// NewLogger возвращает логгер GORM, который НЕ печатает значения параметров.
//
// В параметрах запросов едут хеши паролей, шифртексты ключей и тексты книг.
// ParameterizedQueries оставляет в логе SQL с плейсхолдерами $1, $2 —
// этого хватает для диагностики и не раскрывает данные (ТЗ §13).
func NewLogger(w io.Writer, level logger.LogLevel) logger.Interface {
	return logger.New(log.New(w, "", log.LstdFlags), logger.Config{
		SlowThreshold:             slowQueryThreshold,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
		LogLevel:                  level,
	})
}
