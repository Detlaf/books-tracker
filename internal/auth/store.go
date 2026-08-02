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

// Reasons a refresh token was revoked. Only RevokeReasonRotated marks a token
// whose replay is evidence of theft; see Service.Refresh.
const (
	RevokeReasonRotated = "rotated"
	RevokeReasonLogout  = "logout"
)

type RefreshToken struct {
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	// RevokedAt is nil while the token is live. A non-nil value on a token
	// that is presented again means the token was replayed.
	RevokedAt *time.Time
	// RevokedReason says why, so that replaying a rotated token can be treated
	// as theft while replaying a logged-out one is merely refused. It is empty
	// for live tokens and for rows written before the reason was recorded;
	// empty must never be read as evidence of theft.
	RevokedReason string
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
//   - make RevokeRefreshToken atomic: it must revoke only a live row, and
//     report whether this call is the one that revoked it. A hash that is
//     unknown or already revoked yields (false, nil), not an error — rotation
//     decides on that bool, and logout relies on it to stay idempotent.
type Store interface {
	CreateUser(ctx context.Context, email, passwordHash string) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	CreateRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	RefreshTokenByHash(ctx context.Context, tokenHash string) (RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash, reason string) (revoked bool, err error)
	RevokeAllForUser(ctx context.Context, userID int64) error
}
