# Milestone 7 — Reporting & Statistics

## Goal

Move the Reports screen's arithmetic from the browser to the API. `web/src/lib/reports.js`
already defines the exact figures the screen needs — it was written against `GET /library`
as a stopgap, with a comment saying so. This milestone gives it real endpoints to call
instead, and nothing about the screen's behavior changes.

`specs/backend/implementation.md` already commits this milestone to four endpoints
(`summary`, `by-year`, `by-language`, `top-authors`) and to the rule that a `read` book with
no `finished_at` counts toward totals but cannot be placed in a year. This spec keeps both,
and adds two endpoints the written spec didn't anticipate — `by-month` and `streak` — because
the frontend already computes them and there's no reason to leave two of six report figures
half-migrated.

## Surface

All six routes hang off the existing `authed` group, scoped by `userID(c)` the same way
`/library` and `/collections` are — a caller only ever sees counts and rankings over their
own books.

| Route | Query params | Success |
| --- | --- | --- |
| `GET /stats/summary` | — | `200` summary object |
| `GET /stats/by-year` | — | `200` `{years, undated}` |
| `GET /stats/by-month` | `?year=` (default: current UTC year) | `200` `{year, months}` |
| `GET /stats/by-language` | `?scope=` (default `all`) | `200` `{languages}` |
| `GET /stats/top-authors` | `?scope=&limit=` (default `all`, `5`) | `200` `{authors}` |
| `GET /stats/streak` | — | `200` `{months}` |

`scope` is either the literal string `all` or a four-digit year (`^\d{4}$`); anything else is
a `400`. A bad `year` on `by-month` is the same `400`. There is no `404` case anywhere in this
milestone — every route aggregates over whatever the caller has, including zero books, and
zero is a valid answer, not a missing resource.

### `GET /stats/summary`

```json
{
  "total_read": 42,
  "reading": 3,
  "backlog": 11,
  "this_year": 12,
  "last_year": 9,
  "current_year": 2026,
  "undated_read": 2
}
```

`current_year` is the server's UTC year, echoed back so the client never has to reconcile its
own clock against the server's idea of "this year" — the same reasoning `POST /library`
already applies by rejecting a future `finished_at` in UTC. `this_year` / `last_year` count
`status = 'read'` rows whose `finished_at` falls in that UTC year. `undated_read` counts
`status = 'read'` rows with `finished_at IS NULL` — pre-migration rows that couldn't be
backfilled, or books marked read without a date.

Percent-of-goal, the year-over-year delta label, and the reading goal itself stay client-side
in `stores/settings.js`: a goal has no backend concept and the delta is a formatting choice
over two numbers this endpoint already provides.

### `GET /stats/by-year`

```json
{ "years": [{ "year": "2024", "count": 8 }, { "year": "2025", "count": 15 }], "undated": 2 }
```

`years` is grouped by `strftime('%Y', finished_at)`, ascending, and only ever includes years
with at least one dated `read` book — there is no zero-filling across a gap, matching how the
client-side version only ever plots years it has data for. `undated` is the same count as
`summary.undated_read`, repeated here because the spec requires the by-year response to carry
its own excluded count so a reader of this endpoint alone can reconcile
`sum(years[].count) + undated` against a total they'd get from `/stats/summary`.

`year` is a string, not an int, matching how `finished_at`'s first four characters are already
treated as an opaque label throughout `reports.js` (`entry.finished_at?.slice(0, 4)`) rather
than a number to do arithmetic on.

The scope selector on the Reports screen (the row of year pills) is populated from this
response's `years` list rather than a seventh endpoint — it's the same set of "years with at
least one dated read book" either way.

### `GET /stats/by-month`

```json
{ "year": "2026", "months": [{ "month": 1, "count": 2 }, { "month": 2, "count": 0 }, ...] }
```

