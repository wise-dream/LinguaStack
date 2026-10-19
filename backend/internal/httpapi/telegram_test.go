package httpapi_test

import (
	"crypto/sha256"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
)

type telegramBody struct {
	Enabled         bool   `json:"enabled"`
	BotUsername     string `json:"bot_username"`
	Linked          bool   `json:"linked"`
	Username        string `json:"username"`
	LinkedAt        string `json:"linked_at"`
	NotifyChapters  bool   `json:"notify_chapters"`
	NotifyCompleted bool   `json:"notify_completed"`
	NotifyErrors    bool   `json:"notify_errors"`
}

func telegramEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Cfg.TelegramBotToken = "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawQ"
		d.Cfg.TelegramBotUsername = "linguastack_bot"
	})
}

func getTelegram(t *testing.T, c *apiClient) telegramBody {
	t.Helper()
	var out telegramBody
	r := c.do(http.MethodGet, "/api/telegram", nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	r.JSON(t, &out)
	return out
}

func TestTelegramDisabled(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	require.False(t, getTelegram(t, c).Enabled)
	r := c.do(http.MethodPost, "/api/telegram/link", nil)
	require.Equal(t, http.StatusServiceUnavailable, r.Status)
	require.Contains(t, string(r.Body), "Telegram-бот не настроен")
}

var linkRe = regexp.MustCompile(`^https://t\.me/linguastack_bot\?start=([A-Za-z0-9_-]{43})$`)

func TestTelegramLinkURL(t *testing.T) {
	env := telegramEnv(t)
	c := env.signedIn(t, "u@example.com")
	state := getTelegram(t, c)
	require.True(t, state.Enabled)
	require.Equal(t, "linguastack_bot", state.BotUsername)
	require.False(t, state.Linked)

	link := func() string {
		var out struct {
			URL       string `json:"url"`
			ExpiresAt string `json:"expires_at"`
		}
		r := c.do(http.MethodPost, "/api/telegram/link", nil)
		require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
		r.JSON(t, &out)
		m := linkRe.FindStringSubmatch(out.URL)
		require.NotNil(t, m, out.URL)
		require.NotEmpty(t, out.ExpiresAt)
		return m[1]
	}
	first := link()
	var tokens []models.TelegramLinkToken
	require.NoError(t, env.db.Find(&tokens).Error)
	require.Len(t, tokens, 1)
	hash := sha256.Sum256([]byte(first))
	require.Equal(t, hash[:], tokens[0].TokenHash, "в БД — хеш, а не сам код")

	second := link()
	require.NotEqual(t, first, second)
	require.NoError(t, env.db.Find(&tokens).Error)
	require.Len(t, tokens, 1, "новый код заменяет прежний")
}

func bindTelegram(t *testing.T, env *testEnv, email string) {
	t.Helper()
	var user models.User
	require.NoError(t, env.db.First(&user, "email = ?", email).Error)
	require.NoError(t, env.db.Create(&models.TelegramBinding{UserID: user.ID, TelegramUserID: 5005, ChatID: 5005,
		Username: "kamil", NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}).Error)
}

func TestTelegramSettings(t *testing.T) {
	env := telegramEnv(t)
	c := env.signedIn(t, "u@example.com")
	r := c.do(http.MethodPatch, "/api/telegram", map[string]any{"notify_chapters": false})
	require.Equal(t, http.StatusConflict, r.Status, "без привязки настраивать нечего")

	bindTelegram(t, env, "u@example.com")
	r = c.do(http.MethodPatch, "/api/telegram", map[string]any{"notify_chapters": false})
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	state := getTelegram(t, c)
	require.True(t, state.Linked)
	require.Equal(t, "kamil", state.Username)
	require.NotEmpty(t, state.LinkedAt)
	require.False(t, state.NotifyChapters)
	require.True(t, state.NotifyCompleted)
	require.True(t, state.NotifyErrors)

	r = c.do(http.MethodPatch, "/api/telegram", map[string]any{"notify_everything": true})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, "неизвестное поле")
}

func TestTelegramUnlink(t *testing.T) {
	env := telegramEnv(t)
	c := env.signedIn(t, "u@example.com")
	bindTelegram(t, env, "u@example.com")
	require.True(t, getTelegram(t, c).Linked)
	require.Equal(t, http.StatusNoContent, c.do(http.MethodDelete, "/api/telegram", nil).Status)
	require.False(t, getTelegram(t, c).Linked)
	require.Equal(t, http.StatusNoContent, c.do(http.MethodDelete, "/api/telegram", nil).Status, "повторно — тоже 204")
}

func TestTelegramIsPerUser(t *testing.T) {
	env := telegramEnv(t)
	owner := env.signedIn(t, "u@example.com")
	bindTelegram(t, env, "u@example.com")
	other := env.signedIn(t, "other@example.com")
	require.False(t, getTelegram(t, other).Linked, "чужая привязка не видна")
	require.Equal(t, http.StatusNoContent, other.do(http.MethodDelete, "/api/telegram", nil).Status)
	require.True(t, getTelegram(t, owner).Linked, "и не удаляется чужим запросом")
}

func TestManualSegmentEditSendsNoNotification(t *testing.T) {
	env := newTranslationEnv(t, nil)
	p := env.translated(t)
	bindTelegram(t, env.testEnv, "u@example.com")
	seg := env.segments(t, p.ID)[0]
	r := env.c.do(http.MethodPatch, "/api/segments/"+seg.ID, map[string]any{"translated_text": "Правка."})
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var n int64
	require.NoError(t, env.db.Model(&models.TelegramNotification{}).Count(&n).Error)
	require.Zero(t, n, "уведомления — только из worker, не от каждой правки")
}
