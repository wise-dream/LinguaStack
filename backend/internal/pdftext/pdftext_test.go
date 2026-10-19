package pdftext_test

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/pdftext"
)

// Пул PDFium общий на процесс: модуль компилируется один раз (~3 с).
var shared = pdftext.NewPDFium(pdftext.Options{MaxPages: 100, Timeout: time.Minute, MemoryLimitMB: 512, Instances: 2})

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../importers/testdata/pdf/" + name)
	require.NoError(t, err)
	return data
}

func extract(t *testing.T, name string) *pdftext.Document {
	t.Helper()
	doc, err := shared.Extract(fixture(t, name))
	require.NoError(t, err)
	return doc
}

func outlineTitles(items []pdftext.OutlineItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestPDFiumOutlineAndMeta(t *testing.T) {
	doc := extract(t, "outline.pdf")
	require.Equal(t, "Пробная книга", doc.Title)
	require.Len(t, doc.Pages, 3)
	require.Equal(t, []string{"Глава первая", "Chapter Two", "Глава третья"}, outlineTitles(doc.Outline))
	require.Equal(t, []int{1, 2, 3}, []int{doc.Outline[0].Page, doc.Outline[1].Page, doc.Outline[2].Page})
	require.Equal(t, []string{"Раздел 1.1"}, outlineTitles(doc.Outline[0].Children))
	require.Equal(t, 1, doc.Outline[0].Children[0].Page)
	require.Empty(t, extract(t, "plain.pdf").Outline)
}

func findLine(page pdftext.Page, prefix string) *pdftext.Line {
	for i := range page.Lines {
		if strings.HasPrefix(strings.TrimSpace(page.Lines[i].Text()), prefix) {
			return &page.Lines[i]
		}
	}
	return nil
}

func TestPDFiumLinesAndSpans(t *testing.T) {
	doc := extract(t, "outline.pdf")
	page := doc.Pages[0]
	require.Equal(t, 1, page.Number)
	require.Greater(t, page.Height, page.Width, "A5, книжная")

	heading := findLine(page, "Глава первая")
	require.NotNil(t, heading)
	require.Len(t, heading.Spans, 1)
	require.True(t, heading.Spans[0].Bold)
	body := findLine(page, "Второй абзац.")
	require.NotNil(t, body)
	require.False(t, body.Spans[0].Bold)
	require.Greater(t, heading.Spans[0].Size, body.Spans[0].Size*1.5)
	require.Greater(t, heading.Top, body.Top, "координаты от нижнего края: заголовок выше")

	code := findLine(page, "func main()")
	require.NotNil(t, code)
	require.True(t, code.Spans[0].Mono)
	require.False(t, body.Spans[0].Mono)

	hyphenated := 0
	for _, p := range doc.Pages {
		for _, l := range p.Lines {
			text := l.Text()
			require.NotContains(t, text, "\r")
			require.NotContains(t, text, "\n")
			require.NotContains(t, text, "\u00ad", "невидимые мягкие переносы вырезаны")
			if strings.HasSuffix(text, "\x02") {
				hyphenated++
			}
		}
	}
	require.Positive(t, hyphenated, "перенос в конце строки помечен \\x02 — его снимает разбор вёрстки")
}

func TestPDFiumScannedPageHasImagesNoText(t *testing.T) {
	doc := extract(t, "scanned.pdf")
	require.Len(t, doc.Pages, 1)
	require.Empty(t, doc.Pages[0].Lines)
	require.Positive(t, doc.Pages[0].Images)
	require.Zero(t, extract(t, "plain.pdf").Pages[0].Images)
}

func TestPDFiumEncrypted(t *testing.T) {
	_, err := shared.Extract(fixture(t, "encrypted.pdf"))
	require.True(t, errors.Is(err, pdftext.ErrEncrypted), "err = %v", err)
}

func TestPDFiumCorrupt(t *testing.T) {
	_, err := shared.Extract([]byte("%PDF-1.7\nэто не PDF"))
	require.True(t, errors.Is(err, pdftext.ErrCorrupt), "err = %v", err)
}

func TestPDFiumTooManyPages(t *testing.T) {
	x := pdftext.NewPDFium(pdftext.Options{MaxPages: 2, Timeout: time.Minute, MemoryLimitMB: 512, Instances: 2})
	_, err := x.Extract(fixture(t, "outline.pdf"))
	require.True(t, errors.Is(err, pdftext.ErrTooManyPages), "err = %v", err)
}

func TestPDFiumTimeout(t *testing.T) {
	x := pdftext.NewPDFium(pdftext.Options{MaxPages: 100, Timeout: time.Nanosecond, MemoryLimitMB: 512, Instances: 2})
	_, err := x.Extract(fixture(t, "outline.pdf"))
	require.True(t, errors.Is(err, pdftext.ErrTimeout), "err = %v", err)
	// Убитый экземпляр в пул не вернулся: следующий разбор работает.
	require.Len(t, extract(t, "outline.pdf").Pages, 3)
}

func TestPDFiumConcurrentExtract(t *testing.T) {
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = shared.Extract(fixture(t, "outline.pdf"))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
}
