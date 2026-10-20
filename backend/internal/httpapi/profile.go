package httpapi

import (
	"errors"
	"net/http"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/security"
)

// Профиль (ТЗ §2.1): смена пароля и почты — с текущим паролем.

type passwordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type emailChange struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

// verifyCurrentPassword сверяет текущий пароль. Неудачи пишутся в журнал
// попыток входа: украденная сессия не даёт подбирать пароль быстрее, чем
// форма входа. Неверный пароль — 403, а не 401: сессия жива, и клиент не
// должен выкидывать пользователя на страницу входа.
func (s *server) verifyCurrentPassword(w http.ResponseWriter, r *http.Request, user *models.User, password string) bool {
	ctx, ip := r.Context(), s.byIP(r)
	failures, err := security.RecentFailures(ctx, s.DB, user.Email, ip, s.Cfg.LoginWindow)
	if err != nil {
		internalError(w, r, err)
		return false
	}
	if failures >= int64(s.Cfg.LoginMaxAttempts) {
		writeError(w, http.StatusTooManyRequests, "Слишком много попыток. Повторите позже.")
		return false
	}
	ok := security.VerifyPassword(password, user.PasswordHash)
	if err := security.RecordAttempt(ctx, s.DB, user.Email, ip, ok); err != nil {
		internalError(w, r, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Неверный текущий пароль")
	}
	return ok
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in passwordChange
	if !decodeJSON(w, r, &in) {
		return
	}
	user := UserFromContext(r.Context())
	if !s.verifyCurrentPassword(w, r, user, in.CurrentPassword) {
		return
	}
	if !passwordLengthOK(in.NewPassword) {
		writeError(w, http.StatusUnprocessableEntity, lengthRuleMessage)
		return
	}
	hash, err := security.HashPassword(in.NewPassword)
	if err != nil {
		internalError(w, r, err)
		return
	}
	current := ""
	if cookie, err := r.Cookie(security.SessionCookie); err == nil {
		current = cookie.Value
	}
	// Остальные устройства выходят: пароль меняют, в том числе когда его
	// узнал кто-то другой.
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Update("password_hash", hash).Error; err != nil {
			return err
		}
		return security.RevokeUserSessions(r.Context(), tx, user.ID, current)
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) changeEmail(w http.ResponseWriter, r *http.Request) {
	var in emailChange
	if !decodeJSON(w, r, &in) {
		return
	}
	user := UserFromContext(r.Context())
	if !s.verifyCurrentPassword(w, r, user, in.CurrentPassword) {
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "Некорректный адрес электронной почты")
		return
	}
	// Вложенная транзакция: нарушение уникальности откатывает только её
	// (как при регистрации).
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&models.User{}).Where("id = ?", user.ID).Update("email", email).Error
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		writeError(w, http.StatusConflict, "Адрес уже зарегистрирован")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	user.Email = email
	writeJSON(w, http.StatusOK, user)
}
