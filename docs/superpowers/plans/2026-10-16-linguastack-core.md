# LinguaStack Core (Этапы 1–3) Implementation Plan — Go

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Ревизия 2 (2026-10-16).** Первая ревизия плана была на Python/FastAPI по ТЗ §10;
> владелец проекта выбрал Go. Декомпозиция на 27 задач, тестовые сценарии и все
> проектные решения сохранены, переписан слой реализации. Python-версия — в истории git.
> Тесты в задачах заданы как «имя теста → что именно он утверждает»; это
> однозначная спецификация, а код тестов появляется в репозитории вместе с задачей.

**Goal:** Довести LinguaStack до состояния, в котором загруженная книга (Markdown, ZIP, EPUB, TXT) реально переводится в фоне через Ollama или внешний API, с сохранением прогресса, проверкой структуры и изоляцией пользователей.

**Architecture:** Go-сервис на `net/http` поверх PostgreSQL 17 (GORM + pgx, схема — миграции goose). Импортёры приводят любой формат к Canonical Document Model; сегментатор нарезает CDM на блоки 1:1 и группирует в батчи; сервис перевода маскирует защищённые фрагменты, говорит с моделью по нумерованному конверту и валидирует результат; очередь на арендах (lease) в Postgres с `FOR UPDATE SKIP LOCKED` даёт фоновое выполнение, честность между пользователями и восстановление после перезапуска. Провайдеры (Ollama, OpenAI-compatible, Anthropic) скрыты за интерфейсом `providers.Provider`, который выражает намерение, а не параметры генерации.

**Tech Stack:** Go 1.26 (тулчейн go1.27.2), GORM v1.31 + pgx v5, goose v3, `net/http`, argon2id, AES-256-GCM (stdlib), anthropic-sdk-go v1.82, goldmark, `x/net/html`, testify, golangci-lint v2.14.

**Spec:** [`PROJECT`](../../../PROJECT) (ТЗ v1.1) + [`docs/spec/SPEC-ADDENDUM.md`](../../spec/SPEC-ADDENDUM.md) v1.1. **Исполнитель читает оба.** «ТЗ §N» и «ADD §N» ссылаются на них.

## Global Constraints

