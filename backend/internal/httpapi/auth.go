package httpapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/security"
)

const (
	minPasswordLength = 12
	maxPasswordLength = 256
	maxEmailLength    = 320
)

// Один ответ на «нет такого аккаунта» и «неверный пароль»: различие
// позволяет перечислять зарегистрированные адреса.
const loginFailedMessage = "Неверный адрес или пароль"

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// normalizeEmail приводит адрес к каноническому виду. Без этого
// «A@x.com» и «a@x.com» стали бы двумя аккаунтами одного человека.
func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxEmailLength {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	// ParseAddress принимает и «Имя <a@b>» — нужен голый адрес.
	if err != nil || addr.Address != email {
		return "", false
	}
	return email, true
}

// registrationAllowed — адрес есть в списке приглашённых или его домен
// указан как «@домен»; пустой список — регистрация открыта. email уже
// нормализован (нижний регистр).
func registrationAllowed(allowlist, email string) bool {
	if strings.TrimSpace(allowlist) == "" {
		return true
	}
	domain := email[strings.LastIndexByte(email, '@'):]
	for entry := range strings.SplitSeq(allowlist, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry != "" && (entry == email || entry == domain) {
			return true
		}
	}
	return false
}

const lengthRuleMessage = "Пароль должен содержать от 12 до 256 символов"

func passwordLengthOK(p string) bool {
	n := utf8.RuneCountInString(p)
	return n >= minPasswordLength && n <= maxPasswordLength
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// equalizeTiming сверяет пароль с фиктивным хешем, когда аккаунта нет.
// Иначе ответ для несуществующего адреса приходит заметно быстрее
// (argon2 не выполняется), и по времени можно перечислять аккаунты.
func equalizeTiming(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = security.HashPassword("linguastack-timing-equalizer")
	})
	security.VerifyPassword(password, dummyHash)
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		writeError(w, http.StatusUnprocessableEntity, "Некорректный адрес электронной почты")
		return
	}
	if !passwordLengthOK(in.Password) {
		writeError(w, http.StatusUnprocessableEntity, lengthRuleMessage)
		return
	}
	if !registrationAllowed(s.Cfg.RegistrationAllowlist, email) {
		writeError(w, http.StatusForbidden, "Регистрация по приглашению. Чтобы получить доступ, напишите администратору.")
		return
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		internalError(w, r, err)
		return
	}

	user := models.User{Email: email, PasswordHash: hash, Role: models.RoleUser, IsActive: true}
	// Вставка во вложенной транзакции: нарушение уникальности откатывает
	// только её, а не внешнюю транзакцию. Проверка «уже есть» — по ошибке
	// ограничения, а не предварительным SELECT: так нет гонки двух регистраций.
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		return tx.Create(&user).Error
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		writeError(w, http.StatusConflict, "Адрес уже зарегистрирован")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		writeError(w, http.StatusUnauthorized, loginFailedMessage)
		return
	}
	ctx, ip := r.Context(), s.byIP(r)

	failures, err := security.RecentFailures(ctx, s.DB, email, ip, s.Cfg.LoginWindow)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if failures >= int64(s.Cfg.LoginMaxAttempts) {
		writeError(w, http.StatusTooManyRequests, "Слишком много попыток входа. Повторите позже.")
		return
	}

	var user models.User
	err = s.DB.WithContext(ctx).Where("email = ?", email).First(&user).Error
	found := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internalError(w, r, err)
		return
	}

	authenticated := false
	if found {
		authenticated = security.VerifyPassword(in.Password, user.PasswordHash) && user.IsActive
	} else {
		equalizeTiming(in.Password)
	}

	if err := security.RecordAttempt(ctx, s.DB, email, ip, authenticated); err != nil {
		internalError(w, r, err)
		return
	}
	if !authenticated {
		writeError(w, http.StatusUnauthorized, loginFailedMessage)
		return
	}

	token, err := security.IssueSession(ctx, s.DB, user.ID, s.Cfg.SessionTTL)
	if err != nil {
		internalError(w, r, err)
		return
	}
	http.SetCookie(w, s.sessionCookie(token, int(s.Cfg.SessionTTL/time.Second)))
	s.touchLastSeen(ctx, &user)
	writeJSON(w, http.StatusOK, user)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(security.SessionCookie); err == nil {
		if err := security.RevokeSession(r.Context(), s.DB, cookie.Value); err != nil {
			internalError(w, r, err)
			return
		}
	}
	http.SetCookie(w, s.sessionCookie("", -1))
	w.WriteHeader(http.StatusNoContent)
}

// sessionCookie — единственное место, где собирается cookie сессии.
func (s *server) sessionCookie(value string, maxAge int) *http.Cookie {
	//nolint:gosec // G124: Secure берётся из настроек (по умолчанию true); false — только для разработки по http.
	return &http.Cookie{
		Name:     security.SessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.Cfg.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, UserFromContext(r.Context()))
}
