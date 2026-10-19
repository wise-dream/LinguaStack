# LinguaStack — Этап 5: остальные форматы. План реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** импорт PDF, DOCX и HTML и экспорт переводов в EPUB, Markdown,
DOCX, HTML и TXT — только перевод или двуязычный документ (ТЗ §3, §16
этап 5, критерии приёмки §15 №3 и №12).

**Architecture:** три новых импортёра приводят формат к тому же CDM, что и
этапы 1–3: HTML и DOCX — своим разбором поверх `golang.org/x/net/html` и
`encoding/xml`, PDF — через PDFium, собранный в WebAssembly (go-pdfium +
wazero, без CGO), с эвристиками вёрстки в своём коде. Экспорт — пакет
`exporters` (по адаптеру на формат, ядро о форматах не знает) и задача
экспорта `ExportJob`, которую выполняет worker; файл скачивается через
авторизованный эндпоинт.

**Tech Stack:** `github.com/klippa-app/go-pdfium` v1.21.1 (WebAssembly-бэкенд,
вместе с ним `github.com/tetratelabs/wazero` v1.12.0); остальное — уже в
проекте: goldmark v1.8.6 (встроенная разметка при экспорте),
`golang.org/x/net/html`, `encoding/xml`, `archive/zip`, `image/*` stdlib.
Фикстуры PDF печатает headless Chrome (`google-chrome --print-to-pdf`).

**Spec:** `PROJECT` §3.1–3.2 (форматы), §4.1–4.2 (CDM, адаптеры), §11
(`ExportJob`), §12 (`POST /api/books/{id}/exports`, `GET /api/exports/{id}`),
§13 (безопасность загрузок), §14 (эталонные документы); дополнение
§0.3, §10, §11, §15–16 (добавляются задачей 0).

## Global Constraints

- Версии модулей — точные и опубликованные не позже чем за две недели до
  начала работ (2026-10-05): go-pdfium v1.21.1 вышел 2026-09-29; v1.21.2 и
  v1.21.3 моложе — не брать.
- Без CGO и внешних программ во время работы: образ остаётся
  distroless/static-подобным, импорт PDF не запускает процессов.
- Модуль PDFium не видит файловую систему хоста: `FSConfig` без
  монтирований (по умолчанию go-pdfium монтирует `/`!), предел памяти,
  тайм-аут с уничтожением экземпляра.
- Ошибка из `Importer.Load` — только отказ принять файл (лимиты,
  небезопасные пути); испорченный, зашифрованный или сканированный
  документ — замечание (`Issue`), как у EPUB.
- Встроенная разметка CDM — Markdown; новые импортёры пишут текст через
  `escapeMarkdown` и маркеры `*`/`**`/`` ` ``, как HTML-разбор EPUB.
- В экспорт не попадает исполняемое: сырой HTML не выводится как HTML,
  ссылки — только `http(s):`/`mailto:`, остальные — текстом; в XML
  (EPUB, DOCX) — только допустимые символы XML 1.0.
- Файлы экспорта: `storage/{user}/{book}/exports/{export_uuid}{ext}`
  (дополнение §11); имя для скачивания — только в `Content-Disposition`.
- Правила `CLAUDE.md`: TDD, миграции goose, 404 вместо 403, новый маршрут —
  в `httpapi.Routes()` и матрицу изоляции; комментарии и сообщения
  по-русски; коммит — после `make lint && go test ./...`.

## Review Focus

1. Враждебный PDF: тысячи страниц, бесконечный цикл разбора, огромное
   потребление памяти, попытка прочитать файл хоста. Ожидается отказ или
   замечание за ограниченное время, процесс api жив. Тест — задача 3.
2. Экспорт незаконченного, упавшего или отменённого перевода: файл всё
   равно собирается, непереведённые блоки — в оригинале, их число видно
   пользователю. Тест — задача 5.
3. Текст книги с `<script>`, `javascript:`, `&`, `<` и управляющими
   символами (в PDF это `\x02`, `\x0c`) в экспорте: HTML не исполняет,
   EPUB и DOCX остаются корректным XML. Тест — задачи 5, 6, 7.
4. Название книги с кавычками, переводами строк и не-ASCII в имени файла
   экспорта: заголовок `Content-Disposition` не ломается и не
   внедряется. Тест — задача 8.
5. Зашифрованный PDF, DOCX без `styles.xml`/`numbering.xml`, HTML в
   windows-1251: импорт не падает, пользователь видит замечание.
   Тесты — задачи 1, 2, 4.

---

## Задача 0: решения в дополнении к ТЗ

**Files:** Modify `docs/spec/SPEC-ADDENDUM.md` (§0.3, §10, §11, новые §15 и
§16, таблица трассируемости), `CLAUDE.md` (команды фикстур PDF).

Решения, которые фиксируются (дальше задачи на них ссылаются):

- **PDF через PDFium/WebAssembly.** Сравнение на реальных файлах этой
  машины: `ledongthuc/pdf` в PDF из Word отдаёт нулевые ширины глифов и
  склеивает текст без пробелов; poppler — внешняя программа на C в
  контейнере; PDFium (движок Chrome) в песочнице wasm даёт поток символов
  со своими пробелами и концами строк, шрифтом, размером, закладками и
  метаданными. Цена: ~3 с компиляции модуля при первом PDF, ~300 МБ
  памяти, +15 МБ к бинарю. Режим `Rects` go-pdfium дублирует символы на
  стыках прямоугольников («Chapter Tw|Two») — берётся только режим
  `Chars`.
- **Эвристики вёрстки PDF** (§15.1): строки — по `\r\n` PDFium; абзац
  рвётся при вертикальном зазоре > 1,4 медианного межстрочного шага,
  смене размера шрифта, отступе первой строки > 1,5 кегля, короткой
  предыдущей строке с концевой пунктуацией; `\x02` в конце строки —
  мягкий перенос (склейка без пробела); основной кегль — мода по числу
  символов; заголовок — строка-абзац с кеглем ≥ 1,2 основного или целиком
  жирная, ≤ 80 символов, без концевой точки; уровни — по убыванию кегля;
  колонтитулы — строки в верхних/нижних 8 % страницы, повторяющиеся
  (цифры → `#`) на ≥ половине страниц (минимум 3), и голые номера страниц;
  моноширинные строки подряд — блок кода с отступами; `•`, `–`, `-`,
  `1.`, `1)` в начале абзаца — пункт списка.
