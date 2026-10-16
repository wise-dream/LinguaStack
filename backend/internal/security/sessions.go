package security

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// SessionCookie — имя cookie с токеном сессии.
const SessionCookie = "linguastack_session"

const tokenBytes = 32

// ErrNoSession — сессии нет, она истекла или аккаунт отключён.
// Причины не различаются: вызывающему важно только «не аутентифицирован».
var ErrNoSession = errors.New("сессия не найдена")

// HashToken — то, что хранится в БД вместо токена.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssueSession создаёт сессию и возвращает открытый токен.
func IssueSession(ctx context.Context, db *gorm.DB, userID uuid.UUID, ttl time.Duration) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("генерация токена: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	session := models.UserSession{
		UserID:    userID,
		TokenHash: HashToken(token),
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	if err := db.WithContext(ctx).Create(&session).Error; err != nil {
		return "", fmt.Errorf("запись сессии: %w", err)
	}
	return token, nil
}

// LookupSessionUser возвращает владельца действующей сессии.
func LookupSessionUser(ctx context.Context, db *gorm.DB, token string) (*models.User, error) {
	if token == "" {
		return nil, ErrNoSession
	}
	var session models.UserSession
	err := db.WithContext(ctx).
		Joins("User").
		Where("user_sessions.token_hash = ? AND user_sessions.expires_at > ?", HashToken(token), time.Now().UTC()).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("поиск сессии: %w", err)
	}
	// Отключённый аккаунт не проходит и по ранее выданной сессии.
	if !session.User.IsActive {
		return nil, ErrNoSession
	}
	return &session.User, nil
}

// RevokeSession удаляет сессию. Отсутствие сессии ошибкой не считается.
func RevokeSession(ctx context.Context, db *gorm.DB, token string) error {
	err := db.WithContext(ctx).Where("token_hash = ?", HashToken(token)).Delete(&models.UserSession{}).Error
	if err != nil {
		return fmt.Errorf("отзыв сессии: %w", err)
	}
	return nil
}
