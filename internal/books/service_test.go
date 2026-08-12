package books

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider records what it was asked for and returns canned results.
type fakeProvider struct {
	searchResult []Book
	isbnResult   Book
	err          error

	gotQuery string
	gotISBN  string
	calls    int
}

func (f *fakeProvider) Search(_ context.Context, q string, _, _ int) ([]Book, error) {
	f.calls++
	f.gotQuery = q
	return f.searchResult, f.err
}

func (f *fakeProvider) ByISBN(_ context.Context, isbn string) (Book, error) {
	f.calls++
	f.gotISBN = isbn
	return f.isbnResult, f.err
}

// fakeRepo assigns IDs the way the real store does, without a database.
type fakeRepo struct {
	nextID int64
	err    error
}

func (r *fakeRepo) UpsertBooks(_ context.Context, in []Book) ([]Book, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]Book, 0, len(in))
	for _, b := range in {
		r.nextID++
		b.ID = r.nextID
		out = append(out, b)
	}
	return out, nil
}

func TestSearchPersistsAndReturnsLocalIDs(t *testing.T) {
	provider := &fakeProvider{searchResult: []Book{
		{ExternalID: "vol-1", Title: "Dune"},
		{ExternalID: "vol-2", Title: "Dune Messiah"},
	}}
	svc := NewService(provider, &fakeRepo{})

	found, err := svc.Search(context.Background(), "dune", 1, 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("got %d books, want 2", len(found))
	}
	for _, b := range found {
		if b.ID == 0 {
			t.Fatalf("every result must carry a local ID: %+v", b)
		}
	}
}

func TestSearchRejectsBlankQueryWithoutCallingProvider(t *testing.T) {
	provider := &fakeProvider{}
	svc := NewService(provider, &fakeRepo{})

	for _, q := range []string{"", "   ", "\t"} {
		if _, err := svc.Search(context.Background(), q, 1, 20); !errors.Is(err, ErrBlankQuery) {
			t.Fatalf("Search(%q) error = %v, want ErrBlankQuery", q, err)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("provider called %d times for a blank query; it must be called none", provider.calls)
	}
}

// No results is an empty list, not an error: an unmatched search is a normal
// outcome, unlike an ISBN that resolves to nothing.
func TestSearchWithNoResults(t *testing.T) {
	svc := NewService(&fakeProvider{searchResult: nil}, &fakeRepo{})

	found, err := svc.Search(context.Background(), "asdfghjkl", 1, 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if found == nil || len(found) != 0 {
		t.Fatalf("got %v, want a non-nil empty slice", found)
	}
}

func TestSearchPropagatesProviderError(t *testing.T) {
	svc := NewService(&fakeProvider{err: ErrUpstream}, &fakeRepo{})

	if _, err := svc.Search(context.Background(), "dune", 1, 20); !errors.Is(err, ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestByISBNNormalizesBeforeCallingProvider(t *testing.T) {
	provider := &fakeProvider{isbnResult: Book{ExternalID: "vol-1", Title: "Dune"}}
	svc := NewService(provider, &fakeRepo{})

	book, err := svc.ByISBN(context.Background(), "978-0-441-01359-3")
	if err != nil {
		t.Fatalf("ByISBN: %v", err)
	}
	if provider.gotISBN != "9780441013593" {
		t.Fatalf("provider got %q, want the normalized ISBN", provider.gotISBN)
	}
	if book.ID == 0 {
		t.Fatal("the returned book must carry a local ID")
	}
}

func TestByISBNRejectsInvalidWithoutCallingProvider(t *testing.T) {
	provider := &fakeProvider{}
	svc := NewService(provider, &fakeRepo{})

	if _, err := svc.ByISBN(context.Background(), "0441013598"); !errors.Is(err, ErrInvalidISBN) {
		t.Fatalf("error = %v, want ErrInvalidISBN", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider called %d times for an invalid ISBN; it must be called none", provider.calls)
	}
}

func TestByISBNPropagatesNotFound(t *testing.T) {
	svc := NewService(&fakeProvider{err: ErrNotFound}, &fakeRepo{})

	if _, err := svc.ByISBN(context.Background(), "9780441013593"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
