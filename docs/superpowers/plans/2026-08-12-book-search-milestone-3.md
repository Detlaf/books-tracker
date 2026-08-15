# Book Search & Metadata (Backend Milestone 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authenticated user search books by title/author or look one up by ISBN via Google Books, with every result persisted locally so it carries a stable `book_id` that Milestones 4–7 can address.

**Architecture:** A pure core in `internal/books` — domain type, ISBN validation, a `Provider` interface implemented by a Google Books HTTP client, and a `Service` that fetches upstream then upserts — importing neither gin nor `database/sql`. Persistence extends the existing SQLite store; thin gin handlers in `internal/server` register on the existing `authed` group. Book identity is the Google volume ID, held in a new unique `books.external_id`, and authors move out of `books.author` into a normalized `book_authors` table.

**Tech Stack:** Go 1.25, gin v1.12, SQLite (mattn/go-sqlite3), golang-migrate, `net/http` and `encoding/json` from the standard library. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-12-book-search-design.md`

## Global Constraints

- No new module dependencies. The Google Books client uses `net/http` and `encoding/json` only.
- Search limit defaults to **20**, hard-capped at **40** (Google's own `maxResults` ceiling). `page` is **1-based** and maps to `startIndex = (page - 1) * limit`.
- HTTP client timeout is **5 seconds**, and the request's `context.Context` is propagated to every upstream call.
- `GOOGLE_BOOKS_API_KEY` is **optional**. Present means `&key=` is appended; absent means a keyless call and a server that still starts.
- Upstream detail is **logged, never returned** — a Google error body can echo the request URL, and the request URL carries the API key.
- Error responses are always `{"error": "message"}`, written through the existing `respondError`.
- Status codes: 400 blank `q` or bad ISBN checksum, 401 missing/invalid token, 404 no volume for an ISBN, 429 passed through from Google, 502 for every other upstream failure.
- A volume with **no title or no volume ID is skipped**; a volume with **no authors is kept** and produces zero `book_authors` rows.
- `metadata_source` is `'google_books'` on every upserted row.
- Both endpoints are registered on the **`authed`** group and answer 401 without a bearer token.
- Tests are written first. Every task ends with a passing `go build ./... && go test ./...` and a commit.

---

### Task 1: Migration 000005 — `book_authors` and `books.external_id`

**Files:**
- Create: `internal/db/migrations/000005_book_authors_and_external_id.up.sql`
- Create: `internal/db/migrations/000005_book_authors_and_external_id.down.sql`
- Create: `internal/db/migrations_test.go`
- Modify: `internal/db/db.go` (extract a `migrator` helper from `runMigrations`)

**Interfaces:**
- Consumes: nothing.
- Produces: the schema every later task writes to — `book_authors(book_id, name, position)` and `books.external_id` with a UNIQUE index. Also produces `db.migrator(*sql.DB) (*migrate.Migrate, error)`, package-internal, used by the migration test to stop at an intermediate version.

- [ ] **Step 1: Extract the migrator helper so a test can step versions**

The existing `runMigrations` builds a `*migrate.Migrate` and immediately runs it to head, which leaves no way to stop at version 4 and write a pre-migration row. Split the construction out.

In `internal/db/db.go`, replace the body of `runMigrations` and add `migrator`:

```go
func runMigrations(db *sql.DB) error {
	m, err := migrator(db)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// migrator builds a migrate instance over the embedded migrations. It exists
// separately from runMigrations so tests can step to an intermediate version;
// production code always goes straight to head.
func migrator(db *sql.DB) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return nil, err
	}
	return migrate.NewWithInstance("iofs", src, "sqlite3", driver)
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/db/migrations_test.go`:

```go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration5BackfillsAuthors writes a book under the pre-000005 schema and
// checks the author survives the move from a column to a table. The books table
// is empty in every real deployment today, but the backfill is the destructive
// part of this migration and is worth pinning down.
func TestMigration5BackfillsAuthors(t *testing.T) {
	database, err := sql.Open("sqlite3", withConnParams(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	m, err := migrator(database)
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	if err := m.Migrate(4); err != nil {
		t.Fatalf("migrate to 4: %v", err)
	}

	res, err := database.Exec(`INSERT INTO books (title, author) VALUES ('Dune', 'Frank Herbert')`)
	if err != nil {
		t.Fatalf("insert legacy book: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	var name string
	err = database.QueryRow(
		`SELECT name FROM book_authors WHERE book_id = ? AND position = 0`, id).Scan(&name)
	if err != nil {
		t.Fatalf("read backfilled author: %v", err)
	}
	if name != "Frank Herbert" {
		t.Fatalf("author = %q, want %q", name, "Frank Herbert")
	}
}

// TestMigration5ExternalIDIsUnique checks the dedupe key the store relies on.
// SQLite allows repeated NULLs under a UNIQUE index, which is what lets rows
// predating this column coexist.
func TestMigration5ExternalIDIsUnique(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	insert := `INSERT INTO books (external_id, title) VALUES (?, ?)`
	if _, err := database.Exec(insert, "vol-1", "Dune"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := database.Exec(insert, "vol-1", "Dune reissue"); err == nil {
		t.Fatal("expected a unique constraint violation on a repeated external_id")
	}
	if _, err := database.Exec(insert, nil, "No external id"); err != nil {
		t.Fatalf("first NULL external_id: %v", err)
	}
	if _, err := database.Exec(insert, nil, "Also no external id"); err != nil {
		t.Fatalf("NULL external_id must not collide: %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/db/ -run TestMigration5 -v`
Expected: FAIL — `no such table: book_authors` (first test) and `table books has no column named external_id` (second).

- [ ] **Step 4: Write the up migration**

Create `internal/db/migrations/000005_book_authors_and_external_id.up.sql`:

```sql
CREATE TABLE IF NOT EXISTS book_authors (
    book_id  INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (book_id, position)
);

CREATE INDEX IF NOT EXISTS idx_book_authors_name ON book_authors(name);

INSERT INTO book_authors (book_id, name, position)
SELECT id, author, 0 FROM books WHERE author IS NOT NULL AND author <> '';

ALTER TABLE books DROP COLUMN author;

ALTER TABLE books ADD COLUMN external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_books_external_id ON books(external_id);
```

- [ ] **Step 5: Write the down migration**

Create `internal/db/migrations/000005_book_authors_and_external_id.down.sql`:

```sql
DROP INDEX IF EXISTS idx_books_external_id;

ALTER TABLE books DROP COLUMN external_id;

-- SQLite cannot add a NOT NULL column without a default, so the restored
-- column carries one; the original had none. Books with several authors keep
-- only the first, which is the information the single column can hold.
ALTER TABLE books ADD COLUMN author TEXT NOT NULL DEFAULT '';

UPDATE books SET author = COALESCE(
    (SELECT name FROM book_authors ba WHERE ba.book_id = books.id AND ba.position = 0), '');

DROP INDEX IF EXISTS idx_book_authors_name;

DROP TABLE book_authors;
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/db/ -v`
Expected: PASS for `TestMigration5BackfillsAuthors` and `TestMigration5ExternalIDIsUnique`.

- [ ] **Step 7: Verify the whole suite still builds and passes**

Run: `go build ./... && go test ./...`
Expected: all packages PASS. (No existing code reads `books.author`, so dropping it breaks nothing.)

- [ ] **Step 8: Commit**

```bash
git add internal/db/
git commit -m "Add book_authors table and books.external_id (migration 000005)"
```

---

### Task 2: Book domain type and ISBN validation

**Files:**
- Create: `internal/books/book.go`
- Create: `internal/books/isbn.go`
- Create: `internal/books/errors.go`
- Test: `internal/books/isbn_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `books.Book{ID int64; ExternalID, ISBN, Title string; Authors []string; Language, CoverURL, Source string}`
  - `books.SourceGoogleBooks = "google_books"`
  - `books.NormalizeISBN(raw string) (string, error)`
  - `books.ErrInvalidISBN`, `books.ErrBlankQuery`, `books.ErrNotFound`, `books.ErrUpstream`, `books.ErrRateLimited`

- [ ] **Step 1: Write the failing test**

Create `internal/books/isbn_test.go`:

```go
package books

import (
	"errors"
	"testing"
)

func TestNormalizeISBNAccepts(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"isbn-13", "9780441013593", "9780441013593"},
		{"isbn-13 hyphenated", "978-0-441-01359-3", "9780441013593"},
		{"isbn-13 spaced", "978 0 441 01359 3", "9780441013593"},
		{"isbn-10", "0441013597", "0441013597"},
		{"isbn-10 with X check digit", "080442957X", "080442957X"},
		{"isbn-10 with lowercase x", "080442957x", "080442957X"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeISBN(tc.in)
			if err != nil {
				t.Fatalf("NormalizeISBN(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeISBN(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeISBNRejects(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"too short", "12345"},
		{"too long", "97804410135931"},
		{"bad isbn-13 checksum", "9780441013594"},
		{"bad isbn-10 checksum", "0441013598"},
		{"X in the wrong place", "04410X3597"},
		{"letters", "notanisbn0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeISBN(tc.in); !errors.Is(err, ErrInvalidISBN) {
				t.Fatalf("NormalizeISBN(%q) error = %v, want ErrInvalidISBN", tc.in, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/books/ -v`
Expected: FAIL — build error, `undefined: NormalizeISBN` and `undefined: ErrInvalidISBN`.

- [ ] **Step 3: Write the domain type**

Create `internal/books/book.go`:

```go
// Package books searches an external metadata provider and gives every result
// a stable local identity.
package books

// SourceGoogleBooks is the metadata_source value for rows this package writes.
const SourceGoogleBooks = "google_books"

// Book is one volume, both as a provider returns it and as it is stored. ID is
// the local books.id and stays zero until the row has been upserted, so a
// caller can tell a fetched book from a persisted one.
type Book struct {
	ID         int64
	ExternalID string
	ISBN       string
	Title      string
	Authors    []string
	Language   string
	CoverURL   string
	Source     string
}
```

- [ ] **Step 4: Write the errors**

Create `internal/books/errors.go`:

```go
package books

import "errors"

var (
	// ErrInvalidISBN means the input was not a checksum-valid ISBN-10 or -13.
	ErrInvalidISBN = errors.New("invalid isbn")
	// ErrBlankQuery means a search arrived with an empty or whitespace-only q.
	ErrBlankQuery = errors.New("search query must not be blank")
	// ErrNotFound means the provider matched no volume.
	ErrNotFound = errors.New("no book found")
	// ErrUpstream covers every provider failure a client cannot act on: a 5xx,
	// a timeout, or a body that will not parse. The detail is wrapped for logs
	// and must not reach a response body.
	ErrUpstream = errors.New("book metadata provider unavailable")
	// ErrRateLimited is the provider's 429, kept separate because the client
	// can act on it by backing off.
	ErrRateLimited = errors.New("book metadata provider rate limit exceeded")
)
```

- [ ] **Step 5: Write the ISBN validation**

Create `internal/books/isbn.go`:

```go
package books

import "strings"

// NormalizeISBN strips separators, uppercases an X check digit, and verifies
// the checksum. Validating locally means a typo costs nothing upstream, and it
// keeps the ISBN written to the database in one canonical shape.
func NormalizeISBN(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'x' || r == 'X':
			b.WriteRune('X')
		case r == '-' || r == ' ':
			// Separator; drop it.
		default:
			return "", ErrInvalidISBN
		}
	}

	s := b.String()
	switch {
	case len(s) == 10 && validISBN10(s):
		return s, nil
	case len(s) == 13 && validISBN13(s):
		return s, nil
	default:
		return "", ErrInvalidISBN
	}
}

// validISBN10 weights the digits 10 down to 1; the total must be divisible by
// 11. Only the final digit may be X, standing for 10.
func validISBN10(s string) bool {
	sum := 0
	for i, r := range s {
		d := 0
		switch {
		case r == 'X':
			if i != 9 {
				return false
			}
			d = 10
		case r >= '0' && r <= '9':
			d = int(r - '0')
		default:
			return false
		}
		sum += d * (10 - i)
	}
	return sum%11 == 0
}

// validISBN13 alternates weights 1 and 3; the total must be divisible by 10.
// X is never a valid ISBN-13 character.
func validISBN13(s string) bool {
	sum := 0
	for i, r := range s {
		if r < '0' || r > '9' {
			return false
		}
		d := int(r - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return sum%10 == 0
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/books/ -v`
Expected: PASS — all subtests of `TestNormalizeISBNAccepts` and `TestNormalizeISBNRejects`.

- [ ] **Step 7: Commit**

```bash
go build ./... && go test ./...
git add internal/books/
git commit -m "Add book domain type and ISBN validation"
```

---

### Task 3: Google Books provider

**Files:**
- Create: `internal/books/provider.go`
- Create: `internal/books/google.go`
- Test: `internal/books/google_test.go`

**Interfaces:**
- Consumes: `books.Book`, `books.SourceGoogleBooks`, and the errors from Task 2.
- Produces:
  - `books.Provider` interface — `Search(ctx context.Context, q string, page, limit int) ([]Book, error)` and `ByISBN(ctx context.Context, isbn string) (Book, error)`
  - `books.NewGoogleBooks(apiKey string) *GoogleBooks`, which implements `Provider`
  - `books.DefaultSearchLimit = 20`, `books.MaxSearchLimit = 40`

- [ ] **Step 1: Write the failing test**

Create `internal/books/google_test.go`. The client's `baseURL` is unexported, so these tests construct `GoogleBooks` directly against an `httptest.Server` — no production seam is needed to make it testable.

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/books/ -run 'TestSearch|TestByISBN' -v`
Expected: FAIL — build error, `undefined: GoogleBooks`.

- [ ] **Step 3: Write the Provider interface**

Create `internal/books/provider.go`:

```go
package books

import "context"

// Provider is the external metadata source. Implementations return Books with
// ID left zero — local identity belongs to the store, not the provider — which
// is what lets the service swap a provider without touching persistence.
type Provider interface {
	Search(ctx context.Context, q string, page, limit int) ([]Book, error)
	ByISBN(ctx context.Context, isbn string) (Book, error)
}
```

- [ ] **Step 4: Write the Google Books client**

Create `internal/books/google.go`:

```go
package books

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	googleVolumesURL = "https://www.googleapis.com/books/v1/volumes"
	// DefaultSearchLimit is the page size when a caller does not choose one.
	DefaultSearchLimit = 20
	// MaxSearchLimit is Google's own maxResults ceiling; asking for more is an
	// error upstream, so the client clamps instead of forwarding it.
	MaxSearchLimit = 40
	// requestTimeout bounds a single upstream call. A search that has not
	// answered in five seconds is not worth holding a client request open for.
	requestTimeout = 5 * time.Second
)

// GoogleBooks queries the Google Books volumes API. baseURL is a field rather
// than a constant so tests can point it at an httptest server; nothing in
// production sets it.
type GoogleBooks struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGoogleBooks(apiKey string) *GoogleBooks {
	return &GoogleBooks{
		baseURL: googleVolumesURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: requestTimeout},
	}
}

func (g *GoogleBooks) Search(ctx context.Context, q string, page, limit int) ([]Book, error) {
	if limit < 1 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	if page < 1 {
		page = 1
	}
	return g.fetch(ctx, url.Values{
		"q":          {q},
		"maxResults": {strconv.Itoa(limit)},
		"startIndex": {strconv.Itoa((page - 1) * limit)},
	})
}

// ByISBN takes the first volume for the ISBN. Editions occasionally share an
// ISBN upstream; the first result is Google's own relevance pick.
func (g *GoogleBooks) ByISBN(ctx context.Context, isbn string) (Book, error) {
	found, err := g.fetch(ctx, url.Values{
		"q":          {"isbn:" + isbn},
		"maxResults": {"1"},
	})
	if err != nil {
		return Book{}, err
	}
	if len(found) == 0 {
		return Book{}, ErrNotFound
	}
	return found[0], nil
}

// fetch performs one volumes query. Every failure is wrapped into ErrUpstream
// or ErrRateLimited: the detail is for logs only, because a Google error body
// can echo the request URL and the request URL carries the API key.
func (g *GoogleBooks) fetch(ctx context.Context, params url.Values) ([]Book, error) {
	if g.apiKey != "" {
		params.Set("key", g.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrUpstream, err)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}

	var list volumeList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}

	out := make([]Book, 0, len(list.Items))
	for _, v := range list.Items {
		if b, ok := v.toBook(); ok {
			out = append(out, b)
		}
	}
	return out, nil
}

type volumeList struct {
	Items []volume `json:"items"`
}

type volume struct {
	ID         string `json:"id"`
	VolumeInfo struct {
		Title      string   `json:"title"`
		Authors    []string `json:"authors"`
		Language   string   `json:"language"`
		ImageLinks struct {
			Thumbnail string `json:"thumbnail"`
		} `json:"imageLinks"`
		IndustryIdentifiers []struct {
			Type       string `json:"type"`
			Identifier string `json:"identifier"`
		} `json:"industryIdentifiers"`
	} `json:"volumeInfo"`
}

// toBook reports false for a volume that cannot be stored. A missing title
// violates books.title's NOT NULL constraint and a missing ID leaves the row
// with no dedupe key, so both are dropped rather than filled with a
// placeholder that would later look like real metadata.
func (v volume) toBook() (Book, bool) {
	if v.ID == "" || strings.TrimSpace(v.VolumeInfo.Title) == "" {
		return Book{}, false
	}
	return Book{
		ExternalID: v.ID,
		ISBN:       v.isbn(),
		Title:      v.VolumeInfo.Title,
		Authors:    v.VolumeInfo.Authors,
		Language:   v.VolumeInfo.Language,
		CoverURL:   v.VolumeInfo.ImageLinks.Thumbnail,
		Source:     SourceGoogleBooks,
	}, true
}

// isbn prefers ISBN_13 and falls back to ISBN_10, which is all some older
// volumes carry. Neither present leaves the field empty.
func (v volume) isbn() string {
	var fallback string
	for _, id := range v.VolumeInfo.IndustryIdentifiers {
		switch id.Type {
		case "ISBN_13":
			return id.Identifier
		case "ISBN_10":
			fallback = id.Identifier
		}
	}
	return fallback
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/books/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 6: Commit**

```bash
go build ./... && go test ./...
git add internal/books/
git commit -m "Add Google Books provider"
```

---

### Task 4: Persist books with `UpsertBooks`

**Files:**
- Create: `internal/store/books.go`
- Test: `internal/store/books_test.go`

**Interfaces:**
- Consumes: `books.Book` (Task 2), the `book_authors` table and `books.external_id` (Task 1).
- Produces: `(*store.SQLite).UpsertBooks(ctx context.Context, in []books.Book) ([]books.Book, error)`, returning the input books with `ID` filled in, in the same order.

- [ ] **Step 1: Write the failing test**

Create `internal/store/books_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestUpsertBooks -v`
Expected: FAIL — build error, `s.UpsertBooks undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/store/books.go`:

```go
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kate/book-tracking/internal/books"
)

// UpsertBooks writes each book keyed by its provider volume ID and returns the
// input with local IDs filled in, in the same order. One transaction covers the
// whole batch, so a failure part way through a search result leaves no
// half-written books behind.
func (s *SQLite) UpsertBooks(ctx context.Context, in []books.Book) ([]books.Book, error) {
	if len(in) == 0 {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("upsert books: %w", err)
	}
	defer tx.Rollback()

	out := make([]books.Book, 0, len(in))
	for _, b := range in {
		id, err := upsertBook(ctx, tx, b)
		if err != nil {
			return nil, err
		}
		if err := replaceAuthors(ctx, tx, id, b.Authors); err != nil {
			return nil, err
		}
		b.ID = id
		out = append(out, b)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("upsert books: %w", err)
	}
	return out, nil
}

func upsertBook(ctx context.Context, tx *sql.Tx, b books.Book) (int64, error) {
	const q = `INSERT INTO books (external_id, isbn, title, language, cover_url, metadata_source)
	           VALUES (?, ?, ?, ?, ?, ?)
	           ON CONFLICT(external_id) DO UPDATE SET
	               isbn            = excluded.isbn,
	               title           = excluded.title,
	               language        = excluded.language,
	               cover_url       = excluded.cover_url,
	               metadata_source = excluded.metadata_source
	           RETURNING id`

	var id int64
	err := tx.QueryRowContext(ctx, q,
		b.ExternalID, nullIfEmpty(b.ISBN), b.Title,
		nullIfEmpty(b.Language), nullIfEmpty(b.CoverURL), b.Source,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert book %q: %w", b.ExternalID, err)
	}
	return id, nil
}

// replaceAuthors rewrites a book's author list wholesale. Merging would keep an
// author the provider has since corrected away, and the lists are small enough
// that a delete-and-insert costs nothing.
func replaceAuthors(ctx context.Context, tx *sql.Tx, bookID int64, authors []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM book_authors WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("clear authors for book %d: %w", bookID, err)
	}

	const q = `INSERT INTO book_authors (book_id, name, position) VALUES (?, ?, ?)`
	for i, name := range authors {
		if _, err := tx.ExecContext(ctx, q, bookID, name, i); err != nil {
			return fmt.Errorf("insert author for book %d: %w", bookID, err)
		}
	}
	return nil
}

// nullIfEmpty writes NULL for an absent optional field, so "unknown" has one
// representation in the database rather than two.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS — the new `TestUpsertBooks*` tests and the existing auth store tests.

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./...
git add internal/store/
git commit -m "Persist books and authors with UpsertBooks"
```

---

### Task 5: Books service

**Files:**
- Create: `internal/books/service.go`
- Test: `internal/books/service_test.go`

**Interfaces:**
- Consumes: `Provider` (Task 3), `NormalizeISBN` and the errors (Task 2), and an implementation of the `Repo` interface this task defines — satisfied by `*store.SQLite` from Task 4.
- Produces:
  - `books.Repo` interface — `UpsertBooks(ctx context.Context, in []Book) ([]Book, error)`
  - `books.NewService(provider Provider, repo Repo) *Service`
  - `(*Service).Search(ctx context.Context, q string, page, limit int) ([]Book, error)`
  - `(*Service).ByISBN(ctx context.Context, rawISBN string) (Book, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/books/service_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/books/ -run 'TestSearchPersists|TestByISBNNormalizes' -v`
Expected: FAIL — build error, `undefined: NewService`.

- [ ] **Step 3: Write the implementation**

Create `internal/books/service.go`:

```go
package books

import (
	"context"
	"strings"
)

// Repo persists books and returns them with local IDs. *store.SQLite
// implements it; the interface keeps this package free of database/sql.
type Repo interface {
	UpsertBooks(ctx context.Context, in []Book) ([]Book, error)
}

// Service fetches from a Provider and persists what it finds, so every result
// leaves this package carrying the local ID that the library, rating, and
// collection endpoints address books by.
type Service struct {
	provider Provider
	repo     Repo
}

func NewService(provider Provider, repo Repo) *Service {
	return &Service{provider: provider, repo: repo}
}

// Search validates before it spends an upstream call. An unmatched search is a
// normal outcome and returns an empty slice rather than an error.
func (s *Service) Search(ctx context.Context, q string, page, limit int) ([]Book, error) {
	if strings.TrimSpace(q) == "" {
		return nil, ErrBlankQuery
	}

	found, err := s.provider.Search(ctx, q, page, limit)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return []Book{}, nil
	}
	return s.repo.UpsertBooks(ctx, found)
}

// ByISBN normalizes and checksum-validates locally, so a typo never reaches the
// provider.
func (s *Service) ByISBN(ctx context.Context, rawISBN string) (Book, error) {
	isbn, err := NormalizeISBN(rawISBN)
	if err != nil {
		return Book{}, err
	}

	found, err := s.provider.ByISBN(ctx, isbn)
	if err != nil {
		return Book{}, err
	}

	stored, err := s.repo.UpsertBooks(ctx, []Book{found})
	if err != nil {
		return Book{}, err
	}
	return stored[0], nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/books/ -v`
Expected: PASS for every test in the package.

- [ ] **Step 5: Commit**

```bash
go build ./... && go test ./...
git add internal/books/
git commit -m "Add books service wiring provider to persistence"
```

---

### Task 6: HTTP endpoints, configuration, and wiring

**Files:**
- Create: `internal/server/book_handlers.go`
- Test: `internal/server/book_test.go`
- Modify: `internal/config/config.go` (add `GoogleBooksAPIKey`)
- Modify: `internal/config/config_test.go` (cover the new field)
- Modify: `internal/server/server.go` (add the `books` field, build the service once, register routes)
- Modify: `specs/backend/implementation.md` (record what Milestone 3 shipped)

**Interfaces:**
- Consumes: `books.NewService`, `books.NewGoogleBooks`, `(*books.Service).Search` / `.ByISBN`, `books.DefaultSearchLimit`, `books.MaxSearchLimit`, and the `books` error values; `store.New` and `RequireAuth` from Milestone 2.
- Produces: `GET /books/search?q=&page=&limit=` and `GET /books/isbn/:isbn` on the `authed` group; `config.Config.GoogleBooksAPIKey`.

- [ ] **Step 1: Write the failing config test**

Add to `internal/config/config_test.go`:

```go
// The Google Books key is deliberately optional, unlike JWT_SECRET: a missing
// key degrades quota, it does not open a security hole, and requiring one would
// block any test run or contributor without credentials.
func TestLoadGoogleBooksKeyIsOptional(t *testing.T) {
	t.Setenv("JWT_SECRET", validSecret)
	t.Setenv("GOOGLE_BOOKS_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load must succeed without a Google Books key: %v", err)
	}
	if cfg.GoogleBooksAPIKey != "" {
		t.Fatalf("GoogleBooksAPIKey = %q, want empty", cfg.GoogleBooksAPIKey)
	}
}

func TestLoadReadsGoogleBooksKey(t *testing.T) {
	t.Setenv("JWT_SECRET", validSecret)
	t.Setenv("GOOGLE_BOOKS_API_KEY", "  secret-key\n")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GoogleBooksAPIKey != "secret-key" {
		t.Fatalf("GoogleBooksAPIKey = %q, want it trimmed", cfg.GoogleBooksAPIKey)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/config/ -run GoogleBooks -v`
Expected: FAIL — build error, `cfg.GoogleBooksAPIKey undefined`.

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add the field to `Config`:

```go
type Config struct {
	DSN               string
	Addr              string
	JWTSecret         []byte
	AccessTTL         time.Duration
	RefreshTTL        time.Duration
	GoogleBooksAPIKey string
}
```

and populate it in the `Load` return, trimmed for the same reason `JWT_SECRET` is — a secret file or `docker secret` appends a newline:

```go
	return Config{
		DSN:               envOr("DATABASE_URL", "book_tracking.db"),
		Addr:              envOr("ADDR", ":8080"),
		JWTSecret:         []byte(secret),
		AccessTTL:         15 * time.Minute,
		RefreshTTL:        30 * 24 * time.Hour,
		// Optional: absent means the client calls Google keyless, which is
		// rate-limited per IP but works.
		GoogleBooksAPIKey: strings.TrimSpace(os.Getenv("GOOGLE_BOOKS_API_KEY")),
	}, nil
```

- [ ] **Step 4: Run the config tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Write the failing handler tests**

Create `internal/server/book_test.go`. These reuse `newTestServer`, `registerAndLogin`, and `get` from the existing test files, and swap in a fake provider by assigning the unexported `books` field directly — no production seam is added just for tests.

```go
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
```

- [ ] **Step 6: Run the handler tests to verify they fail**

Run: `go test ./internal/server/ -run TestBook -v`
Expected: FAIL — build error, `srv.books undefined`.

- [ ] **Step 7: Write the handlers**

Create `internal/server/book_handlers.go`:

```go
package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/books"
)

// bookResponse is the wire shape of a book. It omits metadata_source and the
// provider's own fields a client cannot use, and always emits authors so a
// client can iterate without a null check.
type bookResponse struct {
	ID         int64    `json:"id"`
	ExternalID string   `json:"external_id"`
	ISBN       string   `json:"isbn,omitempty"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors"`
	Language   string   `json:"language,omitempty"`
	CoverURL   string   `json:"cover_url,omitempty"`
}

func newBookResponse(b books.Book) bookResponse {
	authors := b.Authors
	if authors == nil {
		authors = []string{}
	}
	return bookResponse{
		ID:         b.ID,
		ExternalID: b.ExternalID,
		ISBN:       b.ISBN,
		Title:      b.Title,
		Authors:    authors,
		Language:   b.Language,
		CoverURL:   b.CoverURL,
	}
}

// respondBooksError maps service errors to status codes. Upstream detail is
// logged rather than returned: a provider error can echo the request URL, and
// the request URL carries the API key.
func respondBooksError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, books.ErrBlankQuery):
		respondError(c, http.StatusBadRequest, "query parameter q must not be blank")
	case errors.Is(err, books.ErrInvalidISBN):
		respondError(c, http.StatusBadRequest, "invalid isbn")
	case errors.Is(err, books.ErrNotFound):
		respondError(c, http.StatusNotFound, "no book found for that isbn")
	case errors.Is(err, books.ErrRateLimited):
		log.Printf("books: provider rate limited: %v", err)
		respondError(c, http.StatusTooManyRequests, "book metadata provider rate limit exceeded")
	case errors.Is(err, books.ErrUpstream):
		log.Printf("books: upstream error: %v", err)
		respondError(c, http.StatusBadGateway, "book metadata provider unavailable")
	default:
		log.Printf("books: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func (s *Server) handleBookSearch(c *gin.Context) {
	page := positiveQuery(c, "page", 1)
	limit := positiveQuery(c, "limit", books.DefaultSearchLimit)
	if limit > books.MaxSearchLimit {
		limit = books.MaxSearchLimit
	}

	found, err := s.books.Search(c.Request.Context(), c.Query("q"), page, limit)
	if err != nil {
		respondBooksError(c, err)
		return
	}

	items := make([]bookResponse, 0, len(found))
	for _, b := range found {
		items = append(items, newBookResponse(b))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "limit": limit})
}

func (s *Server) handleBookByISBN(c *gin.Context) {
	book, err := s.books.ByISBN(c.Request.Context(), c.Param("isbn"))
	if err != nil {
		respondBooksError(c, err)
		return
	}
	c.JSON(http.StatusOK, newBookResponse(book))
}

// positiveQuery reads a positive integer parameter, falling back to def when it
// is missing, unparseable, or below 1. A nonsense page number is not worth a
// 400 when a sane default exists, and it keeps paging bugs in a client from
// surfacing as errors a user sees.
func positiveQuery(c *gin.Context, key string, def int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil || v < 1 {
		return def
	}
	return v
}
```

- [ ] **Step 8: Wire the service and routes**

In `internal/server/server.go`, add the field, build the SQLite store once, and register the routes:

```go
type Server struct {
	db     *sql.DB
	router *gin.Engine
	auth   *auth.Service
	books  *books.Service
	signer *auth.Signer
}

func New(db *sql.DB, cfg config.Config) *Server {
	signer := auth.NewSigner(cfg.JWTSecret, cfg.AccessTTL)
	sqlStore := store.New(db)
	s := &Server{
		db:     db,
		router: gin.Default(),
		auth:   auth.NewService(sqlStore, signer, cfg.RefreshTTL),
		books:  books.NewService(books.NewGoogleBooks(cfg.GoogleBooksAPIKey), sqlStore),
		signer: signer,
	}
	s.routes()
	return s
}
```

Add the import `"github.com/kate/book-tracking/internal/books"`, and extend the authed group in `routes()`:

```go
	// Milestones 5-7 hang their routes off this group.
	authed := s.router.Group("/", RequireAuth(s.signer))
	authed.GET("/me", s.handleMe)
	// Behind auth because every call spends API quota and writes book rows.
	authed.GET("/books/search", s.handleBookSearch)
	authed.GET("/books/isbn/:isbn", s.handleBookByISBN)
```

- [ ] **Step 9: Run the handler tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS — the new `TestBook*` tests plus every existing auth and middleware test.

- [ ] **Step 10: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: all packages PASS.

- [ ] **Step 11: Record what shipped in the backend spec**

In `specs/backend/implementation.md`, replace the Milestone 3 section with:

```markdown
## Milestone 3 — Book Search & Metadata

- Google Books is the metadata provider; `GOOGLE_BOOKS_API_KEY` is optional and the server
  falls back to keyless calls
- `GET /books/search?q=&page=&limit=` — search by title or author. `limit` defaults to 20 and is
  capped at 40 (Google's `maxResults` ceiling); `page` is 1-based
- `GET /books/isbn/:isbn` — ISBN-10 and ISBN-13, normalized and checksum-validated locally so a
  typo costs no upstream call
- Both endpoints sit behind `RequireAuth`: each call spends API quota and writes book rows
- Every result is upserted into `books`, keyed by the Google volume ID in the new unique
  `books.external_id`, so search results carry the local `book_id` that Milestones 4–6 address
- Authors moved from `books.author` into `book_authors(book_id, name, position)` in migration
  `000005`, which backfills existing rows at position 0. Milestone 7's top-authors report groups
  on this table, so a co-authored book counts for each author
- Upstream failures are 502, except Google's 429 which is passed through; provider detail is
  logged and never returned, because an error body can echo the API key in the request URL
- Design: `docs/superpowers/specs/2026-08-12-book-search-design.md`
```

- [ ] **Step 12: Commit**

```bash
go build ./... && go test ./...
git add internal/ specs/backend/implementation.md
git commit -m "Add book search and ISBN lookup endpoints (backend Milestone 3)"
```

---

## Out of Scope

Rate limiting of our own endpoints, a `search_cache` table letting a repeated query skip Google
entirely, and a second `Provider` implementation. The `Provider` interface leaves room for the
last without committing to it now.
