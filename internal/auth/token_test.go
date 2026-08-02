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
