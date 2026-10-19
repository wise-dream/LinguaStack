# LinguaStack — Этап 4: веб-приложение. План реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** веб-интерфейс этапа 4 ТЗ (§16): вход, библиотека, импорт с
просмотром оглавления, настройка перевода, монитор задач и читалка
оригинала и перевода — поверх готового API этапов 1–3.

**Architecture:** SPA на Vue 3 + TypeScript (каталог `frontend/`), собранная
Vite и раздаваемая тем же процессом api (`LINGUASTACK_WEB_ROOT`): один
origin — cookie-сессия без CORS. Недостающее читалке и монитору API
(глава книги, ресурсы, поиск, позиция чтения, статистика провайдера)
добавляется в Go-бэкенд первыми задачами. Сквозные тесты Playwright гоняют
настоящие api и worker с тестовым провайдером, включаемым только флагом.

**Tech Stack:** Vue 3.5.43, vue-router 5.3.1, Vite 8.3.2, TypeScript 6.0.3,
vue-tsc 3.3.12, Vitest 5.0.3 + @vue/test-utils 2.5.1 + jsdom 30.1.2,
Playwright 1.63.0, ESLint 10.12.0 + eslint-plugin-vue 10.11.1 +
typescript-eslint 8.71.1; шрифты @fontsource-variable/literata 5.3.0 и
@fontsource-variable/golos-text 5.3.0. Бэкенд — как в плане этапов 1–3.

**Spec:** `PROJECT` §8 (веб-интерфейс), §6.4 (предупреждение о передаче
наружу), §5.2 (языки по возможностям модели), §16 (этап 4);
`docs/spec/SPEC-ADDENDUM.md` §13 (прогресс без перезагрузки).

## Global Constraints

- Версии пакетов — точные (без `^`/`~`), из Tech Stack; каждая опубликована
  не позже чем за две недели до начала работ (2026-10-05).
  TypeScript 7 не годится: typescript-eslint 8.71 требует `<6.1.0`.
- Node 22 (`node:22.22.2-bookworm-slim` в Dockerfile).
- Зависимости времени выполнения — только `vue`, `vue-router` и два пакета
  шрифтов. Без CSS-фреймворков и UI-библиотек.
- Браузер не ходит ни на какие внешние адреса: шрифты свои, CSP `'self'`.
- Интерфейс по-русски, предложения с заглавной буквы, без капса; кнопка
  называет действие («Загрузить книгу», «Начать перевод»), сообщение об
  успехе — тем же словом.
- Текст книги и перевода попадает в DOM только как текст или через
  `renderInline` (задача 11), который сначала экранирует. `v-html` с
  данными пользователя — нигде больше.
- Названия языков — через `Intl.DisplayNames(['ru'], {type: 'language'})`
  по кодам ISO 639-1 из API.
- Доступность: видимый фокус, подписи у всех полей, `prefers-reduced-motion`,
  ширина от 360 px.
- Бэкенд-правила `CLAUDE.md` действуют: TDD, миграции goose, 404 вместо 403,
  новый маршрут — в `httpapi.Routes()` и матрицу изоляции.

### Дизайн-токены (frontend/src/styles/tokens.css)

| Токен | Значение | Роль |
|---|---|---|
| `--paper` | `#F4F6F3` | фон — холодная бумага |
| `--ink-original` | `#1B2440` | текст оригинала и интерфейса |
| `--ink-translation` | `#14463F` | текст перевода, «готово» |
| `--proof` | `#B3261E` | ошибки, корректорские пометки |
| `--highlight` | `#F3D35C` | предупреждения, находки поиска |
| `--rule` | `#C8CEC6` | линейки и границы |
| `--muted` | `#5E6A66` | второстепенный текст |

Шрифты: Literata — текст книги и заголовки книг; Golos Text — интерфейс.
Текст книги 1.125rem / 1.65, строка не длиннее ~68 знаков в колонке.
Скругления: 6 px у полей и кнопок, 0 у колонок читалки. Единственный
«громкий» элемент — читалка: две колонки как в билингвальном издании,
справа поля с пометками. Библиотека и монитор — списки-ведомости, не
карточки.

