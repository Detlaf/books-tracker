# Milestone 7 — Reporting & Statistics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `web/src/lib/reports.js`'s client-side arithmetic (computed from `GET /library`) with six real backend endpoints (`/stats/summary`, `/stats/by-year`, `/stats/by-month`, `/stats/by-language`, `/stats/top-authors`, `/stats/streak`), and wire the Reports screen to them.

**Architecture:** A new `internal/stats` package (types + a thin validate-then-delegate `Service` over a `Store` interface, no state machine) backed by `internal/store/stats.go` (SQL aggregation against the existing `user_books`/`books`/`book_authors` tables, no migration needed) and exposed via `internal/server/stats_handlers.go` on the existing `authed` route group. The frontend gets a matching `src/api/stats.js` + `src/stores/stats.js`, and `reports.js`/`ReportsView.vue` shrink to pure presentation over the API's shape.

**Tech Stack:** Go 1.x, gin, `database/sql` + `mattn/go-sqlite3`, Vue 3 + Pinia, Vitest.

## Global Constraints

- All six routes hang off the existing `authed` gin group (`internal/server/server.go`), scoped by `userID(c)`.
- `scope` query param is the literal string `all` or a four-digit year (`^\d{4}$`); anything else is `400`.
- A bad `year` on `by-month` is also a `400`. There is no `404` anywhere in this milestone — zero books is a valid answer.
- `current_year` in `/stats/summary` is the server's UTC year (`now.UTC().Year()`), threaded through via an injected clock (`func() time.Time`), the same pattern `library.Service` uses, so boundary cases are testable without sleeping.
- Every list-shaped response field (`years`, `languages`, `authors`) is `[]`, never `null`.
- No DB migration: every field comes from `books`, `book_authors`, `user_books` (present since migrations 000001–000005); `idx_user_books_finished_at` and `idx_user_books_user_status` already cover the query shapes here.
- Module path: `github.com/kate/book-tracking`.

---

## Task 1: `internal/stats` package — types, errors, Store interface, Service

**Files:**
- Create: `internal/stats/stats.go`
- Create: `internal/stats/errors.go`
- Create: `internal/stats/store.go`
- Create: `internal/stats/service.go`
- Create: `internal/stats/service_test.go`

**Interfaces:**
- Produces (used by Task 2's `internal/store/stats.go` and Task 3's handlers):
  - `stats.Summary{TotalRead, Reading, Backlog, ThisYear, LastYear, CurrentYear, UndatedRead int}`
  - `stats.YearCount{Year string; Count int}`
  - `stats.MonthCount{Month, Count int}`
  - `stats.LanguageCount{Language string; Count int}`
  - `stats.AuthorCount{Author string; Count int}`
  - `stats.ErrInvalidScope`, `stats.ErrInvalidYear`
  - `stats.Store` interface (six methods, exact signatures below)
  - `stats.NewService(store Store) *Service`, `stats.NewServiceWithClock(store Store, now func() time.Time) *Service`
  - `(*Service) Summary(ctx, userID int64) (Summary, error)`
  - `(*Service) ByYear(ctx, userID int64) ([]YearCount, int, error)` — int is undated count
  - `(*Service) ByMonth(ctx, userID int64, year string) (string, []MonthCount, error)` — string is the resolved year (defaulted if `year == ""`)
  - `(*Service) ByLanguage(ctx, userID int64, scope string) ([]LanguageCount, error)` — `scope == ""` defaults to `"all"`
  - `(*Service) TopAuthors(ctx, userID int64, scope string, limit int) ([]AuthorCount, error)`
  - `(*Service) Streak(ctx, userID int64) (int, error)`
  - `stats.DefaultTopAuthorsLimit = 5`, `stats.MaxTopAuthorsLimit = 50`

- [ ] **Step 1: Write `internal/stats/stats.go`**

```go
// Package stats computes each user's reading statistics — summary counts,
// yearly/monthly breakdowns, language and author rankings, and reading
// streaks — over their library.
package stats

// Summary is the headline numbers shown at the top of the Reports screen.
// CurrentYear is the server's UTC year, echoed back so the client never has
// to reconcile its own clock against the server's idea of "this year".
type Summary struct {
	TotalRead   int
	Reading     int
	Backlog     int
	ThisYear    int
	LastYear    int
	CurrentYear int
	UndatedRead int
}

// YearCount is one year's count of read books with a finished_at date.
// Year is a string, not an int: it is an opaque label, not a number to do
// arithmetic on, matching how the rest of the codebase treats it.
type YearCount struct {
	Year  string
	Count int
}

// MonthCount is one calendar month's count within a single year. Month is
// 1-12.
type MonthCount struct {
	Month int
	Count int
}

// LanguageCount is one language's count of read books within a scope.
type LanguageCount struct {
	Language string
	Count    int
}

// AuthorCount is one author's count of read books within a scope. A book
// with several authors counts once per author.
type AuthorCount struct {
	Author string
	Count  int
}
```

- [ ] **Step 2: Write `internal/stats/errors.go`**

```go
package stats

import "errors"

var (
	// ErrInvalidScope means scope was neither "all" nor a four-digit year.
	ErrInvalidScope = errors.New(`scope must be "all" or a four-digit year`)
	// ErrInvalidYear means year was not a four-digit year.
	ErrInvalidYear = errors.New("year must be a four-digit year")
)
```

- [ ] **Step 3: Write `internal/stats/store.go`**

```go
package stats

import (
	"context"
	"time"
)

// Store aggregates one user's reading statistics. *store.SQLite implements
// it; the interface keeps this package free of database/sql and lets
// scope/year validation and limit clamping be tested against a fake.
//
// Every method takes userID and scopes by it — a caller only ever sees
// counts and rankings over their own books.
type Store interface {
	// Summary aggregates counts and this/last year totals. now is threaded
	// through rather than read from the system clock so the year boundary
	// is testable without sleeping past a rollover.
	Summary(ctx context.Context, userID int64, now time.Time) (Summary, error)
	// ByYear groups dated read books by year, ascending. The returned int is
	// the count of read books with no finished_at.
	ByYear(ctx context.Context, userID int64) ([]YearCount, int, error)
	// ByMonth returns exactly 12 zero-filled entries for the given year.
	// year arrives already validated as a four-digit string.
	ByMonth(ctx context.Context, userID int64, year string) ([]MonthCount, error)
	// ByLanguage groups read books by language within scope ("all" or a
	// four-digit year, already validated), sorted by count descending then
	// language ascending.
	ByLanguage(ctx context.Context, userID int64, scope string) ([]LanguageCount, error)
	// TopAuthors groups read books by author within scope, sorted by count
	// descending then author ascending, capped at limit.
	TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]AuthorCount, error)
	// Streak counts consecutive calendar months, counting back from the
	// last completed month before now, with at least one read book
	// finished in that month.
	Streak(ctx context.Context, userID int64, now time.Time) (int, error)
}
```

- [ ] **Step 4: Write `internal/stats/service.go`**

