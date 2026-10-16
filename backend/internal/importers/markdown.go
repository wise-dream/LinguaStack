package importers

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"github.com/vnovels/linguastack/internal/cdm"
)

// Markdown — импорт Markdown (ТЗ §3.1: заголовки, списки, таблицы, код, ссылки).
//
// Текст каждого блока — исходный Markdown из участков исходника, а не
// отрендеренный HTML: маскирование и проверка разметки (дополнение §4, §5)
// работают именно с разметкой.
type Markdown struct{}

// NewMarkdown создаёт импортёр Markdown.
func NewMarkdown() Importer { return Markdown{} }

func (Markdown) FormatID() string { return "markdown" }

func (Markdown) Sniff(_ []byte, filename string) bool {
	ext := fileExt(filename)
	return ext == ".md" || ext == ".markdown"
}

func (Markdown) Load(data []byte, filename string) (Result, error) {
	src, issues := sanitizeUTF8(string(data))
	body, fm := splitFrontMatter(normalizeNewlines(strings.TrimPrefix(src, "\ufeff")))
	doc := parseMarkdown([]byte(body))

	meta := cdm.BookMeta{Title: titleFromFilename(filename), Author: fm.author, SourceFormat: "markdown"}
	switch {
	case fm.title != "":
		meta.Title = fm.title
	case doc.firstHeading() != "":
		meta.Title = doc.firstHeading()
	}

	builder := cdm.NewBuilder(meta)
	doc.emit(builder, meta.Title, "", 1)
	return finish(builder.Build(), issues), nil
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// --- front matter ---

type frontMatter struct{ title, author string }

var (
	frontMatterRe = regexp.MustCompile(`(?s)\A---[ \t]*\n(.*?)\n(?:---|\.\.\.)[ \t]*(?:\n|\z)`)
	fmFieldRe     = regexp.MustCompile(`(?m)^(title|author)[ \t]*:[ \t]*(.+?)[ \t]*$`)
)

// splitFrontMatter отделяет YAML-шапку. Без этого goldmark принял бы
// «title: …» + «---» за заголовок второго уровня, и в книге появилась бы
// фальшивая глава. Полный YAML не нужен: берутся только title и author.
func splitFrontMatter(s string) (string, frontMatter) {
	m := frontMatterRe.FindStringSubmatchIndex(s)
	if m == nil {
		return s, frontMatter{}
	}
	var fm frontMatter
	for _, f := range fmFieldRe.FindAllStringSubmatch(s[m[2]:m[3]], -1) {
		value := strings.Trim(f[2], `"'`)
		switch f[1] {
		case "title":
			fm.title = value
		case "author":
			fm.author = value
		}
	}
	return s[m[1]:], fm
}

// --- разбор и обход AST ---

type markdownDoc struct {
	root ast.Node
	src  []byte
}

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.Table)).Parser()

func parseMarkdown(src []byte) markdownDoc {
	return markdownDoc{root: markdownParser.Parse(text.NewReader(src)), src: src}
}

func (d markdownDoc) firstHeading() string {
	for c := d.root.FirstChild(); c != nil; c = c.NextSibling() {
		if h, ok := c.(*ast.Heading); ok {
			return plainText(h, d.src)
		}
	}
	return ""
}

// markdownRefs — относительные ссылки и картинки файла. Импорт репозитория
// по ним восстанавливает порядок глав и ищет отсутствующие ресурсы (ТЗ §4.3).
type markdownRefs struct {
	images []string
	links  []string
}

// emit добавляет блоки документа в builder. Каждый вызов начинает новую
// главу: если файл не открывается заголовком, глава получает fileTitle.
// Глубина главы считается от самого мелкого заголовка файла, поэтому файл,
// где верхний уровень — «##», даёт главы, а не подглавы. baseDir — каталог
// файла в репозитории, относительно него разрешаются пути картинок и ссылок.
//
// baseDepth — глубина файла в оглавлении репозитория (1 — верхний уровень):
// раздел, вложенный в SUMMARY.md, становится подглавой.
func (d markdownDoc) emit(b *cdm.Builder, fileTitle, baseDir string, baseDepth int) markdownRefs {
	if baseDepth < 1 {
		baseDepth = 1
	}
	e := &mdEmitter{b: b, src: d.src, fileTitle: fileTitle, baseDir: baseDir,
		minLevel: minHeadingLevel(d.root), baseDepth: baseDepth}
	e.children(d.root, mdContext{})
	return e.refs
}

type mdContext struct {
	listDepth int
	ordered   bool
	inQuote   bool
}

type mdEmitter struct {
	b         *cdm.Builder
	src       []byte
	fileTitle string
	baseDir   string
	minLevel  int
	baseDepth int
	started   bool
	refs      markdownRefs
}

func minHeadingLevel(root ast.Node) int {
	minLevel := 0
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering && (minLevel == 0 || h.Level < minLevel) {
			minLevel = h.Level
		}
		return ast.WalkContinue, nil
	})
	if minLevel == 0 {
		return 1
	}
	return minLevel
}

func (e *mdEmitter) add(t cdm.BlockType, txt string, level int, meta map[string]string) {
	if !e.started {
		e.b.StartChapter(e.fileTitle, e.baseDepth)
		e.started = true
	}
	e.b.Add(t, txt, level, meta)
}

func (e *mdEmitter) children(n ast.Node, ctx mdContext) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		e.block(c, ctx)
	}
}

