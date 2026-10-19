package importers_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
)

func loadHTML(t *testing.T, src string) importers.Result {
	t.Helper()
	res, err := importers.NewHTML().Load([]byte(src), "book.html")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

// tinyPNG — корректный PNG 1×1.
var tinyPNG, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func TestHTMLChaptersByHeadings(t *testing.T) {
	res := loadHTML(t, `<html><head><title>Книга</title></head><body>
<p>Вступление до заголовков.</p>
<h1>Глава 1</h1><p>Первый абзац.</p>
<h2>Раздел 1.1</h2><p>Вложенный абзац.</p>
<h1>Глава 2</h1><p>Второй абзац.</p>
</body></html>`)
	doc := res.Document
	require.Equal(t, "html", doc.Meta.SourceFormat)
	require.Equal(t, []string{"Книга", "Глава 1", "Глава 2"}, chapterTitles(doc.Chapters),
		"текст до первого заголовка — неявная глава с названием книги")
	require.Equal(t, []string{"Раздел 1.1"}, chapterTitles(doc.Chapters[1].Children))
	require.Equal(t, []string{"Глава 1", "Первый абзац."}, texts(doc.Chapters[1].Blocks))
	require.Equal(t, cdm.Heading, doc.Chapters[1].Blocks[0].Type)
	require.Equal(t, 1, doc.Chapters[1].Blocks[0].Level)
}

func TestHTMLSingleTopHeadingIsBookTitle(t *testing.T) {
	// Как у Project Gutenberg: один h1 с названием книги и главы в h2. Главы
	// не должны оказаться подглавами названия.
	res := loadHTML(t, `<body><h1>Алиса</h1><h2>Глава I</h2><p>Раз.</p><h2>Глава II</h2><p>Два.</p></body>`)
	require.Equal(t, []string{"Алиса", "Глава I", "Глава II"}, chapterTitles(res.Document.Chapters))
}

func TestHTMLMetaFromHead(t *testing.T) {
	res := loadHTML(t, `<!doctype html><html lang="en"><head><title>Site | Title</title>
<meta name="author" content="Lewis Carroll"></head><body><p>Text.</p></body></html>`)
	meta := res.Document.Meta
	require.Equal(t, "Site | Title", meta.Title)
	require.Equal(t, "Lewis Carroll", meta.Author)
	require.Equal(t, "en", meta.Language)

	res = loadHTML(t, `<html><head><title>Alice | Project Gutenberg</title>
<meta name="dc.title" content="Alice's Adventures"><meta name="dc.creator" content="Carroll, Lewis">
<meta name="dc.language" content="en"></head><body><p>Text.</p></body></html>`)
	meta = res.Document.Meta
	require.Equal(t, "Alice's Adventures", meta.Title, "dc.title точнее <title> сайта")
	require.Equal(t, "Carroll, Lewis", meta.Author)
	require.Equal(t, "en", meta.Language)

	res = loadHTML(t, `<body><h1>Заголовок</h1><p>Текст.</p></body>`)
	require.Equal(t, "Заголовок", res.Document.Meta.Title, "без <title> — первый заголовок")
}

func TestHTMLMainContentOnly(t *testing.T) {
	res := loadHTML(t, `<body><header><p>Шапка сайта</p></header>
<nav><ul><li><a href="/">Главная</a></li></ul></nav>
<main><h1>Статья</h1><nav><a href="#a">Оглавление</a></nav><p>Текст статьи.</p></main>
<footer><p>Подвал</p></footer></body>`)
	require.Equal(t, []string{"Статья", "Текст статьи."}, texts(res.Document.Blocks()))

	// Без <main> — <body>, но шапка и подвал страницы и навигация не книга.
	res = loadHTML(t, `<body><header><p>Шапка</p></header><nav>Меню</nav><h1>Книга</h1><p>Текст.</p>
<article><header><h2>Раздел</h2></header><p>Ещё.</p></article><article><p>Третий.</p></article>
<footer><p>Подвал</p></footer></body>`)
	require.Equal(t, []string{"Книга", "Текст.", "Раздел", "Ещё.", "Третий."}, texts(res.Document.Blocks()),
		"шапка внутри article — часть содержимого")

	// Единственный <article> — содержимое.
	res = loadHTML(t, `<body><div>Реклама</div><article><h1>Пост</h1><p>Тело.</p></article></body>`)
	require.Equal(t, []string{"Пост", "Тело."}, texts(res.Document.Blocks()))
}

func TestHTMLWindows1251Meta(t *testing.T) {
	src, err := charmap.Windows1251.NewEncoder().String(
		`<html><head><meta charset="windows-1251"><title>Книга</title></head><body><p>Привет, мир.</p></body></html>`)
	require.NoError(t, err)
	res := loadHTML(t, src)
	require.Equal(t, "Книга", res.Document.Meta.Title)
	require.Equal(t, []string{"Привет, мир."}, texts(res.Document.Blocks()))
	require.Nil(t, issueWith(res.Issues, "encoding_guessed"), "кодировка объявлена — не догадка")
}

func TestHTMLEncodingGuessed(t *testing.T) {
	src, err := charmap.Windows1251.NewEncoder().String(`<body><p>Привет, мир.</p></body>`)
	require.NoError(t, err)
	res := loadHTML(t, src)
	require.Equal(t, []string{"Привет, мир."}, texts(res.Document.Blocks()))
	require.NotNil(t, issueWith(res.Issues, "encoding_guessed"))
}

func TestHTMLDataURIImage(t *testing.T) {
	src := `<body><p>Текст.</p><p><img alt="Схема" src="data:image/png;base64,` +
		base64.StdEncoding.EncodeToString(tinyPNG) + `"></p></body>`
	res := loadHTML(t, src)
	images := blocksOf(res.Document, cdm.Image)
	require.Len(t, images, 1)
	require.Equal(t, "embedded/img-0001.png", images[0].Meta["src"])
	require.Equal(t, "Схема", images[0].Meta["alt"])
	require.Len(t, res.Assets, 1)
	require.Equal(t, "embedded/img-0001.png", res.Assets[0].Name)
	data, err := res.Assets[0].Open()
	require.NoError(t, err)
	require.Equal(t, tinyPNG, data)
	require.Nil(t, issueWith(res.Issues, "missing_resource"))
}

func TestHTMLDataURINotImageIgnored(t *testing.T) {
	res := loadHTML(t, `<body><p>Текст.</p><p><img src="data:text/html;base64,PHNjcmlwdD4="></p></body>`)
	require.Empty(t, blocksOf(res.Document, cdm.Image))
	require.Empty(t, res.Assets)
}

func TestHTMLRemoteImageSkipped(t *testing.T) {
	res := loadHTML(t, `<body><p>Текст <img src="https://example.com/a.png" alt="a"> дальше.</p>
<p><img src="//cdn.example.com/b.png"></p></body>`)
	require.Empty(t, blocksOf(res.Document, cdm.Image))
	require.Equal(t, []string{"Текст дальше."}, texts(res.Document.Blocks()))
	skipped := issueWith(res.Issues, "remote_images_skipped")
	require.NotNil(t, skipped)
	require.Contains(t, skipped.Message, "2")
	require.Empty(t, res.Assets)
}

func TestHTMLRelativeImageMissing(t *testing.T) {
	res := loadHTML(t, `<body><p>Текст.</p><p><img src="images/cover.jpg"></p></body>`)
	missing := issueWith(res.Issues, "missing_resource")
	require.NotNil(t, missing)
	require.Contains(t, missing.Message, "images/cover.jpg")
}

func TestHTMLScriptIsText(t *testing.T) {
	res := loadHTML(t, `<body><script>alert(1)</script><style>p{}</style><p>a &lt;b&gt; c &amp; 2*3</p></body>`)
	require.Equal(t, []string{`a <b> c & 2\*3`}, texts(res.Document.Blocks()))
}

func TestZipWithHTMLPage(t *testing.T) {
	// «Сохранить страницу полностью»: page.html + page_files/.
	res := loadZip(t, makeZip(t,
		zipFile{name: "page.html", body: `<html><head><title>Страница</title></head><body><h1>Глава</h1>` +
			`<p>Текст.</p><p><img src="page_files/a.png" alt="A"></p><p><img src="page_files/gone.png"></p></body></html>`},
		zipFile{name: "page_files/a.png", body: string(tinyPNG)},
		zipFile{name: "page_files/style.css", body: "p{}"},
	))
	doc := res.Document
	require.Equal(t, "html", doc.Meta.SourceFormat)
	require.Equal(t, "Страница", doc.Meta.Title)
	images := blocksOf(doc, cdm.Image)
	require.Len(t, images, 2)
	require.Equal(t, "page_files/a.png", images[0].Meta["src"])
	require.Len(t, res.Assets, 1)
	require.Equal(t, "page_files/a.png", res.Assets[0].Name)
	missing := issueWith(res.Issues, "missing_resource")
	require.NotNil(t, missing)
	require.Contains(t, missing.Message, "page_files/gone.png")
}

func TestZipHTMLOrder(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"ch10.html", "<h1>Десятая</h1><p>10</p>",
		"ch2.html", "<h1>Вторая</h1><p>2</p>",
		"index.html", "<h1>Начало</h1><p>0</p>",
	)...))
	require.Equal(t, []string{"Начало", "Вторая", "Десятая"}, chapterTitles(res.Document.Chapters))
}

