package importers_test

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io/fs"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/storage"
)

type zipFile struct {
	name string
	body string
	mode fs.FileMode
}

func makeZip(t *testing.T, files ...zipFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		if f.mode != 0 {
			hdr.SetMode(f.mode)
		}
		w, err := zw.CreateHeader(hdr)
		require.NoError(t, err)
		_, err = w.Write([]byte(f.body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func files(pairs ...string) []zipFile {
	out := make([]zipFile, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, zipFile{name: pairs[i], body: pairs[i+1]})
	}
	return out
}

var testLimits = importers.ArchiveLimits{MaxEntries: 2000, MaxTotalBytes: 200 << 20, MaxRatio: 100}

func loadZip(t *testing.T, data []byte) importers.Result {
	t.Helper()
	res, err := importers.NewZipRepo(testLimits).Load(data, "repo.zip")
	require.NoError(t, err)
	require.NoError(t, res.Document.Validate())
	return res
}

func chapterTitles(chs []cdm.Chapter) []string {
	out := make([]string, len(chs))
	for i, ch := range chs {
		out[i] = ch.Title
	}
	return out
}

func issueWith(issues []importers.Issue, code string) *importers.Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

func TestZipSummaryDeterminesOrder(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"SUMMARY.md", "- [Intro](zzz.md)\n- [Body](aaa.md)\n",
		"zzz.md", "# Intro\n\nFirst.",
		"aaa.md", "# Body\n\nSecond.",
	)...))
	// Алфавит дал бы aaa, zzz — явная структура проекта обязана победить (ТЗ §4.3).
	require.Equal(t, []string{"Intro", "Body"}, chapterTitles(res.Document.Chapters))
	src := issueWith(res.Issues, "order_source")
	require.NotNil(t, src)
	require.Contains(t, src.Message, "SUMMARY.md")
}

func TestZipSummaryLabelTitlesFileWithoutHeading(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"SUMMARY.md", "- [Введение](intro.md)\n",
		"intro.md", "Текст без заголовка.",
	)...))
	require.Equal(t, []string{"Введение"}, chapterTitles(res.Document.Chapters))
}

func TestZipSummaryNestingBecomesChapterNesting(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"SUMMARY.md", "- [Глава 1](ch1.md)\n  - [Раздел 1.1](ch1/s1.md)\n- [Глава 2](ch2.md)\n",
		"ch1.md", "# Глава 1\n\nа.",
		"ch1/s1.md", "# Раздел 1.1\n\nб.",
		"ch2.md", "# Глава 2\n\nв.",
	)...))
	doc := res.Document
	require.Equal(t, []string{"Глава 1", "Глава 2"}, chapterTitles(doc.Chapters))
	require.Equal(t, []string{"Раздел 1.1"}, chapterTitles(doc.Chapters[0].Children))
}

func TestZipSummaryInSrcDirectory(t *testing.T) {
	// mdBook кладёт SUMMARY.md в src/.
	res := loadZip(t, makeZip(t, files(
		"book.toml", "[book]\ntitle = \"Книга mdBook\"\n",
		"src/SUMMARY.md", "- [B](b.md)\n- [A](a.md)\n",
		"src/a.md", "# A\n\nx.",
		"src/b.md", "# B\n\ny.",
	)...))
	require.Equal(t, []string{"B", "A"}, chapterTitles(res.Document.Chapters))
	require.Equal(t, "Книга mdBook", res.Document.Meta.Title)
}

func TestZipNumericPrefixesNaturalOrder(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"02-second.md", "# Second\n\nB.",
		"10-tenth.md", "# Tenth\n\nJ.",
		"01-first.md", "# First\n\nA.",
	)...))
	// Лексикографически «10» < «02»; нужен числовой порядок.
	require.Equal(t, []string{"First", "Second", "Tenth"}, chapterTitles(res.Document.Chapters))
	require.Contains(t, issueWith(res.Issues, "order_source").Message, "номер")
}

func TestZipNaturalOrderAcrossDirectories(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"part10/a.md", "# P10\n\nx.",
		"part2/a.md", "# P2\n\nx.",
		"part1/b.md", "# P1b\n\nx.",
		"part1/a.md", "# P1a\n\nx.",
	)...))
	require.Equal(t, []string{"P1a", "P1b", "P2", "P10"}, chapterTitles(res.Document.Chapters))
}

