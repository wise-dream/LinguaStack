package httpapi

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/telegram"
)

// Привязка Telegram и настройки уведомлений (ТЗ §2.1, §9, §12;
// дополнение §17). Ресурс один на пользователя — без идентификатора в
// пути: чужую привязку нельзя даже назвать.

type telegramOut struct {
	Enabled         bool       `json:"enabled"`
	BotUsername     string     `json:"bot_username,omitempty"`
	Linked          bool       `json:"linked"`
	Username        string     `json:"username,omitempty"`
	LinkedAt        *time.Time `json:"linked_at,omitempty"`
	NotifyChapters  bool       `json:"notify_chapters"`
	NotifyCompleted bool       `json:"notify_completed"`
	NotifyErrors    bool       `json:"notify_errors"`
}

func (s *server) telegramState(r *http.Request) (telegramOut, error) {
	out := telegramOut{Enabled: s.Cfg.TelegramEnabled(), NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}
	if out.Enabled {
		out.BotUsername = s.Cfg.TelegramBotUsername
	}
	var b models.TelegramBinding
	err := s.DB.WithContext(r.Context()).First(&b, "user_id = ?", UserFromContext(r.Context()).ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	linked := b.CreatedAt.UTC()
	out.Linked, out.Username, out.LinkedAt = true, b.Username, &linked
	out.NotifyChapters, out.NotifyCompleted, out.NotifyErrors = b.NotifyChapters, b.NotifyCompleted, b.NotifyErrors
	return out, nil
}

func (s *server) getTelegram(w http.ResponseWriter, r *http.Request) {
	out, err := s.telegramState(r)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// linkTelegram выдаёт ссылку с новым кодом; прежний код перестаёт
// действовать.
func (s *server) linkTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.TelegramEnabled() {
		writeError(w, http.StatusServiceUnavailable, "Telegram-бот не настроен на этом сервере")
		return
	}
	user := UserFromContext(r.Context())
	code, hash := telegram.LinkCode()
	expires := time.Now().Add(telegram.LinkCodeTTL).UTC()
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.TelegramLinkToken{}, "user_id = ?", user.ID).Error; err != nil {
			return err
		}
		return tx.Create(&models.TelegramLinkToken{TokenHash: hash, UserID: user.ID, ExpiresAt: expires}).Error
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": telegram.LinkURL(s.Cfg.TelegramBotUsername, code), "expires_at": expires})
}

type telegramSettingsIn struct {
	NotifyChapters  *bool `json:"notify_chapters"`
	NotifyCompleted *bool `json:"notify_completed"`
	NotifyErrors    *bool `json:"notify_errors"`
}

func (s *server) patchTelegram(w http.ResponseWriter, r *http.Request) {
	var in telegramSettingsIn
	if !decodeJSON(w, r, &in) {
		return
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	for column, v := range map[string]*bool{"notify_chapters": in.NotifyChapters,
		"notify_completed": in.NotifyCompleted, "notify_errors": in.NotifyErrors} {
		if v != nil {
			updates[column] = *v
		}
	}
	res := s.DB.WithContext(r.Context()).Model(&models.TelegramBinding{}).
		Where("user_id = ?", UserFromContext(r.Context()).ID).Updates(updates)
	if res.Error != nil {
		internalError(w, r, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		writeError(w, http.StatusConflict, "Telegram не привязан")
		return
	}
	s.getTelegram(w, r)
}

func (s *server) unlinkTelegram(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.TelegramLinkToken{}, "user_id = ?", user.ID).Error; err != nil {
			return err
		}
		return tx.Delete(&models.TelegramBinding{}, "user_id = ?", user.ID).Error
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
