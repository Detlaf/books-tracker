package stats

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

const (
	// DefaultTopAuthorsLimit is top-authors' limit when the caller sends
	// none.
	DefaultTopAuthorsLimit = 5
	// MaxTopAuthorsLimit caps limit so an unbounded request cannot return
	// someone's entire author list in one response.
	MaxTopAuthorsLimit = 50
)

var yearPattern = regexp.MustCompile(`^\d{4}$`)

// Service has no state-machine or CRUD logic — every method is a thin
// validate-then-delegate, unlike library.Service or collections.Service.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return NewServiceWithClock(store, func() time.Time { return time.Now().UTC() })
}

// NewServiceWithClock injects the clock so summary's year math and streak's
// "last completed month" are testable without sleeping past a rollover.
func NewServiceWithClock(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now}
}

// validateScope reports whether scope is "all" or a four-digit year.
func validateScope(scope string) error {
	if scope == "all" || yearPattern.MatchString(scope) {
		return nil
	}
	return ErrInvalidScope
}

// clampLimit defaults a non-positive limit and caps an oversized one.
func clampLimit(limit int) int {
	if limit < 1 {
		return DefaultTopAuthorsLimit
	}
	if limit > MaxTopAuthorsLimit {
		return MaxTopAuthorsLimit
	}
	return limit
}

func (s *Service) Summary(ctx context.Context, userID int64) (Summary, error) {
	return s.store.Summary(ctx, userID, s.now())
}

// ByYear normalizes a nil result to an empty slice so callers never have to
// distinguish "no dated reads" from "store returned nil".
func (s *Service) ByYear(ctx context.Context, userID int64) ([]YearCount, int, error) {
	years, undated, err := s.store.ByYear(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	if years == nil {
		years = []YearCount{}
	}
	return years, undated, nil
}

// ByMonth resolves an empty year to the server's current UTC year and
// returns the resolved year alongside the months, so the handler can echo
// it back without recomputing "now" itself.
func (s *Service) ByMonth(ctx context.Context, userID int64, year string) (string, []MonthCount, error) {
	if year == "" {
		year = fmt.Sprintf("%04d", s.now().Year())
	} else if !yearPattern.MatchString(year) {
		return "", nil, ErrInvalidYear
	}

	months, err := s.store.ByMonth(ctx, userID, year)
	if err != nil {
		return "", nil, err
	}
	return year, months, nil
}

// ByLanguage resolves an empty scope to "all".
func (s *Service) ByLanguage(ctx context.Context, userID int64, scope string) ([]LanguageCount, error) {
	if scope == "" {
		scope = "all"
	} else if err := validateScope(scope); err != nil {
		return nil, err
	}

	languages, err := s.store.ByLanguage(ctx, userID, scope)
	if err != nil {
		return nil, err
	}
	if languages == nil {
		languages = []LanguageCount{}
	}
	return languages, nil
}

// TopAuthors resolves an empty scope to "all" and clamps limit.
func (s *Service) TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]AuthorCount, error) {
	if scope == "" {
		scope = "all"
	} else if err := validateScope(scope); err != nil {
		return nil, err
	}

	authors, err := s.store.TopAuthors(ctx, userID, scope, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	if authors == nil {
		authors = []AuthorCount{}
	}
	return authors, nil
}

func (s *Service) Streak(ctx context.Context, userID int64) (int, error) {
	return s.store.Streak(ctx, userID, s.now())
}
