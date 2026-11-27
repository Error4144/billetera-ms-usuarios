package users

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func readProfile(ctx context.Context, tx pgx.Tx, id string) (Profile, error) {
	var profile Profile
	err := tx.QueryRow(ctx, "SELECT id::text,email,nombre,version,creado_en,actualizado_en FROM usuarios WHERE id=$1 FOR UPDATE", id).Scan(&profile.ID, &profile.Email, &profile.Name, &profile.Version, &profile.CreatedAt, &profile.UpdatedAt)
	return profile, err
}
func (s *Service) GetProfile(ctx context.Context, token string) (Profile, error) {
	var result Profile
	_, err := s.withSession(ctx, token, func(tx pgx.Tx, session Session) error {
		var err error
		result, err = readProfile(ctx, tx, session.UserID)
		return err
	})
	return result, err
}
func (s *Service) UpdateProfile(ctx context.Context, token, name string, expected int64) (Profile, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 60 || expected < 1 {
		return Profile{}, ErrInvalid
	}
	var result Profile
	_, err := s.withSession(ctx, token, func(tx pgx.Tx, session Session) error {
		var err error
		result, err = readProfile(ctx, tx, session.UserID)
		if err != nil {
			return err
		}
		if result.Version != expected {
			return ErrConflict
		}
		if result.Name == name {
			return nil
		}
		if result.Version == math.MaxInt64 {
			return ErrConflict
		}
		err = tx.QueryRow(ctx, `UPDATE usuarios SET nombre=$2,version=version+1,actualizado_en=GREATEST(actualizado_en,clock_timestamp())
   WHERE id=$1 RETURNING nombre,version,actualizado_en`, session.UserID, name).Scan(&result.Name, &result.Version, &result.UpdatedAt)
		return err
	})
	return result, err
}
func readPreferences(ctx context.Context, tx pgx.Tx, id string) (Preferences, error) {
	var p Preferences
	err := tx.QueryRow(ctx, `SELECT moneda,zona_horaria,monto_maximo_hormiga,frecuencia_minima,version,actualizado_en
  FROM preferencias_usuario WHERE usuario_id=$1 FOR UPDATE`, id).Scan(&p.Currency, &p.TimeZone, &p.MaximumAmount, &p.MinimumCount, &p.Version, &p.UpdatedAt)
	return p, err
}
func (s *Service) GetPreferences(ctx context.Context, token string) (Preferences, error) {
	var result Preferences
	_, err := s.withSession(ctx, token, func(tx pgx.Tx, session Session) error {
		var err error
		result, err = readPreferences(ctx, tx, session.UserID)
		return err
	})
	return result, err
}
func (s *Service) UpdatePreferences(ctx context.Context, token string, input Preferences, expected int64) (Preferences, error) {
	if input.Currency != "CLP" || input.TimeZone == "" || input.TimeZone == "Local" || strings.TrimSpace(input.TimeZone) != input.TimeZone || input.MaximumAmount <= 0 || input.MinimumCount <= 0 || expected < 1 {
		return Preferences{}, ErrInvalid
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return Preferences{}, ErrInvalid
	}
	var result Preferences
	_, err := s.withSession(ctx, token, func(tx pgx.Tx, session Session) error {
		current, err := readPreferences(ctx, tx, session.UserID)
		if err != nil {
			return err
		}
		if current.Version != expected {
			return ErrConflict
		}
		result = current
		if current.Currency == input.Currency && current.TimeZone == input.TimeZone && current.MaximumAmount == input.MaximumAmount && current.MinimumCount == input.MinimumCount {
			return nil
		}
		if current.Version == math.MaxInt64 {
			return ErrConflict
		}
		err = tx.QueryRow(ctx, `UPDATE preferencias_usuario SET zona_horaria=$2,monto_maximo_hormiga=$3,frecuencia_minima=$4,
   version=version+1,actualizado_en=GREATEST(actualizado_en,clock_timestamp()) WHERE usuario_id=$1
   RETURNING moneda,zona_horaria,monto_maximo_hormiga,frecuencia_minima,version,actualizado_en`, session.UserID, input.TimeZone, input.MaximumAmount, input.MinimumCount).Scan(&result.Currency, &result.TimeZone, &result.MaximumAmount, &result.MinimumCount, &result.Version, &result.UpdatedAt)
		if err != nil {
			return err
		}
		eventID, err := uuid()
		if err != nil {
			return err
		}
		correlation, err := uuid()
		if err != nil {
			return err
		}
		data, err := json.Marshal(map[string]any{"versionPreferencias": strconv.FormatInt(result.Version, 10), "zonaHorariaCambio": current.TimeZone != input.TimeZone})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO outbox_eventos(evento_id,usuario_id,tipo,agregado_id,correlacion_id,datos)
   VALUES($1,$2,'PreferenciasActualizadas',$2,$3,$4::jsonb)`, eventID, session.UserID, correlation, string(data))
		return err
	})
	return result, err
}
