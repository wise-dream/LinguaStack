package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/telegram/telegramtest"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestBotDisabledWaitsForSignal(t *testing.T) {
	t.Setenv("LINGUASTACK_TELEGRAM_BOT_TOKEN", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var log syncBuffer
	start := time.Now()
	require.NoError(t, run(ctx, &log))
	require.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond, "ждёт сигнала, а не выходит: иначе compose перезапускал бы его по кругу")
	require.Contains(t, log.String(), "бот выключен")
}

func TestBotStartsWithoutLeakingToken(t *testing.T) {
	tg := telegramtest.NewServer(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	t.Setenv("LINGUASTACK_DATABASE_URL", cfg.TestDatabaseURL)
	t.Setenv("LINGUASTACK_TELEGRAM_BOT_TOKEN", telegramtest.Token)
	t.Setenv("LINGUASTACK_TELEGRAM_BOT_USERNAME", "other_bot")
	t.Setenv("LINGUASTACK_TELEGRAM_API_URL", tg.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var log syncBuffer
	go func() {
		for !strings.Contains(log.String(), "бот запущен") && ctx.Err() == nil {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	require.NoError(t, run(ctx, &log))
	out := log.String()
	require.Contains(t, out, "linguastack_bot")
	require.Contains(t, out, "не совпадает", "имя в настройках расходится с ботом")
	require.NotContains(t, out, "AAHdq")
}
