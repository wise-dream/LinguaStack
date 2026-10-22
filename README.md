# LinguaStack

Многопользовательская платформа для перевода и чтения книг с использованием
локальных (Ollama) и облачных языковых моделей.

Цикл работы: **импорт → структурирование → перевод → проверка → чтение → экспорт.**

Реализованы этапы 1–7 ТЗ: фундамент (пользователи, сессии, хранилище,
изоляция данных); импорт EPUB, PDF, DOCX, HTML, Markdown, TXT и
репозиториев документации (zip, mdBook, MkDocs); перевод через Ollama,
OpenAI-совместимые API и Anthropic Claude — с очередью, паузой, отменой,
восстановлением после сбоев, проверкой качества и ручной правкой;
экспорт в EPUB, Markdown, DOCX, HTML и TXT — только перевод или вместе с
оригиналом; веб-приложение: библиотека, импорт с оглавлением, настройка
перевода, монитор задач, читалка оригинала и перевода рядом, экспорт,
настройки; Telegram-бот: уведомления о главах, завершении и ошибках,
/status с паузой и продолжением; эксплуатация: ограничение частоты и квоты,
метрики Prometheus и журнал запросов, админ-панель, смена пароля и почты,
восстановление доступа, бэкап с проверкой восстановления в CI, публикация
образа в GHCR.

## Документы

| Документ | Назначение |
|---|---|
| [`PROJECT`](PROJECT) | Техническое задание (v1.1) |
| [`docs/spec/SPEC-ADDENDUM.md`](docs/spec/SPEC-ADDENDUM.md) | Дополнение: решения, которые ТЗ оставил открытыми |
| [`docs/superpowers/plans/`](docs/superpowers/plans/) | План реализации по задачам и отклонения от него |

## Устройство

Веб-приложение (`frontend/`, Vue 3 + TypeScript), два процесса из одного
Go-модуля (`backend/`) и PostgreSQL:

* **api** (`cmd/api`) — HTTP API, импорт книг, управление переводами; он же
  раздаёт собранное веб-приложение (`LINGUASTACK_WEB_ROOT`) — один origin,
  cookie сессии без CORS;
* **bot** (`cmd/bot`) — Telegram-бот: команды пользователей (long polling,
  публичный адрес не нужен) и доставка уведомлений, которые пишет worker.
  Один на развёртывание; без токена выключен;
* **worker** (`cmd/worker`) — фоновый перевод и сборка файлов экспорта.
  Задачи раздаются через аренды в PostgreSQL (`FOR UPDATE SKIP LOCKED`),
  worker-ов может быть несколько; упавший worker теряет аренду, и задачу
  подхватывает другой.

Оригиналы книг, ресурсы и файлы экспорта лежат в файловом хранилище
(`LINGUASTACK_STORAGE_ROOT`; в compose — общий том api и worker), всё
остальное — в БД.

### Форматы

| Импорт | Что извлекается |
|---|---|
| EPUB 2/3 | главы по spine, оглавление (nav/NCX), текст, ссылки, картинки |
| PDF с текстовым слоем | текст, порядок страниц, главы по закладкам или заголовкам, списки, код; колонтитулы убираются; сканы распознаются, но не читаются (OCR — после MVP) |
| DOCX | заголовки по стилям, абзацы, списки, таблицы, код, картинки, ссылки, сноски |
| HTML (файл или ZIP «страница полностью») | содержимое `<main>`/`<article>`, главы по заголовкам, картинки из `data:` и архива |
| Markdown, ZIP-репозиторий | заголовки, списки, таблицы, код, ссылки, порядок глав по SUMMARY.md/mkdocs.yml |
| TXT | кодировки, абзацы |

PDF разбирает PDFium, собранный в WebAssembly (go-pdfium + wazero): без CGO,
в песочнице без доступа к файловой системе. Первый PDF после запуска api
компилирует модуль (~3 с), дальше api держит ~300 МБ памяти.

Экспорт — EPUB 3 (проходит epubcheck), Markdown (с картинками — ZIP),
DOCX, HTML (один файл) и TXT; режим «только перевод» или «оригинал и
перевод». Непереведённые фрагменты остаются в оригинале, их число видно
в списке экспортов. Миграции (goose, `backend/migrations`) накатывают оба
процесса при старте.

## Требования