- **Главы PDF**: есть закладки — главы по ним (глубина = вложенность,
  начало — первая строка страницы закладки, совпадающая с её названием,
  иначе верх страницы); нет — по заголовкам, как в Markdown. Номер
  страницы — `meta.page` каждого блока.
- **Сканы**: страница без текста, но с изображениями. Все такие —
  `scanned_pdf` (error: «текстового слоя нет, распознавание (OCR) пока не
  поддерживается»); часть — `scanned_pages` (warning, номера страниц).
- **DOCX** (§15.2): уровни заголовков — из цепочки стилей (`heading N`,
  `Title`, `outlineLvl`), списки — `numPr` + `numbering.xml`
  (`numFmt=bullet` — маркированный), код — стили `code`, `source code`,
  `html preformatted`, `plain text` и моноширинные шрифты, цитаты —
  `quote`, `intense quote`, `block text`; оглавление Word (`toc N`, SDT
  «Table of Contents») пропускается; сноски — «[n]» в тексте и абзацы
  «[n] …» в конце главы; `mc:Fallback` пропускается (дубли текстовых
  полей); EMF/WMF/TIFF не показываются браузером — замечание
  `unsupported_image`.
- **HTML** (§15.3): кодировка — `charset.DetermineEncoding`; содержимое —
  `<main>`, иначе единственный `<article>`, иначе `<body>`; `<nav>`
  пропускается; главы — по заголовкам; картинки `data:` становятся
  ресурсами книги, внешние не скачиваются (замечание
  `remote_images_skipped`), относительные — из ZIP. ZIP без Markdown, но с
  HTML («Сохранить страницу полностью») — импорт HTML с ресурсами архива;
  `index.html` первым, остальные — в естественном порядке имён.
- **Экспорт** (§16): форматы `epub`, `markdown`, `docx`, `html`, `txt`;
  режимы `translation` и `bilingual` (оригинал, затем перевод — для
  изучающих язык привычный порядок). Непереведённый блок выводится в
  оригинале и считается в `untranslated`. Название главы в оглавлении —
  перевод её первого заголовка, если тот совпадает с названием главы;
  название книги — так же по первому заголовку документа. `\n` в тексте —
  жёсткий перенос, кроме книг из `markdown` и `zip_repo` (там это мягкий
  перенос исходника). Внутренние ссылки исходника — текстом (структура
  файлов экспорта другая). Markdown с картинками — ZIP (`book.md` +
  `images/`), без картинок — `.md`; HTML — один файл с картинками в
  `data:`; TXT — картинки как «[Изображение: alt]».
- **Задача экспорта** выполняется worker-ом: аренда `locked_until` (5 мин
  = тайм-аут экспорта), захват `FOR UPDATE SKIP LOCKED`; упавший worker
  не теряет задачу — её подбирает другой после истечения аренды. Для
  worker в compose монтируется том `storage`.
- **Лимиты** (§10): `LINGUASTACK_PDF_MAX_PAGES` = 2000,
  `LINGUASTACK_PDF_TIMEOUT` = 2m, `LINGUASTACK_PDF_MEMORY_MB` = 1024,
  `LINGUASTACK_EXPORT_TIMEOUT` = 5m,
  `LINGUASTACK_MAX_ACTIVE_EXPORTS_PER_USER` = 3.

- [ ] **Step 1:** правки ADD и CLAUDE.md. **Step 2:** коммит
  `docs: решения этапа 5 — импорт PDF, DOCX, HTML и экспорт`.

## Импорт

### Task 1: HTML-импорт

