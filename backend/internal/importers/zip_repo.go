package importers

import (
	"archive/zip"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"

	"github.com/vnovels/linguastack/internal/cdm"
)

// ZipRepo — импорт ZIP-архива с набором Markdown-файлов (ТЗ §4.3).
//
// Порядок глав определяется по явной структуре проекта, а не только по
// именам файлов: SUMMARY.md (mdBook, GitBook) → nav из mkdocs.yml →
// ссылки из корневого README → естественная сортировка путей. Откуда взят
// порядок, пользователь видит в замечании order_source и может исправить
// его вручную в оглавлении.
type ZipRepo struct {
	limits ArchiveLimits
}

// NewZipRepo создаёт импортёр ZIP-репозиториев.
func NewZipRepo(limits ArchiveLimits) Importer { return ZipRepo{limits: limits} }

func (ZipRepo) FormatID() string { return "zip_repo" }

func (ZipRepo) Sniff(_ []byte, filename string) bool { return fileExt(filename) == ".zip" }

const listLimit = 20 // сколько имён перечислять в одном замечании

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".bmp": true, ".avif": true,
}

// tocEntry — элемент плана книги. path == "" — раздел без собственного файла.
type tocEntry struct {
	path  string
	label string
	depth int
}

type repo struct {
	files   map[string]*zip.File
	mdFiles []string
	issues  []Issue
	cache   map[string][]byte
}

func (z ZipRepo) Load(data []byte, filename string) (Result, error) {
	entries, corrupt, err := openArchive(data, z.limits)
	if err != nil {
		return Result{}, err
	}
	if corrupt {
		meta := cdm.BookMeta{Title: titleFromFilename(filename), SourceFormat: "zip_repo"}
		return finish(cdm.NewBuilder(meta).Build(), []Issue{corruptIssue("ZIP")}), nil
	}

	r := &repo{files: map[string]*zip.File{}, cache: map[string][]byte{}}
	var duplicates []string
	for _, e := range entries {
		if isJunk(e.name) {
			continue
		}
		if _, seen := r.files[e.name]; seen {
			duplicates = append(duplicates, e.name)
			continue
		}
		r.files[e.name] = e.file
		if ext := fileExt(e.name); ext == ".md" || ext == ".markdown" {
			r.mdFiles = append(r.mdFiles, e.name)
		}
	}
	slices.SortFunc(r.mdFiles, naturalCompare)
	if len(duplicates) > 0 {
		r.issues = append(r.issues, Issue{Severity: Warning, Code: "duplicate_entry",
			Message: "В архиве повторяются имена; использована первая запись: " + listNames(duplicates)})
	}

	plan, err := r.plan()
	if err != nil {
		return Result{}, err
	}
	title, err := r.bookTitle(filename)
	if err != nil {
		return Result{}, err
	}

	builder := cdm.NewBuilder(cdm.BookMeta{Title: title, SourceFormat: "zip_repo"})
	var images, links []string
	for _, entry := range plan {
		if entry.path == "" {
			builder.StartChapter(entry.label, entry.depth)
			continue
		}
		src, ok, err := r.read(entry.path)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		label := entry.label
		if label == "" {
			label = titleFromFilename(entry.path)
		}
		refs := parseMarkdown(src).emit(builder, label, path.Dir(entry.path), entry.depth)
		images = append(images, refs.images...)
		links = append(links, refs.links...)
	}

	result := finish(builder.Build(), r.issues)
	result.Assets, result.Issues = r.resources(images, links, result.Issues)
	return result, nil
}

// read читает текстовый файл архива. Нарушение лимитов — отказ (error);
// повреждённая запись — замечание, файл пропускается.
func (r *repo) read(name string) ([]byte, bool, error) {
	if data, ok := r.cache[name]; ok {
		return data, true, nil
	}
	f, ok := r.files[name]
	if !ok {
		return nil, false, nil
	}
	data, err := readEntry(f)
	if errors.Is(err, ErrArchiveLimits) {
		return nil, false, err
	}
	if err != nil {
		r.issues = append(r.issues, Issue{Severity: Warning, Code: "corrupt_entry",
			Message: fmt.Sprintf("Не удалось прочитать %q из архива; файл пропущен.", name)})
		return nil, false, nil
	}
	text, issues := sanitizeUTF8(string(data))
	r.issues = append(r.issues, issues...)
	text, _ = splitFrontMatter(normalizeNewlines(strings.TrimPrefix(text, "\ufeff")))
	r.cache[name] = []byte(text)
	return r.cache[name], true, nil
}

