package store

import (
	"context"
	"errors"
	"testing"

	"github.com/kate/book-tracking/internal/collections"
)

func TestCreateCollectionRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")

	got, err := s.Create(ctx, userID, "Summer reading")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("Create must assign an id")
	}
	if got.Name != "Summer reading" || got.UserID != userID {
		t.Fatalf("got = %+v", got)
	}
	if len(got.BookIDs) != 0 {
		t.Fatalf("BookIDs = %v, want empty", got.BookIDs)
	}
	mustBeUTC(t, "CreatedAt", got.CreatedAt)
}

func TestListReturnsOnlyTheCallersCollections(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	if _, err := s.Create(ctx, a, "A's collection"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, b, "B's collection"); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, a)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "A's collection" {
		t.Fatalf("got = %+v, want only A's collection", got)
	}
}

func TestRenameCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Original")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.Rename(ctx, userID, c.ID, "Renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Renamed" {
		t.Fatalf("Name = %q, want Renamed", got.Name)
	}
}

func TestRenameMissingIsNotFound(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	_, err := s.Rename(context.Background(), userID, 999, "Renamed")
	if !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Another user's collection id must be indistinguishable from one that does
// not exist.
func TestRenameAnotherUsersCollectionIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, s, "owner@b.com")
	other := seedUser(t, s, "other@b.com")
	c, err := s.Create(ctx, owner, "Owner's")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.Rename(ctx, other, c.ID, "Stolen")
	if !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Gone soon")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, userID, c.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := s.List(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d collections after delete, want 0", len(got))
	}
}

func TestDeleteMissingIsNotFound(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	if err := s.Delete(context.Background(), userID, 999); !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddBookToCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Test")
	if err != nil {
		t.Fatal(err)
	}
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	got, err := s.AddBook(ctx, userID, c.ID, bookID)
	if err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	if len(got.BookIDs) != 1 || got.BookIDs[0] != bookID {
		t.Fatalf("BookIDs = %v, want [%d]", got.BookIDs, bookID)
	}
}

func TestAddBookIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Test")
	if err != nil {
		t.Fatal(err)
	}
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	if _, err := s.AddBook(ctx, userID, c.ID, bookID); err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	got, err := s.AddBook(ctx, userID, c.ID, bookID)
	if err != nil {
		t.Fatalf("AddBook (repeat): %v", err)
	}
	if len(got.BookIDs) != 1 {
		t.Fatalf("BookIDs = %v, want exactly one entry", got.BookIDs)
	}
}

func TestAddUnknownBookIsUnknownBook(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.AddBook(ctx, userID, c.ID, 9999)
	if !errors.Is(err, collections.ErrUnknownBook) {
		t.Fatalf("err = %v, want ErrUnknownBook", err)
	}
}

func TestAddBookToAnotherUsersCollectionIsNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	owner := seedUser(t, s, "owner@b.com")
	other := seedUser(t, s, "other@b.com")
	c, err := s.Create(ctx, owner, "Owner's")
	if err != nil {
		t.Fatal(err)
	}
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	_, err = s.AddBook(ctx, other, c.ID, bookID)
	if !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRemoveBookFromCollection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Test")
	if err != nil {
		t.Fatal(err)
	}
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddBook(ctx, userID, c.ID, bookID); err != nil {
		t.Fatal(err)
	}

	if err := s.RemoveBook(ctx, userID, c.ID, bookID); err != nil {
		t.Fatalf("RemoveBook: %v", err)
	}

	list, err := s.List(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list[0].BookIDs) != 0 {
		t.Fatalf("BookIDs = %v, want empty after remove", list[0].BookIDs)
	}
}

func TestRemoveBookNotInCollectionIsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	c, err := s.Create(ctx, userID, "Test")
	if err != nil {
		t.Fatal(err)
	}
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	err = s.RemoveBook(ctx, userID, c.ID, bookID)
	if !errors.Is(err, collections.ErrBookNotInCollection) {
		t.Fatalf("err = %v, want ErrBookNotInCollection", err)
	}
}

func TestRemoveBookFromMissingCollectionIsNotFound(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)

	err := s.RemoveBook(context.Background(), userID, 999, bookID)
	if !errors.Is(err, collections.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// booksFor batches across multiple collections in one query; this exercises
// that path via List, which is the only caller with more than one id.
func TestListBatchesBooksAcrossCollections(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	first, err := s.Create(ctx, userID, "First")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(ctx, userID, "Second")
	if err != nil {
		t.Fatal(err)
	}
	bookA := seedBook(t, s, "vol-a", "A", nil)
	bookB := seedBook(t, s, "vol-b", "B", nil)
	if _, err := s.AddBook(ctx, userID, first.ID, bookA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBook(ctx, userID, second.ID, bookB); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[int64]collections.Collection{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if len(byID[first.ID].BookIDs) != 1 || byID[first.ID].BookIDs[0] != bookA {
		t.Fatalf("first.BookIDs = %v, want [%d]", byID[first.ID].BookIDs, bookA)
	}
	if len(byID[second.ID].BookIDs) != 1 || byID[second.ID].BookIDs[0] != bookB {
		t.Fatalf("second.BookIDs = %v, want [%d]", byID[second.ID].BookIDs, bookB)
	}
}
