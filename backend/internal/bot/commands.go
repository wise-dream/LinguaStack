package bot

import (
	"context"
	"errors"
	"html"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/telegram"
)

// binding — привязка Telegram-пользователя; nil — не привязан.
func (b *Bot) binding(ctx context.Context, telegramUserID int64) (*models.TelegramBinding, error) {
	var bnd models.TelegramBinding
	err := b.db.WithContext(ctx).First(&bnd, "telegram_user_id = ?", telegramUserID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bnd, nil
}

func (b *Bot) email(ctx context.Context, bnd *models.TelegramBinding) (string, error) {
	var user models.User
	if err := b.db.WithContext(ctx).Select("email").First(&user, "id = ?", bnd.UserID).Error; err != nil {
		return "", err
	}
	return user.Email, nil
}

func (b *Bot) start(ctx context.Context, m *telegram.IncomingMessage) error {
	bnd, err := b.binding(ctx, m.From.ID)
	if err != nil {
		return err
	}
	if bnd != nil {
		email, err := b.email(ctx, bnd)
		if err != nil {
			return err
		}
		return b.reply(ctx, m.Chat.ID, "Telegram уже привязан к аккаунту "+html.EscapeString(email)+". /status — ваши переводы.", nil)
	}
	return b.reply(ctx, m.Chat.ID, "Здравствуйте! Чтобы получать сюда уведомления о переводах, привяжите аккаунт: "+b.settingsHint(), nil)
}

var errBadCode = errors.New("код привязки не подходит")

// link привязывает Telegram по коду из ссылки (дополнение §17). Код
// одноразовый: удаляется тем же запросом, что проверяет срок. Один
// Telegram — один аккаунт: прежние привязки и этого Telegram, и этого
// аккаунта заменяются; настройки уведомлений переносятся, если аккаунт
// тот же.
func (b *Bot) link(ctx context.Context, m *telegram.IncomingMessage, code string) error {
	var bnd models.TelegramBinding
	err := b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < now()").Delete(&models.TelegramLinkToken{}).Error; err != nil {
			return err
		}
		var tokens []models.TelegramLinkToken
		res := tx.Clauses(clause.Returning{}).Where("token_hash = ?", telegram.HashCode(code)).Delete(&tokens)
		if res.Error != nil {
			return res.Error
		}
		if len(tokens) == 0 {
			return errBadCode
		}
		userID := tokens[0].UserID
		bnd = models.TelegramBinding{UserID: userID, TelegramUserID: m.From.ID, ChatID: m.Chat.ID, Username: m.From.Username,
			NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}
		var prev models.TelegramBinding
		if err := tx.First(&prev, "user_id = ?", userID).Error; err == nil {
			bnd.NotifyChapters, bnd.NotifyCompleted, bnd.NotifyErrors = prev.NotifyChapters, prev.NotifyCompleted, prev.NotifyErrors
		}
		if err := tx.Where("user_id = ? OR telegram_user_id = ?", userID, m.From.ID).Delete(&models.TelegramBinding{}).Error; err != nil {
			return err
		}
		return tx.Create(&bnd).Error
	})
	if errors.Is(err, errBadCode) {
		return b.reply(ctx, m.Chat.ID, "Код привязки не подходит или устарел. Получите новую ссылку: "+b.settingsHint(), nil)
	}
	if err != nil {
		return err
	}
	email, err := b.email(ctx, &bnd)
	if err != nil {
		return err
	}
	return b.reply(ctx, m.Chat.ID, "Готово: Telegram привязан к аккаунту "+html.EscapeString(email)+
		". Сюда будут приходить уведомления о переводах. /status — ваши переводы.", nil)
}

func (b *Bot) unlink(ctx context.Context, m *telegram.IncomingMessage) error {
	res := b.db.WithContext(ctx).Delete(&models.TelegramBinding{}, "telegram_user_id = ?", m.From.ID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return b.reply(ctx, m.Chat.ID, "Telegram и так не привязан к аккаунту LinguaStack.", nil)
	}
	return b.reply(ctx, m.Chat.ID, "Telegram отвязан: уведомления больше не придут. Привязать снова: "+b.settingsHint(), nil)
}