**Files:** Create `backend/internal/importers/html.go`,
`backend/internal/importers/html_test.go`,
`backend/internal/importers/testdata/real/alice.html` (Project Gutenberg
#11, HTML-версия; README в `testdata/real` дополняется); Modify
`backend/internal/importers/epub.go` (опции `htmlEmitter`),
`backend/internal/importers/zip_repo.go` (ZIP с HTML без Markdown),
`backend/internal/importers/registry.go`, `real_test.go`.

**Interfaces:**
- Produces: `importers.NewHTML() Importer` (`FormatID() == "html"`,
  `Sniff`: `.html`/`.htm`); `htmlEmitter` получает поле
  `splitChapters bool` и `minLevel int` (заголовок открывает главу глубины
  `level-minLevel+1`), EPUB их не включает;
  `loadHTMLFiles(files []htmlFile, fallback cdm.BookMeta, assets func(name string) bool) Result`
  — общий путь для одиночного файла и ZIP, `htmlFile{name string; data []byte}`.

- [ ] **Step 1:** Тесты: `TestHTMLChaptersByHeadings` (h1 → главы, h2 →
  подглавы; текст до первого заголовка — неявная глава с названием книги);
  `TestHTMLMetaFromHead` (`<title>`, `<meta name="author">`, `<html lang>`);
  `TestHTMLMainContentOnly` (`<main>` есть — шапка и подвал сайта не
  попадают; `<nav>` пропускается всегда); `TestHTMLWindows1251Meta`
  (`<meta charset="windows-1251">` → правильный кириллический текст, без
  `encoding_guessed`) и `TestHTMLEncodingGuessed` (нет объявления, байты не
  UTF-8 → замечание `encoding_guessed`); `TestHTMLDataURIImage` (PNG в
  `data:` → ресурс `embedded/img-0001.png`, блок image с этим `src`);
  `TestHTMLRemoteImageSkipped` (`https://…` картинка не попадает в блоки,
  замечание `remote_images_skipped` с числом); `TestHTMLRelativeImageMissing`
  (одиночный файл → `missing_resource`); `TestZipWithHTMLPage`
  (`page.html` + `page_files/a.png` → один документ, картинка — ресурс);
  `TestZipHTMLOrder` (`index.html` первым, `ch2.html` < `ch10.html`);
  `TestHTMLScriptIsText` (`<script>` пропущен, `&lt;b&gt;` в тексте — текст);
  `TestRealGutenbergAliceHTML` (12 глав «CHAPTER …», первый абзац главы I
  начинается с «Alice was beginning to get very tired»).
- [ ] **Step 2:** FAIL. **Step 3:** реализация. `data:` декодируется только
  для `image/png|jpeg|gif|webp|avif|svg+xml`; расширение — по типу.
  Реестр: `EPUB, DOCX (задача 2), ZipRepo, Markdown, HTML, TXT, PDF
  (задача 4)` — HTML добавляется сейчас между Markdown и TXT.
- [ ] **Step 4:** PASS, `make lint`. **Step 5:** коммит `feat: импорт HTML`.

### Task 2: DOCX-импорт

**Files:** Create `backend/internal/importers/docx.go`,
`backend/internal/importers/docx_xml.go` (узел XML и потоковый разбор
тела), `backend/internal/importers/docx_test.go`,
`backend/internal/importers/docx_builder_test.go` (сборка DOCX в тестах,
как `epub_builder_test.go`); Modify `registry.go`.

**Interfaces:**
- Produces: `importers.NewDOCX(limits ArchiveLimits) Importer`
  (`FormatID() == "docx"`; `Sniff`: `.docx` или ZIP с `word/document.xml`);
  ресурсы — имена записей архива (`word/media/image1.png`).

- [ ] **Step 1:** Тесты (`docxBuilder` пишет `[Content_Types].xml`,
  `word/document.xml`, по желанию `styles.xml`, `numbering.xml`, rels,
  `footnotes.xml`, `docProps/core.xml`, медиа):
  `TestDOCXHeadingsFromStyles` (`Heading1`/`Heading2` и локализованный
  стиль `1` с именем `heading 1` → главы и подглавы);
  `TestDOCXOutlineLevel` (`outlineLvl` без стиля заголовка);
  `TestDOCXInlineFormatting` (жирный, курсив, моноширинный шрифт → `**`,
  `*`, `` ` ``; соседние прогоны с одинаковым оформлением сливаются;
  `2*3` экранируется); `TestDOCXHyperlink` (внешняя ссылка → `[t](url)`,
  `w:anchor` → текст); `TestDOCXLists` (маркированный и нумерованный
  уровни из `numbering.xml`, глубина = `ilvl+1`); `TestDOCXTable` (ячейки
  с `row`/`col`, `tblHeader` → `header`, продолжение `vMerge` пропущено);
  `TestDOCXImage` (абзац из одной картинки → image с `src` записи и
  `alt` из `docPr/@descr`; EMF → `unsupported_image`; отсутствующая
  запись → `missing_resource`); `TestDOCXCodeStyle` (подряд идущие абзацы
  стиля `Source Code` → один code_block с переводами строк);
  `TestDOCXFootnotes` («[1]» в абзаце, «[1] текст сноски» в конце главы);
  `TestDOCXSkipsWordTOC` (абзацы `toc 1` и SDT оглавления не попадают);
  `TestDOCXAlternateContent` (текст из `mc:Fallback` не дублируется);
  `TestDOCXCoreProperties` (dc:title, dc:creator, dc:language);
  `TestDOCXWithoutStylesAndNumbering` (минимальный DOCX — только
  `document.xml` — импортируется); `TestDOCXCorrupt` (не ZIP и ZIP без
  `word/document.xml` → `corrupt_file`, не ошибка); `TestDOCXArchiveLimits`
  (лимиты архива → `ErrArchiveLimits`); `TestDOCXRenamedZipSniffed` (DOCX
  с расширением `.zip` уходит в DOCX, а не в ZipRepo).
- [ ] **Step 2:** FAIL. **Step 3:** реализация: `document.xml` читается
  потоково — каждый элемент верхнего уровня `w:body` (`w:p`, `w:tbl`,
  `w:sdt`) декодируется в узел `xmlNode{Name xml.Name; Attr []xml.Attr; Nodes []xmlNode; Text string}`
  по отдельности, память ограничена одним абзацем или таблицей; имена
  сравниваются по локальной части (Strict OOXML с другими пространствами
  имён тоже читается).
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: импорт DOCX`.

### Task 3: извлечение текста PDF (PDFium/WebAssembly)

**Files:** Create `backend/internal/pdftext/pdftext.go` (типы и
`Extractor`), `backend/internal/pdftext/pdfium.go` (пул, защита, сборка
строк из символов), `backend/internal/pdftext/pdftext_test.go`,
`backend/internal/importers/testdata/pdf/*.html` (исходники фикстур),
`backend/internal/importers/testdata/pdf/generate.sh`,
`backend/internal/importers/testdata/pdf/*.pdf`; Modify
`backend/internal/config/config.go`, `backend/go.mod`, `.env.example`.

**Interfaces:**
- Produces:
  `pdftext.Document{Title, Author string; Pages []Page; Outline []OutlineItem}`,
  `pdftext.Page{Number int /*с 1*/; Width, Height float64; Lines []Line; Images int}`,
  `pdftext.Line{Spans []Span; X0, X1, Top, Bottom float64 /*точки, начало координат — левый нижний угол*/}`,
  `pdftext.Span{Text string; Size float64; Bold, Italic, Mono bool}`,
  `pdftext.OutlineItem{Title string; Page int /*с 1; 0 — нет назначения*/; Children []OutlineItem}`;
  `pdftext.Extractor interface { Extract(data []byte) (*Document, error) }`;
  ошибки `pdftext.ErrEncrypted`, `ErrCorrupt`, `ErrTimeout`,
  `ErrTooManyPages` (страниц больше лимита — до извлечения текста);
  `pdftext.NewPDFium(o Options) Extractor`,
  `Options{MaxPages int; Timeout time.Duration; MemoryLimitMB int; Instances int}`
  — пул создаётся при первом вызове, не при старте процесса;
  `config.Settings.PDFMaxPages`, `PDFTimeout`, `PDFMemoryMB`.

- [ ] **Step 1:** Фикстуры: `generate.sh` печатает каждый `*.html` через
  `google-chrome --headless=new --no-pdf-header-footer --print-to-pdf`
  (`--generate-pdf-document-outline` — для `outline.pdf`); исходники:
  `outline.html` (две главы h1 с разрывом страницы, раздел h2,
  кириллица, абзац на несколько строк с мягким переносом, список, блок
  кода моноширинным шрифтом, колонтитул с номером страницы на каждой из
  ≥ 3 страниц), `plain.html` (то же без закладок), `scanned.html`
  (страница из одной PNG-картинки с текстом). Chrome не шифрует PDF,
  поэтому `encrypted.pdf` — это `plain.pdf`, зашифрованный один раз
  утилитой `pdfcpu encrypt -upw x` (запуск из временного модуля, в
  `go.mod` проекта pdfcpu не попадает); как получен — в
  `testdata/pdf/README.md`.
- [ ] **Step 2:** Тесты: `TestPDFiumOutlineAndMeta` (закладки «Глава
  первая» → p1 c дочерней «Раздел 1.1», `Title` из `<title>`);
  `TestPDFiumLinesAndSpans` (строка заголовка — один Span с `Bold` и
  размером больше абзацного; моноширинная строка — `Mono`; символов
  `\r`/`\n` в тексте нет; `\x02` сохраняется на конце строки — его
  обрабатывает разбор вёрстки); `TestPDFiumScannedPageHasImagesNoText`;
  `TestPDFiumEncrypted` → `ErrEncrypted`; `TestPDFiumCorrupt` (`%PDF-1.7`
  и мусор) → `ErrCorrupt`; `TestPDFiumTooManyPages` (`MaxPages: 1` на
  трёхстраничном файле) → `ErrTooManyPages`; `TestPDFiumTimeout`
  (`Timeout: time.Nanosecond`) → `ErrTimeout`, а следующий вызов с
  нормальным тайм-аутом работает (убитый экземпляр не возвращается в
  пул); `TestPDFiumNoHostFilesystem` (модуль собран с пустым
  `FSConfig`: проверяется через функцию сборки конфигурации
  `runtimeOptions(o Options) webassembly.Config` — `FSConfig` без
  монтирований, предел памяти = `MemoryLimitMB*16` страниц);
  `TestPDFiumConcurrentExtract` (два вызова параллельно при `Instances: 1`
  — оба успешны, `-race` чист).
- [ ] **Step 3:** FAIL. **Step 4:** реализация. Конфигурация wazero:
  `wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2|experimental.CoreFeaturesExceptionHandling).WithMemoryLimitPages(n).WithCloseOnContextDone(true)`,
  `FSConfig: wazero.NewFSConfig()`, `Stdout/Stderr: io.Discard`.
  Тайм-аут: извлечение в горутине; по истечении — `instance.Kill()`, ответ
  `ErrTimeout`. Строки — из символов режима `Chars` с
  `CollectFontInformation`: граница строки — `\r\n` от PDFium; подряд
  идущие символы с тем же шрифтом и размером — один Span; `Bold` — вес
  ≥ 600 или имя шрифта с `Bold|Black|Heavy|Semibold`; `Italic` — флаг
  `0x40` или `Italic|Oblique`; `Mono` — флаг `0x1` или `Mono|Courier|Consol|Code`.
  Недопустимые символы XML (кроме `\x02`) вырезаются здесь же. Изображения
  страницы — число объектов `FPDF_PAGEOBJ_IMAGE`.
- [ ] **Step 5:** PASS (в т. ч. `go test -race ./internal/pdftext/`), lint.
  **Step 6:** коммит `feat: извлечение текста PDF через PDFium в WebAssembly`.

### Task 4: PDF-импорт: вёрстка → CDM

**Files:** Create `backend/internal/importers/pdf.go` (импортёр, главы,
замечания), `backend/internal/importers/pdf_layout.go` (строки → абзацы,
заголовки, колонтитулы, списки, код), `backend/internal/importers/pdf_test.go`,
`backend/internal/importers/pdf_layout_test.go`; Modify `registry.go`
(`Default` собирает `pdftext.NewPDFium` из настроек),
`backend/internal/httpapi/books.go` (ошибка `importers.ErrDocumentLimits`
→ 413), `importer.go`.

**Interfaces:**
- Consumes: `pdftext.Extractor`, типы `pdftext.*` (задача 3).
- Produces: `importers.NewPDF(x pdftext.Extractor) Importer`
  (`FormatID() == "pdf"`, `Sniff`: `.pdf` или `%PDF-` в первых 1024
  байтах); `importers.ErrDocumentLimits` (слишком много страниц — отказ,
  413 «Документ превышает допустимые пределы»); замечания `encrypted`
  (error), `corrupt_file` (error), `extract_timeout` (error),
  `scanned_pdf` (error), `scanned_pages` (warning), `pdf_images_skipped`
  (info: «изображения из PDF не переносятся»).

- [ ] **Step 1:** Тесты вёрстки на синтетических страницах (фальшивый
  `Extractor`): `TestPDFLinesJoinIntoParagraph` (три строки одного кегля
  без зазора → один абзац, строки через пробел); `TestPDFSoftHyphenJoins`
  («implemen\x02» + «tations» → «implementations»);
  `TestPDFParagraphBreakByGap`, `TestPDFParagraphBreakByIndent`,
  `TestPDFParagraphBreakShortLine`; `TestPDFHeadingBySize` (кегль 18 при
  основном 10 → heading, уровни по убыванию кегля);
  `TestPDFBoldLineHeading` («3. Персональные данные» жирным → heading;
  жирное начало длинного абзаца — не заголовок); `TestPDFHeadersFooters`
  (повтор вверху/внизу на 4 из 5 страниц и голые номера страниц
  удалены, такой же текст посреди страницы — нет); `TestPDFListItems`
  («• один», «1) два» → list_item, второй — `ordered`);
  `TestPDFMonoLinesCodeBlock` (отступы сохранены, текст не склеен в
  строку); `TestPDFChaptersFromOutline` (глава начинается со строки,
  совпадающей с названием закладки, текст выше на той же странице —
  в предыдущей главе; вложенная закладка → подглава);
  `TestPDFChaptersFromHeadings` (без закладок); `TestPDFPageMeta`
  (`meta.page`); `TestPDFScanned` (все страницы — сканы → `scanned_pdf`;
  одна из трёх → `scanned_pages` с её номером); `TestPDFTitle`
  (метаданные «Microsoft Word - отчёт.docx» не берутся — название по
  первому заголовку, иначе имя файла).
  Интеграционные на фикстурах задачи 3: `TestPDFImportOutlineFixture`
  (главы «Глава первая», «Chapter Two», подглава «Раздел 1.1»; абзац с
  мягким переносом цел; код — code_block; колонтитулов нет в блоках);
  `TestPDFImportPlainFixture` (те же главы по заголовкам);
  `TestPDFImportEncrypted` → `encrypted`; `TestPDFTooManyPagesRefused`
  (через HTTP: 413).
- [ ] **Step 2:** FAIL. **Step 3:** реализация по §15.1 дополнения.
  Реестр: PDF — последним (узнаётся и по содержимому).
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: импорт PDF`.

## Экспорт

### Task 5: ядро экспорта, Markdown, HTML и TXT

**Files:** Create `backend/internal/exporters/book.go` (модель экспорта),
`backend/internal/exporters/inline.go` (встроенная разметка → HTML/XHTML,
текст), `backend/internal/exporters/markdown.go`, `html.go`, `txt.go`,
`registry.go`, `exporters_test.go`, `inline_test.go`;
`backend/internal/exports/assemble.go` (CDM + сегменты → `exporters.Book`),
`backend/internal/exports/assemble_test.go`.

**Interfaces:**
- Produces:
  `exporters.Mode` (`ModeTranslation = "translation"`, `ModeBilingual = "bilingual"`);
  `exporters.Book{ID, Title, Author, Lang, SourceLang string; Mode Mode; HardBreaks bool; Chapters []Chapter; Assets func(name string) ([]byte, error)}`
  (`ID` — UUID задачи экспорта, идентификатор EPUB);
  `exporters.Chapter{Index int; Title string; Blocks []Block; Children []Chapter}`;
  `exporters.Block{Type cdm.BlockType; Level int; Meta map[string]string; Original, Translation string; Translated bool}`
  (`Text() string` — перевод, если есть, иначе оригинал);
  `exporters.Exporter interface { Format() string; Describe(b *Book) Output; Write(w io.Writer, b *Book) error }`,
  `exporters.Output{Ext, ContentType string}`;
  `exporters.Get(format string) (Exporter, bool)`, `exporters.Formats() []string`
  (`epub, markdown, docx, html, txt`);
  `exports.Assemble(doc cdm.Document, sourceFormat string, segs []models.TranslationSegment, mode exporters.Mode, sourceLang, targetLang string) (*exporters.Book, int /*untranslated*/)`.
  Внутренние помощники для задач 6–7: `renderInlineHTML(text string, o inlineOptions) string`
  (`inlineOptions{XHTML, HardBreaks bool; Image func(src string) (string, bool)}`),
  `inlinePlain(text string, hardBreaks bool) string`, `parseInline(text string) (ast.Node, []byte)`,
  `xmlSafe(s string) string`, `bookImages(b *Book) []string`.

- [ ] **Step 1:** Тесты: `TestAssembleTranslationMode` (переводимые блоки
  — перевод, код и картинки — как есть, `untranslated` = блоки без
  `translated_text`); `TestAssembleChapterTitleFromHeading` (первый
  заголовок совпадает с названием → название переведено; не совпадает
  — оригинал); `TestAssembleKeepsTOCOrder` (переставленные главы
  верхнего уровня — в порядке `doc.Chapters`); `TestAssembleHardBreaks`
  (`markdown`, `zip_repo` — мягкие, остальные — жёсткие);
  `TestInlineHTMLEscapes` (`<script>`, `&`, `"` экранированы, сырой HTML
  не выводится, `[x](javascript:…)` → текст `x`, относительная ссылка →
  текст, `https:` и `mailto:` — ссылки; `*a*`, `**b**`, `` `c` `` →
  em/strong/code); `TestInlineHTMLBreaks` (`\n` → `<br/>` при
  `HardBreaks`+`XHTML`, иначе пробел); `TestInlineParagraphOnly`
  («- не список», «# не заголовок», «1. не список» остаются текстом
  абзаца); `TestInlinePlain`; `TestXMLSafe` (`\x02`, `\x0c`, `\x00`
  убраны, `\t\n` и кириллица — нет); `TestMarkdownExport` (front matter
  title/author/lang; заголовки `#`×уровень; вложенные списки с
  отступом; таблица GFM с заголовком; блок кода с ограждением длиннее
  самой длинной серии ``` внутри и языком; `* * *` для разрыва; жёсткий
  перенос `\` + `\n`); `TestMarkdownBilingual` (оригинал, затем перевод;
  заголовок перевода — заголовок, оригинал под ним — абзац; ячейка
  «оригинал<br>перевод»); `TestMarkdownWithImagesIsZip` (`Describe` →
  `.zip`; внутри `book.md` и `images/…`; без картинок — `.md`);
  `TestMarkdownRoundTrip` (экспорт → импорт Markdown даёт те же главы и
  тексты); `TestHTMLExportSelfContained` (одна страница, `lang`, оглавление
  со ссылками на `#c{index}`, картинки — `data:image/png;base64`, нет
  `<script>`, двуязычные абзацы с `lang` оригинала);
  `TestHTMLExportReimports` (импорт HTML задачи 1 видит те же главы);
  `TestTXTExport` (заголовки, списки «- »/«1. », таблица «a | b»,
  картинка «[Изображение: alt]»; LF, UTF-8 без BOM).
