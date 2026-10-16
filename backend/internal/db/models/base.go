// Package models описывает сущности ТЗ §11 для GORM.
//
// Схему задают миграции в backend/migrations, а не эти структуры:
// теги gorm здесь описывают отображение, а не DDL.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base — общие поля всех сущностей.
type Base struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

// BeforeCreate назначает UUID на стороне приложения: идентификатор известен
// до вставки, и его можно сразу использовать, например, в пути к файлу.
func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}
