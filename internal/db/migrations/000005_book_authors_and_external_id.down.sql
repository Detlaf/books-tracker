DROP INDEX IF EXISTS idx_books_external_id;

ALTER TABLE books DROP COLUMN external_id;

-- SQLite cannot add a NOT NULL column without a default, so the restored
-- column carries one; the original had none. Books with several authors keep
-- only the first, which is the information the single column can hold.
ALTER TABLE books ADD COLUMN author TEXT NOT NULL DEFAULT '';

UPDATE books SET author = COALESCE(
    (SELECT name FROM book_authors ba WHERE ba.book_id = books.id AND ba.position = 0), '');

DROP INDEX IF EXISTS idx_book_authors_name;

DROP TABLE book_authors;
