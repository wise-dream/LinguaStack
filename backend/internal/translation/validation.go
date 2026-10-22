package translation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
)

// Severity — тяжесть замечания проверки (дополнение §5).
type Severity string

const (
	OK      Severity = "ok"
	Warning Severity = "warning"
	Error   Severity = "error"
)

func (s Severity) rank() int {
	switch s {
	case Error:
		return 2
	case Warning:
		return 1
	}
	return 0
}

// Finding — замечание проверки перевода.
type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Worst — самое тяжёлое замечание; OK, если замечаний нет.
func Worst(findings []Finding) Severity {
	worst := OK
	for _, f := range findings {
		if f.Severity.rank() > worst.rank() {
			worst = f.Severity
		}
	}
	return worst
}

// ValidationInput — исходник и перевод в обоих видах: маркеры сверяются
// на замаскированных текстах (до восстановления), ссылки и разметка — на
// восстановленных.
type ValidationInput struct {
	Source, Translation             string
	MaskedSource, MaskedTranslation string
	SourceLang, TargetLang          string
	Type                            cdm.BlockType
	Level                           int
}

const (
	untranslatedMinChars = 40  // короче — совпадение законно: «SQL», имена
	ratioMinChars        = 20  // короче — соотношение длин скачет законно
	minLengthRatio       = 0.3 // дополнение §5
	maxLengthRatio       = 3.0
)

var (
	linkRe         = regexp.MustCompile(`!?\[[^\]\n]*\]\([^)\n]*\)`)
	headingMarksRe = regexp.MustCompile(`^\s*(#+)\s`)
	// markupTokens — встроенная разметка, которую модель обязана сохранить.
	// Одиночную * считаем отдельно от **: курсив EPUB-импорт выдаёт именно так.
	markupTokens = []string{"**", "*", "`", "_", "["}
)

// ValidateSegment проверяет перевод одного сегмента. Таблица проверок и
// их тяжести — ровно дополнение §5.
func ValidateSegment(in ValidationInput) []Finding {
	var out []Finding
	add := func(code string, sev Severity, msg string) {
		out = append(out, Finding{Code: code, Severity: sev, Message: msg})
	}

	if strings.TrimSpace(in.Translation) == "" && strings.TrimSpace(in.Source) != "" {
		add("empty_translation", Error, "Перевод пуст при непустом исходнике.")
		return out // остальные проверки на пустом тексте бессмысленны
	}

	if err := cdm.VerifyPlaceholders(in.MaskedSource, in.MaskedTranslation); err != nil {
		var mismatch *cdm.PlaceholderMismatchError
		msg := "Защищённые фрагменты (код, ссылки, формулы) потеряны или продублированы."
		if errors.As(err, &mismatch) {
			msg = fmt.Sprintf("%s Потеряны: %v, лишние: %v.", msg, mismatch.Missing, mismatch.Extra)
		}
		add("placeholder_mismatch", Error, msg)
	}

	// Код на виду (ступень MaskKeepingCode, дополнение §3) метки не
	// стерегут: каждый `…` исходника обязан дойти до перевода байт в байт.
	// Под метками кода в замаскированном тексте нет — проверка пуста.
	if lost := lostCode(in.MaskedSource, in.MaskedTranslation); len(lost) > 0 {
		add("code_changed", Error, "Код в обратных кавычках потерян или изменён: "+strings.Join(lost, ", ")+".")
	}

	if a, b := len(linkRe.FindAllString(in.Source, -1)), len(linkRe.FindAllString(in.Translation, -1)); a != b {
		add("link_count_mismatch", Error, fmt.Sprintf("Число ссылок и изображений изменилось: было %d, стало %d.", a, b))
	}

	sourceChars := utf8.RuneCountInString(strings.TrimSpace(in.Source))
	// Без букв (разделитель «* * *», числа) переводить нечего — совпадение
	// с исходником верное.
	if in.SourceLang != in.TargetLang && sourceChars > untranslatedMinChars && hasLetters(in.Source) &&
		cdm.NormalizeText(in.Source) == cdm.NormalizeText(in.Translation) {
		add("untranslated", Warning, "Перевод дословно совпадает с исходником.")
	}

	if sourceChars >= ratioMinChars {
		ratio := float64(utf8.RuneCountInString(strings.TrimSpace(in.Translation))) / float64(sourceChars)
		if ratio < minLengthRatio || ratio > maxLengthRatio {
			add("length_ratio", Warning, fmt.Sprintf("Длина перевода необычна: %.2f от исходника.", ratio))
		}
	}

	for _, token := range markupTokens {
		if a, b := countMarkup(in.Source, token), countMarkup(in.Translation, token); a != b {
			add("markup_mismatch", Warning, fmt.Sprintf("Разметка «%s» изменилась: было %d, стало %d.", token, a, b))
			break
		}
	}

	if in.Type == cdm.Heading && headingMarks(in.Source) != headingMarks(in.Translation) {
		add("heading_level_changed", Warning, "Уровень заголовка изменился.")
	}
	return out
}

// lostCode — инлайн-код исходника, которого нет в переводе (с учётом
// повторов). Лишний код в переводе не в счёт: это разметка, а не порча.
func lostCode(source, translation string) []string {
	have := map[string]int{}
	for _, c := range cdm.InlineCode(translation) {
		have[c]++
	}
	var lost []string
	for _, c := range cdm.InlineCode(source) {
		if have[c] > 0 {
			have[c]--
			continue
		}
		lost = append(lost, c)
	}
	return lost
}

// countMarkup считает токен; одиночная * — без учёта входящих в **.
func countMarkup(s, token string) int {
	if token == "*" {
		return strings.Count(strings.ReplaceAll(s, "**", ""), "*")
	}
	return strings.Count(s, token)
}

func headingMarks(s string) int {
	if m := headingMarksRe.FindStringSubmatch(s); m != nil {
		return len(m[1])
	}
	return 0
}

func hasLetters(s string) bool {
	return strings.ContainsFunc(s, unicode.IsLetter)
}
