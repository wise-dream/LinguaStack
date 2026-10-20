# LinguaStack — Этап 7: надёжность. План реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** этап 7 ТЗ (§16): ограничение частоты запросов и квоты, метрики
и журнал запросов, админ-панель (§2.1, §8.1 №12), смена пароля и почты и
восстановление доступа (§2.1), проверяемое восстановление из резервной
копии (§13, §14), публикация образа (CI/CD).

**Architecture:** всё в существующих процессах. api получает middleware
журнала запросов и метрик, ограничитель частоты в памяти (api — один на
развёртывание), админские маршруты под `requireAdmin`. Метрики — официальный
клиент Prometheus: HTTP — в процессе, состояние очереди и данных — из БД
при опросе. Квоты пользователя — колонки `users` (пусто — значение по
умолчанию из настроек). Сброс пароля — одноразовый код (хеш в БД) по почте
(SMTP), в Telegram или ссылкой от администратора. Бэкап — скрипты вокруг
`pg_dump` и тома `storage` и «учебная тревога» в CI.

**Tech Stack:** `github.com/prometheus/client_golang` v1.24.1 (2026-07-24;
v1.25.0 моложе двух недель); SMTP — `net/smtp` stdlib; остальное — как на
этапах 1–6.

**Spec:** `PROJECT` §2.1 (восстановление доступа, профиль, роль admin), §8.1
№12, §13 (частота запросов, метрики, бэкап, автозапуск), §14
(«восстановление из резервной копии», изоляция), §16 (этап 7); дополнение
§9, §10, §18 (добавляется задачей 0).

## Global Constraints

- Роль admin не даёт доступа к содержимому книг (ТЗ §2.1, дополнение §9):
  админские ответы — метаданные и счётчики; матрица изоляции проходит и
  для администратора-«чужака».
- Неадминистратору админские маршруты отвечают 404 (как чужой ресурс).
- Запрос сброса пароля отвечает одинаково для любого адреса: по ответу
  нельзя узнать, есть ли такой аккаунт.
- Секреты (токен метрик, пароль SMTP, коды сброса) — не в логах; коды —
  хешем в БД.
- Скрипты бэкапа и проверка восстановления не трогают чужие контейнеры и
  рабочую базу разработки: свой проект compose и свои порты.
- Правила `CLAUDE.md`: TDD, миграции goose, новый маршрут — в
  `httpapi.Routes()`, интерфейс по-русски с проверкой на 390 px; гейт
  коммита — `&&` и `set -o pipefail`.

## Review Focus

1. Администратор открывает книгу, главу, ресурс, перевод или экспорт
   другого пользователя: 404, как у любого чужака. Тест — задача 3.
2. Перебор кодов сброса пароля и регистраций с одного адреса; клиент
   подставляет `X-Forwarded-For`, чтобы обойти лимит. Тест — задачи 1, 6.
3. Отключённый пользователь с открытой вкладкой и идущим переводом:
   сессии гаснут, переводы встают на паузу. Тест — задача 3.
4. Восстановление из бэкапа на чистые тома: книга, ресурсы, перевод и
   экспорт на месте и открываются. Тест — задача 7.
5. Опрос `/metrics` на большой базе не кладёт её: запросы-счётчики дешёвые,
   доступ — только с токеном. Тест — задача 2.

---

## Задача 0: решения в дополнении к ТЗ

- [ ] **Step 1:** §18 — ограничение частоты (значения), доверенные прокси,
  метрики (имена, доступ), админ-панель (что видит и меняет), квоты,
  смена пароля и почты, сброс пароля (каналы, срок 1 ч), бэкап и
  проверка восстановления, публикация образа; §10 — новые лимиты.
  **Step 2:** коммит.

## Task 1: доверенные прокси и ограничение частоты

**Files:** Create `backend/internal/security/limiter.go` (+ тест),
`backend/internal/httpapi/ratelimit.go` (+ тест); Modify `auth.go`
(`clientIP`), `server.go`, `config.go`, `.env.example`.

**Interfaces:**
- Produces: `security.NewLimiter(rate float64, burst int) *Limiter`,
  `(*Limiter).Allow(key string) (bool, time.Duration)` (корзина токенов,
  неиспользуемые ключи забываются); `clientIP` учитывает
  `X-Forwarded-For` только если адрес соединения — из
  `LINGUASTACK_TRUSTED_PROXIES` (CIDR через запятую), берётся правый
  недоверенный адрес; настройки `TrustedProxies`, `RateLimitRPS` (20),
  `RateLimitBurst` (60), `RegisterPerHour` (5), `ResetPerHour` (5),
  `UploadsPerHour` (30).

