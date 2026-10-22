package queue

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/security"
)

// ProviderResolver собирает адаптер для проекта.
type ProviderResolver func(ctx context.Context, project *models.TranslationProject) (providers.Provider, error)

// ErrEgressForbidden — пользователь запретил передачу текста внешнему
// сервису (ТЗ §6.4). Повторять бессмысленно, пока согласие не дано.
var ErrEgressForbidden = errors.New("передача текста внешнему сервису запрещена настройками провайдера")

// ErrOllamaAdminOnly — Ollama сервера доступна только администратору.
var ErrOllamaAdminOnly = errors.New("локальные модели сервера (Ollama) доступны только администратору — подключите свой сервис с ключом API")

// ConfigResolver — сборка по конфигурации провайдера проекта: ключ
// расшифровывается на время батча, согласие на передачу наружу
// проверяется каждый раз.
//
// ollamaAdminOnly — Ollama сервера только проектам администратора
// (LINGUASTACK_OLLAMA_ADMIN_ONLY): подключение обычного пользователя,
// оставшееся с прежних времён, не займёт общий процессор.
func ConfigResolver(db *gorm.DB, secrets *security.SecretBox, opts providers.BuildOptions, ollamaAdminOnly bool) ProviderResolver {
	return func(ctx context.Context, project *models.TranslationProject) (providers.Provider, error) {
		var cfg models.ProviderConfig
		err := db.WithContext(ctx).First(&cfg, "id = ? AND owner_id = ?", project.ProviderConfigID, project.OwnerID).Error
		if err != nil {
			return nil, fmt.Errorf("конфигурация провайдера проекта: %w", err)
		}
		if cfg.ProviderID == "ollama" && ollamaAdminOnly {
			var role string
			if err := db.WithContext(ctx).Model(&models.User{}).Where("id = ?", project.OwnerID).Pluck("role", &role).Error; err != nil {
				return nil, fmt.Errorf("роль владельца проекта: %w", err)
			}
			if role != models.RoleAdmin {
				return nil, ErrOllamaAdminOnly
			}
		}
		if providers.SendsDataExternally(cfg.ProviderID, cfg.BaseURL) && !cfg.AllowExternal {
			return nil, ErrEgressForbidden
		}
		key := ""
		if cfg.SecretCiphertext != "" {
			if secrets == nil {
				return nil, errors.New("не задан LINGUASTACK_SECRET_KEY: ключ провайдера не расшифровать")
			}
			if key, err = secrets.Decrypt(cfg.SecretCiphertext); err != nil {
				return nil, fmt.Errorf("ключ провайдера: %w", err)
			}
		}
		return providers.Build(cfg.ProviderID, cfg.BaseURL, key, opts)
	}
}