// plan строит порядок глав по первому найденному источнику.
func (r *repo) plan() ([]tocEntry, error) {
	sources := []func() ([]tocEntry, string, bool, error){r.fromSummary, r.fromMkdocs, r.fromReadme}
	for _, source := range sources {
		entries, label, strict, err := source()
		if err != nil {
			return nil, err
		}
		if entries == nil {
			continue
		}
		r.issues = append(r.issues, Issue{Severity: Info, Code: "order_source", Message: "Порядок глав взят из " + label + "."})
		return r.complete(entries, strict), nil
	}

	var plan []tocEntry
	numbered := false
	for _, name := range readmeFirst(r.mdFiles) {
		plan = append(plan, tocEntry{path: name, depth: 1})
		if base := path.Base(name); base != "" && unicode.IsDigit(rune(base[0])) {
			numbered = true
		}
	}
	message := "Порядок глав определён по алфавиту; проверьте его в оглавлении."
	if numbered {
		message = "Порядок глав определён по номерам в именах файлов."
	}
	if len(plan) > 0 {
		r.issues = append(r.issues, Issue{Severity: Info, Code: "order_source", Message: message})
	}
	return plan, nil
}

// complete отмечает отсутствующие главы и дописывает файлы, которых нет в
// оглавлении: текст пользователя не должен пропадать молча. Для строгих
// оглавлений (SUMMARY.md, mkdocs.yml) это предупреждение, для README —
// ожидаемое поведение.
func (r *repo) complete(entries []tocEntry, strict bool) []tocEntry {
	seen := map[string]bool{}
	var plan []tocEntry
	var missing []string
	for _, e := range entries {
		if e.path == "" {
			plan = append(plan, e)
			continue
		}
		if seen[e.path] {
			continue
		}
		seen[e.path] = true
		if _, ok := r.files[e.path]; !ok {
			missing = append(missing, e.path)
			continue
		}
		plan = append(plan, e)
	}
	if len(missing) > 0 {
		r.issues = append(r.issues, Issue{Severity: Warning, Code: "missing_chapter",
			Message: "В оглавлении указаны отсутствующие файлы: " + listNames(missing)})
	}

	var unlisted []string
	for _, name := range r.mdFiles {
		if !seen[name] && !isTOCFile(name) {
			unlisted = append(unlisted, name)
			plan = append(plan, tocEntry{path: name, depth: 1})
		}
	}
	if strict && len(unlisted) > 0 {
		r.issues = append(r.issues, Issue{Severity: Warning, Code: "unlisted_file",
			Message: "Файлы не упомянуты в оглавлении и добавлены в конец: " + listNames(unlisted)})
	}
	return plan
}

func isTOCFile(name string) bool {
	return strings.EqualFold(path.Base(name), "SUMMARY.md")
}

// --- источники порядка ---

func (r *repo) fromSummary() ([]tocEntry, string, bool, error) {
	var summary string
	for _, name := range r.mdFiles {
		if isTOCFile(name) && (summary == "" || strings.Count(name, "/") < strings.Count(summary, "/")) {
			summary = name
		}
	}
	if summary == "" {
		return nil, "", false, nil
	}
	src, ok, err := r.read(summary)
	if err != nil || !ok {
		return nil, "", false, err
	}
	doc := parseMarkdown(src)
	var entries []tocEntry
	_ = ast.Walk(doc.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		link, ok := n.(*ast.Link)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}
		dest := string(link.Destination)
		if !isRelative(dest) {
			return ast.WalkContinue, nil // «[Черновик]()» в mdBook и внешние ссылки
		}
		target := resolveIn(path.Dir(summary), dest)
		if ext := fileExt(target); ext != ".md" && ext != ".markdown" {
			return ast.WalkContinue, nil
		}
		entries = append(entries, tocEntry{path: target, label: plainText(link, src), depth: max(listDepth(link), 1)})
		return ast.WalkSkipChildren, nil
	})
	if len(entries) == 0 {
		return nil, "", false, nil
	}
	return entries, summary, true, nil
}

func listDepth(n ast.Node) int {
	depth := 0
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.List); ok {
			depth++
		}
	}
	return depth
}

