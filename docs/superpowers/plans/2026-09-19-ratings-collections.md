# Ratings & Collections (Milestones 5–6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 1–5 star ratings on read books and named book collections to the Go API, then swap the two Vue Pinia stores that currently fake both in `localStorage` for real API calls.

**Architecture:** Ratings fold into the existing `internal/library` package (a rating is keyed by the same `(user_id, book_id)` pair as a library entry, and requires status `read`). Collections get a new `internal/collections` package mirroring `internal/library`'s Service/Store/errors shape. Both hang new routes off the existing `authed` gin group. On the frontend, `stores/ratings.js` stops holding its own state and reads/writes through `stores/library.js`; `stores/collections.js` keeps its public method names but its body becomes async API calls.

**Tech Stack:** Go (gin, database/sql, mattn/go-sqlite3), Vue 3 + Pinia, Vitest.

## Global Constraints

- No schema migration: `ratings`, `collections`, `collection_books` already exist from migration `000001`.
- `score` validation uses `binding:"required,gte=1,lte=5"` — zero and absent both collapse to `400`.
- A collection/entry that doesn't exist, or belongs to another user, is always `404`, never `403` — "not found" and "not yours" must be indistinguishable.
- `POST /collections/:id/books` is idempotent (`INSERT OR IGNORE`); `DELETE /collections/:id/books/:book_id` is not (removing a non-member is `404`).
- No `GET /ratings` or `GET /collections/:id` — out of scope.
- Frontend stores keep their existing public method names; no view's call *shape* changes, only `async`/`await` at call sites.

---

## Task 1: `library.Entry` gains `Rating`, plus rating errors and service methods

**Files:**
- Modify: `internal/library/library.go`
- Modify: `internal/library/errors.go`
- Modify: `internal/library/service.go`
- Modify: `internal/library/service_test.go`
- Modify: `internal/library/library_test.go` (if it constructs `Entry` literals that need updating — check first; likely no change needed since `Rating` is an added field)

**Interfaces:**
- Consumes: existing `library.Store` interface (`internal/library/store.go`), `library.Entry`, `library.Status`.
- Produces: `Entry.Rating *int`; `ErrInvalidRating`, `ErrRatingRequiresRead`; `Service.SetRating(ctx, userID, bookID int64, score int) (Entry, error)`; `Service.ClearRating(ctx, userID, bookID int64) error`. Task 2 adds the `Store.SetRating`/`Store.ClearRating` methods these call.

- [ ] **Step 1: Add `Rating` to `Entry`**

In `internal/library/library.go`, add the field to the `Entry` struct:

```go
// Entry is one book in one user's library. FinishedAt is a pointer because
// "not finished" is a real state the API reports as an explicit null.
// Rating is a pointer for the same reason: no rating is a real state, not a
// zero score.
type Entry struct {
	Book       books.Book
	Status     Status
	FinishedAt *time.Time
	Rating     *int
	AddedAt    time.Time
}
```

- [ ] **Step 2: Add the two new errors**

In `internal/library/errors.go`, add inside the existing `var (...)` block:

```go
	// ErrInvalidRating means the score was not between 1 and 5.
	ErrInvalidRating = errors.New("rating must be between 1 and 5")
	// ErrRatingRequiresRead means the entry's status is not read.
	ErrRatingRequiresRead = errors.New("book must be marked read to be rated")
```

- [ ] **Step 3: Extend `Store` interface**

In `internal/library/store.go`, add to the `Store` interface (this is implemented in Task 2):

```go
	// SetRating upserts a rating. It does not validate the score or the
	// entry's status — that is Service's job — but it does not create an
	// entry that does not exist either; a foreign key violation on user_id
	// or book_id would only happen if the caller already passed a bad
	// (userID, bookID) pair, which Service rules out by loading the entry
	// first.
	SetRating(ctx context.Context, userID, bookID int64, score int) error
	// ClearRating deletes a rating if one exists. It is a no-op, not an
	// error, when there is none.
	ClearRating(ctx context.Context, userID, bookID int64) error
```

- [ ] **Step 4: Write the failing service tests**

Append to `internal/library/service_test.go`:

```go
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
```

Also extend `fakeStore` in the same file with the two new methods (place them near `DeleteEntry`):

```go
func (f *fakeStore) SetRating(_ context.Context, _, bookID int64, score int) error {
	if f.err != nil {
		return f.err
	}
	e, ok := f.entries[bookID]
	if !ok {
		return ErrNotInLibrary
	}
	s := score
	e.Rating = &s
	f.entries[bookID] = e
	return nil
}

func (f *fakeStore) ClearRating(_ context.Context, _, bookID int64) error {
	if f.err != nil {
		return f.err
	}
	e, ok := f.entries[bookID]
	if !ok {
		return ErrNotInLibrary
	}
	e.Rating = nil
	f.entries[bookID] = e
	return nil
}
```

- [ ] **Step 5: Run tests to verify they fail to compile**

Run: `go test ./internal/library/... -run TestSetRating -v`
Expected: FAIL — `svc.SetRating undefined` (method does not exist yet).

- [ ] **Step 6: Implement `Service.SetRating` and `Service.ClearRating`**

Append to `internal/library/service.go`:

```go
// SetRating validates score and status, then upserts the rating. The score
// range is checked before the entry is loaded so a malformed request never
// depends on whether the book is in the caller's library.
func (s *Service) SetRating(ctx context.Context, userID, bookID int64, score int) (Entry, error) {
	if score < 1 || score > 5 {
		return Entry{}, ErrInvalidRating
	}

	current, err := s.store.Entry(ctx, userID, bookID)
	if err != nil {
		return Entry{}, err
	}
	if current.Status != StatusRead {
		return Entry{}, ErrRatingRequiresRead
	}

	if err := s.store.SetRating(ctx, userID, bookID, score); err != nil {
		return Entry{}, err
	}
	return s.store.Entry(ctx, userID, bookID)
}

// ClearRating removes a rating if one exists. Only a missing library entry
// is an error; a missing rating on an existing entry is success, matching
// how DELETE /library/:book_id/rating is documented as idempotent.
func (s *Service) ClearRating(ctx context.Context, userID, bookID int64) error {
	if _, err := s.store.Entry(ctx, userID, bookID); err != nil {
		return err
	}
	return s.store.ClearRating(ctx, userID, bookID)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/library/... -v`
Expected: PASS (all tests in the package, including the pre-existing ones).

- [ ] **Step 8: Commit**

```bash
git add internal/library/library.go internal/library/errors.go internal/library/store.go internal/library/service.go internal/library/service_test.go
git commit -m "library: add Entry.Rating and Service.SetRating/ClearRating"
```

---

## Task 2: SQLite store — rating join, `SetRating`, `ClearRating`

**Files:**
- Modify: `internal/store/library.go`
- Modify: `internal/store/library_test.go`

**Interfaces:**
- Consumes: `library.Entry.Rating *int` and `library.Store` interface additions from Task 1.
- Produces: `(*SQLite).SetRating(ctx, userID, bookID int64, score int) error`, `(*SQLite).ClearRating(ctx, userID, bookID int64) error`. `entryColumns`/`scanEntry` now populate `Entry.Rating`. Nothing later depends on the SQL shape directly — only on the `library.Store` interface, already satisfied.

- [ ] **Step 1: Write the failing store tests**

Append to `internal/store/library_test.go`:

```go
func TestSetRatingRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := s.SetRating(ctx, userID, bookID, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	got, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating == nil || *got.Rating != 4 {
		t.Fatalf("Rating = %v, want 4", got.Rating)
	}
}

func TestSetRatingOverwrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.SetRating(ctx, userID, bookID, 2); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if err := s.SetRating(ctx, userID, bookID, 5); err != nil {
		t.Fatalf("SetRating (overwrite): %v", err)
	}

	got, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating == nil || *got.Rating != 5 {
		t.Fatalf("Rating = %v, want 5", got.Rating)
	}
}

func TestClearRating(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.SetRating(ctx, userID, bookID, 3); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if err := s.ClearRating(ctx, userID, bookID); err != nil {
		t.Fatalf("ClearRating: %v", err)
	}

	got, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating != nil {
		t.Fatalf("Rating = %v, want nil", got.Rating)
	}
}

// Clearing a rating that does not exist must not error: DELETE is
// idempotent at the store layer too.
func TestClearRatingWithoutExistingRating(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}

	if err := s.ClearRating(ctx, userID, bookID); err != nil {
		t.Fatalf("ClearRating: %v", err)
	}
}

// A rating is keyed independently of user_books; a status change away from
// read and back must not disturb it. (The service layer is what stops a
// *new* rating being set on a non-read book — the store itself does not
// enforce status.)
func TestRatingSurvivesStatusChangeAwayAndBackToRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	bookID := seedBook(t, s, "vol-dune", "Dune", nil)
	if _, err := s.AddEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if err := s.SetRating(ctx, userID, bookID, 4); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	if _, err := s.UpdateEntry(ctx, userID, bookID, library.StatusBacklog, nil); err != nil {
		t.Fatalf("UpdateEntry to backlog: %v", err)
	}
	if _, err := s.UpdateEntry(ctx, userID, bookID, library.StatusRead, nil); err != nil {
		t.Fatalf("UpdateEntry back to read: %v", err)
	}

	got, err := s.Entry(ctx, userID, bookID)
	if err != nil {
		t.Fatalf("Entry: %v", err)
	}
	if got.Rating == nil || *got.Rating != 4 {
		t.Fatalf("Rating = %v, want 4 to have survived the round trip", got.Rating)
	}
}

func TestListEntriesIncludesRatings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	userID := seedUser(t, s, "a@b.com")
	rated := seedBook(t, s, "vol-a", "Rated", nil)
	unrated := seedBook(t, s, "vol-b", "Unrated", nil)
	if _, err := s.AddEntry(ctx, userID, rated, library.StatusRead, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEntry(ctx, userID, unrated, library.StatusRead, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRating(ctx, userID, rated, 5); err != nil {
		t.Fatalf("SetRating: %v", err)
	}

	entries, err := s.ListEntries(ctx, library.ListParams{UserID: userID, Sort: library.SortTitle, Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Rating == nil || *entries[0].Rating != 5 {
		t.Fatalf("rated entry Rating = %v, want 5", entries[0].Rating)
	}
	if entries[1].Rating != nil {
		t.Fatalf("unrated entry Rating = %v, want nil", entries[1].Rating)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -run 'TestSetRating|TestClearRating|TestRatingSurvives|TestListEntriesIncludesRatings' -v`
Expected: FAIL to compile — `s.SetRating undefined`.

- [ ] **Step 3: Add the rating join and the two new methods**

In `internal/store/library.go`, change `entryColumns` to include the rating:

```go
// entryColumns is the shared projection behind Entry and ListEntries. The
// COALESCEs turn the nullable optional columns into the empty strings
// books.Book uses, so "unknown" has one representation in Go. r.score stays
// a nullable int rather than COALESCEd to 0: no rating is a real state, not
// a zero score.
const entryColumns = `b.id, b.external_id,
       COALESCE(b.isbn, ''), b.title,
       COALESCE(b.language, ''), COALESCE(b.cover_url, ''),
       COALESCE(b.metadata_source, ''),
       ub.status, ub.finished_at, ub.created_at, r.score`
```

Update the `Entry` query to join `ratings`:

```go
func (s *SQLite) Entry(ctx context.Context, userID, bookID int64) (library.Entry, error) {
	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      LEFT JOIN ratings r ON r.user_id = ub.user_id AND r.book_id = ub.book_id
	      WHERE ub.user_id = ? AND ub.book_id = ?`
```

Update `ListEntries`'s query the same way:

```go
	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      LEFT JOIN ratings r ON r.user_id = ub.user_id AND r.book_id = ub.book_id
	      WHERE ` + where + `
	      ORDER BY ` + order + `
	      LIMIT ? OFFSET ?`
```

Update `scanEntry` to read the new column:

```go
func scanEntry(sc rowScanner) (library.Entry, error) {
	var (
		e          library.Entry
		status     string
		finishedAt sql.NullTime
		rating     sql.NullInt64
	)
	err := sc.Scan(
		&e.Book.ID, &e.Book.ExternalID, &e.Book.ISBN, &e.Book.Title,
		&e.Book.Language, &e.Book.CoverURL, &e.Book.Source,
		&status, &finishedAt, &e.AddedAt, &rating,
	)
	if err != nil {
		return library.Entry{}, err
	}
	e.Status = library.Status(status)
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		e.FinishedAt = &t
	}
	if rating.Valid {
		r := int(rating.Int64)
		e.Rating = &r
	}
	e.AddedAt = e.AddedAt.UTC()
	return e, nil
}
```

Add the two new methods (near `DeleteEntry`):

```go
// SetRating upserts a rating. Validation (score range, status == read) is
// Service's job; this only persists.
func (s *SQLite) SetRating(ctx context.Context, userID, bookID int64, score int) error {
	const q = `INSERT INTO ratings (user_id, book_id, score, created_at)
	           VALUES (?, ?, ?, ?)
	           ON CONFLICT(user_id, book_id) DO UPDATE SET score = excluded.score`

	_, err := s.db.ExecContext(ctx, q, userID, bookID, score, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set rating: %w", err)
	}
	return nil
}

// ClearRating deletes a rating. Deleting a row that does not exist is not an
// error: DELETE /library/:book_id/rating is documented as idempotent.
func (s *SQLite) ClearRating(ctx context.Context, userID, bookID int64) error {
	const q = `DELETE FROM ratings WHERE user_id = ? AND book_id = ?`

	if _, err := s.db.ExecContext(ctx, q, userID, bookID); err != nil {
		return fmt.Errorf("clear rating: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/library.go internal/store/library_test.go
git commit -m "store: join ratings into library entries, add SetRating/ClearRating"
```

---

## Task 3: Rating HTTP handlers and routes

**Files:**
- Modify: `internal/server/library_handlers.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/library_test.go`

**Interfaces:**
- Consumes: `Service.SetRating`/`Service.ClearRating` from Task 1, `ErrInvalidRating`/`ErrRatingRequiresRead` from Task 1, existing `respondError`, `libraryBookID`, `newLibraryEntryResponse`, `respondLibraryError`, `userID`.
- Produces: `PUT /library/:book_id/rating`, `DELETE /library/:book_id/rating` routes; `libraryEntryResponse` gains `"rating": number|null`.

- [ ] **Step 1: Write the failing handler tests**

Append to `internal/server/library_test.go`:

```go
func TestRatingRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPut, "/library/1/rating"},
		{http.MethodDelete, "/library/1/rating"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestSetRatingReturns200WithRating(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPut, fmt.Sprintf("/library/%d/rating", bookID),
		pair.AccessToken, map[string]any{"score": 4})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body libraryEntryBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Rating == nil || *body.Rating != 4 {
		t.Fatalf("rating = %v, want 4", body.Rating)
	}
}

func TestSetRatingRejectsBadScore(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	for _, body := range []map[string]any{
		{"score": 0}, {"score": 6}, {},
	} {
		rec := doAuthedJSON(t, srv, http.MethodPut, fmt.Sprintf("/library/%d/rating", bookID),
			pair.AccessToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %v: status = %d, want 400: %s", body, rec.Code, rec.Body)
		}
	}
}

func TestSetRatingOnNonReadEntryIs400(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "backlog"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodPut, fmt.Sprintf("/library/%d/rating", bookID),
		pair.AccessToken, map[string]any{"score": 3})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestSetRatingMissingEntryIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPut, "/library/9999/rating", pair.AccessToken,
		map[string]any{"score": 3})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestClearRatingIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", pair.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	// No rating was ever set; clearing must still succeed.
	rec := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/library/%d/rating", bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
}

func TestClearRatingMissingEntryIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodDelete, "/library/9999/rating", pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestRatingIsScopedToTheCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLogin(t, srv)
	b := registerAndLoginAs(t, srv, "b@b.com")
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, "/library", a.AccessToken,
		map[string]any{"book_id": bookID, "status": "read"}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	path := fmt.Sprintf("/library/%d/rating", bookID)
	put := doAuthedJSON(t, srv, http.MethodPut, path, b.AccessToken, map[string]any{"score": 5})
	if put.Code != http.StatusNotFound {
		t.Fatalf("cross-user PUT = %d, want 404: %s", put.Code, put.Body)
	}
	del := doAuthedJSON(t, srv, http.MethodDelete, path, b.AccessToken, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user DELETE = %d, want 404: %s", del.Code, del.Body)
	}
}
```

Add `Rating *int` to the shared `libraryEntryBody` in `internal/server/helpers_test.go`:

```go
type libraryEntryBody struct {
	Book struct {
		ID      int64    `json:"id"`
		Title   string   `json:"title"`
		Authors []string `json:"authors"`
	} `json:"book"`
	Status     string  `json:"status"`
	FinishedAt *string `json:"finished_at"`
	Rating     *int    `json:"rating"`
	AddedAt    string  `json:"added_at"`
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run TestSetRating -v`
Expected: FAIL — 404 on unregistered route (no handler wired yet), or compile failure once the response struct references `Rating`.