- [ ] **Step 2:** FAIL. **Step 3:** реализация. Встроенная разметка —
  goldmark с одним парсером абзацев (`parser.WithBlockParsers` только
  `NewParagraphParser`) и стандартными встроенными парсерами; ссылки и
  картинки переписываются обходом AST до рендера; блок `raw_html` в
  HTML/EPUB/DOCX/TXT — его текстовое содержимое (`x/net/html`), в
  Markdown — как есть.
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: экспорт в Markdown, HTML и TXT`.

### Task 6: экспорт EPUB

**Files:** Create `backend/internal/exporters/epub.go`,
`backend/internal/exporters/epub_test.go`.

**Interfaces:**
- Consumes: `exporters.Book`, помощники задачи 5.
- Produces: `Exporter` формата `epub` (`.epub`, `application/epub+zip`).

- [ ] **Step 1:** Тесты: `TestEPUBStructure` (первая запись — несжатый
  `mimetype`; `META-INF/container.xml` указывает на `OEBPS/content.opf`;
  в OPF — `dc:identifier`, `dc:title`, `dc:language` = язык перевода,
  `dcterms:modified`, manifest со всеми файлами, spine по главам верхнего
  уровня; `nav.xhtml` с `epub:type="toc"` и вложенным `ol`; `toc.ncx`
  для читалок EPUB 2); `TestEPUBWellFormedXML` (каждый XHTML, OPF, NCX
  разбирается `encoding/xml` в строгом режиме, в т. ч. для текста с `&`,
  `<`, `\x02`); `TestEPUBImages` (картинка из `Assets` → `OEBPS/images/img0001.png`
  в manifest с media-type, `<img>` ссылается на неё; отсутствующая — alt
  текстом); `TestEPUBBilingual` (абзацы оригинала с `lang` исходного языка
  и классом `original`); `TestEPUBRoundTrip` (импорт EPUB этапа 2: те же
  главы, названия и тексты блоков, подглавы — в оглавлении).
- [ ] **Step 2:** FAIL. **Step 3:** реализация на `archive/zip` +
  `encoding/xml`-экранировании: файл XHTML на главу верхнего уровня
  (`text/ch0001.xhtml`), подглавы — внутри с якорями `c{index}`;
  `style.css` — простая типографика; идентификатор — `urn:uuid:` задачи
  экспорта (поле `Book.ID string`).
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: экспорт EPUB`.

