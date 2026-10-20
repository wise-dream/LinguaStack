package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/config"
)

func TestBuildMailer(t *testing.T) {
	m, err := buildMailer(config.Settings{})
	require.NoError(t, err)
	require.Nil(t, m, "почта не настроена — сброс идёт только в Telegram и через администратора")

	_, err = buildMailer(config.Settings{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPFrom: "noreply@example.com", SMTPTLS: "ssl"})
	require.Error(t, err, "опечатка в настройке — ошибка запуска")

	m, err = buildMailer(config.Settings{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPFrom: "noreply@example.com", SMTPTLS: "starttls"})
	require.NoError(t, err)
	require.NotNil(t, m)
}
