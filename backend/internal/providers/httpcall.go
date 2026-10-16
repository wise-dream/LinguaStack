package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxResponseBody = 32 << 20
	maxErrorDetail  = 300
)

// httpCall — JSON-запрос к API провайдера с приведением ошибок к таксономии.
type httpCall struct {
	client  *http.Client
	method  string
	url     string
	headers map[string]string
	body    any
	// secret вырезается из текстов ошибок: они попадают в логи и в
	// last_error задачи (ТЗ §13 — запрет на раскрытие секретов).
	secret string
}

// do выполняет запрос. parent — контекст вызывающего, ctx — он же с
// тайм-аутом адаптера: так собственный тайм-аут (повторяемый сбой)
// отличается от отмены вызывающим (остановка сервиса, не сбой).
func (c httpCall) do(parent, ctx context.Context, out any) error {
	var reader io.Reader
	if c.body != nil {
		raw, err := json.Marshal(c.body)
		if err != nil {
			return fmt.Errorf("%w: сериализация запроса: %w", ErrBadRequest, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, c.method, c.url, reader)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBadRequest, c.scrub(err.Error()))
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return c.transportError(parent, ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return c.transportError(parent, ctx, err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return statusError(resp.StatusCode, resp.Header, c.scrub(errorDetail(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%w: некорректный ответ провайдера: %s", ErrUnavailable, c.scrub(truncate(string(raw))))
		}
	}
	return nil
}

func (c httpCall) transportError(parent, ctx context.Context, err error) error {
	switch {
	case parent.Err() != nil:
		return fmt.Errorf("запрос к провайдеру прерван: %w", parent.Err())
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%w: тайм-аут ответа: %w", ErrUnavailable, context.DeadlineExceeded)
	}
	return fmt.Errorf("%w: %s", ErrUnavailable, c.scrub(err.Error()))
}

func (c httpCall) scrub(s string) string {
	if c.secret == "" {
		return s
	}
	return strings.ReplaceAll(s, c.secret, "***")
}

func statusError(status int, header http.Header, detail string) error {
	var base error
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		base = ErrAuth
	case status == http.StatusNotFound:
		base = ErrModelNotAvailable
	case status == http.StatusTooManyRequests:
		return &RateLimitedError{RetryAfter: parseRetryAfter(header.Get("Retry-After")), Detail: detail}
	case status == http.StatusRequestTimeout || status >= http.StatusInternalServerError:
		base = ErrUnavailable // включая 529 «перегружен» у Anthropic
	case status >= http.StatusBadRequest:
		base = ErrBadRequest
	default:
		base = ErrUnavailable
	}
	return fmt.Errorf("%w: HTTP %d: %s", base, status, detail)
}

// errorDetail достаёт сообщение из типичных форм ответа об ошибке:
// {"error":"…"}, {"error":{"message":"…"}}, {"message":"…"}.
func errorDetail(raw []byte) string {
	var body struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		var s string
		if json.Unmarshal(body.Error, &s) == nil && s != "" {
			return truncate(s)
		}
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body.Error, &nested) == nil && nested.Message != "" {
			return truncate(nested.Message)
		}
		if body.Message != "" {
			return truncate(body.Message)
		}
	}
	return truncate(strings.TrimSpace(string(raw)))
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxErrorDetail {
		return s
	}
	return string([]rune(s)[:maxErrorDetail]) + "…"
}

// parseRetryAfter понимает обе формы заголовка: секунды и HTTP-дату.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}