* Go — язык `1.26`, тулчейн `go1.27.2` (задан в `backend/go.mod`; при
  `GOTOOLCHAIN=auto` установленный go ≥ 1.21 скачает его сам)
* Docker и Docker Compose
* Ollama — для локальных моделей
* golangci-lint v2, собранный тем же тулчейном:
  `GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`

> На машине разработки `/usr/bin/go` — это gccgo 1.16, он не понимает
> современный `go.mod`. Нужный `go` лежит в `/usr/local/go/bin`, а линтер
> v2 — в `/root/go/bin`: `export PATH=/root/go/bin:/usr/local/go/bin:$PATH`.

## Запуск для разработки

```bash
# 1. База данных
docker compose up -d db
# Healthcheck проверяет целевую БД, а не только сервер: pg_isready отвечает
# «готов» ещё во время initdb.
until [ "$(docker inspect -f '{{.State.Health.Status}}' "$(docker compose ps -q db)")" = healthy ]; do sleep 1; done

# 2. Настройки: .env читают make run-api / run-worker
cp .env.example .env
sed -i "s|^LINGUASTACK_SECRET_KEY=.*|LINGUASTACK_SECRET_KEY=$(openssl rand -base64 32)|" .env

# 3. Процессы (в двух терминалах)
cd backend
make run-api
make run-worker

# 4. Веб-приложение с горячей перезагрузкой: http://localhost:5173
# (Vite проксирует /api на api по :8000)
cd frontend
npm ci
npm run dev
```

Postgres публикуется на **127.0.0.1:5434** — только на localhost и не на 5432/5433,
которые на машине разработки заняты другими проектами.

## Тесты

```bash
cd backend
make test        # без обращений к сети; нужна только БД из docker compose
make lint

# Тесты против живой модели (требуют запущенного Ollama с qwen3:8b):
make test-live
```

Живой тест Anthropic тратит деньги и включается отдельно:
`LINGUASTACK_LIVE_ANTHROPIC=1 ANTHROPIC_API_KEY=… go test -run LiveAnthropic ./internal/providers/`.

Веб-приложение:

```bash
cd frontend
npm run lint && npm run typecheck && npm test   # юнит- и компонентные тесты
npm run e2e      # сквозной сценарий в Chromium: нужны go в PATH и БД из docker compose
```

Сквозной сценарий (`frontend/e2e/run-stack.sh`) собирает приложение, поднимает
настоящие api и worker на чистой базе `linguastack_e2e` с тестовым провайдером
`fake` (`LINGUASTACK_E2E_FAKE_PROVIDER=true` — «перевод» вида «RU:» + текст;
в рабочей системе не включать) и проходит цикл на ширине 1440 и 390 px.
`SCREENSHOTS=1 npm run e2e` сохраняет снимки экранов в `test-results/screens/`.

CI (`.github/workflows/ci.yml`) запускает линтеры, тесты Go с `-race` против
PostgreSQL 17, тесты и сборку веб-приложения, сквозной сценарий и сборку
образа; живые тесты моделей в CI не выполняются.

## Развёртывание

Один образ (`Dockerfile` в корне, distroless, не от root) на все процессы;
api в нём раздаёт и веб-приложение — после запуска оно открывается по адресу
api.

```bash
cp .env.example .env
# В .env: задать LINGUASTACK_SECRET_KEY и LINGUASTACK_DB_PASSWORD,
# удалить строку LINGUASTACK_SESSION_COOKIE_SECURE=false.
docker compose up -d --build
docker compose ps          # api и db — healthy
```

Без сборки на сервере — готовый образ из GitHub Container Registry
(публикуется по тегу `v*`, workflow `release.yml`):

```bash
echo 'LINGUASTACK_IMAGE=ghcr.io/wise-dream/linguastack:1.0.0' >> .env
docker compose pull && docker compose up -d
```

Обновление — сменить тег в `.env` и повторить `pull` и `up -d`; миграции
накатываются при старте.

* api слушает **127.0.0.1:8000** (`LINGUASTACK_PUBLISH_PORT`). Наружу — только
  через обратный прокси с TLS (Caddy, nginx): cookie сессии по умолчанию
  передаётся лишь по HTTPS. Прокси должен не буферизовать
  `GET /api/translations/{id}/events` (Server-Sent Events): для nginx —
  `proxy_buffering off;` и `proxy_read_timeout` больше минуты.