var (
	mkdocsNavRe     = regexp.MustCompile(`(?m)^nav:[ \t]*$`)
	mkdocsItemRe    = regexp.MustCompile(`^([ \t]*)-[ \t]*(.*?)[ \t]*$`)
	mkdocsPairRe    = regexp.MustCompile(`^['"]?(.+?)['"]?[ \t]*:[ \t]*['"]?([^'"]*?)['"]?$`)
	mkdocsDocsDirRe = regexp.MustCompile(`(?m)^docs_dir:[ \t]*['"]?([^'"\n]+?)['"]?[ \t]*$`)
	mkdocsSiteRe    = regexp.MustCompile(`(?m)^site_name:[ \t]*['"]?([^'"\n]+?)['"]?[ \t]*$`)
	bookTomlTitleRe = regexp.MustCompile(`(?m)^title[ \t]*=[ \t]*"([^"\n]+)"`)
)

// fromMkdocs читает nav из mkdocs.yml. Полный YAML не нужен: nav — список
// пар «подпись: путь» и разделов с отступами.
func (r *repo) fromMkdocs() ([]tocEntry, string, bool, error) {
	raw, ok, err := r.readRaw("mkdocs.yml")
	if err != nil || !ok {
		return nil, "", false, err
	}
	docsDir := "docs"
	if m := mkdocsDocsDirRe.FindStringSubmatch(raw); m != nil {
		docsDir = strings.Trim(m[1], "/")
	}
	loc := mkdocsNavRe.FindStringIndex(raw)
	if loc == nil {
		return nil, "", false, nil
	}

	var entries []tocEntry
	var indents []int
	for _, line := range strings.Split(raw[loc[1]:], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break // следующий ключ верхнего уровня
		}
		m := mkdocsItemRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		for len(indents) > 0 && indent < indents[len(indents)-1] {
			indents = indents[:len(indents)-1]
		}
		if len(indents) == 0 || indent > indents[len(indents)-1] {
			indents = append(indents, indent)
		}
		depth := len(indents)

		label, value := "", strings.Trim(m[2], `'"`)
		if pair := mkdocsPairRe.FindStringSubmatch(m[2]); pair != nil && !strings.HasPrefix(m[2], "http") {
			label, value = pair[1], pair[2]
		}
		switch {
		case value == "":
			entries = append(entries, tocEntry{label: label, depth: depth}) // раздел без файла
		case strings.HasSuffix(value, ".md") && isRelative(value):
			entries = append(entries, tocEntry{path: path.Join(docsDir, value), label: label, depth: depth})
		}
	}
	if len(entries) == 0 {
		return nil, "", false, nil
	}
	return entries, "mkdocs.yml (раздел nav)", true, nil
}

// fromReadme — корневой README как оглавление: сам README становится
// вводной главой, за ним файлы в порядке ссылок.
func (r *repo) fromReadme() ([]tocEntry, string, bool, error) {
	readme := r.rootReadme()
	if readme == "" {
		return nil, "", false, nil
	}
	src, ok, err := r.read(readme)
	if err != nil || !ok {
		return nil, "", false, err
	}
	entries := []tocEntry{{path: readme, depth: 1}}
	doc := parseMarkdown(src)
	_ = ast.Walk(doc.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := n.(*ast.Link); ok && entering && isRelative(string(link.Destination)) {
			if target := resolveIn(path.Dir(readme), string(link.Destination)); fileExt(target) == ".md" {
				entries = append(entries, tocEntry{path: target, depth: 1})
			}
		}
		return ast.WalkContinue, nil
	})
	if len(entries) == 1 {
		return nil, "", false, nil
	}
	return entries, "ссылок в " + readme, false, nil
}

func (r *repo) rootReadme() string {
	for _, name := range r.mdFiles {
		if !strings.Contains(name, "/") {
			base := strings.ToLower(name)
			if base == "readme.md" || base == "index.md" {
				return name
			}
		}
	}
	return ""
}

func (r *repo) readRaw(name string) (string, bool, error) {
	f, ok := r.files[name]
	if !ok {
		return "", false, nil
	}
	data, err := readEntry(f)
	if err != nil {
		if errors.Is(err, ErrArchiveLimits) {
			return "", false, err
		}
		return "", false, nil
	}
	text, _ := sanitizeUTF8(string(data))
	return text, true, nil
}

