package db_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/vnovels/linguastack/internal/db"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/testdb"
)

func TestUserRoundTrip(t *testing.T) {
	tx := testdb.New(t)

	before := time.Now()
	user := models.User{Email: "roundtrip@example.com", PasswordHash: "x", Role: models.RoleUser, IsActive: true}
	require.NoError(t, tx.Create(&user).Error)
	require.NotEqual(t, [16]byte{}, [16]byte(user.ID), "UUID должен назначаться при создании")

	var found models.User
	require.NoError(t, tx.Where("email = ?", "roundtrip@example.com").First(&found).Error)
	require.Equal(t, user.ID, found.ID)
	require.Equal(t, models.RoleUser, found.Role)
	// Важен момент времени, а не зона отображения: time.Time — это инстант.
	require.WithinDuration(t, before, found.CreatedAt, time.Minute)
}

func TestEmailIsUnique(t *testing.T) {
	tx := testdb.New(t)

	require.NoError(t, tx.Create(&models.User{Email: "dup@example.com", PasswordHash: "x", Role: models.RoleUser}).Error)
	err := tx.Create(&models.User{Email: "dup@example.com", PasswordHash: "y", Role: models.RoleUser}).Error
	require.Error(t, err)
	// Обработчики различают «уже есть» и прочие сбои через errors.Is, а не по тексту.
	require.True(t, errors.Is(err, gorm.ErrDuplicatedKey), "ожидался ErrDuplicatedKey, получено: %v", err)
}

func TestTestsAreIsolated(t *testing.T) {
	tx := testdb.New(t)
	var count int64
	require.NoError(t, tx.Model(&models.User{}).Where("email = ?", "roundtrip@example.com").Count(&count).Error)
	require.Zero(t, count, "данные соседнего теста не должны быть видны")
}

func TestRoleIsConstrained(t *testing.T) {
	tx := testdb.New(t)
	err := tx.Create(&models.User{Email: "root@example.com", PasswordHash: "x", Role: "superuser"}).Error
	require.Error(t, err, "в схеме допустимы только роли user и admin (ТЗ §2.1)")
}

func TestMigrationsAreIdempotent(t *testing.T) {
	require.NoError(t, db.Migrate(t.Context(), testdb.Shared(t)))
}

func TestAllTimestampColumnsAreTimestamptz(t *testing.T) {
	// Время без зоны сравнивается с now() в зоне сервера — аренды задач
	// в очереди начали бы истекать в зависимости от настроек машины.
	var offenders []string
	require.NoError(t, testdb.Shared(t).Raw(`
		SELECT table_name || '.' || column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND data_type = 'timestamp without time zone'
		   AND table_name <> 'goose_db_version'`).Scan(&offenders).Error)
	require.Empty(t, offenders)
}

func TestGormLoggerDoesNotPrintParameters(t *testing.T) {
	var buf bytes.Buffer
	tx := testdb.New(t).Session(&gorm.Session{Logger: db.NewLogger(&buf, logger.Info)})

	require.NoError(t, tx.Create(&models.User{
		Email: "logged@example.com", PasswordHash: "secret-hash-value", Role: models.RoleUser,
	}).Error)

	out := buf.String()
	require.Contains(t, out, "INSERT INTO", "SQL должен логироваться")
	// Значения параметров — это пароли, ключи и тексты книг.
	require.NotContains(t, out, "secret-hash-value")
	require.NotContains(t, out, "logged@example.com")
}
