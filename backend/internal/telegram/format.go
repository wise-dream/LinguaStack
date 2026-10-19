package telegram

import (
	"html"

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
