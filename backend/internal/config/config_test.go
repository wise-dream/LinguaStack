package config_test

import (
	"strings"
	"testing"

	"github.com/vnovels/linguastack/internal/config"
)

func TestLoadAppliesSpecDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}

	// Значения по умолчанию из дополнения к ТЗ §10.
	cases := []struct {
		name string
		got  int64
		want int64
	}{
		{"MaxUploadBytes", cfg.MaxUploadBytes, 50 * 1024 * 1024},
		{"MaxZipEntries", int64(cfg.MaxZipEntries), 2000},
		{"MaxZipTotalBytes", cfg.MaxZipTotalBytes, 200 * 1024 * 1024},
		{"MaxZipRatio", int64(cfg.MaxZipRatio), 100},
		{"MaxTasksPerUser", int64(cfg.MaxTasksPerUser), 3},
		{"MaxProjectsPerUser", int64(cfg.MaxProjectsPerUser), 20},
		{"MaxSegmentAttempts", int64(cfg.MaxSegmentAttempts), 3},
		{"MaxBatchSegments", int64(cfg.MaxBatchSegments), 20},
		{"MaxBatchChars", int64(cfg.MaxBatchChars), 6000},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, ожидалось %d", c.name, c.got, c.want)
		}
	}
}

func TestSecretKeyHasNoBakedInDefault(t *testing.T) {
	// Вшитый в код ключ шифрования рано или поздно уезжает в продакшен.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if cfg.SecretKey != "" {
		t.Errorf("SecretKey = %q, ожидалась пустая строка", cfg.SecretKey)
	}
}

func TestSessionCookieSecureByDefault(t *testing.T) {
	// Опасное значение по умолчанию — ровно та ошибка, которая доезжает
	// до продакшена: переменную окружения там забывают.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if !cfg.SessionCookieSecure {
		t.Error("SessionCookieSecure по умолчанию должен быть true")
	}
}

func TestRequireSecretKeyReportsHowToFixIt(t *testing.T) {
	cfg := config.Settings{SecretKey: ""}
	_, err := cfg.RequireSecretKey()
	if err == nil {
		t.Fatal("ожидалась ошибка при незаданном ключе")
	}
	// Сообщение обязано говорить, что делать, а не только что сломалось.
	if !strings.Contains(err.Error(), "LINGUASTACK_SECRET_KEY") {
		t.Errorf("в сообщении нет имени переменной: %v", err)
	}
	if !strings.Contains(err.Error(), "openssl rand") {
		t.Errorf("в сообщении нет способа сгенерировать ключ: %v", err)
	}
}

func TestRequireSecretKeyRejectsWrongLength(t *testing.T) {
	// AES-256-GCM требует ровно 32 байта; короткий ключ должен
	// обнаруживаться при старте, а не при первой расшифровке.
	cfg := config.Settings{SecretKey: "dG9vLXNob3J0"}
	if _, err := cfg.RequireSecretKey(); err == nil {
		t.Fatal("ожидалась ошибка для ключа неверной длины")
	}
}

func TestRequireSecretKeyAcceptsValidKey(t *testing.T) {
	cfg := config.Settings{SecretKey: config.GenerateSecretKey()}
	key, err := cfg.RequireSecretKey()
	if err != nil {
		t.Fatalf("валидный ключ отвергнут: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("длина ключа = %d, ожидалось 32", len(key))
	}
}

func TestEnvOverridesDefault(t *testing.T) {
	t.Setenv("LINGUASTACK_MAX_TASKS_PER_USER", "7")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	if cfg.MaxTasksPerUser != 7 {
		t.Errorf("MaxTasksPerUser = %d, ожидалось 7", cfg.MaxTasksPerUser)
	}
}

func TestTrustedProxiesParse(t *testing.T) {
	t.Setenv("LINGUASTACK_TRUSTED_PROXIES", " 10.0.0.0/8, 127.0.0.1 ,::1")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	got := make([]string, 0, len(cfg.TrustedProxies))
	for _, p := range cfg.TrustedProxies {
		got = append(got, p.String())
	}
	if strings.Join(got, ",") != "10.0.0.0/8,127.0.0.1/32,::1/128" {
		t.Errorf("TrustedProxies = %v", got)
	}

	t.Setenv("LINGUASTACK_TRUSTED_PROXIES", "10.0.0.0/33")
	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "10.0.0.0/33") {
		t.Errorf("ожидалась ошибка с негодной сетью, получено %v", err)
	}

	t.Setenv("LINGUASTACK_TRUSTED_PROXIES", "")
	cfg, err = config.Load()
	if err != nil || len(cfg.TrustedProxies) != 0 {
		t.Errorf("пусто — нет доверенных прокси: %v %v", cfg.TrustedProxies, err)
	}
}

func TestRateLimitDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() вернул ошибку: %v", err)
	}
	// Дополнение §18 и §10.
	if cfg.RateLimitRPS != 20 || cfg.RateLimitBurst != 60 || cfg.UploadsPerHour != 30 ||
		cfg.RegisterPerHour != 5 || cfg.ResetPerHour != 5 {
		t.Errorf("лимиты по умолчанию: %+v", []any{cfg.RateLimitRPS, cfg.RateLimitBurst,
			cfg.UploadsPerHour, cfg.RegisterPerHour, cfg.ResetPerHour})
	}
}
