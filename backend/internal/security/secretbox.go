package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	secretKeyLength   = 32 // AES-256
	fingerprintLength = 8
)

// ErrDecrypt — шифртекст повреждён, подменён или создан другим ключом.
// Причина намеренно не уточняется: детали помогают атакующему.
var ErrDecrypt = errors.New("не удалось расшифровать секрет")

// SecretBox шифрует пользовательские API-ключи (ТЗ §6.3, дополнение §8).
//
// AES-256-GCM — аутентифицированное шифрование: подмена шифртекста
// обнаруживается при расшифровке. Каждое шифрование берёт случайный
// nonce; токен — base64(nonce ‖ ciphertext ‖ tag).
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox создаёт шифратор. Неверная длина ключа — ошибка сразу,
// а не при первой попытке расшифровать чужой секрет.
func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != secretKeyLength {
		return nil, fmt.Errorf("ключ шифрования должен быть %d байт, получено %d", secretKeyLength, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("GCM: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

// Encrypt шифрует значение.
func (b *SecretBox) Encrypt(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt расшифровывает токен, созданный Encrypt.
func (b *SecretBox) Decrypt(token string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(token)
	if err != nil {
		return "", ErrDecrypt
	}
	size := b.aead.NonceSize()
	if len(raw) < size+b.aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// Fingerprint — короткий необратимый отпечаток ключа для интерфейса:
// пользователь отличает свои ключи, а восстановить ключ нельзя.
func Fingerprint(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])[:fingerprintLength]
}

// Last4 — последние четыре символа ключа.
func Last4(plain string) string {
	runes := []rune(plain)
	if len(runes) <= 4 {
		return plain
	}
	return string(runes[len(runes)-4:])
}
