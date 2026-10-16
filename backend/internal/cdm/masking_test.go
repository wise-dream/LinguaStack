package cdm_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
)

func TestMaskRoundTrip(t *testing.T) {
	// Главное свойство: Unmask(Mask(x)) == x для любого текста.
	cases := []string{
		"Обычный абзац без кода и ссылок.",
		"Вызовите `get_user()` затем `save()`.",
		"См. [документацию](https://example.com/docs) здесь.",
		"Формула $E = mc^2$ известна.",
		"Блок $$\\int_0^1 x\\,dx$$ посередине.",
		"Картинка ![Схема](img/a.png) в тексте.",
		"Тег <b>жирный</b> и <br/> перенос.",
		"Адрес https://example.com/path?q=1. Конец.",
		"Литерал ⟦1⟧ в тексте и код `x`.",
		"Только скобки ⟦ и ⟧ по отдельности.",
		// Найдено фаззером: восстановленные скобки складывались в «⟦1⟧»
		// и попадали под следующую замену.
		"1⟦0⟦1⟧0000000",
		"⟦⟦1⟧⟧ вложенные литералы",
		"Ссылка с кодом [`run()`](https://x.io) внутри.",
		"Картинка в ссылке [![logo](l.png)](https://x.io).",
		"Фрагмент ```\nfenced\n``` внутри.",
		"Двойные ``code with ` tick`` кавычки.",
		"Стоит $5 и $10, не формула.",
		"",
	}
	for _, text := range cases {
		m := cdm.Mask(text)
		require.Equal(t, text, cdm.Unmask(m.Text, m.Placeholders), "исходник %q, маска %q", text, m.Text)
	}
}

func TestInlineCodeHiddenFromModel(t *testing.T) {
	m := cdm.Mask("Вызовите `get_user()` затем `save()`.")
	require.NotContains(t, m.Text, "get_user()")
	require.NotContains(t, m.Text, "save()")
	require.Len(t, m.Placeholders, 2)
	require.Contains(t, m.Text, "Вызовите", "обычный текст остаётся видимым")
}

func TestLinkTextVisibleTargetHidden(t *testing.T) {
	m := cdm.Mask("См. [документацию](https://example.com/docs) здесь.")
	require.Contains(t, m.Text, "документацию", "текст ссылки переводится")
	require.NotContains(t, m.Text, "example.com")
	require.NotContains(t, m.Text, "](", "синтаксис ссылки защищён целиком")
}

func TestMathHidden(t *testing.T) {
	require.NotContains(t, cdm.Mask("Формула $E = mc^2$ известна.").Text, "mc^2")
	require.NotContains(t, cdm.Mask("Блок $$a+b$$ тут.").Text, "a+b")
}

func TestDollarAmountsAreNotMath(t *testing.T) {
	// Цена — не формула: иначе модель не переведёт фразу вокруг.
	text := "Стоит $5 и $10, всего $15."
	m := cdm.Mask(text)
	require.Equal(t, text, m.Text)
	require.Empty(t, m.Placeholders)
}

func TestHTMLTagsHiddenTextVisible(t *testing.T) {
	m := cdm.Mask("Тег <b>жирный</b> здесь.")
	require.Contains(t, m.Text, "жирный")
	require.NotContains(t, m.Text, "<b>")
	require.NotContains(t, m.Text, "</b>")
}

func TestBareURLHiddenTrailingPunctuationVisible(t *testing.T) {
	m := cdm.Mask("Сайт https://example.com/a. Дальше.")
	require.NotContains(t, m.Text, "example.com")
	// Точка — часть предложения, её модель может переставить при переводе.
	require.Contains(t, m.Text, "⟧. Дальше.")
}

func TestImageHiddenWhole(t *testing.T) {
	m := cdm.Mask("Вот ![Схема](img/a.png) пример.")
	require.NotContains(t, m.Text, "img/a.png")
	require.NotContains(t, m.Text, "![")
}

