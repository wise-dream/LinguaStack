package testdb

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
)

// Фабрики тестовых данных — общие для пакетов БД, очереди, worker-а и API.

var seq atomic.Int64

func unique(prefix string) string { return fmt.Sprintf("%s-%d", prefix, seq.Add(1)) }

// MakeUser создаёт активного пользователя.
func MakeUser(t testing.TB, tx *gorm.DB) *models.User {
	t.Helper()
	u := models.User{Email: unique("user") + "@example.com", PasswordHash: "x", Role: models.RoleUser, IsActive: true}
	if err := tx.Create(&u).Error; err != nil {
		t.Fatalf("пользователь: %v", err)
	}
	return &u
}

// MakeBook создаёт книгу с документом doc (по умолчанию — одна глава, два абзаца).
func MakeBook(t testing.TB, tx *gorm.DB, owner uuid.UUID, doc *cdm.Document) *models.Book {
	t.Helper()
	if doc == nil {
		b := cdm.NewBuilder(cdm.BookMeta{Title: "Книга", SourceFormat: "markdown"})
		b.StartChapter("Глава", 1)
		b.Add(cdm.Paragraph, "One.", 0, nil)
		b.Add(cdm.Paragraph, "Two.", 0, nil)
		built := b.Build()
		doc = &built
	}
	book := models.Book{
		OwnerID: owner, Title: doc.Meta.Title, SourceFormat: doc.Meta.SourceFormat,
		OriginalFilename: "book.md", OriginalSuffix: ".md", Status: models.BookStatusImported,
		BlocksCount: len(doc.Blocks()), TranslatableCount: len(doc.TranslatableBlocks()),
		CDM: models.NewJSON(*doc),
	}
	if err := tx.Create(&book).Error; err != nil {
		t.Fatalf("книга: %v", err)
	}
	return &book
}

// MakeProviderConfig создаёт конфигурацию провайдера вида kind.
func MakeProviderConfig(t testing.TB, tx *gorm.DB, owner uuid.UUID, kind string) *models.ProviderConfig {
	t.Helper()
	cfg := models.ProviderConfig{OwnerID: owner, ProviderID: kind}
	if err := tx.Create(&cfg).Error; err != nil {
		t.Fatalf("конфигурация провайдера: %v", err)
	}
	return &cfg
}

// ProjectOptions — необязательные параметры MakeProject.
type ProjectOptions struct {
	Owner          *uuid.UUID
	Book           *uuid.UUID
	ProviderConfig *uuid.UUID
	Document       *cdm.Document
	TargetLang     string
	Status         string
}

// MakeProject создаёт проект перевода, по необходимости — с владельцем,
// книгой и конфигурацией провайдера.
func MakeProject(t testing.TB, tx *gorm.DB, opts *ProjectOptions) *models.TranslationProject {
	t.Helper()
	if opts == nil {
		opts = &ProjectOptions{}
	}
	owner := opts.Owner
	if owner == nil {
		owner = &MakeUser(t, tx).ID
	}
	book := opts.Book
	if book == nil {
		book = &MakeBook(t, tx, *owner, opts.Document).ID
	}
	cfg := opts.ProviderConfig
	if cfg == nil {
		// Тестовый вид: у Ollama сервера своя очередь (одна задача за раз),
		// тесты, которым она нужна, создают подключение сами.
		cfg = &MakeProviderConfig(t, tx, *owner, "fake").ID
	}
	target, status := opts.TargetLang, opts.Status
	if target == "" {
		target = "ru"
	}
	if status == "" {
		status = models.ProjectQueued
	}
	p := models.TranslationProject{
		OwnerID: *owner, BookID: *book, ProviderConfigID: *cfg, SourceLang: "en", TargetLang: target,
		ModelID: "fake-1", Style: "neutral", Determinism: "strict", Status: status,
	}
	if err := tx.Create(&p).Error; err != nil {
		t.Fatalf("проект: %v", err)
	}
	return &p
}
