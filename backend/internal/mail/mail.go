// Package mail отправляет письма через SMTP (дополнение §18: ссылки сброса
// пароля). Только стандартная библиотека: net/smtp покрывает STARTTLS,
// неявный TLS и AUTH PLAIN, а большего письму со ссылкой не нужно.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Режимы шифрования соединения.
const (
	// TLSStartTLS — по умолчанию: сервер обязан поддерживать STARTTLS,
	// иначе письмо не уходит (пароль SMTP не передаётся открытым текстом).
	TLSStartTLS = "starttls"
	// TLSImplicit — TLS с первого байта (порт 465).
	TLSImplicit = "tls"
	// TLSNone — без шифрования: только для почтового релея на том же хосте.
	TLSNone = "none"
)

const sendTimeout = 30 * time.Second

// Config — настройки SMTP (LINGUASTACK_SMTP_*).
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      string
}

// Message — письмо: текст без разметки.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender отправляет письма.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP — отправка через SMTP-сервер.
type SMTP struct {
	cfg  Config
	from *mail.Address
}

// NewSMTP проверяет настройки. Ошибка — при запуске, а не при первом
// письме, которое кто-то ждёт.
func NewSMTP(cfg Config) (*SMTP, error) {
	if cfg.TLS == "" {
		cfg.TLS = TLSStartTLS
	}
	switch cfg.TLS {
	case TLSStartTLS, TLSImplicit, TLSNone:
	default:
		return nil, fmt.Errorf("LINGUASTACK_SMTP_TLS = %q: допустимы starttls, tls и none", cfg.TLS)
	}
	if cfg.Host == "" || cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, errors.New("LINGUASTACK_SMTP_HOST и LINGUASTACK_SMTP_PORT: нужен адрес SMTP-сервера")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("LINGUASTACK_SMTP_FROM: %w", err)
	}
	return &SMTP{cfg: cfg, from: from}, nil
}

// Send отправляет письмо. Текст ошибки — ответ сервера, без пароля.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.To, "\r\n") {
		return errors.New("адрес получателя содержит перевод строки")
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("адрес получателя: %w", err)
	}
	msg, err := compose(s.from, to, m, time.Now())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	conn, err := s.dial(ctx)
	if err != nil {
		return fmt.Errorf("SMTP: соединение: %w", err)
	}
	// net/smtp не знает контекстов: срок — дедлайном соединения, отмена —
	// закрытием.
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP: приветствие: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("SMTP: сервер не поддерживает STARTTLS (LINGUASTACK_SMTP_TLS)")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("SMTP: STARTTLS: %w", err)
		}
	}
	if s.cfg.Username != "" {
		// PlainAuth сам отказывается слать пароль без TLS на чужой хост.
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("SMTP: вход: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("SMTP: отправитель: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP: получатель: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("SMTP: письмо: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP: письмо не принято: %w", err)
	}
	return c.Quit()
}

func (s *SMTP) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	if s.cfg.TLS == TLSImplicit {
		d := &tls.Dialer{Config: &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}}
		return d.DialContext(ctx, "tcp", addr)
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
}

// compose собирает письмо RFC 5322: тема — RFC 2047, текст — base64 в
// UTF-8 (кириллица в 7-битной почте).
func compose(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, fmt.Errorf("Message-ID: %w", err)
	}
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	var b bytes.Buffer
	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", from.String())
	header("To", to.Address)
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "base64")
	b.WriteString("\r\n")
	body := base64.StdEncoding.EncodeToString([]byte(m.Body))
	for len(body) > 76 {
		b.WriteString(body[:76] + "\r\n")
		body = body[76:]
	}
	b.WriteString(body + "\r\n")
	return b.Bytes(), nil
}
