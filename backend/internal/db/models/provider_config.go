package models

import (
	"time"

	"github.com/google/uuid"
)

// ProviderConfig — настройка провайдера пользователя и ссылка на его
// зашифрованный ключ (ТЗ §11). Ключ наружу не отдаётся: схемы ответов его
// не содержат, а тег json:"-" страхует от случайной сериализации модели.
type ProviderConfig struct {
	Base
	OwnerID          uuid.UUID `gorm:"type:uuid;not null"`
	ProviderID       string    `gorm:"not null"`
	Name             string
	BaseURL          string
	SecretCiphertext string `json:"-"`
	KeyFingerprint   string
	KeyLast4         string
	AllowExternal    bool
	UpdatedAt        time.Time
}