## Review Focus

1. Текст книги с HTML, `<script>` или ссылкой `javascript:` (EPUB, Markdown)
   показывается как текст; ссылки — только `http(s):`, `mailto:` и
   относительные. Тест — задача 11.
2. Сессия истекла, пока страница открыта: любой 401 ведёт на
   `/login?next=<текущий путь>`, после входа — обратно. Тест — задача 6.
3. В главе сегментов больше страницы API (1000): читалка дочитывает все
   страницы. Тест — задача 11.
4. Поток SSE рвётся (таймаут прокси, перезапуск api) посреди многочасового
   перевода: монитор восстанавливается без перезагрузки — после трёх ошибок
   подряд переходит на опрос раз в 10 с. Тест — задача 10.
5. Сервер отклонил загрузку (413, 422 с причиной, зип-бомба): причина
   показана, форма снова доступна. Тест — задача 7.

---

## Бэкенд для интерфейса

### Task 1: Параметры перевода и статистика провайдера

**Files:** Create `backend/migrations/00006_project_stats.sql`; Modify
`internal/translation/languages.go`, `internal/translation/prompt.go`,
`internal/translation/service.go`, `internal/queue/worker.go`,
`internal/httpapi/translations.go`, `internal/httpapi/server.go`,
`internal/httpapi/segments.go`; Test `internal/httpapi/translations_test.go`,
`internal/translation/service_test.go`, `internal/queue/worker_test.go`

**Interfaces:**
- Produces: `GET /api/translation-options` →
  `{"languages":[{"code":"en","name":"English"}…] (по code), "styles":["literary","neutral","simplified","technical"], "determinism":["strict","balanced","creative"]}`;
  `translation.Languages() []Language` (`Language{Code, Name string}`),
  `translation.Styles() []string`; поля проекта и события SSE
  `input_tokens`, `output_tokens` (int64), `provider_calls` (int);
  `BatchOutcome.InputTokens/OutputTokens int64`;
  `queue.Usage{CostUSD float64; InputTokens, OutputTokens int64; Calls int}`,
  `queue.RefreshCounters(ctx, db, projectID uuid.UUID, add Usage) error`;
  `GET /api/translations?book_id=` (не UUID — 422) и поле проекта
  `book_title` — монитору не нужен запрос на каждую строку.

- [ ] **Step 1:** Тесты: `TestTranslationOptions` (en и ru в списке, стили и
  детерминизм ровно эти); `TestBatchUsageSummed` (Fake с токенами: сумма по
  всем вызовам, включая повторы); `TestWorkerAccumulatesUsage` (2 батча →
  `provider_calls` = 2, токены сложены); `TestTranslationResponseHasUsage`;
  `TestTranslationsFilterByBook` (чужая книга — пустой список, не 404);
  `TestTranslationHasBookTitle`.
- [ ] **Step 2:** FAIL. **Step 3:** миграция (`ALTER TABLE translation_projects ADD input_tokens bigint NOT NULL DEFAULT 0, ADD output_tokens bigint NOT NULL DEFAULT 0, ADD provider_calls integer NOT NULL DEFAULT 0`), реализация. Fake получает поле `TokensPerCall int` (вход и выход).
- [ ] **Step 4:** PASS, `make lint`. **Step 5:** коммит `feat: параметры перевода и статистика провайдера для интерфейса`.

### Task 2: Глава книги, ресурсы и сегменты главы

**Files:** Modify `internal/httpapi/books.go`, `internal/httpapi/translations.go`,
`internal/httpapi/server.go`, `internal/storage/files.go`; Test
`internal/httpapi/books_test.go`, `internal/storage/files_test.go`,
`internal/httpapi/isolation_matrix_test.go`

