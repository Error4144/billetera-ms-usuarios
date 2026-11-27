package tests

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"billetera.local/ms-usuarios/internal/users"
	"billetera.local/ms-usuarios/migrations"
)

const accountPassword = "una contraseña larga para sesiones"

func TestSessionsProfilesAndPreferences(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := users.New(pool)
	a, err := service.Register(ctx, users.RegisterInput{Email: "a@example.test", Name: "Persona A", Password: accountPassword})
	if err != nil {
		t.Fatal(err)
	}
	b, err := service.Register(ctx, users.RegisterInput{Email: "b@example.test", Name: "Persona B", Password: accountPassword})
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"a@example.test", "noexiste@example.test"} {
		if _, err = service.Login(ctx, email, "contraseña incorrecta suficientemente larga"); !errors.Is(err, users.ErrUnauthenticated) {
			t.Fatal("login inválido aceptado")
		}
	}
	first, err := service.Login(ctx, " A@Example.test ", accountPassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(ctx, "a@example.test", accountPassword)
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Login(ctx, "b@example.test", accountPassword)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.Session.ID == second.Session.ID {
		t.Fatal("sesiones deben ser independientes")
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.Token)
	if err != nil || len(raw) != 32 {
		t.Fatal("token incorrecto")
	}
	expected := sha256.Sum256(raw)
	var stored []byte
	if err = pool.QueryRow(ctx, "SELECT hash_token FROM sesiones WHERE id=$1", first.Session.ID).Scan(&stored); err != nil || string(stored) != string(expected[:]) || string(stored) == first.Token {
		t.Fatal("token no protegido en almacenamiento")
	}
	// A new service instance still validates the persisted session.
	if session, err := users.New(pool).ValidateSession(ctx, first.Token); err != nil || session.UserID != a.Profile.ID {
		t.Fatal("sesión no persistida")
	}
	profile, err := service.GetProfile(ctx, first.Token)
	if err != nil || profile.ID != a.Profile.ID {
		t.Fatal("perfil propio incorrecto")
	}
	updated, err := service.UpdateProfile(ctx, first.Token, " Nombre nuevo ", profile.Version)
	if err != nil || updated.Name != "Nombre nuevo" || updated.Version != 2 {
		t.Fatal("actualización de perfil incorrecta")
	}
	if _, err = service.UpdateProfile(ctx, second.Token, "sobrescritura", 1); !errors.Is(err, users.ErrConflict) {
		t.Fatal("se aceptó versión de perfil antigua")
	}
	if _, err = service.UpdateProfile(ctx, first.Token, " ", 2); !errors.Is(err, users.ErrInvalid) {
		t.Fatal("nombre vacío aceptado")
	}
	noChange, err := service.UpdateProfile(ctx, first.Token, "Nombre nuevo", 2)
	if err != nil || noChange.Version != 2 {
		t.Fatal("perfil sin cambios incrementó versión")
	}
	profileB, err := service.GetProfile(ctx, other.Token)
	if err != nil || profileB.ID != b.Profile.ID || profileB.Name != "Persona B" {
		t.Fatal("se alteró perfil ajeno")
	}
	prefs, err := service.GetPreferences(ctx, first.Token)
	if err != nil {
		t.Fatal(err)
	}
	input := prefs
	input.TimeZone = "Pacific/Auckland"
	input.MaximumAmount = 9007199254740993
	changed, err := service.UpdatePreferences(ctx, first.Token, input, prefs.Version)
	if err != nil || changed.Version != 2 || changed.MaximumAmount != 9007199254740993 {
		t.Fatal("preferencias o precisión incorrectas")
	}
	unchanged, err := service.UpdatePreferences(ctx, first.Token, input, 2)
	if err != nil || unchanged.Version != 2 {
		t.Fatal("no-op alteró versión")
	}
	var events int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_eventos WHERE usuario_id=$1 AND tipo='PreferenciasActualizadas'", a.Profile.ID).Scan(&events); err != nil || events != 1 {
		t.Fatal("evento de preferencias duplicado")
	}
	var version, zone string
	if err = pool.QueryRow(ctx, "SELECT datos->>'versionPreferencias',datos->>'zonaHorariaCambio' FROM outbox_eventos WHERE usuario_id=$1 AND tipo='PreferenciasActualizadas'", a.Profile.ID).Scan(&version, &zone); err != nil || version != "2" || zone != "true" {
		t.Fatal("evento incorrecto")
	}
	if _, err = service.UpdatePreferences(ctx, first.Token, prefs, 1); !errors.Is(err, users.ErrConflict) {
		t.Fatal("se aceptó versión vieja")
	}
	for _, invalid := range []users.Preferences{
		{Currency: "USD", TimeZone: "UTC", MaximumAmount: 1, MinimumCount: 1},
		{Currency: "CLP", TimeZone: "Local", MaximumAmount: 1, MinimumCount: 1},
		{Currency: "CLP", TimeZone: "invalida/zona", MaximumAmount: 1, MinimumCount: 1},
		{Currency: "CLP", TimeZone: "UTC", MaximumAmount: 0, MinimumCount: 1},
		{Currency: "CLP", TimeZone: "UTC", MaximumAmount: 1, MinimumCount: 0},
	} {
		if _, err = service.UpdatePreferences(ctx, first.Token, invalid, 2); !errors.Is(err, users.ErrInvalid) {
			t.Fatal("preferencia inválida aceptada")
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for index, token := range []string{first.Token, second.Token} {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			value := input
			value.MaximumAmount = int64(4000 + i)
			_, err := service.UpdatePreferences(ctx, token, value, 2)
			results <- err
		}(index, token)
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, users.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal("edición concurrente perdió control de versión")
	}
	prefsB, err := service.GetPreferences(ctx, other.Token)
	if err != nil || prefsB.Version != 1 || prefsB.MaximumAmount != 3000 {
		t.Fatal("se alteraron preferencias ajenas")
	}
	// Force event persistence to fail: preference changes must also roll back.
	if _, err = pool.Exec(ctx, "ALTER TABLE outbox_eventos ADD CONSTRAINT fallo_preferencias CHECK (tipo <> 'PreferenciasActualizadas') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	input.MaximumAmount = 7000
	if _, err = service.UpdatePreferences(ctx, second.Token, input, 3); err == nil {
		t.Fatal("se ignoró fallo de outbox")
	}
	after, err := service.GetPreferences(ctx, second.Token)
	if err != nil || after.Version != 3 || after.MaximumAmount == 7000 {
		t.Fatal("preferencias parciales después del fallo")
	}
	if err = service.Logout(ctx, first.Token); err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, first.Token); err != nil {
		t.Fatal("logout repetido falló")
	}
	if _, err = service.ValidateSession(ctx, first.Token); !errors.Is(err, users.ErrUnauthenticated) {
		t.Fatal("sesión revocada aceptada")
	}
	if _, err = service.GetProfile(ctx, first.Token); !errors.Is(err, users.ErrUnauthenticated) {
		t.Fatal("perfil accesible después de logout")
	}
	if _, err = service.ValidateSession(ctx, second.Token); err != nil {
		t.Fatal("logout revocó otra sesión")
	}
	unknown := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if err = service.Logout(ctx, unknown); err != nil {
		t.Fatal("logout de token canónico desconocido no es idempotente")
	}
	for _, bad := range []string{"", first.Token + "=", strings.Repeat("!", 43), "malformado"} {
		if _, err = service.ValidateSession(ctx, bad); !errors.Is(err, users.ErrUnauthenticated) {
			t.Fatal("token inválido aceptado")
		}
		if err = service.Logout(ctx, bad); !errors.Is(err, users.ErrUnauthenticated) {
			t.Fatal("logout aceptó formato inválido")
		}
	}
}

