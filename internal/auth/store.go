package auth

import (
	"context"
	"strings"
	"time"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

type RefreshToken struct {
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	// RevokedAt is nil while the token is live. A non-nil value on a token
	// that is presented again means the token was replayed.
	RevokedAt *time.Time
}

// NormalizeEmail is applied before every store read and write so that
// "A@B.com" and "a@b.com" are one account. Without it the UNIQUE constraint on
// users.email would allow case-variant duplicates.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Store is the persistence the auth service needs. Implementations must:
//   - return ErrNotFound, and only ErrNotFound, when no row matches;
//   - return ErrEmailTaken from CreateUser on a duplicate email;
//   - return the row from RefreshTokenByHash regardless of its revoked_at or
//     expires_at state — filtering those in SQL would make a revoked token
//     indistinguishable from an unknown one, and reuse detection depends on
//     telling them apart;
//   - return nil from RevokeRefreshToken when no row matches, so that logout
//     is idempotent.
type Store interface {
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RefreshTokenByHash(ctx context.Context, tokenHash string) (RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	RevokeAllForUser(ctx context.Context, userID int64) error
}
