package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/books"
)

// fixedNow is the clock every test in this file runs against, so "future"
// and "past" are exact rather than racing real time.
var fixedNow = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixedNow }

func ptrTime(t time.Time) *time.Time { return &t }

func ptrStatus(s Status) *Status { return &s }

// fakeStore holds one user's entries in a map keyed by book ID.
type fakeStore struct {
	entries map[int64]Entry
	err     error

	gotStatus     Status
	gotFinishedAt *time.Time
	listParams    ListParams
}

func newFakeStore() *fakeStore { return &fakeStore{entries: map[int64]Entry{}} }

func (f *fakeStore) AddEntry(_ context.Context, _, bookID int64, status Status, finishedAt *time.Time) (Entry, error) {
	if f.err != nil {
		return Entry{}, f.err
	}
	if _, ok := f.entries[bookID]; ok {
		return Entry{}, ErrAlreadyInLibrary
	}
	f.gotStatus, f.gotFinishedAt = status, finishedAt
	e := Entry{
		Book:       books.Book{ID: bookID, Title: "Dune"},
		Status:     status,
		FinishedAt: finishedAt,
		AddedAt:    fixedNow,
	}
	f.entries[bookID] = e
	return e, nil
}

func (f *fakeStore) Entry(_ context.Context, _, bookID int64) (Entry, error) {
	if f.err != nil {
		return Entry{}, f.err
	}
	e, ok := f.entries[bookID]
	if !ok {
		return Entry{}, ErrNotInLibrary
	}
	return e, nil
}

func (f *fakeStore) UpdateEntry(_ context.Context, _, bookID int64, status Status, finishedAt *time.Time) (Entry, error) {
	if f.err != nil {
		return Entry{}, f.err
	}
	e, ok := f.entries[bookID]
	if !ok {
		return Entry{}, ErrNotInLibrary
	}
	f.gotStatus, f.gotFinishedAt = status, finishedAt
	e.Status, e.FinishedAt = status, finishedAt
	f.entries[bookID] = e
	return e, nil
}

func (f *fakeStore) DeleteEntry(_ context.Context, _, bookID int64) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.entries[bookID]; !ok {
		return ErrNotInLibrary
	}
	delete(f.entries, bookID)
	return nil
}

