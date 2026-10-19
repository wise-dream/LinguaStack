package httpapi_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
)

type bookBody struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	SourceFormat     string `json:"source_format"`
	OriginalFilename string `json:"original_filename"`
	Chapters         int    `json:"chapters"`
	Blocks           int    `json:"blocks"`
	Translatable     int    `json:"translatable"`
	Issues           []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
	} `json:"issues"`
}

type tocBody struct {
	Chapters []struct {
		Index    int    `json:"index"`
		Title    string `json:"title"`
		Children []struct {
			Title string `json:"title"`
		} `json:"children"`
	} `json:"chapters"`
}

func uploadBook(t *testing.T, c *apiClient, name, content string) bookBody {
	t.Helper()
	r := c.upload(name, []byte(content))
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	var b bookBody
	r.JSON(t, &b)
	return b
}

func tocTitles(t *testing.T, c *apiClient, id string) []string {
	t.Helper()
	r := c.do(http.MethodGet, "/api/books/"+id+"/toc", nil)
	require.Equal(t, http.StatusOK, r.Status)
	var toc tocBody
	r.JSON(t, &toc)
	var titles []string
	for _, ch := range toc.Chapters {
		titles = append(titles, ch.Title)
	}
	return titles
}

func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return err
	}))
	return out
}

func TestUploadMarkdownCreatesBookWithTOC(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "book.md", "# Глава\n\nТекст.\n\n## Раздел\n\nЕщё.")
	require.Equal(t, "Глава", b.Title)
	require.Equal(t, "markdown", b.SourceFormat)
	require.Equal(t, 2, b.Chapters)
	require.Equal(t, 4, b.Translatable)

	r := c.do(http.MethodGet, "/api/books/"+b.ID+"/toc", nil)
	var toc tocBody
	r.JSON(t, &toc)
	require.Equal(t, "Глава", toc.Chapters[0].Title)
	require.Equal(t, "Раздел", toc.Chapters[0].Children[0].Title)
}

func TestUploadRequiresAuthentication(t *testing.T) {
	r := newTestEnv(t).client(t).upload("book.md", []byte("# x"))
	require.Equal(t, http.StatusUnauthorized, r.Status)
}

func TestLibraryListsOnlyOwnBooks(t *testing.T) {
	env := newTestEnv(t)
	mine, theirs := env.signedIn(t, "me@example.com"), env.signedIn(t, "them@example.com")
	uploadBook(t, mine, "a.md", "# Моя\n\nx.")
	uploadBook(t, theirs, "b.md", "# Чужая\n\ny.")

	var list []bookBody
	mine.do(http.MethodGet, "/api/books", nil).JSON(t, &list)
	require.Len(t, list, 1, "ТЗ §2.2: видны только свои книги")
	require.Equal(t, "Моя", list[0].Title)
}

func TestLibraryDoesNotShipDocumentBodies(t *testing.T) {
	// Библиотека не должна гонять по сети целые книги.
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	uploadBook(t, c, "a.md", "# Глава\n\nУникальный текст абзаца.")
	r := c.do(http.MethodGet, "/api/books", nil)
	require.NotContains(t, string(r.Body), "Уникальный текст абзаца")
}

func TestForeignBookIs404(t *testing.T) {
	env := newTestEnv(t)
	owner, intruder := env.signedIn(t, "owner@example.com"), env.signedIn(t, "intruder@example.com")
	b := uploadBook(t, owner, "a.md", "# A\n\nx.\n\n# B\n\ny.")

	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/books/" + b.ID, nil},
		{http.MethodGet, "/api/books/" + b.ID + "/toc", nil},
		{http.MethodPut, "/api/books/" + b.ID + "/toc", map[string][]int{"order": {1, 0}}},
		{http.MethodDelete, "/api/books/" + b.ID, nil},
	} {
		require.Equal(t, http.StatusNotFound, intruder.do(req.method, req.path, req.body).Status, "%s %s", req.method, req.path)
	}
	// Книга владельца не пострадала.
	require.Equal(t, []string{"A", "B"}, tocTitles(t, owner, b.ID))
}

func TestMalformedBookIDIs404(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/books/not-a-uuid", nil).Status)
}

