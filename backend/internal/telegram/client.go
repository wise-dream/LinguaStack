// Package telegram — клиент Telegram Bot API на шесть методов, которые
// нужны боту LinguaStack (дополнение §17). Стандартная библиотека вместо
// SDK: методов мало, а за токеном в адресах запросов нужен свой присмотр.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client вызывает методы Bot API.
type Client struct {
	base  string // https://api.telegram.org
	token string
	hc    *http.Client
}

// NewClient создаёт клиента. hc == nil — http.Client без общего тайм-аута:
// у долгого опроса getUpdates свой срок через контекст.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, hc: hc}
}

// APIError — ответ Telegram с ok=false.
type APIError struct {
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram: %d %s", e.Code, e.Description)
}

// Temporary — стоит повторить позже: лимит запросов или сбой Telegram.
func (e *APIError) Temporary() bool { return e.Code == http.StatusTooManyRequests || e.Code >= 500 }

type envelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call выполняет метод. Ошибки сети пересобираются без адреса: в нём токен
// бота (…/bot<токен>/метод), а *url.Error печатает адрес целиком.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: некорректный адрес API", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: чтение ответа: %w", method, err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return &APIError{Code: resp.StatusCode, Description: "ответ не JSON"}
	}
	if !env.OK {
		code := env.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Code: code, Description: env.Description, RetryAfter: time.Duration(env.Parameters.RetryAfter) * time.Second}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, result); err != nil {
		return fmt.Errorf("telegram %s: разбор результата: %w", method, err)
	}
	return nil
}

// User — пользователь или бот Telegram.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
}

// Chat — чат; Type: private, group, supergroup, channel.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// IncomingMessage — входящее сообщение.
type IncomingMessage struct {
	MessageID int64  `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// CallbackQuery — нажатие кнопки под сообщением бота.
type CallbackQuery struct {
	ID      string           `json:"id"`
	From    User             `json:"from"`
	Message *IncomingMessage `json:"message"`
	Data    string           `json:"data"`
}

// Update — событие из getUpdates.
type Update struct {
	UpdateID int64            `json:"update_id"`
	Message  *IncomingMessage `json:"message"`
	Callback *CallbackQuery   `json:"callback_query"`
}

// Button — кнопка под сообщением: действие (CallbackData) или ссылка (URL).
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// Message — исходящее сообщение в разметке HTML.
type Message struct {
	ChatID   int64
	Text     string
	Keyboard [][]Button
}

// Command — команда в меню бота.
type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type replyMarkup struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

// withMarkup добавляет кнопки, если они есть: «reply_markup: null» Telegram
// принимать не обязан.
func withMarkup(params map[string]any, keyboard [][]Button) map[string]any {
	if keyboard != nil {
		params["reply_markup"] = replyMarkup{InlineKeyboard: keyboard}
	}
	return params
}

var noPreview = map[string]bool{"is_disabled": true}

// GetMe — сам бот: проверка токена и имя для ссылок.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var u User
	err := c.call(ctx, "getMe", struct{}{}, &u)
	return u, err
}

// GetUpdates — долгий опрос: ждёт событий до timeout. offset — следующий
// update_id; всё до него Telegram считает доставленным.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": int(timeout.Seconds()), "allowed_updates": []string{"message", "callback_query"},
	}, &updates)
	return updates, err
}

// SendMessage отправляет сообщение и возвращает его message_id.
func (c *Client) SendMessage(ctx context.Context, m Message) (int64, error) {
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	err := c.call(ctx, "sendMessage", withMarkup(map[string]any{
		"chat_id": m.ChatID, "text": m.Text, "parse_mode": "HTML", "link_preview_options": noPreview,
	}, m.Keyboard), &sent)
	return sent.MessageID, err
}

// EditMessage заменяет текст и кнопки отправленного сообщения.
func (c *Client) EditMessage(ctx context.Context, chatID, messageID int64, text string, keyboard [][]Button) error {
	return c.call(ctx, "editMessageText", withMarkup(map[string]any{
		"chat_id": chatID, "message_id": messageID, "text": text, "parse_mode": "HTML",
		"link_preview_options": noPreview,
	}, keyboard), nil)
}

// AnswerCallback подтверждает нажатие кнопки (иначе у неё крутится
// индикатор) коротким всплывающим текстом.
func (c *Client) AnswerCallback(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// SetCommands задаёт меню команд бота.
func (c *Client) SetCommands(ctx context.Context, commands []Command) error {
	return c.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}
