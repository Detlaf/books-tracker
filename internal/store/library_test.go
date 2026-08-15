package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/library"
)

func seedUser(t *testing.T, s *SQLite, email string) int64 {
	t.Helper()
	u, err := s.CreateUser(context.Background(), email, "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}

// mustBeUTC pins the milestone's "timestamps are RFC 3339 UTC" guarantee.
// time.Time.Equal ignores location, so a value-only comparison would let a
// non-UTC location slip through unnoticed.
func mustBeUTC(t *testing.T, label string, tm time.Time) {
	t.Helper()
	if tm.Location() != time.UTC {
		t.Fatalf("%s location = %v, want UTC", label, tm.Location())
	}
}

func seedBook(t *testing.T, s *SQLite, externalID, title string, authors []string) int64 {
	t.Helper()
	stored, err := s.UpsertBooks(context.Background(), []books.Book{{
		ExternalID: externalID,
		Title:      title,
		Authors:    authors,
		Source:     books.SourceGoogleBooks,
	}})
	if err != nil {
		t.Fatalf("UpsertBooks: %v", err)
	}
	return stored[0].ID
}

func TestAddEntryRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", []string{"Frank Herbert"})

	added, err := s.AddEntry(ctx, userID, bookID, library.StatusReading, nil)
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if added.Book.ID != bookID || added.Book.Title != "Dune" {
		t.Fatalf("entry lost its book: %+v", added.Book)
	}
	if len(added.Book.Authors) != 1 || added.Book.Authors[0] != "Frank Herbert" {
		t.Fatalf("authors = %v, want [Frank Herbert]", added.Book.Authors)
	}
	if added.Status != library.StatusReading {
		t.Fatalf("status = %q", added.Status)
	}
	if added.FinishedAt != nil {
		t.Fatalf("FinishedAt = %v, want nil", added.FinishedAt)
	}
	if added.AddedAt.IsZero() {
		t.Fatal("AddedAt must be populated from created_at")
	}
	mustBeUTC(t, "AddedAt", added.AddedAt)

	found, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if found.Status != added.Status || found.Book.ID != bookID {
		t.Fatalf("round trip mismatch: %+v vs %+v", found, added)
	}

	// AddEntry always writes an already-UTC created_at, so the assertion
	// above would pass even if scanEntry stopped normalizing: the driver
	// hands back whatever offset the stored string carries. Store a
	// non-UTC offset directly and confirm the read path still corrects it.
	if _, err := s.db.Exec(
		`UPDATE user_books SET created_at = ? WHERE user_id = ? AND book_id = ?`,
		"2026-01-02 03:04:05-05:00", userID, bookID); err != nil {
		t.Fatal(err)
	}
	reread, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	mustBeUTC(t, "AddedAt (non-UTC stored offset)", reread.AddedAt)
}

func TestAddEntryStoresFinishedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	finished := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	added, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, &finished)
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if added.FinishedAt == nil || !added.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt = %v, want %v", added.FinishedAt, finished)
	}
	mustBeUTC(t, "FinishedAt", *added.FinishedAt)

	// As in TestAddEntryRoundTrips: AddEntry always writes an already-UTC
	// finished_at, so the assertion above can't distinguish scanEntry's own
	// normalization from the writer's. Store a non-UTC offset directly.
	if _, err := s.db.Exec(
		`UPDATE user_books SET finished_at = ? WHERE user_id = ? AND book_id = ?`,
		"2026-01-02 03:04:05-05:00", userID, bookID); err != nil {
		t.Fatal(err)
	}
	reread, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	mustBeUTC(t, "FinishedAt (non-UTC stored offset)", *reread.FinishedAt)
}

func TestAddEntryDuplicateIsAlreadyInLibrary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddEntry(ctx, userID, bookID, library.StatusReading, nil)
	if !errors.Is(err, library.ErrAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrAlreadyInLibrary", err)
	}
}

func TestAddEntryUnknownBook(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	_, err := s.AddEntry(context.Background(), userID, 9999, library.StatusBacklog, nil)
	if !errors.Is(err, library.ErrUnknownBook) {
		t.Fatalf("err = %v, want ErrUnknownBook", err)
	}
}

// Two users may hold the same book; the entries are independent.
func TestAddEntryIsPerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	if _, err := s.AddEntry(ctx, a, bookID, library.StatusRead, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, b, bookID, library.StatusBacklog, nil); err != nil {
		t.Fatalf("second user must be able to add the same book: %v", err)
	}
}

func TestEntryIsScopedToTheUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	if _, err := s.AddEntry(ctx, a, bookID, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entry(ctx, b, bookID); !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

func TestUpdateEntryWritesBothFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}
	finished := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	updated, err := s.UpdateEntry(ctx, userID, bookID, library.StatusRead, &finished)
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if updated.Status != library.StatusRead {
		t.Fatalf("status = %q, want read", updated.Status)
	}
	if updated.FinishedAt == nil || !updated.FinishedAt.Equal(finished) {
		t.Fatalf("FinishedAt = %v, want %v", updated.FinishedAt, finished)
	}
}

func TestUpdateEntryClearsFinishedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	finished := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, &finished); err != nil {
		t.Fatal(err)
	}

	updated, err := s.UpdateEntry(ctx, userID, bookID, library.StatusBacklog, nil)
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if updated.FinishedAt != nil {
		t.Fatalf("FinishedAt = %v, want nil", updated.FinishedAt)
	}
}

// updated_at must move on a PATCH; the column default only covers insert.
func TestUpdateEntryTouchesUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.Exec(
		`UPDATE user_books SET updated_at = ? WHERE user_id = ? AND book_id = ?`,
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), userID, bookID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateEntry(ctx, userID, bookID, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}

	var updatedAt time.Time
	if err := s.db.QueryRow(
		`SELECT updated_at FROM user_books WHERE user_id = ? AND book_id = ?`,
		userID, bookID).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt.Year() == 2000 {
		t.Fatal("UpdateEntry must set updated_at")
	}
}

func TestUpdateEntryMissingRow(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	_, err := s.UpdateEntry(context.Background(), userID, bookID, library.StatusRead, nil)
	if !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

// The write must be scoped by user_id, not just readable results: user B's
// PATCH on user A's book_id must fail, and must not touch A's row.
func TestUpdateEntryIsScopedToTheUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, a, bookID, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}

	finished := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := s.UpdateEntry(ctx, b, bookID, library.StatusRead, &finished); !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}

	entry, err := s.Entry(ctx, a, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if entry.Status != library.StatusReading || entry.FinishedAt != nil {
		t.Fatalf("user B's update leaked into user A's entry: %+v", entry)
	}
}

