package books

import (
	"context"
	"strings"
)

// Repo persists books and returns them with local IDs. *store.SQLite
// implements it; the interface keeps this package free of database/sql.
type Repo interface {
	UpsertBooks(ctx context.Context, in []Book) ([]Book, error)
}

// Service fetches from a Provider and persists what it finds, so every result
// leaves this package carrying the local ID that the library, rating, and
// collection endpoints address books by.
type Service struct {
	provider Provider
	repo     Repo
}

func NewService(provider Provider, repo Repo) *Service {
	return &Service{provider: provider, repo: repo}
}

// Search validates before it spends an upstream call. An unmatched search is a
// normal outcome and returns an empty slice rather than an error.
func (s *Service) Search(ctx context.Context, q string, page, limit int) ([]Book, error) {
	if strings.TrimSpace(q) == "" {
		return nil, ErrBlankQuery
	}

	found, err := s.provider.Search(ctx, q, page, limit)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return []Book{}, nil
	}
	return s.repo.UpsertBooks(ctx, found)
}

// ByISBN normalizes and checksum-validates locally, so a typo never reaches the
// provider.
func (s *Service) ByISBN(ctx context.Context, rawISBN string) (Book, error) {
	isbn, err := NormalizeISBN(rawISBN)
	if err != nil {
		return Book{}, err
	}

	found, err := s.provider.ByISBN(ctx, isbn)
	if err != nil {
		return Book{}, err
	}

	stored, err := s.repo.UpsertBooks(ctx, []Book{found})
	if err != nil {
		return Book{}, err
	}
	return stored[0], nil
}
