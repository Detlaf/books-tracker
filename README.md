# Book Tracker

Track what you're reading, what you've read, and what's still on the pile.
Search by title, author or ISBN; set a reading status; rate what you finish;
group books into collections; and see statistics on your reading by year,
language and author.

Requirements are in [`product.md`](product.md), the stack is fixed by
[`tech-stack.md`](tech-stack.md), and progress is tracked in
[`roadmap.md`](roadmap.md).

## What exists today

| Component | Stack | Status |
| --- | --- | --- |
| Backend | Go + SQLite | Auth, book search, reading status, ratings, collections, statistics. |
| Web frontend | Vue 3 + Vite | All screens implemented. See [`web/README.md`](web/README.md). |
| Android app | Kotlin | Not started. |

## Running locally

You need **Go 1.25+** and, for the frontend, **Node 20+**.

### 1. Backend

The database is created on first run and migrations are embedded in the binary,
so there is no separate setup or migrate step.

```sh
export JWT_SECRET="$(openssl rand -base64 32)"
go run ./cmd/server
```

It listens on `:8080` and writes to `book_tracking.db` in the working
directory. Check it:

```sh
curl localhost:8080/health     # {"status":"ok"}
```

### 2. Frontend

In a second terminal:

```sh
cd web
npm install
npm run dev
```

Open **<http://localhost:5173>** and create an account. Vite proxies API routes
to `localhost:8080`, so both halves run on their own ports with no CORS setup.

### Configuration

Set on the backend process.

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `JWT_SECRET` | **yes** | — | At least 32 bytes. The server refuses to start without it rather than falling back to a generated key. |
| `DATABASE_URL` | no | `book_tracking.db` | SQLite file path. |
| `ADDR` | no | `:8080` | Listen address. |
| `GOOGLE_BOOKS_API_KEY` | no | — | Without it, Google Books is called keyless, which is rate-limited per IP. |

The frontend reads `VITE_API_BASE` only when the API is served from a different
origin than the app; in development the proxy handles it.

> **If book search returns "provider rate limit exceeded"**, that is Google
> rate-limiting keyless requests from your IP, not a bug. Set
> `GOOGLE_BOOKS_API_KEY`.

## Tests

```sh
go test ./...            # backend
cd web && npm test       # frontend
```

## API

All routes except `/health` and `/auth/*` require an
`Authorization: Bearer <access_token>` header. Access tokens are short-lived;
`/auth/refresh` exchanges a refresh token for a new pair.

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/health` | Liveness check |
| `POST` | `/auth/register` | Create an account |
| `POST` | `/auth/login` | Exchange credentials for a token pair |
| `POST` | `/auth/refresh` | Rotate the token pair |
| `POST` | `/auth/logout` | Revoke a refresh token |
| `GET` | `/me` | Caller's identity |
| `GET` | `/books/search?q=` | Search the metadata provider |
| `GET` | `/books/isbn/:isbn` | Look up one book by ISBN |
| `GET` | `/library` | List your books, with `status`, `sort`, `page`, `limit` |
| `POST` | `/library` | Add a book at a status |
| `PATCH` | `/library/:book_id` | Change status or finish date |
| `DELETE` | `/library/:book_id` | Remove a book |
| `PUT` | `/library/:book_id/rating` | Set a 1–5 rating on a book |
| `DELETE` | `/library/:book_id/rating` | Clear a book's rating |
| `GET` | `/collections` | List your collections |
| `POST` | `/collections` | Create a collection |
| `PATCH` | `/collections/:id` | Rename a collection |
| `DELETE` | `/collections/:id` | Delete a collection |
| `POST` | `/collections/:id/books` | Add a book to a collection |
| `DELETE` | `/collections/:id/books/:book_id` | Remove a book from a collection |
| `GET` | `/stats/summary` | Total read, this/last year, current streak |
| `GET` | `/stats/by-year` | Books read grouped by year |
| `GET` | `/stats/by-month?year=` | Books read grouped by month within a year |
| `GET` | `/stats/by-language?scope=` | Books read grouped by language, `scope` is `all` or a year |
| `GET` | `/stats/top-authors?scope=&limit=` | Most-read authors, `scope` is `all` or a year |
| `GET` | `/stats/streak` | Consecutive months with a finished book |

Reading statuses are `backlog`, `reading` and `read`. The web UI labels the
first one "To Read".

Books only enter the database through the metadata provider: `/books/search`
and `/books/isbn/:isbn` upsert what they find and return a local `id`, and
`POST /library` requires an id that came from one of those. There is no
endpoint for creating a book by hand.

All milestones in [`specs/backend/implementation.md`](specs/backend/implementation.md)
are now implemented, including statistics. The Reports screen in the web app
is wired to the `/stats/*` endpoints above — see [`web/README.md`](web/README.md).

## Layout

```text
cmd/server/        entrypoint
internal/
  auth/            passwords, JWTs, refresh tokens
  books/           metadata provider (Google Books), ISBN handling
  library/         reading status rules, incl. finished_at transitions, and ratings
  collections/     collection CRUD and book membership
  stats/           reading statistics: summary, by-year/month, by-language, top authors, streak
  server/          HTTP handlers, routing, middleware
  store/           SQLite persistence
  db/              connection + embedded migrations
  config/          environment configuration
web/               Vue 3 frontend
specs/             backend implementation plan, by milestone
docs/              design docs per feature
```