### Task 7: экспорт DOCX

**Files:** Create `backend/internal/exporters/docx.go`,
`backend/internal/exporters/docx_test.go`.

**Interfaces:**
- Consumes: `exporters.Book`, `parseInline`, `xmlSafe` (задача 5).
- Produces: `Exporter` формата `docx` (`.docx`,
  `application/vnd.openxmlformats-officedocument.wordprocessingml.document`).

- [ ] **Step 1:** Тесты: `TestDOCXExportParts` (`[Content_Types].xml`,
  `_rels/.rels`, `docProps/core.xml` с title/creator/language,
  `word/document.xml`, `styles.xml` со стилями `Heading1`…`Heading6`
  (`outlineLvl`), `Quote`, `Caption`, `SourceCode`, `Original`;
  `numbering.xml` с маркированным и нумерованным списками);
  `TestDOCXExportWellFormed` (все части — корректный XML, текст с `&`,
  `<`, `\x02`); `TestDOCXExportInline` (жирный/курсив/код — свойства
  прогонов, внешняя ссылка — `w:hyperlink` с внешней связью);
  `TestDOCXExportImage` (PNG — `word/media/image1.png`, `wp:extent` по
  размеру картинки с ограничением ширины 6 дюймов; WebP/SVG — alt
  текстом); `TestDOCXExportRoundTrip` (импорт DOCX задачи 2: те же главы,
  уровни заголовков, списки с глубиной и нумерацией, ячейки таблиц, код,
  картинки).
