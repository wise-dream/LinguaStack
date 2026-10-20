# LinguaStack — Этап 6: Telegram. План реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Telegram-бот по ТЗ §9 и §16 (этап 6): привязка Telegram к
аккаунту, уведомления о готовности главы, о завершении книги и о
критических ошибках со ссылками на веб-приложение, команды `/start`,
`/status`, `/help`, пауза и возобновление перевода из бота, настройки
уведомлений в веб-приложении (§8.1 №11).

**Architecture:** новый процесс `bot` (`cmd/bot`) — единственный, кто
говорит с Telegram: long polling `getUpdates` (публичный адрес не нужен) и
доставка уведомлений из таблицы-«исходящих» `telegram_notifications`.
События пишет worker (глава готова, перевод завершён или упал) — в БД, а
не в Telegram: доставка переживает перезапуски, повторяется после сбоев,
соблюдает лимиты Telegram и склеивает несколько «готова глава» в одно
сообщение. Привязка — одноразовая ссылка `t.me/<бот>?start=<код>` из
веб-приложения. Пауза и возобновление — общий код с HTTP API.

**Tech Stack:** Telegram Bot API через `net/http` + `encoding/json` (свой
клиент на шесть методов, без зависимостей); названия языков —
`golang.org/x/text/language/display` (модуль уже в проекте); остальное —
как на этапах 1–5.

**Spec:** `PROJECT` §9 (бот), §2.1–2.2 (настройка уведомлений, изоляция
привязок), §8.1 №11, §11 (`TelegramBinding`), §12 (`POST /api/telegram/link`),
§13 (секреты), §14 («Telegram-привязка и уведомления»), §15 №13;
дополнение §17 (добавляется задачей 0).

## Global Constraints

- Без новых зависимостей: клиент Bot API — стандартная библиотека.
- Токен бота — только в окружении (`LINGUASTACK_TELEGRAM_BOT_TOKEN`), не
  в логах и не в текстах ошибок: запросы идут на `…/bot<токен>/метод`, и
  ошибки `net/http` содержат адрес — клиент их пересобирает, фильтр логов
  дополнительно маскирует строки вида `123456:AA…`.
- Бот отвечает только в личных чатах: в группе названия книг увидели бы
  другие (ТЗ §9 «не раскрывать содержимое приватных книг»). Сообщения
  содержат названия, языки, счётчики и ссылки — не текст книги.
- Управлять задачами из бота можно только после привязки и только своими
  (проверка владельца на каждом нажатии).
- Правила `CLAUDE.md`: TDD, миграции goose, 404 вместо 403, новый маршрут —
  в `httpapi.Routes()`, интерфейс по-русски, проверка на 390 px.

## Review Focus

1. Чужой или просроченный код привязки, повторное использование кода,
   Telegram-аккаунт, уже привязанный к другому пользователю: привязка
   либо не происходит с понятным ответом, либо переносится явно. Тест —
   задача 4.
2. Нажатие «Пауза» на перевод другого пользователя (подделанный
   callback_data): отказ, проект не меняется. Тест — задача 4.
3. Telegram отвечает 429 (лимит), 403 (бот заблокирован), 5xx, сеть
   недоступна: уведомление не теряется и не повторяется бесконечно,
   процесс жив. Тест — задача 6.
4. Перевод книги из 200 глав на быстрой модели: пользователь не получает
   200 сообщений подряд. Тест — задача 6.
5. Токен бота в тексте ошибки или логе. Тест — задачи 1 и 7.

---

> **Статус: ✅ завершён** (задачи 0–9). Отклонения и находки:
>
> - **Живая проверка** без настоящего Telegram: api, worker и bot против
>   поддельного Bot API (процесс на Python) — привязка по ссылке, перевод
>   «Алисы» с уведомлениями, `/status`, «Приостановить» кнопкой, `/unlink`;
>   токена в логах нет. Нашла: склейка глав только в пределах одного
>   прохода доставщика не спасала от потока сообщений при быстрой модели —
>   теперь сообщение о главах не чаще раза в 5 минут на перевод, «готово»
>   отменяет отложенные главы (15 глав «Алисы» — два сообщения).
> - **Ревью ветки (самопроверка)** нашло: `/status` с десятью длинными
>   названиями превышал предел Telegram в 4096 символов — названия
>   обрезаются, размер ограничен; устаревшая ссылка привязки не
>   останавливала опрос страницы настроек.
> - **Чужой `translation_id`** и прочие ответы API — как в плане; в боте
>   всё по плану, кроме: тесты `cmd/bot` писались вместе с кодом.
> - **Живой бот** с настоящим токеном не проверялся — токена нет; для
>   включения — README, раздел «Telegram-бот».

