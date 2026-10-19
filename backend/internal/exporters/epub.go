package exporters

import (
	"archive/zip"
	"bytes"
	"hash/crc32"
	"html"
	"io"
	"strconv"
	"strings"
	"time"
)

// epubExporter — EPUB 3 со стандартной библиотекой (дополнение §16): OPF,
// nav.xhtml, toc.ncx для читалок EPUB 2 и XHTML на каждую главу — включая
// подглавы: главы-якоря внутри одного файла многие читалки (и наш импорт)
// не различают.
type epubExporter struct{}

func (epubExporter) Format() string { return "epub" }

func (epubExporter) Describe(*Book) Output {
	return Output{Ext: ".epub", ContentType: "application/epub+zip"}
}

const epubMime = "application/epub+zip"

type epubChapter struct {
	ch    *Chapter
	depth int
	file  string // относительно OEBPS/
	id    string
}

func epubChapters(chs []Chapter, depth int, out []epubChapter) []epubChapter {
	for i := range chs {
		id := "ch" + leftPad(chs[i].Index)
		out = append(out, epubChapter{ch: &chs[i], depth: depth, file: "text/" + id + ".xhtml", id: id})
		out = epubChapters(chs[i].Children, depth+1, out)
	}
	return out
}

func (epubExporter) Write(w io.Writer, b *Book) error {
	zw := zip.NewWriter(w)
	// mimetype — первой записью, без сжатия и без дескриптора данных
	// (OCF): по этим байтам EPUB узнают, не распаковывая архив.
	mime := []byte(epubMime)
	date, clock := msDosTime(time.Now())
	mf, err := zw.CreateRaw(&zip.FileHeader{Name: "mimetype", Method: zip.Store, CRC32: crc32.ChecksumIEEE(mime),
		CompressedSize64: uint64(len(mime)), UncompressedSize64: uint64(len(mime)),
		ModifiedDate: date, ModifiedTime: clock}) //nolint:staticcheck // SA1019: CreateRaw читает только эти поля, не Modified
	if err != nil {
		return err
	}
	if _, err := mf.Write(mime); err != nil {
		return err
	}
	put := func(name, body string) error {
		f, err := createZipEntry(zw, name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	if err := put("META-INF/container.xml", `<?xml version="1.0" encoding="utf-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
`); err != nil {
		return err
	}

	order, names := bookImages(b)
	var manifestImages []string
	packed := map[string]string{}
	for _, src := range order {
		data, err := b.Assets(src)
		if err != nil {
			continue
		}
		f, err := createZipEntry(zw, "OEBPS/images/"+names[src])
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
			return err
		}
		packed[src] = "images/" + names[src]
		manifestImages = append(manifestImages, `<item id="`+strings.TrimSuffix(names[src], extOf(names[src]))+
			`" href="images/`+names[src]+`" media-type="`+imageTypes[extOf(names[src])]+`"/>`)
	}

	lang := b.Lang
	if lang == "" {
		lang = "und"
	}
	langAttr := ` lang="` + html.EscapeString(lang) + `" xml:lang="` + html.EscapeString(lang) + `"`
	esc := func(s string) string { return html.EscapeString(xmlSafe(s)) }
	r := &htmlRenderer{book: b, xhtml: true, image: func(src string) (string, bool) {
		name, ok := packed[src]
		return "../" + name, ok
	}}
	chapters := epubChapters(b.Chapters, 1, nil)
	for _, c := range chapters {
		body := `<section id="c` + strconv.Itoa(c.ch.Index) + `" epub:type="chapter">` + "\n" + r.blocks(c.ch.Blocks) + "</section>\n"
		if err := put("OEBPS/"+c.file, xhtmlPage(langAttr, esc(c.ch.Title), `<link rel="stylesheet" type="text/css" href="../style.css"/>`, body)); err != nil {
			return err
		}
	}

	files := map[*Chapter]string{}
	for _, c := range chapters {
		files[c.ch] = c.file
	}
	nav := `<nav epub:type="toc" id="toc"><h1>` + esc(b.Title) + "</h1>" +
		tocList(b.Chapters, func(ch *Chapter) string { return files[ch] }) + "</nav>\n"
	if err := put("OEBPS/nav.xhtml", xhtmlPage(langAttr, esc(b.Title), "", nav)); err != nil {
		return err
	}
	id := b.ID
	if !strings.HasPrefix(id, "urn:") {
		id = "urn:uuid:" + id
	}
	if err := put("OEBPS/toc.ncx", ncxDocument(esc(id), esc(b.Title), b.Chapters, files)); err != nil {
		return err
	}
	if err := put("OEBPS/style.css", bookCSS); err != nil {
		return err
	}

	var opf strings.Builder
	opf.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid"` + ` xml:lang="` + esc(lang) + `">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:identifier id="bookid">` + esc(id) + `</dc:identifier>
<dc:title>` + esc(b.Title) + "</dc:title>\n")
	if b.Author != "" {
		opf.WriteString("<dc:creator>" + esc(b.Author) + "</dc:creator>\n")
	}
	opf.WriteString("<dc:language>" + esc(lang) + "</dc:language>\n" +
		`<meta property="dcterms:modified">` + time.Now().UTC().Format("2006-01-02T15:04:05Z") + "</meta>\n</metadata>\n<manifest>\n" +
		`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n" +
		`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n" +
		`<item id="css" href="style.css" media-type="text/css"/>` + "\n")
	for _, c := range chapters {
		opf.WriteString(`<item id="` + c.id + `" href="` + c.file + `" media-type="application/xhtml+xml"/>` + "\n")
	}
	for _, item := range manifestImages {
		opf.WriteString(item + "\n")
	}
	opf.WriteString("</manifest>\n<spine toc=\"ncx\">")
	for _, c := range chapters {
		opf.WriteString(`<itemref idref="` + c.id + `"/>`)
	}
	opf.WriteString("</spine>\n</package>\n")
	if err := put("OEBPS/content.opf", opf.String()); err != nil {
		return err
	}
	return zw.Close()
}

func xhtmlPage(langAttr, title, head, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"` + langAttr + `>
<head>
<meta charset="utf-8"/>
<title>` + title + `</title>
` + head + `
</head>
<body>
` + body + `</body>
</html>
`
}

// ncxDocument — оглавление EPUB 2 для старых читалок.
func ncxDocument(id, title string, chs []Chapter, files map[*Chapter]string) string {
	var b strings.Builder
	order := 0
	maxDepth := 0
	var points func([]Chapter, int)
	points = func(chs []Chapter, depth int) {
		maxDepth = max(maxDepth, depth)
		for i := range chs {
			order++
			n := strconv.Itoa(order)
			b.WriteString(`<navPoint id="np` + n + `" playOrder="` + n + `"><navLabel><text>` +
				html.EscapeString(xmlSafe(chs[i].Title)) + `</text></navLabel><content src="` + files[&chs[i]] + `"/>`)
			points(chs[i].Children, depth+1)
			b.WriteString("</navPoint>")
		}
	}
	points(chs, 1)
	return `<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
<head><meta name="dtb:uid" content="` + id + `"/><meta name="dtb:depth" content="` + strconv.Itoa(maxDepth) + `"/></head>
<docTitle><text>` + title + `</text></docTitle>
<navMap>` + b.String() + `</navMap>
</ncx>
`
}

// msDosTime — время в формате заголовка ZIP. CreateRaw, в отличие от
// CreateHeader, поле Modified не переводит, а без даты архиватор покажет
// 1980 год.
func msDosTime(t time.Time) (date, clock uint16) {
	t = t.UTC()
	date = uint16(t.Day() + int(t.Month())<<5 + max(t.Year()-1980, 0)<<9) //nolint:gosec // G115: поля даты ZIP ограничены по построению
	clock = uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)           //nolint:gosec // G115: то же
	return date, clock
}