- [ ] **Step 2:** FAIL. **Step 3:** реализация. **Step 4:** PASS, lint.
  **Step 5:** коммит `feat: экспорт DOCX`.

### Task 8: задача экспорта: миграция, worker, API

**Files:** Create `backend/migrations/00009_export_jobs.sql`,
`backend/internal/db/models/export.go`, `backend/internal/exports/runner.go`,
`backend/internal/exports/runner_test.go`, `backend/internal/httpapi/exports.go`,
`backend/internal/httpapi/exports_test.go`; Modify
`backend/internal/storage/files.go` (+ тест), `backend/internal/httpapi/server.go`,
`backend/internal/httpapi/isolation_matrix_test.go`,
`backend/internal/config/config.go`, `backend/cmd/worker/main.go`,
`docker-compose.yml` (том `storage` у worker), `.env.example`.

**Interfaces:**
- Consumes: `exports.Assemble`, `exporters.Get` (задачи 5–7).
- Produces: таблица `export_jobs(id, owner_id, book_id, project_id,
  format, mode, status, file_ext, content_type, size_bytes, untranslated,
  error, locked_until, created_at, updated_at, finished_at)`, внешние ключи
  `ON DELETE CASCADE` на users, books, translation_projects, CHECK на
  format/mode/status; `models.ExportJob`, статусы `ExportPending`,
  `ExportRunning`, `ExportCompleted`, `ExportFailed`;
  `storage.FileStore.SaveExport(userID, bookID, exportID uuid.UUID, ext string, write func(io.Writer) error) (int64, error)`
  (временный файл + rename), `OpenExport(…, ext) (*os.File, error)`,
  `DeleteExport(…, ext) error`, расширения `.epub .md .zip .docx .html .txt`;
  `exports.NewRunner(db *gorm.DB, store *storage.FileStore, cfg config.Settings) *Runner`,
  `(*Runner).Run(ctx) error`, `(*Runner).RunOnce(ctx) (bool, error)`;
  API: `POST /api/books/{id}/exports` (`{"translation_id","format","mode"}`
  → 202), `GET /api/books/{id}/exports` (новые первыми),
  `GET /api/exports/{id}`, `GET /api/exports/{id}/file`,
  `DELETE /api/exports/{id}`; ответ
  `{"id","book_id","translation_id","format","mode","status","file_name","size_bytes","untranslated","error","created_at","finished_at"}`;
  `config.Settings.ExportTimeout`, `MaxActiveExportsPerUser`.

