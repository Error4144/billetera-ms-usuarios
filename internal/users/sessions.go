package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"billetera.local/ms-usuarios/internal/security"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnauthenticated = errors.New("credenciales o sesión inválidas")
	ErrRateLimited     = errors.New("límite temporal de acceso alcanzado")
	ErrConflict        = errors.New("versión desactualizada")
)

type SessionPolicy struct{ TTL, Idle time.Duration }
type Session struct {
	UserID, ID string
	ExpiresAt  time.Time
}
type LoginResult struct {
	Token   string
	Session Session
}

func NewWithPolicy(pool *pgxpool.Pool, policy SessionPolicy) (*Service, error) {
	if policy.TTL <= 0 || policy.Idle <= 0 || policy.Idle > policy.TTL {
		return nil, errors.New("duración de sesión inválida")
	}
	return &Service{pool: pool, policy: policy}, nil
}

// Dummy hash has the same work factor and verifier length as actual passwords.
const dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	var result LoginResult
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) == 0 || len(email) > 254 || security.ValidatePassword(password) != nil {
		return result, ErrUnauthenticated
	}
	key := sha256.Sum256([]byte(email))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	// A database lock serializes attempts for the same identity across replicas.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,4144))", email); err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO limites_login(correo_hash,inicio_ventana) VALUES($1,$2) ON CONFLICT DO NOTHING", key[:], now); err != nil {
		return result, err
	}
	var start time.Time
	var failed int
	var blocked *time.Time
	if err = tx.QueryRow(ctx, "SELECT inicio_ventana,fallos,bloqueado_hasta FROM limites_login WHERE correo_hash=$1 FOR UPDATE", key[:]).Scan(&start, &failed, &blocked); err != nil {
		return result, err
	}
	if blocked != nil && now.Before(*blocked) {
		return result, ErrRateLimited
	}
	if !now.Before(start.Add(15*time.Minute)) || blocked != nil {
		start = now
		failed = 0
	}
	var userID, stored string
	err = tx.QueryRow(ctx, "SELECT id::text,hash_contrasena FROM usuarios WHERE email=$1", email).Scan(&userID, &stored)
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return result, err
	}
	if missing {
		stored = dummyHash
	}
	valid := security.VerifyPassword(password, stored)
	if !valid || missing {
		failed++
		var until *time.Time
		if failed >= 5 {
			value := now.Add(15 * time.Minute)
			until = &value
		}
		if _, err = tx.Exec(ctx, "UPDATE limites_login SET inicio_ventana=$2,fallos=$3,bloqueado_hasta=$4 WHERE correo_hash=$1", key[:], start, failed, until); err != nil {
			return result, err
		}
		if err = tx.Commit(ctx); err != nil {
			return result, err
		}
		return result, ErrUnauthenticated
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return result, err
	}
	hash := sha256.Sum256(raw)
	id, err := uuid()
	if err != nil {
		return result, err
	}
	// Use database time after password verification, not a client-provided timestamp.
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return result, err
	}
	absolute := now.Add(s.policy.TTL)
	if _, err = tx.Exec(ctx, `INSERT INTO sesiones(id,usuario_id,hash_token,creado_en,ultima_actividad_en,expira_en)
  VALUES($1,$2,$3,$4,$4,$5)`, id, userID, hash[:], now, absolute); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM limites_login WHERE correo_hash=$1", key[:]); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return LoginResult{Token: base64.RawURLEncoding.EncodeToString(raw), Session: Session{UserID: userID, ID: id, ExpiresAt: minTime(absolute, now.Add(s.policy.Idle))}}, nil
}

func tokenHash(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, ErrUnauthenticated
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// The session row remains locked until the protected operation is committed.
// Logout and protected writes therefore cannot race past each other on this session.
func (s *Service) withSession(ctx context.Context, token string, operation func(pgx.Tx, Session) error) (Session, error) {
	hash, err := tokenHash(token)
	if err != nil {
		return Session{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(context.Background())
	var session Session
	var expires, last time.Time
	var revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT id::text,usuario_id::text,expira_en,ultima_actividad_en,revocado_en
  FROM sesiones WHERE hash_token=$1 FOR UPDATE`, hash).Scan(&session.ID, &session.UserID, &expires, &last, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return Session{}, err
	}
	if revoked != nil || !now.Before(expires) || !now.Before(last.Add(s.policy.Idle)) {
		return Session{}, ErrUnauthenticated
	}
	// Avoid moving activity backwards on a server clock adjustment.
	if now.Before(last) {
		now = last
	}
	session.ExpiresAt = minTime(expires, now.Add(s.policy.Idle))
	if _, err = tx.Exec(ctx, "UPDATE sesiones SET ultima_actividad_en=$2 WHERE hash_token=$1", hash, now); err != nil {
		return Session{}, err
	}
	if operation != nil {
		if err = operation(tx, session); err != nil {
			return Session{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return session, nil
}
func (s *Service) ValidateSession(ctx context.Context, token string) (Session, error) {
	return s.withSession(ctx, token, nil)
}
func (s *Service) Logout(ctx context.Context, token string) error {
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, "UPDATE sesiones SET revocado_en=clock_timestamp() WHERE hash_token=$1 AND revocado_en IS NULL", hash)
	return err
}
