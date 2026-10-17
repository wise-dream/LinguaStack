package providers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepetitionLoop(t *testing.T) {
	prose := "Алиса начала уставать сидеть с сестрой на берегу без дела: раз или два она " +
		"заглядывала в книгу, которую читала сестра, но там не было ни картинок, ни разговоров. " +
		"«Что толку в книжке, — подумала Алиса, — если в ней нет ни картинок, ни разговоров?» " +
		"И она стала соображать (насколько могла, потому что от жары совсем осовела), стоит ли " +
		"удовольствие сплести венок из ромашек того, чтобы встать и нарвать цветов, как вдруг " +
		"мимо пробежал Белый Кролик с розовыми глазами. "
	sentence := "Вниз, вниз, вниз — неужели этому не будет конца? "
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"один символ", "она так" + strings.Repeat("о", 400), true},
		{"короткий период", "и " + strings.Repeat("вниз, ", 70), true},
		{"обычный текст", prose, false},
		{"повтор в тексте книги", "«Вниз, вниз, вниз!» " + prose, false},
		{"зацикленное предложение", prose + strings.Repeat(sentence, 7), true},
		{"повтор ниже порога", "она так" + strings.Repeat("о", 300), false},
		{"разделитель таблицы", strings.Repeat("|---", 60), false},
		{"короткий ответ", "ооо", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, repetitionLoop(tc.text), tc.name)
	}
}
