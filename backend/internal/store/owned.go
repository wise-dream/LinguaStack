// Package store — доступ к пользовательским ресурсам с проверкой владения.
//
// Owned — единственная точка проверки прав (ТЗ §2.2, дополнение §9).
// Проверка не зависит от дисциплины в каждом обработчике: обработчик не
// может получить ресурс иначе, чем через неё.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

var (
	// ErrNotFound — ресурса нет или он чужой. Случаи намеренно не
	// различаются: отдельный ответ «запрещено» подтвердил бы существование
	// чужого ресурса. HTTP-слой превращает это в 404.
	ErrNotFound = errors.New("не найдено")

	// ErrNoOwnerColumn — у модели нет owner_id. Это ошибка конфигурации:
	// она не должна выглядеть как «не найдено», иначе её замаскирует 404.
	ErrNoOwnerColumn = errors.New("у модели нет колонки owner_id")
)

const ownerColumn = "owner_id"

// Owned загружает ресурс, только если он принадлежит ownerID.
//
// Принимается идентификатор владельца, а не пользователь: роли сюда
// попасть неоткуда, поэтому admin не обходит проверку (ТЗ §2.1) по
// построению, а не по договорённости.
func Owned[T any](ctx context.Context, db *gorm.DB, id, ownerID uuid.UUID) (*T, error) {
	var row T
	if err := requireOwnerColumn(db, &row); err != nil {
		return nil, err
	}
	err := db.WithContext(ctx).Where("id = ? AND "+ownerColumn+" = ?", id, ownerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("загрузка ресурса: %w", err)
	}
	return &row, nil
}

// Shared загружает книгу или перевод из общей библиотеки (дополнение §9):
// их читают все вошедшие пользователи. Тип ограничен книгами и переводами
// — подключение с ключом, экспорт или аккаунт так не загрузить даже по
// ошибке. Изменения — по-прежнему только через Owned.
func Shared[T models.Book | models.TranslationProject](ctx context.Context, db *gorm.DB, id uuid.UUID) (*T, error) {
	var row T
	err := db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("загрузка ресурса: %w", err)
	}
	return &row, nil
}

// OwnedScope ограничивает выборку ресурсами владельца — для списков.
func OwnedScope(ownerID uuid.UUID) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(ownerColumn+" = ?", ownerID)
	}
}

// requireOwnerColumn проверяет модель по её схеме до запроса. Без этого
// модель без owner_id дала бы SQL-ошибку только во время выполнения, а в
// транзакции ещё и сорвала бы её целиком.
func requireOwnerColumn(db *gorm.DB, model any) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return fmt.Errorf("схема модели: %w", err)
	}
	if stmt.Schema.LookUpField(ownerColumn) == nil {
		return fmt.Errorf("%w: %s", ErrNoOwnerColumn, stmt.Schema.Name)
	}
	return nil
}

// OwnedSegment — сегмент перевода, если его проект принадлежит ownerID.
// У сегмента нет своего owner_id: владение проверяется через проект, и
// эта проверка тоже живёт здесь, а не в обработчике.
func OwnedSegment(ctx context.Context, db *gorm.DB, id, ownerID uuid.UUID) (*models.TranslationSegment, *models.TranslationProject, error) {
	var seg models.TranslationSegment
	err := db.WithContext(ctx).
		Joins("JOIN translation_projects p ON p.id = translation_segments.project_id").
		Where("translation_segments.id = ? AND p.owner_id = ?", id, ownerID).
		First(&seg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("загрузка сегмента: %w", err)
	}
	project, err := Owned[models.TranslationProject](ctx, db, seg.ProjectID, ownerID)
	if err != nil {
		return nil, nil, err
	}
	return &seg, project, nil
}
