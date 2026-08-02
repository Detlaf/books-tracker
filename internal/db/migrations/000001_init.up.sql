CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS books (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    isbn            TEXT,
    title           TEXT    NOT NULL,
    author          TEXT    NOT NULL,
    language        TEXT,
    cover_url       TEXT,
    metadata_source TEXT
);

CREATE TABLE IF NOT EXISTS user_books (
    user_id    INTEGER  NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    book_id    INTEGER  NOT NULL REFERENCES books(id)  ON DELETE CASCADE,
    status     TEXT     NOT NULL CHECK(status IN ('backlog','reading','read')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id)
);

CREATE TABLE IF NOT EXISTS ratings (
    user_id    INTEGER  NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    book_id    INTEGER  NOT NULL REFERENCES books(id)  ON DELETE CASCADE,
    score      INTEGER  NOT NULL CHECK(score BETWEEN 1 AND 5),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, book_id)
);

CREATE TABLE IF NOT EXISTS collections (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS collection_books (
    collection_id INTEGER NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    book_id       INTEGER NOT NULL REFERENCES books(id)       ON DELETE CASCADE,
    PRIMARY KEY (collection_id, book_id)
);
