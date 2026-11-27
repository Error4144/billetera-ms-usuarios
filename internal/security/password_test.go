package security

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	password := "  una frase larga con ñ y espacios  "
	a, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("la sal debe variar por contraseña")
	}
	if !VerifyPassword(password, a) {
		t.Fatal("contraseña original rechazada")
	}
	if VerifyPassword(strings.TrimSpace(password), a) || VerifyPassword("otra frase de contraseña", a) {
		t.Fatal("la contraseña se alteró o no se comparó correctamente")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=999999999,t=2,p=1$a$b", phcPrefix + "abc$def", a + "$extra"} {
		if VerifyPassword(password, bad) {
			t.Fatal("hash corrupto aceptado")
		}
	}
}

func TestPasswordBounds(t *testing.T) {
	for _, bad := range []string{"corta", strings.Repeat("a", 129), string([]byte{0xff})} {
		if ValidatePassword(bad) == nil {
			t.Fatal("contraseña inválida aceptada")
		}
		if _, err := HashPassword(bad); err == nil {
			t.Fatal("se creó un hash para entrada inválida")
		}
	}
	for _, good := range []string{strings.Repeat("ñ", 15), strings.Repeat("a", 128)} {
		if err := ValidatePassword(good); err != nil {
			t.Fatal(err)
		}
	}
}
