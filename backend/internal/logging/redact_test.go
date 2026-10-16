package logging_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/logging"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(logging.NewRedactingHandler(slog.NewTextHandler(buf, nil)))
}

func TestRedactsKeyShapedValuesEverywhere(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(&buf)
	log.Error("запрос с ключом sk-ant-api03-abcdefgh12345678 упал",
		"detail", "Bearer sk-proj-ABCDEFGH12345678xyz",
		"error", errors.New("auth failed for sk-or-v1-0123456789abcdef"))
	out := buf.String()
	for _, secret := range []string{"sk-ant-api03-abcdefgh12345678", "sk-proj-ABCDEFGH12345678xyz", "sk-or-v1-0123456789abcdef"} {
		require.NotContains(t, out, secret)
	}
	require.Contains(t, out, "упал", "остальной текст сохраняется")
}

func TestRedactsSensitiveAttributeNames(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf).Info("вход", "password", "hunter2-long-password", "api_key", "plain-value-123",
		"Authorization", "Basic abc", "token", "t0ken-value", "email", "u@example.com")
	out := buf.String()
	for _, secret := range []string{"hunter2-long-password", "plain-value-123", "Basic abc", "t0ken-value"} {
		require.NotContains(t, out, secret)
	}
	require.Contains(t, out, "u@example.com", "несекретные поля не трогаются")
}

func TestRedactsInsideGroupsAndWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(&buf).With("api_key", "preset-secret-value").WithGroup("req")
	log.Info("x", slog.Group("auth", "secret", "nested-secret-value"), "note", "sk-ant-zzzzzzzzzzzz")
	out := buf.String()
	require.NotContains(t, out, "preset-secret-value")
	require.NotContains(t, out, "nested-secret-value")
	require.NotContains(t, out, "sk-ant-zzzzzzzzzzzz")
}

func TestShortSkPrefixedWordsUntouched(t *testing.T) {
	// «sk-» само по себе не секрет: «task-sk-1» не должен превращаться в мусор.
	var buf bytes.Buffer
	newLogger(&buf).Info("ask-sk-ok")
	require.Contains(t, buf.String(), "ask-sk-ok")
}
