package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/security"
)

type contextKey int

const (
	userKey contextKey = iota
	requestInfoKey
)

// UserFromContext возвращает пользователя, установленного requireUser.
func UserFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(userKey).(*models.User)
	return user
}

// requireUser пропускает запрос только с действующей сессией.
func (s *server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(security.SessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Требуется аутентификация")
			return
		}
		user, err := security.LookupSessionUser(r.Context(), s.DB, cookie.Value)
		if errors.Is(err, security.ErrNoSession) {
			writeError(w, http.StatusUnauthorized, "Требуется аутентификация")
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		s.touchLastSeen(r.Context(), user)
		if info := requestInfoFrom(r.Context()); info != nil {
			info.user = user.ID.String()
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}
