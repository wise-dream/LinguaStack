package translation

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vnovels/linguastack/internal/cdm"
)

// Протокол общения с моделью — дополнение §3. Сегменты передаются в
// нумерованном конверте; ответ обязан содержать тот же конверт с тем же
// множеством номеров. Парсер устойчив к преамбуле, хвосту, обёртке ```
// и пробелам внутри маркеров; всё остальное — ProtocolError, которая
// повторяется по ступеням деградации.

var (
	segRe = regexp.MustCompile(`(?m)^[ \t]*<<<[ \t]*SEG[ \t]+(\d+)[ \t]*>>>[ \t]*$`)
	endRe = regexp.MustCompile(`(?m)^[ \t]*<<<[ \t]*END[ \t]*>>>[ \t]*$`)
)

// ProtocolError — ответ модели не соответствует конверту. Сырой ответ
// сохраняется для разбора (дополнение §3, ступень 3).
type ProtocolError struct {
	Reason string
	Raw    string
}

func (e *ProtocolError) Error() string {
	return "нарушение протокола ответа: " + e.Reason
}

// Retryable — нарушение протокола повторяется (дополнение §6.1).
func (e *ProtocolError) Retryable() bool { return true }

// ParseEnvelope разбирает ответ модели в тексты сегментов по номерам.
func ParseEnvelope(text string, expected int) (map[int]string, error) {
	locs := segRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		return nil, &ProtocolError{Reason: "в ответе нет конверта <<<SEG n>>>", Raw: text}
	}
	end := len(text)
	if loc := endRe.FindStringIndex(text[locs[0][1]:]); loc != nil {
		end = locs[0][1] + loc[0]
	}

	got := make(map[int]string, len(locs))
	for i, loc := range locs {
		if loc[0] >= end {
			break // маркеры после <<<END>>> — хвост ответа
		}
		n, err := strconv.Atoi(text[loc[2]:loc[3]])
		if err != nil || n < 1 {
			return nil, &ProtocolError{Reason: "недопустимый номер сегмента " + text[loc[2]:loc[3]], Raw: text}
		}
		if _, dup := got[n]; dup {
			return nil, &ProtocolError{Reason: fmt.Sprintf("сегмент %d повторяется", n), Raw: text}
		}
		stop := end
		if i+1 < len(locs) && locs[i+1][0] < end {
			stop = locs[i+1][0]
		}
		got[n] = strings.TrimSpace(text[loc[1]:stop])
	}

	var missing, extra []int
	for n := 1; n <= expected; n++ {
		if _, ok := got[n]; !ok {
			missing = append(missing, n)
		}
	}
	for n := range got {
		if n > expected {
			extra = append(extra, n)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		slices.Sort(extra)
		return nil, &ProtocolError{Reason: fmt.Sprintf("нет сегментов %v, лишние %v", missing, extra), Raw: text}
	}
	return got, nil
}

// ProtectEnvelope прячет «<<<» и «>>>» в тексте сегмента за плейсхолдеры.
//
// Текст книги — недоверенный вход протокола: строка «<<<END>>>» в абзаце
// обрезала бы перевод, а «<<<SEG 9>>>» — подменила бы сегмент. Плейсхолдер
// восстанавливается обычным Unmask, а его потерю ловит та же проверка
// маркеров, что и потерю кода.
func ProtectEnvelope(m cdm.Masked) cdm.Masked {
	if !strings.Contains(m.Text, "<<<") && !strings.Contains(m.Text, ">>>") {
		return m
	}
	placeholders := make(map[string]string, len(m.Placeholders)+2)
	for k, v := range m.Placeholders {
		placeholders[k] = v
	}
	next := len(m.Placeholders) // номера в cdm.Mask сплошные: 1..n
	var b strings.Builder
	for i := 0; i < len(m.Text); {
		if rest := m.Text[i:]; strings.HasPrefix(rest, "<<<") || strings.HasPrefix(rest, ">>>") {
			next++
			key := "⟦" + strconv.Itoa(next) + "⟧"
			placeholders[key] = rest[:3]
			b.WriteString(key)
			i += 3
			continue
		}
		b.WriteByte(m.Text[i])
		i++
	}
	return cdm.Masked{Text: b.String(), Placeholders: placeholders}
}

