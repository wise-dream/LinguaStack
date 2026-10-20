package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// Квота хранилища (дополнение §18) — сумма размеров оригиналов книг
// пользователя. Ресурсы из архивов и файлы экспорта в неё не входят: их
// размер ограничен пределами архива и самой книгой.

const storageQuotaMessage = "Превышена квота хранилища. Удалите ненужные книги или обратитесь к администратору."

var errStorageQuota = errors.New("превышена квота хранилища")

// storageQuota — предел пользователя: своя квота или значение по
// умолчанию; 0 — без предела.
func (s *server) storageQuota(u *models.User) int64 {
	if u.StorageQuotaBytes != nil {
		return *u.StorageQuotaBytes
	}
	return s.Cfg.StorageQuotaBytes
}

// checkStorageQuota — поместятся ли ещё size байт.
func checkStorageQuota(ctx context.Context, db *gorm.DB, userID uuid.UUID, quota, size int64) error {
	if quota <= 0 {
		return nil
	}
	var used int64
	err := db.WithContext(ctx).Model(&models.Book{}).Where("owner_id = ?", userID).
		Select("coalesce(sum(original_size), 0)").Scan(&used).Error
	if err != nil {
		return fmt.Errorf("занятое хранилище: %w", err)
	}
	if used+size > quota {
		return errStorageQuota
	}
	return nil
}