- [ ] **Step 1:** Тесты: `TestLimiterBucket` (burst, пополнение, Retry-After);
  `TestClientIPTrustedProxy` (без доверия — адрес соединения; с доверием —
  правый недоверенный из цепочки; подделанный левый не помогает);
  `TestAPIRateLimitedPerUser` (сверх burst — 429, `Retry-After`, другой
  пользователь не задет); `TestRegisterRateLimitedPerIP`;
  `TestUploadRateLimited`; `TestHealthNotLimited`.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: ограничение частоты запросов и доверенные прокси`.

## Task 2: метрики и журнал запросов

**Files:** Create `backend/internal/metrics/metrics.go` (+ тест),
`backend/internal/httpapi/observe.go` (+ тест); Modify `server.go`,
`config.go`, `go.mod`, `cmd/worker/main.go` (метрики worker-а — счётчики
батчей и вызовов в общем реестре процесса, адрес
`LINGUASTACK_WORKER_METRICS_ADDR`, по умолчанию выключен).

**Interfaces:**
- Produces: `GET /metrics` — формат Prometheus, только с
  `Authorization: Bearer <LINGUASTACK_METRICS_TOKEN>` (без токена в
  настройках маршрута нет — 404); метрики
  `linguastack_http_requests_total{method,route,status}`,
  `linguastack_http_request_duration_seconds{route}`,
  `linguastack_users`, `linguastack_books`, `linguastack_projects{status}`,
  `linguastack_tasks{status}`, `linguastack_exports{status}`,
  `linguastack_notifications{status}`, `linguastack_provider_cost_usd_total`,
  `linguastack_provider_tokens_total{direction}` + стандартные go/process;
  `metrics.Snapshot(ctx, db) (Snapshot, error)` — те же счётчики из БД
  для админ-панели; журнал: строка на запрос (метод, шаблон маршрута,
  статус, длительность, пользователь), `X-Request-ID` (входящий или новый)
  — в ответе и в записи журнала, в том числе в «внутренней ошибке».

- [ ] **Step 1:** Тесты: `TestMetricsRequireToken` (нет токена в
  настройках — 404; неверный — 401; верный — 200 и имена метрик);
  `TestHTTPMetricsByRoutePattern` (метка — шаблон `/api/books/{id}`, не
  настоящий UUID); `TestSnapshotCounts`; `TestAccessLogAndRequestID`
  (запись с маршрутом и статусом, без cookie и тел; ID пробрасывается).
- [ ] **Step 2–4.** **Step 5:** коммит `feat: метрики Prometheus и журнал запросов`.

## Task 3: администратор — пользователи и обзор

**Files:** Create `backend/migrations/00011_admin_quotas.sql` (квоты,
`last_seen_at`), `backend/internal/httpapi/admin.go` (+ тест); Modify
`cmd/api/main.go` (флаг `-set-role email=role`), `server.go`,
`isolation_matrix_test.go`, `middleware.go`.

**Interfaces:**
- Produces: `api -set-role admin@example.com=admin`;
  `GET /api/admin/overview` → `{"metrics": Snapshot, "providers": [...]}`
  (Ollama сервера: доступен ли, модели); `GET /api/admin/users` (почта,
  роль, активен, создан, последний визит, книг, переводов, байт
  хранилища, квоты); `PATCH /api/admin/users/{id}` (`is_active`, `role`,
  `max_active_projects`, `max_concurrent_tasks`, `storage_quota_bytes`;
  `null` — по умолчанию); отключение гасит сессии и ставит на паузу
  активные переводы; себя отключить и разжаловать нельзя (409).

- [ ] **Step 1:** Тесты: `TestAdminRoutesHiddenFromUsers` (404);
  `TestAdminOverview`; `TestAdminUsersListNoContent` (в ответе нет
  названий книг и текста); `TestAdminDeactivate` (сессии пользователя —
  401, переводы — paused, вход отклонён); `TestAdminCannotDemoteSelf`;
  `TestSetRoleCLI`; матрица изоляции: `TestAdminHasNoContentAccess` —
  все ресурсные маршруты для администратора-чужака — 404.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: администратор — пользователи, обзор, отключение`.

## Task 4: квоты пользователя

**Files:** Modify `httpapi/books.go`, `httpapi/translations.go`,
`queue/repository.go` (захват с квотой пользователя), `config.go`
(`StorageQuotaBytes` = 2 ГиБ; 0 — без предела) (+ тесты).

- [ ] **Step 1:** Тесты: `TestStorageQuotaRefusesUpload` (413 «Превышена
  квота хранилища», файлов не осталось); `TestUserQuotaOverridesDefault`
  (квота активных переводов из `users` вместо настройки);
  `TestClaimRespectsUserTaskQuota`.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: квоты пользователя`.

## Task 5: смена пароля и почты

**Files:** Modify `httpapi/auth.go` (+ тест), `server.go`.

**Interfaces:** `POST /api/me/password` (`current_password`,
`new_password`) — гасит остальные сессии; `PATCH /api/me` (`email`,
`current_password`) — 409, если почта занята.

- [ ] **Step 1:** Тесты: неверный текущий пароль — 403 (не 401: сессия
  жива); короткий новый — 422; прочие сессии погашены, текущая жива;
  смена почты, занятая почта — 409, вход по новой почте.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: смена пароля и почты`.

