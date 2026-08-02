# Authentication (Backend Milestone 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user register, log in, stay logged in on mobile for weeks, and have their session be revocable, with every later milestone's endpoints protected by one reusable middleware.

**Architecture:** Pure auth primitives (bcrypt hashing, JWT signing, refresh-token generation) live in `internal/auth` and import neither gin nor `database/sql`. A `Store` interface separates the auth service from persistence, implemented over SQLite in `internal/store`. Thin gin handlers in `internal/server` translate HTTP to service calls. Access is a 15-minute stateless JWT; refresh is a 30-day opaque random token stored as a SHA-256 hash so it can be revoked.

**Tech Stack:** Go 1.25, gin v1.12, SQLite (mattn/go-sqlite3), golang-migrate, golang-jwt/jwt/v5, golang.org/x/crypto/bcrypt.

**Spec:** `docs/superpowers/specs/2026-08-02-auth-design.md`

## Global Constraints

- Access token TTL: **15 minutes**. Refresh token TTL: **30 days**.
- bcrypt cost: **12**. Password length: **8–72 bytes inclusive**.
- `JWT_SECRET` is **required, minimum 32 bytes**, with no default in any environment.
- JWT algorithm is **HS256 only** — `ParseAccess` must pass `jwt.WithValidMethods`.
- Refresh tokens are **32 random bytes, base64url (raw, unpadded)**, stored as a **hex-encoded SHA-256** hash.
- Emails are **trimmed and lowercased** before every store write and every store read.
- Error responses are always `{"error": "message"}`.
- Login returns 401 `"invalid email or password"` for both unknown-email and wrong-password, and performs a dummy bcrypt comparison when the user is not found.
- `RefreshTokenByHash` returns rows **regardless of `revoked_at`/`expires_at`**; only a missing row is `ErrNotFound`.
- `RevokeRefreshToken` returns `nil` when no row matches — logout is idempotent.
- Every task ends with a passing `go build ./... && go test ./...` and a commit.

---

### Task 1: Configuration with a fail-fast signing key

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config{DSN, Addr string; JWTSecret []byte; AccessTTL, RefreshTTL time.Duration}` and `config.Load() (Config, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"strings"
	"testing"
	"time"
)

const validSecret = "0123456789abcdef0123456789abcdef" // exactly 32 bytes

func TestLoadRequiresSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when JWT_SECRET is unset")
	}
}

func TestLoadRejectsShortSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "too-short")
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for a short JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "32") {
		t.Fatalf("error should state the minimum length, got: %v", err)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", validSecret)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DSN != "book_tracking.db" {
		t.Errorf("DSN = %q, want book_tracking.db", cfg.DSN)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.AccessTTL != 15*time.Minute {
		t.Errorf("AccessTTL = %v, want 15m", cfg.AccessTTL)
	}
	if cfg.RefreshTTL != 30*24*time.Hour {
		t.Errorf("RefreshTTL = %v, want 720h", cfg.RefreshTTL)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", validSecret)
	t.Setenv("DATABASE_URL", "/tmp/other.db")
	t.Setenv("ADDR", ":9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DSN != "/tmp/other.db" || cfg.Addr != ":9999" {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if string(cfg.JWTSecret) != validSecret {
		t.Errorf("JWTSecret not carried through")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `undefined: Load` (build failure).

- [ ] **Step 3: Write minimal implementation**

Create `internal/config/config.go`:

```go
// Package config loads process configuration from the environment.
package config

import (
	"fmt"
	"os"
	"time"
)

// MinSecretLen is the shortest JWT_SECRET accepted. Shorter keys weaken HS256
// enough to be worth refusing outright.
const MinSecretLen = 32

type Config struct {
	DSN        string
	Addr       string
	JWTSecret  []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// Load reads configuration from the environment. It returns an error rather
// than falling back to a generated key when JWT_SECRET is missing: a default
// signing key is how a test secret reaches production.
func Load() (Config, error) {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < MinSecretLen {
		return Config{}, fmt.Errorf(
			"JWT_SECRET must be set and at least %d bytes; generate one with: openssl rand -base64 32",
			MinSecretLen)
	}
	return Config{
		DSN:        envOr("DATABASE_URL", "book_tracking.db"),
		Addr:       envOr("ADDR", ":8080"),
		JWTSecret:  []byte(secret),
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 30 * 24 * time.Hour,
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS — 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Add config loading with a required JWT signing key"
```

---

### Task 2: Password hashing

**Files:**
- Create: `internal/auth/password.go`
- Test: `internal/auth/password_test.go`
- Modify: `go.mod`, `go.sum` (promote `golang.org/x/crypto` to a direct dependency)

**Interfaces:**
- Consumes: nothing.
- Produces: `auth.HashPassword(string) (string, error)`, `auth.VerifyPassword(hash, password string) error`, `auth.VerifyDummyPassword()`, `auth.ErrPasswordLength`, constants `auth.MinPasswordLen = 8` and `auth.MaxPasswordLen = 72`.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get golang.org/x/crypto@latest
```
Expected: `go.mod` now lists `golang.org/x/crypto` without an `// indirect` comment after the next build.

- [ ] **Step 2: Write the failing test**

Create `internal/auth/password_test.go`:

```go
package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	const pw = "correct horse battery"

	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == pw {
		t.Fatal("hash must not equal the plaintext")
	}
	if err := VerifyPassword(hash, pw); err != nil {
		t.Fatalf("VerifyPassword on the correct password: %v", err)
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	a, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of the same password must differ (bcrypt salts each hash)")
	}
}

func TestVerifyPasswordRejectsWrong(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPassword(hash, "wrong password"); err == nil {
		t.Fatal("expected an error for the wrong password")
	}
}

func TestHashPasswordLengthLimits(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{"too short", "short"},
		{"empty", ""},
		{"over bcrypt's 72-byte limit", strings.Repeat("a", 73)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := HashPassword(tt.password); !errors.Is(err, ErrPasswordLength) {
				t.Fatalf("err = %v, want ErrPasswordLength", err)
			}
		})
	}
}

func TestHashPasswordAcceptsBoundaries(t *testing.T) {
	for _, n := range []int{MinPasswordLen, MaxPasswordLen} {
		if _, err := HashPassword(strings.Repeat("a", n)); err != nil {
			t.Fatalf("length %d should be accepted: %v", n, err)
		}
	}
}

func TestVerifyDummyPasswordDoesNotPanic(t *testing.T) {
	VerifyDummyPassword()
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -v`
Expected: FAIL — `undefined: HashPassword`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/auth/password.go`:

```go
// Package auth provides password hashing, token signing, and the
// authentication service. It depends on neither gin nor database/sql.
package auth

import (
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12

	// MinPasswordLen is a floor, not a policy: length is the only password
	// rule this app enforces.
	MinPasswordLen = 8
	// MaxPasswordLen is bcrypt's input limit. Longer input must be rejected
	// rather than silently truncated.
	MaxPasswordLen = 72
)

func HashPassword(password string) (string, error) {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return "", ErrPasswordLength
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// dummyHash is computed once, on first use, so that VerifyDummyPassword costs
// the same as a real comparison without adding startup latency.
var dummyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("dummy password for timing"), bcryptCost)
	if err != nil {
		panic("auth: cannot generate dummy hash: " + err.Error())
	}
	return hash
})

// VerifyDummyPassword burns the same time a real bcrypt comparison would.
// Login calls it when no user matches the email, so response time does not
// reveal whether an account exists.
func VerifyDummyPassword() {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte("wrong"))
}
```

Create `internal/auth/errors.go`:

```go
package auth

import "errors"

var (
	// ErrPasswordLength means the password was shorter than MinPasswordLen or
	// longer than MaxPasswordLen.
	ErrPasswordLength = errors.New("password must be between 8 and 72 bytes")
	// ErrInvalidCredentials covers both an unknown email and a wrong password.
	// The two are deliberately indistinguishable to the caller.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrEmailTaken means registration hit the unique constraint on users.email.
	ErrEmailTaken = errors.New("email already registered")
	// ErrInvalidToken covers missing, malformed, expired, and revoked tokens.
	ErrInvalidToken = errors.New("invalid token")
	// ErrNotFound is returned by a Store when no row matches.
	ErrNotFound = errors.New("not found")
)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS — 6 tests. Takes a few seconds; bcrypt at cost 12 is intentionally slow.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/auth
git commit -m "Add bcrypt password hashing with length limits"
```

---

### Task 3: Access JWTs and refresh token generation

**Files:**
- Create: `internal/auth/token.go`
- Test: `internal/auth/token_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: `auth.ErrInvalidToken` (Task 2).
- Produces: `auth.NewSigner(secret []byte, accessTTL time.Duration) *Signer`, `(*Signer).SignAccess(userID int64) (token string, expiresAt time.Time, err error)`, `(*Signer).ParseAccess(token string) (int64, error)`, `(*Signer).AccessTTL() time.Duration`, `auth.NewRefreshToken() (token, hash string, err error)`, `auth.HashRefreshToken(token string) string`. The unexported field `Signer.now func() time.Time` exists so tests can control expiry.

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get github.com/golang-jwt/jwt/v5@latest
```

- [ ] **Step 2: Write the failing test**

Create `internal/auth/token_test.go`:

```go
package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func TestSignAndParseAccess(t *testing.T) {
	s := NewSigner(testSecret, 15*time.Minute)

	token, expiresAt, err := s.SignAccess(42)
	if err != nil {
		t.Fatalf("SignAccess: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expiresAt %v should be in the future", expiresAt)
	}

	userID, err := s.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID = %d, want 42", userID)
	}
}

