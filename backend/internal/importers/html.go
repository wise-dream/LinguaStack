package importers

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"

	"github.com/vnovels/linguastack/internal/cdm"
)

// HTML — импорт HTML-документа (ТЗ §3.1: содержимое документа и связанные
// ресурсы; дополнение §15.3).
//
// Разметка разбирается тем же htmlEmitter, что и главы EPUB, но главы
// задают заголовки: у одиночного HTML нет spine и оглавления. Связанные
// ресурсы одиночного файла — только картинки в data:; страница вместе с
// каталогом ресурсов приходит ZIP-архивом (см. ZipRepo).
type HTML struct{}

// NewHTML создаёт импортёр HTML.
func NewHTML() Importer { return HTML{} }

func (HTML) FormatID() string { return "html" }

func (HTML) Sniff(_ []byte, filename string) bool {
	ext := fileExt(filename)
	return ext == ".html" || ext == ".htm"
}

func (HTML) Load(data []byte, filename string) (Result, error) {
	base := path.Base(strings.ReplaceAll(filename, "\\", "/"))
	return loadHTMLFiles([]htmlFile{{name: base, data: data}}, titleFromFilename(filename), nil, nil), nil
}

// htmlFile — HTML-документ и его путь от корня книги (по нему разрешаются
// относительные адреса картинок).
type htmlFile struct {
	name string
	data []byte
}

// loadHTMLFiles собирает книгу из HTML-документов по порядку: одиночный
// файл или страницы из ZIP. archive — записи архива для относительных
// картинок (nil — их нет, и картинки считаются отсутствующими).
func loadHTMLFiles(files []htmlFile, fallbackTitle string, archive map[string]*zip.File, issues []Issue) Result {
	res := &htmlResources{archive: archive, resolved: map[string]string{}, embeddedNames: map[string]bool{}}
	var builder *cdm.Builder
	var images []string
	for _, f := range files {
		text, decodeIssues := decodeHTML(f.data)
		issues = append(issues, decodeIssues...)
		doc, err := html.Parse(strings.NewReader(text))
		if err != nil {
			issues = append(issues, Issue{Severity: Warning, Code: "corrupt_entry",
				Message: fmt.Sprintf("Не разобрать HTML %q; файл пропущен.", f.name)})
			continue
		}
		root := contentRoot(doc)
		if builder == nil {
			builder = cdm.NewBuilder(htmlMeta(doc, root, fallbackTitle))
		}
		dir := path.Dir(f.name)
		em := &htmlEmitter{b: builder, dir: dir, blockish: map[*html.Node]bool{},
			splitChapters: true, minLevel: chapterBaseLevel(root),
			imageSrc: func(raw string) (string, bool) { return res.resolve(dir, raw) }}
		em.markBlockish(root)
		em.blocks(root, htmlContext{})
		images = append(images, em.images...)
	}
	if builder == nil {
		builder = cdm.NewBuilder(cdm.BookMeta{Title: fallbackTitle, SourceFormat: "html"})
	}

	result := finish(builder.Build(), issues)
	var fromArchive []string
	for _, name := range images {
		if !res.embeddedNames[name] {
			fromArchive = append(fromArchive, name)
		}
	}
	assets, missing := collectAssets(archive, fromArchive)
	result.Assets = append(res.embedded, assets...)
	if len(missing) > 0 {
		result.Issues = append(result.Issues, missingResourceIssue(missing))
	}
	if res.overLimit > 0 {
		result.Issues = append(result.Issues, Issue{Severity: Warning, Code: "too_many_images",
			Message: fmt.Sprintf("Встроенных изображений больше %d; лишние (%d) пропущены.", MaxEmbeddedImages, res.overLimit)})
	}
	if res.remote > 0 {
		result.Issues = append(result.Issues, Issue{Severity: Info, Code: "remote_images_skipped",
			Message: fmt.Sprintf("Внешние изображения не загружаются (%d): сервер не обращается к адресам из загруженных файлов.", res.remote)})
	}
	return result
}

var metaCharsetRe = regexp.MustCompile(`(?i)<meta[^>]*charset`)

