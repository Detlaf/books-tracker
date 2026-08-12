package store

import (
	"context"
	"testing"

	"github.com/kate/book-tracking/internal/books"
)

func dune() books.Book {
	return books.Book{
		ExternalID: "vol-dune",
		ISBN:       "9780441013593",
		Title:      "Dune",
		Authors:    []string{"Frank Herbert"},
		Language:   "en",
		CoverURL:   "https://example.test/dune.jpg",
		Source:     books.SourceGoogleBooks,
	}
}

func TestUpsertBooksAssignsIDs(t *testing.T) {
	s := newTestStore(t)

	stored, err := s.UpsertBooks(context.Background(), []books.Book{dune()})
	if err != nil {
		t.Fatalf("UpsertBooks: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("got %d books, want 1", len(stored))
	}
	if stored[0].ID == 0 {
		t.Fatal("UpsertBooks must return the assigned local ID")
	}
	if stored[0].Title != "Dune" {
		t.Fatalf("returned book lost its metadata: %+v", stored[0])
	}
}

// The same volume seen twice must update one row rather than create a second:
// this is the whole point of keying on external_id.
func TestUpsertBooksIsIdempotentPerVolume(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertBooks(ctx, []books.Book{dune()})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := dune()
	updated.Title = "Dune (Deluxe Edition)"
	second, err := s.UpsertBooks(ctx, []books.Book{updated})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if second[0].ID != first[0].ID {
		t.Fatalf("ID changed on re-upsert: %d then %d", first[0].ID, second[0].ID)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM books`).Scan(&count); err != nil {
		t.Fatalf("count books: %v", err)
	}
	if count != 1 {
		t.Fatalf("books rows = %d, want 1", count)
	}

	var title string
	if err := s.db.QueryRow(`SELECT title FROM books WHERE id = ?`, first[0].ID).Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "Dune (Deluxe Edition)" {
		t.Fatalf("title = %q, want the updated one", title)
	}
}

func TestUpsertBooksStoresAuthorsInOrder(t *testing.T) {
	s := newTestStore(t)
	b := dune()
	b.Authors = []string{"Neil Gaiman", "Terry Pratchett"}

	stored, err := s.UpsertBooks(context.Background(), []books.Book{b})
	if err != nil {
		t.Fatalf("UpsertBooks: %v", err)
	}

	rows, err := s.db.Query(
		`SELECT name FROM book_authors WHERE book_id = ? ORDER BY position`, stored[0].ID)
	if err != nil {
		t.Fatalf("query authors: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, name)
	}
	if len(got) != 2 || got[0] != "Neil Gaiman" || got[1] != "Terry Pratchett" {
		t.Fatalf("authors = %v, want them in position order", got)
	}
}

// Authors are replaced, not merged, so a volume that lost an author upstream
// loses it locally instead of accumulating stale rows.
func TestUpsertBooksReplacesAuthors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := dune()
	b.Authors = []string{"Frank Herbert", "Mistakenly Credited"}
	stored, err := s.UpsertBooks(ctx, []books.Book{b})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	corrected := dune()
	corrected.Authors = []string{"Frank Herbert"}
	if _, err := s.UpsertBooks(ctx, []books.Book{corrected}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM book_authors WHERE book_id = ?`, stored[0].ID).Scan(&count); err != nil {
		t.Fatalf("count authors: %v", err)
	}
	if count != 1 {
		t.Fatalf("author rows = %d, want 1 after replacement", count)
	}
}

func TestUpsertBooksHandlesMissingOptionalFields(t *testing.T) {
	s := newTestStore(t)

	stored, err := s.UpsertBooks(context.Background(), []books.Book{{
		ExternalID: "vol-anon",
		Title:      "Beowulf",
		Source:     books.SourceGoogleBooks,
	}})
	if err != nil {
		t.Fatalf("UpsertBooks: %v", err)
	}

	var isbn, language, cover any
	err = s.db.QueryRow(
		`SELECT isbn, language, cover_url FROM books WHERE id = ?`, stored[0].ID).
		Scan(&isbn, &language, &cover)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if isbn != nil || language != nil || cover != nil {
		t.Fatalf("absent fields must be NULL, got %v %v %v", isbn, language, cover)
	}
}

func TestUpsertBooksEmptyInput(t *testing.T) {
	s := newTestStore(t)

	stored, err := s.UpsertBooks(context.Background(), nil)
	if err != nil {
		t.Fatalf("UpsertBooks(nil): %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("got %d books, want none", len(stored))
	}
}