func TestParseAccessRejectsExpired(t *testing.T) {
	s := NewSigner(testSecret, 15*time.Minute)
	token, _, err := s.SignAccess(42)
	if err != nil {
		t.Fatal(err)
	}

	// Move the signer's clock past the expiry instead of sleeping.
	s.now = func() time.Time { return time.Now().Add(16 * time.Minute) }

	if _, err := s.ParseAccess(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseAccessRejectsWrongSecret(t *testing.T) {
	signed := NewSigner(testSecret, 15*time.Minute)
	token, _, err := signed.SignAccess(42)
	if err != nil {
		t.Fatal(err)
	}

	other := NewSigner([]byte("fedcba9876543210fedcba9876543210"), 15*time.Minute)
	if _, err := other.ParseAccess(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

// A token re-signed with "alg": "none" must be rejected. This is the classic
// JWT vulnerability: without WithValidMethods, some parsers accept it.
func TestParseAccessRejectsAlgNone(t *testing.T) {
	s := NewSigner(testSecret, 15*time.Minute)

	claims := jwt.RegisteredClaims{
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.ParseAccess(unsigned); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseAccessRejectsTampered(t *testing.T) {
	s := NewSigner(testSecret, 15*time.Minute)
	token, _, err := s.SignAccess(42)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}
	// Corrupt the payload; the signature no longer matches.
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]

	if _, err := s.ParseAccess(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseAccessRejectsGarbage(t *testing.T) {
	s := NewSigner(testSecret, 15*time.Minute)
	for _, token := range []string{"", "not-a-jwt", "a.b.c"} {
		if _, err := s.ParseAccess(token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("ParseAccess(%q) = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestNewRefreshTokenIsRandomAndHashed(t *testing.T) {
	token1, hash1, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	token2, hash2, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}

	if token1 == token2 {
		t.Fatal("two refresh tokens must differ")
	}
	if hash1 == token1 {
		t.Fatal("the stored hash must not equal the token")
	}
	if hash1 == hash2 {
		t.Fatal("hashes of different tokens must differ")
	}
	if got := HashRefreshToken(token1); got != hash1 {
		t.Fatalf("HashRefreshToken is not deterministic: %q vs %q", got, hash1)
	}
	// 32 random bytes as raw base64url.
	if len(token1) != 43 {
		t.Fatalf("token length = %d, want 43", len(token1))
	}
	// SHA-256 as hex.
	if len(hash1) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hash1))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -run 'Token|Access|Refresh' -v`
Expected: FAIL — `undefined: NewSigner`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/auth/token.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Signer issues and verifies access tokens. Access tokens are stateless JWTs:
// verifying one never touches the database.
type Signer struct {
	secret    []byte
	accessTTL time.Duration
	now       func() time.Time
}

func NewSigner(secret []byte, accessTTL time.Duration) *Signer {
	return &Signer{secret: secret, accessTTL: accessTTL, now: time.Now}
}

func (s *Signer) AccessTTL() time.Duration { return s.accessTTL }

func (s *Signer) SignAccess(userID int64) (string, time.Time, error) {
	now := s.now()
	expiresAt := now.Add(s.accessTTL)

	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, expiresAt, nil
}

// ParseAccess verifies a token and returns its user ID. Every failure mode
// collapses to ErrInvalidToken: the caller has no use for the distinction and
// the client must not learn why verification failed.
func (s *Signer) ParseAccess(token string) (int64, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		// Without this, a token re-signed with "alg": "none" would parse.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return s.now() }),
	)
	if err != nil {
		return 0, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, ErrInvalidToken
	}
	return userID, nil
}

// NewRefreshToken returns a token to hand to the client and the hash to store.
// The token is 256 bits of entropy, so a fast hash is enough: bcrypt exists to
// slow down guessing of low-entropy passwords, and would add ~100ms to every
// refresh for no benefit here.
func NewRefreshToken() (token string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashRefreshToken(token), nil
}

func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS — all password and token tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/auth
git commit -m "Add access token signing and refresh token generation"
```

---

### Task 4: Refresh token table and the Store contract

**Files:**
- Create: `internal/db/migrations/000003_refresh_tokens.up.sql`
- Create: `internal/db/migrations/000003_refresh_tokens.down.sql`
- Create: `internal/auth/store.go`

**Interfaces:**
- Consumes: `auth.ErrNotFound` (Task 2).
- Produces: `auth.User{ID int64; Email, PasswordHash string; CreatedAt time.Time}`, `auth.RefreshToken{UserID int64; TokenHash string; ExpiresAt time.Time; RevokedAt *time.Time}`, `auth.Store` interface, `auth.NormalizeEmail(string) string`.

- [ ] **Step 1: Write the migration**

Create `internal/db/migrations/000003_refresh_tokens.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT     NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
```

Create `internal/db/migrations/000003_refresh_tokens.down.sql`:

```sql
DROP INDEX IF EXISTS idx_refresh_tokens_user;

DROP TABLE IF EXISTS refresh_tokens;
```

- [ ] **Step 2: Write the failing test**

Create `internal/auth/store_test.go`:

```go
package auth

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a@b.com", "a@b.com"},
		{"A@B.com", "a@b.com"},
		{"  a@b.com  ", "a@b.com"},
		{"\tMixed@Case.COM\n", "mixed@case.com"},
	}
	for _, tt := range tests {
		if got := NormalizeEmail(tt.in); got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestNormalizeEmail -v`
Expected: FAIL — `undefined: NormalizeEmail`.

- [ ] **Step 4: Write the models and interface**

Create `internal/auth/store.go`:

```go
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestNormalizeEmail -v && go test ./internal/db/ -v`
Expected: PASS. The `internal/db` package has no tests yet, so expect `no test files` — the migration is exercised in Task 5.

- [ ] **Step 6: Commit**

```bash
git add internal/db/migrations internal/auth/store.go internal/auth/store_test.go
git commit -m "Add refresh_tokens migration and the auth Store contract"
```

---

### Task 5: SQLite Store implementation

**Files:**
- Create: `internal/store/sqlite.go`
- Test: `internal/store/sqlite_test.go`

**Interfaces:**
- Consumes: `auth.Store`, `auth.User`, `auth.RefreshToken`, `auth.ErrNotFound`, `auth.ErrEmailTaken` (Task 4), `db.Open` (existing, `internal/db/db.go:17`).
- Produces: `store.New(db *sql.DB) *SQLite`, satisfying `auth.Store`.

**Note on test databases:** these tests use a temp-file SQLite database, not `:memory:`. With `database/sql` connection pooling, each pooled connection to `:memory:` gets its own empty database, so migrations applied on one connection are invisible to the next. A temp file under `t.TempDir()` is equally isolated and is cleaned up automatically. This is a deliberate correction to the spec's wording.

- [ ] **Step 1: Write the failing test**

Create `internal/store/sqlite_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/sqlite.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS — 7 tests.

If `ExpiresAt` comparisons fail, confirm the DSN in `internal/db/db.go:18` still appends `?_foreign_keys=on`; the driver parses `DATETIME` columns into `time.Time` only for recognized column types.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "Add SQLite implementation of the auth store"
```

---

### Task 6: Service — register and login

**Files:**
- Create: `internal/auth/service.go`
- Test: `internal/auth/service_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–4. The tests also use `testSecret`, the package-level `[]byte` declared in `internal/auth/token_test.go` in Task 3 — same package, so do not redeclare it.
- Produces: `auth.NewService(store Store, signer *Signer, refreshTTL time.Duration) *Service`, `(*Service).Register(ctx, email, password string) (User, error)`, `(*Service).Login(ctx, email, password string) (TokenPair, error)`, `auth.TokenPair{AccessToken string; ExpiresIn int; RefreshToken string}`.

- [ ] **Step 1: Write the fake store and the failing test**

Create `internal/auth/service_test.go`:

```go
package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeStore is an in-memory Store. It exists so service tests exercise
// decision logic without SQLite; the SQL itself is covered in internal/store.
type fakeStore struct {
	mu      sync.Mutex
	users   map[string]User
	tokens  map[string]RefreshToken
	nextID  int64
	failNow error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:  make(map[string]User),
		tokens: make(map[string]RefreshToken),
		nextID: 1,
	}
}

func (f *fakeStore) CreateUser(_ context.Context, email, passwordHash string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNow != nil {
		return User{}, f.failNow
	}
	if _, exists := f.users[email]; exists {
		return User{}, ErrEmailTaken
	}
	u := User{ID: f.nextID, Email: email, PasswordHash: passwordHash, CreatedAt: time.Now()}
	f.nextID++
	f.users[email] = u
	return u, nil
}

func (f *fakeStore) UserByEmail(_ context.Context, email string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) CreateRefreshToken(_ context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[tokenHash] = RefreshToken{UserID: userID, TokenHash: tokenHash, ExpiresAt: expiresAt}
	return nil
}

func (f *fakeStore) RefreshTokenByHash(_ context.Context, tokenHash string) (RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.tokens[tokenHash]
	if !ok {
		return RefreshToken{}, ErrNotFound
	}
	return rt, nil
}

func (f *fakeStore) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.tokens[tokenHash]
	if !ok {
		return nil // idempotent, matching the Store contract
	}
	if rt.RevokedAt == nil {
		now := time.Now()
		rt.RevokedAt = &now
		f.tokens[tokenHash] = rt
	}
	return nil
}

func (f *fakeStore) RevokeAllForUser(_ context.Context, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for hash, rt := range f.tokens {
		if rt.UserID == userID && rt.RevokedAt == nil {
			rt.RevokedAt = &now
			f.tokens[hash] = rt
		}
	}
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	svc := NewService(store, NewSigner(testSecret, 15*time.Minute), 30*24*time.Hour)
	return svc, store
}

func TestRegisterThenLogin(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	user, err := svc.Register(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.PasswordHash == "password123" {
		t.Fatal("the password must be stored hashed")
	}

	pair, err := svc.Login(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("Login must return both tokens")
	}
	if pair.ExpiresIn != 900 {
		t.Fatalf("ExpiresIn = %d, want 900", pair.ExpiresIn)
	}
}

func TestRegisterNormalizesEmail(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "  A@B.com ", "password123"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := store.users["a@b.com"]; !ok {
		t.Fatalf("email was not normalized before storage: %v", store.users)
	}

	// The same account, typed differently, must log in.
	if _, err := svc.Login(ctx, "A@B.COM", "password123"); err != nil {
		t.Fatalf("login with a case variant: %v", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Register(ctx, "A@b.com", "password456")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Register(context.Background(), "a@b.com", "short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("err = %v, want ErrPasswordLength", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Login(ctx, "a@b.com", "wrongpassword")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

// An unknown email and a wrong password must be indistinguishable.
func TestLoginUnknownEmailSameError(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Login(context.Background(), "nobody@example.com", "password123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run 'Register|Login' -v`
Expected: FAIL — `undefined: NewService`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/service.go`:

```go
package auth

import (
	"context"
	"errors"
	"time"
)

// TokenPair is what a client receives from login and refresh. ExpiresIn is the
// access token's lifetime in seconds; the refresh token's expiry is not
// disclosed, because the client cannot act on it.
type TokenPair struct {
	AccessToken  string
	ExpiresIn    int
	RefreshToken string
}

type Service struct {
	store      Store
	signer     *Signer
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(store Store, signer *Signer, refreshTTL time.Duration) *Service {
	return &Service{store: store, signer: signer, refreshTTL: refreshTTL, now: time.Now}
}

func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.store.CreateUser(ctx, NormalizeEmail(email), hash)
}

func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := s.store.UserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		// Spend the same time a real comparison would, so response latency
		// does not reveal whether the account exists.
		VerifyDummyPassword()
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, err
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	return s.issuePair(ctx, user.ID)
}

func (s *Service) issuePair(ctx context.Context, userID int64) (TokenPair, error) {
	access, _, err := s.signer.SignAccess(userID)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.store.CreateRefreshToken(ctx, userID, hash, s.now().Add(s.refreshTTL)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		ExpiresIn:    int(s.signer.AccessTTL().Seconds()),
		RefreshToken: refresh,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS — all auth tests.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/service.go internal/auth/service_test.go
git commit -m "Add auth service registration and login"
```

---

### Task 7: Service — refresh rotation, reuse detection, logout

**Files:**
- Modify: `internal/auth/service.go`
- Modify: `internal/auth/service_test.go`

**Interfaces:**
- Consumes: Task 6's `Service`.
- Produces: `(*Service).Refresh(ctx, refreshToken string) (TokenPair, error)`, `(*Service).Logout(ctx, refreshToken string) error`.

- [ ] **Step 1: Write the failing test**

Append to `internal/auth/service_test.go`:

```go
func TestRefreshRotatesToken(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh must issue a new refresh token")
	}

	old, err := store.RefreshTokenByHash(ctx, HashRefreshToken(first.RefreshToken))
	if err != nil {
		t.Fatal(err)
	}
	if old.RevokedAt == nil {
		t.Fatal("the presented refresh token must be revoked after rotation")
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Refresh(context.Background(), "not-a-real-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestRefreshRejectsExpiredToken(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, NewSigner(testSecret, 15*time.Minute), 30*24*time.Hour)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	pair, err := svc.Login(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	// Move the service's clock past the refresh token's 30-day expiry.
	svc.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }

	if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

// Replaying an already-rotated token means it was stolen: every token for that
// user must be revoked, not just the replayed one.
func TestRefreshReuseRevokesFamily(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}

	// Replay the token that rotation already revoked.
	if _, err := svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}

	// The still-live token from the legitimate client must now be dead too.
	live, err := store.RefreshTokenByHash(ctx, HashRefreshToken(second.RefreshToken))
	if err != nil {
		t.Fatal(err)
	}
	if live.RevokedAt == nil {
		t.Fatal("reuse detection must revoke every token for the user")
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("the revoked family member must not refresh, got err = %v", err)
	}
}

func TestLogoutRevokesToken(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Register(ctx, "a@b.com", "password123"); err != nil {
		t.Fatal(err)
	}
	pair, err := svc.Login(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Logout(ctx, pair.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a logged-out token must not refresh, got err = %v", err)
	}
}

func TestLogoutUnknownTokenSucceeds(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.Logout(context.Background(), "not-a-real-token"); err != nil {
		t.Fatalf("logout must be idempotent, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run 'Refresh|Logout' -v`
Expected: FAIL — `svc.Refresh undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/auth/service.go`:

```go
// Refresh rotates a refresh token: the presented token is revoked and a fresh
// pair issued. Presenting an already-revoked token means it was replayed, so
// every token for that user is revoked and the client must log in again.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	hash := HashRefreshToken(refreshToken)

	stored, err := s.store.RefreshTokenByHash(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return TokenPair{}, ErrInvalidToken
	}
	if err != nil {
		return TokenPair{}, err
	}

	if stored.RevokedAt != nil {
		if err := s.store.RevokeAllForUser(ctx, stored.UserID); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, ErrInvalidToken
	}
	if !s.now().Before(stored.ExpiresAt) {
		return TokenPair{}, ErrInvalidToken
	}
	if err := s.store.RevokeRefreshToken(ctx, hash); err != nil {
		return TokenPair{}, err
	}
	return s.issuePair(ctx, stored.UserID)
}

// Logout revokes one refresh token, leaving other devices signed in. It
// succeeds whether or not the token exists: a client that has lost track of
// its session should still be able to log out cleanly.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.store.RevokeRefreshToken(ctx, HashRefreshToken(refreshToken))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS — all auth tests including the six new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/service.go internal/auth/service_test.go
git commit -m "Add refresh rotation with reuse detection and logout"
```

---

### Task 8: HTTP handlers and server wiring

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/auth_handlers.go`
- Create: `internal/server/helpers_test.go`
- Test: `internal/server/auth_test.go`

**Interfaces:**
- Consumes: `auth.Service`, `auth.Signer`, `store.New`, `config.Config`.
- Produces: `server.New(db *sql.DB, cfg config.Config) *Server`, `(*Server).Handler() http.Handler`, `(*Server).Run(addr string) error`, and the four `/auth/*` endpoints.

**Note:** `server.New` changes signature (it currently takes only `*sql.DB` at `internal/server/server.go:15`). `cmd/server/main.go` is updated in Task 9.

- [ ] **Step 1: Write the failing test**

Create `internal/server/auth_test.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type tokenPairResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func doJSON(t *testing.T, srv *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func registerAndLogin(t *testing.T, srv *Server) tokenPairResponse {
	t.Helper()
	creds := map[string]string{"email": "a@b.com", "password": "password123"}

	if rec := doJSON(t, srv, http.MethodPost, "/auth/register", creds); rec.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, http.MethodPost, "/auth/login", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d, body %s", rec.Code, rec.Body)
	}
	var pair tokenPairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	return pair
}

func TestRegisterCreatesUser(t *testing.T) {
	srv := newTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/auth/register",
		map[string]string{"email": "a@b.com", "password": "password123"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	var got struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID == 0 || got.Email != "a@b.com" {
		t.Fatalf("unexpected body: %+v", got)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("password")) {
		t.Fatal("the response must not echo the password")
	}
}

func TestRegisterDuplicateReturns409(t *testing.T) {
	srv := newTestServer(t)
	creds := map[string]string{"email": "a@b.com", "password": "password123"}

	doJSON(t, srv, http.MethodPost, "/auth/register", creds)
	rec := doJSON(t, srv, http.MethodPost, "/auth/register", creds)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body)
	}
}

func TestRegisterValidation(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing email", map[string]string{"password": "password123"}},
		{"invalid email", map[string]string{"email": "not-an-email", "password": "password123"}},
		{"short password", map[string]string{"email": "a@b.com", "password": "short"}},
		{"missing password", map[string]string{"email": "a@b.com"}},
		{"password over 72 bytes", map[string]string{"email": "a@b.com", "password": strings.Repeat("a", 73)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(t, srv, http.MethodPost, "/auth/register", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestLoginWrongPasswordReturns401(t *testing.T) {
	srv := newTestServer(t)
	registerAndLogin(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/auth/login",
		map[string]string{"email": "a@b.com", "password": "wrongpassword"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginUnknownEmailReturnsSameError(t *testing.T) {
	srv := newTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/auth/login",
		map[string]string{"email": "nobody@example.com", "password": "password123"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "invalid email or password" {
		t.Fatalf("error = %q; it must not reveal that the account is unknown", got.Error)
	}
}

func TestRefreshReturnsNewPair(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": pair.RefreshToken})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var next tokenPairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh must rotate the refresh token")
	}
	if next.TokenType != "Bearer" || next.ExpiresIn != 900 {
		t.Fatalf("unexpected pair: %+v", next)
	}
}

func TestRefreshWithGarbageReturns401(t *testing.T) {
	srv := newTestServer(t)

	rec := doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": "not-a-real-token"})

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogoutIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	for i := range 2 {
		rec := doJSON(t, srv, http.MethodPost, "/auth/logout",
			map[string]string{"refresh_token": pair.RefreshToken})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("call %d: status = %d, want 204", i+1, rec.Code)
		}
	}

	rec := doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": pair.RefreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a logged-out token refreshed: status = %d, want 401", rec.Code)
	}
}

func TestMalformedJSONReturns400(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 2: Write the test server helper**

Create `internal/server/helpers_test.go`. It must be a `_test.go` file: a regular
build file importing `testing` would compile test scaffolding into the server binary
and register testing's flags at init.

```go
package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/db"
)

// newTestServer returns a server backed by a throwaway SQLite database with
// migrations applied. Milestones 4-7 reuse this helper.
//
// It uses a temp file rather than ":memory:" because each pooled connection to
// an in-memory SQLite database gets its own empty schema, so migrations run on
// one connection are invisible to the next.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return New(database, config.Config{
		JWTSecret:  []byte("0123456789abcdef0123456789abcdef"),
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 30 * 24 * time.Hour,
	})
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -v`
Expected: FAIL — `too many arguments in call to New`.

- [ ] **Step 4: Rewrite the server and add handlers**

Replace `internal/server/server.go` entirely:

```go
package server

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/store"
)

type Server struct {
	db      *sql.DB
	router  *gin.Engine
	auth    *auth.Service
	signer  *auth.Signer
}

func New(db *sql.DB, cfg config.Config) *Server {
	signer := auth.NewSigner(cfg.JWTSecret, cfg.AccessTTL)
	s := &Server{
		db:     db,
		router: gin.Default(),
		auth:   auth.NewService(store.New(db), signer, cfg.RefreshTTL),
		signer: signer,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authGroup := s.router.Group("/auth")
	authGroup.POST("/register", s.handleRegister)
	authGroup.POST("/login", s.handleLogin)
	authGroup.POST("/refresh", s.handleRefresh)
	authGroup.POST("/logout", s.handleLogout)
}

// Handler exposes the router for tests and for embedding behind another mux.
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Run(addr string) error { return s.router.Run(addr) }
```

Create `internal/server/auth_handlers.go`:

```go
package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
)

type credentialsRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// respondError writes the one error shape the API uses.
func respondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

// respondServiceError maps service errors to status codes. Anything
// unrecognized is a 500 with the detail logged, never returned.
func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		respondError(c, http.StatusConflict, "email already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		respondError(c, http.StatusUnauthorized, "invalid email or password")
	case errors.Is(err, auth.ErrInvalidToken):
		respondError(c, http.StatusUnauthorized, "invalid or expired token")
	case errors.Is(err, auth.ErrPasswordLength):
		respondError(c, http.StatusBadRequest, "password must be between 8 and 72 bytes")
	default:
		log.Printf("auth: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func writeTokenPair(c *gin.Context, pair auth.TokenPair) {
	c.JSON(http.StatusOK, gin.H{
		"access_token":  pair.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    pair.ExpiresIn,
		"refresh_token": pair.RefreshToken,
	})
}

func (s *Server) handleRegister(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.auth.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": user.ID, "email": user.Email})
}

func (s *Server) handleLogin(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := s.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	writeTokenPair(c, pair)
}

func (s *Server) handleRefresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := s.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	writeTokenPair(c, pair)
}

func (s *Server) handleLogout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.auth.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
```

Note: a login with a wrong-length password is rejected by binding as 400 before it reaches the service. That is why `TestLoginWrongPasswordReturns401` uses `"wrongpassword"` — 13 valid bytes — rather than a short string.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS — 9 tests.

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "Add auth HTTP endpoints and server wiring"
```

---

### Task 9: RequireAuth middleware, protected route pattern, and main wiring

**Files:**
- Create: `internal/server/middleware.go`
- Modify: `internal/server/server.go` (add the protected group and a probe route)
- Modify: `cmd/server/main.go`
- Test: `internal/server/middleware_test.go`

**Interfaces:**
- Consumes: `auth.Signer`, `Server.signer` (Task 8).
- Produces: `server.RequireAuth(signer *auth.Signer) gin.HandlerFunc`, `server.userID(c *gin.Context) int64`. Milestones 4–7 hang their routes off the `authed` group and read the caller with `userID(c)`.

- [ ] **Step 1: Write the failing test**

Create `internal/server/middleware_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/auth"
)

// get issues a GET with an optional Authorization header.
func get(t *testing.T, srv *Server, path, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestProtectedRouteAcceptsValidToken(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/me", "Bearer "+pair.AccessToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.UserID == 0 {
		t.Fatal("the middleware must expose the caller's user ID to handlers")
	}
}

func TestProtectedRouteRejectsBadHeaders(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	tests := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"no Bearer prefix", pair.AccessToken},
		{"wrong scheme", "Basic " + pair.AccessToken},
		{"empty bearer", "Bearer "},
		{"garbage token", "Bearer not-a-jwt"},
		{"lowercase scheme", "bearer " + pair.AccessToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := get(t, srv, "/me", tt.header); rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestProtectedRouteRejectsTokenFromAnotherSecret(t *testing.T) {
	srv := newTestServer(t)
	other := auth.NewSigner([]byte("fedcba9876543210fedcba9876543210"), time.Minute)
	token, _, err := other.SignAccess(1)
	if err != nil {
		t.Fatal(err)
	}

	if rec := get(t, srv, "/me", "Bearer "+token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run Protected -v`
Expected: FAIL — 404 on `/me`, since neither the middleware nor the route exists.

- [ ] **Step 3: Write the middleware**

Create `internal/server/middleware.go`:

```go
package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
)

// userIDKey is the gin context key holding the authenticated caller's ID.
const userIDKey = "userID"

// RequireAuth verifies the bearer access token and stores the caller's ID in
// the request context. Verification is signature-only: access tokens are
// stateless, so this never touches the database.
func RequireAuth(signer *auth.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			respondError(c, http.StatusUnauthorized, "missing or malformed authorization header")
			return
		}
		id, err := signer.ParseAccess(token)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		c.Set(userIDKey, id)
		c.Next()
	}
}

// userID returns the authenticated caller's ID. It is only valid inside a
// handler behind RequireAuth, which is the single place the value is set —
// handlers use this instead of repeating an unchecked type assertion.
func userID(c *gin.Context) int64 {
	id, _ := c.MustGet(userIDKey).(int64)
	return id
}
```

- [ ] **Step 4: Add the protected group**

In `internal/server/server.go`, replace the body of `routes()` with:

```go
func (s *Server) routes() {
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authGroup := s.router.Group("/auth")
	authGroup.POST("/register", s.handleRegister)
	authGroup.POST("/login", s.handleLogin)
	authGroup.POST("/refresh", s.handleRefresh)
	authGroup.POST("/logout", s.handleLogout)

	// Milestones 4-7 hang their routes off this group.
	authed := s.router.Group("/", RequireAuth(s.signer))
	authed.GET("/me", s.handleMe)
}
```

Append to `internal/server/auth_handlers.go`:

```go
// handleMe returns the caller's identity. It is the first consumer of the
// protected-route pattern and doubles as a client-side token check.
func (s *Server) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user_id": userID(c)})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS — all handler and middleware tests.

- [ ] **Step 6: Wire main.go to the new config**

Replace `cmd/server/main.go` entirely:

```go
package main

import (
	"log"

	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/db"
	"github.com/kate/book-tracking/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	database, err := db.Open(cfg.DSN)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.New(database, cfg)
	log.Printf("listening on %s", cfg.Addr)
	if err := srv.Run(cfg.Addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 7: Verify the whole build and suite**

Run:
```bash
go build ./... && go vet ./... && go test ./...
```
Expected: all packages PASS, no vet findings.

Then confirm the fail-fast key by hand:
```bash
env -u JWT_SECRET go run ./cmd/server
```
Expected: exits immediately with `configuration error: JWT_SECRET must be set and at least 32 bytes; generate one with: openssl rand -base64 32`.

- [ ] **Step 8: Commit**

```bash
git add internal/server cmd/server
git commit -m "Add RequireAuth middleware and protected route pattern"
```

---

### Task 10: Update the milestone spec

**Files:**
- Modify: `specs/backend/implementation.md`

- [ ] **Step 1: Record what was built**

In `specs/backend/implementation.md`, replace the Milestone 2 section with:

```markdown
## Milestone 2 — Authentication

- `POST /auth/register` — email + password, bcrypt (cost 12), 409 on duplicate
- `POST /auth/login` — returns a 15-minute access JWT and a 30-day refresh token
- `POST /auth/refresh` — rotates the refresh token; replaying a revoked token
  revokes every token for that user
- `POST /auth/logout` — revokes one refresh token; idempotent
- `GET /me` — returns the caller's ID; first consumer of the protected pattern
- `RequireAuth` middleware applied via the `authed` route group, with `userID(c)`
  for handlers. Milestones 4–7 hang their routes off this group.
- `JWT_SECRET` is required at startup, minimum 32 bytes, no default
- Design: `docs/superpowers/specs/2026-08-02-auth-design.md`
```

- [ ] **Step 2: Commit**

```bash
git add specs/backend/implementation.md
git commit -m "Record Milestone 2 endpoints in the backend spec"
```

---

## Verification Checklist

After Task 10, all of the following must hold:

- [ ] `go build ./... && go vet ./... && go test ./...` is clean
- [ ] `env -u JWT_SECRET go run ./cmd/server` refuses to start
- [ ] Registering the same email twice returns 409
- [ ] Login with an unknown email and login with a wrong password return byte-identical bodies
- [ ] A refresh token works exactly once; replaying it kills the user's other sessions
- [ ] `GET /me` returns 401 without a token and the caller's ID with one
- [ ] Migration 000003 rolls back cleanly (`refresh_tokens` and its index are dropped)
