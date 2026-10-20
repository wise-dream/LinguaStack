package main

import (
	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/mail"
)

// buildMailer — отправка писем по LINGUASTACK_SMTP_*; без адреса сервера
// почта выключена (nil).
func buildMailer(cfg config.Settings) (mail.Sender, error) {
	if cfg.SMTPHost == "" {
		return nil, nil
	}
	m, err := mail.NewSMTP(mail.Config{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		From: cfg.SMTPFrom, TLS: cfg.SMTPTLS,
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}