**Interfaces:**
- Produces: `GET /api/books/{id}/chapters/{index}` →
  `{"index":3,"title":"…","blocks":[cdm.Block…]}` — только собственные блоки
  главы (не вложенных), поиск index по всему дереву, нет главы — 404;
  `GET /api/books/{id}/assets/{path...}` — ресурс книги;
  `GET /api/translations/{id}/segments?chapter=N` (N ≥ 0, иначе 422);
  `storage.FileStore.OpenAsset(userID, bookID uuid.UUID, member string) (*os.File, error)`.
  Клиент строит адрес картинки как `/api/books/{id}/assets/` + `block.meta.src`.

- [ ] **Step 1:** Тесты: `TestChapterBlocks` (вложенная глава находится по
  index; блоки родителя не включают блоки детей); `TestChapterMissing404`;
  `TestEPUBImageServedByMetaSrc` (фикстура EPUB с картинкой: `meta.src`
  блока image отдаётся по маршруту assets, `Content-Type: image/png`);
  `TestSVGAssetIsSandboxed` (`Content-Security-Policy` содержит `sandbox` и
  `default-src 'none'`, `X-Content-Type-Options: nosniff`);
  `TestAssetPathTraversal404` (`../`, абсолютный путь, симлинк, расширение
  не из списка — 404); `TestSegmentsFilterByChapter`; новые маршруты — в
  матрице изоляции.