func TestZipMkdocsNavOrder(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"mkdocs.yml", "site_name: Документация\nnav:\n  - Главная: index.md\n  - 'Руководство':\n      - 'Установка': guide/install.md\n  - about.md\ntheme: material\n",
		"docs/about.md", "# О проекте\n\nx.",
		"docs/index.md", "# Главная\n\nx.",
		"docs/guide/install.md", "# Установка\n\nx.",
	)...))
	doc := res.Document
	require.Equal(t, "Документация", doc.Meta.Title)
	// Раздел навигации без файла — пустая глава-«часть»: без неё «Установка»
	// вложилась бы в «Главную», и структура документации исказилась бы.
	require.Equal(t, []string{"Главная", "Руководство", "Установка", "О проекте"}, titlesInReadingOrder(doc))
	require.Equal(t, []string{"Установка"}, chapterTitles(doc.Chapters[1].Children))
	require.Contains(t, issueWith(res.Issues, "order_source").Message, "mkdocs.yml")
}

func TestZipReadmeLinksOrder(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"README.md", "# Проект\n\nСм. [вторую](b.md) и [первую](a.md).",
		"a.md", "# A\n\nx.",
		"b.md", "# B\n\nx.",
		"c.md", "# C\n\nx.",
	)...))
	// README — вводная глава, затем ссылки в порядке упоминания, затем остальное.
	require.Equal(t, []string{"Проект", "B", "A", "C"}, chapterTitles(res.Document.Chapters))
	require.Equal(t, "Проект", res.Document.Meta.Title)
}

func titlesInReadingOrder(doc cdm.Document) []string {
	var out []string
	var walk func([]cdm.Chapter)
	walk = func(chs []cdm.Chapter) {
		for _, ch := range chs {
			out = append(out, ch.Title)
			walk(ch.Children)
		}
	}
	walk(doc.Chapters)
	return out
}

func TestZipStripsSingleTopLevelDirectory(t *testing.T) {
	// Архив репозитория с GitHub целиком лежит в «repo-main/».
	res := loadZip(t, makeZip(t, files(
		"repo-main/SUMMARY.md", "- [Two](2.md)\n- [One](1.md)\n",
		"repo-main/1.md", "# One\n\nx.",
		"repo-main/2.md", "# Two\n\nx.",
	)...))
	require.Equal(t, []string{"Two", "One"}, chapterTitles(res.Document.Chapters))
}

func TestZipIgnoresHiddenAndMacOSXEntries(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"a.md", "# A\n\nx.",
		"__MACOSX/._a.md", "\x00\x05\x16\x07мусор",
		".github/README.md", "# Не глава\n\nx.",
		".DS_Store", "\x00\x00",
	)...))
	require.Equal(t, []string{"A"}, chapterTitles(res.Document.Chapters))
}

func TestZipSlipRejected(t *testing.T) {
	_, err := importers.NewZipRepo(testLimits).Load(makeZip(t, files(
		"ok.md", "# Ok\n\nx.", "../../etc/passwd", "root",
	)...), "repo.zip")
	require.ErrorIs(t, err, storage.ErrUnsafePath)
}

func TestZipSlipInHiddenEntryStillRejected(t *testing.T) {
	// Служебные записи пропускаются, но их имена проверяются: опасное имя —
	// признак злонамеренного архива целиком.
	_, err := importers.NewZipRepo(testLimits).Load(makeZip(t, files(
		"ok.md", "# Ok\n\nx.", "__MACOSX/../../escape", "x",
	)...), "repo.zip")
	require.ErrorIs(t, err, storage.ErrUnsafePath)
}

func TestZipSymlinkEntryRejected(t *testing.T) {
	data := makeZip(t, zipFile{name: "ok.md", body: "# Ok\n\nx."},
		zipFile{name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777})
	_, err := importers.NewZipRepo(testLimits).Load(data, "repo.zip")
	require.ErrorIs(t, err, storage.ErrUnsafePath)
}

