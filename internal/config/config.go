// Package config loads process configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// MinSecretLen is the shortest JWT_SECRET accepted. Shorter keys weaken HS256
// enough to be worth refusing outright.
const MinSecretLen = 32

type Config struct {
	DSN               string
	Addr              string
	JWTSecret         []byte
	AccessTTL         time.Duration
	RefreshTTL        time.Duration
	GoogleBooksAPIKey string
}

// Load reads configuration from the environment. It returns an error rather
// than falling back to a generated key when JWT_SECRET is missing: a default
// signing key is how a test secret reaches production.
func Load() (Config, error) {
	// Trimmed before the length check: `docker secret` and
	// `kubectl create secret --from-file` both append a newline, and signing
	// with it would give each environment a silently different key.
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
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
		// Optional: absent means the client calls Google keyless, which is
		// rate-limited per IP but works.
		GoogleBooksAPIKey: strings.TrimSpace(os.Getenv("GOOGLE_BOOKS_API_KEY")),
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
