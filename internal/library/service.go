package library

import (
	"context"
	"time"
)

// Service holds the only logic in this package that is not CRUD: the rules
// deciding what finished_at becomes on each transition.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return NewServiceWithClock(store, func() time.Time { return time.Now().UTC() })
}

// NewServiceWithClock injects the clock so the boundary cases — a date exactly
// equal to now, a date one second ahead — are testable without sleeping.
func NewServiceWithClock(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now}
}

// Add puts a book in the caller's library. status is required: defaulting it
// would make the most common mistake, omitting it, invisible.
func (s *Service) Add(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error) {
	resolved, err := resolveFinishedAt(nil, status, finishedAt, s.now())
	if err != nil {
		return Entry{}, err
	}
	return s.store.AddEntry(ctx, userID, bookID, status, resolved)
}

// Update changes status, finished_at, or both. A nil for either means the
// client did not send the field, not that it should be cleared — clearing
// finished_at happens by moving off read.
func (s *Service) Update(ctx context.Context, userID, bookID int64, status *Status, finishedAt *time.Time) (Entry, error) {
	if status == nil && finishedAt == nil {
		return Entry{}, ErrEmptyUpdate
	}

	current, err := s.store.Entry(ctx, userID, bookID)
	if err != nil {
		return Entry{}, err
	}

	next := current.Status
	if status != nil {
		next = *status
	}

	resolved, err := resolveFinishedAt(&current, next, finishedAt, s.now())
	if err != nil {
		return Entry{}, err
	}
	return s.store.UpdateEntry(ctx, userID, bookID, next, resolved)
}

func (s *Service) Remove(ctx context.Context, userID, bookID int64) error {
	return s.store.DeleteEntry(ctx, userID, bookID)
}

// List normalizes paging and sort before handing them to the store, so the
// store never has to decide what a zero page means.
func (s *Service) List(ctx context.Context, p ListParams) ([]Entry, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.Limit < 1 {
		p.Limit = DefaultListLimit
	}
	if p.Limit > MaxListLimit {
		p.Limit = MaxListLimit
	}
	if p.Sort == "" {
		p.Sort = DefaultSort
	}

	found, err := s.store.ListEntries(ctx, p)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return []Entry{}, nil
	}
	return found, nil
}

// resolveFinishedAt decides what finished_at becomes. current is nil on an
// add. It is a package-level function over its inputs rather than a method so
// the whole transition table can be exercised in one place.
//
//	→ read, no date, none present     → now
//	→ read, no date, one present      → left alone
//	→ read, explicit date             → the supplied date
//	read → backlog/reading            → cleared
//	unchanged status, explicit date   → the supplied date, read only
func resolveFinishedAt(current *Entry, status Status, requested *time.Time, now time.Time) (*time.Time, error) {
	if requested != nil {
		if status != StatusRead {
			return nil, ErrFinishedAtNotRead
		}
		if requested.After(now) {
			return nil, ErrFutureFinishedAt
		}
		utc := requested.UTC()
		return &utc, nil
	}

	if status != StatusRead {
		return nil, nil
	}
	if current != nil && current.FinishedAt != nil {
		// Re-marking a book read must not rewrite a date the user set.
		// Copy rather than alias: the caller owns the Entry we were handed,
		// and every other return path here is already UTC-normalized.
		t := current.FinishedAt.UTC()
		return &t, nil
	}
	utc := now.UTC()
	return &utc, nil
}
