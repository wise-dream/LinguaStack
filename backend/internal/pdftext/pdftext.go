// Package pdftext извлекает из PDF текст с вёрсткой: строки, шрифт и кегль
// каждого куска, закладки и метаданные (дополнение §15.1). Что из строк
// получаются абзацы и заголовки, решает импортёр — здесь только данные.
package pdftext

import (
	"errors"
	"strings"
	"time"
)

// Span — кусок строки одним шрифтом.
type Span struct {
	Text   string
	Size   float64 // кегль в единицах документа: сравнивается только внутри одного PDF
	Bold   bool
	Italic bool
	Mono   bool
}

// Line — строка текста в порядке чтения PDFium. Координаты — в точках,
// начало — левый нижний угол страницы: у верхней строки Top больше.
type Line struct {
	Spans  []Span
	X0, X1 float64
	Top    float64
	Bottom float64
}

// Text — текст строки. «\x02» в конце — перенос, который поставил PDFium
// (дефис в конце строки): склеивать слово или нет, решает разбор вёрстки.
func (l Line) Text() string {
	var b strings.Builder
	for _, s := range l.Spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Page — страница; Number — с 1. Images — число изображений: страница
// без текста, но с картинками — скан.
type Page struct {
	Number        int
	Width, Height float64
	Lines         []Line
	Images        int
}

// OutlineItem — закладка. Page — с 1; 0 — назначения нет.
type OutlineItem struct {
	Title    string
	Page     int
	Children []OutlineItem
}

// Document — извлечённый PDF.
type Document struct {
	Title, Author string
	Pages         []Page
	Outline       []OutlineItem
}

// Extractor извлекает текст PDF.
type Extractor interface {
	Extract(data []byte) (*Document, error)
}

var (
	ErrEncrypted    = errors.New("PDF защищён паролем")
	ErrCorrupt      = errors.New("PDF повреждён")
	ErrTimeout      = errors.New("разбор PDF не уложился в отведённое время")
	ErrTooManyPages = errors.New("в PDF больше страниц, чем допустимо")
)

// Options — пределы разбора (дополнение §10).
type Options struct {
	MaxPages      int           // больше — ErrTooManyPages до извлечения текста
	Timeout       time.Duration // на документ; по истечении экземпляр уничтожается
	MemoryLimitMB int           // память модуля wasm
	Instances     int           // одновременных разборов
}

// lineHyphen — перенос в конце строки. PDFium сам помечает так дефис в
// конце строки; Chrome печатает перенос символом U+2010 или оставляет
// мягкий перенос U+00AD — они приводятся к той же метке.
const lineHyphen = "\x02"

// markLineHyphen ставит метку переноса вместо концевого U+2010 или U+00AD.
func markLineHyphen(s string) string {
	trimmed := strings.TrimRight(s, " ")
	for _, h := range []string{"\u2010", "\u00ad"} {
		if strings.HasSuffix(trimmed, h) {
			return strings.TrimSuffix(trimmed, h) + lineHyphen
		}
	}
	return s
}

// cleanText убирает невидимое и недопустимое: мягкий перенос U+00AD внутри
// строки (его оставляет в тексте Chrome) и управляющие символы, которых не
// бывает в тексте книги и которые не допускает XML. «\x02» остаётся.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\u00ad':
			return -1
		case r == '\x02' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || r == '\ufffe' || r == '\uffff':
			return -1
		}
		return r
	}, s)
}
