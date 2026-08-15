# Milestone 4 — Reading Status

## Goal

Let an authenticated user keep a library: add a book, move it between `backlog`,
`reading`, and `read`, and remove it. Milestone 3 made every search result carry a local
`book_id`; this milestone is what those IDs are for.

Ratings (Milestone 5) and collections (Milestone 6) both address books through the same
`(user_id, book_id)` pair, and Milestone 7's reports read `finished_at`. So the rules
written here are the ones the rest of the backend depends on.

## Surface

All four routes hang off the existing `authed` group. Every query is scoped by
`userID(c)`: a book in another user's library is indistinguishable from one that does not
exist, and the scoping is asserted in handler tests rather than left to review.

| Route | Body / params | Success | Errors |
| --- | --- | --- | --- |
| `GET /library` | `?status=&page=&limit=&sort=` | `200` `{items, page, limit}` | `400` bad `status` or `sort` |
| `POST /library` | `{book_id, status, finished_at?}` | `201` entry | `400`, `404` unknown book, `409` already present |
| `PATCH /library/:book_id` | `{status?, finished_at?}` | `200` entry | `400`, `404` not in library |
| `DELETE /library/:book_id` | — | `204` | `404` not in library |

### `GET /library`

`status` filters to one of the three statuses; omitted means all. An unrecognized value is
a `400` rather than an empty list, because a typo that returns `200 []` reads as "you have
no books" and sends the client looking in the wrong place.

`page` and `limit` reuse `positiveQuery` and the 1-based convention from `/books/search`.
`limit` defaults to 20 and is capped at 100. Google's ceiling of 40 does not apply here:
this is a local join, not a quota-metered upstream call.

`sort` accepts `added_at`, `finished_at`, or `title`, each with an optional `-` prefix for
descending. Anything else is a `400`. The default is `-added_at`. The value is resolved
through an allowlist to a fixed `ORDER BY` fragment — it is never interpolated into SQL.
Rows with a NULL `finished_at` sort last under both directions of `finished_at`, so an
unfinished book never displaces a finished one at the top of the list.

### `POST /library`

The body identifies the book by local `book_id` only. The client is expected to have hit
`/books/search` or `/books/isbn/:isbn` first, both of which upsert the book and return its
ID. This keeps provider calls, API-quota spend, and 502/429 failure modes off the library
write path entirely.

An unknown `book_id` is a `404`. A book already in the caller's library is a `409`, not an
upsert: `POST` means add and `PATCH` means change, and a silent upsert lets a stale client
reset a deliberately-set `read` back to `backlog`. This mirrors the `409` that
`/auth/register` already returns for a duplicate email.

`status` is required. Defaulting it would make the most common mistake — omitting it —
invisible.

### `DELETE /library/:book_id`

Returns `404` when the row is absent rather than a silent `204`. This is a user-initiated
removal from a list the user is looking at, so a no-op is more likely a bug than a retry.
Deleting an entry does not delete the `books` row, which is shared across users.

## The `finished_at` rules

This is the only logic in the milestone that is not CRUD. It lives in `library.Service` as
a single function over `(current entry, requested status, requested finished_at)`, so it
can be exhaustively unit-tested without a database or an HTTP request.

| Transition | Result |
| --- | --- |
| → `read`, no date supplied, none present | set to now (UTC) |
| → `read`, no date supplied, one already present | left alone |
| → `read`, explicit date supplied | use the supplied date |
| `read` → `backlog` or `reading` | cleared to NULL |
| status unchanged, explicit date supplied | use it, only when status is `read` |

Leaving an existing date alone on a repeat transition to `read` means re-marking a book
read does not silently rewrite the date a user set. Supplying an explicit date is how a
user backdates a book finished before they started using the app.

Rejected with `400`:

- a `finished_at` in the future, as `ErrFutureFinishedAt`
- a `finished_at` paired with a non-`read` status, as `ErrFinishedAtNotRead`
- a `PATCH` body with neither `status` nor `finished_at`, as `ErrEmptyUpdate` — always a
  client bug, never a meaningful no-op

Both timestamps are parsed and emitted as RFC 3339 UTC. "Now" is injected into the service
as a clock function so the boundary cases are testable without sleeping.

## Structure

A new `internal/library` package, matching how `internal/auth` and `internal/books` are
already laid out and wired in `server.New`.

```text
internal/library/
  library.go        Entry, Status, sort keys
  errors.go         typed sentinels
  store.go          the interface Service depends on
  service.go        transition rules, validation
  service_test.go   against a fake store
internal/store/library.go        SQL; implements library.Store
internal/server/library_handlers.go
```

`Status` is a defined string type with `ParseStatus` returning `ErrInvalidStatus`. The
`CHECK` constraint from migration `000001` stays as a backstop, not the primary validation
— a constraint violation surfacing as a 500 is not an error message.

Sentinels in `errors.go`: `ErrUnknownBook`, `ErrAlreadyInLibrary`, `ErrNotInLibrary`,
`ErrInvalidStatus`, `ErrFutureFinishedAt`, `ErrFinishedAtNotRead`, `ErrEmptyUpdate`,
`ErrInvalidSort`. A `respondLibraryError` in the handler file maps them to status codes by
`errors.Is`, following `respondBooksError`; anything unrecognized is a logged `500`.

## Schema

No new tables. `user_books` already carries the status `CHECK` from `000001`,
`finished_at` from `000002`, and `created_at` / `updated_at`. `created_at` is what the API
exposes as `added_at`.

One new migration, `000006`:

```sql
CREATE INDEX IF NOT EXISTS idx_user_books_user_status
    ON user_books(user_id, status);
```

`GET /library?status=reading` has no supporting index today —
`idx_user_books_finished_at` covers `(user_id, finished_at)` only. Without this, the
application's most-hit endpoint scans the table on every list view.

`POST` and `PATCH` set `updated_at` explicitly; the column default only covers insert.

## Response shape

```json
{
  "book": {
    "id": 42,
    "external_id": "zyTCAlFPjgYC",
    "title": "Dune",
    "authors": ["Frank Herbert"],
    "cover_url": "https://…"
  },
  "status": "reading",
  "finished_at": null,
  "added_at": "2026-08-15T12:00:00Z"
}
```

`bookResponse` and `newBookResponse` are reused unchanged from `book_handlers.go`.

`finished_at` is always present and explicitly `null` when unset, rather than `omitempty`.
A client distinguishing "not finished" from "field absent" should not have to guess.

`GET /library` wraps these as `{items, page, limit}`, with `items` an empty array rather
than `null` when the page is empty — the same guarantee `authors` already makes.

The store loads a page in two queries, not N+1: one join across `user_books` and `books`
for the entries, then one `WHERE book_id IN (…)` for authors, keyed back together in Go.
Both are bounded by `limit`, the same reasoning that made `UpsertBooks` a single batch.

## Testing

- `library/service_test.go` — the transition table, case by case, against a fake store.
  Future dates, dates on non-`read` statuses, empty updates, repeat transitions to `read`.
- `store/library_test.go` — against a real temp SQLite database, as `store/books_test.go`
  does: add, list with each filter and each sort key, NULL `finished_at` ordering, update,
  delete, and the unique-violation path behind `ErrAlreadyInLibrary`.
- `server/library_test.go` — status codes for each error, `401` without a token, and the
  cross-user scoping assertion: user A cannot read, patch, or delete user B's entry, and
  each attempt is a `404` rather than a `403`.

## Out of scope

Bulk add, moving several books at once, import from another service, and any endpoint that
resolves an ISBN or external ID directly into the library. Ratings are Milestone 5.