func TestZipWithMarkdownIgnoresHTML(t *testing.T) {
	// Репозиторий документации с HTML рядом — по-прежнему Markdown-книга.
	res := loadZip(t, makeZip(t, files(
		"README.md", "# Книга\n\nТекст.",
		"docs/page.html", "<h1>Чужое</h1>",
	)...))
	require.Equal(t, "zip_repo", res.Document.Meta.SourceFormat)
	require.Equal(t, []string{"Книга"}, chapterTitles(res.Document.Chapters))
}

func TestPickHTMLByExtension(t *testing.T) {
	r := defaultRegistry(t)
	for _, name := range []string{"a.html", "B.HTM"} {
		imp, err := r.Pick([]byte("<p>x</p>"), name)
		require.NoError(t, err)
		require.Equal(t, "html", imp.FormatID(), name)
	}
}

func TestHTMLEmbeddedImagesCapped(t *testing.T) {
	// Тысячи крошечных картинок в data: — тысячи файлов в хранилище.
	var b strings.Builder
	b.WriteString("<body><p>Текст.</p>")
	img := `<p><img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(tinyPNG) + `"></p>`
	for i := 0; i < importers.MaxEmbeddedImages+5; i++ {
		b.WriteString(strings.Replace(img, "base64,", "base64,"+strings.Repeat(" ", i), 1))
	}
	b.WriteString("</body>")
	res := loadHTML(t, b.String())
	require.Len(t, res.Assets, importers.MaxEmbeddedImages)
	require.NotNil(t, issueWith(res.Issues, "too_many_images"))
}
