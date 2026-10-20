package bot_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/bot"
	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/telegram"
	"github.com/vnovels/linguastack/internal/telegram/telegramtest"
	"github.com/vnovels/linguastack/internal/testdb"
)

const me = int64(5005)

type env struct {
	db  *gorm.DB
	tg  *telegramtest.Server
	bot *bot.Bot
	seq int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tx := testdb.New(t)
	tg := telegramtest.NewServer(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.PublicURL = "https://ls.example"
	cfg.TelegramBotUsername = "linguastack_bot"
	return &env{db: tx, tg: tg, bot: bot.New(tx, tg.Client(), cfg)}
}

func (e *env) send(t *testing.T, chatType string, from int64, text string) string {
	t.Helper()
	e.seq++
	u := telegram.Update{UpdateID: e.seq, Message: &telegram.IncomingMessage{MessageID: e.seq,
		From: telegram.User{ID: from, Username: "kamil"}, Chat: telegram.Chat{ID: from, Type: chatType}, Text: text}}
	before := len(e.tg.Sent())
	require.NoError(t, e.bot.HandleUpdate(context.Background(), u))
	sent := e.tg.Sent()
	require.Len(t, sent, before+1, "одно сообщение в ответ на %q", text)
	require.Equal(t, from, sent[len(sent)-1].ChatID)
	return sent[len(sent)-1].Text
}

func (e *env) press(t *testing.T, from int64, data string) string {
	t.Helper()
	e.seq++
	u := telegram.Update{UpdateID: e.seq, Callback: &telegram.CallbackQuery{ID: fmt.Sprint("cb", e.seq), From: telegram.User{ID: from},
		Message: &telegram.IncomingMessage{MessageID: 77, Chat: telegram.Chat{ID: from, Type: "private"}}, Data: data}}
	require.NoError(t, e.bot.HandleUpdate(context.Background(), u))
	answers := e.tg.Answers()
	require.NotEmpty(t, answers)
	return answers[len(answers)-1]
}

func (e *env) linkCode(t *testing.T, user *models.User, expires time.Time) string {
	t.Helper()
	code, hash := telegram.LinkCode()
	require.NoError(t, e.db.Create(&models.TelegramLinkToken{TokenHash: hash, UserID: user.ID, ExpiresAt: expires}).Error)
	return code
}

func (e *env) binding(t *testing.T, telegramID int64) *models.TelegramBinding {
	t.Helper()
	var b models.TelegramBinding
	if err := e.db.First(&b, "telegram_user_id = ?", telegramID).Error; err != nil {
		return nil
	}
	return &b
}

func TestStartLinksAccount(t *testing.T) {
	e := newEnv(t)
	user := testdb.MakeUser(t, e.db)
	code := e.linkCode(t, user, time.Now().Add(time.Minute))
	reply := e.send(t, "private", me, "/start "+code)
	require.Contains(t, reply, "Готово")
	require.Contains(t, reply, user.Email)
	b := e.binding(t, me)
	require.NotNil(t, b)
	require.Equal(t, user.ID, b.UserID)
	require.Equal(t, me, b.ChatID)
	require.Equal(t, "kamil", b.Username)
	require.True(t, b.NotifyChapters && b.NotifyCompleted && b.NotifyErrors)
	var left int64
	require.NoError(t, e.db.Model(&models.TelegramLinkToken{}).Count(&left).Error)
	require.Zero(t, left, "код одноразовый")
}

func TestStartCodeReuseOrExpired(t *testing.T) {
	e := newEnv(t)
	user := testdb.MakeUser(t, e.db)
	code := e.linkCode(t, user, time.Now().Add(time.Minute))
	e.send(t, "private", me, "/start "+code)
	require.Contains(t, e.send(t, "private", 6006, "/start "+code), "не подходит или устарел")
	require.Nil(t, e.binding(t, 6006))

	other := testdb.MakeUser(t, e.db)
	expired := e.linkCode(t, other, time.Now().Add(-time.Minute))
	require.Contains(t, e.send(t, "private", 7007, "/start "+expired), "не подходит или устарел")
	require.Nil(t, e.binding(t, 7007))
	require.Contains(t, e.send(t, "private", 7007, "/start чужой"), "не подходит или устарел")
}

func TestStartMovesTelegramToNewAccount(t *testing.T) {
	e := newEnv(t)
	first, second := testdb.MakeUser(t, e.db), testdb.MakeUser(t, e.db)
	e.send(t, "private", me, "/start "+e.linkCode(t, first, time.Now().Add(time.Minute)))
	require.NoError(t, e.db.Model(&models.TelegramBinding{}).Where("user_id = ?", first.ID).Update("notify_chapters", false).Error)
	e.send(t, "private", me, "/start "+e.linkCode(t, second, time.Now().Add(time.Minute)))
	b := e.binding(t, me)
	require.Equal(t, second.ID, b.UserID, "Telegram переехал ко второму аккаунту")
	var n int64
	require.NoError(t, e.db.Model(&models.TelegramBinding{}).Where("user_id = ?", first.ID).Count(&n).Error)
	require.Zero(t, n)

	// Тот же аккаунт привязывает тот же Telegram заново — настройки целы.
	require.NoError(t, e.db.Model(&models.TelegramBinding{}).Where("user_id = ?", second.ID).Update("notify_errors", false).Error)
	e.send(t, "private", me, "/start "+e.linkCode(t, second, time.Now().Add(time.Minute)))
	require.False(t, e.binding(t, me).NotifyErrors)
}

func TestGroupChatRefused(t *testing.T) {
	e := newEnv(t)
	user := testdb.MakeUser(t, e.db)
	code := e.linkCode(t, user, time.Now().Add(time.Minute))
	for _, kind := range []string{"group", "supergroup"} {
		require.Contains(t, e.send(t, kind, me, "/start "+code), "только в личном чате")
		require.Contains(t, e.send(t, kind, me, "/status"), "только в личном чате")
	}
	require.Nil(t, e.binding(t, me))
}

func TestStatusUnlinked(t *testing.T) {
	e := newEnv(t)
	reply := e.send(t, "private", me, "/status")
	require.Contains(t, reply, "не привязан")
	require.Contains(t, reply, "https://ls.example/settings")
}

func linkedUser(t *testing.T, e *env) *models.User {
	t.Helper()
	user := testdb.MakeUser(t, e.db)
	require.NoError(t, e.db.Create(&models.TelegramBinding{UserID: user.ID, TelegramUserID: me, ChatID: me,
		NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}).Error)
	return user
}

func project(t *testing.T, e *env, owner *models.User, status string, done, total int, updated time.Time) *models.TranslationProject {
	t.Helper()
	p := testdb.MakeProject(t, e.db, &testdb.ProjectOptions{Owner: &owner.ID, Status: status})
	require.NoError(t, e.db.Model(p).Updates(map[string]any{"segments_done": done, "segments_total": total, "updated_at": updated}).Error)
	return p
}

func TestStatusLists(t *testing.T) {
	e := newEnv(t)
	user := linkedUser(t, e)
	now := time.Now()
	done := project(t, e, user, models.ProjectCompleted, 873, 873, now)
	running := project(t, e, user, models.ProjectRunning, 390, 873, now.Add(-time.Hour))
	paused := project(t, e, user, models.ProjectPaused, 10, 100, now.Add(-2*time.Hour))
	stranger := testdb.MakeUser(t, e.db)
	project(t, e, stranger, models.ProjectRunning, 1, 2, now)

	reply := e.send(t, "private", me, "/status")
	require.Contains(t, reply, "Переводится: 45% (390 из 873)")
	require.Contains(t, reply, "Приостановлен: 10% (10 из 100)")
	require.Contains(t, reply, "Готово")
	require.Contains(t, reply, "→ русский")
	require.Contains(t, reply, "https://ls.example/translations/"+running.ID.String())
	require.Less(t, strings.Index(reply, running.ID.String()), strings.Index(reply, done.ID.String()), "идущие — первыми")
	require.Equal(t, 3, strings.Count(reply, "/translations/"), "только свои переводы")

	kb := e.tg.Sent()[len(e.tg.Sent())-1].Keyboard
	require.Len(t, kb, 2)
	require.Equal(t, "p:"+running.ID.String(), kb[0][0].CallbackData)
	require.Equal(t, "Приостановить 1", kb[0][0].Text)
	require.Equal(t, "r:"+paused.ID.String(), kb[1][0].CallbackData)
	require.Equal(t, "Продолжить 2", kb[1][0].Text)
}

func TestStatusLimitedToTen(t *testing.T) {
	e := newEnv(t)
	user := linkedUser(t, e)
	for i := range 12 {
		project(t, e, user, models.ProjectCompleted, 1, 1, time.Now().Add(-time.Duration(i)*time.Minute))
	}
	reply := e.send(t, "private", me, "/status")
	require.Equal(t, 10, strings.Count(reply, "/translations/"))
	require.Contains(t, reply, "ещё 2")
}

func TestStatusNoProjects(t *testing.T) {
	e := newEnv(t)
	linkedUser(t, e)
	require.Contains(t, e.send(t, "private", me, "/status"), "Переводов пока нет")
}

func TestPauseCallback(t *testing.T) {
	e := newEnv(t)
	user := linkedUser(t, e)
	p := project(t, e, user, models.ProjectRunning, 1, 10, time.Now())
	require.Equal(t, "Перевод приостановлен", e.press(t, me, "p:"+p.ID.String()))
	var got models.TranslationProject
	require.NoError(t, e.db.First(&got, "id = ?", p.ID).Error)
	require.Equal(t, models.ProjectPaused, got.Status)
	edits := e.tg.Edits()
	require.NotEmpty(t, edits, "сообщение со статусом обновлено")
	require.Equal(t, int64(77), edits[len(edits)-1].MessageID)
	require.Contains(t, edits[len(edits)-1].Text, "Приостановлен")

	require.Equal(t, "Перевод продолжается", e.press(t, me, "r:"+p.ID.String()))
	require.NoError(t, e.db.First(&got, "id = ?", p.ID).Error)
	require.Equal(t, models.ProjectQueued, got.Status)
	require.Equal(t, "Сейчас это действие недоступно", e.press(t, me, "r:"+p.ID.String()))
}

func TestForeignCallbackRefused(t *testing.T) {
	e := newEnv(t)
	linkedUser(t, e)
	stranger := testdb.MakeUser(t, e.db)
	p := project(t, e, stranger, models.ProjectRunning, 1, 10, time.Now())
	require.Equal(t, "Перевод не найден", e.press(t, me, "p:"+p.ID.String()))
	require.Equal(t, "Перевод не найден", e.press(t, me, "p:не-uuid"))
	require.Equal(t, "Перевод не найден", e.press(t, me, "x:"+p.ID.String()))
	var got models.TranslationProject
	require.NoError(t, e.db.First(&got, "id = ?", p.ID).Error)
	require.Equal(t, models.ProjectRunning, got.Status)

	require.Equal(t, "Telegram не привязан", e.press(t, 9999, "p:"+p.ID.String()))
}

func TestHelpUnlinkUnknown(t *testing.T) {
	e := newEnv(t)
	require.Contains(t, e.send(t, "private", me, "/help"), "/status")
	require.Contains(t, e.send(t, "private", me, "/start"), "Настройки")
	linkedUser(t, e)
	require.Contains(t, e.send(t, "private", me, "/start@linguastack_bot"), "уже привязан")
	require.Contains(t, e.send(t, "private", me, "привет"), "/help")
	require.Contains(t, e.send(t, "private", me, "/unlink"), "отвязан")
	require.Nil(t, e.binding(t, me))
	require.Contains(t, e.send(t, "private", me, "/unlink"), "не привязан")
}

func TestRunPollsAndConfirms(t *testing.T) {
	e := newEnv(t)
	e.tg.Push(telegram.Update{UpdateID: 1, Message: &telegram.IncomingMessage{From: telegram.User{ID: me},
		Chat: telegram.Chat{ID: me, Type: "private"}, Text: "/help"}})
	e.bot.PollTimeout = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		for len(e.tg.Sent()) == 0 && ctx.Err() == nil {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	require.NoError(t, e.bot.Run(ctx))
	require.Len(t, e.tg.Sent(), 1, "обновление обработано один раз: offset подтверждает его")
}

func TestStatusFitsTelegramLimit(t *testing.T) {
	// Telegram не принимает сообщения длиннее 4096 символов: десять книг с
	// длинными названиями не должны оставить /status без ответа.
	e := newEnv(t)
	user := linkedUser(t, e)
	long := strings.Repeat("Очень длинное название книги ", 12)
	for i := range 10 {
		p := project(t, e, user, models.ProjectFailed, 1, 2, time.Now().Add(-time.Duration(i)*time.Minute))
		require.NoError(t, e.db.Model(&models.Book{}).Where("id = ?", p.BookID).Update("title", long).Error)
		require.NoError(t, e.db.Model(p).Update("last_error", strings.Repeat("ошибка провайдера ", 30)).Error)
	}
	reply := e.send(t, "private", me, "/status")
	require.LessOrEqual(t, utf8.RuneCountInString(reply), 4096)
	require.Equal(t, 10, strings.Count(reply, "/translations/"))
}
