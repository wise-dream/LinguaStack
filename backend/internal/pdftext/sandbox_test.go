package pdftext

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Review Focus №1: модуль PDFium не видит файловую систему хоста. go-pdfium
// монтирует в модуль корень «/», если FSConfig не задан.
func TestPDFiumSandboxConfig(t *testing.T) {
	cfg := poolConfig(Options{MemoryLimitMB: 1024, Instances: 2})
	require.NotNil(t, cfg.FSConfig, "без FSConfig go-pdfium монтирует корень хоста")
	require.NotNil(t, cfg.RuntimeConfig)
	require.Equal(t, 2, cfg.MaxTotal)
	require.Equal(t, uint32(16384), memoryLimitPages(1024), "страница wasm — 64 КиБ")
}

func TestCleanTextKeepsHyphenMarker(t *testing.T) {
	require.Equal(t, "ab\x02", cleanText("a\u00adb\x02"))
	require.Equal(t, "xy", cleanText("x\x00\x0c\x1fy"))
	require.Equal(t, "\tкириллица", cleanText("\tкириллица"))
}

func TestLineEndHyphensMarked(t *testing.T) {
	require.Equal(t, "пре\x02", markLineHyphen("пре\u2010"))
	require.Equal(t, "пре\x02", markLineHyphen("пре\u00ad "))
	require.Equal(t, "PDF-", markLineHyphen("PDF-"), "обычный дефис PDFium помечает сам, если это перенос")
	require.Equal(t, "a\u2010b", markLineHyphen("a\u2010b"))
}
