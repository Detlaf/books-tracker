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
		// jwt/v5 only validates exp when it is present, so a correctly signed
		// token minted without one would never expire.
		jwt.WithExpirationRequired(),
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