- [ ] **Step 2:** FAIL. **Step 3:** `OpenAsset` — через `os.Root`,
  `ValidateMemberName` и `rejectSymlinks`; типы:
  `.png .jpg .jpeg .gif .webp .avif .svg`, остальное 404; заголовки
  `Cache-Control: private, max-age=86400`, для всех ресурсов
  `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`.
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: глава книги, ресурсы и сегменты главы для читалки`.

### Task 3: Поиск по книге и позиция чтения

**Files:** Create `backend/migrations/00007_reading_positions.sql`,
`internal/db/models/reading.go`, `internal/httpapi/reader.go`; Modify
`internal/httpapi/server.go`; Test `internal/httpapi/reader_test.go`,
`internal/httpapi/isolation_matrix_test.go`

**Interfaces:**
- Produces:
  `GET /api/books/{id}/search?q=&translation_id=` →
  `{"results":[{"chapter_index":int,"node_id":"…","in":"original"|"translation","snippet":"…"}],"truncated":bool}`;
  `GET /api/books/{id}/position` → `{"chapter_index":int,"node_id":"…","translation_id":"…"|null,"mode":"parallel"|"original"|"translation"}` или `null`;
  `PUT /api/books/{id}/position` с тем же телом → 204.
- [ ] **Step 1:** Тесты: `TestSearchOriginalCaseInsensitive` («АЛИСА» находит
  «Алиса»); `TestSearchTranslationEscapesLike` (`%` и `_` в запросе ищутся
  буквально); `TestSearchQueryLength` (1 символ и 201 — 422);
  `TestSearchLimit50SetsTruncated`; `TestSearchForeignTranslation404`
  (чужой или от другой книги проект — 404); `TestPositionRoundTrip`;
  `TestPositionUnknownChapter422`; `TestPositionDeletedWithBook`;
  `TestPositionTranslationDeletedBecomesNull`; матрица изоляции.
- [ ] **Step 2:** FAIL. **Step 3:** таблица `reading_positions(user_id uuid
  REFERENCES users ON DELETE CASCADE, book_id uuid REFERENCES books ON DELETE
  CASCADE, translation_id uuid NULL REFERENCES translation_projects ON DELETE
  SET NULL, chapter_index int NOT NULL, node_id text NOT NULL DEFAULT '',
  mode text NOT NULL DEFAULT 'parallel', updated_at timestamptz NOT NULL
  DEFAULT now(), PRIMARY KEY (user_id, book_id))`; upsert `ON CONFLICT`.
  Поиск: `q` обрезается, 2–200 символов; оригинал — по блокам CDM
  (`strings.ToLower` с обеих сторон); перевод — `ILIKE ? ESCAPE '\'`;
  сниппет — ±60 символов вокруг первого совпадения; не больше 50 находок,
  порядок — глава, затем позиция.
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: поиск по книге и позиция чтения`.

### Task 4: Раздача SPA и тестовый провайдер для сквозных тестов

**Files:** Create `internal/httpapi/web.go`; Modify `internal/config/config.go`,
`internal/httpapi/server.go`, `internal/httpapi/providers.go`,
`internal/providers/build.go`, `internal/providers/fake.go`,
`cmd/api/main.go`, `cmd/worker/main.go`, `backend/Dockerfile`; Test
`internal/httpapi/web_test.go`, `internal/httpapi/providers_test.go`

**Interfaces:**
- Produces: `Settings.WebRoot` (`LINGUASTACK_WEB_ROOT`, пусто — выключено),
  `Settings.E2EFakeProvider` (`LINGUASTACK_E2E_FAKE_PROVIDER`, false),
  `Settings.E2EFakeDelay` (`LINGUASTACK_E2E_FAKE_DELAY`, 0);
  вид провайдера `"fake"` виден в `/api/providers` и принимается при
  создании конфигурации только при включённом флаге;
  `providers.BuildOptions.Fake bool`, `.FakeDelay time.Duration`;
  `Fake.Delay time.Duration` (ждёт, уважая ctx).
- [ ] **Step 1:** Тесты: `TestSPAFallbackServesIndex` (`/books/123` → index.html,
  `Cache-Control: no-cache`); `TestSPAAssetsImmutable` (`/assets/x.js` —
  `max-age=31536000, immutable`); `TestUnknownAPIPathIsJSON404`
  (`/api/nope` — JSON, не index.html); `TestSPASecurityHeaders` (CSP
  `default-src 'self'`, `frame-ancestors 'none'`, nosniff);
  `TestSPANoDirectoryListing`; `TestFakeProviderHiddenByDefault` и
  `TestFakeProviderAvailableWithFlag`.
- [ ] **Step 2:** FAIL. **Step 3:** CSP для страниц:
  `default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'`;
  файлы — через `os.Root`. Старт с включённым тестовым провайдером пишет
  `slog.Warn`. Dockerfile: этап `node:22.22.2-bookworm-slim` собирает
  `frontend/` (`npm ci && npm run build`), итоговый образ получает
  `/app/web` и `LINGUASTACK_WEB_ROOT=/app/web`; контекст сборки compose —
  корень репозитория.
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: раздача веб-приложения и тестовый провайдер`.

## Веб-приложение

### Task 5: Каркас frontend, токены, оболочка

**Files:** Create `frontend/{package.json,package-lock.json,index.html,vite.config.ts,tsconfig.json,tsconfig.node.json,eslint.config.js,playwright.config.ts}`,
`frontend/src/{main.ts,App.vue,router.ts,env.d.ts}`,
`frontend/src/styles/{tokens.css,base.css}`,
`frontend/src/components/AppHeader.vue`, `frontend/src/App.test.ts`

**Interfaces:**
- Produces: скрипты `npm run dev | build | typecheck | lint | test | e2e`;
  Vite проксирует `/api` на `http://localhost:8000`; маршруты (страницы —
  заглушки до своих задач): `/login`, `/register`, `/`, `/import`,
  `/books/:id`, `/books/:id/translate`, `/translations`, `/translations/:id`,
  `/read/:bookId` (query `t`, `ch`), `/settings/providers`.
- [ ] **Step 1:** тест `App.test.ts`: оболочка показывает навигацию
  «Библиотека», «Переводы», «Модели»; ссылка текущего раздела помечена
  `aria-current="page"`.
- [ ] **Step 2:** FAIL. **Step 3:** каркас, токены из Global Constraints,
  `@fontsource-variable/*` импортом в `main.ts` (только latin и cyrillic).
- [ ] **Step 4:** `npm run lint && npm run typecheck && npm test && npm run build`. **Step 5:** коммит `feat: каркас веб-приложения`.

### Task 6: Клиент API, сессия, вход и регистрация