func TestExpirationAndLoginCooldown(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := users.New(pool)
	if _, err := service.Register(ctx, users.RegisterInput{Email: "limits@example.test", Name: "Prueba", Password: accountPassword}); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"limits@example.test", "missing@example.test"} {
		for range 5 {
			if _, err := service.Login(ctx, email, "contraseña errónea de pruebas"); !errors.Is(err, users.ErrUnauthenticated) {
				t.Fatal("respuesta de credenciales incorrecta")
			}
		}
		if _, err := users.New(pool).Login(ctx, email, accountPassword); !errors.Is(err, users.ErrRateLimited) {
			t.Fatal("límite no persiste entre instancias")
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE limites_login SET bloqueado_hasta=clock_timestamp()-INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	logged, err := service.Login(ctx, "limits@example.test", accountPassword)
	if err != nil {
		t.Fatal("no se recuperó el acceso tras enfriamiento")
	}
	if _, err = pool.Exec(ctx, `UPDATE sesiones SET creado_en=clock_timestamp()-INTERVAL '1 hour',ultima_actividad_en=clock_timestamp()-INTERVAL '31 minutes'
  WHERE id=$1`, logged.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ValidateSession(ctx, logged.Token); !errors.Is(err, users.ErrUnauthenticated) {
		t.Fatal("inactividad vencida aceptada")
	}
	logged, err = service.Login(ctx, "limits@example.test", accountPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE sesiones SET expira_en=clock_timestamp(),ultima_actividad_en=creado_en WHERE id=$1", logged.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ValidateSession(ctx, logged.Token); !errors.Is(err, users.ErrUnauthenticated) {
		t.Fatal("expiración absoluta aceptada")
	}
	if _, err = users.NewWithPolicy(pool, users.SessionPolicy{TTL: time.Minute, Idle: time.Hour}); err == nil {
		t.Fatal("política inválida aceptada")
	}
	if _, err = service.ValidateSession(ctx, base64.RawURLEncoding.EncodeToString(make([]byte, 32))); !errors.Is(err, users.ErrUnauthenticated) {
		t.Fatal("token desconocido aceptado")
	}
}
