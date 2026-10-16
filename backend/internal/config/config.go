// Package config держит настройки приложения.
//
// Все лимиты вынесены сюда, а не зашиты в код: ТЗ §13 требует настраиваемых
// ограничений, а конкретные значения по умолчанию зафиксированы в
// docs/spec/SPEC-ADDENDUM.md §10.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// SecretKeyLength — размер ключа AES-256-GCM в байтах.
const SecretKeyLength = 32

const generateHint = "сгенерировать: openssl rand -base64 32"

type Settings struct {
	DatabaseURL     string `env:"LINGUASTACK_DATABASE_URL" envDefault:"postgres://linguastack:linguastack@localhost:5434/linguastack?sslmode=disable"`
	TestDatabaseURL string `env:"LINGUASTACK_TEST_DATABASE_URL" envDefault:"postgres://linguastack:linguastack@localhost:5434/linguastack_test?sslmode=disable"`

	// Ключ шифрования пользовательских API-ключей (дополнение §8).
	// Намеренно без значения по умолчанию: вшитый в код ключ рано или поздно
	// уезжает в продакшен. Пустая строка означает «не настроен», и обращение
	// к нему падает с понятным сообщением — см. RequireSecretKey.
	SecretKey string `env:"LINGUASTACK_SECRET_KEY"`

	StorageRoot   string `env:"LINGUASTACK_STORAGE_ROOT" envDefault:"./storage"`
	OllamaBaseURL string `env:"LINGUASTACK_OLLAMA_BASE_URL" envDefault:"http://localhost:11434"`
	// Контекстное окно Ollama (ТЗ §6.2). Задаётся явно: значение сервера по
	// умолчанию меньше батча, и Ollama молча обрезает запрос.
	OllamaNumCtx int `env:"LINGUASTACK_OLLAMA_NUM_CTX" envDefault:"8192"`
	// Разрешить OpenAI-совместимые сервисы на частных адресах (локальный
	// vLLM, LM Studio). По умолчанию запрещено: адрес задаёт пользователь,
	// и без запрета сервер можно направить во внутреннюю сеть (SSRF).
	AllowPrivateProviderURLs bool `env:"LINGUASTACK_ALLOW_PRIVATE_PROVIDER_URLS" envDefault:"false"`
	// Тайм-аут одного запроса к модели. Локальная генерация батча на CPU
	// может идти минутами.
	ProviderTimeout time.Duration `env:"LINGUASTACK_PROVIDER_TIMEOUT" envDefault:"10m"`
	HTTPAddr        string        `env:"LINGUASTACK_HTTP_ADDR" envDefault:":8000"`

	// Cookie сессии только по HTTPS. По умолчанию true: продакшен, где
	// забыли переменную, не должен отдавать сессию по открытому каналу.
	// false выставляется явно — для локальной разработки по http и тестов.
	SessionCookieSecure bool          `env:"LINGUASTACK_SESSION_COOKIE_SECURE" envDefault:"true"`
	SessionTTL          time.Duration `env:"LINGUASTACK_SESSION_TTL" envDefault:"720h"`

	// --- лимиты, дополнение §10 ---
	MaxUploadBytes     int64         `env:"LINGUASTACK_MAX_UPLOAD_BYTES" envDefault:"52428800"`
	MaxZipEntries      int           `env:"LINGUASTACK_MAX_ZIP_ENTRIES" envDefault:"2000"`
	MaxZipTotalBytes   int64         `env:"LINGUASTACK_MAX_ZIP_TOTAL_BYTES" envDefault:"209715200"`
	MaxZipRatio        int           `env:"LINGUASTACK_MAX_ZIP_RATIO" envDefault:"100"`
	MaxTasksPerUser    int           `env:"LINGUASTACK_MAX_TASKS_PER_USER" envDefault:"3"`
	MaxProjectsPerUser int           `env:"LINGUASTACK_MAX_PROJECTS_PER_USER" envDefault:"20"`
	MaxSegmentAttempts int           `env:"LINGUASTACK_MAX_SEGMENT_ATTEMPTS" envDefault:"3"`
	LoginMaxAttempts   int           `env:"LINGUASTACK_LOGIN_MAX_ATTEMPTS" envDefault:"10"`
	LoginWindow        time.Duration `env:"LINGUASTACK_LOGIN_WINDOW" envDefault:"15m"`

	// --- сегментация и батчинг, дополнение §2 ---
	MaxBatchSegments int `env:"LINGUASTACK_MAX_BATCH_SEGMENTS" envDefault:"20"`
	MaxBatchChars    int `env:"LINGUASTACK_MAX_BATCH_CHARS" envDefault:"6000"`

	// --- очередь, дополнение §7 ---
	TaskLease time.Duration `env:"LINGUASTACK_TASK_LEASE" envDefault:"2m"`
}

// Load читает настройки из окружения, подставляя значения по умолчанию.
func Load() (Settings, error) {
	return env.ParseAs[Settings]()
}

// RequireSecretKey возвращает ключ шифрования или ошибку с инструкцией.
//
// Проверка отложена до момента реального использования: сервис должен
// запускаться, отвечать на health-check и применять миграции без ключа,
// но любая работа с пользовательскими секретами без него обязана прерваться.
func (s Settings) RequireSecretKey() ([]byte, error) {
	if s.SecretKey == "" {
		return nil, fmt.Errorf("LINGUASTACK_SECRET_KEY не задан; %s", generateHint)
	}
	key, err := base64.StdEncoding.DecodeString(s.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("LINGUASTACK_SECRET_KEY не является base64 (%w); %s", err, generateHint)
	}
	if len(key) != SecretKeyLength {
		return nil, fmt.Errorf(
			"LINGUASTACK_SECRET_KEY должен содержать %d байт после декодирования, получено %d; %s",
			SecretKeyLength, len(key), generateHint,
		)
	}
	return key, nil
}

// GenerateSecretKey создаёт новый ключ — для тестов и подсказок в документации.
func GenerateSecretKey() string {
	key := make([]byte, SecretKeyLength)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("crypto/rand недоступен: %v", err))
	}
	return base64.StdEncoding.EncodeToString(key)
}
