package providers

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Таксономия ошибок — дополнение §6.1. Все адаптеры приводят ошибки
// транспорта к ней, чтобы очередь решала о повторе, не зная провайдера.
var (
	// ErrUnavailable — сеть, 5xx, сервер не запущен. Повторять с паузой.
	ErrUnavailable = errors.New("провайдер недоступен")
	// ErrAuth — неверный или отозванный ключ. Повтор бессмыслен.
	ErrAuth = errors.New("провайдер отклонил учётные данные")
	// ErrModelNotAvailable — модели нет (в Ollama не загружена). Повтор бессмыслен.
	ErrModelNotAvailable = errors.New("модель недоступна")
	// ErrBadRequest — провайдер отверг запрос (400). Дефект запроса, а не среды.
	ErrBadRequest = errors.New("провайдер отклонил запрос")
)

// RateLimitedError — превышен лимит частоты (429). Повторять после RetryAfter.
type RateLimitedError struct {
	RetryAfter time.Duration // 0 — провайдер не сообщил
	Detail     string
}

func (e *RateLimitedError) Error() string {
	msg := "превышен лимит запросов провайдера"
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(", повтор через %s", e.RetryAfter)
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

func (e *RateLimitedError) Retryable() bool { return true }

// RefusedError — модель отказалась по политике безопасности. У Anthropic
// это HTTP 200 со stop_reason "refusal": такой ответ нельзя принять за
// перевод, а повтор того же текста даст тот же отказ.
type RefusedError struct {
	Category    string
	Explanation string
}

func (e *RefusedError) Error() string {
	msg := "модель отказалась обрабатывать текст"
	if e.Category != "" {
		msg += " (категория: " + e.Category + ")"
	}
	if e.Explanation != "" {
		msg += ": " + e.Explanation
	}
	return msg
}

func (e *RefusedError) Retryable() bool { return false }

// Retryable — единственное место, где решается, повторять ли запрос.
//
// Ошибка может сама объявить повторяемость методом Retryable() bool — так
// ошибка протокола из пакета перевода участвует в решении, не создавая
// цикла импортов.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	var self interface{ Retryable() bool }
	if errors.As(err, &self) {
		return self.Retryable()
	}
	switch {
	case errors.Is(err, ErrUnavailable):
		return true
	case errors.Is(err, context.DeadlineExceeded):
		return true // наш тайм-аут на запрос: сервер мог быть просто занят
	}
	// context.Canceled — остановка сервиса, а не сбой провайдера.
	return false
}

// RetryAfter — рекомендованная пауза, если провайдер её сообщил.
func RetryAfter(err error) (time.Duration, bool) {
	var rl *RateLimitedError
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		return rl.RetryAfter, true
	}
	return 0, false
}