- [ ] **Step 3: Add the response field, request type, handlers, and error mapping**

In `internal/server/library_handlers.go`, update `libraryEntryResponse` and `newLibraryEntryResponse`:

```go
// libraryEntryResponse reuses bookResponse unchanged. FinishedAt and Rating
// are pointers without omitempty so they are always present and explicitly
// null when unset: a client should not have to guess whether the field is
// absent or the value is genuinely empty.
type libraryEntryResponse struct {
	Book       bookResponse `json:"book"`
	Status     string       `json:"status"`
	FinishedAt *string      `json:"finished_at"`
	Rating     *int         `json:"rating"`
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
		Rating:     e.Rating,
		AddedAt:    e.AddedAt.UTC().Format(time.RFC3339),
	}
}
```

Add the two new error cases to `respondLibraryError`, right after the `ErrFinishedAtNotRead` case:

```go
	case errors.Is(err, library.ErrInvalidRating):
		respondError(c, http.StatusBadRequest, "score must be between 1 and 5")
	case errors.Is(err, library.ErrRatingRequiresRead):
		respondError(c, http.StatusBadRequest, "book must be marked read to be rated")
```

Add the request type and the two handlers at the end of the file:

```go
// ratingRequest uses the same "required" + range pattern addLibraryRequest
// uses for BookID: zero and absent both fail "required", so they produce
// the same 400 rather than a zero score sneaking through as "unset".
type ratingRequest struct {
	Score int `json:"score" binding:"required,gte=1,lte=5"`
}

func (s *Server) handleRatingSet(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	var req ratingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	entry, err := s.library.SetRating(c.Request.Context(), userID(c), bookID, req.Score)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	c.JSON(http.StatusOK, newLibraryEntryResponse(entry))
}

func (s *Server) handleRatingDelete(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	if err := s.library.ClearRating(c.Request.Context(), userID(c), bookID); err != nil {
		respondLibraryError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
```

- [ ] **Step 4: Wire the routes**

In `internal/server/server.go`, add inside `routes()`, after the existing library routes:

```go
	authed.PUT("/library/:book_id/rating", s.handleRatingSet)
	authed.DELETE("/library/:book_id/rating", s.handleRatingDelete)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server/library_handlers.go internal/server/server.go internal/server/library_test.go internal/server/helpers_test.go
git commit -m "server: add rating endpoints"
```

---

## Task 4: `internal/collections` package — types, errors, Store interface, Service

**Files:**
- Create: `internal/collections/collections.go`
- Create: `internal/collections/errors.go`
- Create: `internal/collections/store.go`
- Create: `internal/collections/service.go`
- Create: `internal/collections/service_test.go`

**Interfaces:**
- Consumes: nothing outside this package.
- Produces: `Collection{ID, UserID, Name, BookIDs, CreatedAt}`; `ErrNotFound`, `ErrEmptyName`, `ErrUnknownBook`, `ErrBookNotInCollection`; `Store` interface (`Create`, `List`, `Rename`, `Delete`, `AddBook`, `RemoveBook`); `Service` with the same method set minus the trimming/validation it owns. Task 5 implements `Store` against SQLite; Task 6's handlers call `Service`.

- [ ] **Step 1: Define the type**

Write `internal/collections/collections.go`:

```go
// Package collections keeps each user's named groupings of library books.
package collections

import "time"

// Collection is one user's named group of books. BookIDs is never nil: an
// empty collection reports []int64{}, matching how library.Entry's slices
// are never nil either.
type Collection struct {
	ID        int64
	UserID    int64
	Name      string
	BookIDs   []int64
	CreatedAt time.Time
}
```

- [ ] **Step 2: Define the errors**

Write `internal/collections/errors.go`:

```go
package collections

import "errors"

var (
	// ErrNotFound means the collection id has no row, or belongs to another
	// user — the two must be indistinguishable to the caller.
	ErrNotFound = errors.New("collection not found")
	// ErrEmptyName means the name was empty after trimming whitespace.
	ErrEmptyName = errors.New("collection name must not be empty")
	// ErrUnknownBook means the book_id has no row in books.
	ErrUnknownBook = errors.New("unknown book")
	// ErrBookNotInCollection means the caller tried to remove a book that is
	// not a member. Unlike adding, removing is not idempotent.
	ErrBookNotInCollection = errors.New("book not in collection")
)
```

- [ ] **Step 3: Define the Store interface**

Write `internal/collections/store.go`:

```go
package collections

import "context"

// Store persists collections. *store.SQLite implements it; the interface
// keeps this package free of database/sql and lets name validation be
// tested against a fake.
//
// Every method takes userID and scopes by it. A collection belonging to
// another user must come back as ErrNotFound, never as someone else's row.
type Store interface {
	// Create inserts a new collection. name arrives already trimmed and
	// non-empty.
	Create(ctx context.Context, userID int64, name string) (Collection, error)
	// List returns every collection the caller owns.
	List(ctx context.Context, userID int64) ([]Collection, error)
	// Rename overwrites name, or returns ErrNotFound. name arrives already
	// trimmed and non-empty.
	Rename(ctx context.Context, userID, id int64, name string) (Collection, error)
	// Delete removes a collection, or returns ErrNotFound.
	Delete(ctx context.Context, userID, id int64) error
	// AddBook is idempotent: adding a book already in the collection is a
	// no-op that still returns the current collection. It returns
	// ErrNotFound for a bad collection id and ErrUnknownBook for a bad
	// book id.
	AddBook(ctx context.Context, userID, id, bookID int64) (Collection, error)
	// RemoveBook is not idempotent: removing a book that is not a member
	// returns ErrBookNotInCollection. It returns ErrNotFound for a bad
	// collection id.
	RemoveBook(ctx context.Context, userID, id, bookID int64) error
}
```

- [ ] **Step 4: Write the failing service tests**

Write `internal/collections/service_test.go`:

```go
package collections

import (
	"context"
	"errors"
	"testing"
)

// fakeStore holds one user's collections in a map keyed by collection ID.
type fakeStore struct {
	items  map[int64]Collection
	nextID int64
	err    error
}

func newFakeStore() *fakeStore { return &fakeStore{items: map[int64]Collection{}, nextID: 1} }

func (f *fakeStore) Create(_ context.Context, userID int64, name string) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c := Collection{ID: f.nextID, UserID: userID, Name: name, BookIDs: []int64{}}
	f.items[c.ID] = c
	f.nextID++
	return c, nil
}

func (f *fakeStore) List(_ context.Context, userID int64) ([]Collection, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []Collection
	for _, c := range f.items {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeStore) Rename(_ context.Context, userID, id int64, name string) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return Collection{}, ErrNotFound
	}
	c.Name = name
	f.items[id] = c
	return c, nil
}

func (f *fakeStore) Delete(_ context.Context, userID, id int64) error {
	if f.err != nil {
		return f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	delete(f.items, id)
	return nil
}

func (f *fakeStore) AddBook(_ context.Context, userID, id, bookID int64) (Collection, error) {
	if f.err != nil {
		return Collection{}, f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return Collection{}, ErrNotFound
	}
	for _, b := range c.BookIDs {
		if b == bookID {
			return c, nil
		}
	}
	c.BookIDs = append(c.BookIDs, bookID)
	f.items[id] = c
	return c, nil
}

func (f *fakeStore) RemoveBook(_ context.Context, userID, id, bookID int64) error {
	if f.err != nil {
		return f.err
	}
	c, ok := f.items[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	idx := -1
	for i, b := range c.BookIDs {
		if b == bookID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrBookNotInCollection
	}
	c.BookIDs = append(c.BookIDs[:idx], c.BookIDs[idx+1:]...)
	f.items[id] = c
	return nil
}

func newTestService(store Store) *Service { return NewService(store) }

func TestCreateTrimsName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.Create(context.Background(), 1, "  Summer reading  ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Name != "Summer reading" {
		t.Fatalf("Name = %q, want trimmed", got.Name)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	for _, name := range []string{"", "   "} {
		_, err := svc.Create(context.Background(), 1, name)
		if !errors.Is(err, ErrEmptyName) {
			t.Fatalf("name %q: err = %v, want ErrEmptyName", name, err)
		}
	}
}

func TestRenameTrimsName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Original")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.Rename(context.Background(), 1, c.ID, "  Renamed  ")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Renamed" {
		t.Fatalf("Name = %q, want trimmed", got.Name)
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Original")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Rename(context.Background(), 1, c.ID, "   ")
	if !errors.Is(err, ErrEmptyName) {
		t.Fatalf("err = %v, want ErrEmptyName", err)
	}
}

func TestRenameMissingIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.Rename(context.Background(), 1, 999, "New name")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListNeverReturnsNil(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	got, err := svc.List(context.Background(), 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Fatal("List must return an empty slice, not nil")
	}
}

func TestDeleteMissingIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	if err := svc.Delete(context.Background(), 1, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddBookMissingCollectionIsNotFound(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)

	_, err := svc.AddBook(context.Background(), 1, 999, 42)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddBookIsIdempotent(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.AddBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("AddBook: %v", err)
	}
	got, err := svc.AddBook(context.Background(), 1, c.ID, 42)
	if err != nil {
		t.Fatalf("AddBook (repeat): %v", err)
	}
	if len(got.BookIDs) != 1 {
		t.Fatalf("BookIDs = %v, want exactly one entry", got.BookIDs)
	}
}

func TestRemoveBookNotInCollectionIsError(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = svc.RemoveBook(context.Background(), 1, c.ID, 42)
	if !errors.Is(err, ErrBookNotInCollection) {
		t.Fatalf("err = %v, want ErrBookNotInCollection", err)
	}
}

func TestRemoveBookRemovesIt(t *testing.T) {
	f := newFakeStore()
	svc := newTestService(f)
	c, err := svc.Create(context.Background(), 1, "Test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.AddBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("AddBook: %v", err)
	}

	if err := svc.RemoveBook(context.Background(), 1, c.ID, 42); err != nil {
		t.Fatalf("RemoveBook: %v", err)
	}
}
```

