package translation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/translation"
)

func validate(source, translated string, opts ...func(*translation.ValidationInput)) []translation.Finding {
	m := cdm.Mask(source)
	in := translation.ValidationInput{
		Source: source, Translation: translated, MaskedSource: m.Text, MaskedTranslation: maskedLike(m, translated),
		SourceLang: "en", TargetLang: "ru", Type: cdm.Paragraph,
	}
	for _, o := range opts {
		o(&in)
	}
	return translation.ValidateSegment(in)
}

// maskedLike строит замаскированный перевод так, как его видел бы сервис:
// фрагменты исходника, встретившиеся в переводе, заменяются их маркерами.
func maskedLike(m cdm.Masked, translated string) string {
	out := translated
	for key, value := range m.Placeholders {
		out = strings.Replace(out, value, key, 1)
	}
	return out
}

func codes(findings []translation.Finding) []string {
	out := make([]string, len(findings))
	for i, f := range findings {
		out[i] = f.Code
	}
	return out
}

func TestCleanTranslationHasNoFindings(t *testing.T) {
	require.Empty(t, validate("The book is on the table.", "Книга лежит на столе."))
	require.Equal(t, translation.OK, translation.Worst(nil))
}

func TestEmptyTranslationIsError(t *testing.T) {
	f := validate("Непустой исходник.", "   ")
	require.Contains(t, codes(f), "empty_translation")
	require.Equal(t, translation.Error, translation.Worst(f))
}

func TestPlaceholderMismatchIsError(t *testing.T) {
	f := validate("Call `run()` now.", "Вызовите сейчас.")
	require.Contains(t, codes(f), "placeholder_mismatch")
	require.Equal(t, translation.Error, translation.Worst(f))
}

func TestReorderedPlaceholdersAreFine(t *testing.T) {
	require.Empty(t, validate("Use `a` then `b` in this order.", "Сначала `b`, а потом `a` в таком порядке."))
}

func TestLinkCountChangeIsError(t *testing.T) {
	// Модель дописала свою ссылку — сломается навигация в книге.
	f := validate("See the documentation for details.", "См. [документацию](http://x) для деталей.")
	require.Contains(t, codes(f), "link_count_mismatch")
	require.Equal(t, translation.Error, translation.Worst(f))
}

func TestUntranslatedCopyIsWarning(t *testing.T) {
	source := "This is a reasonably long English sentence that should change."
	f := validate(source, source)
	require.Contains(t, codes(f), "untranslated")
	require.Equal(t, translation.Warning, translation.Worst(f))
}

func TestShortIdenticalTextIsFine(t *testing.T) {
	// «SQL», «2026», имена — законно совпадают (порог 40 символов, ADD §5).
	require.Empty(t, validate("SQL", "SQL"))
	require.Empty(t, validate("Linux kernel 6.1", "Linux kernel 6.1"))
}

func TestSameLanguagePairSkipsUntranslatedCheck(t *testing.T) {
	// Перевод en→en (упрощение) законно может совпадать с исходником.
	source := "This is a reasonably long English sentence that should change."
	require.NotContains(t, codes(validate(source, source, func(in *translation.ValidationInput) { in.TargetLang = "en" })), "untranslated")
}

func TestLengthRatioOutsideBoundsIsWarning(t *testing.T) {
	f := validate("A short sentence that is here.", strings.Repeat("Очень длинно. ", 20))
	require.Contains(t, codes(f), "length_ratio")
	require.Equal(t, translation.Warning, translation.Worst(f))

	f = validate("A rather long English sentence with many words in it.", "Да.")
	require.Contains(t, codes(f), "length_ratio")
}

func TestLengthRatioSkippedForShortSources(t *testing.T) {
	// У коротких строк соотношение скачет законно: «OK» → «Хорошо».
	require.Empty(t, validate("OK", "Хорошо"))
}

func TestInlineMarkupCountChangeIsWarning(t *testing.T) {
	for source, translated := range map[string]string{
		"This is **important** and **needed** for us.": "Это **важно** и нужно для нас.",
		"This is *emphasised* text in the sentence.":   "Это выделенный текст в предложении.",
	} {
		f := validate(source, translated)
		require.Contains(t, codes(f), "markup_mismatch", source)
		require.Equal(t, translation.Warning, translation.Worst(f))
	}
}

func TestHeadingLevelChangeIsWarning(t *testing.T) {
	f := validate("# Title of chapter", "## Заголовок главы", func(in *translation.ValidationInput) {
		in.Type, in.Level = cdm.Heading, 1
	})
	require.Contains(t, codes(f), "heading_level_changed")
}

func TestWorstPicksErrorOverWarning(t *testing.T) {
	f := validate("Code `x` and a reasonably long sentence around it.", "")
	require.Equal(t, translation.Error, translation.Worst(f))
}

func TestFindingsHaveReadableMessages(t *testing.T) {
	for _, f := range validate("Call `run()` now please, quickly.", "") {
		require.NotEmpty(t, f.Message, f.Code)
	}
}

func TestSeverityJSON(t *testing.T) {
	raw, err := json.Marshal(translation.Finding{Code: "x", Severity: translation.Warning, Message: "m"})
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"x","severity":"warning","message":"m"}`, string(raw))
}
