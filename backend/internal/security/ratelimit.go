package security

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// RecentFailures — число неудачных входов пары (email, ip) в окне.
//
// Проверяется ДО сверки пароля: иначе лимит обходится угадыванием (ТЗ §13).
func RecentFailures(ctx context.Context, db *gorm.DB, email, ip string, window time.Duration) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Model(&models.LoginAttempt{}).
		Where("email = ? AND ip = ? AND NOT succeeded AND created_at >= ?",
			email, ip, time.Now().UTC().Add(-window)).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("подсчёт попыток входа: %w", err)
	}
	return n, nil
}

// RecordAttempt пишет попытку в журнал.
//
// Успешный вход снимает накопленные неудачи этой пары: иначе законный
// пользователь однажды окажется заблокирован задним числом.
func RecordAttempt(ctx context.Context, db *gorm.DB, email, ip string, succeeded bool) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.LoginAttempt{Email: email, IP: ip, Succeeded: succeeded}).Error; err != nil {
			return fmt.Errorf("запись попытки входа: %w", err)
		}
		if !succeeded {
			return nil
		}
		err := tx.Where("email = ? AND ip = ? AND NOT succeeded", email, ip).
			Delete(&models.LoginAttempt{}).Error
		if err != nil {
			return fmt.Errorf("сброс неудачных попыток: %w", err)
		}
		return nil
	})
}