- [ ] **Step 5: Run tests to verify they fail to compile**

Run: `go test ./internal/collections/... -v`
Expected: FAIL — `undefined: NewService` (package has no non-test file defining `Service` yet).

- [ ] **Step 6: Implement `Service`**

Write `internal/collections/service.go`:

```go
package collections

import (
	"context"
	"strings"
)

// Service trims and validates the one thing this package is not pure CRUD
// about — the name — and delegates everything else to the store.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, userID int64, name string) (Collection, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Collection{}, ErrEmptyName
	}
	return s.store.Create(ctx, userID, trimmed)
}

// List normalizes a nil result to an empty slice so callers never have to
// distinguish "no collections" from "store returned nil".
func (s *Service) List(ctx context.Context, userID int64) ([]Collection, error) {
	found, err := s.store.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return []Collection{}, nil
	}
	return found, nil
}

func (s *Service) Rename(ctx context.Context, userID, id int64, name string) (Collection, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Collection{}, ErrEmptyName
	}
	return s.store.Rename(ctx, userID, id, trimmed)
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	return s.store.Delete(ctx, userID, id)
}

func (s *Service) AddBook(ctx context.Context, userID, id, bookID int64) (Collection, error) {
	return s.store.AddBook(ctx, userID, id, bookID)
}

func (s *Service) RemoveBook(ctx context.Context, userID, id, bookID int64) error {
	return s.store.RemoveBook(ctx, userID, id, bookID)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/collections/... -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/collections/
git commit -m "collections: add Service, Store interface, and types"
```

---

## Task 5: SQLite store for collections

**Files:**
- Create: `internal/store/collections.go`
- Create: `internal/store/collections_test.go`

**Interfaces:**
- Consumes: `collections.Collection`, `collections.Store`, `collections.ErrNotFound`, `collections.ErrUnknownBook`, `collections.ErrBookNotInCollection` from Task 4.
- Produces: `(*SQLite)` satisfying `collections.Store`. Task 6's `server.New` wires `collections.NewService(sqlStore)` against it.

- [ ] **Step 1: Write the failing store tests**

Write `internal/store/collections_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -run 'Collection' -v`
Expected: FAIL to compile — `s.Create undefined` (no `Create(ctx, userID, name)` on `*SQLite` yet; note `store.go` already has a user-level `CreateUser`, not `Create`).

- [ ] **Step 3: Implement the SQLite store**

Write `internal/store/collections.go`:

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

	"github.com/kate/book-tracking/internal/collections"
)

func (s *SQLite) Create(ctx context.Context, userID int64, name string) (collections.Collection, error) {
	const q = `INSERT INTO collections (user_id, name, created_at) VALUES (?, ?, ?)`

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, q, userID, name, now)
	if err != nil {
		return collections.Collection{}, fmt.Errorf("create collection: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return collections.Collection{}, fmt.Errorf("create collection: %w", err)
	}
	return collections.Collection{
		ID: id, UserID: userID, Name: name, BookIDs: []int64{}, CreatedAt: now,
	}, nil
}

