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
	"net/netip"
	"strings"
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
	// Ollama сервера — общий процессор: по умолчанию ею пользуется только
	// администратор, остальные подключают свои сервисы со своим ключом.
	OllamaAdminOnly bool `env:"LINGUASTACK_OLLAMA_ADMIN_ONLY" envDefault:"true"`
	// Разрешить OpenAI-совместимые сервисы на частных адресах (локальный
	// vLLM, LM Studio). По умолчанию запрещено: адрес задаёт пользователь,
	// и без запрета сервер можно направить во внутреннюю сеть (SSRF).
	AllowPrivateProviderURLs bool `env:"LINGUASTACK_ALLOW_PRIVATE_PROVIDER_URLS" envDefault:"false"`
	// Тайм-аут одного запроса к модели. Локальная генерация батча на CPU
	// может идти минутами.
	ProviderTimeout time.Duration `env:"LINGUASTACK_PROVIDER_TIMEOUT" envDefault:"10m"`
	HTTPAddr        string        `env:"LINGUASTACK_HTTP_ADDR" envDefault:":8000"`
	// Каталог собранного веб-приложения (frontend/dist). Пусто — api
	// отдаёт только /api: для разработки фронтенд раздаёт Vite.
	WebRoot string `env:"LINGUASTACK_WEB_ROOT"`

	// Тестовый провайдер «fake» для сквозных тестов интерфейса: переводит
	// как «RU:» + текст. В рабочей системе выдал бы мусор за перевод —
	// поэтому выключен и включается только явно.
	E2EFakeProvider bool          `env:"LINGUASTACK_E2E_FAKE_PROVIDER" envDefault:"false"`
	E2EFakeDelay    time.Duration `env:"LINGUASTACK_E2E_FAKE_DELAY" envDefault:"0s"`

	// Cookie сессии только по HTTPS. По умолчанию true: продакшен, где
	// забыли переменную, не должен отдавать сессию по открытому каналу.
	// false выставляется явно — для локальной разработки по http и тестов.
	SessionCookieSecure bool          `env:"LINGUASTACK_SESSION_COOKIE_SECURE" envDefault:"true"`
	SessionTTL          time.Duration `env:"LINGUASTACK_SESSION_TTL" envDefault:"720h"`

	// --- лимиты, дополнение §10 ---
	MaxUploadBytes     int64 `env:"LINGUASTACK_MAX_UPLOAD_BYTES" envDefault:"52428800"`
	MaxZipEntries      int   `env:"LINGUASTACK_MAX_ZIP_ENTRIES" envDefault:"2000"`
	MaxZipTotalBytes   int64 `env:"LINGUASTACK_MAX_ZIP_TOTAL_BYTES" envDefault:"209715200"`
	MaxZipRatio        int   `env:"LINGUASTACK_MAX_ZIP_RATIO" envDefault:"100"`
	MaxTasksPerUser    int   `env:"LINGUASTACK_MAX_TASKS_PER_USER" envDefault:"3"`
	MaxProjectsPerUser int   `env:"LINGUASTACK_MAX_PROJECTS_PER_USER" envDefault:"20"`
	// Хранилище пользователя — сумма размеров оригиналов книг (дополнение
	// §18): 2 ГиБ; 0 — без предела.
	StorageQuotaBytes  int64         `env:"LINGUASTACK_STORAGE_QUOTA_BYTES" envDefault:"2147483648"`
	MaxSegmentAttempts int           `env:"LINGUASTACK_MAX_SEGMENT_ATTEMPTS" envDefault:"3"`
	LoginMaxAttempts   int           `env:"LINGUASTACK_LOGIN_MAX_ATTEMPTS" envDefault:"10"`
	LoginWindow        time.Duration `env:"LINGUASTACK_LOGIN_WINDOW" envDefault:"15m"`
	// Частота запросов (дополнение §18): на пользователя — в секунду и
	// всплеск; загрузок на пользователя, регистраций и запросов сброса
	// пароля с одного IP — в час. 0 — без ограничения.
	RateLimitRPS    float64 `env:"LINGUASTACK_RATE_LIMIT_RPS" envDefault:"20"`
	RateLimitBurst  int     `env:"LINGUASTACK_RATE_LIMIT_BURST" envDefault:"60"`
	UploadsPerHour  int     `env:"LINGUASTACK_UPLOADS_PER_HOUR" envDefault:"30"`
	RegisterPerHour int     `env:"LINGUASTACK_REGISTER_PER_HOUR" envDefault:"5"`
	ResetPerHour    int     `env:"LINGUASTACK_RESET_PER_HOUR" envDefault:"5"`
	// Регистрация по приглашению: адреса и домены («@team.example») через
	// запятую. Пусто — регистрация открыта.
	RegistrationAllowlist string `env:"LINGUASTACK_REGISTRATION_ALLOWLIST"`
	// Прокси, которым верится X-Forwarded-For (CIDR или адрес через
	// запятую). Пусто — IP клиента берётся из соединения.
	TrustedProxies CIDRList `env:"LINGUASTACK_TRUSTED_PROXIES"`
	// Разбор PDF (дополнение §15.1): страниц больше — отказ принять файл;
	// по тайм-ауту экземпляр PDFium уничтожается; память модуля wasm.
	PDFMaxPages int           `env:"LINGUASTACK_PDF_MAX_PAGES" envDefault:"2000"`
	PDFTimeout  time.Duration `env:"LINGUASTACK_PDF_TIMEOUT" envDefault:"2m"`
	PDFMemoryMB int           `env:"LINGUASTACK_PDF_MEMORY_MB" envDefault:"1024"`
	// Экспорт (дополнение §16): время сборки файла — оно же аренда задачи;
	// одновременно ждущих и выполняемых экспортов на пользователя.
	ExportTimeout           time.Duration `env:"LINGUASTACK_EXPORT_TIMEOUT" envDefault:"5m"`
	MaxActiveExportsPerUser int           `env:"LINGUASTACK_MAX_ACTIVE_EXPORTS_PER_USER" envDefault:"3"`

	// --- эксплуатация, дополнение §18 ---
	// Токен доступа к /metrics (Authorization: Bearer). Пусто — метрик нет.
	MetricsToken string `env:"LINGUASTACK_METRICS_TOKEN"`
	// Адрес, где worker отдаёт свои метрики (например «:9101»); пусто —
	// не отдаёт. Доступ — тем же токеном.
	WorkerMetricsAddr string `env:"LINGUASTACK_WORKER_METRICS_ADDR"`

	// Почта для ссылок сброса пароля (необязательно). TLS: starttls (по
	// умолчанию), tls (порт 465) или none — только для релея на том же хосте.
	SMTPHost         string        `env:"LINGUASTACK_SMTP_HOST"`
	SMTPPort         int           `env:"LINGUASTACK_SMTP_PORT" envDefault:"587"`
	SMTPUsername     string        `env:"LINGUASTACK_SMTP_USERNAME"`
	SMTPPassword     string        `env:"LINGUASTACK_SMTP_PASSWORD"`
	SMTPFrom         string        `env:"LINGUASTACK_SMTP_FROM"`
	SMTPTLS          string        `env:"LINGUASTACK_SMTP_TLS" envDefault:"starttls"`
	PasswordResetTTL time.Duration `env:"LINGUASTACK_PASSWORD_RESET_TTL" envDefault:"1h"`

	// --- Telegram, дополнение §17 ---
	// Токен — секрет развёртывания (ТЗ §9). Без токена и имени бот
	// выключен, а api отвечает enabled: false.
	TelegramBotToken    string `env:"LINGUASTACK_TELEGRAM_BOT_TOKEN"`
	TelegramBotUsername string `env:"LINGUASTACK_TELEGRAM_BOT_USERNAME"`
	TelegramAPIURL      string `env:"LINGUASTACK_TELEGRAM_API_URL" envDefault:"https://api.telegram.org"`
	// PublicURL — адрес веб-приложения для ссылок в сообщениях бота; без
	// него сообщения без ссылок.
	PublicURL string `env:"LINGUASTACK_PUBLIC_URL"`

	// --- сегментация и батчинг, дополнение §2 ---
	MaxBatchSegments int `env:"LINGUASTACK_MAX_BATCH_SEGMENTS" envDefault:"20"`
	MaxBatchChars    int `env:"LINGUASTACK_MAX_BATCH_CHARS" envDefault:"6000"`

	// --- очередь, дополнение §7 ---
	TaskLease time.Duration `env:"LINGUASTACK_TASK_LEASE" envDefault:"2m"`
	// Сколько после сигнала остановки ждать начатый вызов модели. Дольше —
	// вызов обрывается, задача возвращается в очередь без траты попытки.
	// Должно быть меньше срока, который даёт оркестратор до SIGKILL
	// (stop_grace_period в docker-compose.yml).
	WorkerShutdownGrace time.Duration `env:"LINGUASTACK_WORKER_SHUTDOWN_GRACE" envDefault:"25s"`
}

// CIDRList — список сетей из переменной окружения: «10.0.0.0/8, 127.0.0.1».
// Адрес без маски — сеть из одного адреса.
type CIDRList []netip.Prefix

// UnmarshalText разбирает список; негодная сеть — ошибка запуска, а не
// молча пропущенный прокси.
func (l *CIDRList) UnmarshalText(text []byte) error {
	var out CIDRList
	for item := range strings.SplitSeq(string(text), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			addr, err := netip.ParseAddr(item)
			if err != nil {
				return fmt.Errorf("адрес прокси %q: %w", item, err)
			}
			out = append(out, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(item)
		if err != nil {
			return fmt.Errorf("сеть прокси %q: %w", item, err)
		}
		out = append(out, p.Masked())
	}
	*l = out
	return nil
}

// TelegramEnabled — бот настроен: есть токен и имя для ссылок.
func (s Settings) TelegramEnabled() bool {
	return s.TelegramBotToken != "" && s.TelegramBotUsername != ""
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