- **Go:** язык `go 1.26.0`, `toolchain go1.27.2` в `go.mod`. Не понижать до 1.25 — вне поддержки (ADD §0.1). `/usr/bin/go` на машине — gccgo 1.16, использовать `export PATH=/root/go/bin:/usr/local/go/bin:$PATH`.
- **Зависимости — только из ADD §0.3.** Новая зависимость — осознанное решение с записью в ADD, а не мимоходом.
- **Схема БД — только миграциями goose** в `backend/migrations/`; `AutoMigrate` не используется нигде, включая тесты (ADD §0.2).
- **Anthropic model IDs без суффиксов даты:** `claude-opus-5-5` (по умолчанию), `claude-sonnet-5-5`, `claude-haiku-5-5`. Цены — ADD §6.2.
- **Anthropic: никогда не передавать `temperature`, `top_p`, `top_k`** — Opus/Sonnet 5.5 отвечают 400. Глубина — `output_config.effort` (ADD §6).
- **Anthropic: проверять `stop_reason == "refusal"` до чтения `content`** (ADD §6.1).
- **Перед написанием кода против Anthropic SDK** — прочитать `go/claude-api/README.md` из навыка `claude-api`; сигнатуры SDK не угадывать.
- **Оригинал неизменен** (ТЗ §4.1): загруженный файл после записи только читается.
- **Изоляция:** доступ к ресурсу — только через `store.Owned[T]`; чужой ресурс → **404, не 403**; роль `admin` обхода не даёт (ADD §9).
- **Секреты:** API-ключи пользователей не попадают ни в ответы, ни в логи (ADD §8).
- **Ошибки:** сигнальные — `errors.Is`, типизированные — `errors.As`; оборачивать через `%w`. Сравнение ошибок через `==` запрещено (линтер `errorlint`).
- **Контекст:** каждый сетевой и DB-вызов принимает `context.Context` первым аргументом (линтер `noctx`).
- **TDD:** сначала падающий тест, затем минимальная реализация. Тесты без сети; живые — `TestLive…` + `LINGUASTACK_LIVE_OLLAMA=1`.
- **Время:** все колонки `timestamptz`, все значения в UTC.
- **Коммит после каждой задачи**, Conventional Commits, сообщение заканчивается строкой `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Готовность задачи:** `make test` и `make lint` зелёные.

## Review Focus

Пять классов входа, которые ТЗ подразумевает, но ни одна задача не проверяет естественным образом. Молчание спецификации про вход — не разрешение ломаться на нём.

1. **Глава без переводимых блоков** (целиком код или изображения) — прогресс `0/0` не должен давать деление на ноль или вечное «в работе»; проект обязан дойти до `completed`. → тесты в задачах 19 и 25.
2. **Исходник, в котором уже есть маркеры `⟦1⟧`** — маскирование обязано их экранировать, иначе восстановление подставит чужой фрагмент. → тест в задаче 7.
3. **Один блок больше лимита батча** (глава одним абзацем на мегабайт) — явная ошибка с именем блока, не молчаливое обрезание. → тест в задаче 19.
4. **Повторный импорт того же файла и два проекта перевода на одну книгу** — ТЗ §5.1 разрешает несколько проектов; нельзя допустить коллизий в хранилище и перезаписи чужого прогресса. → тесты в задачах 13 и 22.
5. **Пауза или отмена, пока worker держит батч** — оплаченный ответ модели не теряется, а запись после отмены не «воскрешает» проект. → тесты в задаче 25.

Дополнительно по данным: TXT с BOM, CRLF и в cp1251; пустые абзацы; EPUB без `toc.ncx`. → задачи 9, 12.

---

## File Structure

| Путь | Ответственность |
|---|---|
| `docker-compose.yml` | PostgreSQL 17 на `127.0.0.1:5434` |
| `backend/go.mod`, `Makefile`, `.golangci.yml` | тулчейн, команды, линтер |
| `backend/cmd/api/main.go` | HTTP-сервер, корректное завершение |
| `backend/cmd/worker/main.go` | worker очереди |
| `backend/migrations/*.sql`, `embed.go` | схема БД (goose), встроена в бинарь |
| `internal/config` | `Settings`, лимиты ADD §10, `RequireSecretKey` |
| `internal/db` | открытие GORM, накат миграций |
| `internal/db/models` | модели сущностей ТЗ §11 |
| `internal/store` | `Owned[T]` — единственная проверка владения |
| `internal/testdb` | тестовая БД: накат миграций, транзакция на тест |
| `internal/security` | пароли, `SecretBox`, сессии, лимит входа — файл на механизм |
| `internal/storage` | безопасные пути, UUID-имена, лимиты |
| `internal/cdm` | Canonical Document Model, `NodeID`/`ContentHash`, маскирование |
| `internal/importers` | интерфейс `Importer`, реестр, по файлу на формат |
| `internal/providers` | интерфейс `Provider`, ошибки, `Fake`, Ollama, OpenAI-compatible, Anthropic |
| `internal/translation` | сегментатор, конверт, валидация, сервис перевода батча |
| `internal/queue` | захват/аренда задач, worker |
| `internal/httpapi` | маршруты, middleware аутентификации, обработчики по ресурсам |

---

## Этап 1 — Фундамент

> **Статус: ✅ завершён** (задачи 1–8, коммиты `f7ee23b`…). Отклонения от
> плана, принятые по ходу и проверенные тестами:
>
> - **Задача 2:** недоступная тестовая БД роняет тесты (`t.Fatal`), а не
>   пропускает их — пропуск давал бы ложное «ok» половине сюиты.
> - **Задача 4:** cookie сессии `Secure` по умолчанию (`true`), `false` —
>   явно для разработки и тестов; для отсутствующего аккаунта пароль
>   сверяется с фиктивным хешем, чтобы время ответа не выдавало адреса.
> - **Задача 5:** вместо `SafeExtractMember` — `ValidateMemberName` (проверка
>   имени без диска) и `SaveAsset` (запись ресурсов через `os.Root`, симлинк
>   на пути — подмена). Оригинал связывается через `os.Link` и не
>   перезаписывается.
> - **Задача 7:** фаззер нашёл три дефекта, главный — некорректный
>   `Unmask` на последовательных `ReplaceAll` (класс Review Focus №2).
>   Переписан в один проход; найденные входы — в `testdata/fuzz`.
> - Тесты безопасности задач 4 и 8 проверены мутациями.

### Task 1: Каркас, конфигурация, health-эндпоинт — ✅ сделано (`f7ee23b`)

`internal/config` (`Settings`, `Load`, `RequireSecretKey`, `GenerateSecretKey`), `internal/httpapi` (`Deps`, `NewServer`, `GET /api/health`), `cmd/api` с завершением по SIGTERM, `Makefile`, `.golangci.yml`. Проверено: 8 тестов, линтер, реальный бинарь отвечает на `/api/health` и корректно завершается.

---

### Task 2: Слой БД, миграции goose, тестовая база

**Files:**
- Create: `backend/internal/db/db.go`, `backend/internal/db/migrate.go`
- Create: `backend/migrations/embed.go`, `backend/migrations/00001_users.sql`
- Create: `backend/internal/db/models/base.go`, `backend/internal/db/models/user.go`
- Create: `backend/internal/testdb/testdb.go`
- Test: `backend/internal/db/db_test.go`

**Interfaces:**
- Produces:
  - `db.Open(ctx context.Context, url string) (*gorm.DB, error)` — GORM поверх pgx; логгер GORM пишет в `slog` **без значений параметров** (в них могут быть пароли и ключи).
  - `db.Migrate(ctx context.Context, gdb *gorm.DB) error` — `goose.Up` из `migrations.FS` с `lock.NewPostgresSessionLocker()`.
  - `migrations.FS embed.FS`.
  - `models.Base{ID uuid.UUID; CreatedAt time.Time}` с хуком `BeforeCreate`, ставящим `uuid.New()` при нулевом ID.
  - `models.User{Base; Email string; PasswordHash string; Role string; IsActive bool}`; константы `models.RoleUser`, `models.RoleAdmin`.
  - `testdb.New(t *testing.T) *gorm.DB` — один раз на процесс: создать `linguastack_test` (гонку `CREATE DATABASE` между пакетами переживать по коду `42P04`), накатить миграции; на каждый тест — `Begin()` и `t.Cleanup(Rollback)`. Если БД недоступна — `t.Skip` с инструкцией `docker compose up -d db`.

- [ ] **Step 1: Тесты** (`db_test.go`):
  - `TestUserRoundTrip` → после `Create` у пользователя ненулевой UUID и `CreatedAt` в UTC; повторное чтение по email возвращает тот же ID и роль `user`.
  - `TestEmailIsUnique` → второй `Create` с тем же email возвращает ошибку, и `errors.Is(err, gorm.ErrDuplicatedKey)` истинно (нужен `TranslateError: true` в конфиге GORM).
  - `TestTestsAreIsolated` → запись из `TestUserRoundTrip` в этом тесте не видна.
  - `TestMigrationsAreIdempotent` → повторный `db.Migrate` на уже накаченной базе не ошибается.
  - `TestGormLoggerDoesNotPrintParameters` → SQL-лог при `Create(&User{PasswordHash: "secret-hash-value"})` не содержит `secret-hash-value`.
- [ ] **Step 2:** `go test ./internal/db/...` → FAIL (пакета нет).
- [ ] **Step 3:** Реализовать. Миграция `00001_users.sql`: таблица `users` (`id uuid pk`, `email varchar(320) not null unique`, `password_hash varchar(255) not null`, `role varchar(16) not null default 'user'`, `is_active boolean not null default true`, `created_at timestamptz not null default now()`), секции `-- +goose Up` / `-- +goose Down`.
- [ ] **Step 4:** `go test ./internal/db/...` → PASS; `make lint` → 0 issues.
- [ ] **Step 5:** Добавить в `Makefile` цель `migrate` (накат на `LINGUASTACK_DATABASE_URL` через `cmd/api -migrate` либо `goose` CLI), прогнать на dev-базе.
- [ ] **Step 6:** Коммит — `feat: слой БД на GORM, миграции goose, изолированная тестовая база`.

---

### Task 3: Пароли и шифрование секретов

**Files:**
- Create: `internal/security/passwords.go`, `internal/security/secretbox.go`
- Test: `internal/security/passwords_test.go`, `internal/security/secretbox_test.go`

**Interfaces:**
- Produces: `security.HashPassword(plain string) (string, error)`; `security.VerifyPassword(plain, encoded string) bool` (никогда не паникует и не возвращает ошибку — повреждённая запись означает «не совпало»); `security.NewSecretBox(key []byte) (*SecretBox, error)`; `(*SecretBox).Encrypt(plain string) (string, error)`; `(*SecretBox).Decrypt(token string) (string, error)` (ошибка `security.ErrDecrypt` при подмене или чужом ключе); `security.Fingerprint(plain string) string` (8 hex-символов sha256); `security.Last4(plain string) string`.

- [ ] **Step 1: Тесты:**
  - `TestHashIsArgon2idAndVerifies` → хеш начинается с `$argon2id$`; верный пароль проходит, неверный нет.
  - `TestHashIsSalted` → два хеша одного пароля различаются.
  - `TestVerifyRejectsGarbage` → `VerifyPassword("x", "not-a-hash")` и `VerifyPassword("x", "")` возвращают `false`.
  - `TestSecretRoundTrip` → `Decrypt(Encrypt(k)) == k`; шифртекст не содержит `k`.
  - `TestEncryptionIsRandomised` → два шифрования одного значения различаются (случайный nonce).
  - `TestDecryptRejectsTampering` → изменение одного байта шифртекста → `errors.Is(err, ErrDecrypt)`.
  - `TestDecryptRejectsForeignKey` → шифртекст одного ключа не расшифровывается другим.
  - `TestDecryptRejectsTruncatedToken` → токен короче nonce → `ErrDecrypt`, не паника.
  - `TestNewSecretBoxRejectsWrongKeyLength` → ключ 16 и 31 байт → ошибка.
  - `TestFingerprintAndLast4` → стабильны, различаются для разных значений; `Last4("sk-…2345") == "2345"`; `Last4` короткой строки не паникует.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать: `argon2id.CreateHash(plain, argon2id.DefaultParams)`; AES-256-GCM, токен `base64.RawStdEncoding(nonce‖sealed)`. **Step 4:** PASS + lint.
- [ ] **Step 5:** Коммит — `feat: argon2id для паролей и AES-256-GCM для пользовательских API-ключей`.

---

### Task 4: Сессии, регистрация, вход, защита от перебора

**Files:**
- Create: `migrations/00002_sessions.sql`, `internal/db/models/session.go`, `internal/db/models/login_attempt.go`
- Create: `internal/security/sessions.go`, `internal/security/ratelimit.go`
- Create: `internal/httpapi/auth.go`, `internal/httpapi/middleware.go`, `internal/httpapi/respond.go`
- Test: `internal/httpapi/auth_test.go`, `internal/httpapi/helpers_test.go`

**Interfaces:**
- Produces:
  - `security.SessionCookie = "linguastack_session"`; `security.IssueSession(ctx, db, userID uuid.UUID, ttl time.Duration) (token string, err error)` — в БД только `sha256(token)`; `security.LookupSessionUser(ctx, db, token) (*models.User, error)` → `security.ErrNoSession` для неизвестной, истёкшей сессии или неактивного пользователя; `security.RevokeSession(ctx, db, token) error`.
  - `security.RecentFailures(ctx, db, email, ip string, window time.Duration) (int64, error)`; `security.RecordAttempt(ctx, db, email, ip string, succeeded bool) error` — успех удаляет накопленные неудачи этой пары.
  - `httpapi.UserFromContext(ctx) *models.User`; middleware `requireUser(next http.Handler) http.Handler` → 401 без валидной сессии.
  - Эндпоинты ТЗ §12: `POST /api/auth/register` (201 / 409 / 422), `POST /api/auth/login` (200 + cookie / 401 / 429), `POST /api/auth/logout` (204), `GET /api/me` (200 / 401).
  - `respond.go`: `writeJSON`, `writeError(w, status, message)` с телом `{"error": "..."}`, `decodeJSON(r, dst, maxBytes)` — тело запроса ограничено (`http.MaxBytesReader`), неизвестные поля отвергаются.
  - Тестовые помощники: `newTestServer(t) (*httptest.Server, *gorm.DB)` и `newClient(t, srv)` с cookie jar.

- [ ] **Step 1: Тесты** — перенос 15 сценариев из Python-ревизии:
  - `TestRegisterCreatesAccount` → 201, тело содержит email и роль `user`.
  - `TestRegisterRejectsDuplicateEmail` → второй раз 409.
  - `TestRegisterValidatesInput` (табличный) → пароль короче 12 символов, невалидный email, неизвестное поле, тело больше лимита → 422 (а для лимита — 413).
  - `TestLoginSetsHttpOnlySameSiteCookie` → `Set-Cookie` содержит `HttpOnly` и `SameSite=Lax` (сравнение без учёта регистра — RFC 6265).
  - `TestMeRequiresAuthentication` → 401.
  - `TestMeReturnsCurrentUser` → после входа 200 и свой email.
  - `TestPasswordNeverEchoed` → ни пароль, ни `password_hash` не встречаются в ответе регистрации и `/api/me`.
  - `TestLoginFailureDoesNotRevealAccountExistence` → «нет аккаунта» и «неверный пароль» дают одинаковые код и тело.
  - `TestLoginThrottledAfterLimit` → после 10 неудач 11-я попытка с **верным** паролем → 429.
  - `TestThrottleScopedPerEmail` → перебор одного адреса не блокирует вход другому.
  - `TestSuccessfulLoginClearsFailures` → 9 неудач, успешный вход, ещё 9 неудач → вход по-прежнему возможен.
  - `TestLogoutInvalidatesSession` → после logout `/api/me` → 401, даже если клиент сохранил старую cookie.
  - `TestSessionTokenStoredHashed` → значение cookie не совпадает ни с одним `token_hash` в БД.
  - `TestUnknownCookieRejected` → выдуманный токен → 401.
  - `TestDeactivatedUserCannotAuthenticate` → и вход, и ранее выданная сессия перестают работать.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать; лимит проверяется **до** сверки пароля. IP — `r.RemoteAddr` без порта (доверие `X-Forwarded-For` — только за явно настроенным прокси, этап 7). **Step 4:** PASS + lint.
- [ ] **Step 5:** Коммит — `feat: регистрация, вход по сессионной cookie, защита от перебора`.

---

### Task 5: Файловое хранилище с защитой путей

> Реализовано с уточнением API — см. статус этапа 1.

**Files:** Create `internal/storage/files.go`; Test `internal/storage/files_test.go`

**Interfaces:**
- Produces: `storage.New(root string, maxBytes int64) (*FileStore, error)`; `(*FileStore).BookDir(userID, bookID uuid.UUID) string`; `SaveOriginal(userID, bookID uuid.UUID, suffix string, r io.Reader) (string, error)`; `SaveNormalized(userID, bookID uuid.UUID, v any) (string, error)`; `OpenOriginal(userID, bookID uuid.UUID, suffix string) (*os.File, error)`; `DeleteBook(userID, bookID uuid.UUID) error`; `SafeExtractMember(targetDir, member string) (string, error)`; ошибки `ErrUnsafePath`, `ErrFileTooLarge`, `ErrDisallowedExtension`.
- Идентификаторы — `uuid.UUID`, не строки: обход каталога через идентификатор невозможен по построению, проверка — на уровне типа.

- [ ] **Step 1: Тесты** — перенос 22 сценариев:
  - `TestPathNeverContainsUserSuppliedName` → файл называется `original.txt`, путь содержит оба UUID.
  - `TestSafeExtractMemberRejects` (табличный) → `../escape.md`, `/abs/path.md`, `nested/../../escape.md`, `a/b/../../../escape.md`, `./../escape.md`, `..\..\escape.md`, `""`, `dir/` → `ErrUnsafePath`.
  - `TestSafeExtractMemberAccepts` (табличный) → `ch1.md`, `nested/ch2.md`, `a/b/c/ch3.md` лежат внутри целевого каталога.
  - `TestSafeExtractMemberRejectsSymlinkEscape` → если внутри цели есть симлинк наружу, запись через него отвергается.
  - `TestOversizedStreamRejectedAndNotLeftBehind` → поток 5000 байт при лимите 10 → `ErrFileTooLarge`, файла на диске нет.
  - `TestStreamWithinLimitSaved`, `TestUnknownExtensionRejected` (`.exe`), `TestExtensionCaseInsensitive` (`.EPUB` → `original.epub`).
  - `TestNormalizedSavedAsJSON`, `TestOpenOriginalRoundTrip`.
  - `TestDeleteBookRemovesDirectory`, `TestDeleteBookIdempotent`, `TestDeleteBookDoesNotTouchOtherBooks`.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать; поток читается через `io.LimitReader(r, maxBytes+1)`, превышение — удаление частичного файла; `SafeExtractMember` отвергает абсолютные пути, `..`, `\`, затем сверяет `filepath.EvalSymlinks` родителя с целью. **Step 4:** PASS + lint.
- [ ] **Step 5:** Коммит — `feat: файловое хранилище с защитой от path traversal`.

---

### Task 6: Canonical Document Model и стабильные идентификаторы

**Files:** Create `internal/cdm/model.go`, `internal/cdm/ids.go`; Test `internal/cdm/model_test.go`, `internal/cdm/ids_test.go`

**Interfaces:**
- Produces:
  - `cdm.BlockType` (string) с константами `Heading, Paragraph, ListItem, TableCell, Caption, Blockquote, CodeBlock, MathBlock, Image, ThematicBreak, RawHTML`; `cdm.IsTranslatable(BlockType) bool` — первые шесть.
  - `cdm.Block{NodeID, Type, Text string; Level int; SourceHash string; Meta map[string]string}` (JSON-теги snake_case).
  - `cdm.Chapter{Index int; Title string; Blocks []Block; Children []Chapter}`; `cdm.BookMeta{Title, Author, Language, SourceFormat string}`; `cdm.Document{Meta BookMeta; Chapters []Chapter}`.
  - `(*Document).Blocks() []Block` (обход в глубину, порядок сохраняется); `TranslatableBlocks() []Block`; `TOC() []TOCEntry`; `TOCEntry{Index int; Title string; Children []TOCEntry}`; `Validate() error` — уникальность `NodeID`.
  - `cdm.NodeID(chapterIndex, ordinal int) string` → `ch0003/b0017`; `cdm.NormalizeText(string) string` (NFC + свёртка пробелов + обрезка); `cdm.ContentHash(string) string` (первые 16 байт sha256, hex).
  - `cdm.NewBuilder(meta BookMeta) *Builder` — общий помощник импортёров: `StartChapter(title string, depth int)`, `Add(type, text string, level int, meta map[string]string)`, `Build() Document`. Сам выдаёт глобальные индексы глав в порядке обхода и `NodeID`/`SourceHash` блокам, чтобы импортёры не дублировали эту логику.

- [ ] **Step 1: Тесты:**
  - `TestNodeIDDeterministicAndReadable` → `NodeID(3, 17) == "ch0003/b0017"`.
  - `TestContentHashIgnoresWhitespace` → `"Hello   world"`, `"  Hello world\n"` и `"Hello world"` дают один хеш; `"Hello worlds"` — другой.
  - `TestContentHashNormalizesUnicode` → `"café"` и `"café"` дают один хеш.
  - `TestTranslatableBlocksExcludeCodeAndImages`.
  - `TestBuilderAssignsUniqueNodeIDsAcrossChapters` и `TestValidateDetectsDuplicateNodeIDs`.
  - `TestBuilderNestsChaptersByDepth` → глава глубины 2 становится `Children` предыдущей главы глубины 1; индексы глобальны и уникальны.
  - `TestTOCPreservesOrderAndNesting`.
  - `TestDocumentJSONRoundTrip` → сериализация и обратное чтение дают равный документ (CDM хранится в JSONB).
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать. **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: Canonical Document Model и стабильные идентификаторы блоков`.

---

### Task 7: Маскирование защищённых фрагментов

**Files:** Create `internal/cdm/masking.go`; Test `internal/cdm/masking_test.go`

**Interfaces:**
- Produces: `cdm.Mask(text string) Masked`; `cdm.Masked{Text string; Placeholders map[string]string}`; `cdm.Unmask(text string, placeholders map[string]string) string`; `cdm.VerifyPlaceholders(masked, translated string) error` → `*PlaceholderMismatchError{Missing, Extra []string}`.
- Маркер `⟦N⟧` (U+27E6/U+27E7). Порядок: сначала экранировать существующие `⟦`/`⟧` (символы Private Use Area), затем маскировать: fenced-код → inline-код → `$$…$$` → `$…$` → изображения целиком → цели ссылок (текст ссылки остаётся видимым) → HTML-теги.

- [ ] **Step 1: Тесты** (табличный `TestMaskRoundTrip` + отдельные):
  - inline-код маскируется и возвращается дословно; модель не видит `get_user()`.
  - у ссылки `[документацию](https://x/docs)` текст виден, URL — нет.
  - формулы `$E = mc^2$` и `$$…$$` маскируются.
  - **Review Focus №2:** `"Литерал ⟦1⟧ в тексте и код `x`."` после `Unmask(Mask(..))` возвращается точно.
  - текст без защищённых фрагментов — `Mask` не меняет его, `Placeholders` пуст.
  - `TestVerifyDetectsDroppedMarker`, `TestVerifyDetectsDuplicatedMarker`, `TestVerifyAllowsReordering` (порядок слов при переводе меняется — важно мультимножество).
  - `TestDollarAmountsAreNotMath` → `"стоит $5 и $10"` не маскируется как формула (цена, а не математика).
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать. **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: маскирование кода, ссылок и формул перед переводом`.

---

### Task 8: Единая проверка владения

**Files:** Create `internal/store/owned.go`; Test `internal/store/owned_test.go`

**Interfaces:**
- Produces: `store.ErrNotFound`; `store.Owned[T any](ctx, db *gorm.DB, id, ownerID uuid.UUID) (*T, error)` — `WHERE id = ? AND owner_id = ?`; `store.OwnedScope(ownerID uuid.UUID) func(*gorm.DB) *gorm.DB` для списков. В HTTP-слое `ErrNotFound` → 404.
- Модель с владельцем обязана иметь колонку `owner_id`; тест проверяет, что вызов `Owned` для модели без неё — ошибка конфигурации, а не «найдено».

- [ ] **Step 1: Тесты** (на временной модели `ownedFixture` из миграции `00003_books.sql` задачи 13 нельзя — её ещё нет, поэтому тест создаёт таблицу в транзакции теста):
  - `TestOwnedReturnsResourceToOwner`.
  - `TestOwnedReturnsNotFoundForForeignResource` → `errors.Is(err, store.ErrNotFound)`.
  - `TestAdminRoleDoesNotBypass` → пользователь с ролью `admin` получает `ErrNotFound` на чужой ресурс (ТЗ §2.1).
  - `TestOwnedNotFoundForMissingID`.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать. **Step 4:** PASS + lint.
- [ ] **Step 5:** Коммит — `feat: единая проверка владения ресурсом (404, без обхода для admin)`.

Параметризованный тест по **всем** ресурсным эндпоинтам — в задаче 26.

---

## Этап 2 — Импорт

### Task 9: Интерфейс импортёра, реестр, TXT

**Files:** Create `internal/importers/importer.go`, `registry.go`, `txt.go`; Test `txt_test.go`, `registry_test.go`

**Interfaces:**
- Produces:
  - `importers.Severity` (`Info`, `Warning`, `Error`); `importers.Issue{Severity; Code, Message, NodeID string}`; `importers.Result{Document cdm.Document; Issues []Issue}`.
  - `type Importer interface { FormatID() string; Sniff(data []byte, filename string) bool; Load(data []byte, filename string) (Result, error) }`.
  - **Семантика двух каналов:** `Issues` — проблемы, которые пользователь проверяет до перевода (ТЗ §3.1: пустой документ, угаданная кодировка, отсутствующая глава, повреждённый файл). `error` — только отказ принять загрузку (нарушение безопасности: `storage.ErrUnsafePath`, лимиты ZIP).
  - `importers.Pick(data []byte, filename string) (Importer, error)` → `ErrUnsupportedFormat`; `importers.Default(cfg config.Settings) *Registry`.
  - `importers.NewTXT() Importer` (`FormatID() == "txt"`).

- [ ] **Step 1: Тесты:**
  - `TestTXTSplitsParagraphsOnBlankLines` → `"А.\n\nБ.\n\n\nВ."` → три блока `Paragraph`.
  - `TestTXTStripsBOM`, `TestTXTHandlesCRLF`.
  - `TestTXTDecodesCP1251WithIssue` → кириллица в cp1251 читается, есть `Issue{Code: "encoding_guessed"}`.
  - `TestTXTEmptyFileIsIssueNotError` → `err == nil`, нет блоков, есть `Issue{Severity: Error, Code: "empty_document"}`.
  - `TestTXTTitleFromFilename` → `Meta.Title == "my-book"` для `my-book.txt`.
  - `TestPickByExtensionAndRejectUnknown` → `.txt` → txt; `.bin` с бинарным содержимым → `ErrUnsupportedFormat`.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать: `utf8.Valid` → UTF-8; иначе `charmap.Windows1251` из `x/text/encoding` с Issue. **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: интерфейс импортёра, реестр форматов и импорт TXT`.

---

### Task 10: Markdown

**Files:** Create `internal/importers/markdown.go`; Test `markdown_test.go`, `testdata/sample.md`

**Interfaces:**
- Produces: `importers.NewMarkdown() Importer` (`FormatID() == "markdown"`). Разбор `goldmark` с расширением таблиц (`extension.Table`); обход AST. Главы — по заголовкам; глубина заголовка задаёт вложенность через `cdm.Builder`. **Текст блока — исходный Markdown из сегментов AST** (`node.Lines()` / позиции в исходнике), а не отрендеренный HTML: маскирование и валидация работают с разметкой.

- [ ] **Step 1: Тесты** (на фикстуре с двумя главами, подразделом, кодом, списком, таблицей):
  - `TestChaptersSplitOnHeadings`, `TestSubsectionBecomesChild`.
  - `TestCodeBlockPresentButNotTranslatable` → один `CodeBlock` с исходным текстом, отсутствует в `TranslatableBlocks`.
  - `TestListItemsAreSeparateBlocks`, `TestTableCellsAreBlocks`, `TestHeadingCarriesLevel`.
  - `TestInlineMarkupPreservedInBlockText` → блок `"Это **важно** и `код`."` хранится с `**` и обратными кавычками.
  - `TestDocumentWithoutHeadingsYieldsOneChapter`.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать. **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: импорт Markdown`.

---

### Task 11: ZIP-репозитории — порядок глав и защита от зип-бомб

**Files:** Create `internal/importers/zip_repo.go`; Test `zip_repo_test.go`

**Interfaces:**
- Produces: `importers.NewZipRepo(maxEntries int, maxTotal int64, maxRatio int) Importer` (`FormatID() == "zip_repo"`); `importers.DiscoverOrder(names []string, summary []byte) (order []string, source string)`.
- Порядок глав (ТЗ §4.3 — алфавит не единственный способ): `SUMMARY.md` → `mkdocs.yml` (`nav`) → числовые префиксы с естественной сортировкой → ссылки из корневого `README.md`/`index.md` → алфавит. Источник порядка — `Issue{Info, "order_source"}`.
- Каждый `.md` разбирается `NewMarkdown()` — переиспользование, не копия.

- [ ] **Step 1: Тесты** (архивы строятся в тесте через `archive/zip`):
  - `TestSummaryDeterminesOrder` → `SUMMARY.md` ставит `zzz.md` раньше `aaa.md`; есть Issue `order_source` с упоминанием `SUMMARY.md`.
  - `TestNumericPrefixesNaturalOrder` → `01-…`, `02-…`, `10-…` (лексикографически `10` < `02`).
  - `TestZipSlipRejected` → запись `../../etc/passwd` → `errors.Is(err, storage.ErrUnsafePath)`.
  - `TestTooManyEntriesRejected`, `TestCompressionBombRejected` (2 МБ нулей, ratio > 100), `TestTotalSizeLimit`.
  - `TestMissingReferencedChapterIsIssue` → `Issue{Code: "missing_chapter"}` с именем файла.
  - `TestImagesPreservedAsImageBlocks`.
  - `TestNonMarkdownFilesIgnoredWithoutError` → `.png`, `.gitignore` в архиве не ломают импорт.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать; все проверки до чтения содержимого: число записей, сумма `UncompressedSize64`, отношение `UncompressedSize64/CompressedSize64`, имя через `storage.SafeExtractMember`. Чтение записи — через `io.LimitReader` на объявленный размер (заголовок ZIP может лгать). **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: импорт ZIP-репозиториев с порядком глав и защитой от зип-бомб`.

---

### Task 12: EPUB

**Files:** Create `internal/importers/epub.go`; Test `epub_test.go`, `testdata_epub_test.go` (сборщик EPUB для тестов)

**Interfaces:**
- Produces: `importers.NewEPUB() Importer` (`FormatID() == "epub"`). Stdlib: `archive/zip` → `META-INF/container.xml` → OPF (`encoding/xml`: metadata, manifest, spine) → оглавление из NCX (EPUB 2) или `nav` (EPUB 3), иначе spine с Issue `toc_missing`. HTML глав — `x/net/html`: `h1–h6` → `Heading` с уровнем, `p` → `Paragraph`, `li` → `ListItem`, `pre` → `CodeBlock`, `img` → `Image` (`Meta{src, alt}`), `blockquote`, `td/th`.
- Тот же лимит ZIP, что в задаче 11 (EPUB — тоже архив).

- [ ] **Step 1: Тесты** (EPUB собирается в тесте функцией `buildEPUB(t, opts)`, бинарных фикстур в репозитории нет):
  - `TestEPUBChaptersAndMetadata` → title, author, порядок глав.
  - `TestEPUBParagraphsTranslatable`, `TestEPUBPreIsCodeBlock`.
  - `TestEPUBImagesRecordSourceAndAlt` → `Meta["src"]` заканчивается на `x.png`, `Meta["alt"] == "Схема"`.
  - `TestEPUBWithoutTOCFallsBackToSpine` → главы есть, Issue `toc_missing`.
  - `TestCorruptEPUBIsIssueNotPanic` → мусор вместо EPUB → `err == nil`, Issue `{Error, "corrupt_file"}`.
  - `TestEPUBRejectsZipSlip` → запись с `..` → `storage.ErrUnsafePath`.
  - `TestEPUBDecodesHTMLEntities` → `&laquo;Текст&raquo;` → `«Текст»`.
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать. **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: импорт EPUB на стандартной библиотеке`.

---

### Task 13: API книг

**Files:** Create `migrations/00003_books.sql`, `internal/db/models/book.go`, `internal/httpapi/books.go`; Test `books_test.go`

**Interfaces:**
- Produces: `models.Book{Base; OwnerID uuid.UUID; Title, Author, SourceFormat, OriginalFilename, OriginalSuffix, Language, Status string; OriginalSize int64; CDM datatypes.JSONType[cdm.Document]; Issues datatypes.JSONSlice[importers.Issue]}`.
- Эндпоинты ТЗ §12: `POST /api/books` (multipart, поле `file`), `GET /api/books`, `GET /api/books/{id}`, `GET /api/books/{id}/toc`, `PUT /api/books/{id}/toc` (ручной порядок глав, ТЗ §4.3: тело `{"order": [индексы]}`), `DELETE /api/books/{id}`.
- `Deps` пополняется `Store *storage.FileStore` и `Importers *importers.Registry`.

- [ ] **Step 1: Тесты:**
  - `TestUploadMarkdownCreatesBookWithTOC` → 201, затем оглавление `["Ch"]`.
  - `TestLibraryListsOnlyOwnBooks` (ТЗ §2.2).
  - `TestForeignBookIs404` → GET, TOC, PUT TOC, DELETE чужой книги → 404.
  - `TestImportIssuesReturnedOnUpload` → пустой TXT → 201 и Issue `empty_document` в ответе (ТЗ §3.1).
  - `TestOversizedUploadIs413`, `TestUnsupportedFormatIs415`, `TestZipSlipUploadIs400` (и ничего не сохранено).
  - `TestDeleteRemovesFilesAndRow` (ТЗ §13).
  - **Review Focus №4:** `TestSameFileTwiceYieldsIndependentBooks` → два разных ID, оба читаются.
  - `TestManualTOCReorderPersists` → `[1, 0]` → оглавление `["B", "A"]`; `TestTOCReorderRejectsInvalidPermutation` → дубли или выход за диапазон → 422.
  - `TestOriginalFileUnchangedOnDisk` → байты `original.*` совпадают с загруженными (ТЗ §4.1).
- [ ] **Step 2:** FAIL. **Step 3:** Реализовать; тело ограничено `http.MaxBytesReader(MaxUploadBytes)`; книга и файл создаются согласованно (файл пишется до коммита транзакции, при ошибке — `DeleteBook`). **Step 4:** PASS + lint. **Step 5:** Коммит — `feat: API книг — загрузка, библиотека, оглавление, удаление`.

---

## Этап 3 — Перевод

### Task 14: Интерфейс провайдера, ошибки, Fake

**Files:** Create `internal/providers/provider.go`, `errors.go`, `fake.go`; Test `errors_test.go`, `fake_test.go`

**Interfaces:**
- Produces:

```go
type Determinism string // Strict, Balanced, Creative — ADD §6

type CompletionRequest struct {
	System          string
	User            string
	Determinism     Determinism
	MaxOutputTokens int // 0 = значение адаптера по умолчанию
}

type CompletionResult struct {
	Text              string
	InputTokens       *int
	OutputTokens      *int
	CachedInputTokens *int
	ModelID           string
	CostUSD           *float64
}

type Health struct {
	Available bool
	Detail    string
}

type Provider interface {
	ID() string
	SendsDataExternally() bool // ТЗ §6.3: интерфейс обязан предупреждать
	ListModels(ctx context.Context) ([]ModelCapabilities, error)
	Health(ctx context.Context) Health
	Capabilities(ctx context.Context, modelID string) (ModelCapabilities, error)
	Complete(ctx context.Context, modelID string, req CompletionRequest) (CompletionResult, error)
}
```

  - Ошибки — таблица ADD §6.1; `providers.Retryable(err error) bool`; `RetryAfter(err error) (time.Duration, bool)`.
  - `providers.Fake{Scenario string; Prefix string; FailTimes int; ViolateTimes int; Calls []CompletionRequest}` — сценарии ADD §14: `echo`, `prefix`, `protocol_violation`, `drops_placeholder`, `rate_limited`, `timeout`, `refusal`, `empty`. Fake понимает конверт `<<<SEG n>>>` (задача 20) и отвечает в нём, чтобы сервис перевода тестировался целиком. Потокобезопасен (mutex) — его зовёт worker.

- [ ] **Step 1: Тесты:** `TestRetryableMatchesSpec` (табличный по ADD §6.1, включая обёрнутые через `%w`); `TestRetryAfterExtracted`; `TestFakeEcho`; `TestFakeFailTimesThenSucceeds`; `TestFakeRateLimitedCarriesRetryAfter`; `TestFakeRefusalIsNotRetryable`; `TestFakeRecordsCalls`.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: интерфейс провайдера, таксономия ошибок и тестовый провайдер`.

---

### Task 15: Ollama

**Files:** Create `internal/providers/ollama.go`; Test `ollama_test.go`

**Interfaces:**
- Produces: `providers.NewOllama(baseURL string, client *http.Client) *Ollama`; `ID() == "ollama"`, `SendsDataExternally() == false`. `GET /api/tags`, `GET /api/version`, `POST /api/chat` (`stream: false`). Намерение → `options.temperature`: 0.0 / 0.3 / 0.8. Стоимость `nil`.

- [ ] **Step 1: Тесты** (`httptest.Server` вместо реального Ollama): `TestOllamaListModels` (цена nil), `TestOllamaStrictSendsTemperatureZero` (и `stream == false`, токены из `prompt_eval_count`/`eval_count`), `TestOllamaConnectionRefusedIsUnavailable`, `TestOllamaMissingModelIsModelNotAvailable` (404), `TestOllama5xxIsUnavailable`, `TestOllamaTimeoutIsUnavailable` (контекст с дедлайном), `TestOllamaContextCancellationStopsRequest`, `TestLiveOllamaTranslatesSentence` (`qwen3:8b`, пропуск без `LINGUASTACK_LIVE_OLLAMA=1`).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint.
- [ ] **Step 5:** `LINGUASTACK_LIVE_OLLAMA=1 go test -run Live ./internal/providers/` → PASS против реального сервера.
- [ ] **Step 6:** Коммит — `feat: адаптер Ollama`.

---

### Task 16: OpenAI-compatible

**Files:** Create `internal/providers/openai_compat.go`; Test `openai_compat_test.go`

**Interfaces:**
- Produces: `providers.NewOpenAICompatible(baseURL, apiKey string, client *http.Client) *OpenAICompatible`; `ID() == "openai_compatible"`, `SendsDataExternally() == true`. `POST {base}/chat/completions`, `Authorization: Bearer`; `GET {base}/models`. Один адаптер на OpenAI, OpenRouter, Groq, DeepSeek, vLLM, LM Studio.

- [ ] **Step 1: Тесты:** `TestOpenAISendsBearerAndParsesChoice`, `TestOpenAI401IsAuthAndNotRetryable`, `TestOpenAI429CarriesRetryAfter`, `TestOpenAIKeyAbsentFromErrorText` (текст ошибки 500 не содержит ключа — он попадает в логи и `last_error`), `TestOpenAIEmptyChoicesIsBadRequest`, `TestOpenAIRetryAfterHTTPDateParsed` (заголовок бывает и секундами, и датой).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: адаптер OpenAI-совместимых API`.

---

### Task 17: Anthropic

**Files:** Create `internal/providers/anthropic.go`; Test `anthropic_test.go`

**Перед реализацией:** прочитать `go/claude-api/README.md` навыка `claude-api` (создание клиента, `Messages.New`, типы параметров, `cache_control`, `stop_reason`, типы ошибок). Сигнатуры SDK не угадывать.

**Interfaces:**
- Produces: `providers.NewAnthropic(apiKey string, opts ...option.RequestOption) *Anthropic`; `ID() == "anthropic"`, `SendsDataExternally() == true`. Каталог ADD §6.2 с ценами.
- Три особенности (ADD §6): нет `temperature`/`top_p`/`top_k`, намерение → `output_config.effort` (`low`/`medium`/`high`); `stop_reason == "refusal"` → `*RefusedError` **до** чтения `content`; системный промпт с `cache_control: ephemeral`, кэшированные токены из `usage.cache_read_input_tokens`.
- Тесты гоняют SDK против `httptest.Server` через `option.WithBaseURL` — проверяется **реальное тело запроса**, которое уходит в API, а не вызовы заглушки.

- [ ] **Step 1: Тесты:** `TestAnthropicNeverSendsSamplingParams` (в JSON-теле нет `temperature`, `top_p`, `top_k`), `TestAnthropicDeterminismMapsToEffort` (табличный), `TestAnthropicSystemPromptCacheControlled`, `TestAnthropicRefusalRaisedBeforeContent` (категория из `stop_details`, не повторяется), `TestAnthropicCostFromCatalog` (1M вход + 1M выход на `claude-opus-5-5` = $24.00), `TestAnthropicCacheReadsReported`, `TestAnthropicModelIDsHaveNoDateSuffix`, `TestAnthropicErrorMapping` (табличный: 401 → `ErrAuth`, 404 → `ErrModelNotAvailable`, 429 → `RateLimitedError`, 400 → `ErrBadRequest`, 529/500 → `ErrUnavailable`), `TestAnthropicKeyAbsentFromErrorText`, `TestAnthropicMaxTokensAlwaysSent`.
- [ ] **Step 2–4:** FAIL → реализовать (повторы SDK отключить: `option.WithMaxRetries(0)` — повторами управляет очередь, иначе бюджет повторов умножается) → PASS + lint. **Step 5:** Коммит — `feat: адаптер Anthropic (effort вместо temperature, отказы, кэш)`.

---

### Task 18: Конфигурации провайдеров

**Files:** Create `migrations/00004_provider_configs.sql`, `internal/db/models/provider_config.go`, `internal/providers/registry.go`, `internal/httpapi/providers.go`, `internal/logging/redact.go`; Test `providers_api_test.go`, `redact_test.go`

**Interfaces:**
- Produces: `models.ProviderConfig{Base; OwnerID; ProviderID, BaseURL, SecretCiphertext, KeyFingerprint, KeyLast4 string; AllowExternal bool}` — JSON-тег `-` у `SecretCiphertext`.
- `providers.Build(cfg models.ProviderConfig, box *security.SecretBox, settings config.Settings) (Provider, error)` — ключ расшифровывается только здесь.
- Эндпоинты ТЗ §12: `GET /api/providers` (каталог с `sends_data_externally`), `GET /api/providers/{id}/models`, `POST /api/provider-configs`, `GET /api/provider-configs`, `GET /api/provider-configs/{id}`, `DELETE /api/provider-configs/{id}`.
- `logging.NewRedactingHandler(inner slog.Handler) slog.Handler` — вырезает подстроки вида `sk-[A-Za-z0-9_-]{8,}` и значения атрибутов с ключами `api_key`, `password`, `token`.

- [ ] **Step 1: Тесты:** `TestAPIKeyNeverReturned` (ни в создании, ни в списке, ни в деталях; есть `key_last4` и 8-символьный `key_fingerprint`), `TestAPIKeyAbsentFromLogs` (буферный `slog`-обработчик за весь запрос), `TestCiphertextInDBIsNotPlaintext`, `TestForeignProviderConfigIs404`, `TestOllamaNeedsNoKey`, `TestProvidersDeclareExternalEgress` (ТЗ §6.3), `TestRedactingHandlerMasksKeys`, `TestCreateConfigWithoutSecretKeyConfiguredIs503` (сервер без `LINGUASTACK_SECRET_KEY` не принимает ключи, но сообщает об этом понятно).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: конфигурации провайдеров с шифрованием ключей`.

---

### Task 19: Сегментатор и батчер

**Files:** Create `internal/translation/segmenter.go`; Test `segmenter_test.go`

**Interfaces:**
- Produces: `translation.SegmentRef{NodeID string; ChapterIndex, Ordinal int; Type cdm.BlockType; Text, SourceHash string; Level int}`; `translation.Batch{Segments []SegmentRef; ContextBefore, ContextAfter string}`; `translation.Segment(doc cdm.Document) []SegmentRef`; `translation.BuildBatches(segs []SegmentRef, maxSegments, maxChars int) ([]Batch, error)`; `*SegmentTooLargeError{NodeID string; Chars, Limit int}`.
- Длина — в рунах (`utf8.RuneCountInString`), не в байтах: кириллица занимает 2 байта на символ, и лимит в байтах вдвое урезал бы русские батчи.

- [ ] **Step 1: Тесты:** `TestOneSegmentPerTranslatableBlock`, `TestBatchesRespectSegmentLimit` (25 → 10/10/5), `TestBatchesRespectCharLimit`, `TestBatchNeverSpansChapters`, `TestContextCarriesNeighbours`, **Review Focus №1** `TestCodeOnlyChapterYieldsNoSegments`, **Review Focus №3** `TestOversizedBlockIsNamedError` (`NodeID == "ch0000/b0000"`, `Chars == 50000`, `Limit == 6000`), `TestBlankBlocksSkipped`, `TestCharLimitCountsRunesNotBytes`.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: сегментатор CDM и батчер с контекстом соседей`.

---

### Task 20: Конверт и его парсер

**Files:** Create `internal/translation/prompt.go`; Test `prompt_test.go`

**Interfaces:**
- Produces: `translation.BuildSystemPrompt(src, tgt, style string, glossary map[string]string) string` — **детерминирован** (ключи глоссария сортируются, никаких дат и идентификаторов — иначе рушится кэш префикса, ADD §6.2); `translation.BuildUserPrompt(b Batch) string`; `translation.ParseEnvelope(text string, expected int) (map[int]string, error)`; `*ProtocolError{Reason, Raw string}`.

- [ ] **Step 1: Тесты:** `TestParseWellFormed`, `TestParseToleratesPreambleAndTrailer`, `TestParseMissingSegment`, `TestParseExtraSegment`, `TestParseDuplicateIndex`, `TestParseMissingEndMarkerKeepsReceived`, `TestParseNoEnvelope`, `TestParseMultilineSegment`, `TestParseTrimsOnlyEnvelopeWhitespace` (внутренние переводы строк сохраняются), `TestSystemPromptDeterministic` (порядок глоссария не влияет), `TestSystemPromptMentionsLanguagesAndGlossary`, `TestUserPromptMarksContextSeparately` (контекст стоит до `<<<SEG 1>>>` и помечен как не подлежащий переводу).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: протокол нумерованного конверта и устойчивый парсер`.

---

### Task 21: Валидация

**Files:** Create `internal/translation/validation.go`; Test `validation_test.go`

**Interfaces:**
- Produces: `translation.Severity` (`OK`, `Warning`, `Error`, упорядочены); `translation.Finding{Code string; Severity Severity; Message string}`; `translation.ValidateSegment(in ValidationInput) []Finding`; `ValidationInput{Source, Translation, MaskedSource, MaskedTranslation, SourceLang, TargetLang string; Type cdm.BlockType; Level int}`; `translation.Worst([]Finding) Severity`.
- Таблица проверок и severity — **ровно** ADD §5. Маркеры сверяются на замаскированных текстах (до восстановления), ссылки и разметка — на восстановленных.

- [ ] **Step 1: Тесты** (табличный `TestValidateSegment`): чистый перевод → нет находок; пустой → Error `empty_translation`; потерянный маркер → Error `placeholder_mismatch`; переставленные маркеры → ok; изменилось число ссылок → Error `link_count_mismatch`; дословная копия длиной > 40 → Warning `untranslated`; короткое совпадение (`"SQL"`) → ok; соотношение длин вне [0.3, 3.0] → Warning `length_ratio`; изменилось число `**` → Warning `markup_mismatch`; уровень заголовка изменился → Warning `heading_level_changed`; одинаковая пара языков → без `untranslated`; `Worst` выбирает Error.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: валидация перевода`.

---

### Task 22: Модели перевода

**Files:** Create `migrations/00005_translation.sql`, `internal/db/models/translation.go`; Test `translation_models_test.go`

**Interfaces:**
- Produces (ТЗ §11):
  - `models.TranslationProject{Base; OwnerID, BookID, ProviderConfigID uuid.UUID; SourceLang, TargetLang, ModelID, Style, Determinism, Status string; SegmentsTotal, SegmentsDone, SegmentsFailed, WarningsCount int; CostUSD float64; UpdatedAt time.Time}`; статусы `draft, queued, running, paused, completed, completed_with_warnings, failed, cancelled`; `(p *TranslationProject) IsTerminal() bool`; `ProgressPercent() float64` — **0 сегментов = 100%** (Review Focus №1).
  - `models.TranslationSegment{Base; ProjectID uuid.UUID; NodeID string; ChapterIndex, Ordinal int; Type, SourceText, SourceHash string; TranslatedText *string; Status, Severity string; Findings datatypes.JSONSlice[translation.Finding]; Attempts int; LastError string; UpdatedAt time.Time}`; уникальность `(project_id, node_id)`; статусы `pending, running, completed, failed, stale`.
  - `models.TranslationSegmentRevision{Base; SegmentID uuid.UUID; TranslatedText, Origin string}` — `machine`, `manual`, `retry` (ТЗ §5.1).
  - `models.TranslationTask{Base; ProjectID, OwnerID uuid.UUID; Kind, Status string; Priority, Attempts, MaxAttempts int; LockedBy *string; LockedUntil, AvailableAt, StartedAt, FinishedAt *time.Time; LastError string}`; статусы **ровно** ТЗ §7.
  - Индексы: `translation_tasks(status, priority desc, created_at)`, `translation_tasks(owner_id, status)`, `translation_segments(project_id, status)`.
  - Внешние ключи с `ON DELETE CASCADE` от книги к проектам, сегментам, ревизиям, задачам — удаление книги не оставляет висящих строк (ТЗ §13).

- [ ] **Step 1: Тесты:** `TestSegmentNodeIDUniquePerProject`, **Review Focus №4** `TestTwoProjectsOnOneBookIndependent`, `TestRevisionHistoryPreserved`, `TestTaskStatusesMatchSpec`, `TestProgressPercentZeroSegmentsIs100`, `TestDeletingBookCascades`.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: модели проектов перевода, сегментов, ревизий и задач`.

---

### Task 23: Сервис перевода батча

**Files:** Create `internal/translation/service.go`; Test `service_test.go`

**Interfaces:**
- Produces: `translation.NewService(p providers.Provider, modelID string, opts ServiceOptions) *Service`; `(*Service).TranslateBatch(ctx, b Batch, system string, det providers.Determinism, src, tgt string) (BatchOutcome, error)`; `BatchOutcome{Results map[string]SegmentOutcome; ProviderCalls int; CostUSD float64}`; `SegmentOutcome{NodeID string; Translated *string; Severity Severity; Findings []Finding; Err string}`.
- Конвейер: `cdm.Mask` → `BuildUserPrompt` → `Complete` → `ParseEnvelope` → `VerifyPlaceholders` → `cdm.Unmask` → `ValidateSegment`.
- Деградация ADD §3: повтор батча (до 2 раз) → по одному сегменту → `Error` с сохранением последнего ответа в `Err`.
- **Разделение ответственности:** ошибки протокола и качества сервис обрабатывает сам; инфраструктурные (`Retryable(err) == true` и не `ProtocolError`) **возвращает наружу** — повторами с backoff и арендой управляет очередь. `RefusedError` превращается в `Error` у сегментов (переводить нечего, повтор бессмыслен).

- [ ] **Step 1: Тесты:** `TestHappyPathOneCall`, `TestProtocolViolationRetriesThenRecovers`, `TestPersistentViolationFailsSegmentsKeepingRaw`, `TestDroppedPlaceholderIsError`, `TestProtectedCodeRestoredVerbatim`, `TestModelNeverSeesRawCode`, `TestRefusalBecomesSegmentError`, `TestRateLimitPropagatesToCaller`, `TestEmptyTranslationIsError`, `TestSingletonFallbackSalvagesGoodSegments` (в батче из трёх один «ломающий» сегмент — два других переводятся).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: сервис перевода батча с маскированием, валидацией и деградацией`.

---

### Task 24: Очередь на арендах

**Files:** Create `internal/queue/repository.go`; Test `repository_test.go`

**Interfaces:**
- Produces: `queue.Enqueue(ctx, db, project *models.TranslationProject, priority int) (*models.TranslationTask, error)`; `queue.ClaimNext(ctx, db, workerID string, lease time.Duration, maxPerUser int) (*models.TranslationTask, error)` — `nil, nil` если задач нет; `queue.Heartbeat(ctx, db, taskID uuid.UUID, workerID string, lease time.Duration) (bool, error)`; `queue.Complete(...)`; `queue.Fail(ctx, db, taskID, workerID, errText string, retryIn time.Duration) error` — при исчерпании `MaxAttempts` статус `failed`, иначе `pending` с `available_at = now() + retryIn`; `queue.CancelForProject(ctx, db, projectID) error`.
- SQL захвата — **ровно** ADD §7 через `db.Raw(...).Scan(&task)`, дополненный условием `available_at <= now()`. Ключи `Heartbeat`/`Complete`/`Fail` проверяют `locked_by = workerID` — чужая аренда не продлевается и не завершается.
- **Тестовая изоляция:** `SKIP LOCKED` проверяется только на **разных соединениях**, поэтому тесты пакета используют `testdb.NewCommitting(t)` — реальные коммиты и удаление своих строк в `t.Cleanup`; тесты пакета не запускаются `t.Parallel()`.

- [ ] **Step 1: Тесты:** `TestClaimMarksRunning`, `TestClaimedTaskNotGivenToSecondWorker`, `TestConcurrentClaimsNeverDuplicate` (20 горутин на 10 задач — каждая задача выдана ровно одному), `TestExpiredLeaseReclaimable` (ТЗ §7, восстановление после перезапуска), `TestPerUserLimit` (ТЗ §7), `TestBusyUserDoesNotBlockOthers`, `TestPriorityBeforeFIFO`, `TestHeartbeatOnlyForHolder`, `TestCompletedNeverReclaimed`, `TestFailWithRetryDelaysAvailability`, `TestFailBeyondMaxAttemptsIsTerminal`.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: очередь на арендах с честностью и восстановлением после перезапуска`.

---

### Task 25: Worker

**Files:** Create `internal/queue/worker.go`, `cmd/worker/main.go`; Test `worker_test.go`

**Interfaces:**
- Produces: `queue.NewWorker(id string, db *gorm.DB, resolve ProviderResolver, cfg config.Settings) *Worker`; `type ProviderResolver func(ctx context.Context, project *models.TranslationProject) (providers.Provider, error)`; `(*Worker).RunOnce(ctx) (bool, error)`; `(*Worker).Run(ctx) error` (до отмены контекста; heartbeat в отдельной горутине).
- На задачу: материализовать сегменты из CDM идемпотентно (`ON CONFLICT (project_id, node_id) DO NOTHING`) → батчи из `pending`/`failed`-с-оставшимися-попытками → `TranslateBatch` → **запись после каждого батча** (ТЗ §7) с условием `status <> 'completed'` → пересчёт счётчиков проекта → проверка паузы/отмены между батчами.
- Статус проекта обновляется **только если он не терминальный**: `UPDATE … WHERE status NOT IN ('cancelled','failed')` — иначе запись после отмены «воскресит» проект (Review Focus №5).

- [ ] **Step 1: Тесты** (`workerEnv`: committing-БД, `Fake`, проекты из Markdown-строки):
  - `TestWorkerTranslatesWholeBook`.
  - `TestRerunDoesNotRetranslateCompleted` (ТЗ §7, §17).
  - `TestProgressSurvivesCrash` (ТЗ §15.8): Fake падает после первого вызова → часть сохранена → новый worker после истечения аренды доводит до конца.
  - `TestPauseStopsBetweenBatchesKeepingProgress`, `TestResumeContinuesWithoutRetranslating`.
  - **Review Focus №5:** `TestCancelMidBatchKeepsPaidResult`, `TestWritesAfterCancelDoNotRevive`.
  - **Review Focus №1:** `TestBookWithoutTranslatableBlocksCompletes` (`0/0`, статус `completed`, прогресс 100%).
  - `TestUnavailableProviderFailsTaskAfterAttempts`.
  - `TestOneBadSegmentDoesNotStopBook`.
  - `TestWorkerStopsOnContextCancel` (корректное завершение: текущий батч дописан, аренда отпущена).
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: worker очереди с идемпотентностью, паузой, отменой и восстановлением`.

---

### Task 26: API переводов, сегментов и поток прогресса

**Files:** Create `internal/httpapi/translations.go`, `segments.go`, `events.go`; Test `translations_test.go`, `isolation_matrix_test.go`

**Interfaces:**
- Produces (ТЗ §12): `POST /api/books/{id}/translations`, `GET /api/translations`, `GET /api/translations/{id}`, `POST /api/translations/{id}/pause|resume|cancel`, `GET /api/translations/{id}/segments`, `GET /api/segments/{id}/revisions`, `POST /api/segments/{id}/retry`, `PATCH /api/segments/{id}`, `GET /api/translations/{id}/events` (SSE, ADD §13: `http.Flusher`, кадр раз в секунду, закрытие на терминальном статусе и при отключении клиента).
- Переходы статусов — явная таблица; `resume` из `cancelled`/`completed` → 409.

- [ ] **Step 1: Тесты:** `TestCreateTranslationEnqueues`, `TestExternalProviderRejectedWhenEgressForbidden` (ТЗ §6.4), `TestTooManyActiveProjectsIs429`, `TestUnknownLanguageIs422`, `TestPauseResumeCancel`, `TestCancelledCannotResume`, `TestPatchSegmentRecordsManualRevision`, `TestRetrySegmentRequeuesOnlyIt`, `TestSSEEmitsProgressAndCloses`, `TestSSEStopsWhenClientDisconnects`.
  - `isolation_matrix_test.go` — табличный `TestEveryResourceEndpointIs404ForForeignUser` и `TestEveryResourceEndpointRequires401` по полному списку ресурсных маршрутов. **Список маршрутов берётся из зарегистрированного мультиплексора**, а не дублируется вручную: новый эндпоинт без записи в матрице роняет тест.
- [ ] **Step 2–4:** FAIL → реализовать → PASS + lint. **Step 5:** Коммит — `feat: API переводов, правка сегментов и поток прогресса`.

---

### Task 27: Сквозной сценарий, развёртывание, CI

**Files:** Create `internal/e2e/e2e_test.go`, `.github/workflows/ci.yml`, `backend/Dockerfile`, `CLAUDE.md`; Modify `README.md`, `docker-compose.yml` (сервисы `api` и `worker`)

- [ ] **Step 1: Тесты:** `TestFullCycleImportTranslateValidateRead` (ТЗ §17: книга с кодом и ссылкой → перевод через Fake → код не тронут, ссылка сохранена, все сегменты переведены); `TestOriginalUnchangedAfterTranslation` (ТЗ §15.9); `TestLiveOllamaEndToEnd` (`qwen3:8b`).
- [ ] **Step 2:** `make test && make lint` — всё зелёное.
- [ ] **Step 3:** `make test-live` — книга реально переводится локальной моделью.
- [ ] **Step 4:** Dockerfile (многоэтапный, `golang:1.27` → `distroless/static`), сервисы `api` и `worker` в compose с `restart: unless-stopped` (ТЗ §13); README — развёртывание, резервное копирование (`pg_dump` + `storage/`).
- [ ] **Step 5:** CI: Postgres как service-контейнер, `actions/setup-go` с `go-version-file: backend/go.mod`, `golangci-lint` v2.14.0, `go test ./...`.
- [ ] **Step 6:** Коммит — `feat: сквозной тест, развёртывание в Docker и CI`.

---

## За рамками этого плана

| Этап | Содержание |
|---|---|
| 4 | Vue 3 + TypeScript: библиотека, импортёр, монитор задач, читалка (ТЗ §8) |
| 5 | PDF, DOCX, HTML; экспорт EPUB/MD/DOCX/HTML/TXT (ТЗ §3.2) |
| 6 | Telegram-бот (ТЗ §9) |
| 7 | Резервное копирование, метрики, лимиты нагрузки, доверенные прокси для IP, расширенный CI/CD (ТЗ §13) |

Критерии §15.11 (читалка), §15.12 (экспорт), §15.13 (Telegram) закрываются этими этапами; остальные двенадцать — этим планом (трассировка в ADD).