func (s *SQLite) List(ctx context.Context, userID int64) ([]collections.Collection, error) {
	const q = `SELECT id, user_id, name, created_at FROM collections
	           WHERE user_id = ? ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()

	var out []collections.Collection
	var ids []int64
	for rows.Next() {
		var c collections.Collection
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("list collections: %w", err)
		}
		c.CreatedAt = c.CreatedAt.UTC()
		out = append(out, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}

	books, err := s.booksFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].BookIDs = booksOrEmpty(books[out[i].ID])
	}
	return out, nil
}

// get loads one collection scoped to userID, including its books. It is the
// shared "mutate, then re-read" tail for Rename, AddBook, and RemoveBook.
func (s *SQLite) get(ctx context.Context, userID, id int64) (collections.Collection, error) {
	const q = `SELECT id, user_id, name, created_at FROM collections
	           WHERE id = ? AND user_id = ?`

	var c collections.Collection
	row := s.db.QueryRowContext(ctx, q, id, userID)
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return collections.Collection{}, collections.ErrNotFound
	}
	if err != nil {
		return collections.Collection{}, fmt.Errorf("get collection: %w", err)
	}
	c.CreatedAt = c.CreatedAt.UTC()

	books, err := s.booksFor(ctx, []int64{c.ID})
	if err != nil {
		return collections.Collection{}, err
	}
	c.BookIDs = booksOrEmpty(books[c.ID])
	return c, nil
}

func (s *SQLite) Rename(ctx context.Context, userID, id int64, name string) (collections.Collection, error) {
	const q = `UPDATE collections SET name = ? WHERE id = ? AND user_id = ?`

	res, err := s.db.ExecContext(ctx, q, name, id, userID)
	if err != nil {
		return collections.Collection{}, fmt.Errorf("rename collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return collections.Collection{}, fmt.Errorf("rename collection: %w", err)
	}
	if n == 0 {
		return collections.Collection{}, collections.ErrNotFound
	}
	return s.get(ctx, userID, id)
}

func (s *SQLite) Delete(ctx context.Context, userID, id int64) error {
	const q = `DELETE FROM collections WHERE id = ? AND user_id = ?`

	res, err := s.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if n == 0 {
		return collections.ErrNotFound
	}
	return nil
}

// AddBook is INSERT OR IGNORE: adding a book already in the collection is a
// no-op, not an error, and the response always reflects the current state.
func (s *SQLite) AddBook(ctx context.Context, userID, id, bookID int64) (collections.Collection, error) {
	if _, err := s.get(ctx, userID, id); err != nil {
		return collections.Collection{}, err
	}

	const q = `INSERT OR IGNORE INTO collection_books (collection_id, book_id) VALUES (?, ?)`
	if _, err := s.db.ExecContext(ctx, q, id, bookID); err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintForeignKey {
			return collections.Collection{}, collections.ErrUnknownBook
		}
		return collections.Collection{}, fmt.Errorf("add book to collection: %w", err)
	}
	return s.get(ctx, userID, id)
}

func (s *SQLite) RemoveBook(ctx context.Context, userID, id, bookID int64) error {
	if _, err := s.get(ctx, userID, id); err != nil {
		return err
	}

	const q = `DELETE FROM collection_books WHERE collection_id = ? AND book_id = ?`
	res, err := s.db.ExecContext(ctx, q, id, bookID)
	if err != nil {
		return fmt.Errorf("remove book from collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove book from collection: %w", err)
	}
	if n == 0 {
		return collections.ErrBookNotInCollection
	}
	return nil
}

// booksFor loads every member book id for a page of collections in one
// query, keyed back by collection ID in Go — the same shape as
// (*SQLite).authorsFor in library.go.
func (s *SQLite) booksFor(ctx context.Context, collectionIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(collectionIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(collectionIDs)), ",")
	q := `SELECT collection_id, book_id FROM collection_books
	      WHERE collection_id IN (` + placeholders + `)
	      ORDER BY collection_id, book_id`

	args := make([]any, 0, len(collectionIDs))
	for _, id := range collectionIDs {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load collection books: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var collectionID, bookID int64
		if err := rows.Scan(&collectionID, &bookID); err != nil {
			return nil, fmt.Errorf("load collection books: %w", err)
		}
		out[collectionID] = append(out[collectionID], bookID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load collection books: %w", err)
	}
	return out, nil
}

func booksOrEmpty(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ collections.Store = (*SQLite)(nil)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/collections.go internal/store/collections_test.go
git commit -m "store: implement collections.Store against SQLite"
```

---

## Task 6: Collection HTTP handlers, routes, and server wiring

**Files:**
- Create: `internal/server/collection_handlers.go`
- Create: `internal/server/collection_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `collections.Service`, `collections.Collection`, `collections.Err*` from Task 4; SQLite store satisfying `collections.Store` from Task 5; `respondError`, `userID`, `libraryBookID` (reused for `:book_id`) from existing server package.
- Produces: `GET/POST /collections`, `PATCH/DELETE /collections/:id`, `POST /collections/:id/books`, `DELETE /collections/:id/books/:book_id`. `Server.collections *collections.Service`.

- [ ] **Step 1: Write the failing handler tests**

Write `internal/server/collection_test.go`:

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

type collectionBody struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	BookIDs   []int64 `json:"book_ids"`
	CreatedAt string  `json:"created_at"`
}

type collectionListBody struct {
	Items []collectionBody `json:"items"`
}

func TestCollectionRoutesRequireAuth(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/collections"},
		{http.MethodPost, "/collections"},
		{http.MethodPatch, "/collections/1"},
		{http.MethodDelete, "/collections/1"},
		{http.MethodPost, "/collections/1/books"},
		{http.MethodDelete, "/collections/1/books/1"},
	}
	for _, c := range cases {
		rec := doJSON(t, srv, c.method, c.path, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}

func TestCreateCollectionReturns201(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Summer reading"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var body collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "Summer reading" {
		t.Fatalf("name = %q", body.Name)
	}
	if body.BookIDs == nil || len(body.BookIDs) != 0 {
		t.Fatalf("book_ids = %v, want []", body.BookIDs)
	}
	if body.CreatedAt == "" {
		t.Fatal("created_at must be present")
	}
}

func TestCreateCollectionRejectsEmptyName(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	for _, name := range []string{"", "   "} {
		rec := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
			map[string]any{"name": name})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("name %q: status = %d, want 400: %s", name, rec.Code, rec.Body)
		}
	}
}

func TestListCollectionsReturnsEmptyArrayNotNull(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := get(t, srv, "/collections", "Bearer "+pair.AccessToken)
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

func TestRenameCollectionReturns200(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Original"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/collections/%d", c.ID),
		pair.AccessToken, map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var renamed collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", renamed.Name)
	}
}

func TestRenameCollectionMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodPatch, "/collections/9999", pair.AccessToken,
		map[string]any{"name": "Renamed"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestDeleteCollectionReturns204(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Gone soon"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/collections/%d", c.ID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
}

func TestDeleteCollectionMissingIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)

	rec := doAuthedJSON(t, srv, http.MethodDelete, "/collections/9999", pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAddBookToCollectionReturns201WithUpdatedList(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": bookID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var updated collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.BookIDs) != 1 || updated.BookIDs[0] != bookID {
		t.Fatalf("book_ids = %v, want [%d]", updated.BookIDs, bookID)
	}
}

func TestAddBookToCollectionIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	path := fmt.Sprintf("/collections/%d/books", c.ID)

	if rec := doAuthedJSON(t, srv, http.MethodPost, path, pair.AccessToken,
		map[string]any{"book_id": bookID}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}
	rec := doAuthedJSON(t, srv, http.MethodPost, path, pair.AccessToken, map[string]any{"book_id": bookID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var updated collectionBody
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.BookIDs) != 1 {
		t.Fatalf("book_ids = %v, want exactly one entry", updated.BookIDs)
	}
}

func TestAddUnknownBookToCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}

	rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": 9999})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestAddBookToMissingCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodPost, "/collections/9999/books", pair.AccessToken,
		map[string]any{"book_id": bookID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

func TestRemoveBookFromCollectionReturns204(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)
	if rec := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		pair.AccessToken, map[string]any{"book_id": bookID}); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body)
	}

	rec := doAuthedJSON(t, srv, http.MethodDelete,
		fmt.Sprintf("/collections/%d/books/%d", c.ID, bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
}

func TestRemoveBookNotInCollectionIs404(t *testing.T) {
	srv := newTestServer(t)
	pair := registerAndLogin(t, srv)
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", pair.AccessToken,
		map[string]any{"name": "Test"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rec := doAuthedJSON(t, srv, http.MethodDelete,
		fmt.Sprintf("/collections/%d/books/%d", c.ID, bookID), pair.AccessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
}

// Another user's collection must be indistinguishable from one that does
// not exist: 404 everywhere, never 403, and never visible in a list.
func TestCollectionsAreScopedToTheCaller(t *testing.T) {
	srv := newTestServer(t)
	a := registerAndLogin(t, srv)
	b := registerAndLoginAs(t, srv, "b@b.com")
	created := doAuthedJSON(t, srv, http.MethodPost, "/collections", a.AccessToken,
		map[string]any{"name": "A's collection"})
	var c collectionBody
	if err := json.Unmarshal(created.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	bookID := seedBookRow(t, srv, "vol-dune", "Dune", nil)

	rename := doAuthedJSON(t, srv, http.MethodPatch, fmt.Sprintf("/collections/%d", c.ID),
		b.AccessToken, map[string]any{"name": "Stolen"})
	if rename.Code != http.StatusNotFound {
		t.Fatalf("cross-user rename = %d, want 404: %s", rename.Code, rename.Body)
	}
	del := doAuthedJSON(t, srv, http.MethodDelete, fmt.Sprintf("/collections/%d", c.ID), b.AccessToken, nil)
	if del.Code != http.StatusNotFound {
		t.Fatalf("cross-user delete = %d, want 404: %s", del.Code, del.Body)
	}
	add := doAuthedJSON(t, srv, http.MethodPost, fmt.Sprintf("/collections/%d/books", c.ID),
		b.AccessToken, map[string]any{"book_id": bookID})
	if add.Code != http.StatusNotFound {
		t.Fatalf("cross-user add book = %d, want 404: %s", add.Code, add.Body)
	}

	list := get(t, srv, "/collections", "Bearer "+b.AccessToken)
	var listBody collectionListBody
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Items) != 0 {
		t.Fatalf("user B sees %d of user A's collections", len(listBody.Items))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/... -run Collection -v`
Expected: FAIL — `s.collections undefined` (compile failure, since `Server` has no `collections` field yet).

- [ ] **Step 3: Implement the handlers**

Write `internal/server/collection_handlers.go`:

```go
package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/collections"
)

// collectionRequest carries the name for both create and rename. It has no
// binding tag: an empty or whitespace-only name is Service's job to reject,
// the same way updateLibraryRequest leaves validation to library.Service.
type collectionRequest struct {
	Name string `json:"name"`
}

// addBookRequest identifies the book by local ID only, the same contract
// addLibraryRequest uses.
type addBookRequest struct {
	BookID int64 `json:"book_id" binding:"required,gt=0"`
}

type collectionResponse struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	BookIDs   []int64 `json:"book_ids"`
	CreatedAt string  `json:"created_at"`
}

func newCollectionResponse(c collections.Collection) collectionResponse {
	ids := c.BookIDs
	if ids == nil {
		ids = []int64{}
	}
	return collectionResponse{
		ID:        c.ID,
		Name:      c.Name,
		BookIDs:   ids,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// respondCollectionsError maps service errors to status codes. A missing
// collection and another user's collection are both 404, mirroring
// respondLibraryError's treatment of library entries.
func respondCollectionsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, collections.ErrEmptyName):
		respondError(c, http.StatusBadRequest, "collection name must not be empty")
	case errors.Is(err, collections.ErrNotFound):
		respondError(c, http.StatusNotFound, "collection not found")
	case errors.Is(err, collections.ErrUnknownBook):
		respondError(c, http.StatusNotFound, "no book with that id")
	case errors.Is(err, collections.ErrBookNotInCollection):
		respondError(c, http.StatusNotFound, "book not in that collection")
	default:
		log.Printf("collections: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

// collectionID reads the :id path parameter.
func collectionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) handleCollectionsList(c *gin.Context) {
	items, err := s.collections.List(c.Request.Context(), userID(c))
	if err != nil {
		respondCollectionsError(c, err)
		return
	}

	resp := make([]collectionResponse, 0, len(items))
	for _, i := range items {
		resp = append(resp, newCollectionResponse(i))
	}
	c.JSON(http.StatusOK, gin.H{"items": resp})
}

func (s *Server) handleCollectionsCreate(c *gin.Context) {
	var req collectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := s.collections.Create(c.Request.Context(), userID(c), req.Name)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newCollectionResponse(created))
}

func (s *Server) handleCollectionsRename(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	var req collectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.collections.Rename(c.Request.Context(), userID(c), id, req.Name)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newCollectionResponse(updated))
}

func (s *Server) handleCollectionsDelete(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	if err := s.collections.Delete(c.Request.Context(), userID(c), id); err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleCollectionsAddBook(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	var req addBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.collections.AddBook(c.Request.Context(), userID(c), id, req.BookID)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newCollectionResponse(updated))
}

func (s *Server) handleCollectionsRemoveBook(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	if err := s.collections.RemoveBook(c.Request.Context(), userID(c), id, bookID); err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
```

- [ ] **Step 4: Wire the server**

In `internal/server/server.go`, add the import and field:

```go
import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/collections"
	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/library"
	"github.com/kate/book-tracking/internal/store"
)

type Server struct {
	db          *sql.DB
	router      *gin.Engine
	auth        *auth.Service
	books       *books.Service
	library     *library.Service
	collections *collections.Service
	signer      *auth.Signer
}

func New(db *sql.DB, cfg config.Config) *Server {
	signer := auth.NewSigner(cfg.JWTSecret, cfg.AccessTTL)
	sqlStore := store.New(db)
	s := &Server{
		db:          db,
		router:      gin.Default(),
		auth:        auth.NewService(sqlStore, signer, cfg.RefreshTTL),
		books:       books.NewService(books.NewGoogleBooks(cfg.GoogleBooksAPIKey), sqlStore),
		library:     library.NewService(sqlStore),
		collections: collections.NewService(sqlStore),
		signer:      signer,
	}
	s.routes()
	return s
}
```

Add the routes in `routes()`, after the rating routes added in Task 3:

```go
	authed.GET("/collections", s.handleCollectionsList)
	authed.POST("/collections", s.handleCollectionsCreate)
	authed.PATCH("/collections/:id", s.handleCollectionsRename)
	authed.DELETE("/collections/:id", s.handleCollectionsDelete)
	authed.POST("/collections/:id/books", s.handleCollectionsAddBook)
	authed.DELETE("/collections/:id/books/:book_id", s.handleCollectionsRemoveBook)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS for every package.

- [ ] **Step 6: Run the whole build and vet**

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/server/collection_handlers.go internal/server/collection_test.go internal/server/server.go
git commit -m "server: add collection endpoints and wire the collections service"
```

---

## Task 7: `api/library.js` rating calls, `stores/library.js` applyRating, `stores/ratings.js` rewrite

**Files:**
- Modify: `web/src/api/library.js`
- Modify: `web/src/stores/library.js`
- Modify: `web/src/stores/ratings.js`

**Interfaces:**
- Consumes: `PUT/DELETE /library/:book_id/rating` from Task 3; `useLibraryStore().byBookId` (existing computed).
- Produces: `setRating(bookId, score)`, `clearRating(bookId)` in `api/library.js`; `library.applyRating(bookId, rating)`; `useRatingsStore()` with the same public shape (`get`, `set`, `clear`) but no `byBookId`/`hydrate`/`reset`.

This task has no automated test (frontend has no existing Vitest coverage for either store, matching the spec's "Testing" section) — verify by reading the diff and, once Task 11 finishes the wiring, the manual smoke test.

- [ ] **Step 1: Add rating calls to `api/library.js`**

Append to `web/src/api/library.js`:

```js
export function setRating(bookId, score) {
  return request(`/library/${bookId}/rating`, { method: 'PUT', body: { score } })
}

export function clearRating(bookId) {
  return request(`/library/${bookId}/rating`, { method: 'DELETE' })
}
```

- [ ] **Step 2: Add `applyRating` to `stores/library.js`**

In `web/src/stores/library.js`, add the function near `upsert`:

```js
  // Patches the cached entry's rating in place rather than refetching: the
  // rating endpoints don't return a fresh library list, only the one entry
  // the ratings store already has via setRating/clearRating's response.
  function applyRating(bookId, rating) {
    const i = entries.value.findIndex((e) => e.book.id === bookId)
    if (i < 0) return
    entries.value[i] = { ...entries.value[i], rating }
  }
```

Add `applyRating` to the returned object:

```js
  return {
    entries, loading, error, loaded,
    byBookId, count, counts, filtered,
    fetchAll, add, setStatus, setFinishedAt, remove, applyRating, reset,
  }
```

- [ ] **Step 3: Rewrite `stores/ratings.js`**

Replace the full contents of `web/src/stores/ratings.js`:

```js
import { defineStore } from 'pinia'
import { useLibraryStore } from '@/stores/library'
import { setRating, clearRating } from '@/api/library'

// Ratings are folded into library entries server-side (backend Milestone 5):
// GET/POST/PATCH /library already return "rating", so this store holds no
// state of its own — it reads through useLibraryStore() and writes through
// the rating endpoints, patching the cached entry rather than refetching.

export const useRatingsStore = defineStore('ratings', () => {
  function get(bookId) {
    return useLibraryStore().byBookId.get(bookId)?.rating ?? 0
  }

  // Sets the exact value. Toggling off is the caller's decision — the star
  // widget treats a click on the current rating as "clear", but an import
  // must not silently undo a rating it is re-applying.
  async function set(bookId, rating) {
    const library = useLibraryStore()
    if (!rating) {
      await clearRating(bookId)
      library.applyRating(bookId, null)
      return
    }
    const clamped = Math.max(1, Math.min(5, rating))
    await setRating(bookId, clamped)
    library.applyRating(bookId, clamped)
  }

  async function clear(bookId) {
    await clearRating(bookId)
    useLibraryStore().applyRating(bookId, null)
  }

  return { get, set, clear }
})
```

- [ ] **Step 4: Commit**

```bash
git add web/src/api/library.js web/src/stores/library.js web/src/stores/ratings.js
git commit -m "web: back ratings store with the rating API"
```

---

## Task 8: `api/collections.js`, `stores/collections.js` rewrite

**Files:**
- Create: `web/src/api/collections.js`
- Modify: `web/src/stores/collections.js`

**Interfaces:**
- Consumes: `GET/POST /collections`, `PATCH/DELETE /collections/:id`, `POST /collections/:id/books`, `DELETE /collections/:id/books/:book_id` from Task 6.
- Produces: `api/collections.js` exports `list`, `create`, `rename`, `remove`, `addBook`, `removeBook`. `useCollectionsStore()` keeps its full existing public method set (`items`, `hydrate`, `create`, `rename`, `remove`, `find`, `addBook`, `removeBook`, `toggleBook`, `contains`, `forgetBook`, `reset`) — bodies become `async`, and items are normalized from the wire's `book_ids` to the store's existing `bookIds` field so no view needs to change field names.

- [ ] **Step 1: Write `api/collections.js`**

```js
import { request } from './client'

export function list() {
  return request('/collections')
}

export function create(name) {
  return request('/collections', { method: 'POST', body: { name } })
}

export function rename(id, name) {
  return request(`/collections/${id}`, { method: 'PATCH', body: { name } })
}

export function remove(id) {
  return request(`/collections/${id}`, { method: 'DELETE' })
}

export function addBook(id, bookId) {
  return request(`/collections/${id}/books`, { method: 'POST', body: { book_id: bookId } })
}

export function removeBook(id, bookId) {
  return request(`/collections/${id}/books/${bookId}`, { method: 'DELETE' })
}
```

- [ ] **Step 2: Rewrite `stores/collections.js`**

Replace the full contents of `web/src/stores/collections.js`:

```js
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as collectionsApi from '@/api/collections'

// Collections are backed by the API (backend Milestone 6). IDs are compared
// as strings throughout: route params (CollectionDetailView's props.id) are
// always strings, but the API returns numeric ids.
//
// The wire shape uses book_ids; this store keeps the field as bookIds so no
// view needs to change — normalize() is the only place the two names meet.
function normalize(c) {
  return { id: c.id, name: c.name, bookIds: c.book_ids }
}

export const useCollectionsStore = defineStore('collections', () => {
  const items = ref([])

  async function hydrate() {
    const { items: fetched } = await collectionsApi.list()
    items.value = fetched.map(normalize)
  }

  async function create(name) {
    const trimmed = name.trim()
    if (!trimmed) return null
    const created = await collectionsApi.create(trimmed)
    const collection = normalize(created)
    items.value = [...items.value, collection]
    return collection
  }

  async function rename(id, name) {
    const trimmed = name.trim()
    if (!trimmed) return
    const updated = await collectionsApi.rename(id, trimmed)
    items.value = items.value.map((c) => (String(c.id) === String(id) ? normalize(updated) : c))
  }

  async function remove(id) {
    await collectionsApi.remove(id)
    items.value = items.value.filter((c) => String(c.id) !== String(id))
  }

  function find(id) {
    return items.value.find((c) => String(c.id) === String(id)) ?? null
  }

  async function addBook(collectionId, bookId) {
    if (bookId == null) return
    const updated = await collectionsApi.addBook(collectionId, bookId)
    items.value = items.value.map((c) =>
      String(c.id) === String(collectionId) ? normalize(updated) : c,
    )
  }

  async function removeBook(collectionId, bookId) {
    await collectionsApi.removeBook(collectionId, bookId)
    items.value = items.value.map((c) =>
      String(c.id) !== String(collectionId)
        ? c
        : { ...c, bookIds: c.bookIds.filter((id) => id !== bookId) },
    )
  }

  async function toggleBook(collectionId, bookId) {
    const c = find(collectionId)
    if (!c) return
    if (c.bookIds.includes(bookId)) await removeBook(collectionId, bookId)
    else await addBook(collectionId, bookId)
  }

  function contains(collectionId, bookId) {
    return find(collectionId)?.bookIds.includes(bookId) ?? false
  }

  // Called when a book leaves the library, so a collection cannot keep
  // pointing at an entry that is gone. This is a local-only cache update —
  // the backend has no "remove this book from every collection" call, and
  // none is needed: the book row staying out of user_books does not orphan
  // collection_books rows in a way that matters to this client.
  function forgetBook(bookId) {
    items.value = items.value.map((c) => ({ ...c, bookIds: c.bookIds.filter((id) => id !== bookId) }))
  }

  function reset() {
    items.value = []
  }

  return {
    items, hydrate, create, rename, remove, find,
    addBook, removeBook, toggleBook, contains, forgetBook, reset,
  }
})
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/collections.js web/src/stores/collections.js
git commit -m "web: back collections store with the collections API"
```

---

## Task 9: `BookDetailDialog.vue` — async rating and collection toggles

**Files:**
- Modify: `web/src/components/BookDetailDialog.vue`

**Interfaces:**
- Consumes: `useRatingsStore().set` (now async, from Task 7), `useCollectionsStore().toggleBook` (now async, from Task 8).
- Produces: `rate(n)` and a new `toggleCollection(id)` handler, both async with the same busy/error pattern `setStatus`/`removeFromLibrary` already use in this file.

- [ ] **Step 1: Update the script**

In `web/src/components/BookDetailDialog.vue`, replace the rating section:

```js
const rating = computed(() => (book.value ? ratings.get(book.value.id) : 0))

// The rating widget is only meaningful on a finished book — the backend
// rejects a rating unless status is read, so the UI enforces the same rule
// here rather than letting users attempt something the API will refuse.
const canRate = computed(() => entry.value?.status === 'read')

// Clicking the star that is already lit clears the rating — the widget's
// only way to say "no rating" without a separate control.
async function rate(n) {
  if (!book.value) return
  const next = rating.value === n ? 0 : n
  busy.value = true
  error.value = ''
  try {
    await ratings.set(book.value.id, next)
  } catch (e) {
    error.value = e.message || 'Could not update that rating.'
  } finally {
    busy.value = false
  }
}

async function toggleCollection(collectionId) {
  if (!book.value) return
  busy.value = true
  error.value = ''
  try {
    await collections.toggleBook(collectionId, book.value.id)
  } catch (e) {
    error.value = e.message || 'Could not update that collection.'
  } finally {
    busy.value = false
  }
}
```

- [ ] **Step 2: Update the template**

Replace the rating and collections blocks:

```html
      <div>
        <div class="field-label">Your rating</div>
        <div class="stars">
          <button
            v-for="n in 5"
            :key="n"
            type="button"
            class="star"
            :class="{ 'is-on': n <= rating }"
            :disabled="!canRate || busy"
            :aria-label="`${n} star${n > 1 ? 's' : ''}`"
            @click="rate(n)"
          >
            &starf;
          </button>
        </div>
        <p v-if="!canRate" class="stranded-note">Mark this book as read to rate it.</p>
      </div>

      <div>
        <div class="field-label">Collections</div>
        <div v-if="collections.items.length" class="collection-tags">
          <button
            v-for="c in collections.items"
            :key="c.id"
            type="button"
            class="tag"
            :class="collections.contains(c.id, book.id) ? 'tag-accent' : 'tag-outline'"
            :disabled="busy"
            @click="toggleCollection(c.id)"
          >
            {{ c.name }}
          </button>
        </div>
        <p v-else class="stranded-note">No collections yet — create one on the Collections tab.</p>
      </div>
```

(This drops the `<p v-else class="stranded-note">Saved in this browser only — backend Milestone 5.</p>` line entirely — ratings are backed by the API now.)

- [ ] **Step 3: Commit**

```bash
git add web/src/components/BookDetailDialog.vue
git commit -m "web: make rating and collection toggles async in the book dialog"
```

---

## Task 10: `CollectionsView.vue` and `CollectionDetailView.vue` — async create/rename/delete/drag

**Files:**
- Modify: `web/src/views/CollectionsView.vue`
- Modify: `web/src/views/CollectionDetailView.vue`

**Interfaces:**
- Consumes: `useCollectionsStore()`'s now-async `create`, `rename`, `remove`, `addBook`, `removeBook` from Task 8.
- Produces: same views, same routes, with busy/error handling around every store call that now returns a promise.

- [ ] **Step 1: Update `CollectionsView.vue`'s script**

Replace the script block:

```js
<script setup>
import { ref, computed } from 'vue'
import { useCollectionsStore } from '@/stores/collections'
import { useLibraryStore } from '@/stores/library'
import { coverStyle, coverInitial } from '@/lib/covers'

const collections = useCollectionsStore()
const library = useLibraryStore()

const showForm = ref(false)
const newName = ref('')
const busy = ref(false)
const error = ref('')

const cards = computed(() =>
  collections.items.map((c) => ({
    id: c.id,
    name: c.name,
    count: c.bookIds.length,
    // Only books still in the library can be shown; a removed one leaves the
    // count honest but has no cover to draw.
    thumbs: c.bookIds
      .map((id) => library.byBookId.get(id))
      .filter(Boolean)
      .slice(0, 4)
      .map((e) => ({ id: e.book.id, title: e.book.title })),
  })),
)

async function create() {
  busy.value = true
  error.value = ''
  try {
    const created = await collections.create(newName.value)
    if (created) {
      newName.value = ''
      showForm.value = false
    }
  } catch (e) {
    error.value = e.message || 'Could not create that collection.'
  } finally {
    busy.value = false
  }
}
</script>
```

- [ ] **Step 2: Update `CollectionsView.vue`'s template**

Replace the form and drop the stranded note:

```html
  <div v-if="showForm" class="card new-form">
    <div class="field">
      <label for="name">Collection name</label>
      <input id="name" v-model="newName" class="input" type="text" @keyup.enter="create" />
    </div>
    <button class="btn btn-primary" type="button" :disabled="busy" @click="create">Create</button>
    <p v-if="error" class="form-error">{{ error }}</p>
  </div>

  <div class="book-grid wide">
    <RouterLink
      v-for="c in cards"
      :key="c.id"
      :to="{ name: 'collection', params: { id: c.id } }"
      class="collection-card"
    >
      <div class="thumbs">
        <div
          v-for="t in c.thumbs"
          :key="t.id"
          class="thumb"
          :style="coverStyle(t.id)"
        >
          {{ coverInitial(t.title) }}
        </div>
      </div>
      <div class="card-title">{{ c.name }}</div>
      <div class="card-meta">{{ c.count }} {{ c.count === 1 ? 'book' : 'books' }}</div>
    </RouterLink>
  </div>

  <p v-if="!cards.length" class="text-muted empty">
    No collections yet — create one to start grouping books.
  </p>
```

(This drops `<p class="stranded-note">Collections are saved in this browser only — backend Milestone 6.</p>` entirely.)

- [ ] **Step 3: Update `CollectionDetailView.vue`'s script**

Replace the script block:

```js
<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useCollectionsStore } from '@/stores/collections'
import { useLibraryStore } from '@/stores/library'
import BookCover from '@/components/BookCover.vue'

const props = defineProps({ id: { type: String, required: true } })

const router = useRouter()
const collections = useCollectionsStore()
const library = useLibraryStore()

const collection = computed(() => collections.find(props.id))
const dragOver = ref(false)
const busy = ref(false)
const error = ref('')

const books = computed(() =>
  (collection.value?.bookIds ?? [])
    .map((id) => library.byBookId.get(id))
    .filter(Boolean)
    .map((e) => e.book),
)

// The left rail lists everything not already in this collection, so dropping a
// duplicate is not something the user can attempt in the first place.
const available = computed(() => {
  const inCollection = new Set(collection.value?.bookIds ?? [])
  return library.entries.filter((e) => !inCollection.has(e.book.id)).map((e) => e.book)
})

function onDragStart(event, bookId) {
  event.dataTransfer.setData('text/plain', String(bookId))
  event.dataTransfer.effectAllowed = 'copy'
}

async function addBook(bookId) {
  busy.value = true
  error.value = ''
  try {
    await collections.addBook(props.id, bookId)
  } catch (e) {
    error.value = e.message || 'Could not add that book.'
  } finally {
    busy.value = false
  }
}

async function onDrop(event) {
  dragOver.value = false
  const raw = event.dataTransfer.getData('text/plain')
  const bookId = Number(raw)
  // Book ids are integers from the API; anything else came from a drag that
  // did not start in this rail.
  if (!raw || Number.isNaN(bookId)) return
  await addBook(bookId)
}

async function removeMember(bookId) {
  busy.value = true
  error.value = ''
  try {
    await collections.removeBook(props.id, bookId)
  } catch (e) {
    error.value = e.message || 'Could not remove that book.'
  } finally {
    busy.value = false
  }
}

async function renameCollection() {
  const next = window.prompt('Rename collection', collection.value.name)
  if (next === null) return
  busy.value = true
  error.value = ''
  try {
    await collections.rename(props.id, next)
  } catch (e) {
    error.value = e.message || 'Could not rename that collection.'
  } finally {
    busy.value = false
  }
}

async function deleteCollection() {
  if (!window.confirm(`Delete "${collection.value.name}"? The books stay in your library.`)) return
  busy.value = true
  error.value = ''
  try {
    await collections.remove(props.id)
    router.push({ name: 'collections' })
  } catch (e) {
    error.value = e.message || 'Could not delete that collection.'
  } finally {
    busy.value = false
  }
}
</script>
```

- [ ] **Step 4: Update `CollectionDetailView.vue`'s template**

Replace the head, rail-add button, member-remove button, and add an error line:

```html
    <div class="head">
      <h1 class="detail-title">{{ collection.name }}</h1>
      <div class="head-actions">
        <button class="btn btn-secondary" type="button" :disabled="busy" @click="renameCollection">Rename</button>
        <button class="btn btn-secondary danger" type="button" :disabled="busy" @click="deleteCollection">Delete</button>
      </div>
    </div>
    <p class="text-muted page-subtitle">Drag a book from your library onto the collection to add it.</p>
    <p v-if="error" class="form-error">{{ error }}</p>
```

```html
          <div
            v-for="book in available"
            :key="book.id"
            class="rail-item"
            draggable="true"
            @dragstart="onDragStart($event, book.id)"
          >
            <BookCover :book="book" width="24px" height="32px" font-size="12px" />
            <span class="rail-title">{{ book.title }}</span>
            <button class="rail-add" type="button" :disabled="busy" @click="addBook(book.id)">+</button>
          </div>
```

```html
            <button
              class="remove"
              type="button"
              :aria-label="`Remove ${book.title}`"
              :disabled="busy"
              @click="removeMember(book.id)"
            >
              &times;
            </button>
```

- [ ] **Step 5: Commit**

```bash
git add web/src/views/CollectionsView.vue web/src/views/CollectionDetailView.vue
git commit -m "web: make collection create/rename/delete/drag async"
```

---

## Task 11: `App.vue`, `runImport.js`, README — drop the localStorage-only wiring

**Files:**
- Modify: `web/src/App.vue`
- Modify: `web/src/lib/runImport.js`
- Modify: `web/README.md`

**Interfaces:**
- Consumes: the rewritten `useRatingsStore()` (no `hydrate`/`reset`) and `useCollectionsStore()` (unchanged shape, still has `hydrate`/`reset`) from Tasks 7–8.
- Produces: `App.vue` no longer imports or calls into `useRatingsStore`; `runImport.js` awaits the now-async `ratings.set`; `README.md`'s "not backed by the API yet" table drops the ratings and collections rows.

- [ ] **Step 1: Update `App.vue`**

Replace the script block:

```js
<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useLibraryStore } from '@/stores/library'
import { useCollectionsStore } from '@/stores/collections'
import { useSettingsStore } from '@/stores/settings'
import SideNav from '@/components/SideNav.vue'
import BookDetailDialog from '@/components/BookDetailDialog.vue'

const route = useRoute()
const auth = useAuthStore()
const library = useLibraryStore()
const collections = useCollectionsStore()
const settings = useSettingsStore()

// Everything user-scoped is (re)loaded when the signed-in user changes, and
// dropped on sign-out. Ratings have no independent state to load or clear —
// they read through the library store — so only collections and settings
// hydrate here alongside the library fetch.
watch(
  () => auth.userId,
  (id) => {
    if (id === null) {
      library.reset()
      collections.reset()
      settings.reset()
      return
    }
    collections.hydrate()
    settings.hydrate(auth.email.split('@')[0])
    library.fetchAll()
  },
  { immediate: true },
)
</script>
```

- [ ] **Step 2: Update `runImport.js`**

In `web/src/lib/runImport.js`, change the ratings comment and await the now-async call:

```js
      // Ratings go through the same PUT the star widget uses; an imported
      // one lands in the same place.
      if (record.rating > 0) await ratings.set(book.id, record.rating)
```

(This replaces the old `if (record.rating > 0) ratings.set(book.id, record.rating)` line and its "local-only until backend M5" comment.)

- [ ] **Step 3: Update `web/README.md`**

Read the current "What is not backed by the API yet" section first (`web/README.md`, starting at the `## What is not backed by the API yet` heading), then replace it so only the row that still applies remains:

```markdown
## What is not backed by the API yet

One area of the design has no endpoint behind it: display name and annual
reading goal are not specified in any milestone, so `stores/settings.js`
still persists them to `localStorage`, namespaced per user id.

Ratings (backend Milestone 5, `PUT/DELETE /library/:book_id/rating`) and
collections (backend Milestone 6, `/collections`, `/collections/:id/books`)
are now backed by the API — `stores/ratings.js` and `stores/collections.js`
call it directly and hold no `localStorage` state of their own.

Two smaller gaps:
```

(Keep everything from the original "Two smaller gaps:" line onward unchanged — only the paragraph and table above it change.)

- [ ] **Step 4: Commit**

```bash
git add web/src/App.vue web/src/lib/runImport.js web/README.md
git commit -m "web: drop the localStorage-only ratings wiring from App.vue and README"
```

---

## Task 12: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Backend tests, build, and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build succeeds, vet is silent, all tests pass.

- [ ] **Step 2: Frontend build**

Run: `cd web && npm run build`
Expected: build succeeds with no errors (Vite will fail the build on a Vue template/script error, which is the main risk from Tasks 9–10's edits).

- [ ] **Step 3: Frontend unit tests**

Run: `cd web && npm run test`
Expected: the existing 28 Vitest tests (statistics and import parsers) still pass. Neither rewritten store has its own test coverage — that gap is pre-existing per `web/README.md`'s testing notes, not introduced by this plan.

- [ ] **Step 4: Manual smoke test**

Start the backend (`go run ./cmd/server`) and the frontend dev server (`cd web && npm run dev`), then in a browser:
- Mark a book read, rate it, reload the page, and confirm the rating persisted.
- Clear the rating and confirm the stars go dark.
- Try to rate a book that is not marked read and confirm the star row is disabled.
- Create a collection, rename it, and confirm the new name persists after reload.
- Drag a book from the library rail into the collection, and remove it via the × button.
- Delete the collection and confirm it disappears from the Collections tab.
- Import a small spreadsheet with a rating column and confirm the imported rating shows on the book.

This step has no command to run — record the outcome in the PR description or commit message instead of a checkbox here.

- [ ] **Step 5: Commit** (only if Step 4 turned up a fix)

If the smoke test found nothing to fix, skip this step — there is nothing to commit. Otherwise, fix, re-run the affected steps above, and commit as its own change with a message describing what the smoke test caught.