## Task 6: восстановление доступа

**Files:** Create `backend/migrations/00012_password_resets.sql`,
`backend/internal/mail/mail.go` (+ тест с поддельным SMTP-сервером),
`backend/internal/httpapi/reset.go` (+ тест); Modify `config.go`,
`server.go`, `.env.example`.

**Interfaces:** `POST /api/auth/password-reset` (`email`) → 202 всегда;
код (32 байта, хеш, 1 ч) уходит письмом (`LINGUASTACK_SMTP_*`) и/или в
Telegram (привязка и токен бота); `POST /api/auth/password-reset/confirm`
(`token`, `password`) → 204, все сессии погашены; `POST
/api/admin/users/{id}/password-reset` → `{"url"}` для передачи вручную;
ссылка — `<PUBLIC_URL>/reset-password?token=…`.

- [ ] **Step 1:** Тесты: `TestResetRequestSameAnswerForUnknownEmail`;
  `TestResetViaMail` (письмо с ссылкой на поддельный SMTP); `TestResetViaTelegram`
  (поддельный Bot API); `TestResetConfirm` (пароль сменён, сессии погашены,
  код одноразовый, просроченный — 422); `TestResetRateLimitedPerIP`;
  `TestAdminIssuesResetLink`; `TestResetDisabledUserNotSent`.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: восстановление доступа`.

## Task 7: бэкап и проверка восстановления

**Files:** Create `scripts/backup.sh`, `scripts/restore.sh`,
`scripts/backup-drill.sh`; Modify `docker-compose.yml` (порт БД —
`LINGUASTACK_DB_PORT`), `.github/workflows/ci.yml` (задание
`backup-drill`), `README.md`.

- [ ] **Step 1:** `backup.sh` — `pg_dump -Fc` и архив тома `storage` с
  датой, хранит последние N (`KEEP`, 14), проверяет, что дамп читается
  (`pg_restore --list`). `restore.sh` — останавливает api, worker, bot,
  восстанавливает БД и том, запускает. `backup-drill.sh` — свой проект
  compose (`lsdrill`, свои порты): поднять стек, через API —
  пользователь, книга с картинкой, позиция чтения; бэкап; `down -v` (тома стёрты); `up`; восстановление; вход
  тем же паролем, книга и картинка на месте; убрать за собой.
  **Step 2:** прогнать локально. **Step 3:** CI. **Step 4:** коммит
  `feat: скрипты бэкапа и проверка восстановления`.

## Task 8: публикация образа

**Files:** Create `.github/workflows/release.yml`; Modify
`docker-compose.yml` (`image: ${LINGUASTACK_IMAGE:-linguastack:local}`),
`README.md`.

- [ ] **Step 1:** по тегу `v*` и ручному запуску — сборка и публикация
  `ghcr.io/<владелец>/linguastack:<тег>` и `:latest` (GITHUB_TOKEN,
  `packages: write`); actionlint. Развёртывание из реестра:
  `LINGUASTACK_IMAGE=… docker compose pull && docker compose up -d`.
  **Step 2:** коммит `ci: публикация образа в GHCR`.

## Task 9: веб-приложение — админ-панель, профиль, восстановление

**Files:** Create `frontend/src/pages/AdminPage.vue` (+ тест),
`ForgotPasswordPage.vue`, `ResetPasswordPage.vue` (+ тесты),
`frontend/src/api/admin.ts`; Modify `SettingsPage.vue` (пароль, почта),
`LoginPage.vue` («Забыли пароль?»), `router.ts`, `AppHeader.vue`
(«Администрирование» — только админу), `e2e`.

- [ ] **Step 1:** Тесты: админ-панель — обзор (числа, провайдер),
  таблица пользователей, отключение через подтверждение, квоты, ссылка
  сброса; не админ — раздела нет; настройки — смена пароля (ошибка
  текущего пароля показана), почты; «Забыли пароль?» — одинаковое
  сообщение; сброс по ссылке → вход. Сквозной сценарий: забыли пароль →
  сообщение; смена пароля в настройках → выход и вход новым.
- [ ] **Step 2–4.** **Step 5:** коммит `feat: админ-панель, профиль и восстановление доступа в веб-приложении`.

## Task 10: живая проверка, документация, ревью

- [ ] Метрики в compose (`curl` с токеном), лимит частоты на живом api,
  админ через `-set-role`, сброс пароля через поддельный SMTP, учебная
  тревога бэкапа; README, дополнение, статус плана; полный прогон;
  ревью; слияние.

## За рамками этого плана

- Подтверждение почты при регистрации и смене почты (нужен обязательный
  SMTP; сейчас он необязателен).
- Распределённый лимитер (Redis) — api один на развёртывание.
- Алерты и дашборды Grafana — снаружи, по метрикам.
