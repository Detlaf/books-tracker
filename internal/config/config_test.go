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
