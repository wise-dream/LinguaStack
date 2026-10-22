package httpapi_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
)

// Сквозной цикл ТЗ §17: импорт → структурирование → перевод → проверка →
// чтение — через настоящие API и worker.

const e2eBook = "# Введение\n\nThis chapter explains the queue.\n\n```python\nprint(\"not translated\")\n```\n\n" +
	"# Детали\n\nThe worker claims a task. See [docs](https://example.com/docs) and call `claim()`.\n"

func TestFullCycleImportTranslateValidateRead(t *testing.T) {
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, e2eBook)
	require.Equal(t, []string{"Введение", "Детали"}, tocTitles(t, e.c, bookID))

	p := e.project(t, bookID)
	e.runWorker(t)

	done := e.get(t, p.ID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, done.SegmentsTotal, done.SegmentsDone)
	require.Positive(t, done.SegmentsTotal)

	var all []string
	for _, s := range e.segments(t, p.ID) {
		require.Equal(t, "completed", s.Status)
		all = append(all, *s.TranslatedText)
	}
	joined := strings.Join(all, "\n")
	require.NotContains(t, joined, `print("not translated")`, "код в перевод не уходит")
	require.Contains(t, joined, "[docs](https://example.com/docs)", "ссылка сохранена")
	require.Contains(t, joined, "`claim()`", "встроенный код сохранён дословно")
}

func TestOriginalUnchangedAfterTranslation(t *testing.T) {
	// ТЗ §15.9: оригинал сохраняется без изменений.
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, e2eBook)
	e.project(t, bookID)
	e.runWorker(t)

	var stored []byte
	for _, f := range filesUnder(t, e.storage) {
		if strings.HasSuffix(f, "original.md") {
			var err error
			stored, err = os.ReadFile(filepath.Join(e.storage, f))
			require.NoError(t, err)
		}
	}
	require.Equal(t, e2eBook, string(stored))
}

func TestRealEPUBTranslatesEveryParagraph(t *testing.T) {
	// Настоящая книга (Project Gutenberg): каждый переводимый блок
	// импортированного EPUB доходит до перевода.
	e := newTranslationEnv(t, nil)
	raw, err := os.ReadFile("../importers/testdata/real/alice.epub")
	require.NoError(t, err)
	book := uploadBook(t, e.c, "alice.epub", string(raw))
	require.Equal(t, "Alice's Adventures in Wonderland", book.Title)

	p := e.project(t, book.ID)
	e.runWorker(t)
	done := e.get(t, p.ID)
	require.Equal(t, "completed", done.Status)
	require.Equal(t, book.Translatable, done.SegmentsTotal)
	require.Equal(t, book.Translatable, done.SegmentsDone)
	require.Greater(t, done.SegmentsDone, 700)
}

func TestLiveOllamaEndToEnd(t *testing.T) {
	if os.Getenv("LINGUASTACK_LIVE_OLLAMA") != "1" {
		t.Skip("живой Ollama не включён (LINGUASTACK_LIVE_OLLAMA=1)")
	}
	e := newTranslationEnv(t, nil)
	bookID := e.book(t, "# Chapter One\n\nAlice was beginning to get very tired.\n\nCall `run()` before reading [the guide](https://example.com).")
	r := e.create(t, bookID, map[string]any{"provider_config_id": e.ollamaConfig(t), "model_id": "qwen3:8b"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	var p projectBody
	r.JSON(t, &p)

	w := queue.NewWorker("live", e.db, queue.ConfigResolver(e.db, nil, providers.BuildOptions{
		OllamaBaseURL: "http://localhost:11434", OllamaNumCtx: 8192, Timeout: 5 * time.Minute,
	}, false), e.cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	for {
		worked, err := w.RunOnce(ctx)
		require.NoError(t, err)
		if !worked {
			break
		}
	}
	done := e.get(t, p.ID)
	require.Equal(t, models.ProjectCompleted, done.Status, "ошибка: %s", done.LastError)
	for _, s := range e.segments(t, p.ID) {
		t.Logf("%s → %s", s.SourceText, *s.TranslatedText)
	}
	segs := e.segments(t, p.ID)
	require.Contains(t, *segs[2].TranslatedText, "`run()`")
	require.Contains(t, *segs[2].TranslatedText, "(https://example.com)")
}