## Задача 0: решения в дополнении к ТЗ

**Files:** Modify `docs/spec/SPEC-ADDENDUM.md` (новый §17, §10 лимиты,
трассируемость №13).

- [x] **Step 1:** §17 — процесс `bot`, outbox, привязка (код 32 байта,
  base64url, TTL 15 мин, в БД — sha256), личные чаты, склейка глав,
  повтор доставки (429 — по `retry_after`, 5xx и сеть — 30 с × 2ⁿ до 5
  попыток, 403 — пропуск), формат сообщений, команды. **Step 2:** коммит.

## Task 1: клиент Telegram Bot API

**Files:** Create `backend/internal/telegram/client.go`,
`backend/internal/telegram/client_test.go`; Modify
`backend/internal/logging/redact.go` (+ тест).

**Interfaces:**
- Produces: `telegram.NewClient(baseURL, token string, hc *http.Client) *Client`;
  `(*Client).GetMe(ctx) (User, error)`,
  `GetUpdates(ctx, offset int64, timeout time.Duration) ([]Update, error)`,
  `SendMessage(ctx, Message) (int64, error)` (`Message{ChatID int64; Text string; Keyboard [][]Button}` — HTML,
  без превью ссылок), `EditMessage(ctx, chatID, messageID int64, text string, keyboard [][]Button) error`,
  `AnswerCallback(ctx, id, text string) error`, `SetCommands(ctx, []Command) error`;
  типы `Update{UpdateID; Message *IncomingMessage; Callback *CallbackQuery}`,
  `IncomingMessage{MessageID; From User; Chat Chat; Text}`,
  `Chat{ID int64; Type string}`, `User{ID int64; Username, FirstName string}`,
  `CallbackQuery{ID string; From User; Message *IncomingMessage; Data string}`,
  `Button{Text, CallbackData, URL string}`, `Command{Command, Description string}`;
  ошибка `*telegram.APIError{Code int; Description string; RetryAfter time.Duration}`
  (`Temporary() bool` — 429 и 5xx); сетевые ошибки — без адреса.

- [x] **Step 1:** Тесты на httptest-сервере: `TestSendMessageHTML`
  (метод, `chat_id`, `parse_mode=HTML`, `link_preview_options.is_disabled`,
  клавиатура), `TestGetUpdatesOffsetTimeout`, `TestAPIErrorRetryAfter`
  (429 с `parameters.retry_after`), `TestAPIErrorForbidden` (403 не
  временная), `TestNetworkErrorHidesToken` (сервер закрыт — в тексте
  ошибки нет токена), `TestRedactsTelegramToken` (фильтр логов).
- [x] **Step 2:** FAIL. **Step 3:** реализация. **Step 4:** PASS, lint.
  **Step 5:** коммит `feat: клиент Telegram Bot API`.

## Task 2: пауза, возобновление и отмена — общий код

**Files:** Create `backend/internal/queue/control.go`,
`backend/internal/queue/control_test.go`; Modify
`backend/internal/httpapi/translations.go`.

**Interfaces:**
- Produces: `queue.Pause(ctx, db, projectID, ownerID uuid.UUID) (*models.TranslationProject, error)`,
  `queue.Resume(ctx, db, projectID, ownerID uuid.UUID, maxAttempts int) (*models.TranslationProject, error)`,
  `queue.Cancel(ctx, db, projectID, ownerID uuid.UUID) (*models.TranslationProject, error)`;
  ошибки `store.ErrNotFound` (чужой или нет), `queue.ErrBadTransition`
  (`*TransitionError{From string}`).

- [x] **Step 1:** Тесты: `TestPauseResumeCancel` (переходы и задачи
  очереди), `TestControlForeignProjectNotFound`, `TestBadTransition`.
  Существующие тесты HTTP паузы/возобновления — без изменений.
- [x] **Step 2–4:** FAIL → реализация (обработчики HTTP вызывают её) → PASS.
  **Step 5:** коммит `refactor: управление переводом — общий код для API и бота`.

## Task 3: схема и привязка из веб-приложения

**Files:** Create `backend/migrations/00010_telegram.sql`,
`backend/internal/db/models/telegram.go`, `backend/internal/httpapi/telegram.go`,
`backend/internal/httpapi/telegram_test.go`; Modify `config.go`, `server.go`,
`.env.example`.

