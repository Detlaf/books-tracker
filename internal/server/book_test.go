package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/store"
)

type stubProvider struct {
	searchResult []books.Book
	isbnResult   books.Book
	err          error

	gotPage  int
	gotLimit int
}

func (s *stubProvider) Search(_ context.Context, _ string, page, limit int) ([]books.Book, error) {
	s.gotPage, s.gotLimit = page, limit
	return s.searchResult, s.err
}

func (s *stubProvider) ByISBN(_ context.Context, _ string) (books.Book, error) {
	return s.isbnResult, s.err
}

// withProvider points the server's book service at a stub, leaving the real
// SQLite store in place so the tests still exercise persistence.
func withProvider(t *testing.T, srv *Server, p books.Provider) {
	t.Helper()
	srv.books = books.NewService(p, store.New(srv.db))
}

type bookSearchBody struct {
	Items []struct {
		ID      int64    `json:"id"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	} `json:"items"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

func TestBookSearchRequiresAuth(t *testing.T) {
	srv := newTestServer(t)

	if rec := get(t, srv, "/books/search?q=dune", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec := get(t, srv, "/books/isbn/9780441013593", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("isbn lookup status = %d, want 401", rec.Code)
	}
}

func TestBookSearchReturnsPersistedResults(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{searchResult: []books.Book{{
		ExternalID: "vol-dune",
		Title:      "Dune",
		Authors:    []string{"Frank Herbert"},
		Source:     books.SourceGoogleBooks,
	}}})
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/search?q=dune", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body bookSearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(body.Items))
	}
	if body.Items[0].ID == 0 {
		t.Fatal("results must carry the local book id")
	}
	if body.Page != 1 || body.Limit != books.DefaultSearchLimit {
		t.Fatalf("page/limit = %d/%d, want 1/%d", body.Page, body.Limit, books.DefaultSearchLimit)
	}
}

// A volume with no authors must serialize as [] rather than null, so clients
// can iterate without a nil check.
func TestBookSearchSerializesEmptyAuthors(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{searchResult: []books.Book{{
		ExternalID: "vol-anon", Title: "Beowulf", Source: books.SourceGoogleBooks,
	}}})
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/search?q=beowulf", "Bearer "+pair.AccessToken)

	var body bookSearchBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Items[0].Authors == nil {
		t.Fatal("authors must serialize as an empty array, not null")
	}
}

func TestBookSearchClampsPaging(t *testing.T) {
	srv := newTestServer(t)
	provider := &stubProvider{}
	withProvider(t, srv, provider)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/search?q=dune&page=0&limit=500", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if provider.gotPage != 1 {
		t.Fatalf("page = %d, want it clamped to 1", provider.gotPage)
	}
	if provider.gotLimit != books.MaxSearchLimit {
		t.Fatalf("limit = %d, want it capped at %d", provider.gotLimit, books.MaxSearchLimit)
	}
}

func TestBookSearchBlankQueryReturns400(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{})
	pair := registerAndLogin(t, srv)

	for _, path := range []string{"/books/search", "/books/search?q=", "/books/search?q=%20"} {
		if rec := get(t, srv, path, "Bearer "+pair.AccessToken); rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", path, rec.Code)
		}
	}
}

func TestBookSearchUpstreamFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"provider down", books.ErrUpstream, http.StatusBadGateway},
		{"rate limited", books.ErrRateLimited, http.StatusTooManyRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			withProvider(t, srv, &stubProvider{err: tc.err})
			pair := registerAndLogin(t, srv)

			rec := get(t, srv, "/books/search?q=dune", "Bearer "+pair.AccessToken)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestBookByISBNReturnsBook(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{isbnResult: books.Book{
		ExternalID: "vol-dune",
		ISBN:       "9780441013593",
		Title:      "Dune",
		Authors:    []string{"Frank Herbert"},
		Source:     books.SourceGoogleBooks,
	}})
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/isbn/978-0-441-01359-3", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body struct {
		ID    int64  `json:"id"`
		ISBN  string `json:"isbn"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID == 0 || body.Title != "Dune" || body.ISBN != "9780441013593" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestBookByISBNInvalidReturns400(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{})
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/isbn/0441013598", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestBookByISBNNotFoundReturns404(t *testing.T) {
	srv := newTestServer(t)
	withProvider(t, srv, &stubProvider{err: books.ErrNotFound})
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/books/isbn/9780441013593", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