func TestImportIssuesReturnedOnUpload(t *testing.T) {
	// ТЗ §3.1: о проблемах сообщается до начала перевода.
	c := newTestEnv(t).signedIn(t, "u@example.com")
	b := uploadBook(t, c, "empty.txt", "")
	require.NotEmpty(t, b.Issues)
	require.Equal(t, "empty_document", b.Issues[0].Code)
	require.Equal(t, "error", b.Issues[0].Severity)
}

func TestOversizedUploadIs413(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	big := strings.Repeat("слово ", int(env.cfg.MaxUploadBytes)/6+1000)
	require.Equal(t, http.StatusRequestEntityTooLarge, c.upload("big.txt", []byte(big)).Status)
	require.Empty(t, filesUnder(t, env.storage), "ничего не сохранено")
}

func TestUnsupportedFormatIs415(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	require.Equal(t, http.StatusUnsupportedMediaType, c.upload("tool.exe", []byte("MZ\x90\x00")).Status)
	require.Equal(t, http.StatusUnsupportedMediaType, c.upload("noext", []byte("text")).Status)
}

func TestUploadWithoutFileFieldIs422(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	r := c.do(http.MethodPost, "/api/books", []byte("--x\r\n\r\n--x--\r\n"),
		[2]string{"Content-Type", "multipart/form-data; boundary=x"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

func TestZipSlipUploadIs400AndNothingSaved(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	r := c.upload("repo.zip", zipBytes(t, map[string]string{"ok.md": "# Ok", "../../etc/passwd": "root"}))
	require.Equal(t, http.StatusBadRequest, r.Status)

	var count int64
	require.NoError(t, env.db.Model(&models.Book{}).Count(&count).Error)
	require.Zero(t, count)
	require.Empty(t, filesUnder(t, env.storage))
}

func TestUploadSavesArchiveAssets(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "repo.zip", string(zipBytes(t, map[string]string{
		"ch.md":     "# Глава\n\n![схема](img/d.png)\n",
		"img/d.png": "\x89PNG\r\n\x1a\n",
	})))
	stored := filesUnder(t, env.storage)
	require.Contains(t, strings.Join(stored, "\n"), filepath.Join(b.ID, "assets", "img", "d.png"))
}

func TestUploadZipWithHTMLPageIsHTMLBook(t *testing.T) {
	// Формат книги — тот, что определил импорт по содержимому архива: от
	// него зависит, как экспорт понимает переносы строк (дополнение §16).
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "page.zip", string(zipBytes(t, map[string]string{
		"page.html":        `<h1>Глава</h1><p>Текст.</p><p><img src="page_files/a.png"></p>`,
		"page_files/a.png": "\x89PNG\r\n\x1a\n",
	})))
	require.Equal(t, "html", b.SourceFormat)
	require.Contains(t, strings.Join(filesUnder(t, env.storage), "\n"), filepath.Join(b.ID, "assets", "page_files", "a.png"))
}

func TestUploadedFilenameNotUsedOnDisk(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "../../evil name.md", "# Книга\n\nx.")
	require.Equal(t, "evil name.md", b.OriginalFilename, "в метаданных — только базовое имя")
	for _, f := range filesUnder(t, env.storage) {
		require.NotContains(t, f, "evil")
	}
}

func TestDeleteRemovesFilesAndRow(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "a.md", "# A\n\nx.")
	require.NotEmpty(t, filesUnder(t, env.storage))

	require.Equal(t, http.StatusNoContent, c.do(http.MethodDelete, "/api/books/"+b.ID, nil).Status)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/books/"+b.ID, nil).Status)
	require.Empty(t, filesUnder(t, env.storage), "ТЗ §13: при удалении книги удаляются и файлы")
}

func TestSameFileTwiceYieldsIndependentBooks(t *testing.T) {
	// Review Focus №4: повторный импорт не конфликтует в хранилище.
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	first := uploadBook(t, c, "a.md", "# A\n\nx.")
	second := uploadBook(t, c, "a.md", "# A\n\nx.")
	require.NotEqual(t, first.ID, second.ID)

	require.Equal(t, http.StatusNoContent, c.do(http.MethodDelete, "/api/books/"+first.ID, nil).Status)
	require.Equal(t, []string{"A"}, tocTitles(t, c, second.ID), "удаление одной копии не задевает другую")
}

