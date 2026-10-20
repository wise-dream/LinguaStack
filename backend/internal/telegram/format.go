package telegram

import (
	"html"
	"unicode/utf8"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// LanguageName — название языка по-русски для сообщений: «английский».
func LanguageName(code string) string {
	tag, err := language.Parse(code)
	if err != nil {
		return code
	}
	if name := display.Russian.Languages().Name(tag); name != "" {
		return name
	}
	return code
}

// Link — ссылка в разметке HTML сообщения.
func Link(url, text string) string {
	return `<a href="` + html.EscapeString(url) + `">` + html.EscapeString(text) + "</a>"
}

// MaxText — предел длины сообщения Telegram (символов); текст с разметкой
// держим с запасом ниже.
const MaxText = 4096

// Clip укорачивает строку до n символов с многоточием: длинное название
// книги не должно сделать сообщение длиннее предела Telegram.
func Clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
