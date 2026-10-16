package security_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/security"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

func newBox(t *testing.T) *security.SecretBox {
	t.Helper()
	box, err := security.NewSecretBox(newKey(t))
	require.NoError(t, err)
	return box
}

func TestSecretRoundTrip(t *testing.T) {
	box := newBox(t)
	const apiKey = "sk-ant-secret-value-12345"

	token, err := box.Encrypt(apiKey)
	require.NoError(t, err)
	require.NotContains(t, token, apiKey, "шифртекст не должен содержать открытый ключ")

	plain, err := box.Decrypt(token)
	require.NoError(t, err)
	require.Equal(t, apiKey, plain)
}

func TestEncryptionIsRandomised(t *testing.T) {
	box := newBox(t)
	a, err := box.Encrypt("same-value")
	require.NoError(t, err)
	b, err := box.Encrypt("same-value")
	require.NoError(t, err)
	// Повтор nonce в GCM раскрывает XOR открытых текстов — недопустимо.
	require.NotEqual(t, a, b)
}

func TestDecryptRejectsTampering(t *testing.T) {
	box := newBox(t)
	token, err := box.Encrypt("value")
	require.NoError(t, err)

	raw, err := base64.RawStdEncoding.DecodeString(token)
	require.NoError(t, err)
	raw[len(raw)-1] ^= 0x01
	_, err = box.Decrypt(base64.RawStdEncoding.EncodeToString(raw))
	require.True(t, errors.Is(err, security.ErrDecrypt), "получено: %v", err)
}

func TestDecryptRejectsForeignKey(t *testing.T) {
	token, err := newBox(t).Encrypt("value")
	require.NoError(t, err)
	_, err = newBox(t).Decrypt(token)
	require.True(t, errors.Is(err, security.ErrDecrypt), "смена ключа не должна молча давать мусор")
}

func TestDecryptRejectsMalformedTokens(t *testing.T) {
	box := newBox(t)
	for _, token := range []string{"", "!!!не-base64!!!", base64.RawStdEncoding.EncodeToString([]byte("short"))} {
		_, err := box.Decrypt(token)
		require.True(t, errors.Is(err, security.ErrDecrypt), "токен %q: %v", token, err)
	}
}

func TestNewSecretBoxRejectsWrongKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		_, err := security.NewSecretBox(make([]byte, n))
		require.Error(t, err, "ключ длиной %d", n)
	}
}

func TestFingerprintAndLast4(t *testing.T) {
	require.Len(t, security.Fingerprint("abc"), 8)
	require.Equal(t, security.Fingerprint("abc"), security.Fingerprint("abc"))
	require.NotEqual(t, security.Fingerprint("abc"), security.Fingerprint("abd"))
	require.False(t, strings.Contains(security.Fingerprint("sk-ant-1234"), "1234"))

	require.Equal(t, "2345", security.Last4("sk-ant-12345"))
	require.Equal(t, "ab", security.Last4("ab"), "короткая строка не должна паниковать")
}
