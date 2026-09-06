# Implementation Roadmap

## Phase 1 — Backend Foundation

- Set up Go project structure with module layout
- Configure SQLite database with migrations
- Define core data models: Book, User, ReadingStatus, Rating, Collection
- Integrate with a book metadata API (e.g. Open Library or Google Books) for search by title/author
- Implement ISBN lookup endpoint
- REST API for reading status (backlog / reading / read)
- REST API for ratings (1–5 on read books)
- REST API for collections (create, rename, delete, add/remove books)
- Authentication (JWT-based)

## Phase 2 — Web Frontend (Vue.js)

Implemented in `web/` from the design prototype `Book Tracker.dc.html`. See
`web/README.md` for what is wired to the API and what is still browser-local.

- [x] Project scaffold (Vite + Vue 3 + Vue Router + Pinia)
- [x] Book search UI: title, author, ISBN input
- [x] Book detail page with status selector and rating widget
      (ratings are browser-local until Milestone 5)
- [x] Reading list views: backlog, currently reading, read
- [x] Collections management UI (browser-local until Milestone 6)
- [x] Reporting/statistics page — computed client-side from `GET /library`
      until Milestone 7 lands:
  - [x] Books read per year
  - [x] Breakdown by language
  - [x] Most-read authors
- [x] Auth pages (login / register)

## Phase 3 — Android App (Kotlin)

- Project scaffold (Android Studio, Jetpack Compose)
- Auth screens
- Book search screen: text search + camera-based ISBN scanner (ML Kit Barcode Scanning)
- Reading status management
- Rating UI
- Collections browser
- Statistics screen mirroring web reporting tab
- Offline support: local cache of reading list with sync on reconnect

## Phase 4 — Polish & Cross-Cutting Concerns

- API pagination and error handling hardening
- Input validation on backend and both clients
- Unit and integration tests (Go test, Vitest, JUnit/Espresso)
- CI pipeline (lint, test, build for all three targets)
- Deployment: containerize backend, serve frontend as static assets, publish Android APK/AAB