// neutralize заменяет маркерные последовательности в тексте, который не
// восстанавливается (контекст, глоссарий): там допустима замена на похожие
// символы.
var neutralizer = strings.NewReplacer("<<<", "‹‹‹", ">>>", "›››", "\r", " ", "\n", " ")

var styles = map[string]string{
	"neutral":    "Use a natural, faithful style that reads as if originally written in %s.",
	"literary":   "Preserve the author's voice, imagery, rhythm and register; write idiomatic literary %s.",
	"technical":  "Use precise and consistent technical terminology in %s; keep identifiers, product names and code terms as they are.",
	"simplified": "Use plain, simple %s; where the original is dense or technical, explain it more simply without losing meaning.",
}

// Styles — идентификаторы стилей перевода по алфавиту.
func Styles() []string {
	out := make([]string, 0, len(styles))
	for id := range styles {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// IsKnownStyle — поддерживается ли стиль перевода.
func IsKnownStyle(style string) bool {
	_, ok := styles[style]
	return ok
}

func languageLabel(code string) string {
	if name, ok := LanguageName(code); ok {
		return name + " (" + code + ")"
	}
	return code
}

// BuildSystemPrompt — инструкция модели. Детерминирована: глоссарий
// сортируется, никаких дат и идентификаторов — иначе рушится кэш префикса
// и каждая книга дорожает (дополнение §6.2).
func BuildSystemPrompt(src, tgt, style string, glossary map[string]string) string {
	target := languageLabel(tgt)
	styleLine, ok := styles[style]
	if !ok {
		styleLine = styles["neutral"]
	}
	source := languageLabel(src)
	if src == "" || src == "auto" {
		source = "the source language (detect it)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "You are a professional book translator. Translate from %s to %s.\n\n", source, target)
	b.WriteString("Rules:\n")
	b.WriteString("1. The user message contains numbered segments, each starting with a marker line like <<<SEG 1>>>, and a final <<<END>>> line.\n")
	b.WriteString("2. Reply with exactly the same marker lines, numbers and order, each followed by the translation of that segment, then <<<END>>>. Write nothing before the first marker or after <<<END>>>.\n")
	b.WriteString("3. Never merge, split, skip or add segments. Translate every segment, even a short one.\n")
	b.WriteString("4. Tokens like ⟦1⟧ are protected placeholders for code, links and formulas. Copy each one exactly once into the translation where it belongs grammatically. Never translate, change or drop them.\n")
	b.WriteString("5. Keep Markdown formatting: **bold**, *italic*, `code`, link syntax and heading markers.\n")
	b.WriteString("6. Text marked as CONTEXT is for reference only. Do not translate it and do not include it in the reply.\n")
	fmt.Fprintf(&b, "\nStyle: %s\n", fmt.Sprintf(styleLine, target))

	if len(glossary) > 0 {
		terms := make([]string, 0, len(glossary))
		for term := range glossary {
			terms = append(terms, term)
		}
		slices.Sort(terms)
		b.WriteString("\nGlossary — always use these translations:\n")
		for _, term := range terms {
			src := strings.TrimSpace(neutralizer.Replace(term))
			dst := strings.TrimSpace(neutralizer.Replace(glossary[term]))
			if src != "" && dst != "" {
				fmt.Fprintf(&b, "- %s → %s\n", src, dst)
			}
		}
	}
	return b.String()
}

// BuildUserPrompt — сегменты батча в конверте. Тексты сегментов должны
// пройти ProtectEnvelope; контекст нейтрализуется здесь.
func BuildUserPrompt(batch Batch) string {
	var b strings.Builder
	b.WriteString("Translate the segments below.\n\n")
	if batch.ContextBefore != "" {
		b.WriteString("CONTEXT BEFORE (for reference only — do not translate, do not output):\n")
		b.WriteString(neutralizer.Replace(batch.ContextBefore) + "\n\n")
	}
	if batch.ContextAfter != "" {
		b.WriteString("CONTEXT AFTER (for reference only — do not translate, do not output):\n")
		b.WriteString(neutralizer.Replace(batch.ContextAfter) + "\n\n")
	}
	for i, s := range batch.Segments {
		fmt.Fprintf(&b, "<<<SEG %d>>>\n%s\n", i+1, s.Text)
	}
	b.WriteString("<<<END>>>")
	return b.String()
}
