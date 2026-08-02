package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/auth"
	"github.com/kate/book-tracking/internal/db"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return New(database)
}

func TestCreateAndFetchUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "a@b.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("CreateUser must return the assigned ID")
	}

	found, err := s.UserByEmail(ctx, "a@b.com")
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if found.ID != created.ID || found.PasswordHash != "hash" {
		t.Fatalf("round trip mismatch: %+v vs %+v", found, created)
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "a@b.com", "hash"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateUser(ctx, "a@b.com", "other")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestUserByEmailNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UserByEmail(context.Background(), "nobody@example.com")
	if !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRefreshTokenLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user, err := s.CreateUser(ctx, "a@b.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	if err := s.CreateRefreshToken(ctx, user.ID, "hash1", expiresAt); err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	got, err := s.RefreshTokenByHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("RefreshTokenByHash: %v", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("UserID = %d, want %d", got.UserID, user.ID)
	}
	if got.RevokedAt != nil {
		t.Fatal("a new token must not be revoked")
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}

	if got.RevokedReason != "" {
		t.Fatalf("RevokedReason = %q, want empty on a live token", got.RevokedReason)
	}

	revoked, err := s.RevokeRefreshToken(ctx, "hash1", auth.RevokeReasonRotated)
	if err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	if !revoked {
		t.Fatal("revoking a live token must report revoked = true")
	}

	// The revoked row must still come back — reuse detection depends on it.
	got, err = s.RefreshTokenByHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("a revoked token must still be returned, got: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("RevokedAt must be set after revocation")
	}
	if got.RevokedReason != auth.RevokeReasonRotated {
		t.Fatalf("RevokedReason = %q, want %q", got.RevokedReason, auth.RevokeReasonRotated)
	}
}

// Only the first revoke wins. That bool is what stops two concurrent refreshes
// of the same token from both being served.
func TestRevokeRefreshTokenIsWonOnlyOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user, err := s.CreateUser(ctx, "a@b.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRefreshToken(ctx, user.ID, "hash1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if revoked, err := s.RevokeRefreshToken(ctx, "hash1", auth.RevokeReasonRotated); err != nil || !revoked {
		t.Fatalf("first revoke: revoked = %v, err = %v; want true, nil", revoked, err)
	}
	revoked, err := s.RevokeRefreshToken(ctx, "hash1", auth.RevokeReasonLogout)
	if err != nil {
		t.Fatalf("second revoke must not error, got: %v", err)
	}
	if revoked {
		t.Fatal("the second revoke must report revoked = false")
	}

	// The losing revoke must not overwrite why the token was first revoked.
	got, err := s.RefreshTokenByHash(ctx, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RevokedReason != auth.RevokeReasonRotated {
		t.Fatalf("RevokedReason = %q, want it unchanged at %q", got.RevokedReason, auth.RevokeReasonRotated)
	}
}

func TestRevokeRefreshTokenUnknownIsNoOp(t *testing.T) {
	s := newTestStore(t)
	revoked, err := s.RevokeRefreshToken(context.Background(), "nosuchhash", auth.RevokeReasonLogout)
	if err != nil {
		t.Fatalf("revoking an unknown token must succeed, got: %v", err)
	}
	if revoked {
		t.Fatal("an unknown hash must report revoked = false")
	}
}

func TestRefreshTokenByHashNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.RefreshTokenByHash(context.Background(), "nosuchhash")
	if !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRevokeAllForUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	user, err := s.CreateUser(ctx, "a@b.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour)
	for _, h := range []string{"h1", "h2", "h3"} {
		if err := s.CreateRefreshToken(ctx, user.ID, h, expiresAt); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RevokeAllForUser(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	for _, h := range []string{"h1", "h2", "h3"} {
		got, err := s.RefreshTokenByHash(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		if got.RevokedAt == nil {
			t.Errorf("token %s should be revoked", h)
		}
	}
}

// Compile-time check that SQLite satisfies the interface the service needs.
var _ auth.Store = (*SQLite)(nil)
