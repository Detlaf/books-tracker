# Bookish — web frontend

Vue 3 + Vite + Vue Router + Pinia. Implements the design prototype
`Book Tracker.dc.html` from the Claude Design project *Book tracking web app
prototype* (`072992ee-2efb-40ea-9d6f-50a4c24d64dd`).

## Running

The frontend talks to the Go API in the repository root. Start that first:

```sh
# from the repository root
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/server        # listens on :8080
```

Then:

```sh
cd web
npm install
npm run dev                # http://localhost:5173
```

`vite.config.js` proxies `/auth`, `/books`, `/library`, `/me` and `/health` to
`localhost:8080`, so no CORS setup is needed in development. For a deployment
that serves the two from different hosts, set `VITE_API_BASE` to the API's
origin.

Book search calls Google Books through the backend. Without
`GOOGLE_BOOKS_API_KEY` set on the server the provider is called keyless, which
is rate-limited per IP — a `429` on the Search tab usually means that, not a
bug.

```sh
npm run build              # production bundle into dist/
npm test                   # vitest
```

## Layout

| Path | What lives there |
| --- | --- |
| `src/styles/design-system.css` | The "Classical" design system, ported verbatim. Source of truth for tokens. |
| `src/styles/app.css` | App shell and the classes lifted out of the prototype's repeated inline styles. |
| `src/api/` | Thin fetch wrappers. `client.js` holds tokens, refreshes on 401, replays once. |
| `src/stores/` | Pinia stores. `library` is server-backed; `ratings`, `collections` and `settings` are not (see below). |
| `src/lib/` | Pure logic — statistics, import parsing, status vocabulary, cover placeholders. Where the tests are. |
| `src/views/` | One per screen in the design, plus the collection detail screen. |

## Status vocabulary

The design says **To Read / Reading / Read**. The API says
**`backlog` / `reading` / `read`** (`library.ParseStatus` rejects anything
else). The API vocabulary is used throughout the code; `src/lib/status.js` is
the only place the design's labels are attached to it.

## What is not backed by the API yet

Three areas of the design have no endpoints behind them. They work, but only in
the browser they were used in — `localStorage`, namespaced per user id.

| Feature | Blocked on | Store |
| --- | --- | --- |
| 1–5 star ratings | Backend Milestone 5 (`PUT/DELETE /library/:book_id/rating`) | `stores/ratings.js` |
| Collections | Backend Milestone 6 (`/collections`, `/collections/:id/books`) | `stores/collections.js` |
| Display name, annual goal | No endpoint specified in any milestone | `stores/settings.js` |

Each store is a single module whose body is the whole migration: swap the
`localStorage` reads for API calls and nothing outside it changes. The
`ratings` and `collections` tables already exist from migration `000001`.

Two smaller gaps:

- **Reports** is computed on the client from `GET /library` (`src/lib/reports.js`),
  because Milestone 7's `/stats/*` endpoints do not exist. Every figure the
  design shows is derivable from the library list, so the screen is complete —
  it just does the arithmetic locally. It follows M7's stated rule that read
  books without a `finished_at` count toward totals but cannot be placed in a
  year, and reports that excluded count so the charts reconcile.
- **Manual book entry** (the prototype's "no catalog match for that ISBN" form)
  is not implemented. Books only enter the database through the metadata
  provider — `/books/search` and `/books/isbn/:isbn` upsert them and
  `POST /library` requires an id from one of those — so there is no endpoint a
  manual form could submit to. The Search tab explains this in place of the
  form.

## Deviations from the prototype

- **Registration.** The prototype accepted any credentials. The card also
  registers, since an account must exist against the real API, and
  `roadmap.md` Phase 2 lists login *and* register.
- **Real cover art.** The API returns `cover_url`; it is used when present, and
  the prototype's tinted letter tile is the fallback. Tile colors are hashed
  from the book id rather than taken from catalog position, so a book keeps its
  color everywhere it appears.
- **Search is debounced** (350 ms) and guarded against out-of-order responses.
  Every keystroke would otherwise spend provider quota.
- **Collections can be added to by clicking `+`**, not only by dragging. The
  design's drag-and-drop is mouse-only.
- **Routes instead of tabs.** Each screen has a URL, so the back button and
  deep links work.
