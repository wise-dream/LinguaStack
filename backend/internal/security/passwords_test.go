package security_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/security"
)

func TestHashIsArgon2idAndVerifies(t *testing.T) {
	hash, err := security.HashPassword("correct horse battery staple")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hash, "$argon2id$"), "ТЗ §13: безопасное хеширование, получено %q", hash)
	require.True(t, security.VerifyPassword("correct horse battery staple", hash))
	require.False(t, security.VerifyPassword("wrong", hash))
}

func TestHashIsSalted(t *testing.T) {
	a, err := security.HashPassword("same")
	require.NoError(t, err)
	b, err := security.HashPassword("same")
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestVerifyRejectsGarbage(t *testing.T) {
	// Повреждённая запись в БД означает «не совпало», а не падение входа.
	for _, encoded := range []string{"", "not-a-hash", "$argon2id$v=19$broken"} {
		require.False(t, security.VerifyPassword("anything", encoded), "хеш %q", encoded)
	}
}