* **IP клиента за прокси.** Лимиты регистраций и сброса пароля и журнал
  попыток входа считаются по IP. За обратным прокси и Docker все запросы
  приходят с адреса моста — задать `LINGUASTACK_TRUSTED_PROXIES` (для
  compose — `172.16.0.0/12`), а прокси должен передавать
  `X-Forwarded-For` (nginx: `proxy_set_header X-Forwarded-For
  $proxy_add_x_forwarded_for;`, Caddy — по умолчанию).
* Параллельность: процесс worker ведёт `LINGUASTACK_WORKER_CONCURRENCY`
  задач сразу (по умолчанию 4) — большая книга одного пользователя не держит
  очередь остальных; ещё больше — `docker compose up -d --scale worker=3`.
  Сколько задач одного пользователя выполняется одновременно, ограничивает
  `LINGUASTACK_MAX_TASKS_PER_USER`; Ollama сервера всегда получает одну
  задачу за раз — она одна на весь процессор.
* Остановка и обновление безопасны: по SIGTERM worker дописывает начатый
  батч (не дольше `LINGUASTACK_WORKER_SHUTDOWN_GRACE`, 25 с) и возвращает
  задачу в очередь без траты попытки; `stop_grace_period` worker-а — 40 с.
  Обновление: `git pull && docker compose up -d --build`.
* **Ollama.** Из контейнера `127.0.0.1` хоста не виден. Ollama на хосте должен
  слушать адрес моста Docker (`OLLAMA_HOST=0.0.0.0`, порт 11434 закрыт
  файрволом снаружи) — тогда подойдёт адрес по умолчанию
  `http://host.docker.internal:11434`; иначе задать
  `LINGUASTACK_DOCKER_OLLAMA_BASE_URL`.
* **Модель на CPU.** Без видеокарты модель класса 8B выдаёт единицы токенов в
  секунду, батч по умолчанию (6000 символов) идёт больше десяти минут. Это
  работает — тайм-аут Ollama считается по тишине в потоке, а не по всему
  ответу, — но прогресс виден редко, пауза и отмена срабатывают только на
  границе батча, а при перезапуске теряется больше. Удобнее
  `LINGUASTACK_MAX_BATCH_CHARS=1500`.
* Адрес Ollama общий для сервера, пользователи его не задают. По умолчанию
  Ollama сервера доступна только администратору (общий процессор):
  остальные её не видят и подключают свои сервисы с ключом API;
  `LINGUASTACK_OLLAMA_ADMIN_ONLY=false` открывает её всем. Адрес
  OpenAI-совместимого API задаёт пользователь, поэтому частные адреса
  (внутренняя сеть, metadata облака) запрещены — локальный vLLM или LM Studio
  разрешаются явно: `LINGUASTACK_ALLOW_PRIVATE_PROVIDER_URLS=true`.

### Telegram-бот