func TestManualTOCReorderPersists(t *testing.T) {
	// ТЗ §4.3: пользователь вручную исправляет порядок глав.
	c := newTestEnv(t).signedIn(t, "u@example.com")
	b := uploadBook(t, c, "a.md", "# A\n\nx.\n\n# B\n\ny.\n\n# C\n\nz.")
	r := c.do(http.MethodPut, "/api/books/"+b.ID+"/toc", map[string][]int{"order": {2, 0, 1}})
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, []string{"C", "A", "B"}, tocTitles(t, c, b.ID))
}

func TestTOCReorderKeepsNodeIDs(t *testing.T) {
	// Идентификаторы блоков не должны зависеть от порядка глав — на них
	// ссылаются сегменты перевода.
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	b := uploadBook(t, c, "a.md", "# A\n\nx.\n\n# B\n\ny.")

	var before, after models.Book
	require.NoError(t, env.db.Where("id = ?", b.ID).First(&before).Error)
	c.do(http.MethodPut, "/api/books/"+b.ID+"/toc", map[string][]int{"order": {1, 0}})
	require.NoError(t, env.db.Where("id = ?", b.ID).First(&after).Error)

	ids := func(book models.Book) map[string]string {
		out := map[string]string{}
		for _, blk := range book.CDM.Data.Blocks() {
			out[blk.NodeID] = blk.Text
		}
		return out
	}
	require.Equal(t, ids(before), ids(after))
}

func TestTOCReorderRejectsInvalidPermutation(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	b := uploadBook(t, c, "a.md", "# A\n\nx.\n\n# B\n\ny.")
	for _, order := range [][]int{{0}, {0, 0}, {0, 2}, {-1, 0}, {1, 0, 2}, {}} {
		r := c.do(http.MethodPut, "/api/books/"+b.ID+"/toc", map[string][]int{"order": order})
		require.Equal(t, http.StatusUnprocessableEntity, r.Status, "порядок %v", order)
	}
	require.Equal(t, []string{"A", "B"}, tocTitles(t, c, b.ID))
}

func TestOriginalFileUnchangedOnDisk(t *testing.T) {
	// ТЗ §4.1: оригинал хранится неизменным.
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	content := "\ufeff# Книга\r\n\r\nТекст с BOM и CRLF."
	b := uploadBook(t, c, "a.md", content)
	c.do(http.MethodPut, "/api/books/"+b.ID+"/toc", map[string][]int{"order": {0}})

	var stored []byte
	for _, f := range filesUnder(t, env.storage) {
		if strings.HasSuffix(f, "original.md") {
			var err error
			stored, err = os.ReadFile(filepath.Join(env.storage, f))
			require.NoError(t, err)
		}
	}
	require.Equal(t, content, string(stored), "байт в байт, включая BOM и CRLF")
}

type chapterBody struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Blocks []struct {
		NodeID string            `json:"node_id"`
		Type   string            `json:"type"`
		Text   string            `json:"text"`
		Meta   map[string]string `json:"meta"`
	} `json:"blocks"`
}

func getChapter(t *testing.T, c *apiClient, bookID string, index int) chapterBody {
	t.Helper()
	r := c.do(http.MethodGet, fmt.Sprintf("/api/books/%s/chapters/%d", bookID, index), nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var ch chapterBody
	r.JSON(t, &ch)
	return ch
}

func chapterTexts(ch chapterBody) []string {
	var out []string
	for _, b := range ch.Blocks {
		out = append(out, b.Text)
	}
	return out
}

func TestChapterBlocks(t *testing.T) {
	// Читалка грузит книгу по главам (ТЗ §8.3); вложенная глава находится
	// по index, а блоки родителя не включают блоки детей.
	env := newTestEnv(t)
	c := env.signedIn(t, "r@example.com")
	b := uploadBook(t, c, "book.md", "# A\n\nOne.\n\n## A1\n\nNested.\n\n# B\n\nTwo.")
	var toc struct {
		Chapters []struct {
			Index    int `json:"index"`
			Children []struct {
				Index int `json:"index"`
			} `json:"children"`
		} `json:"chapters"`
	}
	c.do(http.MethodGet, "/api/books/"+b.ID+"/toc", nil).JSON(t, &toc)

	parent := getChapter(t, c, b.ID, toc.Chapters[0].Index)
	require.Equal(t, "A", parent.Title)
	require.Contains(t, chapterTexts(parent), "One.")
	require.NotContains(t, chapterTexts(parent), "Nested.")

	nested := getChapter(t, c, b.ID, toc.Chapters[0].Children[0].Index)
	require.Equal(t, "A1", nested.Title)
	require.Contains(t, chapterTexts(nested), "Nested.")
	require.NotEmpty(t, nested.Blocks[0].NodeID)
}

func TestChapterMissing404(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "r@example.com")
	b := uploadBook(t, c, "book.md", "# A\n\nOne.")
	for _, index := range []string{"999", "-1", "abc"} {
		require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/books/"+b.ID+"/chapters/"+index, nil).Status, index)
	}
}

