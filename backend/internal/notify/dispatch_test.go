package notify_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/notify"
	"github.com/vnovels/linguastack/internal/telegram"
	"github.com/vnovels/linguastack/internal/testdb"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []telegram.Message
	errs []error // ответы по очереди; nil — успех
}

func (f *fakeSender) SendMessage(_ context.Context, m telegram.Message) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return 0, err
		}
	}
	f.sent = append(f.sent, m)
	return int64(len(f.sent)), nil
}

type dispatchEnv struct {
	tx      *gorm.DB
	sender  *fakeSender
	d       *notify.Dispatcher
	project *models.TranslationProject
}

func newDispatchEnv(t *testing.T, publicURL string) *dispatchEnv {
	t.Helper()
	tx := testdb.New(t)
	b := cdm.NewBuilder(cdm.BookMeta{Title: "Алиса <в> Стране & чудес", SourceFormat: "epub"})
	for i := range 6 {
		b.StartChapter(fmt.Sprintf("Глава %d", i+1), 1)
		b.Add(cdm.Paragraph, "x", 0, nil)
	}
	doc := b.Build()
	p := testdb.MakeProject(t, tx, &testdb.ProjectOptions{Document: &doc, Status: models.ProjectRunning})
	require.NoError(t, tx.Model(p).Updates(map[string]any{"segments_total": 873, "segments_done": 390}).Error)
	require.NoError(t, tx.Create(&models.TelegramBinding{UserID: p.OwnerID, TelegramUserID: 5005, ChatID: 5005,
		NotifyChapters: true, NotifyCompleted: true, NotifyErrors: true}).Error)
	cfg, err := config.Load()
	require.NoError(t, err)
	cfg.PublicURL = publicURL
	sender := &fakeSender{}
	d := notify.NewDispatcher(tx, sender, cfg)
	d.SendGap = 0
	return &dispatchEnv{tx: tx, sender: sender, d: d, project: p}
}

func (e *dispatchEnv) add(t *testing.T, kind string, chapter *int) *models.TelegramNotification {
	t.Helper()
	key := kind + ":" + e.project.ID.String()
	if chapter != nil {
		key += fmt.Sprintf(":%d", *chapter)
	}
	n := &models.TelegramNotification{UserID: e.project.OwnerID, ProjectID: e.project.ID, Kind: kind, ChapterIndex: chapter,
		DedupeKey: key, Status: models.NotificationPending, AvailableAt: time.Now().Add(-time.Second)}
	require.NoError(t, e.tx.Create(n).Error)
	return n
}

func ptr(i int) *int { return &i }

func (e *dispatchEnv) run(t *testing.T) int {
	t.Helper()
	n, err := e.d.RunOnce(context.Background())
	require.NoError(t, err)
	return n
}

func (e *dispatchEnv) reload(t *testing.T, n *models.TelegramNotification) models.TelegramNotification {
	t.Helper()
	var got models.TelegramNotification
	require.NoError(t, e.tx.First(&got, "id = ?", n.ID).Error)
	return got
}

func TestCompletedMessage(t *testing.T) {
	e := newDispatchEnv(t, "https://ls.example/")
	require.NoError(t, e.tx.Model(e.project).Updates(map[string]any{"status": models.ProjectCompletedWithWarnings,
		"segments_done": 873, "warnings_count": 2}).Error)
	n := e.add(t, models.NotifyCompleted, nil)
	require.Equal(t, 1, e.run(t))
	require.Len(t, e.sender.sent, 1)
	m := e.sender.sent[0]
	require.Equal(t, int64(5005), m.ChatID)
	require.Contains(t, m.Text, "<b>Алиса &lt;в&gt; Стране &amp; чудес</b>")
	require.Contains(t, m.Text, "на русский готов: 873 из 873 фрагментов, с замечаниями — 2")
	require.Contains(t, m.Text, `href="https://ls.example/translations/`+e.project.ID.String()+`"`)
	require.Contains(t, m.Text, `href="https://ls.example/read/`+e.project.BookID.String()+`?t=`+e.project.ID.String()+`"`)
	got := e.reload(t, n)
	require.Equal(t, models.NotificationSent, got.Status)
	require.NotNil(t, got.SentAt)
	require.Zero(t, e.run(t), "отправленное не повторяется")
}

func TestChaptersCoalesced(t *testing.T) {
	e := newDispatchEnv(t, "https://ls.example")
	for i := range 5 {
		e.add(t, models.NotifyChapter, ptr(i))
	}
	require.Equal(t, 1, e.run(t), "пять глав — одно сообщение")
	text := e.sender.sent[0].Text
	require.Contains(t, text, "готовы главы «Глава 1», «Глава 2», «Глава 3» и ещё 2")
	require.Contains(t, text, "Переведено 390 из 873")
	require.Contains(t, text, "&amp;ch=0")

	e.add(t, models.NotifyChapter, ptr(5))
	require.Equal(t, 1, e.run(t))
	require.Contains(t, e.sender.sent[1].Text, "готова глава «Глава 6»")
}

