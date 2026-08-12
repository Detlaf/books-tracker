package books

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const duneVolumes = `{
  "items": [
    {
      "id": "vol-dune",
      "volumeInfo": {
        "title": "Dune",
        "authors": ["Frank Herbert"],
        "language": "en",
        "imageLinks": {"thumbnail": "https://example.test/dune.jpg"},
        "industryIdentifiers": [
          {"type": "ISBN_10", "identifier": "0441013597"},
          {"type": "ISBN_13", "identifier": "9780441013593"}
        ]
      }
    },
    {
      "id": "vol-untitled",
      "volumeInfo": {"authors": ["Nobody"]}
    },
    {
      "id": "vol-anon",
      "volumeInfo": {"title": "Beowulf"}
    }
  ]
}`

// newTestClient returns a client pointed at a stub server, plus a pointer to
// the query values of the most recent request so tests can assert on them.
func newTestClient(t *testing.T, status int, body string) (*GoogleBooks, *url.Values) {
	t.Helper()
	var lastQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query()
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &GoogleBooks{baseURL: srv.URL, client: srv.Client()}, &lastQuery
}

func TestSearchMapsVolumes(t *testing.T) {
	client, _ := newTestClient(t, http.StatusOK, duneVolumes)

	found, err := client.Search(context.Background(), "dune", 1, 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// vol-untitled is dropped: books.title is NOT NULL and a titleless volume
	// is useless to a reader. vol-anon has no authors and is kept.
	if len(found) != 2 {
		t.Fatalf("got %d books, want 2: %+v", len(found), found)
	}

	dune := found[0]
	if dune.ExternalID != "vol-dune" || dune.Title != "Dune" {
		t.Fatalf("unexpected identity: %+v", dune)
	}
	if dune.ISBN != "9780441013593" {
		t.Fatalf("ISBN = %q, want the ISBN_13", dune.ISBN)
	}
	if len(dune.Authors) != 1 || dune.Authors[0] != "Frank Herbert" {
		t.Fatalf("Authors = %v", dune.Authors)
	}
	if dune.Language != "en" || dune.CoverURL != "https://example.test/dune.jpg" {
		t.Fatalf("unexpected metadata: %+v", dune)
	}
	if dune.Source != SourceGoogleBooks {
		t.Fatalf("Source = %q, want %q", dune.Source, SourceGoogleBooks)
	}
	if len(found[1].Authors) != 0 {
		t.Fatalf("a volume with no authors must map to none, got %v", found[1].Authors)
	}
}

func TestSearchSendsPagingAndKey(t *testing.T) {
	client, query := newTestClient(t, http.StatusOK, `{"items":[]}`)
	client.apiKey = "secret-key"

	if _, err := client.Search(context.Background(), "dune", 3, 10); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := query.Get("q"); got != "dune" {
		t.Fatalf("q = %q", got)
	}
	if got := query.Get("maxResults"); got != "10" {
		t.Fatalf("maxResults = %q, want 10", got)
	}
	if got := query.Get("startIndex"); got != "20" {
		t.Fatalf("startIndex = %q, want 20 for page 3 at limit 10", got)
	}
	if got := query.Get("key"); got != "secret-key" {
		t.Fatalf("key = %q, want the configured API key", got)
	}
}

func TestSearchOmitsKeyWhenUnset(t *testing.T) {
	client, query := newTestClient(t, http.StatusOK, `{"items":[]}`)

	if _, err := client.Search(context.Background(), "dune", 1, 20); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, ok := (*query)["key"]; ok {
		t.Fatal("no key parameter should be sent when none is configured")
	}
}

func TestSearchCapsLimit(t *testing.T) {
	client, query := newTestClient(t, http.StatusOK, `{"items":[]}`)

	if _, err := client.Search(context.Background(), "dune", 1, 500); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := query.Get("maxResults"); got != "40" {
		t.Fatalf("maxResults = %q, want the 40 ceiling", got)
	}
}

func TestSearchUpstreamFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"rate limited", http.StatusTooManyRequests, `{"error":{"code":429}}`, ErrRateLimited},
		{"server error", http.StatusInternalServerError, `boom`, ErrUpstream},
		{"malformed body", http.StatusOK, `{"items": [`, ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, tc.status, tc.body)
			_, err := client.Search(context.Background(), "dune", 1, 20)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestByISBNQueriesAndUnwraps(t *testing.T) {
	client, query := newTestClient(t, http.StatusOK, duneVolumes)

	book, err := client.ByISBN(context.Background(), "9780441013593")
	if err != nil {
		t.Fatalf("ByISBN: %v", err)
	}
	if got := query.Get("q"); got != "isbn:9780441013593" {
		t.Fatalf("q = %q, want the isbn: prefix", got)
	}
	if book.ExternalID != "vol-dune" {
		t.Fatalf("got %+v, want the first volume", book)
	}
}

func TestByISBNNoMatch(t *testing.T) {
	client, _ := newTestClient(t, http.StatusOK, `{"totalItems": 0}`)

	if _, err := client.ByISBN(context.Background(), "9780441013593"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
