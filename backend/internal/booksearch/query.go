// Package booksearch — поиск по тексту книги и её перевода (ТЗ §8.3,
// дополнение §19): формы слов, несколько слов в любом порядке, фраза в
// кавычках, начало последнего слова; если по словам ничего нет — подстрока,
// затем подсказка при опечатке.
package booksearch

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/translation"
)

// Query — разобранный запрос.
type Query struct {
	Raw     string
	Phrases []string // в кавычках: слова подряд и в этом порядке
	Words   []string // вне кавычек, строчными
	// Prefix — последнее слово запроса не дописано (за ним ничего нет):
	// ищется как начало слова.
	Prefix bool
	// CJK — в запросе иероглифы или азбука: слова там не разделены
	// пробелами, поиск по словам не работает, только подстрока.
	CJK bool
}

var (
	phraseRe = regexp.MustCompile(`"([^"]*)"|«([^»]*)»`)
	wordRe   = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

// Parse разбирает запрос. Непарная кавычка не делает фразы: слова за ней
// ищутся как обычные.
func Parse(raw string) Query {
	q := Query{Raw: raw}
	rest := phraseRe.ReplaceAllStringFunc(raw, func(m string) string {
		sub := phraseRe.FindStringSubmatch(m)
		if phrase := strings.TrimSpace(sub[1] + sub[2]); phrase != "" {
			q.Phrases = append(q.Phrases, phrase)
		}
		return " \x00 " // граница: слово до фразы не считается последним
	})
	trimmed := strings.TrimRightFunc(rest, unicode.IsSpace)
	for _, loc := range wordRe.FindAllStringIndex(rest, -1) {
		q.Words = append(q.Words, strings.ToLower(rest[loc[0]:loc[1]]))
		q.Prefix = loc[1] == len(trimmed)
	}
	q.CJK = strings.IndexFunc(raw, isCJK) >= 0
	return q
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Thai)
}

// OriginalLanguage — язык оригинала для поиска: из метаданных книги, иначе
// язык оригинала, заданный у перевода (sourceLangs — по убыванию важности,
// «auto» пропускается), иначе догадка по алфавиту. "" — язык неизвестен.
func OriginalLanguage(bookLang string, sourceLangs []string, doc *cdm.Document) string {
	for _, lang := range append([]string{bookLang}, sourceLangs...) {
		primary, _, _ := strings.Cut(strings.ToLower(lang), "-")
		if translation.IsKnownLanguage(primary) {
			return primary
		}
	}
	return guessByScript(doc)
}

// guessByScript: кириллица — русский, латиница — английский. Догадка
// грубая, но поиск от неё не ломается: запрос и текст разбираются одним
// словарём, точные слова находятся всегда, страдают только формы.
func guessByScript(doc *cdm.Document) string {
	const sample = 20000
	var cyrillic, latin, seen int
	var visit func(chapters []cdm.Chapter)
	visit = func(chapters []cdm.Chapter) {
		for i := range chapters {
			for _, b := range chapters[i].Blocks {
				for _, r := range b.Text {
					if seen >= sample {
						return
					}
					switch {
					case unicode.Is(unicode.Cyrillic, r):
						cyrillic++
					case unicode.Is(unicode.Latin, r):
						latin++
					}
					seen++
				}
			}
			visit(chapters[i].Children)
		}
	}
	visit(doc.Chapters)
	letters := cyrillic + latin
	switch {
	case letters == 0 || letters*2 < seen/2:
		// Букв почти нет: иероглифы, цифры, код.
		return ""
	case cyrillic >= latin:
		return "ru"
	default:
		return "en"
	}
}