func TestEPUBImageServedByMetaSrc(t *testing.T) {
	// Адрес картинки клиент строит из meta.src блока image: он должен
	// совпадать с именем сохранённого ресурса.
	env := newTestEnv(t)
	c := env.signedIn(t, "r@example.com")
	raw, err := os.ReadFile("../importers/testdata/real/alice.epub")
	require.NoError(t, err)
	b := uploadBook(t, c, "alice.epub", string(raw))
	src := ""
	for i := 0; i < b.Chapters && src == ""; i++ {
		r := c.do(http.MethodGet, fmt.Sprintf("/api/books/%s/chapters/%d", b.ID, i), nil)
		if r.Status != http.StatusOK {
			continue
		}
		var ch chapterBody
		r.JSON(t, &ch)
		for _, blk := range ch.Blocks {
			if blk.Type == "image" {
				src = blk.Meta["src"]
				break
			}
		}
	}
	require.NotEmpty(t, src, "в «Алисе» есть обложка")
	r := c.do(http.MethodGet, "/api/books/"+b.ID+"/assets/"+src, nil)
	require.Equal(t, http.StatusOK, r.Status, "src=%s", src)
	require.Equal(t, "image/jpeg", r.Header.Get("Content-Type"))
	require.True(t, bytes.HasPrefix(r.Body, []byte{0xFF, 0xD8}), "это JPEG")
	require.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
	require.Equal(t, "private, max-age=86400", r.Header.Get("Cache-Control"))
}

func TestSVGAssetIsSandboxed(t *testing.T) {
	// SVG может нести скрипт: отданный с нашего origin, он выполнился бы с
	// сессией пользователя. CSP sandbox запрещает скрипты.
	env := newTestEnv(t)
	c := env.signedIn(t, "r@example.com")
	b := uploadBook(t, c, "repo.zip", string(zipBytes(t, map[string]string{
		"ch.md":     "# Глава\n\n![схема](img/a.svg)\n",
		"img/a.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.cookie)</script></svg>`,
	})))
	r := c.do(http.MethodGet, "/api/books/"+b.ID+"/assets/img/a.svg", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.Equal(t, "image/svg+xml", r.Header.Get("Content-Type"))
	csp := r.Header.Get("Content-Security-Policy")
	require.Contains(t, csp, "sandbox")
	require.Contains(t, csp, "default-src 'none'")
	require.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
}

func TestAssetPathTraversal404(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "r@example.com")
	b := uploadBook(t, c, "repo.zip", string(zipBytes(t, map[string]string{
		"ch.md":     "# Глава\n\n![схема](img/d.png)\n",
		"img/d.png": "\x89PNG\r\n\x1a\n",
	})))
	var bookDir string
	for _, f := range filesUnder(t, env.storage) {
		if strings.Contains(f, b.ID) && strings.Contains(f, "original") {
			bookDir = filepath.Join(env.storage, filepath.Dir(f))
		}
	}
	require.NotEmpty(t, bookDir)
	// Подложенные напрямую файлы: тип не из списка и ссылка на оригинал.
	require.NoError(t, os.WriteFile(filepath.Join(bookDir, "assets", "evil.html"), []byte("<script>x</script>"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(bookDir, "original.zip"), filepath.Join(bookDir, "assets", "link.png")))

	for _, p := range []string{"../original.zip", "%2e%2e/original.zip", "img/..%2f..%2foriginal.zip", "evil.html", "link.png", "nope.png"} {
		r := c.do(http.MethodGet, "/api/books/"+b.ID+"/assets/"+p, nil)
		require.Equal(t, http.StatusNotFound, r.Status, "%s: %s", p, r.Body)
	}
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/books/"+b.ID+"/assets/img/d.png", nil).Status)
}