**Files:** Create `frontend/src/api/{client.ts,types.ts,auth.ts,books.ts,translations.ts,providers.ts,reader.ts}`,
`frontend/src/session.ts`, `frontend/src/pages/{LoginPage.vue,RegisterPage.vue}`;
Modify `frontend/src/router.ts`; Test `frontend/src/api/client.test.ts`,
`frontend/src/session.test.ts`, `frontend/src/pages/LoginPage.test.ts`

**Interfaces:**
- Produces: `request<T>(method: string, path: string, body?: unknown | FormData): Promise<T>`
  (`credentials: 'same-origin'`; ошибка — `ApiError {status: number; message: string}`,
  `message` из `{"error": …}`, иначе «Сервер недоступен (HTTP 502)», сеть —
  «Нет связи с сервером»; 204 → `undefined`);
  `onUnauthorized(handler: () => void)` — вызывается на любой 401, кроме
  `/api/auth/*`; типы из Go-ответов (`Book`, `TocEntry`, `Chapter`, `Block`,
  `Project`, `Segment`, `ProviderKind`, `ProviderConfig`, `ModelCaps`,
  `TranslationOptions`, `Position`, `SearchResult`); функции модулей —
  по одной на маршрут API с теми же именами, что у обработчиков Go
  (`listBooks`, `uploadBook`, `getTOC`, `putTOC`, `createTranslation`, …);
  `useSession(): {me: Ref<User|null>, load(), login(email, pw), register(email, pw), logout()}`.
- [ ] **Step 1:** тесты: разбор ошибок трёх видов; 401 вызывает обработчик,
  а 401 входа — нет; страж маршрутов без сессии ведёт на
  `/login?next=/books/1` (Review Focus №2); после входа — на `next`, но не
  на внешний адрес (`next=//evil.com` → `/`); форма входа показывает текст
  ошибки сервера; регистрация входит сразу после создания (API регистрации
  сессию не выдаёт — клиент вызывает login).
- [ ] **Step 2–4:** FAIL → реализация → PASS + lint + typecheck. **Step 5:** коммит `feat: вход, регистрация и клиент API`.

### Task 7: Библиотека и импорт

**Files:** Create `frontend/src/pages/{LibraryPage.vue,ImportPage.vue}`,
`frontend/src/components/{LangPair.vue,StatusMark.vue,ConfirmButton.vue,TocTree.vue}`,
`frontend/src/lib/format.ts`; Test `*.test.ts` рядом

**Interfaces:**
- Consumes: `listBooks`, `listTranslations`, `deleteBook`, `uploadBook(file, onProgress: (fraction: number) => void)` (XMLHttpRequest — ради прогресса), `getTOC`, `putTOC(order: number[])`.
- Produces: `ConfirmButton` (первое нажатие меняет подпись на «Точно удалить?», второе в течение 4 с — подтверждает; Esc отменяет) — используется в задачах 8 и 10; `TocTree` (дерево глав, режим перестановки верхнего уровня кнопками «Выше»/«Ниже»); `formatDate`, `formatDuration(ms)`, `languageName(code)`.
- [ ] **Step 1:** тесты: строка книги — название, автор, формат, дата, пары
  языков переводов («английский → русский») и состояние; пустая библиотека
  приглашает загрузить книгу; удаление только после подтверждения; импорт
  показывает причину 413/422 и снова доступен (Review Focus №5); замечания
  импорта (`issues`) видны с уровнем; перестановка отправляет перестановку
  индексов `[1,0,2]`.
- [ ] **Step 2–4:** FAIL → реализация → PASS. **Step 5:** коммит `feat: библиотека и импорт книги`.

### Task 8: Модели и провайдеры

**Files:** Create `frontend/src/pages/ProvidersPage.vue`; Test рядом

