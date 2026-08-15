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
	return doJSONWithHeader(t, srv, method, path, body, "")
}

// doJSONWithHeader is doJSON with an Authorization header, which every route
// behind RequireAuth needs.
func doJSONWithHeader(t *testing.T, srv *Server, method, path string, body any, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func registerAndLogin(t *testing.T, srv *Server) tokenPairResponse {
	t.Helper()
	return registerAndLoginAs(t, srv, "a@b.com")
}

// registerAndLoginAs registers and logs in one account. Cross-user tests need
// a second one, and the email is the only thing that differs.
func registerAndLoginAs(t *testing.T, srv *Server, email string) tokenPairResponse {
	t.Helper()
	creds := map[string]string{"email": email, "password": "password123"}

	if rec := doJSON(t, srv, http.MethodPost, "/auth/register", creds); rec.Code != http.StatusCreated {
		t.Fatalf("register %s: status %d, body %s", email, rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, http.MethodPost, "/auth/login", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status %d, body %s", email, rec.Code, rec.Body)
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

// The branch's headline security property, driven through the real stack:
// once a refresh token has been rotated it is spent, and presenting it again
// is refused rather than served.
func TestRefreshRejectsPreRotationToken(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": pair.RefreshToken})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var rotated tokenPairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}

	rec = doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": pair.RefreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the pre-rotation token refreshed: status = %d, want 401; body %s", rec.Code, rec.Body)
	}

	// Replaying a rotated token is theft, so the token it was rotated into
	// must be dead too.
	rec = doJSON(t, srv, http.MethodPost, "/auth/refresh",
		map[string]string{"refresh_token": rotated.RefreshToken})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse detection did not revoke the family: status = %d, want 401", rec.Code)
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