1. В Telegram у [@BotFather](https://t.me/BotFather): `/newbot` — получить
   токен и имя бота.
2. В `.env`: `LINGUASTACK_TELEGRAM_BOT_TOKEN=<токен>`,
   `LINGUASTACK_TELEGRAM_BOT_USERNAME=<имя без @>`,
   `LINGUASTACK_PUBLIC_URL=https://<адрес сайта>` (для ссылок в сообщениях).
3. `docker compose up -d` — сервис `bot` проверит токен (`docker compose
   logs bot`: «бот запущен») и задаст меню команд.

Пользователь привязывает Telegram в «Настройках» сайта: одноразовая ссылка
на бота действует 15 минут. Бот отвечает только в личном чате; сообщает,
что глава готова (не чаще раза в 5 минут на перевод — главы склеиваются),
что книга переведена и что перевод остановился из-за ошибки; `/status`
показывает ход переводов с кнопками «Приостановить» и «Продолжить»,
`/unlink` отвязывает. Токен бота — только в `.env`, в логи не попадает.

### Администрирование и наблюдение

* **Первый администратор** — после регистрации:
  `docker compose exec api /app/api -set-role admin@example.com=admin`.
  Раздел «Администрирование» в веб-приложении: обзор (переводы, задачи,
  экспорт, уведомления, расход внешних моделей, доступность Ollama),
  пользователи — отключение (сессии гаснут, переводы встают на паузу),
  роль, квоты, ссылка сброса пароля. Книг и текстов администратор не видит.
* **Метрики** — `GET /metrics` с `Authorization: Bearer` и
  `LINGUASTACK_METRICS_TOKEN`: запросы и длительность по шаблону маршрута,
  проекты, задачи, экспорты и уведомления по статусу, стоимость и токены
  провайдеров. Worker отдаёт счётчики батчей на
  `LINGUASTACK_WORKER_METRICS_ADDR` (`:9101`) с тем же токеном.
* **Журнал** — строка на запрос: метод, шаблон маршрута, статус,
  длительность, пользователь, `X-Request-ID` (входящий от прокси или новый,
  возвращается в ответе). Строка запроса, cookie и тела не пишутся.
* **Почта** для ссылок сброса пароля — `LINGUASTACK_SMTP_*` (STARTTLS
  обязателен по умолчанию) и `LINGUASTACK_PUBLIC_URL`. Без почты ссылка
  уходит в привязанный Telegram или её выдаёт администратор.
* **Закрытая регистрация** — `LINGUASTACK_REGISTRATION_ALLOWLIST`: адреса
  и домены (`anna@example.com, @team.example`), которым можно
  зарегистрироваться; пусто — регистрация открыта.
* **Квоты по умолчанию** — `LINGUASTACK_STORAGE_QUOTA_BYTES` (2 ГиБ
  оригиналов книг), `LINGUASTACK_MAX_PROJECTS_PER_USER`,
  `LINGUASTACK_MAX_TASKS_PER_USER`; свою квоту пользователю задаёт
  администратор.

### Резервное копирование

Состояние — это три вещи: **БД**, **том `storage`** (оригиналы книг,
ресурсы, файлы экспорта) и **`LINGUASTACK_SECRET_KEY`** (без него
сохранённые ключи API провайдеров не расшифровать — храните его отдельно от
бэкапов).

```bash
scripts/backup.sh /srv/backups          # db.dump + storage.tar.gz, последние 14 (KEEP)
scripts/restore.sh /srv/backups/linguastack-20261020T030000Z
```

`backup.sh` работает на запущенном стеке и проверяет, что дамп читается
(`pg_restore --list`). Ежедневно — cron:
`0 3 * * * cd /srv/linguastack && scripts/backup.sh /srv/backups`; копии
стоит увозить с сервера. `restore.sh` спрашивает подтверждение (`--yes` —
без вопроса), останавливает api, worker и bot, пересоздаёт базу из дампа,
заменяет содержимое тома и запускает стек.

`scripts/backup-drill.sh` — «учебная тревога» (задание `backup-drill` в
CI): отдельный проект compose `lsdrill` на портах 55434/58000 наполняется
через API, бэкапится, тома стираются, копия восстанавливается и
проверяется — вход, книга с картинкой, позиция чтения, перевод и экспорт.
Рабочий стек и чужие контейнеры она не трогает и убирает за собой.

После перезагрузки сервера стек поднимается сам: у сервисов
`restart: unless-stopped`, служба Docker должна быть включена
(`systemctl enable docker`).

## HTTP API

Все маршруты, кроме health и входа, требуют cookie сессии. Чужие объекты
отвечают 404, а не 403 — их существование не раскрывается.

| Маршрут | Назначение |
|---|---|
| `POST /api/auth/register`, `/login`, `/logout`, `GET /api/me` | Учётная запись и сессия |
| `POST /api/books` (multipart, поле `file`) | Импорт EPUB, PDF, DOCX, HTML, MD, TXT, zip-репозитория документации |
| `GET /api/books`, `GET/DELETE /api/books/{id}` | Книги |
| `GET/PUT /api/books/{id}/toc` | Оглавление; перестановка глав |
| `GET /api/books/{id}/chapters/{index}` | Блоки одной главы для читалки |
| `GET /api/books/{id}/assets/{path}` | Картинки книги (только изображения, CSP sandbox) |
| `GET /api/books/{id}/search?q=&translation_id=` | Поиск по книге и её переводу |
| `GET/PUT /api/books/{id}/position` | Позиция чтения: глава, фрагмент, перевод, режим |
| `GET /api/translation-options` | Языки, стили и точность для настройки перевода |
| `GET /api/providers`, `/api/providers/{kind}/models` | Доступные провайдеры и модели |
| `POST/GET /api/provider-configs`, `GET/PATCH/DELETE /api/provider-configs/{id}` | Свои конфигурации провайдеров (ключ API хранится зашифрованным и не возвращается) |
| `GET /api/provider-configs/{id}/models`, `/health` | Модели и проверка доступа |
| `POST /api/books/{id}/translations` | Запуск перевода: языки, модель, стиль, детерминизм, глоссарий |
| `GET /api/translations?book_id=`, `/api/translations/{id}` | Проекты перевода, прогресс и статистика провайдера (запросы, токены, стоимость) |
| `POST /api/translations/{id}/pause`, `/resume`, `/cancel` | Управление |
| `GET /api/translations/{id}/segments?offset=&limit=&chapter=&status=&severity=` | Оригинал и перевод по сегментам с замечаниями проверки; `status=failed`, `severity=warning` — найти проблемные |
| `GET /api/translations/{id}/events` | Прогресс в реальном времени (SSE) |
| `PATCH /api/segments/{id}`, `POST /api/segments/{id}/retry`, `GET /api/segments/{id}/revisions` | Ручная правка, повтор, история версий |
| `POST /api/books/{id}/exports` (`translation_id`, `format`, `mode`) | Экспорт перевода: `epub`, `markdown`, `docx`, `html`, `txt`; `translation` или `bilingual` |
| `GET /api/books/{id}/exports`, `GET/DELETE /api/exports/{id}` | История экспортов книги, состояние, удаление |
| `GET /api/exports/{id}/file` | Скачивание готового файла (вложение «Название (язык).ext») |
| `GET/PATCH/DELETE /api/telegram` | Привязка Telegram своего аккаунта: состояние, виды уведомлений, отвязка |
| `POST /api/telegram/link` | Одноразовая ссылка `t.me/<бот>?start=<код>` на 15 минут |
| `POST /api/me/password`, `PATCH /api/me` | Смена пароля (остальные сессии гаснут) и почты — с текущим паролем |
| `POST /api/auth/password-reset`, `/password-reset/confirm` | Ссылка сброса пароля на почту или в Telegram (ответ 202 на любой адрес); новый пароль по коду |
| `GET /api/admin/overview`, `GET /api/admin/users?q=` | Администратору: счётчики системы и Ollama сервера; пользователи с объёмом и квотами (без содержимого книг) |
| `PATCH /api/admin/users/{id}`, `POST /api/admin/users/{id}/password-reset` | Активность, роль, квоты (`null` — по умолчанию); ссылка сброса для передачи вручную |
| `GET /metrics` | Prometheus, только с `Authorization: Bearer <LINGUASTACK_METRICS_TOKEN>` |

Сверх лимита частоты — 429 с `Retry-After`. Админские маршруты для
остальных — 404.

Пример полного цикла с локальной моделью:

```bash
C="curl -s -b cj -c cj"
AUTH='{"email":"me@example.com","password":"correct horse battery"}'
$C -XPOST localhost:8000/api/auth/register -H 'content-type: application/json' -d "$AUTH"
$C -XPOST localhost:8000/api/auth/login -H 'content-type: application/json' -d "$AUTH"
BOOK=$($C -F file=@book.epub localhost:8000/api/books | jq -r .id)
CFG=$($C -XPOST localhost:8000/api/provider-configs -H 'content-type: application/json' \
   -d '{"provider_id":"ollama","name":"Локальный Ollama"}' | jq -r .id)
P=$($C -XPOST localhost:8000/api/books/$BOOK/translations -H 'content-type: application/json' \
   -d "{\"target_lang\":\"ru\",\"provider_config_id\":\"$CFG\",\"model_id\":\"qwen3:8b\",\"style\":\"literary\"}" | jq -r .id)
$C -N localhost:8000/api/translations/$P/events          # прогресс
$C "localhost:8000/api/translations/$P/segments?limit=20" # результат
E=$($C -XPOST localhost:8000/api/books/$BOOK/exports -H 'content-type: application/json' \
   -d "{\"translation_id\":\"$P\",\"format\":\"epub\",\"mode\":\"bilingual\"}" | jq -r .id)
$C localhost:8000/api/exports/$E                          # status: completed
$C -OJ localhost:8000/api/exports/$E/file                 # файл с именем из Content-Disposition
```

Облачный провайдер получает текст книги, поэтому конфигурацию с ним нужно
явно разрешить (`"allow_external": true`) — иначе перевод не запустится.
