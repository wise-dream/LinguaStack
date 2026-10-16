package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/vnovels/linguastack/internal/translation"
)

// Статусы проекта перевода.
const (
	ProjectDraft                 = "draft"
	ProjectQueued                = "queued"
	ProjectRunning               = "running"
	ProjectPaused                = "paused"
	ProjectCompleted             = "completed"
	ProjectCompletedWithWarnings = "completed_with_warnings"
	ProjectFailed                = "failed"
	ProjectCancelled             = "cancelled"
)

// TranslationProject — перевод одной книги на один язык (ТЗ §5.1). У книги
// может быть несколько проектов: en→ru и en→de.
type TranslationProject struct {
	Base
	OwnerID          uuid.UUID `gorm:"type:uuid;not null"`
	BookID           uuid.UUID `gorm:"type:uuid;not null"`
	ProviderConfigID uuid.UUID `gorm:"type:uuid;not null"`
	SourceLang       string
	TargetLang       string
	ModelID          string
	Style            string
	Determinism      string
	Glossary         JSON[map[string]string]
	Status           string
	SegmentsTotal    int
	SegmentsDone     int
	SegmentsFailed   int
	WarningsCount    int
	CostUSD          float64 `gorm:"column:cost_usd"`
	LastError        string
	UpdatedAt        time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

// IsTerminal — работа над проектом закончена и сама не продолжится.
func (p *TranslationProject) IsTerminal() bool {
	switch p.Status {
	case ProjectCompleted, ProjectCompletedWithWarnings, ProjectFailed, ProjectCancelled:
		return true
	}
	return false
}

// ProgressPercent — доля обработанных сегментов (переведённых и исчерпавших
// попытки). Книга без переводимых блоков — это 100%, а не деление на ноль
// (Review Focus №1).
func (p *TranslationProject) ProgressPercent() float64 {
	if p.SegmentsTotal == 0 {
		return 100
	}
	return float64(p.SegmentsDone+p.SegmentsFailed) * 100 / float64(p.SegmentsTotal)
}

// Статусы сегмента.
const (
	SegmentPending   = "pending"
	SegmentRunning   = "running"
	SegmentCompleted = "completed"
	SegmentFailed    = "failed"
	SegmentStale     = "stale" // исходник изменился после перевода
)

// TranslationSegment — исходный текст блока, перевод и итог проверки (ТЗ §11).
type TranslationSegment struct {
	Base
	ProjectID      uuid.UUID `gorm:"type:uuid;not null"`
	NodeID         string
	ChapterIndex   int
	Ordinal        int
	BlockType      string
	Level          int
	SourceText     string
	SourceHash     string
	TranslatedText *string
	Status         string
	Severity       string
	Findings       JSON[[]translation.Finding]
	Attempts       int
	LastError      string
	UpdatedAt      time.Time
}

// Происхождение версии перевода.
const (
	OriginMachine = "machine"
	OriginManual  = "manual"
	OriginRetry   = "retry"
)

// TranslationSegmentRevision — версия перевода сегмента (ТЗ §5.1).
type TranslationSegmentRevision struct {
	Base
	SegmentID      uuid.UUID `gorm:"type:uuid;not null"`
	TranslatedText string
	Origin         string
}

// Статусы задачи — ровно ТЗ §7.
const (
	TaskPending   = "pending"
	TaskRunning   = "running"
	TaskCompleted = "completed"
	TaskFailed    = "failed"
	TaskCancelled = "cancelled"
)

// TaskStatuses — все статусы задачи.
var TaskStatuses = []string{TaskPending, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled}

// TranslationTask — единица работы очереди (ТЗ §11, дополнение §7).
type TranslationTask struct {
	Base
	ProjectID   uuid.UUID `gorm:"type:uuid;not null"`
	OwnerID     uuid.UUID `gorm:"type:uuid;not null"`
	Kind        string    `gorm:"default:translate"`
	Status      string
	Priority    int
	Attempts    int
	MaxAttempts int
	LockedBy    *string
	LockedUntil *time.Time
	AvailableAt time.Time `gorm:"default:now()"`
	StartedAt   *time.Time
	FinishedAt  *time.Time
	LastError   string
}
