package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/mail"
	"github.com/vnovels/linguastack/internal/telegram/telegramtest"
)

// mailbox — отправитель писем, который их запоминает. Сам SMTP проверяет
// пакет mail на поддельном сервере.
type mailbox struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *mailbox) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailbox) messages() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.sent...)
}

const publicURL = "https://ls.example"

func newResetEnv(t *testing.T, adjust func(*httpapi.Deps)) (*testEnv, *mailbox) {
	t.Helper()
	box := &mailbox{}
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.Mailer = box
		d.Cfg.PublicURL = publicURL
		d.Cfg.OllamaBaseURL = noOllama
		if adjust != nil {
			adjust(d)
		}
	})
	return env, box
}

func requestReset(c *apiClient, email string) *response {
	return c.do(http.MethodPost, "/api/auth/password-reset", map[string]string{"email": email})
}

func confirmReset(c *apiClient, token, password string) int {
	return c.do(http.MethodPost, "/api/auth/password-reset/confirm",
		map[string]string{"token": token, "password": password}).Status
}

// tokenFrom достаёт код из ссылки сброса.
func tokenFrom(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, publicURL+"/reset-password?token=")
	require.GreaterOrEqual(t, start, 0, "в тексте нет ссылки: %s", text)
	link := text[start:]
	if end := strings.IndexAny(link, " \n\"<"); end > 0 {
		link = link[:end]
	}
	u, err := url.Parse(link)
	require.NoError(t, err)
	token := u.Query().Get("token")
	require.NotEmpty(t, token)
	return token
}

func waitMail(t *testing.T, box *mailbox, n int) []mail.Message {
	t.Helper()
	require.Eventually(t, func() bool { return len(box.messages()) >= n }, 5*time.Second, 10*time.Millisecond)
	return box.messages()
}

func TestResetRequestSameAnswerForUnknownEmail(t *testing.T) {
	env, box := newResetEnv(t, nil)
	env.signedIn(t, "known@example.com")
	c := env.client(t)
	known := requestReset(c, "known@example.com")
	unknown := requestReset(c, "nobody@example.com")
	require.Equal(t, http.StatusAccepted, known.Status)
	require.Equal(t, known.Status, unknown.Status)
	require.Equal(t, string(known.Body), string(unknown.Body), "по ответу не узнать, есть ли аккаунт")

	msgs := waitMail(t, box, 1)
	time.Sleep(50 * time.Millisecond)
	require.Len(t, box.messages(), 1, "письмо только существующему")
	require.Equal(t, "known@example.com", msgs[0].To)
	require.Equal(t, http.StatusUnprocessableEntity, requestReset(c, "не адрес").Status)
}

func TestResetViaMail(t *testing.T) {
	env, box := newResetEnv(t, nil)
	env.signedIn(t, "mail@example.com")
	require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "Mail@Example.com").Status)
	msg := waitMail(t, box, 1)[0]
	require.Equal(t, "mail@example.com", msg.To)
	require.Contains(t, msg.Subject, "Сброс пароля")
	require.Contains(t, msg.Body, "1 час")
	token := tokenFrom(t, msg.Body)

	require.Equal(t, http.StatusNoContent, confirmReset(env.client(t), token, newPassword))
	require.Equal(t, http.StatusOK, env.client(t).login("mail@example.com", newPassword).Status)
}

func TestResetViaTelegram(t *testing.T) {
	tg := telegramtest.NewServer(t)
	env, box := newResetEnv(t, func(d *httpapi.Deps) {
		d.Mailer = nil // почта не настроена — только Telegram
		d.Cfg.TelegramAPIURL, d.Cfg.TelegramBotToken, d.Cfg.TelegramBotUsername = tg.URL(), telegramtest.Token, "ls_bot"
	})
	env.signedIn(t, "tg@example.com")
	var user models.User
	require.NoError(t, env.db.First(&user, "email = ?", "tg@example.com").Error)
	require.NoError(t, env.db.Create(&models.TelegramBinding{UserID: user.ID, TelegramUserID: 42, ChatID: 4242,
		NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}).Error)

	require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "tg@example.com").Status)
	require.Eventually(t, func() bool { return len(tg.Sent()) == 1 }, 5*time.Second, 10*time.Millisecond)
	sent := tg.Sent()[0]
	require.Equal(t, int64(4242), sent.ChatID)
	require.Contains(t, sent.Text, "Сброс пароля")
	token := tokenFrom(t, sent.Text)
	require.Equal(t, http.StatusNoContent, confirmReset(env.client(t), token, newPassword))
	require.Empty(t, box.messages())
}