**Interfaces:**
- Consumes: `listKinds`, `listProviderConfigs`, `createProviderConfig`, `patchProviderConfig`, `deleteProviderConfig`, `configHealth`, `configModels`.
- [ ] **Step 1:** тесты: поле адреса API — только у вида с `user_base_url`;
  ключ — поле пароля, после сохранения виден только `…last4`; для вида с
  `sends_data_externally` чекбокс «Разрешить отправку текста книг этому
  сервису» с пояснением (ТЗ §6.4); 503 (нет ключа шифрования) и 409
  (конфигурация используется) — понятным текстом; «Проверить» показывает
  ответ health.
- [ ] **Step 2–4:** FAIL → реализация → PASS. **Step 5:** коммит `feat: настройка моделей и провайдеров`.

### Task 9: Книга и настройка перевода

**Files:** Create `frontend/src/pages/{BookPage.vue,TranslationSetupPage.vue}`,
`frontend/src/components/GlossaryEditor.vue`; Test рядом

**Interfaces:**
- Consumes: `getBook`, `getTOC`, `listTranslations` (по `book_id`), `getTranslationOptions`, `listProviderConfigs`, `configModels`, `createTranslation`.
- [ ] **Step 1:** тесты: страница книги — метаданные, оглавление и переводы
  книги с действиями «Читать», «Продолжить» (для paused и failed —
  `resumeTranslation`, ТЗ §8.2) и «Новый перевод»; язык исходника по
  умолчанию — язык книги, иначе
  «Определить автоматически» (`auto`); если у модели есть
  `supported_languages` — в списках только они (ТЗ §5.2); конфигурация,
  отправляющая текст наружу, показывает предупреждение «Текст книги будет
  отправлен в …» до запуска, а без разрешения — объясняет, где его дать,
  и кнопка недоступна; глоссарий: пустые строки отбрасываются, пробелы
  обрезаются; ответ 400/409/422 показан у формы; после запуска — переход
  на `/translations/:id`.
- [ ] **Step 2–4:** FAIL → реализация → PASS. **Step 5:** коммит `feat: страница книги и настройка перевода`.

### Task 10: Монитор переводов

**Files:** Create `frontend/src/lib/progress.ts`,
`frontend/src/pages/{TranslationsPage.vue,TranslationDetailPage.vue}`,
`frontend/src/components/ProgressBar.vue`; Test рядом

**Interfaces:**
- Produces: `useProgress(projectId: Ref<string>): {event: Ref<ProgressEvent|null>, mode: Ref<'stream'|'polling'|'done'>}` — EventSource
  на `/api/translations/{id}/events`; терминальный статус закрывает поток;
  после трёх `error` подряд — опрос `getTranslation` каждые 10 с.
- [ ] **Step 1:** тесты (EventSource подменяется фейком): событие обновляет
  прогресс; терминальный статус — поток закрыт, `mode='done'`; три ошибки
  подряд — опрос (Review Focus №4); монитор делит переводы на «Идут»,
  «Ждут» (queued, paused), «Завершены» (completed, completed_with_warnings,
  failed, cancelled); строка — книга, пара языков, модель, «497 из 873»,
  ошибки, предупреждения, время работы, стоимость и токены; детальная
  страница: «Приостановить» / «Продолжить» / «Отменить перевод» (через
  `ConfirmButton`), сегменты с фильтрами «Все», «С ошибками»,
  «С предупреждениями» (`status=failed`, `severity=warning`), постранично,
  «Перевести заново» у сегмента; ответ 409 показан текстом сервера.
- [ ] **Step 2–4:** FAIL → реализация → PASS. **Step 5:** коммит `feat: монитор переводов`.

### Task 11: Читалка

**Files:** Create `frontend/src/lib/inline.ts`, `frontend/src/lib/position.ts`,
`frontend/src/pages/ReaderPage.vue`,
`frontend/src/reader/{ReaderColumns.vue,SegmentRow.vue,ReaderSearch.vue,ProofMark.vue}`;
Test рядом

