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

	if err := s.RevokeRefreshToken(ctx, "hash1"); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}

	// The revoked row must still come back — reuse detection depends on it.
	got, err = s.RefreshTokenByHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("a revoked token must still be returned, got: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("RevokedAt must be set after revocation")
	}
}

func TestRevokeRefreshTokenUnknownIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.RevokeRefreshToken(context.Background(), "nosuchhash"); err != nil {
		t.Fatalf("revoking an unknown token must succeed, got: %v", err)
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