Always exactly 12 entries, zero-filled, `month` 1–12 — unlike `by-year`, the chart needs every
month present to draw a continuous axis. `year` defaults to the server's current UTC year when
omitted; the Reports screen always has `summary.current_year` in hand by the time it calls
this, but the default keeps the endpoint usable on its own. Month labels (`Jan`, `Feb`, ...)
stay a frontend concern in `reports.js`'s `MONTH_ABBREVS`, same as today.

### `GET /stats/by-language`

```json
{ "languages": [{ "language": "English", "count": 30 }, { "language": "Unknown", "count": 2 }] }
```

Grouped over `status = 'read'` rows within `scope`, `COALESCE(books.language, '')` mapped to
the literal string `"Unknown"` — the client already treats a falsy language this way
(`e.book.language || 'Unknown'`); moving that into SQL keeps one book from being "English" in
one report and "Unknown" in another because a filter path handled the empty string
differently. Sorted by count descending, then `language` ascending as a deterministic tiebreak
(the client-side version's tie order depended on `Map` insertion order, which was never a
real guarantee).

`pct` (bar width as a percentage of the top language) is a presentation value derived from
`count / max(counts)` and stays client-side, same as `barHeight` on the year and month charts.

### `GET /stats/top-authors`

```json
{ "authors": [{ "author": "Frank Herbert", "count": 3 }, { "author": "Ann Leckie", "count": 1 }] }
```

Grouped over `book_authors.name` joined through `book_authors.book_id` for `status = 'read'`
rows within `scope` — a book with three authors counts once for each of them, per
`specs/backend/implementation.md`'s note that Milestone 3 normalized authors into their own
table for exactly this report. Sorted by count descending, then `author` ascending, matching
the client-side tiebreak in `reports.js.topAuthors` exactly. `limit` reuses `positiveQuery`
from `book_handlers.go`, defaulting to 5 and capped at 50 — there's no upstream quota to
protect here, but an unbounded `limit` still shouldn't return someone's entire author list in
one response. `initial` (the avatar letter) stays client-side.

### `GET /stats/streak`

```json
{ "months": 4 }
```

Consecutive calendar months, counting back from the last *completed* month, with at least one
`read` book finished in that month. The in-progress month is never counted — it isn't over,
so a zero there isn't a broken streak — matching `reports.js.currentStreak` exactly, including
its 240-month bound against a runaway loop on bad data.

## Structure

A new `internal/stats` package, matching `internal/collections`'s shape: a pure `Service`
over a `Store` interface, so scope/year parsing and limit clamping are unit-testable without a
database.

```text
internal/stats/
  stats.go          Summary, YearCount, MonthCount, LanguageCount, AuthorCount
  errors.go         ErrInvalidScope, ErrInvalidYear
  store.go          the interface Service depends on
  service.go        scope/year parsing, limit clamping, delegation
  service_test.go   against a fake store
internal/store/stats.go        SQL aggregation; implements stats.Store
internal/server/stats_handlers.go
```

Unlike `library` and `collections`, `Service` here has no state-machine or CRUD logic — every
method is a thin validate-then-delegate. `Store`'s six methods return the SQL-aggregated
result directly:

```go
type Store interface {
    Summary(ctx context.Context, userID int64, now time.Time) (Summary, error)
    ByYear(ctx context.Context, userID int64) ([]YearCount, int, error) // int is undated count
    ByMonth(ctx context.Context, userID int64, year string) ([]MonthCount, error)
    ByLanguage(ctx context.Context, userID int64, scope string) ([]LanguageCount, error)
    TopAuthors(ctx context.Context, userID int64, scope string, limit int) ([]AuthorCount, error)
    Streak(ctx context.Context, userID int64, now time.Time) (int, error)
}
```

`now` is threaded through from the handler (`time.Now().UTC()`) rather than called inside the
store, the same reasoning `library.Service`'s clock injection already uses — `summary`'s
year math and `streak`'s "last completed month" are both boundary-sensitive and need to be
testable without sleeping past a month rollover.

