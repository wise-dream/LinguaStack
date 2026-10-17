package providers

import "unicode/utf8"

// Пороги распознавания зацикливания (см. repetitionLoop).
const (
	maxShortPeriod = 20  // «оооо», «вниз, вниз,»
	minLoopRunes   = 400 // короткий период должен занять столько символов
	loopRepeats    = 6   // длинный период должен повториться столько раз подряд
	maxLoopPeriod  = 200 // длиннее — уже не цикл, а абзацы
)

// repetitionLoop сообщает, что текст кончается зацикливанием: один и тот
// же фрагмент подряд до самого конца. Короткий период (до 20 символов:
// «оооо», «вниз, вниз,») — от 400 символов, длинный (одно и то же
// предложение) — от шести повторов. Проза и разметка книги (строка таблицы |---|---|,
// рефрен песни) столько подряд не повторяются, а модель в цикле без
// обрыва генерировала бы до num_predict — на CPU это десятки минут.
func repetitionLoop(text string) bool {
	tail := lastRunes(text, loopRepeats*maxLoopPeriod)
	for p := 1; p <= maxLoopPeriod; p++ {
		window := loopRepeats * p
		if p <= maxShortPeriod {
			window = minLoopRunes
		}
		if window > len(tail) {
			continue
		}
		if !periodic(tail[len(tail)-window:], p) {
			continue
		}
		// Сплошное «оооо» периодично с любым шагом: длинным периодом
		// считается только фрагмент, который сам не сводится к короткому
		// — иначе порог в 400 символов обходился бы через шесть повторов.
		if p > maxShortPeriod && hasShortPeriod(tail[len(tail)-p:]) {
			continue
		}
		return true
	}
	return false
}

func hasShortPeriod(unit []rune) bool {
	for q := 1; q <= maxShortPeriod; q++ {
		if periodic(unit, q) {
			return true
		}
	}
	return false
}

func periodic(r []rune, p int) bool {
	for i := p; i < len(r); i++ {
		if r[i] != r[i-p] {
			return false
		}
	}
	return true
}

// lastRunes — не больше n последних символов строки.
func lastRunes(s string, n int) []rune {
	start := len(s)
	for count := 0; start > 0 && count < n; count++ {
		_, size := utf8.DecodeLastRuneInString(s[:start])
		start -= size
	}
	return []rune(s[start:])
}