- [ ] **Step 1:** Тесты: `TestCreateExportQueued` (202, `pending`);
  `TestCreateExportValidation` (формат/режим не из списка — 422; перевод
  другой книги пользователя — 422; чужой перевод — 404);
  `TestCreateExportLimit` (четвёртый активный — 429);
  `TestRunnerCompletesExport` (после `RunOnce`: `completed`, размер,
  `untranslated`, файл на диске); `TestRunnerFailedJob` (исключение
  рендера → `failed` с сообщением без внутренних путей);
  `TestRunnerReclaimsExpiredLease` (задача `running` с истёкшей арендой
  подбирается снова); `TestRunnerSkipLocked` (два раннера на
  `testdb.Committing` не берут одну задачу); `TestDownloadExport`
  (`Content-Type` формата, `Content-Disposition: attachment` с
  `filename*=UTF-8''…`, `X-Content-Type-Options: nosniff`, CSP `sandbox`,
  `Cache-Control: private, no-store`); `TestDownloadNotReady` (409);
  `TestDownloadFilenameSanitized` (название с `"`, `\r\n`, `/` и
  кириллицей → заголовок один, без перевода строки, имя
  «Название (ru).epub»); `TestDeleteExport` (файл и строка удалены;
  выполняющийся — 409); `TestBookDeleteRemovesExports` (каскад и файлы);
  `TestListBookExports`; матрица изоляции — ресурс `exports`
  (`{id}` → чужой экспорт), публичные маршруты не меняются.