func TestZipTooManyEntriesRejected(t *testing.T) {
	var fs []zipFile
	for i := range 5 {
		fs = append(fs, zipFile{name: strings.Repeat("f", i+1) + ".md", body: "# H\n\nx."})
	}
	_, err := importers.NewZipRepo(importers.ArchiveLimits{MaxEntries: 3, MaxTotalBytes: 1 << 20, MaxRatio: 100}).
		Load(makeZip(t, fs...), "repo.zip")
	require.ErrorIs(t, err, importers.ErrArchiveLimits)
}

func TestZipCompressionBombRejected(t *testing.T) {
	data := makeZip(t, zipFile{name: "bomb.md", body: strings.Repeat("\x00", 4<<20)})
	_, err := importers.NewZipRepo(importers.ArchiveLimits{MaxEntries: 10, MaxTotalBytes: 100 << 20, MaxRatio: 100}).
		Load(data, "repo.zip")
	require.ErrorIs(t, err, importers.ErrArchiveLimits)
	require.Contains(t, err.Error(), "ratio")
}

func TestZipTotalSizeLimit(t *testing.T) {
	data := makeZip(t, files("a.md", strings.Repeat("слово ", 400), "b.md", strings.Repeat("текст ", 400))...)
	_, err := importers.NewZipRepo(importers.ArchiveLimits{MaxEntries: 10, MaxTotalBytes: 3000, MaxRatio: 100}).
		Load(data, "repo.zip")
	require.ErrorIs(t, err, importers.ErrArchiveLimits)
}

func TestZipEntryLargerThanDeclaredIsNotRead(t *testing.T) {
	// Заголовок ZIP лжёт о размере, чтобы обойти лимит по сумме объявленных
	// размеров. Защитное свойство: больше объявленного не читается, а
	// запись не попадает в книгу. (archive/zip сам обрывает чтение с
	// ErrFormat — отличить это от обычной порчи нельзя, поэтому запись
	// пропускается с замечанием, а не отклоняется весь архив.)
	body := []byte(strings.Repeat("ложь ", 1000))
	var compressed bytes.Buffer
	fw, err := flate.NewWriter(&compressed, flate.BestCompression)
	require.NoError(t, err)
	_, err = fw.Write(body)
	require.NoError(t, err)
	require.NoError(t, fw.Close())

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	honest, err := zw.Create("honest.md")
	require.NoError(t, err)
	_, err = honest.Write([]byte("# Честная\n\nТекст."))
	require.NoError(t, err)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name: "liar.md", Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(body),
		CompressedSize64: uint64(compressed.Len()), UncompressedSize64: 10,
	})
	require.NoError(t, err)
	_, err = w.Write(compressed.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	res := loadZip(t, buf.Bytes())
	require.Equal(t, []string{"Честная"}, chapterTitles(res.Document.Chapters))
	for _, b := range res.Document.Blocks() {
		require.NotContains(t, b.Text, "ложь")
	}
	require.NotNil(t, issueWith(res.Issues, "corrupt_entry"))
}

func TestZipMissingReferencedChapterIsIssue(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"SUMMARY.md", "- [Here](present.md)\n- [Gone](absent.md)\n",
		"present.md", "# Here\n\nx.",
	)...))
	// ТЗ §4.3: отсутствующие главы выявляются, а не пропадают молча.
	missing := issueWith(res.Issues, "missing_chapter")
	require.NotNil(t, missing)
	require.Contains(t, missing.Message, "absent.md")
}

func TestZipUnlistedFilesAppendedWithWarning(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"SUMMARY.md", "- [A](a.md)\n",
		"a.md", "# A\n\nx.",
		"forgotten.md", "# Забытая\n\nx.",
	)...))
	require.Equal(t, []string{"A", "Забытая"}, chapterTitles(res.Document.Chapters),
		"текст пользователя не пропадает молча")
	unlisted := issueWith(res.Issues, "unlisted_file")
	require.NotNil(t, unlisted)
	require.Contains(t, unlisted.Message, "forgotten.md")
}

func TestZipMissingImageIsIssue(t *testing.T) {
	res := loadZip(t, makeZip(t, files("ch.md", "# Ch\n\nТекст ![схема](img/none.png) внутри.")...))
	missing := issueWith(res.Issues, "missing_resource")
	require.NotNil(t, missing, "встроенная в абзац картинка тоже проверяется")
	require.Contains(t, missing.Message, "img/none.png")
}

