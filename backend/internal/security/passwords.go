// Package security собирает механизмы защиты: пароли, секреты, сессии,
// ограничение попыток входа. По файлу на механизм.
package security

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// HashPassword хеширует пароль Argon2id (ТЗ §13).
//
// Параметры библиотеки по умолчанию сознательно не переопределяются,
// чтобы обновлялись вместе с ней.
func HashPassword(plain string) (string, error) {
	hash, err := argon2id.CreateHash(plain, argon2id.DefaultParams)
	if err != nil {
		return "", fmt.Errorf("хеширование пароля: %w", err)
	}
	return hash, nil
}

// VerifyPassword сверяет пароль с хешем.
//
// Никогда не возвращает ошибку: повреждённая или пустая запись в БД
// должна означать «не совпало», а не падение процесса входа.
func VerifyPassword(plain, encoded string) bool {
	if encoded == "" {
		return false
	}
	match, err := argon2id.ComparePasswordAndHash(plain, encoded)
	return err == nil && match
}
