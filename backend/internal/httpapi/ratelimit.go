package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/security"
)

// limits — ограничители частоты (дополнение §18). nil-ограничитель
// пропускает всё: так выключенный лимит и сервер для Routes() не требуют
// особых случаев.
type limits struct {
	user     *security.Limiter // любой запрос с входом, по пользователю
	uploads  *security.Limiter // загрузки книг, по пользователю
	register *security.Limiter // регистрации, по IP
	reset    *security.Limiter // запросы сброса пароля, по IP
}

func newLimits(cfg config.Settings) limits {
	return limits{
		user:     security.NewLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst),
		uploads:  security.PerHour(cfg.UploadsPerHour),
		register: security.PerHour(cfg.RegisterPerHour),
		reset:    security.PerHour(cfg.ResetPerHour),
	}
}

const (
	tooManyRequests  = "Слишком много запросов. Повторите позже."
	tooManyUploads   = "Слишком много загрузок. Повторите позже."
	tooManyRegisters = "Слишком много регистраций с этого адреса. Повторите позже."
)

func (s *server) clientIP(r *http.Request) string {
	return security.ClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"), s.Cfg.TrustedProxies)
}

// byUser — ключ лимита по пользователю; маршрут уже прошёл requireUser.
func byUser(r *http.Request) string { return UserFromContext(r.Context()).ID.String() }

// limited пропускает запрос, если у ключа есть токен; иначе 429 с
// Retry-After в целых секундах.
func limited(l *security.Limiter, key func(*http.Request) string, message string, next http.HandlerFunc) http.HandlerFunc {
	if l == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(key(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(wait)))
			writeError(w, http.StatusTooManyRequests, message)
			return
		}
		next(w, r)
	}
}

func retryAfterSeconds(wait time.Duration) int {
	return max(1, int(math.Ceil(wait.Seconds())))
}