func TestZipBrokenBodyLinkIsWarning(t *testing.T) {
	res := loadZip(t, makeZip(t, files("ch.md", "# Ch\n\nСм. [там](nowhere.md).")...))
	broken := issueWith(res.Issues, "broken_link")
	require.NotNil(t, broken)
	require.Equal(t, importers.Warning, broken.Severity)
}

func TestZipImagesBecomeBlocksAndAssets(t *testing.T) {
	res := loadZip(t, makeZip(t, files(
		"chapters/ch.md", "# Ch\n\n![диаграмма](../img/d.png)\n",
		"img/d.png", "\x89PNG\r\n\x1a\nданные",
		"img/unused.png", "\x89PNG",
	)...))
	images := blocksOf(res.Document, cdm.Image)
	require.Len(t, images, 1)
	require.Equal(t, "img/d.png", images[0].Meta["src"], "путь разрешён относительно файла главы")
	require.Nil(t, issueWith(res.Issues, "missing_resource"))

	require.Len(t, res.Assets, 1, "сохраняются только используемые ресурсы")
	require.Equal(t, "img/d.png", res.Assets[0].Name)
	raw, err := res.Assets[0].Open()
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(raw, []byte("\x89PNG")))
}

func TestZipNonMarkdownFilesIgnored(t *testing.T) {
	res := loadZip(t, makeZip(t, files("a.md", "# A\n\nx.", "script.py", "print(1)", "data.json", "{}")...))
	require.Equal(t, []string{"A"}, chapterTitles(res.Document.Chapters))
}

func TestZipCorruptArchiveIsIssue(t *testing.T) {
	res, err := importers.NewZipRepo(testLimits).Load([]byte("PK\x03\x04 не архив"), "repo.zip")
	require.NoError(t, err)
	require.NotNil(t, issueWith(res.Issues, "corrupt_file"))
}

func TestZipWithoutMarkdownIsEmpty(t *testing.T) {
	res := loadZip(t, makeZip(t, files("readme.txt", "текст")...))
	require.NotNil(t, issueWith(res.Issues, "empty_document"))
}

func TestPickZip(t *testing.T) {
	imp, err := defaultRegistry(t).Pick(makeZip(t, files("a.md", "# A")...), "repo.zip")
	require.NoError(t, err)
	require.Equal(t, "zip_repo", imp.FormatID())
}

func zipSeed(entries map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func FuzzZipRepoLoad(f *testing.F) {
	f.Add(zipSeed(map[string]string{"SUMMARY.md": "- [A](a.md)\n  - [B](b/b.md)", "a.md": "# A", "b/b.md": "## B"}))
	f.Add(zipSeed(map[string]string{"mkdocs.yml": "nav:\n  - x.md\n  - S:\n    - y.md", "docs/x.md": "x", "docs/y.md": "y"}))
	f.Add(zipSeed(map[string]string{"r-main/README.md": "[c](c.md)", "r-main/c.md": "![i](i.png)"}))
	f.Add(zipSeed(map[string]string{"10.md": "a", "2.md": "b", "__MACOSX/._x": "z"}))
	f.Add([]byte("PK\x03\x04"))
	limits := importers.ArchiveLimits{MaxEntries: 200, MaxTotalBytes: 4 << 20, MaxRatio: 100}
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := importers.NewZipRepo(limits).Load(data, "fuzz.zip")
		if err != nil {
			if !errors.Is(err, storage.ErrUnsafePath) && !errors.Is(err, importers.ErrArchiveLimits) {
				t.Fatalf("недопустимый класс ошибки: %v", err)
			}
			return
		}
		if err := res.Document.Validate(); err != nil {
			t.Fatalf("документ нарушает инварианты: %v", err)
		}
		for _, b := range res.Document.Blocks() {
			if !utf8.ValidString(b.Text) {
				t.Fatalf("невалидный UTF-8 в %s", b.NodeID)
			}
		}
		for _, a := range res.Assets {
			if _, err := storage.ValidateMemberName(a.Name); err != nil {
				t.Fatalf("ресурс с небезопасным именем: %q", a.Name)
			}
		}
	})
}
