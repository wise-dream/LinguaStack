package telegram

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"
)

// LinkCodeTTL — срок действия кода привязки (дополнение §17).
const LinkCodeTTL = 15 * time.Minute

// LinkCode — новый код привязки для ссылки t.me/<бот>?start=<код>: 32
// случайных байта в base64url (43 символа; Telegram допускает до 64 из
// [A-Za-z0-9_-]). В БД хранится только хеш.
func LinkCode() (code string, hash []byte) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // crypto/rand.Read не возвращает ошибок
	code = base64.RawURLEncoding.EncodeToString(raw)
	return code, HashCode(code)
}

// HashCode — хеш кода привязки для поиска в БД.
func HashCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// LinkURL — ссылка, открывающая бота с кодом.
func LinkURL(botUsername, code string) string {
	return "https://t.me/" + botUsername + "?start=" + code
}