**Interfaces:**
- Produces: таблицы `telegram_bindings(user_id PK, telegram_user_id UNIQUE,
  chat_id, username, notify_chapters, notify_completed, notify_errors,
  created_at, updated_at)`, `telegram_link_tokens(token_hash PK, user_id
  UNIQUE, expires_at, created_at)`, `telegram_notifications(id, user_id,
  project_id, kind, chapter_index, dedupe_key UNIQUE, status, attempts,
  available_at, last_error, created_at, sent_at)`; модели
  `models.TelegramBinding`, `models.TelegramLinkToken`,
  `models.TelegramNotification`; настройки `TelegramBotToken`,
  `TelegramBotUsername`, `TelegramAPIURL` (по умолчанию
  `https://api.telegram.org`), `PublicURL`;
  API: `GET /api/telegram` → `{"enabled","bot_username","linked","username","linked_at","notify_chapters","notify_completed","notify_errors"}`,
  `POST /api/telegram/link` → `{"url","expires_at"}` (новый код
  заменяет прежний), `PATCH /api/telegram` (флаги уведомлений),
  `DELETE /api/telegram` → 204;
  `telegram.LinkCode() (code string, hash []byte)`, `telegram.HashCode(code string) []byte`.

- [x] **Step 1:** Тесты: `TestTelegramDisabled` (без токена — `enabled:false`,
  `POST link` → 503 «Telegram-бот не настроен»); `TestTelegramLinkURL`
  (`https://t.me/<бот>?start=<43 символа>`, в БД — хеш, не код; второй
  запрос заменяет код); `TestTelegramSettings` (PATCH флагов без привязки
  — 409, с привязкой — сохраняются); `TestTelegramUnlink`;
  `TestTelegramRoutesRequireAuth` — через общий тест матрицы.
- [x] **Step 2–4.** **Step 5:** коммит `feat: привязка Telegram в API`.

## Task 4: бот — команды и привязка

**Files:** Create `backend/internal/bot/bot.go` (цикл getUpdates и
разбор), `backend/internal/bot/commands.go`, `backend/internal/bot/status.go`,
`backend/internal/bot/bot_test.go`, `backend/internal/telegram/fake_test.go`
→ `backend/internal/telegram/telegramtest/fake.go` (поддельный сервер для
тестов bot и notify).

**Interfaces:**
- Consumes: клиент (задача 1), `queue.Pause/Resume` (задача 2), таблицы
  задачи 3.
- Produces: `bot.New(db *gorm.DB, tg *telegram.Client, cfg config.Settings) *Bot`,
  `(*Bot).HandleUpdate(ctx, telegram.Update) error`, `(*Bot).Run(ctx) error`;
  `telegramtest.NewServer(t) *Server` (`Updates(...)`, `Sent() []telegram.Message`, `URL()`).

- [x] **Step 1:** Тесты: `TestStartLinksAccount` (`/start <код>` в личном
  чате → привязка, код удалён, ответ «Готово: …»); `TestStartCodeReuseOrExpired`
  («код не подходит или устарел»); `TestStartMovesTelegramToNewAccount`
  (тот же Telegram, код другого аккаунта → привязка переносится);
  `TestGroupChatRefused` (любая команда в группе — «только в личном чате»,
  привязки нет); `TestStatusUnlinked` (инструкция); `TestStatusLists`
  (активные первыми, прогресс «45% (390 из 873)», язык по-русски, ссылка,
  кнопки «Приостановить»/«Продолжить» только для подходящих статусов,
  не больше 10 переводов); `TestPauseCallback` (проект на паузе, ответ на
  нажатие, сообщение обновлено); `TestForeignCallbackRefused` (Review
  Focus №2); `TestHelp`, `TestUnlinkCommand`, `TestUnknownText`.
- [x] **Step 2–4.** **Step 5:** коммит `feat: команды Telegram-бота и привязка`.

## Task 5: события уведомлений в worker

**Files:** Create `backend/internal/notify/enqueue.go`,
`backend/internal/notify/enqueue_test.go`; Modify `backend/internal/queue/worker.go`
(+ тест).

**Interfaces:**
- Produces: `notify.ChaptersDone(ctx, db, projectID uuid.UUID, nodeIDs []string) error`,
  `notify.Completed(ctx, db, projectID uuid.UUID) error`,
  `notify.Failed(ctx, db, projectID uuid.UUID) error` — запись, только если у
  владельца есть привязка с включённым видом уведомлений; повтор события —
  `ON CONFLICT (dedupe_key) DO NOTHING`.

- [x] **Step 1:** Тесты: `TestChapterDoneOnceWhenAllSegmentsProcessed`
  (глава с pending — нет; все completed/failed — одна запись, повтор не
  дублирует); `TestNoBindingNoNotification`; `TestSettingDisabledNoNotification`;
  `TestWorkerEnqueuesChapterAndCompleted` (книга из двух глав через Fake —
  две главы и «готово»); `TestWorkerEnqueuesFailed` (неповторяемая ошибка
  провайдера); правка сегмента через API не создаёт «failed».