`errors.go`: `ErrInvalidScope` (scope is neither `all` nor `^\d{4}$`), `ErrInvalidYear` (same
shape check, on `by-month`'s `year`). A `respondStatsError` in the handler file maps both to
`400`, following `respondLibraryError`'s pattern; anything unrecognized is a logged `500`.

## Schema

No migration. Every field comes from `books`, `book_authors`, and `user_books`, all present
since migrations `000001`–`000005`. `idx_user_books_finished_at` (`user_id, finished_at`)
already covers the grouping this milestone does most: `summary`, `by-year`, `by-month`, and
`streak` all filter `user_books` by `user_id` and read `finished_at`. `by-language` and
`top-authors` add a join to `books` (and `book_authors` for the latter) but still start from
that same indexed scan.

## Response shape notes

Every response is a single JSON object (not a bare array) even where the payload is
essentially one list (`by-year`, `by-language`, `top-authors`) — wrapping it (`{"years": [...]}`
etc.) leaves room for `by-year`'s `undated` sibling field without a shape change, and matches
how `GET /library` wraps `items` in `{items, page, limit}` rather than returning a bare array.
Empty results are `[]`, never `null`, same guarantee `authors` and `GET /library`'s `items`
already make.

## Frontend wiring

- `src/api/stats.js` — six thin fetch wrappers, one per endpoint, following `src/api/`'s
  existing style (a plain function per call, no class).
- `src/stores/stats.js` — a Pinia store that fetches all six on `load()` and exposes them as
  state, following `stores/library.js`'s async-loaded shape. `scope` and the by-month `year`
  are store-level refs that trigger a re-fetch of only the scoped endpoints
  (`by-language`, `top-authors`) or the year-dependent one (`by-month`) — not a full reload.
- `src/lib/reports.js` shrinks to just the pure presentation helpers that take the API's shape
  as input: `barHeight`/`barColor` for the year and month charts, `pct` for language bars,
  `initial` for author avatars, and `yoyLabel` from `this_year`/`last_year`. The data-shaping
  functions it replaces (`summary`, `byYear`, `byMonth`, `byLanguage`, `topAuthors`,
  `currentStreak`, `readEntries`, `datedReadEntries`, `scopedRead`, `tally`) are deleted, not
  kept as dead code — `library.entries` is no longer read by this screen at all.
- `ReportsView.vue` switches from `library.entries` + local computation to `stats` store state
  plus the surviving presentation helpers. `availableYears` becomes `stats.byYear.years.map(y
  => y.year)` instead of a `reports.js` function.
- `web/README.md`'s "What is not backed by the API yet" table loses its Reports row and
  gap-2 bullet, the same way Milestone 6 dropped the Collections row.

## Testing

- `stats/service_test.go` — scope/year validation against a fake store: `all`, a valid year,
  an empty string, `"20xx"`, a 5-digit year, negative/zero/huge `limit` clamping.
- `store/stats_test.go` — against a real temp SQLite database, as `store/collections_test.go`
  does: seeded users and books across statuses and years, verifying each aggregation,
  cross-user isolation (a second user's books never affect the first user's counts),
  undated `read` books counted in `summary`/`by-language`/`top-authors` but excluded from
  `by-year`'s `years` and counted in its `undated`, a co-authored book counted once per
  author, and `streak` across a real month boundary using an injected `now`.
- `server/stats_test.go` — `401` without a token on all six routes, `400` on bad `scope`/
  `year`, correct shapes and status codes, and the cross-user scoping assertion.
- Frontend: Vitest on the surviving `reports.js` helpers with fixtures shaped like the new API
  responses, and on `stores/stats.js`'s fetch/re-fetch-on-scope-change behavior.

## Out of scope

Any endpoint that returns per-book detail (e.g. "which books did I read in March") — every
route here returns an aggregate, never a list of entries; that's what `GET /library`'s
existing filters are for. Caching or pre-aggregating stats server-side — the query volume at
this app's scale doesn't warrant it, and it can be added later without changing the response
shapes. Exposing `by-year`/`by-language`/`top-authors` for `reading` or `backlog` statuses —
the design prototype and the written spec both frame every report around books *read*.