func TestDeleteEntryRemovesTheRowButNotTheBook(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteEntry(ctx, userID, bookID); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if _, err := s.Entry(ctx, userID, bookID); !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM books WHERE id = ?`, bookID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("deleting an entry must not delete the shared book row")
	}
}

func TestDeleteEntryMissingRow(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	err := s.DeleteEntry(context.Background(), userID, 9999)
	if !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}
}

// Mirrors TestUpdateEntryIsScopedToTheUser: user B's DELETE on user A's
// book_id must fail, and A's row must still be there afterwards.
func TestDeleteEntryIsScopedToTheUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, a, bookID, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteEntry(ctx, b, bookID); !errors.Is(err, library.ErrNotInLibrary) {
		t.Fatalf("err = %v, want ErrNotInLibrary", err)
	}

	if _, err := s.Entry(ctx, a, bookID); err != nil {
		t.Fatalf("user B's delete removed user A's entry: %v", err)
	}
}

// seedLibrary gives userID four books: Anathem (backlog, no date),
// Blindsight (read, finished 2026-01-01), Cryptonomicon (read, 2026-02-01),
// Waypoint (reading, no date). Anathem and Waypoint are both unfinished so
// finished_at ordering has a NULL at each end of book_id space, and Waypoint
// sorts last by title/added_at but not by finished_at — that mismatch is
// what keeps the finished_at sort tests from coinciding with the title/
// added_at ones.
func seedLibrary(t *testing.T, s *SQLite, userID int64) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	ids := map[string]int64{
		"Anathem":       seedBook(t, s, "vol-a", "Anathem", []string{"Neal Stephenson"}),
		"Blindsight":    seedBook(t, s, "vol-b", "Blindsight", []string{"Peter Watts"}),
		"Cryptonomicon": seedBook(t, s, "vol-c", "Cryptonomicon", []string{"Neal Stephenson"}),
		"Waypoint":      seedBook(t, s, "vol-w", "Waypoint", []string{"Ann Leckie"}),
	}
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	if _, err := s.AddEntry(ctx, userID, ids["Anathem"], library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, ids["Blindsight"], library.StatusRead, &jan); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, ids["Cryptonomicon"], library.StatusRead, &feb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, ids["Waypoint"], library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}

	// created_at is stamped from the wall clock, and four inserts in a row
	// can land in the same instant. Pin them a day apart so the added_at
	// ordering test asserts on a known sequence rather than on timing.
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, title := range []string{"Anathem", "Blindsight", "Cryptonomicon", "Waypoint"} {
		if _, err := s.db.Exec(
			`UPDATE user_books SET created_at = ? WHERE user_id = ? AND book_id = ?`,
			base.Add(time.Duration(i)*24*time.Hour), userID, ids[title]); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func titles(entries []library.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Book.Title)
	}
	return out
}

func TestListEntriesFiltersByStatus(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)
	backlog := library.StatusBacklog

	got, err := s.ListEntries(context.Background(), library.ListParams{
		UserID: userID, Status: &backlog, Sort: library.SortTitle, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if !slices.Equal(titles(got), []string{"Anathem"}) {
		t.Fatalf("titles = %v, want [Anathem]", titles(got))
	}
}

func TestListEntriesNoFilterReturnsEverything(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)

	got, err := s.ListEntries(context.Background(), library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if !slices.Equal(titles(got), []string{"Anathem", "Blindsight", "Cryptonomicon", "Waypoint"}) {
		t.Fatalf("titles = %v", titles(got))
	}
}

func TestListEntriesSortsByTitleBothWays(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)
	ctx := context.Background()

	asc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(asc), []string{"Anathem", "Blindsight", "Cryptonomicon", "Waypoint"}) {
		t.Fatalf("ascending titles = %v", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitleDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(desc), []string{"Waypoint", "Cryptonomicon", "Blindsight", "Anathem"}) {
		t.Fatalf("descending titles = %v", titles(desc))
	}
}

func TestListEntriesSortsByAddedAt(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)
	ctx := context.Background()

	asc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortAddedAt, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(asc), []string{"Anathem", "Blindsight", "Cryptonomicon", "Waypoint"}) {
		t.Fatalf("added_at ascending = %v, want insertion order", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortAddedAtDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(desc), []string{"Waypoint", "Cryptonomicon", "Blindsight", "Anathem"}) {
		t.Fatalf("added_at descending = %v", titles(desc))
	}
}

// An unfinished book must never displace a finished one at the top of the
// list, in either direction. Anathem and Waypoint are both unfinished, so
// each direction also has to place two NULLs correctly relative to each
// other (by book_id) — and neither expected sequence here matches the
// title or added_at orderings for the same fixture, so this can't pass by
// accident of a sort that isn't actually keyed on finished_at.
func TestListEntriesSortsNullFinishedAtLast(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)
	ctx := context.Background()

	asc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortFinishedAt, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(asc), []string{"Blindsight", "Cryptonomicon", "Anathem", "Waypoint"}) {
		t.Fatalf("finished_at ascending = %v, want NULL last", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortFinishedAtDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(desc), []string{"Cryptonomicon", "Blindsight", "Waypoint", "Anathem"}) {
		t.Fatalf("finished_at descending = %v, want NULL last", titles(desc))
	}
}

func TestListEntriesPages(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)
	ctx := context.Background()

	first, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 1, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(first), []string{"Anathem", "Blindsight"}) {
		t.Fatalf("page 1 = %v", titles(first))
	}

	second, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 2, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(titles(second), []string{"Cryptonomicon", "Waypoint"}) {
		t.Fatalf("page 2 = %v", titles(second))
	}
}

func TestListEntriesLoadsAuthors(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	ids := seedLibrary(t, s, userID)

	// Every writer in this codebase already stores UTC-formatted timestamps,
	// so a plain Location() check below would pass even if scanEntry stopped
	// normalizing. Store one row's created_at and finished_at with a non-UTC
	// offset directly, so the assertions are pinned to scanEntry's own work.
	if _, err := s.db.Exec(
		`UPDATE user_books SET created_at = ? WHERE user_id = ? AND book_id = ?`,
		"2026-06-01 00:00:00-05:00", userID, ids["Anathem"]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`UPDATE user_books SET finished_at = ? WHERE user_id = ? AND book_id = ?`,
		"2026-01-01 00:00:00-05:00", userID, ids["Blindsight"]); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListEntries(context.Background(), library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if len(e.Book.Authors) == 0 {
			t.Fatalf("%q lost its authors", e.Book.Title)
		}
		mustBeUTC(t, e.Book.Title+" AddedAt", e.AddedAt)
		if e.FinishedAt != nil {
			mustBeUTC(t, e.Book.Title+" FinishedAt", *e.FinishedAt)
		}
	}
	if got[0].Book.Authors[0] != "Neal Stephenson" {
		t.Fatalf("Anathem authors = %v", got[0].Book.Authors)
	}
}

func TestListEntriesIsScopedToTheUser(t *testing.T) {
	s := newTestStore(t)
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	seedLibrary(t, s, a)

	got, err := s.ListEntries(context.Background(), library.ListParams{
		UserID: b, Sort: library.SortTitle, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("user B sees %d of user A's entries", len(got))
	}
}