**Interfaces:**
- Consumes: `getTOC`, `listTranslations` (по `book_id`), `getChapter`, `listSegments({chapter})`, `patchSegment`, `retrySegment`, `searchBook`, `getPosition`, `putPosition`, адрес ресурса из задачи 2.
- Produces: `renderInline(text: string): string` — экранирует `& < > " '`,
  затем `**жирный**`, `*курсив*`/`_курсив_`, `` `код` ``, `[текст](url)`
  (url — только `http:`, `https:`, `mailto:` или относительный; иначе —
  текстом; `rel="noopener noreferrer"`, `target="_blank"` для внешних);
  `fetchChapterSegments(projectId, chapter): Promise<Segment[]>` —
  дочитывает страницы по 1000; `usePositionSaver(save, delayMs = 1500)` —
  сохраняет последний видимый сегмент с задержкой и при уходе со страницы.
- [ ] **Step 1:** тесты: `renderInline` — `<script>`, `<img onerror>`,
  `[x](javascript:alert(1))`, `"` в url — как текст (Review Focus №1);
  курсив и код отрисованы; `fetchChapterSegments` при 2500 сегментах делает
  3 запроса (Review Focus №3); выравнивание по `node_id`: код и картинка —
  во всю ширину, у непереведённого сегмента — пометка «Ещё не переведено»;
  режимы «Рядом», «Оригинал», «Перевод» (запоминаются в позиции);
  правка: Ctrl+Enter сохраняет, Esc отменяет, ошибка сервера показана;
  «Перевести заново» и 409; поиск переходит к главе и подсвечивает
  сегмент; позиция восстанавливается при открытии и сохраняется не чаще
  раза в 1,5 с; пометки на полях — для `warning`/`error` с текстом
  `findings`; переключатель языковой версии — переводы этой книги
  (ТЗ §8.3), выбранный хранится в позиции (`translation_id`) и в query `t`.
- [ ] **Step 2–4:** FAIL → реализация → PASS. **Step 5:** коммит `feat: читалка оригинала и перевода`.

### Task 12: Сквозной сценарий, CI, документация, визуальная вычитка

**Files:** Create `frontend/e2e/full-cycle.spec.ts`, `frontend/e2e/run-stack.sh`;
Modify `.github/workflows/ci.yml`, `docker-compose.yml`, `README.md`,
`CLAUDE.md`, этот план (статус и отклонения)

- [ ] **Step 1:** `run-stack.sh` собирает api и worker, поднимает их на
  отдельной БД `linguastack_e2e` с `LINGUASTACK_E2E_FAKE_PROVIDER=true`,
  `LINGUASTACK_E2E_FAKE_DELAY=300ms`, `LINGUASTACK_WEB_ROOT=frontend/dist`;
  Playwright `webServer` использует его.
- [ ] **Step 2:** сценарий: регистрация → вход → конфигурация «fake» →
  загрузка Markdown-книги с кодом и ссылкой → оглавление → запуск перевода →
  монитор показывает живой прогресс до «Готово» → читалка «Рядом»
  показывает перевод, код не тронут → правка сегмента сохраняется →
  поиск находит слово перевода → перезагрузка возвращает к той же главе;
  тот же сценарий на ширине 390 px (раскладка столбцом).
- [ ] **Step 3:** CI: задача `frontend` (`npm ci`, lint, typecheck, test,
  build) и `e2e` (PostgreSQL, Go, браузер Playwright, `npm run e2e`);
  сборка образа с контекстом — корень репозитория.
- [ ] **Step 4:** визуальная вычитка: скриншоты Playwright каждой страницы
  на 1440 и 390 px, правка по замечаниям (контраст, переносы, фокус).
- [ ] **Step 5:** README (разработка фронтенда, `npm run dev` с прокси,
  развёртывание одним образом), CLAUDE.md (команды фронтенда), план —
  статус. Коммит `feat: сквозной сценарий веб-приложения и CI`.

---

## За рамками этого плана

| Что | Где по ТЗ |
|---|---|
| Глоссарии как отдельные сущности (сейчас — глоссарий проекта) | §8.1 п.9 |
| История экспортов, экспорт | §8.1 п.10, этап 5 |
| Настройки аккаунта и Telegram | §8.1 п.11, этап 6 |
| Административная панель | §8.1 п.12 |