func TestCompletedSupersedesChapters(t *testing.T) {
	e := newDispatchEnv(t, "")
	require.NoError(t, e.tx.Model(e.project).Update("status", models.ProjectCompleted).Error)
	ch := e.add(t, models.NotifyChapter, ptr(5))
	e.add(t, models.NotifyCompleted, nil)
	require.Equal(t, 1, e.run(t))
	require.Contains(t, e.sender.sent[0].Text, "готов")
	require.Equal(t, models.NotificationSkipped, e.reload(t, ch).Status)
}

func TestFailedMessage(t *testing.T) {
	e := newDispatchEnv(t, "https://ls.example")
	require.NoError(t, e.tx.Model(e.project).Updates(map[string]any{"status": models.ProjectFailed,
		"last_error": "провайдер: ключ отозван"}).Error)
	e.add(t, models.NotifyFailed, nil)
	e.run(t)
	text := e.sender.sent[0].Text
	require.Contains(t, text, "на русский остановлен: провайдер: ключ отозван")
	require.Contains(t, text, "/status")
}

func TestRetryAfter429(t *testing.T) {
	e := newDispatchEnv(t, "")
	e.sender.errs = []error{&telegram.APIError{Code: 429, Description: "Too Many Requests", RetryAfter: time.Minute}}
	n := e.add(t, models.NotifyChapter, ptr(0))
	require.Zero(t, e.run(t))
	got := e.reload(t, n)
	require.Equal(t, models.NotificationPending, got.Status)
	require.Zero(t, got.Attempts, "лимит — не неудача")
	require.True(t, got.AvailableAt.After(time.Now().Add(50*time.Second)))
	require.Zero(t, e.run(t), "ещё рано")
}

func TestTransientBackoffThenFailed(t *testing.T) {
	e := newDispatchEnv(t, "")
	e.d.Backoff = func(int) time.Duration { return -time.Second } // сразу снова
	for range 5 {
		e.sender.errs = append(e.sender.errs, &telegram.APIError{Code: 502, Description: "Bad Gateway"})
	}
	n := e.add(t, models.NotifyChapter, ptr(0))
	for range 4 {
		e.run(t)
		require.Equal(t, models.NotificationPending, e.reload(t, n).Status)
	}
	e.run(t)
	got := e.reload(t, n)
	require.Equal(t, models.NotificationFailed, got.Status)
	require.Equal(t, 5, got.Attempts)
	require.Contains(t, got.LastError, "502")
}

func TestNetworkErrorRetried(t *testing.T) {
	e := newDispatchEnv(t, "")
	e.sender.errs = []error{errors.New("telegram sendMessage: connection refused")}
	n := e.add(t, models.NotifyChapter, ptr(0))
	e.run(t)
	got := e.reload(t, n)
	require.Equal(t, models.NotificationPending, got.Status)
	require.Equal(t, 1, got.Attempts)
}

func TestBlockedBotSkipped(t *testing.T) {
	e := newDispatchEnv(t, "")
	e.sender.errs = []error{&telegram.APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}}
	n := e.add(t, models.NotifyChapter, ptr(0))
	e.run(t)
	got := e.reload(t, n)
	require.Equal(t, models.NotificationSkipped, got.Status)
	require.Contains(t, got.LastError, "blocked")
}

func TestUnlinkedSkipped(t *testing.T) {
	e := newDispatchEnv(t, "")
	n := e.add(t, models.NotifyChapter, ptr(0))
	require.NoError(t, e.tx.Delete(&models.TelegramBinding{}, "user_id = ?", e.project.OwnerID).Error)
	e.run(t)
	require.Empty(t, e.sender.sent)
	require.Equal(t, models.NotificationSkipped, e.reload(t, n).Status)
}

func TestSettingTurnedOffSkipped(t *testing.T) {
	e := newDispatchEnv(t, "")
	n := e.add(t, models.NotifyChapter, ptr(0))
	require.NoError(t, e.tx.Model(&models.TelegramBinding{}).Where("user_id = ?", e.project.OwnerID).
		Update("notify_chapters", false).Error)
	e.run(t)
	require.Empty(t, e.sender.sent)
	require.Equal(t, models.NotificationSkipped, e.reload(t, n).Status)
}

func TestNoPublicURLNoLinks(t *testing.T) {
	e := newDispatchEnv(t, "")
	e.add(t, models.NotifyChapter, ptr(0))
	e.run(t)
	require.False(t, strings.Contains(e.sender.sent[0].Text, "href"))
}
