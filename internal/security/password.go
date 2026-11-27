package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var ErrPassword = errors.New("contraseña debe tener entre 15 y 128 caracteres Unicode válidos")

const phcPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrPassword
	}
	n := utf8.RuneCountInString(password)
	if n < 15 || n > 128 {
		return ErrPassword
	}
	return nil
}

func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return phcPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}

func VerifyPassword(password, encoded string) bool {
	if ValidatePassword(password) != nil || !strings.HasPrefix(encoded, phcPrefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, phcPrefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(expected) != 32 {
		return false
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	return subtle.ConstantTimeCompare(hash, expected) == 1
}