func TestResetConfirm(t *testing.T) {
	env, box := newResetEnv(t, nil)
	tab := env.signedIn(t, "c@example.com")
	require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "c@example.com").Status)
	token := tokenFrom(t, waitMail(t, box, 1)[0].Body)
	anon := env.client(t)

	require.Equal(t, http.StatusUnprocessableEntity, confirmReset(anon, token, "short"), "короткий пароль")
	require.Equal(t, http.StatusUnprocessableEntity, confirmReset(anon, "forged-token", newPassword))
	require.Equal(t, http.StatusNoContent, confirmReset(anon, token, newPassword), "короткий пароль код не сжёг")
	require.Equal(t, http.StatusUnauthorized, tab.do(http.MethodGet, "/api/me", nil).Status, "все сессии погашены")
	require.Equal(t, http.StatusUnauthorized, env.client(t).login("c@example.com", testPassword).Status)
	require.Equal(t, http.StatusOK, env.client(t).login("c@example.com", newPassword).Status)
	require.Equal(t, http.StatusUnprocessableEntity, confirmReset(anon, token, "another-password-123"), "код одноразовый")

	// Просроченный код не работает.
	require.NoError(t, env.db.Exec("DELETE FROM password_resets").Error)
	require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "c@example.com").Status)
	expired := tokenFrom(t, waitMail(t, box, 2)[1].Body)
	require.NoError(t, env.db.Exec("UPDATE password_resets SET expires_at = now() - interval '1 second'").Error)
	require.Equal(t, http.StatusUnprocessableEntity, confirmReset(anon, expired, "another-password-123"))
}

func TestResetThrottledPerAccount(t *testing.T) {
	// Чужой почтовый ящик не завалить письмами: не чаще раза в минуту.
	env, box := newResetEnv(t, nil)
	env.signedIn(t, "spam@example.com")
	for range 3 {
		require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "spam@example.com").Status)
	}
	waitMail(t, box, 1)
	time.Sleep(50 * time.Millisecond)
	require.Len(t, box.messages(), 1)
	var codes int64
	require.NoError(t, env.db.Model(&models.PasswordReset{}).Count(&codes).Error)
	require.EqualValues(t, 1, codes, "у аккаунта один действующий код")
}

func TestResetRateLimitedPerIP(t *testing.T) {
	env, _ := newResetEnv(t, func(d *httpapi.Deps) { d.Cfg.ResetPerHour = 2 })
	c := env.client(t)
	require.Equal(t, http.StatusAccepted, requestReset(c, "a@example.com").Status)
	require.Equal(t, http.StatusAccepted, requestReset(c, "b@example.com").Status)
	r := requestReset(c, "c@example.com")
	require.Equal(t, http.StatusTooManyRequests, r.Status)
	require.NotEmpty(t, r.Header.Get("Retry-After"))
}

func TestAdminIssuesResetLink(t *testing.T) {
	env, box := newResetEnv(t, nil)
	admin := env.signedIn(t, "admin@example.com")
	env.promote(t, "admin@example.com")
	user := env.signedIn(t, "forgot@example.com")
	id := user.meID(t)

	r := admin.do(http.MethodPost, "/api/admin/users/"+id+"/password-reset", nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var out struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	r.JSON(t, &out)
	require.True(t, strings.HasPrefix(out.URL, publicURL+"/reset-password?token="), out.URL)
	require.WithinDuration(t, time.Now().Add(time.Hour), out.ExpiresAt, time.Minute)
	require.Equal(t, http.StatusNoContent, confirmReset(env.client(t), tokenFrom(t, out.URL), newPassword))
	require.Empty(t, box.messages(), "ссылку передаёт администратор, письмо не уходит")

	require.Equal(t, http.StatusNotFound,
		admin.do(http.MethodPost, "/api/admin/users/00000000-0000-0000-0000-000000000000/password-reset", nil).Status)
}

func TestResetDisabledUserNotSent(t *testing.T) {
	env, box := newResetEnv(t, nil)
	env.signedIn(t, "off@example.com")
	require.NoError(t, env.db.Model(&models.User{}).Where("email = ?", "off@example.com").Update("is_active", false).Error)
	require.Equal(t, http.StatusAccepted, requestReset(env.client(t), "off@example.com").Status)
	time.Sleep(100 * time.Millisecond)
	require.Empty(t, box.messages())
	var codes int64
	require.NoError(t, env.db.Model(&models.PasswordReset{}).Count(&codes).Error)
	require.Zero(t, codes)
}