- [x] **Step 2–4.** **Step 5:** коммит `feat: события уведомлений Telegram из worker`.

## Task 6: доставка уведомлений

**Files:** Create `backend/internal/notify/dispatch.go`,
`backend/internal/notify/messages.go`, `backend/internal/notify/dispatch_test.go`.

**Interfaces:**
- Produces: `notify.NewDispatcher(db *gorm.DB, sender Sender, cfg config.Settings) *Dispatcher`,
  `(*Dispatcher).RunOnce(ctx) (int, error)`, `(*Dispatcher).Run(ctx) error`;
  `notify.Sender interface { SendMessage(ctx, telegram.Message) (int64, error) }`.

- [x] **Step 1:** Тесты: `TestCompletedMessage` (название и язык,
  «873 из 873», замечания, ссылка `…/translations/{id}`, HTML экранирован:
  название `<b>&` не ломает разметку); `TestChaptersCoalesced` (пять глав
  проекта — одно сообщение «готовы главы «A», «B», «C» и ещё 2»);
  `TestCompletedSupersedesChapters` (есть «готово» — главы того же
  проекта не шлются); `TestFailedMessage`; `TestRetryAfter429`
  (`available_at` = now + retry_after, попытка не тратится);
  `TestTransientBackoffThenFailed` (5xx 5 раз → failed);
  `TestBlockedBotSkipped` (403 → skipped); `TestUnlinkedSkipped`;
  `TestSettingTurnedOffSkipped`; `TestNoPublicURLNoLinks`.
- [x] **Step 2–4.** **Step 5:** коммит `feat: доставка уведомлений Telegram`.

## Task 7: процесс bot и развёртывание

**Files:** Create `backend/cmd/bot/main.go`; Modify `Dockerfile`
(собрать `./cmd/bot`), `docker-compose.yml` (сервис `bot`), `backend/Makefile`
(`run-bot`), `.github/workflows/ci.yml` (если нужно), `.env.example`.

- [x] **Step 1:** `cmd/bot`: без токена — пишет «бот выключен» и ждёт
  сигнала (compose с `restart: unless-stopped` не перезапускает его по
  кругу); с токеном — `getMe` (проверка токена, имя бота в лог), сверка с
  `LINGUASTACK_TELEGRAM_BOT_USERNAME` (расхождение — предупреждение),
  `setMyCommands`, параллельно `Bot.Run` и `Dispatcher.Run`; SIGTERM —
  чистая остановка. **Step 2:** образ и compose собираются;
  `docker compose up -d` без токена — `bot` в статусе Up. **Step 3:**
  коммит `feat: процесс Telegram-бота и развёртывание`.

## Task 8: настройки Telegram в веб-приложении

**Files:** Create `frontend/src/api/telegram.ts`,
`frontend/src/pages/SettingsPage.vue`, `frontend/src/pages/SettingsPage.test.ts`;
Modify `frontend/src/router.ts`, `frontend/src/components/AppHeader.vue`
(ссылка «Настройки»), `frontend/src/api/types.ts`, `frontend/e2e/full-cycle.spec.ts`.

- [x] **Step 1:** Тесты: бот не настроен — «Telegram-бот не настроен на
  этом сервере», кнопок нет; не привязан — «Привязать Telegram» → ссылка
  «Открыть бота» и срок действия, опрос раз в 3 с до привязки; привязан —
  «@username, с 19 окт.», переключатели трёх видов уведомлений
  (PATCH), «Отвязать» через ConfirmButton. Сквозной сценарий: страница
  настроек на 1440 и 390 px. **Step 2–4.** **Step 5:** коммит
  `feat: настройки Telegram в веб-приложении`.

## Task 9: живая проверка, документация, ревью

- [x] **Step 1:** Локальный стенд api + worker + bot против поддельного
  сервера Telegram (скрипт в scratchpad): привязка по ссылке из API,
  перевод провайдером fake → сообщения о главах (склеенные) и о
  завершении, `/status`, «Приостановить» кнопкой, `/unlink`; логи без
  токена. Живой бот — только если владелец даст токен.
- [x] **Step 2:** README, дополнение (трассируемость №13), статус плана;
  полный прогон; ревью ветки; слияние в main.

## За рамками этого плана

- Постановка нового перевода из бота (ТЗ: «дополнительные команды
  могут…») — нужен выбор модели и языков, удобнее на сайте.
- Webhook вместо long polling (нужен публичный HTTPS-адрес).
- Восстановление доступа, профиль, админ-панель — этап 7.
