# Reading Status (Milestone 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authenticated user keep a library — add a book, move it between `backlog` / `reading` / `read`, list it, and remove it — with the `finished_at` rules the later milestones depend on.

**Architecture:** A new `internal/library` package holds the domain types, typed error sentinels, a `Store` interface, and a `Service` carrying the only non-CRUD logic (the `finished_at` transition rules, exercised against a fake store with an injected clock). `internal/store/library.go` implements `library.Store` over SQLite, loading a page in two queries rather than N+1. `internal/server/library_handlers.go` maps HTTP to the service and typed errors to status codes, mirroring `respondBooksError`. This is exactly how `internal/auth` and `internal/books` are already laid out and wired in `server.New`.

**Tech Stack:** Go 1.25, gin v1.12, mattn/go-sqlite3, golang-migrate v4, stdlib `testing`.

## Global Constraints

- Every query is scoped by `userID(c)`. Another user's entry must be indistinguishable from one that does not exist — always `404`, never `403`. Assert this in handler tests.
- Timestamps are parsed and emitted as RFC 3339 in UTC.
- `finished_at` is always present in JSON, explicitly `null` when unset — never `omitempty`.
- List responses use `items` as an empty array, never `null`.
- `sort` is resolved through an allowlist to a fixed `ORDER BY` fragment. Never interpolate a client value into SQL.
- `POST` and `PATCH` set `updated_at` explicitly; the column default only covers insert.
- Rows with a NULL `finished_at` sort last under **both** directions of `finished_at`.
- Unrecognized errors are a logged `500`; the detail never reaches a response body.
- Sentinels required in `internal/library/errors.go`: `ErrUnknownBook`, `ErrAlreadyInLibrary`, `ErrNotInLibrary`, `ErrInvalidStatus`, `ErrFutureFinishedAt`, `ErrFinishedAtNotRead`, `ErrEmptyUpdate`, `ErrInvalidSort`.
- Out of scope: bulk add, multi-book moves, import, any endpoint resolving an ISBN or external ID straight into the library, ratings.

Run the whole suite with `go test ./...` before every commit.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/db/migrations/000006_user_books_user_status_index.up.sql` | The `(user_id, status)` index |
| `internal/db/migrations/000006_user_books_user_status_index.down.sql` | Drops it |
| `internal/library/library.go` | `Entry`, `Status`, `ParseStatus`, `Sort`, `ParseSort`, `ListParams`, limits |
| `internal/library/errors.go` | The eight typed sentinels |
| `internal/library/store.go` | The `Store` interface `Service` depends on |
| `internal/library/service.go` | Transition rules, validation, orchestration |
| `internal/library/library_test.go` | `ParseStatus` / `ParseSort` |
| `internal/library/service_test.go` | The transition table against a fake store |
| `internal/store/library.go` | SQL; implements `library.Store` |
| `internal/store/library_test.go` | Against a real temp SQLite database |
| `internal/server/library_handlers.go` | The four handlers, request/response shapes, `respondLibraryError` |
| `internal/server/library_test.go` | Status codes, `401`, cross-user scoping |
| `internal/server/server.go` | Wire `library.Service` and the four routes |

---

### Task 1: Migration 000006 — the `(user_id, status)` index

`GET /library?status=reading` has no supporting index today: `idx_user_books_finished_at` covers `(user_id, finished_at)` only. Without this the most-hit endpoint scans the table on every list view.

**Files:**
- Create: `internal/db/migrations/000006_user_books_user_status_index.up.sql`
- Create: `internal/db/migrations/000006_user_books_user_status_index.down.sql`
- Test: `internal/db/migrations_test.go` (add a test)

**Interfaces:**
- Consumes: nothing.
- Produces: schema at version 6. No Go symbols.

- [ ] **Step 1: Write the failing test**

Append to `internal/db/migrations_test.go`:

```go
func TestUserBooksUserStatusIndexExists(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	var name string
	err = database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_user_books_user_status").Scan(&name)
	if err != nil {
		t.Fatalf("idx_user_books_user_status must exist after migrations: %v", err)
	}
}
```

If `path/filepath` is not already imported in that file, add it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestUserBooksUserStatusIndexExists -v`
Expected: FAIL with `sql: no rows in result set`.

- [ ] **Step 3: Write the migration**

`internal/db/migrations/000006_user_books_user_status_index.up.sql`:

```sql
-- GET /library?status=... filters by (user_id, status); the existing
-- idx_user_books_finished_at covers (user_id, finished_at) and does not help.
CREATE INDEX IF NOT EXISTS idx_user_books_user_status
    ON user_books(user_id, status);
```

`internal/db/migrations/000006_user_books_user_status_index.down.sql`:

```sql
DROP INDEX IF EXISTS idx_user_books_user_status;
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/db/ -v`
Expected: PASS, including any existing up/down migration round-trip test.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/000006_user_books_user_status_index.up.sql \
        internal/db/migrations/000006_user_books_user_status_index.down.sql \
        internal/db/migrations_test.go
git commit -m "feat(db): index user_books on (user_id, status)"
```

---

### Task 2: `internal/library` domain types and error sentinels

`Status` is a defined string type validated in Go. The `CHECK` constraint from migration `000001` stays as a backstop, not the primary validation — a constraint violation surfacing as a 500 is not an error message.

**Files:**
- Create: `internal/library/library.go`
- Create: `internal/library/errors.go`
- Test: `internal/library/library_test.go`

**Interfaces:**
- Consumes: `books.Book` from `internal/books`.
- Produces:
  - `type Status string`; `StatusBacklog`, `StatusReading`, `StatusRead`
  - `func ParseStatus(s string) (Status, error)`
  - `type Sort string`; `SortAddedAt`, `SortAddedAtDesc`, `SortFinishedAt`, `SortFinishedAtDesc`, `SortTitle`, `SortTitleDesc`, `DefaultSort`
  - `func ParseSort(s string) (Sort, error)`
  - `type Entry struct { Book books.Book; Status Status; FinishedAt *time.Time; AddedAt time.Time }`
  - `type ListParams struct { UserID int64; Status *Status; Sort Sort; Page int; Limit int }`
  - `const DefaultListLimit = 20`, `const MaxListLimit = 100`
  - The eight sentinels listed in Global Constraints.

- [ ] **Step 1: Write the failing test**

Create `internal/library/library_test.go`:

```go
package library

import (
	"errors"
	"testing"
)

func TestParseStatusAcceptsTheThreeStatuses(t *testing.T) {
	for _, want := range []Status{StatusBacklog, StatusReading, StatusRead} {
		got, err := ParseStatus(string(want))
		if err != nil {
			t.Fatalf("ParseStatus(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseStatus(%q) = %q", want, got)
		}
	}
}

// A typo must be an error rather than a silently-ignored filter.
func TestParseStatusRejectsAnythingElse(t *testing.T) {
	for _, in := range []string{"", "READ", "finished", "backlog "} {
		if _, err := ParseStatus(in); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("ParseStatus(%q) err = %v, want ErrInvalidStatus", in, err)
		}
	}
}

func TestParseSortAcceptsEveryKeyInBothDirections(t *testing.T) {
	want := []Sort{
		SortAddedAt, SortAddedAtDesc,
		SortFinishedAt, SortFinishedAtDesc,
		SortTitle, SortTitleDesc,
	}
	for _, w := range want {
		got, err := ParseSort(string(w))
		if err != nil {
			t.Fatalf("ParseSort(%q): %v", w, err)
		}
		if got != w {
			t.Fatalf("ParseSort(%q) = %q", w, got)
		}
	}
}

func TestParseSortEmptyIsTheDefault(t *testing.T) {
	got, err := ParseSort("")
	if err != nil {
		t.Fatalf("ParseSort(\"\"): %v", err)
	}
	if got != DefaultSort {
		t.Fatalf("ParseSort(\"\") = %q, want %q", got, DefaultSort)
	}
	if DefaultSort != SortAddedAtDesc {
		t.Fatalf("DefaultSort = %q, want -added_at", DefaultSort)
	}
}

func TestParseSortRejectsUnknownKeys(t *testing.T) {
	for _, in := range []string{"author", "-author", "added", "+added_at", "added_at desc"} {
		if _, err := ParseSort(in); !errors.Is(err, ErrInvalidSort) {
			t.Fatalf("ParseSort(%q) err = %v, want ErrInvalidSort", in, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/ -v`
Expected: FAIL — the package does not exist / undefined identifiers.

- [ ] **Step 3: Write `errors.go`**

```go
package library

import "errors"

var (
	// ErrUnknownBook means the book_id has no row in books. The client is
	// expected to have obtained the ID from /books/search or /books/isbn.
	ErrUnknownBook = errors.New("unknown book")
	// ErrAlreadyInLibrary means the caller already has this book. POST adds
	// and PATCH changes; a silent upsert would let a stale client reset a
	// deliberately-set read back to backlog.
	ErrAlreadyInLibrary = errors.New("book already in library")
	// ErrNotInLibrary means the caller has no entry for this book. It also
	// covers another user's entry, which must be indistinguishable from one
	// that does not exist.
	ErrNotInLibrary = errors.New("book not in library")
	// ErrInvalidStatus means the status was not backlog, reading, or read.
	ErrInvalidStatus = errors.New("status must be backlog, reading, or read")
	// ErrInvalidSort means the sort key was not in the allowlist.
	ErrInvalidSort = errors.New("invalid sort key")
	// ErrFutureFinishedAt means a book was marked finished after now.
	ErrFutureFinishedAt = errors.New("finished_at must not be in the future")
	// ErrFinishedAtNotRead means a finished_at arrived with a status other
	// than read, which has no meaning.
	ErrFinishedAtNotRead = errors.New("finished_at is only valid with status read")
	// ErrEmptyUpdate means a PATCH carried neither status nor finished_at.
	// That is always a client bug, never a meaningful no-op.
	ErrEmptyUpdate = errors.New("update must set status or finished_at")
)
```

- [ ] **Step 4: Write `library.go`**

```go
// Package library keeps each user's books and the status they are in.
package library

import (
	"time"

	"github.com/kate/book-tracking/internal/books"
)

// Status is where a book sits in a user's library. The values match the CHECK
// constraint on user_books, which stays as a backstop: validating here means a
// typo is a 400 with a message rather than a constraint violation surfacing as
// a 500.
type Status string

const (
	StatusBacklog Status = "backlog"
	StatusReading Status = "reading"
	StatusRead    Status = "read"
)

// ParseStatus validates a client-supplied status.
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusBacklog:
		return StatusBacklog, nil
	case StatusReading:
		return StatusReading, nil
	case StatusRead:
		return StatusRead, nil
	default:
		return "", ErrInvalidStatus
	}
}

// Sort is an allowlisted list ordering. The store maps each value to a fixed
// ORDER BY fragment; the client's string never reaches SQL.
type Sort string

const (
	SortAddedAt        Sort = "added_at"
	SortAddedAtDesc    Sort = "-added_at"
	SortFinishedAt     Sort = "finished_at"
	SortFinishedAtDesc Sort = "-finished_at"
	SortTitle          Sort = "title"
	SortTitleDesc      Sort = "-title"

	// DefaultSort shows the most recently added book first, which is what a
	// user who just added one expects to see.
	DefaultSort = SortAddedAtDesc
)

// ParseSort validates a client-supplied sort. An empty value is the default;
// anything unrecognized is an error rather than a silent fallback, so a typo
// does not masquerade as a deliberate ordering.
func ParseSort(s string) (Sort, error) {
	if s == "" {
		return DefaultSort, nil
	}
	switch Sort(s) {
	case SortAddedAt, SortAddedAtDesc,
		SortFinishedAt, SortFinishedAtDesc,
		SortTitle, SortTitleDesc:
		return Sort(s), nil
	default:
		return "", ErrInvalidSort
	}
}

// Entry is one book in one user's library. FinishedAt is a pointer because
// "not finished" is a real state the API reports as an explicit null.
type Entry struct {
	Book       books.Book
	Status     Status
	FinishedAt *time.Time
	AddedAt    time.Time
}

const (
	// DefaultListLimit is the page size when a caller does not choose one.
	DefaultListLimit = 20
	// MaxListLimit caps a page. Google's ceiling of 40 does not apply: this
	// is a local join, not a quota-metered upstream call.
	MaxListLimit = 100
)

// ListParams is one page of one user's library. A nil Status means every
// status; Page is 1-based, matching /books/search.
type ListParams struct {
	UserID int64
	Status *Status
	Sort   Sort
	Page   int
	Limit  int
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/library/ -v`
Expected: PASS — all five tests.

- [ ] **Step 6: Commit**

```bash
git add internal/library/library.go internal/library/errors.go internal/library/library_test.go
git commit -m "feat(library): add Status, Sort, Entry, and error sentinels"
```

---

### Task 3: `library.Store` interface and `library.Service` transition rules

The `finished_at` rules are the only logic in the milestone that is not CRUD. They live in one pure function over `(current entry, requested status, requested finished_at, now)`, so the whole table is exhaustively unit-testable without a database or an HTTP request. "Now" is injected as a clock function so the boundary cases are testable without sleeping.

The table this implements:

| Transition | Result |
| --- | --- |
| → `read`, no date supplied, none present | set to now (UTC) |
| → `read`, no date supplied, one already present | left alone |
| → `read`, explicit date supplied | use the supplied date |
| `read` → `backlog` or `reading` | cleared to NULL |
| status unchanged, explicit date supplied | use it, only when status is `read` |

**Files:**
- Create: `internal/library/store.go`
- Create: `internal/library/service.go`
- Test: `internal/library/service_test.go`

**Interfaces:**
- Consumes: everything from Task 2.
- Produces:
  - `type Store interface { AddEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error); Entry(ctx context.Context, userID, bookID int64) (Entry, error); UpdateEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error); DeleteEntry(ctx context.Context, userID, bookID int64) error; ListEntries(ctx context.Context, p ListParams) ([]Entry, error) }`
  - `func NewService(store Store) *Service`
  - `func NewServiceWithClock(store Store, now func() time.Time) *Service`
  - `func (s *Service) Add(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error)`
  - `func (s *Service) Update(ctx context.Context, userID, bookID int64, status *Status, finishedAt *time.Time) (Entry, error)`
  - `func (s *Service) Remove(ctx context.Context, userID, bookID int64) error`
  - `func (s *Service) List(ctx context.Context, p ListParams) ([]Entry, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/library/service_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/ -v`
Expected: FAIL — `undefined: Service`, `undefined: NewServiceWithClock`, `undefined: Store`.

- [ ] **Step 3: Write `store.go`**

```go
package library

import (
	"context"
	"time"
)

// Store persists library entries. *store.SQLite implements it; the interface
// keeps this package free of database/sql and lets the transition rules be
// tested against a fake.
//
// Every method takes userID and scopes by it. An entry belonging to another
// user must come back as ErrNotInLibrary, never as someone else's row.
type Store interface {
	// AddEntry inserts a new entry. It returns ErrAlreadyInLibrary when the
	// caller already has the book and ErrUnknownBook when no such book exists.
	AddEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error)
	// Entry returns one entry, or ErrNotInLibrary.
	Entry(ctx context.Context, userID, bookID int64) (Entry, error)
	// UpdateEntry overwrites status and finished_at, and returns
	// ErrNotInLibrary when there is no such row.
	UpdateEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error)
	// DeleteEntry removes an entry, or returns ErrNotInLibrary.
	DeleteEntry(ctx context.Context, userID, bookID int64) error
	// ListEntries returns one page. Page and Limit are already normalized.
	ListEntries(ctx context.Context, p ListParams) ([]Entry, error)
}
```

- [ ] **Step 4: Write `service.go`**

```go
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
		return current.FinishedAt, nil
	}
	utc := now.UTC()
	return &utc, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/library/ -v`
Expected: PASS — every test in `library_test.go` and `service_test.go`.

- [ ] **Step 6: Commit**

```bash
git add internal/library/store.go internal/library/service.go internal/library/service_test.go
git commit -m "feat(library): add Store interface and finished_at transition rules"
```

---

### Task 4: `internal/store/library.go` — the SQL implementation

A page loads in two queries, not N+1: one join across `user_books` and `books` for the entries, then one `WHERE book_id IN (…)` for authors, keyed back together in Go. Both are bounded by `limit` — the same reasoning that made `UpsertBooks` a single batch.

**Files:**
- Create: `internal/store/library.go`
- Test: `internal/store/library_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2 and 3; `newTestStore(t)` from `internal/store/sqlite_test.go`; `nullIfEmpty` from `internal/store/books.go`.
- Produces: `*SQLite` satisfies `library.Store` — the five methods with exactly the signatures in Task 3.

- [ ] **Step 1: Write the failing test**

Create `internal/store/library_test.go`:

```go
package store

import (
	"context"
	"errors"
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

	found, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if found.Status != added.Status || found.Book.ID != bookID {
		t.Fatalf("round trip mismatch: %+v vs %+v", found, added)
	}
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

// seedLibrary gives userID three books: Anathem (backlog, no date),
// Blindsight (read, finished 2026-01-01), Cryptonomicon (read, 2026-02-01).
func seedLibrary(t *testing.T, s *SQLite, userID int64) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	ids := map[string]int64{
		"Anathem":       seedBook(t, s, "vol-a", "Anathem", []string{"Neal Stephenson"}),
		"Blindsight":    seedBook(t, s, "vol-b", "Blindsight", []string{"Peter Watts"}),
		"Cryptonomicon": seedBook(t, s, "vol-c", "Cryptonomicon", []string{"Neal Stephenson"}),
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

	// created_at is stamped from the wall clock, and three inserts in a row
	// can land in the same instant. Pin them a day apart so the added_at
	// ordering test asserts on a known sequence rather than on timing.
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for i, title := range []string{"Anathem", "Blindsight", "Cryptonomicon"} {
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

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	if !equalStrings(titles(got), []string{"Anathem"}) {
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
	if !equalStrings(titles(got), []string{"Anathem", "Blindsight", "Cryptonomicon"}) {
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
	if !equalStrings(titles(asc), []string{"Anathem", "Blindsight", "Cryptonomicon"}) {
		t.Fatalf("ascending titles = %v", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitleDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(titles(desc), []string{"Cryptonomicon", "Blindsight", "Anathem"}) {
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
	if !equalStrings(titles(asc), []string{"Anathem", "Blindsight", "Cryptonomicon"}) {
		t.Fatalf("added_at ascending = %v, want insertion order", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortAddedAtDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(titles(desc), []string{"Cryptonomicon", "Blindsight", "Anathem"}) {
		t.Fatalf("added_at descending = %v", titles(desc))
	}
}

// An unfinished book must never displace a finished one at the top of the
// list, in either direction.
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
	if !equalStrings(titles(asc), []string{"Blindsight", "Cryptonomicon", "Anathem"}) {
		t.Fatalf("finished_at ascending = %v, want NULL last", titles(asc))
	}

	desc, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortFinishedAtDesc, Page: 1, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(titles(desc), []string{"Cryptonomicon", "Blindsight", "Anathem"}) {
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
	if !equalStrings(titles(first), []string{"Anathem", "Blindsight"}) {
		t.Fatalf("page 1 = %v", titles(first))
	}

	second, err := s.ListEntries(ctx, library.ListParams{
		UserID: userID, Sort: library.SortTitle, Page: 2, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(titles(second), []string{"Cryptonomicon"}) {
		t.Fatalf("page 2 = %v", titles(second))
	}
}

func TestListEntriesLoadsAuthors(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")
	seedLibrary(t, s, userID)

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -v`
Expected: FAIL to compile — `s.AddEntry undefined (type *SQLite has no field or method AddEntry)`, and the same for the other four methods.

- [ ] **Step 3: Write `internal/store/library.go`**

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/kate/book-tracking/internal/library"
)

// entryColumns is the shared projection behind Entry and ListEntries. The
// COALESCEs turn the nullable optional columns into the empty strings
// books.Book uses, so "unknown" has one representation in Go.
const entryColumns = `b.id, b.external_id,
       COALESCE(b.isbn, ''), b.title,
       COALESCE(b.language, ''), COALESCE(b.cover_url, ''),
       COALESCE(b.metadata_source, ''),
       ub.status, ub.finished_at, ub.created_at`

// orderBy maps an already-validated Sort to a fixed fragment. The client's
// string is never interpolated: an unknown key cannot reach here, and if one
// did it would fall back to the default rather than reach SQL.
//
// "finished_at IS NULL" sorts 0 before 1 in both directions, which keeps an
// unfinished book from displacing a finished one at the top of the list. Each
// fragment ends in book_id so a page boundary is stable between requests.
var orderBy = map[library.Sort]string{
	library.SortAddedAt:        `ub.created_at ASC, ub.book_id ASC`,
	library.SortAddedAtDesc:    `ub.created_at DESC, ub.book_id DESC`,
	library.SortFinishedAt:     `ub.finished_at IS NULL, ub.finished_at ASC, ub.book_id ASC`,
	library.SortFinishedAtDesc: `ub.finished_at IS NULL, ub.finished_at DESC, ub.book_id DESC`,
	library.SortTitle:          `b.title COLLATE NOCASE ASC, ub.book_id ASC`,
	library.SortTitleDesc:      `b.title COLLATE NOCASE DESC, ub.book_id DESC`,
}

func (s *SQLite) AddEntry(ctx context.Context, userID, bookID int64, status library.Status, finishedAt *time.Time) (library.Entry, error) {
	const q = `INSERT INTO user_books (user_id, book_id, status, finished_at, created_at, updated_at)
	           VALUES (?, ?, ?, ?, ?, ?)`

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, q, userID, bookID, string(status), utcOrNil(finishedAt), now, now)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) {
			switch sqliteErr.ExtendedCode {
			case sqlite3.ErrConstraintPrimaryKey, sqlite3.ErrConstraintUnique:
				return library.Entry{}, library.ErrAlreadyInLibrary
			case sqlite3.ErrConstraintForeignKey:
				return library.Entry{}, library.ErrUnknownBook
			}
		}
		return library.Entry{}, fmt.Errorf("add library entry: %w", err)
	}
	return s.Entry(ctx, userID, bookID)
}

func (s *SQLite) Entry(ctx context.Context, userID, bookID int64) (library.Entry, error) {
	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      WHERE ub.user_id = ? AND ub.book_id = ?`

	row := s.db.QueryRowContext(ctx, q, userID, bookID)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Entry{}, library.ErrNotInLibrary
	}
	if err != nil {
		return library.Entry{}, fmt.Errorf("library entry: %w", err)
	}

	authors, err := s.authorsFor(ctx, []int64{bookID})
	if err != nil {
		return library.Entry{}, err
	}
	entry.Book.Authors = authorsOrEmpty(authors[bookID])
	return entry, nil
}

func (s *SQLite) UpdateEntry(ctx context.Context, userID, bookID int64, status library.Status, finishedAt *time.Time) (library.Entry, error) {
	// updated_at is set explicitly: the column default only covers insert.
	const q = `UPDATE user_books SET status = ?, finished_at = ?, updated_at = ?
	           WHERE user_id = ? AND book_id = ?`

	res, err := s.db.ExecContext(ctx, q,
		string(status), utcOrNil(finishedAt), time.Now().UTC(), userID, bookID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("update library entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return library.Entry{}, fmt.Errorf("update library entry: %w", err)
	}
	if n == 0 {
		return library.Entry{}, library.ErrNotInLibrary
	}
	return s.Entry(ctx, userID, bookID)
}

// DeleteEntry removes the user_books row only. The books row is shared across
// users and stays.
func (s *SQLite) DeleteEntry(ctx context.Context, userID, bookID int64) error {
	const q = `DELETE FROM user_books WHERE user_id = ? AND book_id = ?`

	res, err := s.db.ExecContext(ctx, q, userID, bookID)
	if err != nil {
		return fmt.Errorf("delete library entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete library entry: %w", err)
	}
	if n == 0 {
		return library.ErrNotInLibrary
	}
	return nil
}

// ListEntries loads a page in two queries rather than N+1: one join for the
// entries, then one batched lookup for their authors. Both are bounded by
// Limit.
func (s *SQLite) ListEntries(ctx context.Context, p library.ListParams) ([]library.Entry, error) {
	order, ok := orderBy[p.Sort]
	if !ok {
		order = orderBy[library.DefaultSort]
	}

	args := []any{p.UserID}
	where := `ub.user_id = ?`
	if p.Status != nil {
		where += ` AND ub.status = ?`
		args = append(args, string(*p.Status))
	}
	args = append(args, p.Limit, (p.Page-1)*p.Limit)

	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      WHERE ` + where + `
	      ORDER BY ` + order + `
	      LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list library entries: %w", err)
	}
	defer rows.Close()

	entries := make([]library.Entry, 0, p.Limit)
	bookIDs := make([]int64, 0, p.Limit)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("list library entries: %w", err)
		}
		entries = append(entries, entry)
		bookIDs = append(bookIDs, entry.Book.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list library entries: %w", err)
	}

	authors, err := s.authorsFor(ctx, bookIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Book.Authors = authorsOrEmpty(authors[entries[i].Book.ID])
	}
	return entries, nil
}

// rowScanner is what *sql.Row and *sql.Rows have in common, so one scan
// function serves both the single-entry and list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(sc rowScanner) (library.Entry, error) {
	var (
		e          library.Entry
		status     string
		finishedAt sql.NullTime
	)
	err := sc.Scan(
		&e.Book.ID, &e.Book.ExternalID, &e.Book.ISBN, &e.Book.Title,
		&e.Book.Language, &e.Book.CoverURL, &e.Book.Source,
		&status, &finishedAt, &e.AddedAt,
	)
	if err != nil {
		return library.Entry{}, err
	}
	e.Status = library.Status(status)
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		e.FinishedAt = &t
	}
	e.AddedAt = e.AddedAt.UTC()
	return e, nil
}

// authorsFor loads every author for a page of books in one query, keyed back
// by book ID in Go.
func (s *SQLite) authorsFor(ctx context.Context, bookIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(bookIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(bookIDs)), ",")
	q := `SELECT book_id, name FROM book_authors
	      WHERE book_id IN (` + placeholders + `)
	      ORDER BY book_id, position`

	args := make([]any, 0, len(bookIDs))
	for _, id := range bookIDs {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load authors: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bookID int64
			name   string
		)
		if err := rows.Scan(&bookID, &name); err != nil {
			return nil, fmt.Errorf("load authors: %w", err)
		}
		out[bookID] = append(out[bookID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load authors: %w", err)
	}
	return out, nil
}

// authorsOrEmpty keeps books.Book.Authors non-nil, matching what UpsertBooks
// hands back and what the JSON layer guarantees.
func authorsOrEmpty(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

// utcOrNil writes NULL for an unset finished_at and normalizes the rest, so
// every stored timestamp is comparable without a location.
func utcOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ library.Store = (*SQLite)(nil)
```

`scanEntry` populates `e.Book`'s fields without ever naming the `books` package, so `internal/store/library.go` does not import it — only the test file does.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/store/ -v`
Expected: PASS — every test in `library_test.go` plus the existing store tests.

- [ ] **Step 5: Run the whole suite and vet**

Run: `go vet ./... && go test ./...`
Expected: no vet output; all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/library.go internal/store/library_test.go
git commit -m "feat(store): implement library.Store over SQLite"
```

---

### Task 5: HTTP handlers and route wiring

Four routes on the existing `authed` group. `respondLibraryError` maps sentinels to status codes by `errors.Is`, following `respondBooksError`; anything unrecognized is a logged `500`. `bookResponse` and `newBookResponse` are reused unchanged from `book_handlers.go`.

| Route | Body / params | Success | Errors |
| --- | --- | --- | --- |
| `GET /library` | `?status=&page=&limit=&sort=` | `200` `{items, page, limit}` | `400` bad `status` or `sort` |
| `POST /library` | `{book_id, status, finished_at?}` | `201` entry | `400`, `404` unknown book, `409` already present |
| `PATCH /library/:book_id` | `{status?, finished_at?}` | `200` entry | `400`, `404` not in library |
| `DELETE /library/:book_id` | — | `204` | `404` not in library |

**Files:**
- Create: `internal/server/library_handlers.go`
- Modify: `internal/server/server.go` (add the `library` field, construct the service, register four routes)
- Test: `internal/server/library_test.go`

**Interfaces:**
- Consumes: `library.Service` and everything from Tasks 2–4; `bookResponse` / `newBookResponse` / `positiveQuery` / `respondError` from `internal/server`; `userID(c)` from `middleware.go`; `newTestServer` / `registerAndLogin` / `doJSON` / `get` from the existing test helpers.
- Produces: `s.library *library.Service` on `Server`; the four handlers `handleLibraryList`, `handleLibraryAdd`, `handleLibraryUpdate`, `handleLibraryDelete`.

- [ ] **Step 1: Write the failing test**

Create `internal/server/library_test.go`:

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/store"
)

type libraryEntryBody struct {
	Book struct {
		ID      int64    `json:"id"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	} `json:"book"`
	Status     string  `json:"status"`
	FinishedAt *string `json:"finished_at"`
	AddedAt    string  `json:"added_at"`
}