```go
package stats

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

const (
	// DefaultTopAuthorsLimit is top-authors' limit when the caller sends
	// none.
	DefaultTopAuthorsLimit = 5
	// MaxTopAuthorsLimit caps limit so an unbounded request cannot return
	// someone's entire author list in one response.
	MaxTopAuthorsLimit = 50
)

var yearPattern = regexp.MustCompile(`^\d{4}$`)

// Service has no state-machine or CRUD logic — every method is a thin
// validate-then-delegate, unlike library.Service or collections.Service.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return NewServiceWithClock(store, func() time.Time { return time.Now().UTC() })
}

// NewServiceWithClock injects the clock so summary's year math and streak's
// "last completed month" are testable without sleeping past a rollover.
func NewServiceWithClock(store Store, now func() time.Time) *Service {
	return &Service{store: store, now: now}
}

// validateScope reports whether scope is "all" or a four-digit year.
func validateScope(scope string) error {
	if scope == "all" || yearPattern.MatchString(scope) {
		return nil
	}
	return ErrInvalidScope
}

// clampLimit defaults a non-positive limit and caps an oversized one.
func clampLimit(limit int) int {
	if limit < 1 {
		return DefaultTopAuthorsLimit
	}
	if limit > MaxTopAuthorsLimit {
		return MaxTopAuthorsLimit
	}
	return limit
}

func (s *Service) Summary(ctx context.Context, userID int64) (Summary, error) {
	return s.store.Summary(ctx, userID, s.now())
}

// ByYear normalizes a nil result to an empty slice so callers never have to
// distinguish "no dated reads" from "store returned nil".
func (s *Service) ByYear(ctx context.Context, userID int64) ([]YearCount, int, error) {
	years, undated, err := s.store.ByYear(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	if years == nil {
		years = []YearCount{}
	}
	return years, undated, nil
}

// ByMonth resolves an empty year to the server's current UTC year and
// returns the resolved year alongside the months, so the handler can echo
// it back without recomputing "now" itself.
func (s *Service) ByMonth(ctx context.Context, userID int64, year string) (string, []MonthCount, error) {
	if year == "" {
		year = fmt.Sprintf("%04d", s.now().Year())
	} else if !yearPattern.MatchString(year) {
		return "", nil, ErrInvalidYear
	}

	months, err := s.store.ByMonth(ctx, userID, year)
	if err != nil {
		return "", nil, err
	}
	return year, months, nil
}

// ByLanguage resolves an empty scope to "all".
func (s *Service) ByLanguage(ctx context.Context, userID int64, scope string) ([]LanguageCount, error) {
	if scope == "" {
		scope = "all"
	} else if err := validateScope(scope); err != nil {
		return nil, err
	}

	languages, err := s.store.ByLanguage(ctx, userID, scope)
	if err != nil {
		return nil, err
	}
	if languages == nil {
		languages = []LanguageCount{}
	}
	return languages, nil
}

// TopAuthors resolves an empty scope to "all" and clamps limit.
func (s *Service) TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]AuthorCount, error) {
	if scope == "" {
		scope = "all"
	} else if err := validateScope(scope); err != nil {
		return nil, err
	}

	authors, err := s.store.TopAuthors(ctx, userID, scope, clampLimit(limit))
	if err != nil {
		return nil, err
	}
	if authors == nil {
		authors = []AuthorCount{}
	}
	return authors, nil
}

func (s *Service) Streak(ctx context.Context, userID int64) (int, error) {
	return s.store.Streak(ctx, userID, s.now())
}
```

- [ ] **Step 5: Write `internal/stats/service_test.go`**

```go
package stats

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeStore records the args each method was called with and returns
// canned results, so Service's validation/defaulting logic can be tested
// without a database.
type fakeStore struct {
	byMonthYear     string
	byLanguageScope string
	topAuthorsScope string
	topAuthorsLimit int
}

func (f *fakeStore) Summary(_ context.Context, _ int64, _ time.Time) (Summary, error) {
	return Summary{TotalRead: 1}, nil
}

func (f *fakeStore) ByYear(_ context.Context, _ int64) ([]YearCount, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ByMonth(_ context.Context, _ int64, year string) ([]MonthCount, error) {
	f.byMonthYear = year
	return make([]MonthCount, 12), nil
}

func (f *fakeStore) ByLanguage(_ context.Context, _ int64, scope string) ([]LanguageCount, error) {
	f.byLanguageScope = scope
	return nil, nil
}

func (f *fakeStore) TopAuthors(_ context.Context, _ int64, scope string, limit int) ([]AuthorCount, error) {
	f.topAuthorsScope = scope
	f.topAuthorsLimit = limit
	return nil, nil
}

func (f *fakeStore) Streak(_ context.Context, _ int64, _ time.Time) (int, error) {
	return 4, nil
}

func fixedNow() time.Time { return time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC) }

func TestByMonthDefaultsYearToNow(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	year, months, err := svc.ByMonth(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if year != "2026" {
		t.Fatalf("year = %q, want 2026", year)
	}
	if f.byMonthYear != "2026" {
		t.Fatalf("store received year = %q, want 2026", f.byMonthYear)
	}
	if len(months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(months))
	}
}

func TestByMonthRejectsBadYear(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	for _, year := range []string{"20xx", "20266", "202", "all"} {
		if _, _, err := svc.ByMonth(context.Background(), 1, year); !errors.Is(err, ErrInvalidYear) {
			t.Fatalf("year %q: err = %v, want ErrInvalidYear", year, err)
		}
	}
}

func TestByMonthAcceptsValidYear(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	year, _, err := svc.ByMonth(context.Background(), 1, "2024")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if year != "2024" {
		t.Fatalf("year = %q, want 2024", year)
	}
}

func TestByLanguageDefaultsScopeToAll(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.ByLanguage(context.Background(), 1, ""); err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if f.byLanguageScope != "all" {
		t.Fatalf("scope = %q, want all", f.byLanguageScope)
	}
}

func TestByLanguageRejectsBadScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	for _, scope := range []string{"20xx", "24", "20266", "reading"} {
		if _, err := svc.ByLanguage(context.Background(), 1, scope); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("scope %q: err = %v, want ErrInvalidScope", scope, err)
		}
	}
}

func TestByLanguageAcceptsYearScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.ByLanguage(context.Background(), 1, "2025"); err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if f.byLanguageScope != "2025" {
		t.Fatalf("scope = %q, want 2025", f.byLanguageScope)
	}
}

func TestTopAuthorsClampsLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, DefaultTopAuthorsLimit},
		{-5, DefaultTopAuthorsLimit},
		{3, 3},
		{50, 50},
		{51, MaxTopAuthorsLimit},
		{10000, MaxTopAuthorsLimit},
	}
	for _, c := range cases {
		f := &fakeStore{}
		svc := NewServiceWithClock(f, fixedNow)
		if _, err := svc.TopAuthors(context.Background(), 1, "all", c.in); err != nil {
			t.Fatalf("in %d: TopAuthors: %v", c.in, err)
		}
		if f.topAuthorsLimit != c.want {
			t.Fatalf("in %d: limit = %d, want %d", c.in, f.topAuthorsLimit, c.want)
		}
	}
}

func TestTopAuthorsRejectsBadScope(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	if _, err := svc.TopAuthors(context.Background(), 1, "not-a-year", 5); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("err = %v, want ErrInvalidScope", err)
	}
}

func TestByYearNeverReturnsNilSlice(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	years, _, err := svc.ByYear(context.Background(), 1)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if years == nil {
		t.Fatal("ByYear must return an empty slice, not nil")
	}
}

func TestStreakDelegatesToStoreWithNow(t *testing.T) {
	f := &fakeStore{}
	svc := NewServiceWithClock(f, fixedNow)

	got, err := svc.Streak(context.Background(), 1)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 4 {
		t.Fatalf("Streak = %d, want 4", got)
	}
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/stats/...`
Expected: PASS (all tests above compile and pass against the fake store).

- [ ] **Step 7: Commit**

```bash
git add internal/stats
git commit -m "Add internal/stats package: types, errors, Store interface, Service"
```

---

## Task 2: `internal/store/stats.go` — SQL aggregation

**Files:**
- Create: `internal/store/stats.go`
- Create: `internal/store/stats_test.go`

**Interfaces:**
- Consumes: `stats.Summary/YearCount/MonthCount/LanguageCount/AuthorCount` and `stats.Store` from Task 1. Test helpers `newTestStore(t)`, `seedUser(t, s, email)`, `seedBook(t, s, externalID, title, authors)` from `internal/store/library_test.go` (same package, already in the codebase — no need to redefine them).
- Produces: `*store.SQLite` satisfying `stats.Store`, plus a package-private `seedRead(t, s, userID, bookID, finishedAt *time.Time)` test helper for Task 2's own tests (finishedAt nil means an undated read).

- [ ] **Step 1: Write `internal/store/stats.go`**

```go
package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/kate/book-tracking/internal/stats"
)

func (s *SQLite) Summary(ctx context.Context, userID int64, now time.Time) (stats.Summary, error) {
	year := now.UTC().Year()

	const q = `SELECT
		COALESCE(SUM(CASE WHEN status = 'read' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'reading' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'backlog' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NOT NULL
			AND strftime('%Y', finished_at) = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NOT NULL
			AND strftime('%Y', finished_at) = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'read' AND finished_at IS NULL THEN 1 ELSE 0 END), 0)
		FROM user_books WHERE user_id = ?`

	row := s.db.QueryRowContext(ctx, q, strconv.Itoa(year), strconv.Itoa(year-1), userID)
	var sm stats.Summary
	if err := row.Scan(&sm.TotalRead, &sm.Reading, &sm.Backlog, &sm.ThisYear, &sm.LastYear, &sm.UndatedRead); err != nil {
		return stats.Summary{}, fmt.Errorf("stats summary: %w", err)
	}
	sm.CurrentYear = year
	return sm, nil
}

func (s *SQLite) ByYear(ctx context.Context, userID int64) ([]stats.YearCount, int, error) {
	const q = `SELECT strftime('%Y', finished_at) AS year, COUNT(*)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL
		GROUP BY year ORDER BY year ASC`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("stats by year: %w", err)
	}
	defer rows.Close()

	var years []stats.YearCount
	for rows.Next() {
		var y stats.YearCount
		if err := rows.Scan(&y.Year, &y.Count); err != nil {
			return nil, 0, fmt.Errorf("stats by year: %w", err)
		}
		years = append(years, y)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("stats by year: %w", err)
	}

	const undatedQ = `SELECT COUNT(*) FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NULL`
	var undated int
	if err := s.db.QueryRowContext(ctx, undatedQ, userID).Scan(&undated); err != nil {
		return nil, 0, fmt.Errorf("stats by year undated: %w", err)
	}
	return years, undated, nil
}

func (s *SQLite) ByMonth(ctx context.Context, userID int64, year string) ([]stats.MonthCount, error) {
	const q = `SELECT CAST(strftime('%m', finished_at) AS INTEGER), COUNT(*)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL
		  AND strftime('%Y', finished_at) = ?
		GROUP BY 1`

	rows, err := s.db.QueryContext(ctx, q, userID, year)
	if err != nil {
		return nil, fmt.Errorf("stats by month: %w", err)
	}
	defer rows.Close()

	counts := map[int]int{}
	for rows.Next() {
		var month, count int
		if err := rows.Scan(&month, &count); err != nil {
			return nil, fmt.Errorf("stats by month: %w", err)
		}
		counts[month] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats by month: %w", err)
	}

	months := make([]stats.MonthCount, 12)
	for i := 0; i < 12; i++ {
		months[i] = stats.MonthCount{Month: i + 1, Count: counts[i+1]}
	}
	return months, nil
}

func (s *SQLite) ByLanguage(ctx context.Context, userID int64, scope string) ([]stats.LanguageCount, error) {
	args := []any{userID}
	where := `ub.user_id = ? AND ub.status = 'read'`
	if scope != "all" {
		where += ` AND ub.finished_at IS NOT NULL AND strftime('%Y', ub.finished_at) = ?`
		args = append(args, scope)
	}

	q := `SELECT COALESCE(NULLIF(b.language, ''), 'Unknown') AS language, COUNT(*)
		FROM user_books ub
		JOIN books b ON b.id = ub.book_id
		WHERE ` + where + `
		GROUP BY language
		ORDER BY COUNT(*) DESC, language ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stats by language: %w", err)
	}
	defer rows.Close()

	var languages []stats.LanguageCount
	for rows.Next() {
		var l stats.LanguageCount
		if err := rows.Scan(&l.Language, &l.Count); err != nil {
			return nil, fmt.Errorf("stats by language: %w", err)
		}
		languages = append(languages, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats by language: %w", err)
	}
	return languages, nil
}

func (s *SQLite) TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]stats.AuthorCount, error) {
	args := []any{userID}
	where := `ub.user_id = ? AND ub.status = 'read'`
	if scope != "all" {
		where += ` AND ub.finished_at IS NOT NULL AND strftime('%Y', ub.finished_at) = ?`
		args = append(args, scope)
	}
	args = append(args, limit)

	q := `SELECT ba.name, COUNT(*)
		FROM user_books ub
		JOIN book_authors ba ON ba.book_id = ub.book_id
		WHERE ` + where + `
		GROUP BY ba.name
		ORDER BY COUNT(*) DESC, ba.name ASC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("stats top authors: %w", err)
	}
	defer rows.Close()

	var authors []stats.AuthorCount
	for rows.Next() {
		var a stats.AuthorCount
		if err := rows.Scan(&a.Author, &a.Count); err != nil {
			return nil, fmt.Errorf("stats top authors: %w", err)
		}
		authors = append(authors, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats top authors: %w", err)
	}
	return authors, nil
}

// Streak loads every distinct year-month with a finished read book in one
// query, then walks backward in Go — the same 240-month-bounded loop
// reports.js.currentStreak uses — rather than issuing up to 240 queries.
func (s *SQLite) Streak(ctx context.Context, userID int64, now time.Time) (int, error) {
	const q = `SELECT DISTINCT strftime('%Y-%m', finished_at)
		FROM user_books
		WHERE user_id = ? AND status = 'read' AND finished_at IS NOT NULL`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return 0, fmt.Errorf("stats streak: %w", err)
	}
	defer rows.Close()

	finished := map[string]bool{}
	for rows.Next() {
		var ym string
		if err := rows.Scan(&ym); err != nil {
			return 0, fmt.Errorf("stats streak: %w", err)
		}
		finished[ym] = true
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("stats streak: %w", err)
	}

	y, m := now.UTC().Year(), int(now.UTC().Month())
	streak := 0
	for i := 0; i < 240; i++ {
		m--
		if m == 0 {
			m = 12
			y--
		}
		if finished[fmt.Sprintf("%04d-%02d", y, m)] {
			streak++
		} else {
			break
		}
	}
	return streak, nil
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ stats.Store = (*SQLite)(nil)
```

- [ ] **Step 2: Write `internal/store/stats_test.go`**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/kate/book-tracking/internal/library"
)

// seedRead adds a book to userID's library as read with the given
// finished_at (nil for undated).
func seedRead(t *testing.T, s *SQLite, userID, bookID int64, finishedAt *time.Time) {
	t.Helper()
	if _, err := s.AddEntry(context.Background(), userID, bookID, library.StatusRead, finishedAt); err != nil {
		t.Fatalf("seedRead: %v", err)
	}
}

func datedAt(rfc3339 string) *time.Time {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestSummaryCountsByStatusAndYear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)

	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	b2 := seedBook(t, s, "vol-2", "Book 2", nil)
	b3 := seedBook(t, s, "vol-3", "Book 3", nil)
	b4 := seedBook(t, s, "vol-4", "Book 4", nil)
	b5 := seedBook(t, s, "vol-5", "Book 5", nil)

	seedRead(t, s, userID, b1, datedAt("2026-03-01T00:00:00Z")) // this year
	seedRead(t, s, userID, b2, datedAt("2025-03-01T00:00:00Z")) // last year
	seedRead(t, s, userID, b3, nil)                             // undated
	if _, err := s.AddEntry(ctx, userID, b4, library.StatusReading, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, b5, library.StatusBacklog, nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.Summary(ctx, userID, now)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 3 {
		t.Errorf("TotalRead = %d, want 3", got.TotalRead)
	}
	if got.Reading != 1 || got.Backlog != 1 {
		t.Errorf("Reading = %d, Backlog = %d, want 1, 1", got.Reading, got.Backlog)
	}
	if got.ThisYear != 1 || got.LastYear != 1 {
		t.Errorf("ThisYear = %d, LastYear = %d, want 1, 1", got.ThisYear, got.LastYear)
	}
	if got.CurrentYear != 2026 {
		t.Errorf("CurrentYear = %d, want 2026", got.CurrentYear)
	}
	if got.UndatedRead != 1 {
		t.Errorf("UndatedRead = %d, want 1", got.UndatedRead)
	}
}

func TestSummaryZeroBooksIsAllZero(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	got, err := s.Summary(context.Background(), userID, time.Now().UTC())
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 0 || got.Reading != 0 || got.Backlog != 0 {
		t.Fatalf("got = %+v, want all zero", got)
	}
}

func TestByYearExcludesUndatedAndReportsThemSeparately(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	b2 := seedBook(t, s, "vol-2", "Book 2", nil)
	b3 := seedBook(t, s, "vol-3", "Book 3", nil)

	seedRead(t, s, userID, b1, datedAt("2025-01-01T00:00:00Z"))
	seedRead(t, s, userID, b2, datedAt("2026-06-01T00:00:00Z"))
	seedRead(t, s, userID, b3, nil)

	years, undated, err := s.ByYear(ctx, userID)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if len(years) != 2 || years[0].Year != "2025" || years[1].Year != "2026" {
		t.Fatalf("years = %+v, want [2025:1 2026:1] ascending", years)
	}
	if undated != 1 {
		t.Fatalf("undated = %d, want 1", undated)
	}
}

func TestByYearOnlyIncludesYearsWithData(t *testing.T) {
	s := newTestStore(t)
	userID := seedUser(t, s, "a@b.com")

	years, undated, err := s.ByYear(context.Background(), userID)
	if err != nil {
		t.Fatalf("ByYear: %v", err)
	}
	if len(years) != 0 || undated != 0 {
		t.Fatalf("years = %+v, undated = %d, want empty", years, undated)
	}
}

func TestByMonthIsZeroFilledAcrossTwelveMonths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	seedRead(t, s, userID, b1, datedAt("2026-03-15T00:00:00Z"))

	months, err := s.ByMonth(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	if len(months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(months))
	}
	for i, m := range months {
		if m.Month != i+1 {
			t.Fatalf("months[%d].Month = %d, want %d", i, m.Month, i+1)
		}
	}
	if months[2].Count != 1 {
		t.Fatalf("March count = %d, want 1", months[2].Count)
	}
	if months[0].Count != 0 {
		t.Fatalf("January count = %d, want 0", months[0].Count)
	}
}

func TestByMonthScopesToTheRequestedYear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	b1 := seedBook(t, s, "vol-1", "Book 1", nil)
	seedRead(t, s, userID, b1, datedAt("2025-03-15T00:00:00Z"))

	months, err := s.ByMonth(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByMonth: %v", err)
	}
	for _, m := range months {
		if m.Count != 0 {
			t.Fatalf("month %d count = %d, want 0 (book was finished in 2025)", m.Month, m.Count)
		}
	}
}

func TestByLanguageMapsMissingLanguageToUnknown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	en1 := seedBook(t, s, "vol-en1", "English 1", nil)
	en2 := seedBook(t, s, "vol-en2", "English 2", nil)
	unset := seedBook(t, s, "vol-unset", "No Language", nil)
	setBookLanguage(t, s, en1, "English")
	setBookLanguage(t, s, en2, "English")

	seedRead(t, s, userID, en1, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, en2, datedAt("2026-02-01T00:00:00Z"))
	seedRead(t, s, userID, unset, nil) // undated but still counted within scope=all

	languages, err := s.ByLanguage(ctx, userID, "all")
	if err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if len(languages) != 2 || languages[0].Language != "English" || languages[0].Count != 2 {
		t.Fatalf("languages = %+v, want English:2 first", languages)
	}
	if languages[1].Language != "Unknown" || languages[1].Count != 1 {
		t.Fatalf("languages[1] = %+v, want Unknown:1", languages[1])
	}
}

func TestByLanguageScopedToYearExcludesUndated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	dated := seedBook(t, s, "vol-dated", "Dated", nil)
	undated := seedBook(t, s, "vol-undated", "Undated", nil)
	setBookLanguage(t, s, dated, "English")
	setBookLanguage(t, s, undated, "English")

	seedRead(t, s, userID, dated, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, undated, nil)

	languages, err := s.ByLanguage(ctx, userID, "2026")
	if err != nil {
		t.Fatalf("ByLanguage: %v", err)
	}
	if len(languages) != 1 || languages[0].Count != 1 {
		t.Fatalf("languages = %+v, want exactly one English:1 (undated excluded by year scope)", languages)
	}
}

func TestTopAuthorsCreditsEveryCoAuthorOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	dune := seedBook(t, s, "vol-dune", "Dune", []string{"Frank Herbert"})
	coauthored := seedBook(t, s, "vol-co", "Co-written", []string{"Ann Leckie", "Frank Herbert"})

	seedRead(t, s, userID, dune, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, userID, coauthored, datedAt("2026-02-01T00:00:00Z"))

	authors, err := s.TopAuthors(ctx, userID, "all", 5)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("authors = %+v, want 2 distinct authors", authors)
	}
	if authors[0].Author != "Frank Herbert" || authors[0].Count != 2 {
		t.Fatalf("authors[0] = %+v, want Frank Herbert:2 (co-authored book counts once per author)", authors[0])
	}
	if authors[1].Author != "Ann Leckie" || authors[1].Count != 1 {
		t.Fatalf("authors[1] = %+v, want Ann Leckie:1", authors[1])
	}
}

func TestTopAuthorsRespectsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	for i := 0; i < 3; i++ {
		b := seedBook(t, s, "vol-"+string(rune('a'+i)), "Book", []string{"Author " + string(rune('A'+i))})
		seedRead(t, s, userID, b, datedAt("2026-01-01T00:00:00Z"))
	}

	authors, err := s.TopAuthors(ctx, userID, "all", 2)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("len(authors) = %d, want 2", len(authors))
	}
}

func TestStreakCountsBackFromLastCompletedMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC) // "now" is August

	jul := seedBook(t, s, "vol-jul", "July", nil)
	jun := seedBook(t, s, "vol-jun", "June", nil)
	may := seedBook(t, s, "vol-may", "May", nil)
	mar := seedBook(t, s, "vol-mar", "March", nil)

	seedRead(t, s, userID, jul, datedAt("2026-07-10T00:00:00Z"))
	seedRead(t, s, userID, jun, datedAt("2026-06-10T00:00:00Z"))
	seedRead(t, s, userID, may, datedAt("2026-05-10T00:00:00Z"))
	seedRead(t, s, userID, mar, datedAt("2026-03-10T00:00:00Z")) // gap at April breaks the streak

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 3 {
		t.Fatalf("Streak = %d, want 3", got)
	}
}

func TestStreakIgnoresTheInProgressMonth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	b := seedBook(t, s, "vol-1", "Book", nil)
	seedRead(t, s, userID, b, datedAt("2026-08-01T00:00:00Z")) // this month, not counted

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 0 {
		t.Fatalf("Streak = %d, want 0 (in-progress month must not count)", got)
	}
}

func TestStreakCrossesAYearBoundary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	now := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
	dec := seedBook(t, s, "vol-dec", "December", nil)
	nov := seedBook(t, s, "vol-nov", "November", nil)
	seedRead(t, s, userID, dec, datedAt("2025-12-10T00:00:00Z"))
	seedRead(t, s, userID, nov, datedAt("2025-11-10T00:00:00Z"))

	got, err := s.Streak(ctx, userID, now)
	if err != nil {
		t.Fatalf("Streak: %v", err)
	}
	if got != 2 {
		t.Fatalf("Streak = %d, want 2", got)
	}
}

func TestCrossUserIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.September, 24, 0, 0, 0, 0, time.UTC)
	a := seedUser(t, s, "a@b.com")
	b := seedUser(t, s, "b@b.com")
	bookA := seedBook(t, s, "vol-a", "A", []string{"Author A"})
	bookB := seedBook(t, s, "vol-b", "B", []string{"Author B"})
	seedRead(t, s, a, bookA, datedAt("2026-01-01T00:00:00Z"))
	seedRead(t, s, b, bookB, datedAt("2026-01-01T00:00:00Z"))

	got, err := s.Summary(ctx, a, now)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.TotalRead != 1 {
		t.Fatalf("a's TotalRead = %d, want 1 (b's book must not count)", got.TotalRead)
	}

	authors, err := s.TopAuthors(ctx, a, "all", 5)
	if err != nil {
		t.Fatalf("TopAuthors: %v", err)
	}
	if len(authors) != 1 || authors[0].Author != "Author A" {
		t.Fatalf("authors = %+v, want only Author A", authors)
	}
}

// setBookLanguage is a direct SQL update: UpsertBooks does not expose a way
// to set language on a book seeded through seedBook's authors-only path,
// and stats tests need to distinguish "English" from "no language set".
func setBookLanguage(t *testing.T, s *SQLite, bookID int64, language string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE books SET language = ? WHERE id = ?`, language, bookID); err != nil {
		t.Fatalf("setBookLanguage: %v", err)
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/store/... -run 'Summary|ByYear|ByMonth|ByLanguage|TopAuthors|Streak|CrossUserIsolation' -v`
Expected: PASS for every test above.

- [ ] **Step 4: Run the full store package to confirm no regressions**

Run: `go test ./internal/store/...`
Expected: PASS (existing collections/library/auth tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/store/stats.go internal/store/stats_test.go
git commit -m "Add internal/store/stats.go: SQL aggregation for reading statistics"
```

---

## Task 3: HTTP handlers + routing

**Files:**
- Create: `internal/server/stats_handlers.go`
- Create: `internal/server/stats_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `stats.NewService`, `stats.Service.{Summary,ByYear,ByMonth,ByLanguage,TopAuthors,Streak}`, `stats.ErrInvalidScope`, `stats.ErrInvalidYear`, `stats.DefaultTopAuthorsLimit`, `stats.MaxTopAuthorsLimit` (Task 1). `positiveQuery`, `respondError`, `userID(c)` (existing, `internal/server/book_handlers.go` / `auth_handlers.go` / `middleware.go`). Test helpers `newTestServer(t)`, `registerAndLogin(t, srv)`, `doAuthedJSON`, `get`, `doJSON` (existing, same package).
- Produces: `(*Server) handleStatsSummary/handleStatsByYear/handleStatsByMonth/handleStatsByLanguage/handleStatsTopAuthors/handleStatsStreak(c *gin.Context)`, wired onto `authed` group at `/stats/summary`, `/stats/by-year`, `/stats/by-month`, `/stats/by-language`, `/stats/top-authors`, `/stats/streak`.

- [ ] **Step 1: Write `internal/server/stats_handlers.go`**

```go
package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/stats"
)

// respondStatsError maps service errors to status codes. There is no 404
// case in this milestone — every route aggregates over whatever the caller
// has, including zero books.
func respondStatsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, stats.ErrInvalidScope):
		respondError(c, http.StatusBadRequest, `scope must be "all" or a four-digit year`)
	case errors.Is(err, stats.ErrInvalidYear):
		respondError(c, http.StatusBadRequest, "year must be a four-digit year")
	default:
		log.Printf("stats: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

type summaryResponse struct {
	TotalRead   int `json:"total_read"`
	Reading     int `json:"reading"`
	Backlog     int `json:"backlog"`
	ThisYear    int `json:"this_year"`
	LastYear    int `json:"last_year"`
	CurrentYear int `json:"current_year"`
	UndatedRead int `json:"undated_read"`
}

func newSummaryResponse(sm stats.Summary) summaryResponse {
	return summaryResponse{
		TotalRead:   sm.TotalRead,
		Reading:     sm.Reading,
		Backlog:     sm.Backlog,
		ThisYear:    sm.ThisYear,
		LastYear:    sm.LastYear,
		CurrentYear: sm.CurrentYear,
		UndatedRead: sm.UndatedRead,
	}
}

func (s *Server) handleStatsSummary(c *gin.Context) {
	sm, err := s.stats.Summary(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newSummaryResponse(sm))
}

type yearCountResponse struct {
	Year  string `json:"year"`
	Count int    `json:"count"`
}

func (s *Server) handleStatsByYear(c *gin.Context) {
	years, undated, err := s.stats.ByYear(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]yearCountResponse, 0, len(years))
	for _, y := range years {
		items = append(items, yearCountResponse{Year: y.Year, Count: y.Count})
	}
	c.JSON(http.StatusOK, gin.H{"years": items, "undated": undated})
}

type monthCountResponse struct {
	Month int `json:"month"`
	Count int `json:"count"`
}

func (s *Server) handleStatsByMonth(c *gin.Context) {
	year, months, err := s.stats.ByMonth(c.Request.Context(), userID(c), c.Query("year"))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]monthCountResponse, 0, len(months))
	for _, m := range months {
		items = append(items, monthCountResponse{Month: m.Month, Count: m.Count})
	}
	c.JSON(http.StatusOK, gin.H{"year": year, "months": items})
}

type languageCountResponse struct {
	Language string `json:"language"`
	Count    int    `json:"count"`
}

func (s *Server) handleStatsByLanguage(c *gin.Context) {
	languages, err := s.stats.ByLanguage(c.Request.Context(), userID(c), c.Query("scope"))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]languageCountResponse, 0, len(languages))
	for _, l := range languages {
		items = append(items, languageCountResponse{Language: l.Language, Count: l.Count})
	}
	c.JSON(http.StatusOK, gin.H{"languages": items})
}

type authorCountResponse struct {
	Author string `json:"author"`
	Count  int    `json:"count"`
}

func (s *Server) handleStatsTopAuthors(c *gin.Context) {
	limit := positiveQuery(c, "limit", stats.DefaultTopAuthorsLimit)
	if limit > stats.MaxTopAuthorsLimit {
		limit = stats.MaxTopAuthorsLimit
	}

	authors, err := s.stats.TopAuthors(c.Request.Context(), userID(c), c.Query("scope"), limit)
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]authorCountResponse, 0, len(authors))
	for _, a := range authors {
		items = append(items, authorCountResponse{Author: a.Author, Count: a.Count})
	}
	c.JSON(http.StatusOK, gin.H{"authors": items})
}

func (s *Server) handleStatsStreak(c *gin.Context) {
	months, err := s.stats.Streak(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"months": months})
}
```

- [ ] **Step 2: Wire the service and routes into `internal/server/server.go`**

Add the import and the `stats` field/init:

```go
	"github.com/kate/book-tracking/internal/library"
	"github.com/kate/book-tracking/internal/stats"
	"github.com/kate/book-tracking/internal/store"
```

```go
type Server struct {
	db          *sql.DB
	router      *gin.Engine
	auth        *auth.Service
	books       *books.Service
	library     *library.Service
	collections *collections.Service
	stats       *stats.Service
	signer      *auth.Signer
}
```

```go
		library:     library.NewService(sqlStore),
		collections: collections.NewService(sqlStore),
		stats:       stats.NewService(sqlStore),
		signer:      signer,
```

Add the routes right after the collections routes, before the closing brace of `routes()`:

```go
	authed.GET("/stats/summary", s.handleStatsSummary)
	authed.GET("/stats/by-year", s.handleStatsByYear)
	authed.GET("/stats/by-month", s.handleStatsByMonth)
	authed.GET("/stats/by-language", s.handleStatsByLanguage)
	authed.GET("/stats/top-authors", s.handleStatsTopAuthors)
	authed.GET("/stats/streak", s.handleStatsStreak)
```

- [ ] **Step 3: Run the build to catch wiring mistakes**

Run: `go build ./...`
Expected: builds cleanly.

- [ ] **Step 4: Write `internal/server/stats_test.go`**

```go
package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestStatsRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	paths := []string{
		"/stats/summary", "/stats/by-year", "/stats/by-month",
		"/stats/by-language", "/stats/top-authors", "/stats/streak",
	}
	for _, p := range paths {
		rec := get(t, srv, p, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d, want 401", p, rec.Code)
		}
	}
}

func TestStatsSummaryShapeWithNoBooks(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/summary", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}

	var body struct {
		TotalRead   int `json:"total_read"`
		Reading     int `json:"reading"`
		Backlog     int `json:"backlog"`
		ThisYear    int `json:"this_year"`
		LastYear    int `json:"last_year"`
		CurrentYear int `json:"current_year"`
		UndatedRead int `json:"undated_read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalRead != 0 || body.CurrentYear == 0 {
		t.Fatalf("body = %+v, want zero counts and a real current_year", body)
	}
}

func TestStatsByYearEmptyArraysNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-year", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["years"]) != "[]" {
		t.Fatalf("years = %s, want []", raw["years"])
	}
}

func TestStatsByMonthDefaultsAndReturnsTwelveMonths(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-month", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Year   string `json:"year"`
		Months []struct {
			Month int `json:"month"`
			Count int `json:"count"`
		} `json:"months"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Year == "" {
		t.Fatal("year must default rather than be empty")
	}
	if len(body.Months) != 12 {
		t.Fatalf("len(months) = %d, want 12", len(body.Months))
	}
}

func TestStatsByMonthRejectsBadYear(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-month?year=20xx", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsByLanguageRejectsBadScope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/by-language?scope=nonsense", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsTopAuthorsRejectsBadScope(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/top-authors?scope=nonsense", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestStatsStreakShape(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/stats/streak", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Months int `json:"months"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Months != 0 {
		t.Fatalf("months = %d, want 0 for a fresh user", body.Months)
	}
}

// TestStatsScopingIsPerCaller confirms a second user's reads never leak into
// the first user's stats, exercised through the HTTP layer.
func TestStatsScopingIsPerCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLoginAs(t, srv, "a@b.com")
	registerAndLoginAs(t, srv, "b@b.com")

	rec := get(t, srv, "/stats/summary", "Bearer "+a.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		TotalRead int `json:"total_read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalRead != 0 {
		t.Fatalf("TotalRead = %d, want 0 (neither user has read anything)", body.TotalRead)
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/server/... -run Stats -v`
Expected: PASS for every test above.

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: everything passes, no regressions in auth/library/collections/books.

- [ ] **Step 7: Commit**

```bash
git add internal/server/stats_handlers.go internal/server/stats_test.go internal/server/server.go
git commit -m "Add /stats/* HTTP handlers and wire them onto the authed route group"
```

---

## Task 4: Frontend API layer + Pinia store

**Files:**
- Create: `web/src/api/stats.js`
- Create: `web/src/stores/stats.js`
- Create: `web/src/stores/stats.test.js`

**Interfaces:**
- Consumes: `request` from `web/src/api/client.js` (existing).
- Produces (used by Task 5's `ReportsView.vue` and `App.vue`): `useStatsStore()` exposing refs `summary` (`{totalRead, reading, backlog, thisYearCount, lastYearCount, currentYear, undatedRead}` or `null` before load), `byYear` (`{years: [{year,count}], undated}`), `byMonth` (`{year, months: [{month,count}]}`), `byLanguage` (`[{language,count}]`), `topAuthors` (`[{author,count}]`), `streak` (number), `scope` (string, default `'all'`), `loading`, `loaded`, `error`; and methods `load()`, `setScope(next)`, `setMonthYear(year)`, `reset()`.

- [ ] **Step 1: Write `web/src/api/stats.js`**

```js
import { request } from './client'

export function summary() {
  return request('/stats/summary')
}

export function byYear() {
  return request('/stats/by-year')
}

export function byMonth(year) {
  return request(`/stats/by-month${year ? `?year=${year}` : ''}`)
}

export function byLanguage(scope) {
  return request(`/stats/by-language${scope ? `?scope=${scope}` : ''}`)
}

export function topAuthors(scope, limit) {
  const params = new URLSearchParams()
  if (scope) params.set('scope', scope)
  if (limit) params.set('limit', String(limit))
  const qs = params.toString()
  return request(`/stats/top-authors${qs ? `?${qs}` : ''}`)
}

export function streak() {
  return request('/stats/streak')
}
```

- [ ] **Step 2: Write `web/src/stores/stats.js`**

```js
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as statsApi from '@/api/stats'

// Backed by backend Milestone 7 (/stats/*). Six figures, one store: summary,
// by-year, by-month, by-language, top-authors and streak. scope drives
// by-language/top-authors; changing it only re-fetches those two, not a
// full reload.
function normalizeSummary(s) {
  return {
    totalRead: s.total_read,
    reading: s.reading,
    backlog: s.backlog,
    thisYearCount: s.this_year,
    lastYearCount: s.last_year,
    currentYear: s.current_year,
    undatedRead: s.undated_read,
  }
}

export const useStatsStore = defineStore('stats', () => {
  const summary = ref(null)
  const byYear = ref({ years: [], undated: 0 })
  const byMonth = ref({ year: '', months: [] })
  const byLanguage = ref([])
  const topAuthors = ref([])
  const streak = ref(0)
  const scope = ref('all')
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref('')

  async function fetchScoped() {
    const scopeParam = scope.value === 'all' ? undefined : scope.value
    const [language, authors] = await Promise.all([
      statsApi.byLanguage(scopeParam),
      statsApi.topAuthors(scopeParam),
    ])
    byLanguage.value = language.languages
    topAuthors.value = authors.authors
  }

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const [s, y, m, streakResult] = await Promise.all([
        statsApi.summary(),
        statsApi.byYear(),
        statsApi.byMonth(),
        statsApi.streak(),
      ])
      summary.value = normalizeSummary(s)
      byYear.value = y
      byMonth.value = m
      streak.value = streakResult.months
      await fetchScoped()
      loaded.value = true
    } catch (e) {
      error.value = e.message || 'Could not load your reading stats.'
    } finally {
      loading.value = false
    }
  }

  async function setScope(next) {
    scope.value = next
    await fetchScoped()
  }

  async function setMonthYear(year) {
    byMonth.value = await statsApi.byMonth(year)
  }

  function reset() {
    summary.value = null
    byYear.value = { years: [], undated: 0 }
    byMonth.value = { year: '', months: [] }
    byLanguage.value = []
    topAuthors.value = []
    streak.value = 0
    scope.value = 'all'
    loaded.value = false
    error.value = ''
  }

  return {
    summary, byYear, byMonth, byLanguage, topAuthors, streak, scope,
    loading, loaded, error,
    load, setScope, setMonthYear, reset,
  }
})
```

- [ ] **Step 3: Write `web/src/stores/stats.test.js`**

```js
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useStatsStore } from './stats'
import * as statsApi from '@/api/stats'

vi.mock('@/api/stats')

function stubAll() {
  statsApi.summary.mockResolvedValue({
    total_read: 10, reading: 2, backlog: 3,
    this_year: 4, last_year: 5, current_year: 2026, undated_read: 1,
  })
  statsApi.byYear.mockResolvedValue({ years: [{ year: '2025', count: 4 }], undated: 1 })
  statsApi.byMonth.mockResolvedValue({
    year: '2026',
    months: Array.from({ length: 12 }, (_, i) => ({ month: i + 1, count: 0 })),
  })
  statsApi.byLanguage.mockResolvedValue({ languages: [{ language: 'English', count: 4 }] })
  statsApi.topAuthors.mockResolvedValue({ authors: [{ author: 'Ann Leckie', count: 2 }] })
  statsApi.streak.mockResolvedValue({ months: 3 })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
})

describe('load', () => {
  it('fetches all six endpoints and normalizes summary to camelCase', async () => {
    stubAll()
    const store = useStatsStore()

    await store.load()

    expect(store.summary).toEqual({
      totalRead: 10, reading: 2, backlog: 3,
      thisYearCount: 4, lastYearCount: 5, currentYear: 2026, undatedRead: 1,
    })
    expect(store.byYear).toEqual({ years: [{ year: '2025', count: 4 }], undated: 1 })
    expect(store.byMonth.year).toBe('2026')
    expect(store.byLanguage).toEqual([{ language: 'English', count: 4 }])
    expect(store.topAuthors).toEqual([{ author: 'Ann Leckie', count: 2 }])
    expect(store.streak).toBe(3)
    expect(store.loaded).toBe(true)
    expect(store.loading).toBe(false)
  })

  it('fetches the scoped endpoints with no scope param on the default "all" scope', async () => {
    stubAll()
    const store = useStatsStore()

    await store.load()

    expect(statsApi.byLanguage).toHaveBeenCalledWith(undefined)
    expect(statsApi.topAuthors).toHaveBeenCalledWith(undefined)
  })

  it('records an error message on failure', async () => {
    statsApi.summary.mockRejectedValue(new Error('boom'))
    statsApi.byYear.mockResolvedValue({ years: [], undated: 0 })
    statsApi.byMonth.mockResolvedValue({ year: '2026', months: [] })
    statsApi.streak.mockResolvedValue({ months: 0 })
    const store = useStatsStore()

    await store.load()

    expect(store.error).toBe('boom')
    expect(store.loading).toBe(false)
  })
})

describe('setScope', () => {
  it('only re-fetches by-language and top-authors', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()
    vi.clearAllMocks()
    statsApi.byLanguage.mockResolvedValue({ languages: [] })
    statsApi.topAuthors.mockResolvedValue({ authors: [] })

    await store.setScope('2025')

    expect(store.scope).toBe('2025')
    expect(statsApi.byLanguage).toHaveBeenCalledWith('2025')
    expect(statsApi.topAuthors).toHaveBeenCalledWith('2025')
    expect(statsApi.summary).not.toHaveBeenCalled()
    expect(statsApi.byYear).not.toHaveBeenCalled()
    expect(statsApi.byMonth).not.toHaveBeenCalled()
  })
})

describe('setMonthYear', () => {
  it('only re-fetches by-month', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()
    vi.clearAllMocks()
    statsApi.byMonth.mockResolvedValue({ year: '2024', months: [] })

    await store.setMonthYear('2024')

    expect(store.byMonth).toEqual({ year: '2024', months: [] })
    expect(statsApi.byMonth).toHaveBeenCalledWith('2024')
    expect(statsApi.summary).not.toHaveBeenCalled()
  })
})

describe('reset', () => {
  it('clears all state back to defaults', async () => {
    stubAll()
    const store = useStatsStore()
    await store.load()

    store.reset()

    expect(store.summary).toBeNull()
    expect(store.byYear).toEqual({ years: [], undated: 0 })
    expect(store.byLanguage).toEqual([])
    expect(store.topAuthors).toEqual([])
    expect(store.streak).toBe(0)
    expect(store.scope).toBe('all')
    expect(store.loaded).toBe(false)
  })
})
```

- [ ] **Step 4: Run the tests**

Run: `npm --prefix web run test -- src/stores/stats.test.js`
Expected: PASS for every test above.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/stats.js web/src/stores/stats.js web/src/stores/stats.test.js
git commit -m "Add stats API wrappers and Pinia store for backend Milestone 7"
```

---

## Task 5: Shrink `reports.js`, rewire `ReportsView.vue`, wire `App.vue`, update README

**Files:**
- Modify: `web/src/lib/reports.js`
- Modify: `web/src/lib/reports.test.js`
- Modify: `web/src/views/ReportsView.vue`
- Modify: `web/src/App.vue`
- Modify: `web/README.md`

**Interfaces:**
- Consumes: `useStatsStore()` from Task 4.
- Produces: `reports.js` exports only `barHeight(count, max, opts)`, `barColor(isHighlighted, opts)`, `pct(count, max)`, `initial(name)`, `yoyLabel(thisYear, lastYear)`.

- [ ] **Step 1: Replace `web/src/lib/reports.js` in full**

```js
// Pure presentation helpers for the Reports screen. All data shaping now
// happens server-side (backend Milestone 7, /stats/*, internal/stats) — this
// module only turns API-shaped numbers into pixels, percentages and labels.

// barHeight scales count against max onto [min, min + scale], the shared
// visual language of the year and month charts. When zeroStub is set, a
// zero count gets that fixed height instead of collapsing to the minimum —
// the month chart uses this so an empty month still reads as a bar, not a
// gap in the axis.
export function barHeight(count, max, { min = 10, scale = 110, zeroStub = null } = {}) {
  if (zeroStub !== null && count === 0) return zeroStub
  return Math.round((count / Math.max(1, max)) * scale) + min
}

export function barColor(isHighlighted, { on = 'var(--color-accent)', off = 'var(--color-neutral-400)' } = {}) {
  return isHighlighted ? on : off
}

// pct is a bar width as a percentage of the leading count in its group.
export function pct(count, max) {
  return Math.round((count / Math.max(1, max)) * 100)
}

// initial is the avatar letter for an author row.
export function initial(name) {
  return (name?.[0] || '?').toUpperCase()
}

export function yoyLabel(thisYearCount, lastYearCount) {
  const delta = thisYearCount - lastYearCount
  return lastYearCount === 0
    ? `vs ${lastYearCount} last year`
    : `${delta >= 0 ? '+' : ''}${delta} vs last year`
}
```

- [ ] **Step 2: Replace `web/src/lib/reports.test.js` in full**

```js
import { describe, it, expect } from 'vitest'
import { barHeight, barColor, pct, initial, yoyLabel } from './reports'

describe('barHeight', () => {
  it('scales the leader to min + scale', () => {
    expect(barHeight(10, 10, { min: 10, scale: 110 })).toBe(120)
  })

  it('scales a smaller count proportionally', () => {
    expect(barHeight(5, 10, { min: 10, scale: 110 })).toBe(65)
  })

  it('treats a zero max as one, so an empty group does not divide by zero', () => {
    expect(barHeight(0, 0, { min: 10, scale: 110 })).toBe(10)
  })

  it('uses the zero stub instead of the scaled minimum when given one', () => {
    expect(barHeight(0, 5, { min: 6, scale: 70, zeroStub: 2 })).toBe(2)
  })
})

describe('barColor', () => {
  it('returns the "on" color when highlighted', () => {
    expect(barColor(true, { on: 'accent', off: 'neutral' })).toBe('accent')
  })

  it('returns the "off" color otherwise', () => {
    expect(barColor(false, { on: 'accent', off: 'neutral' })).toBe('neutral')
  })

  it('defaults to the design system tokens', () => {
    expect(barColor(true)).toBe('var(--color-accent)')
    expect(barColor(false)).toBe('var(--color-neutral-400)')
  })
})

describe('pct', () => {
  it('scales against the leader', () => {
    expect(pct(2, 2)).toBe(100)
    expect(pct(1, 2)).toBe(50)
  })

  it('treats a zero max as one', () => {
    expect(pct(0, 0)).toBe(0)
  })
})

describe('initial', () => {
  it('uppercases the first letter', () => {
    expect(initial('ann leckie')).toBe('A')
  })

  it('falls back to a question mark for an empty name', () => {
    expect(initial('')).toBe('?')
  })
})

describe('yoyLabel', () => {
  it('shows a signed delta when last year had reads', () => {
    expect(yoyLabel(5, 4)).toBe('+1 vs last year')
    expect(yoyLabel(3, 4)).toBe('-1 vs last year')
  })

  it('phrases the first year without a delta', () => {
    expect(yoyLabel(1, 0)).toBe('vs 0 last year')
  })
})
```

- [ ] **Step 3: Run the reports.js tests**

Run: `npm --prefix web run test -- src/lib/reports.test.js`
Expected: PASS.

- [ ] **Step 4: Replace `web/src/views/ReportsView.vue` in full**

```vue
<script setup>
import { computed } from 'vue'
import { useStatsStore } from '@/stores/stats'
import { useSettingsStore } from '@/stores/settings'
import { barHeight, barColor, pct, initial, yoyLabel } from '@/lib/reports'

const stats = useStatsStore()
const settings = useSettingsStore()

const MONTH_LABELS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const years = computed(() => stats.byYear.years.map((y) => y.year))

const yearBars = computed(() => {
  const max = Math.max(1, ...stats.byYear.years.map((y) => y.count))
  const currentYear = String(stats.summary?.currentYear ?? '')
  return stats.byYear.years.map((y) => ({
    year: y.year,
    count: y.count,
    height: barHeight(y.count, max, { min: 10, scale: 110 }),
    color: barColor(y.year === currentYear),
  }))
})

const monthBars = computed(() => {
  const max = Math.max(1, ...stats.byMonth.months.map((m) => m.count))
  return stats.byMonth.months.map((m) => ({
    label: MONTH_LABELS[m.month - 1],
    count: m.count,
    height: barHeight(m.count, max, { min: 6, scale: 70, zeroStub: 2 }),
    color: barColor(m.count > 0, { on: 'var(--color-accent-500)', off: 'var(--color-neutral-200)' }),
  }))
})

const yoy = computed(() =>
  stats.summary ? yoyLabel(stats.summary.thisYearCount, stats.summary.lastYearCount) : '',
)

const scopeLabel = computed(() => (stats.scope === 'all' ? 'all time' : stats.scope))

const goalPct = computed(() =>
  stats.summary
    ? Math.min(100, Math.round((stats.summary.thisYearCount / Math.max(1, settings.readingGoal)) * 100))
    : 0,
)

const languageBars = computed(() => {
  const max = Math.max(1, ...stats.byLanguage.map((l) => l.count))
  return stats.byLanguage.map((l) => ({ ...l, pct: pct(l.count, max) }))
})

const authorRows = computed(() => stats.topAuthors.map((a) => ({ ...a, initial: initial(a.author) })))

function selectScope(value) {
  stats.setScope(value)
}
</script>

<template>
  <h1 class="page-title">Reports</h1>
  <p class="text-muted page-subtitle">Your reading, by the numbers.</p>

  <p v-if="stats.loading && !stats.loaded" class="text-muted">Loading…</p>

  <template v-else-if="stats.summary">
    <div class="stat-grid">
      <div class="card">
        <div class="card-kicker">Total read</div>
        <div class="stat-value">{{ stats.summary.totalRead }}</div>
        <div class="card-meta">all time</div>
      </div>
      <div class="card">
        <div class="card-kicker">This year</div>
        <div class="stat-value">{{ stats.summary.thisYearCount }}</div>
        <div class="card-meta">{{ yoy }}</div>
      </div>
      <div class="card">
        <div class="card-kicker">Current streak</div>
        <div class="stat-value">{{ stats.streak }}</div>
        <div class="card-meta">months in a row with a finish</div>
      </div>
      <div class="card">
        <div class="card-kicker">{{ stats.summary.currentYear }} goal</div>
        <div class="stat-value">{{ stats.summary.thisYearCount }} / {{ settings.readingGoal }}</div>
        <div class="meter goal-meter"><span :style="{ width: goalPct + '%' }" /></div>
      </div>
    </div>

    <h3 class="section-heading">Books finished by year</h3>
    <div v-if="yearBars.length" class="year-chart">
      <div v-for="y in yearBars" :key="y.year" class="year-col">
        <div class="year-count">{{ y.count }}</div>
        <div class="year-bar" :style="{ background: y.color, height: y.height + 'px' }" />
        <div class="text-muted year-label">{{ y.year }}</div>
      </div>
    </div>
    <p v-else class="text-muted chart-empty">Nothing finished yet — mark a book as read to start the chart.</p>

    <!--
      /stats/by-year's rule: a read book with no finish date counts toward
      the summary but cannot be placed in a year. Saying so keeps the
      by-year total reconcilable against "Total read" instead of looking
      like a bug.
    -->
    <p v-if="stats.byYear.undated" class="text-muted excluded-note">
      {{ stats.byYear.undated }} read
      {{ stats.byYear.undated === 1 ? 'book has' : 'books have' }} no finish date and
      {{ stats.byYear.undated === 1 ? 'is' : 'are' }} not shown in the year and month charts.
    </p>

    <h3 class="section-heading">{{ stats.byMonth.year }} by month</h3>
    <div class="month-chart">
      <div v-for="m in monthBars" :key="m.label" class="month-col">
        <div class="month-bar" :style="{ background: m.color, height: m.height + 'px' }" />
        <div class="text-muted month-label">{{ m.label }}</div>
      </div>
    </div>

    <div class="scope-row">
      <button type="button" class="pill-sm" :class="{ 'is-active': stats.scope === 'all' }" @click="selectScope('all')">
        All time
      </button>
      <button
        v-for="y in years"
        :key="y"
        type="button"
        class="pill-sm"
        :class="{ 'is-active': stats.scope === y }"
        @click="selectScope(y)"
      >
        {{ y }}
      </button>
    </div>

    <div class="breakdowns">
      <div>
        <h3 class="section-heading">By language ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="l in languageBars" :key="l.language">
            <div class="bar-head">
              <span>{{ l.language }}</span>
              <span class="text-muted">{{ l.count }}</span>
            </div>
            <div class="meter"><span :style="{ width: l.pct + '%' }" /></div>
          </div>
          <p v-if="!languageBars.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>

      <div>
        <h3 class="section-heading">Most-read authors ({{ scopeLabel }})</h3>
        <div class="bars">
          <div v-for="a in authorRows" :key="a.author" class="author-row">
            <div class="author-avatar">{{ a.initial }}</div>
            <div class="author-name">{{ a.author }}</div>
            <span class="tag tag-neutral">{{ a.count }}</span>
          </div>
          <p v-if="!authorRows.length" class="text-muted">No finished books in this period.</p>
        </div>
      </div>
    </div>
  </template>
</template>

<style scoped>
.goal-meter { height: 6px; border-radius: 3px; margin-top: 4px; }
.goal-meter > span { background: var(--color-accent); }

.year-chart {
  display: flex;
  align-items: flex-end;
  gap: 18px;
  height: 140px;
  margin-bottom: 10px;
  border-bottom: 1px solid var(--color-divider);
}
.year-col { display: flex; flex-direction: column; align-items: center; gap: 6px; width: 60px; }
.year-count { font-size: 12px; color: var(--color-accent-700); font-weight: 600; }
.year-bar { width: 36px; border-radius: 2px 2px 0 0; }
.year-label { font-size: 12px; }
.chart-empty { margin-bottom: 24px; }
.excluded-note { font-size: 12px; margin: 0 0 24px; }

.month-chart {
  display: flex;
  align-items: flex-end;
  gap: 6px;
  height: 90px;
  margin-bottom: 34px;
  border-bottom: 1px solid var(--color-divider);
}
.month-col { display: flex; flex-direction: column; align-items: center; gap: 4px; flex: 1; }
.month-bar { width: 100%; max-width: 26px; border-radius: 2px 2px 0 0; }
.month-label { font-size: 10px; }

.scope-row { display: flex; gap: 16px; margin-bottom: 8px; flex-wrap: wrap; }

.breakdowns { display: grid; grid-template-columns: 1fr 1fr; gap: 40px; margin-top: 24px; }
.bars { display: flex; flex-direction: column; gap: 10px; }
.bar-head { display: flex; justify-content: space-between; font-size: 13px; margin-bottom: 3px; }

.author-row { display: flex; align-items: center; gap: 10px; }
.author-avatar {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: var(--color-accent-100);
  color: var(--color-accent-800);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-family: var(--font-heading);
  font-weight: 600;
  flex: none;
}
.author-name { flex: 1; font-size: 13px; }

@media (max-width: 760px) {
  .breakdowns { grid-template-columns: 1fr; gap: 24px; }
}
</style>
```

- [ ] **Step 5: Wire the stats store into `web/src/App.vue`**

In the `<script setup>` block, add the import and instantiation:

```js
import { useStatsStore } from '@/stores/stats'
```

```js
const stats = useStatsStore()
```

Update the `watch` callback to load/reset stats alongside the existing stores:

```js
watch(
  () => auth.userId,
  (id) => {
    if (id === null) {
      library.reset()
      collections.reset()
      settings.reset()
      stats.reset()
      return
    }
    collections.hydrate()
    settings.hydrate(auth.email.split('@')[0])
    library.fetchAll()
    stats.load()
  },
  { immediate: true },
)
```

- [ ] **Step 6: Update `web/README.md`**

Replace the paragraph and bullet list from `## What is not backed by the API yet` (the "Ratings ... collections ..." paragraph through the end of the "Reports" bullet) with:

```markdown
Ratings (backend Milestone 5, `PUT/DELETE /library/:book_id/rating`),
collections (backend Milestone 6, `/collections`, `/collections/:id/books`),
and reports (backend Milestone 7, `/stats/*`) are now backed by the API —
`stores/ratings.js`, `stores/collections.js` and `stores/stats.js` call it
directly and hold no `localStorage` state of their own.

One smaller gap remains:

- **Manual book entry** (the prototype's "no catalog match for that ISBN" form)
  is not implemented. Books only enter the database through the metadata
  provider — `/books/search` and `/books/isbn/:isbn` upsert them and
  `POST /library` requires an id from one of those — so there is no endpoint a
  manual form could submit to. The Search tab explains this in place of the
  form.
```

(This is the same edit pattern Milestone 6 used to drop the Collections row — leave the rest of the file, including the Layout table and "Deviations from the prototype" section, untouched.)

- [ ] **Step 7: Run the full frontend test suite**

Run: `npm --prefix web run test`
Expected: PASS, including `src/lib/import.test.js` (unaffected), `src/lib/reports.test.js`, `src/stores/stats.test.js`.

- [ ] **Step 8: Run the frontend build**

Run: `npm --prefix web run build`
Expected: builds cleanly — this catches any leftover reference to a deleted `reports.js` export (e.g. `summary`, `byYear`, `currentStreak`) from `ReportsView.vue` or elsewhere.

- [ ] **Step 9: Manual smoke check (optional but recommended)**

Run: `npm --prefix web run dev` (with the Go server running via `go run ./cmd/server`), sign in, mark a few books read with different finish dates, and open the Reports tab. Confirm the year/month charts, language/author breakdowns, streak and goal all render using live `/stats/*` data, and that clicking a year pill re-renders the language/author sections without a full-page reload.

- [ ] **Step 10: Commit**

```bash
git add web/src/lib/reports.js web/src/lib/reports.test.js web/src/views/ReportsView.vue web/src/App.vue web/README.md
git commit -m "Wire Reports screen to /stats/* and shrink reports.js to pure presentation helpers"
```

---

## Self-Review Notes

- **Spec coverage:** All six endpoints (Task 3), the `internal/stats` package shape from the spec's Structure section (Task 1), the SQL layer and its indexes (Task 2), frontend wiring (`api/stats.js`, `stores/stats.js`, `reports.js` shrink, `ReportsView.vue`, README) (Tasks 4-5), and the Testing section's four bullets (service_test.go/store/stats_test.go/server/stats_test.go/frontend Vitest) are each covered by a task above. `positiveQuery`/limit clamping, the `Unknown` language mapping, undated-book handling, co-author crediting, cross-user isolation, and the 240-month streak bound are each an explicit test case.
- **Placeholder scan:** no TBD/TODO/"add error handling"-style steps; every step has complete, runnable code.
- **Type consistency:** `stats.Service.ByMonth` returns `(string, []MonthCount, error)` consistently between Task 1's interface note, Task 1's implementation, and Task 3's handler call site. `stats.Store` method signatures in `store.go` (Task 1) match the implementations in `internal/store/stats.go` (Task 2) exactly, enforced by the `var _ stats.Store = (*SQLite)(nil)` compile-time guard. Frontend `useStatsStore()`'s field names (`byYear`, `byMonth`, `byLanguage`, `topAuthors`, `streak`, `scope`) are used identically across Task 4's store/test and Task 5's `ReportsView.vue`/`App.vue`.
