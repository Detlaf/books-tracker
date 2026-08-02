package auth

import (
	"context"
	"errors"
	"time"
)

// TokenPair is what a client receives from login and refresh. ExpiresIn is the
// access token's lifetime in seconds; the refresh token's expiry is not
// disclosed, because the client cannot act on it.
type TokenPair struct {
	AccessToken  string
	ExpiresIn    int
	RefreshToken string
}

type Service struct {
	store      Store
	signer     *Signer
	refreshTTL time.Duration
	now        func() time.Time
}

func NewService(store Store, signer *Signer, refreshTTL time.Duration) *Service {
	return &Service{store: store, signer: signer, refreshTTL: refreshTTL, now: time.Now}
}

func (s *Service) Register(ctx context.Context, email, password string) (User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	return s.store.CreateUser(ctx, NormalizeEmail(email), hash)
}

func (s *Service) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := s.store.UserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, ErrNotFound) {
		// Spend the same time a real comparison would, so response latency
		// does not reveal whether the account exists.
		VerifyDummyPassword()
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, err
	}
	if err := VerifyPassword(user.PasswordHash, password); err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	return s.issuePair(ctx, user.ID)
}

func (s *Service) issuePair(ctx context.Context, userID int64) (TokenPair, error) {
	access, _, err := s.signer.SignAccess(userID)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.store.CreateRefreshToken(ctx, userID, hash, s.now().Add(s.refreshTTL)); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		ExpiresIn:    int(s.signer.AccessTTL().Seconds()),
		RefreshToken: refresh,
	}, nil
}
