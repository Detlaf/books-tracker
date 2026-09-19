package collections

import (
	"context"
	"strings"
)

// Service trims and validates the one thing this package is not pure CRUD
// about — the name — and delegates everything else to the store.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, userID int64, name string) (Collection, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Collection{}, ErrEmptyName
	}
	return s.store.Create(ctx, userID, trimmed)
}

// List normalizes a nil result to an empty slice so callers never have to
// distinguish "no collections" from "store returned nil".
func (s *Service) List(ctx context.Context, userID int64) ([]Collection, error) {
	found, err := s.store.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return []Collection{}, nil
	}
	return found, nil
}

func (s *Service) Rename(ctx context.Context, userID, id int64, name string) (Collection, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Collection{}, ErrEmptyName
	}
	return s.store.Rename(ctx, userID, id, trimmed)
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	return s.store.Delete(ctx, userID, id)
}

func (s *Service) AddBook(ctx context.Context, userID, id, bookID int64) (Collection, error) {
	return s.store.AddBook(ctx, userID, id, bookID)
}

func (s *Service) RemoveBook(ctx context.Context, userID, id, bookID int64) error {
	return s.store.RemoveBook(ctx, userID, id, bookID)
}
