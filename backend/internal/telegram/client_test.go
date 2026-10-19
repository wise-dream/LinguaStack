package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/telegram"
)

const token = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawQ"

type call struct {
	method string
	body   map[string]any
}

func server(t *testing.T, reply func(method string, body map[string]any) (int, string)) (*telegram.Client, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, ok := strings.CutPrefix(r.URL.Path, "/bot"+token+"/")
		require.True(t, ok, "путь %s", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, call{method: method, body: body})
		status, payload := reply(method, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(srv.Close)
	return telegram.NewClient(srv.URL, token, srv.Client()), &calls
}

func TestSendMessageHTML(t *testing.T) {
	c, calls := server(t, func(string, map[string]any) (int, string) {
		return 200, `{"ok":true,"result":{"message_id":42,"chat":{"id":7,"type":"private"}}}`
	})
	id, err := c.SendMessage(context.Background(), telegram.Message{ChatID: 7, Text: "<b>Книга</b>",
		Keyboard: [][]telegram.Button{{{Text: "Пауза", CallbackData: "p:1"}, {Text: "Открыть", URL: "https://x.example/a"}}}})
	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	got := (*calls)[0]
	require.Equal(t, "sendMessage", got.method)
	require.EqualValues(t, 7, got.body["chat_id"])
	require.Equal(t, "<b>Книга</b>", got.body["text"])
	require.Equal(t, "HTML", got.body["parse_mode"])
	require.Equal(t, map[string]any{"is_disabled": true}, got.body["link_preview_options"])
	require.Equal(t, map[string]any{"inline_keyboard": []any{[]any{
		map[string]any{"text": "Пауза", "callback_data": "p:1"},
		map[string]any{"text": "Открыть", "url": "https://x.example/a"},
	}}}, got.body["reply_markup"])
}

func TestGetUpdatesOffsetTimeout(t *testing.T) {
	c, calls := server(t, func(string, map[string]any) (int, string) {
		return 200, `{"ok":true,"result":[
			{"update_id":10,"message":{"message_id":1,"from":{"id":5,"username":"kamil"},"chat":{"id":5,"type":"private"},"text":"/start abc"}},
			{"update_id":11,"callback_query":{"id":"cb1","from":{"id":5},"message":{"message_id":2,"chat":{"id":5,"type":"private"}},"data":"p:x"}}]}`
	})
	updates, err := c.GetUpdates(context.Background(), 10, 25*time.Second)
	require.NoError(t, err)
	require.Len(t, updates, 2)
	require.Equal(t, int64(10), updates[0].UpdateID)
	require.Equal(t, "/start abc", updates[0].Message.Text)
	require.Equal(t, "kamil", updates[0].Message.From.Username)
	require.Equal(t, "private", updates[0].Message.Chat.Type)
	require.Equal(t, "p:x", updates[1].Callback.Data)
	require.Equal(t, int64(2), updates[1].Callback.Message.MessageID)
	body := (*calls)[0].body
	require.EqualValues(t, 10, body["offset"])
	require.EqualValues(t, 25, body["timeout"])
	require.Equal(t, []any{"message", "callback_query"}, body["allowed_updates"])
}

func TestAPIErrorRetryAfter(t *testing.T) {
	c, _ := server(t, func(string, map[string]any) (int, string) {
		return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
	})
	_, err := c.SendMessage(context.Background(), telegram.Message{ChatID: 1, Text: "x"})
	var apiErr *telegram.APIError
	require.True(t, errors.As(err, &apiErr), "err = %v", err)
	require.Equal(t, 429, apiErr.Code)
	require.Equal(t, 7*time.Second, apiErr.RetryAfter)
	require.True(t, apiErr.Temporary())
}

func TestAPIErrorForbidden(t *testing.T) {
	c, _ := server(t, func(string, map[string]any) (int, string) {
		return 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	})
	err := c.AnswerCallback(context.Background(), "cb", "x")
	var apiErr *telegram.APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, 403, apiErr.Code)
	require.False(t, apiErr.Temporary())
	require.NotContains(t, err.Error(), token)
}

func TestNetworkErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c := telegram.NewClient(srv.URL, token, nil)
	_, err := c.GetMe(context.Background())
	require.Error(t, err)
	require.NotContains(t, err.Error(), token)
	require.NotContains(t, err.Error(), "AAHdq")
	require.Contains(t, err.Error(), "getMe")
}

func TestEditAndCommands(t *testing.T) {
	c, calls := server(t, func(string, map[string]any) (int, string) { return 200, `{"ok":true,"result":true}` })
	require.NoError(t, c.EditMessage(context.Background(), 7, 42, "текст", nil))
	require.NoError(t, c.SetCommands(context.Background(), []telegram.Command{{Command: "status", Description: "Переводы"}}))
	require.Equal(t, "editMessageText", (*calls)[0].method)
	require.EqualValues(t, 42, (*calls)[0].body["message_id"])
	require.Equal(t, "setMyCommands", (*calls)[1].method)
	require.Equal(t, []any{map[string]any{"command": "status", "description": "Переводы"}}, (*calls)[1].body["commands"])
}

func TestNoKeyboardOmitsMarkup(t *testing.T) {
	c, calls := server(t, func(string, map[string]any) (int, string) { return 200, `{"ok":true,"result":{"message_id":1}}` })
	_, err := c.SendMessage(context.Background(), telegram.Message{ChatID: 1, Text: "x"})
	require.NoError(t, err)
	require.NoError(t, c.EditMessage(context.Background(), 1, 1, "y", nil))
	for _, cl := range *calls {
		_, has := cl.body["reply_markup"]
		require.False(t, has, cl.method)
	}
}
