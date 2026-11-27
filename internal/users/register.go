package users

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"billetera.local/ms-usuarios/internal/security"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalid     = errors.New("datos de registro inválidos")
	ErrEmailExists = errors.New("correo ya registrado")
)

type RegisterInput struct{ Email, Password, Name string }
type Profile struct {
	ID, Email, Name      string
	Version              int64
	CreatedAt, UpdatedAt time.Time
}
type Preferences struct {
	Currency, TimeZone string
	MaximumAmount      int64
	MinimumCount       int32
	Version            int64
	UpdatedAt          time.Time
}
type Registration struct {
	Profile     Profile
	Preferences Preferences
}
type Service struct {
	pool   *pgxpool.Pool
	policy SessionPolicy
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, policy: SessionPolicy{TTL: 24 * time.Hour, Idle: 30 * time.Minute}}
}

func uuid() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func normalize(input RegisterInput) (RegisterInput, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 60 || len(input.Email) > 254 {
		return input, ErrInvalid
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || !strings.Contains(input.Email, "@") {
		return input, ErrInvalid
	}
	if err := security.ValidatePassword(input.Password); err != nil {
		return input, errors.Join(ErrInvalid, err)
	}
	return input, nil
}

// Register commits identity, initial preferences and the publishable event together.
// Kafka availability is deliberately independent of the local transaction.
func (s *Service) Register(ctx context.Context, input RegisterInput) (Registration, error) {
	var result Registration
	input, err := normalize(input)
	if err != nil {
		return result, err
	}
	hash, err := security.HashPassword(input.Password)
	if err != nil {
		return result, err
	}
	userID, err := uuid()
	if err != nil {
		return result, err
	}
	eventID, err := uuid()
	if err != nil {
		return result, err
	}
	correlationID, err := uuid()
	if err != nil {
		return result, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(ctx, `INSERT INTO usuarios(id,nombre,email,hash_contrasena)
        VALUES($1,$2,$3,$4) RETURNING id::text,nombre,email,version,creado_en,actualizado_en`,
		userID, input.Name, input.Email, hash).Scan(&result.Profile.ID, &result.Profile.Name, &result.Profile.Email,
		&result.Profile.Version, &result.Profile.CreatedAt, &result.Profile.UpdatedAt)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" && pgerr.ConstraintName == "usuarios_email_key" {
			return Registration{}, ErrEmailExists
		}
		return Registration{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO preferencias_usuario(usuario_id) VALUES($1)
        RETURNING moneda,zona_horaria,monto_maximo_hormiga,frecuencia_minima,version,actualizado_en`,
		userID).Scan(&result.Preferences.Currency, &result.Preferences.TimeZone, &result.Preferences.MaximumAmount,
		&result.Preferences.MinimumCount, &result.Preferences.Version, &result.Preferences.UpdatedAt)
	if err != nil {
		return Registration{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_eventos(evento_id,usuario_id,tipo,agregado_id,correlacion_id,datos)
        VALUES($1,$2,'UsuarioRegistrado',$2,$3,'{}'::jsonb)`, eventID, userID, correlationID)
	if err != nil {
		return Registration{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return result, nil
}
