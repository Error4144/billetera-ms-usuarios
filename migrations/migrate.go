package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// Check validates history without modifying the database when the server starts.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	paths, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	for _, path := range paths {
		sql, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(sql)
		var checksum string
		if err = pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", path).Scan(&checksum); err != nil {
			return err
		}
		if checksum != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("migration %s checksum mismatch", path)
		}
	}
	return nil
}

// Apply serializes migrations and records their checksums in the same transaction.
// A failed migration leaves neither partial tables nor a successful version entry.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(41440001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version TEXT PRIMARY KEY, checksum TEXT NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`); err != nil {
		return err
	}
	paths, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		sql, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(sql)
		checksum := hex.EncodeToString(sum[:])
		var previous string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", path).Scan(&previous)
		if err == nil {
			if checksum != previous {
				return fmt.Errorf("migration %s checksum mismatch", path)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", path, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", path, checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
