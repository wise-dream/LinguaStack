package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

// BookStatusImported — книга импортирована; проблемы содержимого — в Issues.
const BookStatusImported = "imported"

// Book — исходный документ и его нормализованное представление.
// Оригинал лежит в файловом хранилище и после записи не меняется (ТЗ §4.1).
type Book struct {
	Base
	OwnerID           uuid.UUID `gorm:"type:uuid;not null"`
	Title             string
	Author            string
	Language          string
	SourceFormat      string
	OriginalFilename  string
	OriginalSuffix    string
	OriginalSize      int64
	Status            string
	ChaptersCount     int
	BlocksCount       int
	TranslatableCount int
	CDM               JSON[cdm.Document]      `gorm:"column:cdm"`
	Issues            JSON[[]importers.Issue] `gorm:"column:issues"`
	UpdatedAt         time.Time
}