- [ ] **Step 2:** FAIL. **Step 3:** реализация. Захват:
  `UPDATE export_jobs SET status='running', locked_until=now()+$lease … WHERE id = (SELECT id FROM export_jobs WHERE status='pending' OR (status='running' AND locked_until < now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *`;
  рендер с `context.WithTimeout(ExportTimeout)`; запись результата —
  условием `status='running' AND id=?` (строки нет — книга удалена:
  файл убирается). Worker запускает `Runner.Run` рядом с переводчиками.
- [ ] **Step 4:** PASS, lint. **Step 5:** коммит `feat: задача экспорта, скачивание и история экспортов`.

## Интерфейс и проверка

### Task 9: экспорт и новые форматы в веб-приложении

**Files:** Create `frontend/src/api/exports.ts`,
`frontend/src/components/ExportPanel.vue`,
`frontend/src/components/ExportPanel.test.ts`,
`frontend/src/components/ExportList.vue`; Modify
`frontend/src/api/types.ts`, `frontend/src/pages/TranslationDetailPage.vue`,
`frontend/src/pages/BookPage.vue` (+ тесты), `frontend/src/pages/ImportPage.vue`
(+ тест), `frontend/e2e/full-cycle.spec.ts`.

**Interfaces:**
- Consumes: API задачи 8.
- Produces: `createExport(bookId, {translation_id, format, mode})`,
  `listBookExports(bookId)`, `getExport(id)`, `deleteExport(id)`,
  `exportFileUrl(id)` (`/api/exports/{id}/file`).

- [ ] **Step 1:** Тесты: `ExportPanel` — форматы «EPUB», «Markdown», «Word
  (DOCX)», «HTML», «Текст (TXT)», режимы «Только перевод» и «Оригинал и
  перевод»; кнопка «Экспортировать»; опрос раз в секунду до `completed`
  или `failed`; готово — ссылка «Скачать» с размером («1,2 МБ»);
  `untranslated > 0` — «N фрагментов не переведены и оставлены в
  оригинале»; `failed` — текст ошибки и кнопка «Повторить экспорт»;
  429 — сообщение сервера. `BookPage` — раздел «Экспорты» (история:
  формат, режим, дата, «Скачать», «Удалить» через ConfirmButton).
  `ImportPage` — `accept` включает `.pdf,.docx,.html,.htm`, подсказка
  перечисляет форматы. Сквозной сценарий: после перевода — экспорт
  Markdown и EPUB, `page.waitForEvent('download')`, имя файла и
  содержимое Markdown содержат перевод («RU:»); импорт HTML-книги.
- [ ] **Step 2:** FAIL. **Step 3:** реализация на токенах
  `tokens.css`, проверка на 390 px. **Step 4:** `npm run lint &&
  npm run typecheck && npm test`, `npm run e2e`. **Step 5:** коммит
  `feat: экспорт и новые форматы импорта в веб-приложении`.

### Task 10: документация, живая проверка, ревью

**Files:** Modify `README.md` (форматы импорта и экспорта, PDF-фикстуры),
`docs/spec/SPEC-ADDENDUM.md` (трассируемость: №3 и №12 закрыты),
этот план (статус и отклонения).

- [ ] **Step 1:** Живая проверка на реальных файлах этой машины (в
  репозиторий не попадают): PDF из LaTeX (`petsc4py.pdf`), PDF из Word с
  кириллицей, DOCX из Word, HTML Алисы, — импорт через API, просмотр
  оглавления и блоков; перевод Алисы провайдером fake → экспорт во все
  пять форматов в обоих режимах → EPUB и DOCX снова импортируются;
  `docker compose up -d --build`: импорт PDF в контейнере (wasm без
  доступа к ФС) и экспорт через worker с томом `storage`.
- [ ] **Step 2:** Полный прогон: `make lint`, `go test -race ./...`,
  `npm run lint && npm run typecheck && npm test`, `npm run e2e`.
- [ ] **Step 3:** Ревью всей ветки (Review Focus выше), исправления с
  тестами. **Step 4:** коммит `docs: этап 5 — форматы импорта и экспорта`,
  слияние в main.

## За рамками этого плана

- OCR сканированных PDF (ТЗ §3.1 — после MVP) и экспорт в PDF (§3.2 —
  дополнительная функция).
- Изображения и таблицы из PDF (переносится только текст и структура).
- Глоссарии как сущности, панель администратора, Telegram (этап 6).
