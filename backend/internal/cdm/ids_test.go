package cdm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
)

func TestNodeIDDeterministicAndReadable(t *testing.T) {
	require.Equal(t, "ch0003/b0017", cdm.NodeID(3, 17))
	require.Equal(t, cdm.NodeID(3, 17), cdm.NodeID(3, 17))
	require.NotEqual(t, cdm.NodeID(3, 17), cdm.NodeID(17, 3))
}

func TestContentHashIgnoresInsignificantWhitespace(t *testing.T) {
	// Дополнение §1: переформатирование не должно помечать перевод устаревшим.
	base := cdm.ContentHash("Hello world")
	for _, variant := range []string{"Hello   world", "  Hello world\n", "Hello\tworld", "Hello\u00a0world"} {
		require.Equal(t, base, cdm.ContentHash(variant), "вариант %q", variant)
	}
	require.NotEqual(t, base, cdm.ContentHash("Hello worlds"))
}

func TestContentHashNormalizesUnicode(t *testing.T) {
	// «é» одним символом и как e + комбинируемый акцент — один и тот же текст.
	require.Equal(t, cdm.ContentHash("caf\u00e9"), cdm.ContentHash("cafe\u0301"))
}

func TestContentHashIsCaseSensitive(t *testing.T) {
	// Регистр — смысловое изменение («Москва» и «москва»), перевод обязан обновиться.
	require.NotEqual(t, cdm.ContentHash("Apple"), cdm.ContentHash("apple"))
}

func TestContentHashShape(t *testing.T) {
	require.Len(t, cdm.ContentHash("x"), 32, "16 байт в hex")
}
