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
