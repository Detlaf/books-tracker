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
