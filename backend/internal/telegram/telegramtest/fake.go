// Package telegramtest — поддельный сервер Telegram Bot API для тестов бота
// и доставки уведомлений: очередь обновлений, записанные сообщения и
// внедряемые ошибки.
package telegramtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/vnovels/linguastack/internal/telegram"
)

// Token — токен поддельного бота.
const Token = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawQ"

// Edit — изменённое сообщение.
type Edit struct {
	ChatID, MessageID int64
	Text              string
	Keyboard          [][]telegram.Button
}

// Server — поддельный Bot API.
type Server struct {
	srv     *httptest.Server
	mu      sync.Mutex
	updates []telegram.Update
	sent    []telegram.Message
	edits   []Edit
	answers []string
	nextID  int64
	// Fail, если задан, решает за сервер: status != 0 — ответить ошибкой
	// с этим кодом и телом.
	Fail func(method string) (status int, body string)
}

// NewServer запускает сервер; он закроется вместе с тестом.
func NewServer(t testing.TB) *Server {
	s := &Server{nextID: 100}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

// Client — клиент, настроенный на этот сервер.
func (s *Server) Client() *telegram.Client {
	return telegram.NewClient(s.srv.URL, Token, s.srv.Client())
}

// URL — адрес сервера.
func (s *Server) URL() string { return s.srv.URL }

// Push добавляет обновление для getUpdates.
func (s *Server) Push(u telegram.Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, u)
}

// Sent — отправленные сообщения.
func (s *Server) Sent() []telegram.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]telegram.Message(nil), s.sent...)
}

// Edits — изменённые сообщения.
func (s *Server) Edits() []Edit {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Edit(nil), s.edits...)
}

// Answers — ответы на нажатия кнопок.
func (s *Server) Answers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.answers...)
}

type keyboardJSON struct {
	InlineKeyboard [][]telegram.Button `json:"inline_keyboard"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	method, ok := strings.CutPrefix(r.URL.Path, "/bot"+Token+"/")
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	if s.Fail != nil {
		if status, body := s.Fail(method); status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
			return
		}
	}
	var body struct {
		Offset      int64        `json:"offset"`
		ChatID      int64        `json:"chat_id"`
		MessageID   int64        `json:"message_id"`
		Text        string       `json:"text"`
		ReplyMarkup keyboardJSON `json:"reply_markup"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	var result any = true
	switch method {
	case "getMe":
		result = telegram.User{ID: 1, Username: "linguastack_bot", FirstName: "LinguaStack"}
	case "getUpdates":
		var out []telegram.Update
		for _, u := range s.updates {
			if u.UpdateID >= body.Offset {
				out = append(out, u)
			}
		}
		result = out
	case "sendMessage":
		s.nextID++
		s.sent = append(s.sent, telegram.Message{ChatID: body.ChatID, Text: body.Text, Keyboard: body.ReplyMarkup.InlineKeyboard})
		result = map[string]any{"message_id": s.nextID}
	case "editMessageText":
		s.edits = append(s.edits, Edit{ChatID: body.ChatID, MessageID: body.MessageID, Text: body.Text, Keyboard: body.ReplyMarkup.InlineKeyboard})
	case "answerCallbackQuery":
		s.answers = append(s.answers, body.Text)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}