func (e *mdEmitter) block(n ast.Node, ctx mdContext) {
	switch node := n.(type) {
	case *ast.Heading:
		e.b.StartChapter(plainText(node, e.src), node.Level-e.minLevel+e.baseDepth)
		e.started = true
		e.b.Add(cdm.Heading, linesText(node, e.src), node.Level, nil)
	case *ast.Paragraph, *ast.TextBlock:
		e.textBlock(n, ctx)
	case *ast.FencedCodeBlock:
		var meta map[string]string
		if lang := string(node.Language(e.src)); lang != "" {
			meta = map[string]string{"language": lang}
		}
		e.add(cdm.CodeBlock, strings.TrimRight(rawLines(node, e.src), "\n"), 0, meta)
	case *ast.CodeBlock:
		e.add(cdm.CodeBlock, strings.TrimRight(rawLines(node, e.src), "\n"), 0, nil)
	case *ast.List:
		inner := mdContext{listDepth: ctx.listDepth + 1, ordered: node.IsOrdered(), inQuote: ctx.inQuote}
		for item := node.FirstChild(); item != nil; item = item.NextSibling() {
			e.children(item, inner)
		}
	case *ast.Blockquote:
		e.children(node, mdContext{listDepth: ctx.listDepth, ordered: ctx.ordered, inQuote: true})
	case *ast.ThematicBreak:
		e.add(cdm.ThematicBreak, "", 0, nil)
	case *ast.HTMLBlock:
		e.add(cdm.RawHTML, strings.TrimRight(rawLines(node, e.src), "\n"), 0, nil)
	case *east.Table:
		e.table(node)
	default:
		e.children(n, ctx)
	}
}

func (e *mdEmitter) textBlock(n ast.Node, ctx mdContext) {
	raw := linesText(n, e.src)
	if strings.TrimSpace(raw) == "" {
		return
	}
	e.collectLinks(n)

	if img, ok := soleImage(n); ok {
		e.add(cdm.Image, "", 0, map[string]string{"src": e.resolve(string(img.Destination)), "alt": plainText(img, e.src)})
		return
	}
	if trimmed := strings.TrimSpace(raw); len(trimmed) >= 4 && strings.HasPrefix(trimmed, "$$") && strings.HasSuffix(trimmed, "$$") {
		e.add(cdm.MathBlock, raw, 0, nil)
		return
	}
	switch {
	case ctx.listDepth > 0:
		meta := map[string]string{"depth": strconv.Itoa(ctx.listDepth)}
		if ctx.ordered {
			meta["ordered"] = "true"
		}
		e.add(cdm.ListItem, raw, 0, meta)
	case ctx.inQuote:
		e.add(cdm.Blockquote, raw, 0, nil)
	default:
		e.add(cdm.Paragraph, raw, 0, nil)
	}
}

func (e *mdEmitter) table(t *east.Table) {
	row := 0
	for r := t.FirstChild(); r != nil; r = r.NextSibling() {
		_, header := r.(*east.TableHeader)
		col := 0
		for c := r.FirstChild(); c != nil; c = c.NextSibling() {
			meta := map[string]string{"row": strconv.Itoa(row), "col": strconv.Itoa(col)}
			if header {
				meta["header"] = "true"
			}
			e.collectLinks(c)
			e.add(cdm.TableCell, linesText(c, e.src), 0, meta)
			col++
		}
		row++
	}
}

// collectLinks запоминает относительные ссылки и картинки — включая
// встроенные в текст абзаца. По ним импорт репозитория определяет порядок
// глав, ищет отсутствующие главы и ресурсы (ТЗ §4.3).
func (e *mdEmitter) collectLinks(n ast.Node) {
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := c.(type) {
		case *ast.Link:
			if dest := string(node.Destination); isRelative(dest) {
				if target := e.resolve(dest); target != "" {
					e.refs.links = append(e.refs.links, target)
				}
			}
		case *ast.Image:
			if dest := string(node.Destination); isRelative(dest) {
				if target := e.resolve(dest); target != "" {
					e.refs.images = append(e.refs.images, target)
				}
			}
		}
		return ast.WalkContinue, nil
	})
}

var schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

func isRelative(dest string) bool {
	return dest != "" && !schemeRe.MatchString(dest) && !strings.HasPrefix(dest, "/") &&
		!strings.HasPrefix(dest, "#") && !strings.HasPrefix(dest, "//")
}

// resolve переводит относительный путь из файла в путь от корня репозитория;
// внешние адреса и якоря возвращаются как есть (без фрагмента).
func (e *mdEmitter) resolve(dest string) string {
	if !isRelative(dest) {
		return dest
	}
	if i := strings.IndexAny(dest, "#?"); i >= 0 {
		dest = dest[:i]
	}
	if dest == "" {
		return ""
	}
	return path.Join(e.baseDir, dest)
}

func soleImage(n ast.Node) (*ast.Image, bool) {
	if n.ChildCount() != 1 {
		return nil, false
	}
	img, ok := n.FirstChild().(*ast.Image)
	return img, ok
}

// linesText — исходный текст листового блока: строки без концевых переводов
// строки, соединённые "\n".
func linesText(n ast.Node, src []byte) string {
	lines := n.Lines()
	parts := make([]string, 0, lines.Len())
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		parts = append(parts, strings.TrimRight(string(seg.Value(src)), "\n"))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// rawLines — исходные строки как есть (для кода важны отступы).
func rawLines(n ast.Node, src []byte) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// plainText — текст без разметки, для оглавления и alt.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch t := c.(type) {
			case *ast.Text:
				b.Write(t.Segment.Value(src))
				if t.SoftLineBreak() || t.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(t.Value)
			case *ast.RawHTML:
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