func (f *fakeStore) ListEntries(_ context.Context, p ListParams) ([]Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.listParams = p
	out := make([]Entry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func newTestService(store Store) *Service {
	return NewServiceWithClock(store, clock)
}

// seed puts an entry in the store without going through the service, so a
// test can start from a state the transition rules would not produce.
func seed(f *fakeStore, bookID int64, status Status, finishedAt *time.Time) {
	f.entries[bookID] = Entry{
		Book:       books.Book{ID: bookID, Title: "Dune"},
		Status:     status,
		FinishedAt: finishedAt,
		AddedAt:    fixedNow.Add(-24 * time.Hour),
	}
}

func TestAddReadWithoutDateStampsNow(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.Add(context.Background(), 1, 42, StatusRead, nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(fixedNow) {
		t.Fatalf("FinishedAt = %v, want %v", got.FinishedAt, fixedNow)
	}
}

func TestAddNonReadLeavesFinishedAtNil(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.Add(context.Background(), 1, 42, StatusBacklog, nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.FinishedAt != nil {
		t.Fatalf("FinishedAt = %v, want nil", got.FinishedAt)
	}
}

// Backdating is how a user records a book finished before they started using
// the app.
func TestAddReadWithExplicitDateUsesIt(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	backdated := fixedNow.Add(-365 * 24 * time.Hour)

	got, err := svc.Add(context.Background(), 1, 42, StatusRead, ptrTime(backdated))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(backdated) {
		t.Fatalf("FinishedAt = %v, want %v", got.FinishedAt, backdated)
	}
}

func TestAddRejectsFutureFinishedAt(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Add(context.Background(), 1, 42, StatusRead, ptrTime(fixedNow.Add(time.Second)))
	if !errors.Is(err, ErrFutureFinishedAt) {
		t.Fatalf("err = %v, want ErrFutureFinishedAt", err)
	}
}

// Exactly now is not the future.
func TestAddAcceptsFinishedAtEqualToNow(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if _, err := svc.Add(context.Background(), 1, 42, StatusRead, ptrTime(fixedNow)); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestAddRejectsFinishedAtWithNonReadStatus(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Add(context.Background(), 1, 42, StatusReading, ptrTime(fixedNow))
	if !errors.Is(err, ErrFinishedAtNotRead) {
		t.Fatalf("err = %v, want ErrFinishedAtNotRead", err)
	}
}

func TestAddDuplicateIsAlreadyInLibrary(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusBacklog, nil)
	svc := newTestService(f)

	_, err := svc.Add(context.Background(), 1, 42, StatusReading, nil)
	if !errors.Is(err, ErrAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrAlreadyInLibrary", err)
	}
}

func TestUpdateToReadStampsNow(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusReading, nil)
	svc := newTestService(f)

	got, err := svc.Update(context.Background(), 1, 42, ptrStatus(StatusRead), nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(fixedNow) {
		t.Fatalf("FinishedAt = %v, want %v", got.FinishedAt, fixedNow)
	}
}

// Re-marking a book read must not silently rewrite the date a user set.
func TestUpdateToReadKeepsAnExistingDate(t *testing.T) {
	f := newFakeStore()
	original := fixedNow.Add(-48 * time.Hour)
	seed(f, 42, StatusRead, ptrTime(original))
	svc := newTestService(f)

	got, err := svc.Update(context.Background(), 1, 42, ptrStatus(StatusRead), nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(original) {
		t.Fatalf("FinishedAt = %v, want the original %v", got.FinishedAt, original)
	}
}

func TestUpdateToReadWithExplicitDateOverwrites(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow.Add(-48*time.Hour)))
	svc := newTestService(f)
	corrected := fixedNow.Add(-72 * time.Hour)

	got, err := svc.Update(context.Background(), 1, 42, ptrStatus(StatusRead), ptrTime(corrected))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(corrected) {
		t.Fatalf("FinishedAt = %v, want %v", got.FinishedAt, corrected)
	}
}

func TestUpdateAwayFromReadClearsTheDate(t *testing.T) {
	for _, to := range []Status{StatusBacklog, StatusReading} {
		f := newFakeStore()
		seed(f, 42, StatusRead, ptrTime(fixedNow.Add(-48*time.Hour)))
		svc := newTestService(f)

		got, err := svc.Update(context.Background(), 1, 42, ptrStatus(to), nil)
		if err != nil {
			t.Fatalf("Update to %s: %v", to, err)
		}
		if got.FinishedAt != nil {
			t.Fatalf("FinishedAt after %s = %v, want nil", to, got.FinishedAt)
		}
	}
}

// A date-only PATCH on a book already read is how a user corrects the date.
func TestUpdateDateOnlyOnReadEntry(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow.Add(-48*time.Hour)))
	svc := newTestService(f)
	corrected := fixedNow.Add(-96 * time.Hour)

	got, err := svc.Update(context.Background(), 1, 42, nil, ptrTime(corrected))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Status != StatusRead {
		t.Fatalf("Status = %q, want it unchanged", got.Status)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(corrected) {
		t.Fatalf("FinishedAt = %v, want %v", got.FinishedAt, corrected)
	}
}

func TestUpdateDateOnlyOnNonReadEntryIsRejected(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusReading, nil)
	svc := newTestService(f)

	_, err := svc.Update(context.Background(), 1, 42, nil, ptrTime(fixedNow))
	if !errors.Is(err, ErrFinishedAtNotRead) {
		t.Fatalf("err = %v, want ErrFinishedAtNotRead", err)
	}
}

func TestUpdateRejectsFutureFinishedAt(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, nil)
	svc := newTestService(f)

	_, err := svc.Update(context.Background(), 1, 42, nil, ptrTime(fixedNow.Add(time.Second)))
	if !errors.Is(err, ErrFutureFinishedAt) {
		t.Fatalf("err = %v, want ErrFutureFinishedAt", err)
	}
}

func TestUpdateWithNeitherFieldIsRejected(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusReading, nil)
	svc := newTestService(f)

	_, err := svc.Update(context.Background(), 1, 42, nil, nil)
	if !errors.Is(err, ErrEmptyUpdate) {
		t.Fatalf("err = %v, want ErrEmptyUpdate", err)
	}
}

// An empty update must be rejected before the store is asked for the entry,
// so a client bug does not depend on whether the book exists.
func TestUpdateWithNeitherFieldDoesNotTouchTheStore(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Update(context.Background(), 1, 999, nil, nil)
	if !errors.Is(err, ErrEmptyUpdate) {
		t.Fatalf("err = %v, want ErrEmptyUpdate", err)
	}
}

func TestUpdateMissingEntryIsNotInLibrary(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Update(context.Background(), 1, 42, ptrStatus(StatusRead), nil)
	if !errors.Is(err, ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

func TestRemoveMissingEntryIsNotInLibrary(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if err := svc.Remove(context.Background(), 1, 42); !errors.Is(err, ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

func TestListNormalizesPagingAndSort(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if _, err := svc.List(context.Background(), ListParams{UserID: 1}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if f.listParams.Page != 1 {
		t.Fatalf("Page = %d, want 1", f.listParams.Page)
	}
	if f.listParams.Limit != DefaultListLimit {
		t.Fatalf("Limit = %d, want %d", f.listParams.Limit, DefaultListLimit)
	}
	if f.listParams.Sort != DefaultSort {
		t.Fatalf("Sort = %q, want %q", f.listParams.Sort, DefaultSort)
	}
}

func TestListCapsLimit(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.List(context.Background(), ListParams{UserID: 1, Page: 1, Limit: 1000})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if f.listParams.Limit != MaxListLimit {
		t.Fatalf("Limit = %d, want %d", f.listParams.Limit, MaxListLimit)
	}
}

func TestListNeverReturnsNil(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.List(context.Background(), ListParams{UserID: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("List must return an empty slice, not nil")
	}
}
