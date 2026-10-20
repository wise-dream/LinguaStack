package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// requestInfo — сведения о запросе для журнала. Указатель кладётся в
// контекст до маршрутизации: requireUser работает с копией запроса и
// дописывает пользователя сюда, иначе внешний слой его не увидит.
type requestInfo struct {
	id   string
	user string
}

func requestInfoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(requestInfoKey).(*requestInfo)
	return info
}

// requestID — ID запроса из контекста или пустая строка.
func requestID(ctx context.Context) string {
	if info := requestInfoFrom(ctx); info != nil {
		return info.id
	}
	return ""
}

// observe журналирует запрос и учитывает его в метриках (дополнение §18):
// метод, шаблон маршрута, статус, длительность, пользователь и
// X-Request-ID. Строка запроса, заголовки и тела не журналируются — в них
// бывают cookie, пароли и текст книг.
func (s *server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &requestInfo{id: incomingRequestID(r.Header.Get("X-Request-ID"))}
		w.Header().Set("X-Request-ID", info.id)
		sw := &statusWriter{ResponseWriter: w}
		r = r.WithContext(context.WithValue(r.Context(), requestInfoKey, info))
		next.ServeHTTP(sw, r)

		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		route := routeLabel(r.Pattern)
		elapsed := time.Since(start)
		s.Metrics.HTTP.Observe(r.Method, route, status, elapsed)
		// Проверки здоровья идут каждые несколько секунд — в обычном
		// журнале они только шум.
		level := slog.LevelInfo
		if route == "/api/health" {
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "запрос", "method", r.Method, "route", route, "status", status,
			"duration_ms", elapsed.Milliseconds(), "user", info.user, "request_id", info.id)
	})
}

// routeLabel — шаблон маршрута без метода: «GET /api/books/{id}» →
// «/api/books/{id}». Запрос без маршрута — «unmatched»: настоящий путь в
// метке дал бы ряд на каждый случайный адрес.
func routeLabel(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}

const maxRequestIDLength = 128

// incomingRequestID берёт ID от прокси, если он безопасен для журнала,
// иначе создаёт новый.
func incomingRequestID(v string) string {
	if v != "" && len(v) <= maxRequestIDLength && strings.Trim(v, requestIDAlphabet) == "" {
		return v
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

const requestIDAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.:"

// statusWriter запоминает код ответа.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush — поток событий проверяет http.Flusher приведением типа.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		f.Flush()
	}
}

// Unwrap — для http.ResponseController.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
