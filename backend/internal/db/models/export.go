package models

import (
	"time"

	"github.com/google/uuid"
)

// Статусы задачи экспорта.
const (
	ExportPending   = "pending"
	ExportRunning   = "running"
	ExportCompleted = "completed"
	ExportFailed    = "failed"
)

// ExportJob — задача экспорта перевода в файл (ТЗ §11).
type ExportJob struct {
	Base
	OwnerID      uuid.UUID `gorm:"type:uuid;not null"`
	BookID       uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID    uuid.UUID `gorm:"type:uuid;not null"`
	Format       string
	Mode         string
	Status       string
	BaseName     string
	FileExt      string
	ContentType  string
	SizeBytes    int64
	Untranslated int
	Error        string
	LockedUntil  *time.Time
	UpdatedAt    time.Time
	FinishedAt   *time.Time
}

// IsActive — задача ещё выполняется или ждёт.
func (j *ExportJob) IsActive() bool { return j.Status == ExportPending || j.Status == ExportRunning }
