package cdm

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Маскирование защищённых фрагментов перед переводом (ТЗ §5.3, дополнение §4).
//
// Внутри переводимого текста код, формулы, URL, HTML-теги и синтаксис
// Markdown-ссылок заменяются маркерами ⟦1⟧, ⟦2⟧, … (U+27E6/U+27E7 — редкие
// символы, которые хорошо переживают round-trip через модель). После
// перевода мультимножество маркеров обязано совпасть с отправленным, иначе
// результат отвергается — так модель не может незаметно испортить код.

const (
	markerOpen  = '⟦'
	markerClose = '⟧'
)

var (
	markerRe = regexp.MustCompile(`⟦(\d+)⟧`)

	// Порядок проходов — приоритет: раньше захваченный фрагмент поздние
	// проходы уже не видят.
	fencedRe      = regexp.MustCompile("(?s)```.*?```")
	doubleTickRe  = regexp.MustCompile("``[^\n]+?``")
	inlineCodeRe  = regexp.MustCompile("`[^`\n]+`")
	displayMathRe = regexp.MustCompile(`(?s)\$\$.+?\$\$`)
	// TeX-правило: после открывающего и перед закрывающим $ нет пробела.
	// Поэтому «$5 и $10» — не формула.
	inlineMathRe = regexp.MustCompile(`\$[^\s$](?:[^$\n]*[^\s$])?\$`)
	imageRe      = regexp.MustCompile(`!\[[^\]\n]*\]\([^)\n]*\)`)
	linkRe       = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\n]*)\)`)
	htmlTagRe    = regexp.MustCompile(`<(?:/?[a-zA-Z][^<>\n]*|!--.*?--)>`)
	bareURLRe    = regexp.MustCompile(`https?://[^\s<>()\[\]⟦⟧"']+`)
)

// Masked — текст с маркерами и значения, которые они заменяют.
type Masked struct {
	Text         string            `json:"text"`
	Placeholders map[string]string `json:"placeholders,omitempty"`
}

type masker struct {
	placeholders map[string]string
	next         int
}

func (m *masker) put(value string) string {
	m.next++
	key := string(markerOpen) + strconv.Itoa(m.next) + string(markerClose)
	m.placeholders[key] = value
	return key
}

// replace заменяет каждое совпадение результатом fn; если fn вернул
// ok == false, совпадение остаётся как есть.
func replace(re *regexp.Regexp, text string, fn func(match string, groups []string) (string, bool)) string {
	locs := re.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		groups := make([]string, 0, len(loc)/2)
		for i := 2; i < len(loc); i += 2 {
			if loc[i] >= 0 {
				groups = append(groups, text[loc[i]:loc[i+1]])
			} else {
				groups = append(groups, "")
			}
		}
		b.WriteString(text[last:loc[0]])
		if out, ok := fn(text[loc[0]:loc[1]], groups); ok {
			b.WriteString(out)
		} else {
			b.WriteString(text[loc[0]:loc[1]])
		}
		last = loc[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// Mask заменяет защищённые фрагменты маркерами.
func Mask(text string) Masked {
	m := &masker{placeholders: map[string]string{}}
	whole := func(match string, _ []string) (string, bool) { return m.put(match), true }

	// 1. Литеральные ⟦ и ⟧ из исходника сами становятся плейсхолдерами —
	//    раньше всех, чтобы маркеры, вставленные дальше, не путались с ними.
	//    Отдельная схема экранирования не нужна, а потерю такой скобки
	//    моделью ловит та же проверка мультимножества.
	//    Копируются исходные байты, а не перекодированная руна: range по
	//    строке превращает невалидный UTF-8 в U+FFFD и молча портит текст.
	var escaped strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == markerOpen || r == markerClose {
			escaped.WriteString(m.put(string(r)))
		} else {
			escaped.WriteString(text[i : i+size])
		}
		i += size
	}
	out := escaped.String()

	out = replace(fencedRe, out, whole)
	out = replace(doubleTickRe, out, whole)
	out = replace(inlineCodeRe, out, whole)
	out = replace(displayMathRe, out, whole)
	out = replace(inlineMathRe, out, whole)
	out = replace(imageRe, out, whole)
	// Ссылка: текст остаётся видимым, синтаксис вокруг защищён полностью.
	out = replace(linkRe, out, func(match string, groups []string) (string, bool) {
		return m.put("[") + groups[0] + m.put("]("+groups[1]+")"), true
	})
	out = replace(htmlTagRe, out, whole)
	// Хвостовая пунктуация — часть предложения, а не адреса: модель вправе
	// переставить её при переводе.
	out = replace(bareURLRe, out, func(match string, _ []string) (string, bool) {
		url := strings.TrimRight(match, ".,;:!?")
		return m.put(url) + match[len(url):], true
	})

	if len(m.placeholders) == 0 {
		return Masked{Text: out}
	}
	return Masked{Text: out, Placeholders: m.placeholders}
}

func markerNumber(key string) int {
	sub := markerRe.FindStringSubmatch(key)
	if sub == nil {
		return 0
	}
	n, _ := strconv.Atoi(sub[1])
	return n
}

// Unmask возвращает защищённые фрагменты на место.
//
// Подстановка — один проход по маркерам исходного текста: подставленное
// повторно не сканируется. Последовательные ReplaceAll здесь некорректны —
// восстановленные литеральные скобки могут сложиться в новую строку вида
// ⟦1⟧ и попасть под следующую замену (нашёл фаззер на входе «1⟦0⟦1⟧0»).
//
// Значение плейсхолдера может содержать более ранние маркеры (код внутри
// текста ссылки, экранированная скобка внутри кода), поэтому значения
// раскрываются рекурсивно. Рекурсия конечна: в значении маркера N бывают
// только маркеры с меньшими номерами, созданные до него.
func Unmask(text string, placeholders map[string]string) string {
	if len(placeholders) == 0 {
		return text
	}
	var expand func(s string, depth int) string
	expand = func(s string, depth int) string {
		return markerRe.ReplaceAllStringFunc(s, func(key string) string {
			value, ok := placeholders[key]
			if !ok || depth > len(placeholders) {
				return key // чужой маркер оставляем как есть; его отвергнет VerifyPlaceholders
			}
			return expand(value, depth+1)
		})
	}
	return expand(text, 0)
}

// PlaceholderMismatchError — модель потеряла, продублировала или выдумала маркеры.
type PlaceholderMismatchError struct {
	Missing []string
	Extra   []string
}

func (e *PlaceholderMismatchError) Error() string {
	return fmt.Sprintf("маркеры не совпали: потеряны %v, лишние %v", e.Missing, e.Extra)
}

// VerifyPlaceholders сверяет мультимножества маркеров. Порядок не важен:
// при переводе порядок слов меняется.
func VerifyPlaceholders(masked, translated string) error {
	want := countMarkers(masked)
	got := countMarkers(translated)
	var missing, extra []string
	for k, n := range want {
		for range n - got[k] {
			missing = append(missing, k)
		}
	}
	for k, n := range got {
		for range n - want[k] {
			extra = append(extra, k)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	byNumber := func(a, b string) int { return markerNumber(a) - markerNumber(b) }
	slices.SortFunc(missing, byNumber)
	slices.SortFunc(extra, byNumber)
	return &PlaceholderMismatchError{Missing: missing, Extra: extra}
}

func countMarkers(text string) map[string]int {
	counts := map[string]int{}
	for _, k := range markerRe.FindAllString(text, -1) {
		counts[k]++
	}
	return counts
}
