package importers_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type epubChapter struct {
	file  string // имя файла в OEBPS/, по умолчанию chN.xhtml
	title string // подпись в оглавлении
	body  string // содержимое <body>
	depth int    // глубина в оглавлении, по умолчанию 1
}

type epubOptions struct {
	title, author, language string
	chapters                []epubChapter
	noTOC                   bool              // без toc.ncx и nav
	nav                     map[string]string // EPUB 3 nav: файл → подпись (перекрывает NCX)
	spine                   []string          // порядок чтения, если отличается от порядка chapters
	extra                   map[string]string // дополнительные файлы: путь в архиве → содержимое
}

// buildEPUB собирает минимальный корректный EPUB, чтобы не держать
// бинарные фикстуры в репозитории.
func buildEPUB(t *testing.T, o epubOptions) []byte {
	t.Helper()
	for i := range o.chapters {
		if o.chapters[i].file == "" {
			o.chapters[i].file = fmt.Sprintf("ch%d.xhtml", i+1)
		}
		if o.chapters[i].depth == 0 {
			o.chapters[i].depth = 1
		}
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string, method uint16) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	// По спецификации mimetype — первая запись и без сжатия.
	write("mimetype", "application/epub+zip", zip.Store)
	write("META-INF/container.xml", `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`, zip.Deflate)

	var manifest, spine strings.Builder
	for i, ch := range o.chapters {
		fmt.Fprintf(&manifest, `<item id="c%d" href="%s" media-type="application/xhtml+xml"/>`, i, ch.file)
		write("OEBPS/"+strings.ReplaceAll(ch.file, "%20", " "), xhtml(ch.title, ch.body), zip.Deflate)
	}
	order := o.spine
	if order == nil {
		for _, ch := range o.chapters {
			order = append(order, ch.file)
		}
	}
	for _, file := range order {
		for i, ch := range o.chapters {
			if ch.file == file {
				fmt.Fprintf(&spine, `<itemref idref="c%d"/>`, i)
			}
		}
	}

	spineAttr := ""
	if !o.noTOC {
		manifest.WriteString(`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`)
		spineAttr = ` toc="ncx"`
		write("OEBPS/toc.ncx", ncx(o.chapters), zip.Deflate)
	}
	if o.nav != nil {
		manifest.WriteString(`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`)
		var items strings.Builder
		for _, ch := range o.chapters {
			if label, ok := o.nav[ch.file]; ok {
				fmt.Fprintf(&items, `<li><a href="%s">%s</a></li>`, ch.file, label)
			}
		}
		write("OEBPS/nav.xhtml", `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>nav</title></head>
<body><nav epub:type="toc"><ol>`+items.String()+`</ol></nav></body></html>`, zip.Deflate)
	}
	for name, body := range o.extra {
		write(name, body, zip.Deflate)
		if strings.HasPrefix(name, "OEBPS/") && strings.HasSuffix(name, ".png") {
			fmt.Fprintf(&manifest, `<item id="%s" href="%s" media-type="image/png"/>`,
				strings.NewReplacer("/", "_", ".", "_").Replace(name), strings.TrimPrefix(name, "OEBPS/"))
		}
	}

	author := ""
	if o.author != "" {
		author = "<dc:creator>" + o.author + "</dc:creator>"
	}
	lang := o.language
	if lang == "" {
		lang = "en"
	}
	write("OEBPS/content.opf", `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">urn:uuid:test</dc:identifier>
    <dc:title>`+o.title+`</dc:title>`+author+`<dc:language>`+lang+`</dc:language>
  </metadata>
  <manifest>`+manifest.String()+`</manifest>
  <spine`+spineAttr+`>`+spine.String()+`</spine>
</package>`, zip.Deflate)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func xhtml(title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title>
<style>p { margin: 0 }</style></head>
<body>` + body + `</body></html>`
}

// ncx строит навигацию EPUB 2 с вложенностью по depth.
func ncx(chapters []epubChapter) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>`)
	depth := 0
	for i, ch := range chapters {
		for depth >= ch.depth {
			b.WriteString("</navPoint>")
			depth--
		}
		fmt.Fprintf(&b, `<navPoint id="n%d"><navLabel><text>%s</text></navLabel><content src="%s"/>`, i, ch.title, ch.file)
		depth = ch.depth
	}
	for ; depth > 0; depth-- {
		b.WriteString("</navPoint>")
	}
	b.WriteString("</navMap></ncx>")
	return b.String()
}
