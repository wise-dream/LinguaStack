package models

import (
	"time"

	"github.com/google/uuid"
)

// UserSession — серверная сессия. В БД хранится только хеш токена.
type UserSession struct {
	Base
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	TokenHash string    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null"`
	User      User      `gorm:"foreignKey:UserID"`
}

// LoginAttempt — запись журнала попыток входа.
type LoginAttempt struct {
	Base
	Email     string `gorm:"not null"`
	IP        string `gorm:"column:ip;not null"`
	Succeeded bool   `gorm:"not null"`
}

// PasswordReset — одноразовый код сброса пароля; хранится хеш.
type PasswordReset struct {
	TokenHash []byte    `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid"`
	ExpiresAt time.Time
	CreatedAt time.Time
}
