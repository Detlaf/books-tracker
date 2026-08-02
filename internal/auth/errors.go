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