// bookTitle: book.toml (mdBook) → site_name (mkdocs) → заголовок README → имя архива.
func (r *repo) bookTitle(filename string) (string, error) {
	for _, candidate := range []struct {
		file string
		re   *regexp.Regexp
	}{{"book.toml", bookTomlTitleRe}, {"mkdocs.yml", mkdocsSiteRe}} {
		raw, ok, err := r.readRaw(candidate.file)
		if err != nil {
			return "", err
		}
		if m := candidate.re.FindStringSubmatch(raw); ok && m != nil {
			return strings.TrimSpace(m[1]), nil
		}
	}
	if readme := r.rootReadme(); readme != "" {
		src, ok, err := r.read(readme)
		if err != nil {
			return "", err
		}
		if title := parseMarkdown(src).firstHeading(); ok && title != "" {
			return title, nil
		}
	}
	return titleFromFilename(filename), nil
}

// resources собирает используемые картинки как ресурсы книги и отмечает
// отсутствующие ресурсы и битые ссылки на главы (ТЗ §4.3).
func (r *repo) resources(images, links []string, issues []Issue) ([]Asset, []Issue) {
	var assets []Asset
	var missing []string
	seen := map[string]bool{}
	for _, name := range images {
		if seen[name] {
			continue
		}
		seen[name] = true
		f, ok := r.files[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !imageExtensions[fileExt(name)] {
			continue
		}
		size, err := entrySize(f)
		if err != nil {
			continue // уже отсечено в openArchive; повторная проверка дешевле допущения
		}
		assets = append(assets, Asset{Name: name, Size: size, Open: func() ([]byte, error) { return readEntry(f) }})
	}
	if len(missing) > 0 {
		issues = append(issues, Issue{Severity: Warning, Code: "missing_resource",
			Message: "Не найдены изображения: " + listNames(missing)})
	}

	var broken []string
	seenLinks := map[string]bool{}
	for _, name := range links {
		if seenLinks[name] || fileExt(name) != ".md" {
			continue
		}
		seenLinks[name] = true
		if _, ok := r.files[name]; !ok {
			broken = append(broken, name)
		}
	}
	if len(broken) > 0 {
		issues = append(issues, Issue{Severity: Warning, Code: "broken_link",
			Message: "Ссылки ведут на отсутствующие файлы: " + listNames(broken)})
	}
	return assets, issues
}

// --- помощники ---

func resolveIn(dir, dest string) string {
	if i := strings.IndexAny(dest, "#?"); i >= 0 {
		dest = dest[:i]
	}
	return path.Join(dir, dest)
}

func readmeFirst(names []string) []string {
	out := slices.Clone(names)
	slices.SortStableFunc(out, func(a, b string) int {
		ra, rb := isRootReadme(a), isRootReadme(b)
		switch {
		case ra && !rb:
			return -1
		case rb && !ra:
			return 1
		}
		return 0
	})
	return out
}

func isRootReadme(name string) bool {
	lower := strings.ToLower(name)
	return lower == "readme.md" || lower == "index.md"
}

func listNames(names []string) string {
	if len(names) <= listLimit {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:listLimit], ", ") + fmt.Sprintf(" и ещё %d", len(names)-listLimit)
}

// naturalCompare сравнивает пути «по-человечески»: числа внутри имён
// сравниваются как числа, поэтому «10-…» идёт после «2-…».
func naturalCompare(a, b string) int {
	ca, cb := chunks(a), chunks(b)
	for i := 0; i < len(ca) && i < len(cb); i++ {
		x, y := ca[i], cb[i]
		xDigit, yDigit := unicode.IsDigit(rune(x[0])), unicode.IsDigit(rune(y[0]))
		if xDigit && yDigit {
			nx, _ := strconv.ParseUint(x, 10, 64)
			ny, _ := strconv.ParseUint(y, 10, 64)
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
			continue
		}
		if c := strings.Compare(strings.ToLower(x), strings.ToLower(y)); c != 0 {
			return c
		}
	}
	return len(ca) - len(cb)
}

func chunks(s string) []string {
	var out []string
	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || isASCIIDigit(s[i]) != isASCIIDigit(s[i-1]) {
			out = append(out, s[start:i])
			start = i
		}
	}
	return out
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }
