CREATE TABLE IF NOT EXISTS book_authors (
    book_id  INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (book_id, position)
);

CREATE INDEX IF NOT EXISTS idx_book_authors_name ON book_authors(name);

INSERT INTO book_authors (book_id, name, position)
SELECT id, author, 0 FROM books WHERE author IS NOT NULL AND author <> '';

ALTER TABLE books DROP COLUMN author;

ALTER TABLE books ADD COLUMN external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_books_external_id ON books(external_id);
