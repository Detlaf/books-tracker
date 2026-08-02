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

- Project scaffold (Vite + Vue 3 + Vue Router + Pinia)
- Book search UI: title, author, ISBN input
- Book detail page with status selector and rating widget
- Reading list views: backlog, currently reading, read
- Collections management UI
- Reporting/statistics page:
  - Books read per year
  - Breakdown by language
  - Most-read authors
- Auth pages (login / register)

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
