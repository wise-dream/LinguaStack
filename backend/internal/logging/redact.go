// Package logging — обработчики slog. Главный — фильтр секретов: ключи
// API не должны попадать в логи (ТЗ §13), даже если оказались в тексте
// ошибки от провайдера.
package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

const mask = "***"

// keyRe — значения в форме ключей провайдеров (sk-…, sk-ant-…, sk-proj-…).
var keyRe = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`)

var sensitiveKeys = map[string]bool{
	"password": true, "api_key": true, "apikey": true, "token": true, "secret": true,
	"authorization": true, "cookie": true, "set-cookie": true, "secret_ciphertext": true,
}

type redactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler оборачивает обработчик фильтром секретов.
func NewRedactingHandler(inner slog.Handler) slog.Handler {
	return redactingHandler{inner: inner}
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	clean := slog.NewRecord(r.Time, r.Level, redactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = redactAttr(a)
	}
	return redactingHandler{inner: h.inner.WithAttrs(clean)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{inner: h.inner.WithGroup(name)}
}

func redactString(s string) string { return keyRe.ReplaceAllString(s, mask) }

func redactAttr(a slog.Attr) slog.Attr {
	if sensitiveKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, mask)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindGroup:
		group := v.Group()
		clean := make([]any, len(group))
		for i, g := range group {
			clean[i] = redactAttr(g)
		}
		return slog.Group(a.Key, clean...)
	case slog.KindString:
		return slog.String(a.Key, redactString(v.String()))
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, redactString(err.Error()))
		}
		return slog.String(a.Key, redactString(v.String()))
	}
	return a
}