// decodeHTML: объявленная кодировка (BOM, <meta charset>) — по объявлению;
// без объявления — как TXT: UTF-8, если байты валидны, иначе
// windows-1251 с замечанием. charset.DetermineEncoding в этом случае
// смотрит только первые 1024 байта и выбирает windows-1252 — для
// русскоязычных книг это гарантированная порча текста.
func decodeHTML(data []byte) (string, []Issue) {
	if bytes.HasPrefix(data, bomUTF8) || bytes.HasPrefix(data, bomUTF16LE) || bytes.HasPrefix(data, bomUTF16BE) ||
		!metaCharsetRe.Match(data[:min(len(data), 1024)]) {
		return decodeText(data)
	}
	enc, _, _ := charset.DetermineEncoding(data, "")
	decoded, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return decodeText(data)
	}
	return sanitizeUTF8(string(decoded))
}

// contentRoot — элемент с содержимым документа (дополнение §15.3):
// <main>, иначе единственный <article>, иначе <body> без шапки и подвала
// страницы. Навигация не импортируется нигде.
func contentRoot(doc *html.Node) *html.Node {
	body := findElement(doc, atom.Body)
	if body == nil {
		body = doc
	}
	removeElements(body, func(n *html.Node) bool { return n.DataAtom == atom.Nav })
	if main := findElement(body, atom.Main); main != nil {
		return main
	}
	var articles []*html.Node
	walkNodes(body, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.DataAtom == atom.Article {
			articles = append(articles, n)
			return false
		}
		return true
	})
	if len(articles) == 1 {
		return articles[0]
	}
	for c := body.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && (c.DataAtom == atom.Header || c.DataAtom == atom.Footer) {
			body.RemoveChild(c)
		}
		c = next
	}
	return body
}

func removeElements(root *html.Node, match func(*html.Node) bool) {
	var found []*html.Node
	walkNodes(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
			return false
		}
		return true
	})
	for _, n := range found {
		n.Parent.RemoveChild(n)
	}
}

// chapterBaseLevel — уровень заголовка, который открывает главу верхнего
// уровня. Единственный заголовок самого крупного уровня — название книги
// (так у Project Gutenberg: один h1 и главы в h2); тогда главы верхнего
// уровня — заголовки следующего уровня, а не подглавы названия.
func chapterBaseLevel(root *html.Node) int {
	counts := map[int]int{}
	walkNodes(root, func(n *html.Node) bool {
		if n.Type == html.ElementNode && skippedTags[n.DataAtom] {
			return false
		}
		if level := headingLevel(n); level > 0 {
			counts[level]++
		}
		return true
	})
	var levels []int
	for level := range counts {
		levels = append(levels, level)
	}
	slices.Sort(levels)
	switch {
	case len(levels) == 0:
		return 1
	case counts[levels[0]] == 1 && len(levels) > 1:
		return levels[1]
	}
	return levels[0]
}

func headingLevel(n *html.Node) int {
	if n.Type != html.ElementNode {
		return 0
	}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return int(n.Data[1] - '0')
	}
	return 0
}

// htmlMeta — метаданные из <head>: Dublin Core точнее <title>, который у
// сайтов обычно несёт ещё и название сайта.
func htmlMeta(doc, root *html.Node, fallbackTitle string) cdm.BookMeta {
	meta := cdm.BookMeta{Title: fallbackTitle, SourceFormat: "html"}
	var dcTitle, dcLang string
	walkNodes(doc, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.DataAtom != atom.Meta {
			return true
		}
		content := collapseSpaces(attr(n, "content"))
		switch strings.ToLower(attr(n, "name")) {
		case "dc.title":
			dcTitle = content
		case "author", "dc.creator":
			if meta.Author == "" {
				meta.Author = content
			}
		case "dc.language":
			dcLang = content
		}
		return false
	})
	title := ""
	if t := findElement(doc, atom.Title); t != nil {
		title = collapseSpaces(textContent(t))
	}
	if h := firstHeadingNode(root); title == "" && h != nil {
		title = collapseSpaces(textContent(h))
	}
	switch {
	case dcTitle != "":
		meta.Title = dcTitle
	case title != "":
		meta.Title = title
	}
	if h := findElement(doc, atom.Html); h != nil {
		meta.Language = attr(h, "lang")
	}
	if meta.Language == "" {
		meta.Language = dcLang
	}
	return meta
}

