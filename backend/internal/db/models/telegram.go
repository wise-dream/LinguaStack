package models

import (
	"time"

	"github.com/google/uuid"
)

// TelegramBinding — привязка Telegram к аккаунту (ТЗ §11) и настройки
// уведомлений. Один Telegram — один аккаунт и наоборот.
type TelegramBinding struct {
	UserID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	TelegramUserID  int64
	ChatID          int64
	Username        string
	NotifyChapters  bool
	NotifyCompleted bool
	NotifyErrors    bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TelegramLinkToken — одноразовый код привязки; хранится хеш.
type TelegramLinkToken struct {
	TokenHash []byte    `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Виды и статусы уведомлений.
const (
	NotifyChapter   = "chapter"
	NotifyCompleted = "completed"
	NotifyFailed    = "failed"

	NotificationPending = "pending"
	NotificationSent    = "sent"
	NotificationSkipped = "skipped"
	NotificationFailed  = "failed"
)

// TelegramNotification — уведомление в «исходящих» (дополнение §17).
type TelegramNotification struct {
	Base
	UserID       uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID    uuid.UUID `gorm:"type:uuid;not null"`
	Kind         string
	ChapterIndex *int
	DedupeKey    string
	Status       string
	Attempts     int
	AvailableAt  time.Time
	LastError    string
	SentAt       *time.Time
}
