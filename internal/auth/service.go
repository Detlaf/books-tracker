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

// Refresh rotates a refresh token: the presented token is revoked and a fresh
// pair issued.
//
// The revoke is the authoritative step, not the read. Two requests presenting
// the same live token both read RevokedAt == nil, so deciding on the read would
// hand both of them a valid pair — exactly the thief-races-the-victim case
// rotation exists to catch. Only one caller can win the atomic revoke; whoever
// loses it presented a token that was already spent, and is handled as a replay.
//
// What a replay costs depends on why the token was revoked. A rotated token
// replayed is evidence of theft, so the whole family goes. A logged-out token
// replayed is refused on its own: cascading there would let one stale token,
// replayed in a loop, sign the user out everywhere forever.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	hash := HashRefreshToken(refreshToken)

	stored, err := s.store.RefreshTokenByHash(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return TokenPair{}, ErrInvalidToken
	}
	if err != nil {
		return TokenPair{}, err
	}

	// Fast path: obviously spent, no need to attempt the write.
	if stored.RevokedAt != nil {
		return s.rejectReplay(ctx, stored.UserID, stored.RevokedReason)
	}
	if !s.now().Before(stored.ExpiresAt) {
		return TokenPair{}, ErrInvalidToken
	}

	revoked, err := s.store.RevokeRefreshToken(ctx, hash, RevokeReasonRotated)
	if err != nil {
		return TokenPair{}, err
	}
	if !revoked {
		// Someone revoked it between our read and our write. Re-read to learn
		// who: the reason was not there when we first looked.
		current, err := s.store.RefreshTokenByHash(ctx, hash)
		if errors.Is(err, ErrNotFound) {
			return TokenPair{}, ErrInvalidToken
		}
		if err != nil {
			return TokenPair{}, err
		}
		return s.rejectReplay(ctx, current.UserID, current.RevokedReason)
	}
	return s.issuePair(ctx, stored.UserID)
}

// rejectReplay refuses a token that was already revoked, cascading to the whole
// family only when the token had been rotated. Any other reason — logout, or
// the empty reason on rows written before it was recorded — is refused alone.
func (s *Service) rejectReplay(ctx context.Context, userID int64, reason string) (TokenPair, error) {
	if reason == RevokeReasonRotated {
		if err := s.store.RevokeAllForUser(ctx, userID); err != nil {
			return TokenPair{}, err
		}
	}
	return TokenPair{}, ErrInvalidToken
}

// Logout revokes one refresh token, leaving other devices signed in. It
// succeeds whether or not the token exists, and whether or not this call is the
// one that revoked it: a client that has lost track of its session should still
// be able to log out cleanly.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	_, err := s.store.RevokeRefreshToken(ctx, HashRefreshToken(refreshToken), RevokeReasonLogout)
	return err
}
