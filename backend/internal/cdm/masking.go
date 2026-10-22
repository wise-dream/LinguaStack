package cdm

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
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

var (
	// inlineCodeAnyRe — инлайн-код по тем же правилам, что у Mask, одним
	// проходом: ``…`` раньше `…`.
	inlineCodeAnyRe = regexp.MustCompile("``[^\n]+?``|`[^`\n]+`")
	// inlineCodeValueRe — значение маркера, которое целиком инлайн-код.
	inlineCodeValueRe = regexp.MustCompile("^(?:``[^\n]+``|`[^`\n]+`)$")
)

// InlineCode — инлайн-код текста вместе с обратными кавычками, в порядке
// появления. Блоки ``` в счёт не идут, как и в Mask.
func InlineCode(text string) []string {
	return inlineCodeAnyRe.FindAllString(fencedRe.ReplaceAllString(text, " "), -1)
}

// MaskKeepingCode — Mask, у которого инлайн-код остаётся в тексте как есть,
// в обратных кавычках, а остальное — под метками. Это последняя ступень
// перевода одного фрагмента (дополнение §3): метку на месте кода слабая
// модель выбрасывает — для неё это пустое место в предложении, — а сам код
// переносит. Сохранность такого кода проверяется по InlineCode, а не по
// меткам.
//
// Оставшиеся метки перенумерованы сплошь с 1 в прежнем порядке — значение
// метки по-прежнему ссылается только на меньшие номера (Unmask). Код с
// меткой внутри (экранированная скобка) остаётся под меткой.
func MaskKeepingCode(text string) Masked {
	m := Mask(text)
	keys := make([]string, 0, len(m.Placeholders))
	for k := range m.Placeholders {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return markerNumber(a) - markerNumber(b) })

	renamed := make(map[string]string, len(keys)) // старая метка → новая метка или сам код
	kept := make([]string, 0, len(keys))
	for _, k := range keys {
		v := m.Placeholders[k]
		if inlineCodeValueRe.MatchString(v) && !strings.HasPrefix(v, "```") && !strings.ContainsAny(v, "⟦⟧") {
			renamed[k] = v
			continue
		}
		kept = append(kept, k)
		renamed[k] = string(markerOpen) + strconv.Itoa(len(kept)) + string(markerClose)
	}
	// Один проход: подставленный код меток не содержит, новые номера
	// повторно не сканируются.
	rewrite := func(s string) string {
		return markerRe.ReplaceAllStringFunc(s, func(k string) string {
			if r, ok := renamed[k]; ok {
				return r
			}
			return k
		})
	}
	out := Masked{Text: rewrite(m.Text)}
	if len(kept) > 0 {
		out.Placeholders = make(map[string]string, len(kept))
		for _, k := range kept {
			out.Placeholders[renamed[k]] = rewrite(m.Placeholders[k])
		}
	}
	return out
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

// RepairMarkers чинит метки, которые модель записала с ошибкой: «⟧2⟧»,
// «⟦2» или «2⟧» вместо «⟦2⟧» — а одиночную скобку ставит на место
// единственной потерянной метки. Чинится только метка, которой не хватает
// в ответе: так перевод слабой модели не отвергается из-за одной скобки, а
// выдуманную или лишнюю метку по-прежнему ловит VerifyPlaceholders.
// Неоднозначный случай (две потерянные метки на одну скобку) не трогается.
// Когда все метки на месте, одиночные скобки выбрасываются: скобки
// исходника сами стали метками (Mask), так что в ответе это мусор модели.
func RepairMarkers(masked, translated string) string {
	missing := countMarkers(masked)
	for k, n := range countMarkers(translated) {
		missing[k] -= n
	}
	lost := 0
	for _, n := range missing {
		lost += max(n, 0)
	}
	if lost == 0 && !strings.ContainsAny(markerRe.ReplaceAllString(translated, ""), "⟦⟧") {
		return translated
	}
	take := func(num string) bool {
		key := string(markerOpen) + num + string(markerClose)
		if missing[key] <= 0 {
			return false
		}
		missing[key]--
		lost--
		return true
	}

	var b strings.Builder
	var orphans []int // позиции одиночных скобок в b
	prevDigit := false
	for i := 0; i < len(translated); {
		rest := translated[i:]
		if m := markerRe.FindStringIndex(rest); m != nil && m[0] == 0 {
			b.WriteString(rest[:m[1]])
			i += m[1]
			prevDigit = false
			continue
		}
		if m := brokenMarkerRe.FindStringSubmatchIndex(rest); m != nil {
			var num string
			bare := false
			switch {
			case m[2] >= 0: // ⟧N⟧
				num = rest[m[2]:m[3]]
			case m[4] >= 0: // ⟦N
				num = rest[m[4]:m[5]]
			default: // N⟧
				num, bare = rest[m[6]:m[7]], true
			}
			// «N⟧» — только с начала числа: из «12⟧» не выйдет «1⟦2⟧».
			if (!bare || !prevDigit) && take(num) {
				b.WriteString(string(markerOpen) + num + string(markerClose))
				i += m[1]
				prevDigit = false
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(rest)
		if r == markerOpen || r == markerClose {
			orphans = append(orphans, b.Len())
		}
		b.WriteString(rest[:size])
		prevDigit = unicode.IsDigit(r)
		i += size
	}
	out := b.String()
	if lost == 0 {
		for i := len(orphans) - 1; i >= 0; i-- {
			_, size := utf8.DecodeRuneInString(out[orphans[i]:])
			out = out[:orphans[i]] + out[orphans[i]+size:]
		}
		return out
	}
	if lost != 1 || len(orphans) != 1 {
		return out
	}
	for k, n := range missing {
		if n > 0 {
			at := orphans[0]
			_, size := utf8.DecodeRuneInString(out[at:])
			return out[:at] + k + out[at+size:]
		}
	}
	return out
}

// brokenMarkerRe — испорченная метка: «⟧N⟧», «⟦N» без закрывающей или «N⟧»
// без открывающей. Целые «⟦N⟧» RepairMarkers пропускает раньше.
var brokenMarkerRe = regexp.MustCompile(`^(?:⟧(\d+)⟧|⟦(\d+)|(\d+)⟧)`)
