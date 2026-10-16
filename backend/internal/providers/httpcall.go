package providers

import (
	"bufio"
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
	req, err := c.newRequest(ctx)
	if err != nil {
		return err
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

func (c httpCall) newRequest(ctx context.Context) (*http.Request, error) {
	var reader io.Reader
	if c.body != nil {
		raw, err := json.Marshal(c.body)
		if err != nil {
			return nil, fmt.Errorf("%w: сериализация запроса: %w", ErrBadRequest, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, c.method, c.url, reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadRequest, c.scrub(err.Error()))
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// errSilence — причина отмены потока, в котором слишком долго нет данных.
var errSilence = errors.New("тишина в потоке ответа")

// maxStreamLine — предел одной строки потока (одно событие NDJSON).
const maxStreamLine = 1 << 20

// stream выполняет запрос с потоковым ответом NDJSON, передавая onLine
// каждую непустую строку. Тайм-аут idle — не на весь ответ, а на тишину:
// долгая, но идущая генерация (локальная модель на CPU даёт единицы
// токенов в секунду, батч идёт дольше десяти минут) не обрывается, а
// зависший сервер обнаруживается так же быстро, как раньше.
func (c httpCall) stream(parent context.Context, idle time.Duration, onLine func([]byte) error) error {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	timer := time.AfterFunc(idle, func() { cancel(errSilence) })
	defer timer.Stop()

	req, err := c.newRequest(ctx)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return c.streamError(parent, ctx, idle, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return statusError(resp.StatusCode, resp.Header, c.scrub(errorDetail(raw)))
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), maxStreamLine)
	total := 0
	for sc.Scan() {
		timer.Reset(idle)
		line := bytes.TrimSpace(sc.Bytes())
		if total += len(line); total > maxResponseBody {
			return fmt.Errorf("%w: ответ провайдера больше %d байт", ErrUnavailable, maxResponseBody)
		}
		if len(line) == 0 {
			continue
		}
		if err := onLine(line); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return c.streamError(parent, ctx, idle, err)
	}
	return nil
}

func (c httpCall) streamError(parent, ctx context.Context, idle time.Duration, err error) error {
	switch {
	case parent.Err() != nil:
		return fmt.Errorf("запрос к провайдеру прерван: %w", parent.Err())
	case errors.Is(context.Cause(ctx), errSilence):
		return fmt.Errorf("%w: модель не отвечает дольше %s: %w", ErrUnavailable, idle, context.DeadlineExceeded)
	}
	return fmt.Errorf("%w: %s", ErrUnavailable, c.scrub(err.Error()))
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

// minSecretLength — короче этого строка секретом не считается: её замена
// только портила бы текст ошибки (ключ «k» превращал «max_tokens» в
// «max_to***ens» и ломал распознавание отвергнутых параметров).
const minSecretLength = 8

func (c httpCall) scrub(s string) string {
	if len(c.secret) < minSecretLength {
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
