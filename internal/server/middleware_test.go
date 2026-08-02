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
