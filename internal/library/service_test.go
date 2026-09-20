package library

import (
	"context"
	"errors"
	"math"
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

// fakeStore holds one user's entries in a map keyed by book ID. Ratings live
// in a separate map, independent of entries, mirroring the real SQLite
// schema where the ratings table has no FK to user_books: DeleteEntry must
// not implicitly clear a rating, only an explicit ClearRating call does.
type fakeStore struct {
	entries map[int64]Entry
	ratings map[int64]int
	err     error

	gotStatus     Status
	gotFinishedAt *time.Time
	listParams    ListParams
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[int64]Entry{}, ratings: map[int64]int{}}
}

// withRating fills in e.Rating from the ratings map at read time, since
// Entry values in f.entries never carry a rating themselves.
func (f *fakeStore) withRating(e Entry) Entry {
	if s, ok := f.ratings[e.Book.ID]; ok {
		score := s
		e.Rating = &score
	} else {
		e.Rating = nil
	}
	return e
}

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
	return f.withRating(e), nil
}

func (f *fakeStore) Entry(_ context.Context, _, bookID int64) (Entry, error) {
	if f.err != nil {
		return Entry{}, f.err
	}
	e, ok := f.entries[bookID]
	if !ok {
		return Entry{}, ErrNotInLibrary
	}
	return f.withRating(e), nil
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
	return f.withRating(e), nil
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

func (f *fakeStore) SetRating(_ context.Context, _, bookID int64, score int) error {
	if f.err != nil {
		return f.err
	}
	if _, ok := f.entries[bookID]; !ok {
		return ErrNotInLibrary
	}
	f.ratings[bookID] = score
	return nil
}

// ClearRating mirrors the real store: ratings have no FK to user_books, so
// clearing one never depends on whether a library entry exists. Deleting a
// rating that was never set (or whose entry is already gone, as when Remove
// calls this right after DeleteEntry) is a no-op, not an error.
func (f *fakeStore) ClearRating(_ context.Context, _, bookID int64) error {
	if f.err != nil {
		return f.err
	}
	delete(f.ratings, bookID)
	return nil
}

func (f *fakeStore) ListEntries(_ context.Context, p ListParams) ([]Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.listParams = p
	out := make([]Entry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, f.withRating(e))
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

// resolveFinishedAt must not hand back the caller's *time.Time on the
// left-alone path: mutating the pointer the fake store still holds must not
// change what Update already returned.
func TestUpdateToReadDoesNotAliasTheStoredPointer(t *testing.T) {
	f := newFakeStore()
	original := fixedNow.Add(-48 * time.Hour)
	storedFinishedAt := ptrTime(original)
	seed(f, 42, StatusRead, storedFinishedAt)
	svc := newTestService(f)

	got, err := svc.Update(context.Background(), 1, 42, ptrStatus(StatusRead), nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	*storedFinishedAt = fixedNow.Add(-1 * time.Hour)

	if got.FinishedAt == nil || !got.FinishedAt.Equal(original) {
		t.Fatalf("FinishedAt = %v after mutating the store's pointer, want the original %v", got.FinishedAt, original)
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

func TestRemoveClearsAnyExistingRating(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)
	if _, err := svc.SetRating(context.Background(), 1, 42, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if err := svc.Remove(context.Background(), 1, 42); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Re-seed the same book id to simulate a re-add, and confirm no stale
	// rating survived the removal.
	seed(f, 42, StatusBacklog, nil)
	got, err := f.Entry(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating != nil {
		t.Fatalf("Rating = %v, want nil after remove + re-add", got.Rating)
	}
}

// This is the store-level fact that makes Service.Remove's extra
// ClearRating call necessary: DeleteEntry alone does not touch a rating,
// so without that follow-up call a removed-then-re-added book would
// resurface a stale rating.
func TestDeleteEntryAloneDoesNotClearRating(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)
	if _, err := svc.SetRating(context.Background(), 1, 42, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if err := f.DeleteEntry(context.Background(), 1, 42); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}

	if _, ok := f.ratings[42]; !ok {
		t.Fatal("DeleteEntry alone cleared the rating — it must not; Service.Remove's ClearRating call is what should do that")
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

// A page number near math.MaxInt would make the store's (Page-1)*Limit
// overflow negative; SQLite then clamps a negative OFFSET to zero and
// returns page 1's rows mislabeled as the huge page. List must clamp Page
// before it reaches the store.
func TestListClampsHugePageSoTheOffsetCannotOverflow(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.List(context.Background(), ListParams{UserID: 1, Page: math.MaxInt, Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	offset := (f.listParams.Page - 1) * f.listParams.Limit
	if offset < 0 {
		t.Fatalf("offset = %d, want non-negative (Page=%d, Limit=%d)", offset, f.listParams.Page, f.listParams.Limit)
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

func TestSetRatingOnReadEntrySucceeds(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)

	got, err := svc.SetRating(context.Background(), 1, 42, 4)
	if err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	if got.Rating == nil || *got.Rating != 4 {
		t.Fatalf("Rating = %v, want 4", got.Rating)
	}
}

func TestSetRatingOverwritesExisting(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)

	if _, err := svc.SetRating(context.Background(), 1, 42, 2); err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	got, err := svc.SetRating(context.Background(), 1, 42, 5)
	if err != nil {
		t.Fatalf("SetRating: %v", err)
	}
	if got.Rating == nil || *got.Rating != 5 {
		t.Fatalf("Rating = %v, want 5", got.Rating)
	}
}

func TestSetRatingRejectsNonReadStatus(t *testing.T) {
	for _, status := range []Status{StatusBacklog, StatusReading} {
		f := newFakeStore()
		seed(f, 42, status, nil)
		svc := newTestService(f)

		_, err := svc.SetRating(context.Background(), 1, 42, 3)
		if !errors.Is(err, ErrRatingRequiresRead) {
			t.Fatalf("status %s: err = %v, want ErrRatingRequiresRead", status, err)
		}
	}
}

func TestSetRatingRejectsOutOfRangeScore(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)

	for _, score := range []int{0, 6, -1} {
		_, err := svc.SetRating(context.Background(), 1, 42, score)
		if !errors.Is(err, ErrInvalidRating) {
			t.Fatalf("score %d: err = %v, want ErrInvalidRating", score, err)
		}
	}
}

// The score check must not depend on whether the entry exists: a bad score
// is always a 400, even against a book the caller never added.
func TestSetRatingRejectsOutOfRangeScoreBeforeLoadingEntry(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.SetRating(context.Background(), 1, 999, 9)
	if !errors.Is(err, ErrInvalidRating) {
		t.Fatalf("err = %v, want ErrInvalidRating", err)
	}
}

func TestSetRatingMissingEntryIsNotInLibrary(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.SetRating(context.Background(), 1, 42, 3)
	if !errors.Is(err, ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

func TestClearRatingRemovesExisting(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)
	if _, err := svc.SetRating(context.Background(), 1, 42, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if err := svc.ClearRating(context.Background(), 1, 42); err != nil {
		t.Fatalf("ClearRating: %v", err)
	}
	got, err := f.Entry(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating != nil {
		t.Fatalf("Rating = %v, want nil after clear", got.Rating)
	}
}

// Clearing a rating that was never set is still success: DELETE is
// idempotent.
func TestClearRatingWithoutExistingRatingIsNoop(t *testing.T) {
	f := newFakeStore()
	seed(f, 42, StatusRead, ptrTime(fixedNow))
	svc := newTestService(f)

	if err := svc.ClearRating(context.Background(), 1, 42); err != nil {
		t.Fatalf("ClearRating: %v", err)
	}
}

func TestClearRatingMissingEntryIsNotInLibrary(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if err := svc.ClearRating(context.Background(), 1, 42); !errors.Is(err, ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}
