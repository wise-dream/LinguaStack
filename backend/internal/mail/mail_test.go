package mail_test

import (
	"bufio"
	"encoding/base64"
	"io"
	"mime"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	linguamail "github.com/vnovels/linguastack/internal/mail"
)

// fakeSMTP — минимальный SMTP-сервер: записывает конверт, данные и AUTH.
type fakeSMTP struct {
	ln       net.Listener
	starttls bool   // объявлять STARTTLS
	rejectTo string // RCPT на этот адрес — 550

	mu    sync.Mutex
	auth  []string
	from  string
	to    []string
	data  string
	plain bool // AUTH пришёл по открытому каналу
}

func startSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeSMTP{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			say("250-fake")
			if f.starttls {
				say("250-STARTTLS")
			}
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			f.mu.Lock()
			f.auth = strings.Split(string(raw), "\x00")
			f.plain = true
			f.mu.Unlock()
			say("235 ok")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.mu.Lock()
			f.from = strings.Trim(line[len("MAIL FROM:"):], "<> ")
			f.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			to := strings.Trim(line[len("RCPT TO:"):], "<> ")
			if to == f.rejectTo {
				say("550 no such user")
				continue
			}
			f.mu.Lock()
			f.to = append(f.to, to)
			f.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func TestSendPlainText(t *testing.T) {
	srv := startSMTP(t)
	s, err := linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: srv.port(), TLS: linguamail.TLSNone,
		Username: "mailer", Password: "smtp-secret", From: "LinguaStack <noreply@ls.example>"})
	require.NoError(t, err)
	err = s.Send(t.Context(), linguamail.Message{To: "reader@example.com", Subject: "Сброс пароля LinguaStack",
		Body: "Перейдите по ссылке: https://ls.example/reset-password?token=abc"})
	require.NoError(t, err)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Equal(t, "noreply@ls.example", srv.from)
	require.Equal(t, []string{"reader@example.com"}, srv.to)
	require.Equal(t, []string{"", "mailer", "smtp-secret"}, srv.auth)

	msg, err := mail.ReadMessage(strings.NewReader(srv.data))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	require.Equal(t, "Сброс пароля LinguaStack", subject)
	require.Equal(t, "reader@example.com", msg.Header.Get("To"))
	require.NotEmpty(t, msg.Header.Get("Date"))
	require.NotEmpty(t, msg.Header.Get("Message-ID"))
	require.Contains(t, msg.Header.Get("Content-Type"), "charset=utf-8")
	require.Equal(t, "base64", msg.Header.Get("Content-Transfer-Encoding"))
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	require.NoError(t, err)
	require.Contains(t, string(body), "https://ls.example/reset-password?token=abc")
}

func TestStartTLSRequiredBeforeAuth(t *testing.T) {
	// Сервер не умеет STARTTLS: пароль не уходит открытым текстом.
	srv := startSMTP(t)
	s, err := linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: srv.port(),
		Username: "mailer", Password: "smtp-secret", From: "noreply@ls.example"})
	require.NoError(t, err)
	err = s.Send(t.Context(), linguamail.Message{To: "reader@example.com", Subject: "x", Body: "y"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "smtp-secret")
	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.False(t, srv.plain, "AUTH не отправлен")
	require.Empty(t, srv.data)
}

func TestSendErrors(t *testing.T) {
	srv := startSMTP(t)
	srv.rejectTo = "ghost@example.com"
	s, err := linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: srv.port(), TLS: linguamail.TLSNone,
		Username: "mailer", Password: "smtp-secret", From: "noreply@ls.example"})
	require.NoError(t, err)
	err = s.Send(t.Context(), linguamail.Message{To: "ghost@example.com", Subject: "x", Body: "y"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "smtp-secret")

	err = s.Send(t.Context(), linguamail.Message{To: "a@example.com\r\nBcc: victim@example.com", Subject: "x", Body: "y"})
	require.Error(t, err, "перевод строки в адресе — подделка заголовков")

	_, err = linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: 25, TLS: "ssl3", From: "a@b.c"})
	require.Error(t, err)
	_, err = linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: 25, From: "не адрес"})
	require.Error(t, err)
	s, err = linguamail.NewSMTP(linguamail.Config{Host: "127.0.0.1", Port: 9, TLS: linguamail.TLSNone, From: "a@b.c"})
	require.NoError(t, err)
	require.Error(t, s.Send(t.Context(), linguamail.Message{To: "a@example.com", Subject: "x", Body: "y"}))
}
