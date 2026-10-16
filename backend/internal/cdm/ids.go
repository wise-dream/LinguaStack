// Package cdm — Canonical Document Model (ТЗ §4.1): единое внутреннее
// представление, к которому импортёры приводят любой формат.
package cdm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Схема идентификаторов — дополнение §1. Две разные задачи решаются двумя
// значениями: NodeID стабилен к правке текста блока (позиционный), а
// ContentHash обнаруживает саму правку. При повторном импорте NodeID
// совпадает, и по хешу видно, устарел ли перевод.

// NodeID — позиционный идентификатор блока, читаемый в логах.
func NodeID(chapterIndex, ordinal int) string {
	return fmt.Sprintf("ch%04d/b%04d", chapterIndex, ordinal)
}

// NormalizeText — NFC и свёртка пробельных символов, чтобы
// переформатирование не меняло хеш.
func NormalizeText(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(s)), " ")
}

// ContentHash — отпечаток содержимого блока: первые 16 байт sha256 в hex.
func ContentHash(s string) string {
	sum := sha256.Sum256([]byte(NormalizeText(s)))
	return hex.EncodeToString(sum[:16])
}
