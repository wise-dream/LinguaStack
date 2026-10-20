package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
)

// setRole выполняет «api -set-role email=роль»: так назначается первый
// администратор (дополнение §18) — в веб-интерфейсе роль меняет только
// администратор.
func setRole(ctx context.Context, db *gorm.DB, spec string) error {
	email, role, ok := strings.Cut(strings.TrimSpace(spec), "=")
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if !ok || email == "" {
		return fmt.Errorf("-set-role: ожидается адрес=роль, например admin@example.com=admin")
	}
	if role != models.RoleUser && role != models.RoleAdmin {
		return fmt.Errorf("-set-role: роль %q — допустимы user и admin", role)
	}
	res := db.WithContext(ctx).Model(&models.User{}).Where("email = ?", email).Update("role", role)
	if res.Error != nil {
		return fmt.Errorf("назначение роли: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("-set-role: пользователь %s не найден — сначала зарегистрируйтесь", email)
	}
	slog.Info("роль назначена", "email", email, "role", role)
	return nil
}