type libraryListBody struct {
	Items []libraryEntryBody `json:"items"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}

// doAuthedJSON is doJSON with a bearer token; the library routes all sit
// behind RequireAuth.
func doAuthedJSON(t *testing.T, srv *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	rec := doJSONWithHeader(t, srv, method, path, body, "Bearer "+token)
	return rec
}

// seedBookRow puts a book in the database directly, standing in for the
// /books/search call a client would make first.
func seedBookRow(t *testing.T, srv *Server, externalID, title string, authors []string) int64 {
	t.Helper()
	stored, err := store.New(srv.db).UpsertBooks(t.Context(), []books.Book{{
		ExternalID: externalID,
		Title:      title,
		Authors:    authors,
		Source:     books.SourceGoogleBooks,
	}})
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}
	return stored[0].ID
}

func TestLibraryRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/library"},
		{http.MethodPost, "/library"},
		{http.MethodPatch, "/library/1"},
		{http.MethodDelete, "/library/1"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestAddToLibraryReturns201WithTheEntry(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", []string{"Frank Herbert"})

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var body libraryEntryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Book.ID != bookID || body.Book.Title != "Dune" {
		t.Fatalf("book = %+v", body.Book)
	}
	if body.Status != "reading" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.AddedAt == "" {
		t.Fatal("added_at must be present")
	}
}

// finished_at must be an explicit null, never absent: a client should not
// have to guess whether the field is missing or the book is unfinished.
func TestAddToLibraryEmitsExplicitNullFinishedAt(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	v, ok := raw["finished_at"]
	if !ok {
		t.Fatal("finished_at must always be present")
	}
	if string(v) != "null" {
		t.Fatalf("finished_at = %s, want null", v)
	}
}

func TestAddToLibraryUnknownBookIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": 9999, "status": "backlog"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// POST adds and PATCH changes; a second POST must not silently reset a
// deliberately-set status.
func TestAddToLibraryDuplicateIs409(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	body := map[string]any{"book_id": bookID, "status": "read"}

	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken, body); rec.Code != http.StatusCreated {
		t.Fatalf("first add = %d: %s", rec.Code, rec.Body)
	}
	rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestAddToLibraryRejectsBadInput(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing status", map[string]any{"book_id": bookID}},
		{"bad status", map[string]any{"book_id": bookID, "status": "finished"}},
		{"missing book_id", map[string]any{"status": "backlog"}},
		{"future finished_at", map[string]any{"book_id": bookID, "status": "read", "finished_at": future}},
		{"finished_at with reading", map[string]any{"book_id": bookID, "status": "reading", "finished_at": "2026-01-01T00:00:00Z"}},
		{"unparseable finished_at", map[string]any{"book_id": bookID, "status": "read", "finished_at": "yesterday"}},
	}
	for _, c := range cases {
		rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400: %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestUpdateLibraryEntryStampsFinishedAt(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body libraryEntryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "read" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.FinishedAt == nil {
		t.Fatal("moving to read must stamp finished_at")
	}
	if _, err := time.Parse(time.RFC3339, *body.FinishedAt); err != nil {
		t.Fatalf("finished_at %q is not RFC 3339: %v", *body.FinishedAt, err)
	}
}

func TestUpdateLibraryEntryEmptyBodyIs400(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "reading"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestUpdateLibraryEntryMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/library/%d", bookID),
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestUpdateLibraryEntryBadBookIDIs400(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPatch, "/library/not-a-number",
		pair.AccessToken, map[string]any{"status": "read"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestDeleteLibraryEntry(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/library/%d", bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 must have an empty body, got %s", rec.Body)
	}
}

// A removal from a list the user is looking at should not silently no-op:
// that is more likely a bug than a retry.
func TestDeleteLibraryEntryMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodDelete, "/library/9999", pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestListLibraryReturnsPageEnvelope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	for _, b := range []struct {
		ext, title, status string
	}{
		{"vol-a", "Anathem", "backlog"},
		{"vol-b", "Blindsight", "read"},
	} {
		id := seedBookRow(t, srv, b.ext, b.title, []string{"Someone"})
		if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
			map[string]any{"book_id": id, "status": b.status}); rec.Code != http.StatusCreated {
			t.Fatal(rec.Body)
		}
	}

	rec := get(t, srv, "/library?sort=title", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(body.Items))
	}
	if body.Items[0].Book.Title != "Anathem" {
		t.Fatalf("sort=title did not apply: %v", body.Items[0].Book.Title)
	}
	if body.Page != 1 || body.Limit != 20 {
		t.Fatalf("page/limit = %d/%d, want 1/20", body.Page, body.Limit)
	}
}

func TestListLibraryFiltersByStatus(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	backlogID := seedBookRow(t, srv, "vol-a", "Anathem", nil)
	readID := seedBookRow(t, srv, "vol-b", "Blindsight", nil)
	for id, status := range map[int64]string{backlogID: "backlog", readID: "read"} {
		if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
			map[string]any{"book_id": id, "status": status}); rec.Code != http.StatusCreated {
			t.Fatal(rec.Body)
		}
	}

	rec := get(t, srv, "/library?status=read", "Bearer "+pair.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].Book.ID != readID {
		t.Fatalf("items = %+v, want only the read book", body.Items)
	}
}

// A typo returning 200 [] reads as "you have no books" and sends the client
// looking in the wrong place.
func TestListLibraryRejectsBadStatusAndSort(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	for _, path := range []string{"/library?status=finished", "/library?sort=author"} {
		rec := get(t, srv, path, "Bearer "+pair.AccessToken)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: %s", path, rec.Code, rec.Body)
		}
	}
}

func TestListLibraryEmptyIsAnArrayNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/library", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["items"]) != "[]" {
		t.Fatalf("items = %s, want []", raw["items"])
	}
}

func TestListLibraryCapsLimit(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/library?limit=5000", "Bearer "+pair.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Limit != 100 {
		t.Fatalf("limit = %d, want it capped at 100", body.Limit)
	}
}

// registerSecondUser adds another account to the same server and returns its
// token pair, so cross-user scoping can be asserted end to end.
func registerSecondUser(t *testing.T, srv *Server) tokenPairResponse {
	t.Helper()
	creds := map[string]string{"email": "b@b.com", "password": "password123"}

	if rec := doJSON(t, srv, http.MethodPost, "/auth/register", creds); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	rec := doJSON(t, srv, http.MethodPost, "/auth/login", creds)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var pair tokenPairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil {
		t.Fatal(err)
	}
	return pair
}

// Another user's entry must be indistinguishable from one that does not
// exist: 404 everywhere, never 403, and never visible in a list.
func TestLibraryIsScopedToTheCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLogin(t, srv)
	b := registerSecondUser(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", a.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	path := fmt.Sprintf("/library/%d", bookID)
	patch := doAuthedJSON(t, srv, http.MethodPatch, path, b.AccessToken,
		map[string]any{"status": "backlog"})
	if patch.Code != http.StatusNotFound {
		t.Fatalf("cross-user PATCH = %d, want 404: %s", patch.Code, patch.Body)
	}

	del := doAuthedJSON(t, srv, http.MethodDelete, path, b.AccessToken, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user DELETE = %d, want 404: %s", del.Code, del.Body)
	}

	list := get(t, srv, "/library", "Bearer "+b.AccessToken)
	var body libraryListBody
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("user B sees %d of user A's entries", len(body.Items))
	}

	// User A's entry is untouched by any of it.
	stillThere := get(t, srv, "/library", "Bearer "+a.AccessToken)
	var aBody libraryListBody
	if err := json.Unmarshal(stillThere.Body.Bytes(), &aBody); err != nil {
		t.Fatal(err)
	}
	if len(aBody.Items) != 1 || aBody.Items[0].Status != "read" {
		t.Fatalf("user A's entry changed: %+v", aBody.Items)
	}
}
```

- [ ] **Step 2: Add the `doJSONWithHeader` test helper**

`doJSON` in `internal/server/auth_test.go` sends no Authorization header, and the library tests need one on every write. Add this next to it in `internal/server/auth_test.go`:

```go
// doJSONWithHeader is doJSON with an Authorization header, which every route
// behind RequireAuth needs.
func doJSONWithHeader(t *testing.T, srv *Server, method, path string, body any, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -v`
Expected: FAIL — the routes 404 and `handleLibrary*` are undefined.

- [ ] **Step 4: Write `internal/server/library_handlers.go`**

```go
package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/library"
)

// addLibraryRequest identifies the book by local ID only. The client is
// expected to have hit /books/search or /books/isbn/:isbn first, both of which
// upsert the book and return its ID — which keeps provider calls, quota spend,
// and 502/429 failure modes off the library write path entirely.
//
// Status is required: defaulting it would make the most common mistake,
// omitting it, invisible.
type addLibraryRequest struct {
	BookID     int64   `json:"book_id"     binding:"required"`
	Status     string  `json:"status"      binding:"required"`
	FinishedAt *string `json:"finished_at"`
}

// updateLibraryRequest has no required fields at the binding layer; the
// service rejects a body with neither as ErrEmptyUpdate, so the message is the
// same whichever field the client forgot.
type updateLibraryRequest struct {
	Status     *string `json:"status"`
	FinishedAt *string `json:"finished_at"`
}

// libraryEntryResponse reuses bookResponse unchanged. FinishedAt is a pointer
// without omitempty so it is always present and explicitly null when unset: a
// client distinguishing "not finished" from "field absent" should not have to
// guess.
type libraryEntryResponse struct {
	Book       bookResponse `json:"book"`
	Status     string       `json:"status"`
	FinishedAt *string      `json:"finished_at"`
	AddedAt    string       `json:"added_at"`
}

func newLibraryEntryResponse(e library.Entry) libraryEntryResponse {
	var finishedAt *string
	if e.FinishedAt != nil {
		s := e.FinishedAt.UTC().Format(time.RFC3339)
		finishedAt = &s
	}
	return libraryEntryResponse{
		Book:       newBookResponse(e.Book),
		Status:     string(e.Status),
		FinishedAt: finishedAt,
		AddedAt:    e.AddedAt.UTC().Format(time.RFC3339),
	}
}

// respondLibraryError maps service errors to status codes. A missing entry and
// another user's entry are both 404: an entry the caller does not own must be
// indistinguishable from one that does not exist.
func respondLibraryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, library.ErrInvalidStatus):
		respondError(c, http.StatusBadRequest, "status must be backlog, reading, or read")
	case errors.Is(err, library.ErrInvalidSort):
		respondError(c, http.StatusBadRequest,
			"sort must be added_at, finished_at, or title, optionally prefixed with -")
	case errors.Is(err, library.ErrFutureFinishedAt):
		respondError(c, http.StatusBadRequest, "finished_at must not be in the future")
	case errors.Is(err, library.ErrFinishedAtNotRead):
		respondError(c, http.StatusBadRequest, "finished_at is only valid with status read")
	case errors.Is(err, library.ErrEmptyUpdate):
		respondError(c, http.StatusBadRequest, "update must set status or finished_at")
	case errors.Is(err, library.ErrUnknownBook):
		respondError(c, http.StatusNotFound, "no book with that id")
	case errors.Is(err, library.ErrNotInLibrary):
		respondError(c, http.StatusNotFound, "book not in your library")
	case errors.Is(err, library.ErrAlreadyInLibrary):
		respondError(c, http.StatusConflict, "book already in your library")
	default:
		log.Printf("library: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

// parseFinishedAt turns an optional RFC 3339 string into an optional time. A
// nil in means the client did not send the field.
func parseFinishedAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return nil, err
	}
	utc := t.UTC()
	return &utc, nil
}

// libraryBookID reads the :book_id path parameter.
func libraryBookID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("book_id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "book_id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) handleLibraryList(c *gin.Context) {
	// An unrecognized status is a 400 rather than an empty list: a typo that
	// returns 200 [] reads as "you have no books".
	var status *library.Status
	if raw := c.Query("status"); raw != "" {
		parsed, err := library.ParseStatus(raw)
		if err != nil {
			respondLibraryError(c, err)
			return
		}
		status = &parsed
	}

	sort, err := library.ParseSort(c.Query("sort"))
	if err != nil {
		respondLibraryError(c, err)
		return
	}

	page := positiveQuery(c, "page", 1)
	limit := positiveQuery(c, "limit", library.DefaultListLimit)
	if limit > library.MaxListLimit {
		limit = library.MaxListLimit
	}

	entries, err := s.library.List(c.Request.Context(), library.ListParams{
		UserID: userID(c),
		Status: status,
		Sort:   sort,
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		respondLibraryError(c, err)
		return
	}

	items := make([]libraryEntryResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, newLibraryEntryResponse(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "limit": limit})
}

func (s *Server) handleLibraryAdd(c *gin.Context) {
	var req addLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	status, err := library.ParseStatus(req.Status)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	finishedAt, err := parseFinishedAt(req.FinishedAt)
	if err != nil {
		respondError(c, http.StatusBadRequest, "finished_at must be an RFC 3339 timestamp")
		return
	}

	entry, err := s.library.Add(c.Request.Context(), userID(c), req.BookID, status, finishedAt)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newLibraryEntryResponse(entry))
}

func (s *Server) handleLibraryUpdate(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	var req updateLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	var status *library.Status
	if req.Status != nil {
		parsed, err := library.ParseStatus(*req.Status)
		if err != nil {
			respondLibraryError(c, err)
			return
		}
		status = &parsed
	}
	finishedAt, err := parseFinishedAt(req.FinishedAt)
	if err != nil {
		respondError(c, http.StatusBadRequest, "finished_at must be an RFC 3339 timestamp")
		return
	}

	entry, err := s.library.Update(c.Request.Context(), userID(c), bookID, status, finishedAt)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	c.JSON(http.StatusOK, newLibraryEntryResponse(entry))
}

func (s *Server) handleLibraryDelete(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	if err := s.library.Remove(c.Request.Context(), userID(c), bookID); err != nil {
		respondLibraryError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
```

A `PATCH` with an empty JSON object (`{}`) binds cleanly to `updateLibraryRequest` with both fields nil, and the service turns that into `ErrEmptyUpdate` → `400`. A `PATCH` with no body at all fails `ShouldBindJSON` → `400`. Both are the intended outcome.

- [ ] **Step 5: Wire the service and the routes in `server.go`**

Add the import:

```go
	"github.com/kate/book-tracking/internal/library"
```

Add the field to `Server`:

```go
type Server struct {
	db      *sql.DB
	router  *gin.Engine
	auth    *auth.Service
	books   *books.Service
	library *library.Service
	signer  *auth.Signer
}
```

Construct it in `New`, alongside the others:

```go
	s := &Server{
		db:      db,
		router:  gin.Default(),
		auth:    auth.NewService(sqlStore, signer, cfg.RefreshTTL),
		books:   books.NewService(books.NewGoogleBooks(cfg.GoogleBooksAPIKey), sqlStore),
		library: library.NewService(sqlStore),
		signer:  signer,
	}
```

Register the routes in `routes()`, immediately after the two `/books/...` lines. Leave the `// Milestones 5-7 hang their routes off this group.` comment where it is:

```go
	authed.GET("/library", s.handleLibraryList)
	authed.POST("/library", s.handleLibraryAdd)
	authed.PATCH("/library/:book_id", s.handleLibraryUpdate)
	authed.DELETE("/library/:book_id", s.handleLibraryDelete)
```

- [ ] **Step 6: Run the server tests**

Run: `go test ./internal/server/ -v`
Expected: PASS — every test in `library_test.go` plus the existing auth, book, and middleware tests.

- [ ] **Step 7: Run the whole suite, vet, and format**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt -l` prints nothing; no vet output; every package PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/server/library_handlers.go internal/server/library_test.go \
        internal/server/server.go internal/server/auth_test.go
git commit -m "feat(server): add library CRUD endpoints"
```

---

## Notes for the implementer

- `t.Context()` is used in `seedBookRow`. It requires Go 1.24+; this module is on 1.25, so it is available. If you prefer consistency with the rest of the suite, `context.Background()` works identically here.
- `sqlite3.ErrConstraintPrimaryKey` is the extended code a composite-`PRIMARY KEY` violation raises on `user_books`. `ErrConstraintUnique` is checked alongside it because which one SQLite reports depends on how the constraint is declared, and getting a `500` instead of a `409` here is exactly the failure `TestAddEntryDuplicateIsAlreadyInLibrary` catches.
- Foreign keys are enforced because `internal/db/db.go` sets `_foreign_keys=on` on every connection. Without it, `TestAddEntryUnknownBook` would insert happily and fail.
- The `user_id` foreign key can also fire `ErrConstraintForeignKey`, which `AddEntry` reports as `ErrUnknownBook`. That path is unreachable in practice — `userID(c)` comes from a verified token, so the user row exists.
