package models

import "time"

// Роли пользователя. Роль admin НЕ даёт доступа к содержимому книг
// (ТЗ §2.1, дополнение §9) — проверка владения её не учитывает.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// User — пользователь и данные аккаунта.
type User struct {
	Base
	Email        string `gorm:"not null" json:"email"`
	PasswordHash string `gorm:"not null" json:"-"`
	Role         string `gorm:"not null" json:"role"`
	IsActive     bool   `gorm:"not null" json:"-"`
	// Квоты (дополнение §18); nil — значение по умолчанию из настроек.
	MaxActiveProjects  *int       `json:"-"`
	MaxConcurrentTasks *int       `json:"-"`
	StorageQuotaBytes  *int64     `json:"-"`
	LastSeenAt         *time.Time `json:"-"`
}
