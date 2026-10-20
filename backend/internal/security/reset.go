package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vnovels/linguastack/internal/db/models"
)

// ErrResetInvalid — кода нет, он истёк или уже использован.
var ErrResetInvalid = errors.New("код сброса пароля недействителен")

// bytea — байты как одно значение: голый []byte в Raw внутри скобок GORM
// разворачивает в список параметров, по одному на байт.
type bytea []byte

func (b bytea) Value() (driver.Value, error) { return []byte(b), nil }

func hashReset(token string) bytea {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// IssueReset создаёт код сброса пароля: 32 случайных байта, в БД — хеш.
// Прежний код пользователя гаснет: действует только последняя ссылка.
// Срок считают часы БД, как и у остальных сроков очереди.
func IssueReset(ctx context.Context, db *gorm.DB, userID uuid.UUID, ttl time.Duration) (string, time.Time, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("генерация кода сброса: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	var expires time.Time
	err := db.WithContext(ctx).Raw(`
INSERT INTO password_resets (token_hash, user_id, expires_at)
VALUES (?, ?, now() + make_interval(secs => ?))
ON CONFLICT (user_id) DO UPDATE
   SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, created_at = now()
RETURNING expires_at`, hashReset(token), userID, ttl.Seconds()).Scan(&expires).Error
	if err != nil {
		return "", time.Time{}, fmt.Errorf("запись кода сброса: %w", err)
	}
	return token, expires, nil
}

// ResetIssuedSince — выдавался ли пользователю код позже, чем window назад.
func ResetIssuedSince(ctx context.Context, db *gorm.DB, userID uuid.UUID, window time.Duration) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Model(&models.PasswordReset{}).
		Where("user_id = ? AND expires_at > now() AND created_at > now() - make_interval(secs => ?)", userID, window.Seconds()).
		Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("проверка кода сброса: %w", err)
	}
	return n > 0, nil
}

// ConsumeReset гасит код и возвращает владельца. Проверка срока и
// удаление — один запрос: код не сработает дважды и при гонке.
func ConsumeReset(ctx context.Context, db *gorm.DB, token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, ErrResetInvalid
	}
	db = db.WithContext(ctx)
	if err := db.Where("expires_at <= now()").Delete(&models.PasswordReset{}).Error; err != nil {
		return uuid.Nil, fmt.Errorf("удаление истёкших кодов: %w", err)
	}
	var used []models.PasswordReset
	res := db.Clauses(clause.Returning{}).Where("token_hash = ?", hashReset(token)).Delete(&used)
	if res.Error != nil {
		return uuid.Nil, fmt.Errorf("погашение кода сброса: %w", res.Error)
	}
	if len(used) == 0 {
		return uuid.Nil, ErrResetInvalid
	}
	return used[0].UserID, nil
}