// MaxEmbeddedImages — сколько картинок из data: становятся ресурсами книги:
// столько же, сколько записей в ZIP (дополнение §10). Иначе файл в 50 МБ
// из крошечных картинок дал бы сотни тысяч файлов в хранилище.
const MaxEmbeddedImages = 2000

// embeddedImageTypes — какие картинки из data: становятся ресурсами.
var embeddedImageTypes = map[string]string{
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
	"image/avif": ".avif", "image/svg+xml": ".svg",
}

// htmlResources разрешает адреса картинок HTML: data: — в ресурсы книги,
// внешние — пропуск, относительные — в записи архива. Результат
// запоминается: одну картинку разбор спрашивает дважды (абзац из одной
// картинки и встроенная разметка).
type htmlResources struct {
	archive       map[string]*zip.File
	resolved      map[string]string // dir + "\x00" + адрес → имя ("" — пропущена)
	embedded      []Asset
	embeddedNames map[string]bool
	remote        int
	overLimit     int // картинки из data: сверх MaxEmbeddedImages
}

func (r *htmlResources) resolve(dir, raw string) (string, bool) {
	key := dir + "\x00" + raw
	if name, ok := r.resolved[key]; ok {
		return name, name != ""
	}
	var name string
	switch lower := strings.ToLower(raw); {
	case strings.HasPrefix(lower, "data:"):
		name = r.embed(raw)
	case strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:") || strings.HasPrefix(raw, "//"):
		r.remote++
	case schemeRe.MatchString(raw):
		// другие схемы картинкой не бывают
	default:
		name = cleanHref(dir, raw)
	}
	r.resolved[key] = name
	return name, name != ""
}

func (r *htmlResources) embed(raw string) string {
	if len(r.embedded) >= MaxEmbeddedImages {
		r.overLimit++
		return ""
	}
	header, payload, ok := strings.Cut(raw[len("data:"):], ",")
	if !ok {
		return ""
	}
	params := strings.Split(header, ";")
	ext, ok := embeddedImageTypes[strings.ToLower(strings.TrimSpace(params[0]))]
	if !ok {
		return ""
	}
	var data []byte
	var err error
	if strings.EqualFold(strings.TrimSpace(params[len(params)-1]), "base64") {
		data, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(payload), ""))
	} else {
		var s string
		s, err = url.PathUnescape(payload)
		data = []byte(s)
	}
	if err != nil || len(data) == 0 {
		return ""
	}
	var name string
	for n := len(r.embedded) + 1; ; n++ {
		name = fmt.Sprintf("embedded/img-%04d%s", n, ext)
		if _, taken := r.archive[name]; !taken {
			break
		}
	}
	r.embeddedNames[name] = true
	r.embedded = append(r.embedded, Asset{Name: name, Size: int64(len(data)),
		Open: func() ([]byte, error) { return data, nil }})
	return name
}

// htmlPages — страницы HTML в архиве без Markdown: только самого верхнего
// из встречающихся уровней каталогов (рядом с page.html браузер кладёт в
// page_files/ ещё и HTML встроенных фреймов), index.html первым, остальные
// в естественном порядке имён.
func htmlPages(names []string) []string {
	depth := func(name string) int { return strings.Count(name, "/") }
	minDepth := -1
	var pages []string
	for _, name := range names {
		switch fileExt(name) {
		case ".html", ".htm", ".xhtml":
		default:
			continue
		}
		pages = append(pages, name)
		if d := depth(name); minDepth < 0 || d < minDepth {
			minDepth = d
		}
	}
	pages = slices.DeleteFunc(pages, func(name string) bool { return depth(name) != minDepth })
	isIndex := func(name string) bool {
		base := strings.ToLower(path.Base(name))
		return base == "index.html" || base == "index.htm"
	}
	slices.SortFunc(pages, func(a, b string) int {
		switch ia, ib := isIndex(a), isIndex(b); {
		case ia && !ib:
			return -1
		case ib && !ia:
			return 1
		}
		return naturalCompare(a, b)
	})
	return pages
}
