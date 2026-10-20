package httpapi

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/mail"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/telegram"
)

// Восстановление доступа (ТЗ §2.1, дополнение §18). Ответ на запрос
// одинаков для любого адреса, а доставка идёт после ответа: ни текст, ни
// время ответа не выдают, есть ли такой аккаунт.

const (
	resetAccepted     = "Если такой аккаунт есть, ссылка для сброса пароля отправлена на почту или в Telegram."
	resetInvalid      = "Ссылка для сброса пароля недействительна или устарела. Запросите новую."
	tooManyResets     = "Слишком много запросов сброса пароля с этого адреса. Повторите позже."
	resetResendWindow = time.Minute
	resetSendTimeout  = time.Minute
)

type resetRequestIn struct {
	Email string `json:"email"`
}

type resetConfirmIn struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// resetDelivery — куда отправить ссылку.
type resetDelivery struct {
	email  string // пусто — почта не настроена
	chatID int64  // 0 — Telegram не привязан или выключен
}

func (s *server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in resetRequestIn
	if !decodeJSON(w, r, &in) {
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "Некорректный адрес электронной почты")
		return
	}
	if err := s.issueResetByEmail(r.Context(), email); err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"message": resetAccepted})
}

// issueResetByEmail выдаёт код и запускает доставку. Нет аккаунта, он
// отключён, код выдан минуту назад или ссылку некуда отправить — молча
// ничего не делает.
func (s *server) issueResetByEmail(ctx context.Context, email string) error {
	var user models.User
	err := s.DB.WithContext(ctx).Where("email = ? AND is_active", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.Cfg.PublicURL == "" {
		slog.Warn("сброс пароля: LINGUASTACK_PUBLIC_URL не задан — ссылку не построить", "user", user.ID)
		return nil
	}
	to, err := s.resetChannels(ctx, &user)
	if err != nil || (to.email == "" && to.chatID == 0) {
		if err == nil {
			slog.Warn("сброс пароля: нет канала доставки (почта не настроена, Telegram не привязан)", "user", user.ID)
		}
		return err
	}
	// Повторные запросы не заваливают чужой ящик письмами.
	recent, err := security.ResetIssuedSince(ctx, s.DB, user.ID, resetResendWindow)
	if err != nil || recent {
		return err
	}
	token, _, err := security.IssueReset(ctx, s.DB, user.ID, s.Cfg.PasswordResetTTL)
	if err != nil {
		return err
	}
	// Отправка — после ответа клиенту: SMTP отвечает секундами, и время
	// ответа выдало бы, что аккаунт существует.
	go s.deliverReset(context.WithoutCancel(ctx), user.ID, to, s.resetURL(token))
	return nil
}

func (s *server) resetChannels(ctx context.Context, user *models.User) (resetDelivery, error) {
	var to resetDelivery
	if s.Mailer != nil {
		to.email = user.Email
	}
	if s.Cfg.TelegramEnabled() {
		var bnd models.TelegramBinding
		err := s.DB.WithContext(ctx).First(&bnd, "user_id = ?", user.ID).Error
		switch {
		case err == nil:
			to.chatID = bnd.ChatID
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return to, err
		}
	}
	return to, nil
}

// resetURL — ссылка на страницу нового пароля. Без LINGUASTACK_PUBLIC_URL
// — относительная (её достраивает веб-приложение администратора).
func (s *server) resetURL(token string) string {
	return strings.TrimRight(s.Cfg.PublicURL, "/") + "/reset-password?token=" + url.QueryEscape(token)
}

// deliverReset отправляет ссылку во все доступные каналы. Ссылка и код в
// журнал не пишутся.
func (s *server) deliverReset(ctx context.Context, userID uuid.UUID, to resetDelivery, link string) {
	ctx, cancel := context.WithTimeout(ctx, resetSendTimeout)
	defer cancel()
	ttl := ruDuration(s.Cfg.PasswordResetTTL)
	if to.email != "" {
		err := s.Mailer.Send(ctx, mail.Message{To: to.email, Subject: "Сброс пароля LinguaStack", Body: fmt.Sprintf(
			"Здравствуйте!\n\nДля аккаунта %s в LinguaStack запрошен сброс пароля.\n\n"+
				"Задать новый пароль: %s\n\n"+
				"Ссылка действует %s и сработает один раз. Если вы не запрашивали сброс, "+
				"ничего не делайте — пароль останется прежним.\n", to.email, link, ttl)})
		logDelivery("почта", userID, err)
	}
	if to.chatID != 0 {
		tg := telegram.NewClient(s.Cfg.TelegramAPIURL, s.Cfg.TelegramBotToken, nil)
		_, err := tg.SendMessage(ctx, telegram.Message{ChatID: to.chatID, Text: fmt.Sprintf(
			"<b>Сброс пароля LinguaStack</b>\n\n%s. Ссылка действует %s и сработает один раз. "+
				"Если вы не запрашивали сброс, ничего не делайте.",
			telegram.Link(link, "Задать новый пароль"), html.EscapeString(ttl))})
		logDelivery("telegram", userID, err)
	}
}

func logDelivery(channel string, userID uuid.UUID, err error) {
	if err != nil {
		slog.Warn("ссылка сброса пароля не отправлена", "channel", channel, "user", userID, "error", err)
		return
	}
	slog.Info("ссылка сброса пароля отправлена", "channel", channel, "user", userID)
}

func (s *server) confirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in resetConfirmIn
	if !decodeJSON(w, r, &in) {
		return
	}
	// Пароль проверяется до кода: опечатка в новом пароле не сжигает ссылку.
	if !passwordLengthOK(in.Password) {
		writeError(w, http.StatusUnprocessableEntity, lengthRuleMessage)
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		internalError(w, r, err)
		return
	}
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		userID, err := security.ConsumeReset(r.Context(), tx, in.Token)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Update("password_hash", hash).Error; err != nil {
			return err
		}
		// Все сессии гаснут: сбрасывают, в том числе когда пароль украден.
		return security.RevokeUserSessions(r.Context(), tx, userID, "")
	})
	if errors.Is(err, security.ErrResetInvalid) {
		writeError(w, http.StatusUnprocessableEntity, resetInvalid)
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminResetLink выдаёт ссылку сброса, которую администратор передаёт
// пользователю сам, когда ни почта, ни Telegram не настроены.
func (s *server) adminResetLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	var user models.User
	err = s.DB.WithContext(r.Context()).First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	token, expires, err := security.IssueReset(r.Context(), s.DB, user.ID, s.Cfg.PasswordResetTTL)
	if err != nil {
		internalError(w, r, err)
		return
	}
	slog.Info("администратор выдал ссылку сброса пароля", "admin", UserFromContext(r.Context()).ID, "user", user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"url": s.resetURL(token), "expires_at": expires.UTC()})
}

// ruDuration — «1 час», «30 минут», «2 часа».
func ruDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return plural(int(d/time.Hour), "час", "часа", "часов")
	}
	return plural(int(d.Round(time.Minute)/time.Minute), "минуту", "минуты", "минут")
}

func plural(n int, one, few, many string) string {
	word := many
	switch {
	case n%10 == 1 && n%100 != 11:
		word = one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = few
	}
	return fmt.Sprintf("%d %s", n, word)
}
