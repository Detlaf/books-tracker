// Package store implements auth.Store over SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/kate/book-tracking/internal/auth"
)

type SQLite struct {
	db *sql.DB
}

func New(db *sql.DB) *SQLite { return &SQLite{db: db} }

func (s *SQLite) CreateUser(ctx context.Context, email, passwordHash string) (auth.User, error) {
	const q = `INSERT INTO users (email, password_hash) VALUES (?, ?)`

	res, err := s.db.ExecContext(ctx, q, email, passwordHash)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			return auth.User{}, auth.ErrEmailTaken
		}
		return auth.User{}, fmt.Errorf("create user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return auth.User{}, fmt.Errorf("create user: %w", err)
	}
	return auth.User{ID: id, Email: email, PasswordHash: passwordHash}, nil
}

func (s *SQLite) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	const q = `SELECT id, email, password_hash, created_at FROM users WHERE email = ?`

	var u auth.User
	err := s.db.QueryRowContext(ctx, q, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("user by email: %w", err)
	}
	return u, nil
}

func (s *SQLite) CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	const q = `INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`

	if _, err := s.db.ExecContext(ctx, q, userID, tokenHash, expiresAt.UTC()); err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// RefreshTokenByHash returns the row whatever its state. Filtering revoked or
// expired rows here would collapse "unknown" and "revoked" into one result and
// break reuse detection; the service inspects the state fields instead.
func (s *SQLite) RefreshTokenByHash(ctx context.Context, tokenHash string) (auth.RefreshToken, error) {
	const q = `SELECT user_id, token_hash, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = ?`

	var (
		rt        auth.RefreshToken
		revokedAt sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, q, tokenHash).
		Scan(&rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.RefreshToken{}, auth.ErrNotFound
	}
	if err != nil {
		return auth.RefreshToken{}, fmt.Errorf("refresh token by hash: %w", err)
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		rt.RevokedAt = &t
	}
	return rt, nil
}

// RevokeRefreshToken is a no-op when no row matches, which is what makes
// logout idempotent.
func (s *SQLite) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`

	if _, err := s.db.ExecContext(ctx, q, time.Now().UTC(), tokenHash); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *SQLite) RevokeAllForUser(ctx context.Context, userID int64) error {
	const q = `UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`

	if _, err := s.db.ExecContext(ctx, q, time.Now().UTC(), userID); err != nil {
		return fmt.Errorf("revoke all for user: %w", err)
	}
	return nil
}