func TestPreexistingMarkersEscaped(t *testing.T) {
	// Review Focus №2: исходник уже содержит ⟦1⟧. Без экранирования Unmask
	// подставил бы сюда чужой фрагмент и тихо испортил текст.
	original := "Литерал ⟦1⟧ в тексте и код `x`."
	m := cdm.Mask(original)
	require.Equal(t, original, cdm.Unmask(m.Text, m.Placeholders))

	// Модель видит только наши маркеры: если убрать из маски все известные
	// плейсхолдеры, ни одной скобки ⟦ или ⟧ не остаётся.
	require.Empty(t, strayBrackets(m))
}

// strayBrackets — скобки-маркеры в маске, не входящие ни в один плейсхолдер.
func strayBrackets(m cdm.Masked) string {
	// Ключ заменяется пробелом, а не удаляется: удаление склеило бы
	// соседние обломки невалидного UTF-8 в настоящую скобку (нашёл фаззер).
	rest := m.Text
	for key := range m.Placeholders {
		rest = strings.ReplaceAll(rest, key, " ")
	}
	var stray strings.Builder
	for _, r := range rest {
		if r == '⟦' || r == '⟧' {
			stray.WriteRune(r)
		}
	}
	return stray.String()
}

func FuzzMaskRoundTrip(f *testing.F) {
	for _, seed := range []string{
		"Вызовите `get_user()`.", "[текст](https://x.io/a)", "$a+b$ и $$c$$",
		"<b>x</b>", "⟦1⟧⟧⟦", "https://x.io.", "![a](b.png)", "``a ` b``",
		"```\ncode\n```", "[`x`](u)", "[![i](i.png)](u)", "$5 и $10",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		m := cdm.Mask(text)
		if got := cdm.Unmask(m.Text, m.Placeholders); got != text {
			t.Fatalf("round-trip нарушен:\nисходник %q\nмаска    %q\nвышло    %q", text, m.Text, got)
		}
		if err := cdm.VerifyPlaceholders(m.Text, m.Text); err != nil {
			t.Fatalf("маска не согласуется сама с собой: %v", err)
		}
		if s := strayBrackets(m); s != "" {
			t.Fatalf("в маске остались литеральные скобки %q: %q", s, m.Text)
		}
	})
}

func TestPlainTextUnchanged(t *testing.T) {
	text := "Обычный абзац без кода и ссылок."
	m := cdm.Mask(text)
	require.Equal(t, text, m.Text)
	require.Empty(t, m.Placeholders)
}

func TestVerifyDetectsDroppedMarker(t *testing.T) {
	m := cdm.Mask("Код `a` и `b`.")
	broken := strings.Replace(m.Text, "⟦2⟧", "", 1)
	err := cdm.VerifyPlaceholders(m.Text, broken)
	var mismatch *cdm.PlaceholderMismatchError
	require.True(t, errors.As(err, &mismatch), "получено: %v", err)
	require.Equal(t, []string{"⟦2⟧"}, mismatch.Missing)
	require.Empty(t, mismatch.Extra)
}

func TestVerifyDetectsDuplicatedMarker(t *testing.T) {
	m := cdm.Mask("Код `a`.")
	err := cdm.VerifyPlaceholders(m.Text, m.Text+" ⟦1⟧")
	var mismatch *cdm.PlaceholderMismatchError
	require.True(t, errors.As(err, &mismatch))
	require.Equal(t, []string{"⟦1⟧"}, mismatch.Extra)
}

func TestVerifyDetectsInventedMarker(t *testing.T) {
	m := cdm.Mask("Код `a`.")
	err := cdm.VerifyPlaceholders(m.Text, "Code ⟦1⟧ ⟦7⟧.")
	var mismatch *cdm.PlaceholderMismatchError
	require.True(t, errors.As(err, &mismatch))
	require.Equal(t, []string{"⟦7⟧"}, mismatch.Extra)
}

func TestVerifyAllowsReordering(t *testing.T) {
	// При переводе порядок слов меняется — важно мультимножество маркеров.
	m := cdm.Mask("`a` затем `b`")
	require.NoError(t, cdm.VerifyPlaceholders(m.Text, "⟦2⟧ after ⟦1⟧"))
}

func TestVerifyNoMarkers(t *testing.T) {
	require.NoError(t, cdm.VerifyPlaceholders("plain", "простой"))
}
