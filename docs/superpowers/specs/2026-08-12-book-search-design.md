# Book Search & Metadata Design — Backend Milestone 3

Date: 2026-08-12
Status: Approved, not yet implemented
Covers: `specs/backend/implementation.md` Milestone 3

## Goal

Let an authenticated user find books by title/author or by ISBN, backed by an external metadata
provider, and give every result a stable local identity. Milestones 4–7 all address books by a
local `book_id`, so this milestone owns book identity as much as it owns search.

## Decisions

| Decision | Choice | Reason |
|---|---|---|
| Metadata provider | Google Books | Title, authors, language and thumbnail arrive in one volume response; Open Library needs follow-up calls and has messier language data |
| API key | `GOOGLE_BOOKS_API_KEY`, optional | Google works keyless at low volume; requiring it would block any test run or contributor without a key |
| Caching | Upsert every result into `books`, keyed by Google volume ID | Search results come back already carrying a local `id`, so Milestone 4 can add one to a library without a second lookup |
| Dedupe key | `books.external_id` UNIQUE (Google volume ID) | ISBN is absent on many volumes and duplicated across editions; the volume ID is the provider's own stable key |
| Authors | Normalized `book_authors` table | Milestone 7 ranks authors by books read; a joined `"A, B"` string would rank a co-authored pair as its own author |
| Route auth | Both endpoints behind `RequireAuth` | Every request spends API quota and writes rows; anonymous callers should not be able to do either |
| Upstream failures | 502 for everything except a passed-through 429 | One upstream-failure code the clients can handle; splitting 504 out adds a case without changing what a client does |
| Tests | Written first | Same as Milestone 2 |

## Architecture

Mirrors Milestone 2: a pure core importing neither gin nor `database/sql`, a `Store` interface for
persistence, thin gin handlers.

```
internal/books/
  book.go       Book{ID, ExternalID, ISBN, Title, Authors []string, Language, CoverURL, Source}
  provider.go   Provider interface: Search(ctx, q, page, limit) ([]Book, error)
                                    ByISBN(ctx, isbn) (Book, error)
  google.go     Google Books client — net/http only, volume JSON -> Book
  service.go    Service{provider, repo}: fetch upstream -> upsert -> return rows with local IDs
  errors.go     ErrNotFound, ErrInvalidISBN, ErrUpstream

internal/store/
  books.go      UpsertBooks(ctx, []Book) ([]Book, error) — extends the existing SQLite store

internal/server/
  book_handlers.go   gin -> service -> JSON, registered on the `authed` group
```

The `Provider` seam is what keeps this testable without network access: `google.go` takes an
injectable base URL, so tests point it at an `httptest.Server` serving fixture JSON, and the
service is tested against a fake provider.

## Data flow

```
GET /books/search?q=dune
  -> RequireAuth
  -> Service.Search
       -> Provider.Search        (Google Books volumes API)
       -> Store.UpsertBooks      (one transaction, returns local IDs)
  -> {"items": [{id: 42, external_id: "...", title, authors: [...], ...}], "page": 1, "limit": 20}
```

## Schema — migration `000005`

Two changes in one migration:

**`book_authors`**

```sql
CREATE TABLE book_authors (
    book_id  INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (book_id, position)
);
CREATE INDEX idx_book_authors_name ON book_authors(name);
```

Backfilled from the existing `books.author` at `position = 0`, after which
`ALTER TABLE books DROP COLUMN author`. The table holds no rows in practice today — nothing
creates books yet — but the backfill is written and tested regardless. The index on `name` serves
Milestone 7's top-authors report.

**`books.external_id`**

```sql
ALTER TABLE books ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX idx_books_external_id ON books(external_id);
```

SQLite permits multiple NULLs under a UNIQUE index, so rows predating this column are unaffected.
`metadata_source` is set to `'google_books'` on every upserted row.

The down migration reverses both: re-add `author`, backfill from `position = 0`, drop
`book_authors`, drop `external_id`.

**Upsert semantics.** One transaction per call:
`INSERT INTO books (...) VALUES (...) ON CONFLICT(external_id) DO UPDATE SET ... RETURNING id`,
then delete and reinsert that book's `book_authors` rows. Re-running a search overwrites the
metadata rather than creating duplicates, and a book that lost an author upstream loses it locally.

## Endpoints

Both are registered on the existing `authed` group and answer 401 without a bearer token.

### `GET /books/search?q=&page=&limit=`

- `q` is required and non-blank; empty or whitespace-only is 400 with no outbound call.
- `limit` defaults to 20 and is capped at 40 — Google's own `maxResults` ceiling.
- `page` is 1-based, defaults to 1, and maps to `startIndex = (page - 1) * limit`.
- Response: `{"items": [...], "page": n, "limit": n}`. An empty result set is 200 with `items: []`,
  not 404.

### `GET /books/isbn/:isbn`

- The ISBN is normalized (hyphens and spaces stripped, trailing `x` uppercased) and checksum-
  validated locally, for both ISBN-10 and ISBN-13. An invalid ISBN is 400 before any outbound call.
- Queried upstream as `q=isbn:<normalized>`, taking the first volume.
- No matching volume is 404.

### Mapping rules

- A volume with no title is skipped — `books.title` is NOT NULL and a titleless volume is unusable.
- A volume with no `authors` is kept and produces zero `book_authors` rows.
- `industryIdentifiers` is searched for ISBN_13 first, then ISBN_10; neither present leaves
  `isbn` NULL.
- `imageLinks.thumbnail` becomes `cover_url` when present.

## Configuration

`config.Load` gains `GoogleBooksAPIKey string`, read from `GOOGLE_BOOKS_API_KEY`. It is optional:
when set it is appended as `&key=`, when absent the client calls Google keyless and the server
starts normally. This deliberately departs from `JWT_SECRET`'s fail-fast rule — a missing key
degrades quota, it does not create a security hole.

The HTTP client uses a 5-second timeout, and the request's context is propagated so a client
disconnect cancels the upstream call.

## Error handling

All responses use the existing `{"error": "..."}` shape via `respondError`.

| Condition | Status |
|---|---|
| Blank `q`, invalid ISBN checksum | 400 |
| Missing/invalid bearer token | 401 |
| No volume matches an ISBN | 404 |
| Google returns 429 | 429 |
| Google returns 5xx, times out, or sends an unparseable body | 502 |

Upstream detail is logged, never returned: a Google error body can echo the request URL, and the
request URL carries the API key.

## Testing

Test-first throughout, matching Milestone 2.

- **Pure units:** ISBN normalization and checksum validation (valid ISBN-10, valid ISBN-13,
  `X` check digit, wrong checksum, wrong length); volume JSON → `Book` mapping, including missing
  title, missing authors, missing ISBN, and ISBN_10-only.
- **Provider:** against an `httptest.Server` with fixture JSON — a normal search, an empty result
  set, a 429, a 500, and a malformed body. One test asserts the API key is sent when configured
  and omitted when not.
- **Store:** against a temp SQLite database, as in `sqlite_test.go` — upserting the same volume ID
  twice yields one row with updated fields, author replacement drops removed authors, and the
  `000005` backfill preserves an existing `books.author` value.
- **Handlers:** through the existing `helpers_test.go` harness — 401 without a token, 400 on blank
  `q`, 404 on an unknown ISBN, 502 on provider failure, and a 200 whose items carry local IDs.

## Out of scope

Rate limiting of our own endpoints, a `search_cache` table that would let a repeated query skip
Google entirely, and any second provider implementation. The `Provider` interface leaves room for
the last of these without committing to it now.
