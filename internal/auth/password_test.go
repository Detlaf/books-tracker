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
