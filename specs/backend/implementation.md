# Backend Implementation Plan

## Milestone 1 — Project Setup & Database

- Initialize Go module and directory structure (`cmd/`, `internal/`, `migrations/`)
- Configure SQLite with a migration runner
- Define and run initial schema migrations:
  - `books` (id, isbn, title, author, language, cover_url, metadata_source)
  - `user_books` (user_id, book_id, status, finished_at, created_at, updated_at)
  - `ratings` (user_id, book_id, score, created_at)
  - `collections` (id, user_id, name, created_at)
  - `collection_books` (collection_id, book_id)
  - `users` (id, email, password_hash, created_at)
- `finished_at` (nullable `DATETIME` on `user_books`) records when a book was finished; it is the
  grouping key for the by-year report in Milestone 7. Added in migration `000002`, which backfills
  existing `read` rows from `updated_at` and indexes `(user_id, finished_at)`.

## Milestone 2 — Authentication

- `POST /auth/register` — email + password, bcrypt (cost 12), 409 on duplicate
- `POST /auth/login` — returns a 15-minute access JWT and a 30-day refresh token
- `POST /auth/refresh` — rotates the refresh token. The rotation is decided by an
  atomic revoke, so two requests racing on one live token cannot both be served;
  replaying a *rotated* token revokes every token for that user
- `POST /auth/logout` — revokes one refresh token; idempotent. Replaying a
  logged-out token is refused on its own and does **not** revoke the family,
  so a lost response cannot log a user out everywhere
- `GET /me` — returns the caller's ID; first consumer of the protected pattern
- `RequireAuth` middleware applied via the `authed` route group, with `userID(c)`
  for handlers. Milestones 4–7 hang their routes off this group.
- `JWT_SECRET` is required at startup, minimum 32 bytes after trimming surrounding
  whitespace, no default
- Design: `docs/superpowers/specs/2026-08-02-auth-design.md`

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

## Milestone 4 — Reading Status

- `GET /library` — list the authenticated user's books with their statuses and `finished_at`
- `POST /library` — add a book to the user's library with an initial status
- `PATCH /library/:book_id` — update status (backlog / reading / read)
- `finished_at` maintenance on status transitions:
  - → `read`: set `finished_at` to now, unless the request supplies an explicit date (lets users
    backdate books they finished before adding them) or a value is already present
  - `read` → `backlog` / `reading`: clear `finished_at` back to NULL
  - Reject an explicit `finished_at` in the future, or one paired with a non-`read` status
- `DELETE /library/:book_id` — remove a book from the library

## Milestone 5 — Ratings

- `PUT /library/:book_id/rating` — set or update a rating (1–5, only allowed when status is `read`)
- `DELETE /library/:book_id/rating` — remove a rating
- Validation: reject rating if book status is not `read`

## Milestone 6 — Collections

- `GET /collections` — list user's collections
- `POST /collections` — create a collection
- `PATCH /collections/:id` — rename a collection
- `DELETE /collections/:id` — delete a collection
- `POST /collections/:id/books` — add a book to a collection
- `DELETE /collections/:id/books/:book_id` — remove a book from a collection

## Milestone 7 — Reporting & Statistics

- `GET /stats/summary` — total books read, reading, in backlog
- `GET /stats/by-year` — books read grouped by `strftime('%Y', finished_at)`
- `GET /stats/by-language` — books read grouped by language
- `GET /stats/top-authors` — authors ranked by number of books read
- All stats count only rows with `status = 'read'`. Rows whose `finished_at` is NULL (pre-migration
  data that could not be backfilled, or books marked read without a date) are excluded from
  `by-year` but still counted in the summary, language, and author reports; `by-year` should report
  that excluded count alongside the buckets so totals reconcile.
