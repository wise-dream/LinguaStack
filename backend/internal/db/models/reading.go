package models

import (
	"time"

	"github.com/google/uuid"
)

// Режимы читалки (ТЗ §8.3): оригинал и перевод рядом или одна версия.
const (
	ReadParallel    = "parallel"
	ReadOriginal    = "original"
	ReadTranslation = "translation"
)

// ReadingPosition — где пользователь остановился в книге. Хранится на
// сервере, чтобы продолжить чтение с другого устройства.
type ReadingPosition struct {
	UserID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	BookID        uuid.UUID  `gorm:"type:uuid;primaryKey"`
	TranslationID *uuid.UUID `gorm:"type:uuid"`
	ChapterIndex  int
	NodeID        string
	Mode          string
	UpdatedAt     time.Time
}
