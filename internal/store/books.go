package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kate/book-tracking/internal/books"
)

// UpsertBooks writes each book keyed by its provider volume ID and returns the
// input with local IDs filled in, in the same order. One transaction covers the
// whole batch, so a failure part way through a search result leaves no
// half-written books behind.
func (s *SQLite) UpsertBooks(ctx context.Context, in []books.Book) ([]books.Book, error) {
	if len(in) == 0 {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("upsert books: %w", err)
	}
	defer tx.Rollback()

	out := make([]books.Book, 0, len(in))
	for _, b := range in {
		id, err := upsertBook(ctx, tx, b)
		if err != nil {
			return nil, err
		}
		if err := replaceAuthors(ctx, tx, id, b.Authors); err != nil {
			return nil, err
		}
		b.ID = id
		out = append(out, b)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("upsert books: %w", err)
	}
	return out, nil
}

func upsertBook(ctx context.Context, tx *sql.Tx, b books.Book) (int64, error) {
	const q = `INSERT INTO books (external_id, isbn, title, language, cover_url, metadata_source)
	           VALUES (?, ?, ?, ?, ?, ?)
	           ON CONFLICT(external_id) DO UPDATE SET
	               isbn            = excluded.isbn,
	               title           = excluded.title,
	               language        = excluded.language,
	               cover_url       = excluded.cover_url,
	               metadata_source = excluded.metadata_source
	           RETURNING id`

	var id int64
	err := tx.QueryRowContext(ctx, q,
		b.ExternalID, nullIfEmpty(b.ISBN), b.Title,
		nullIfEmpty(b.Language), nullIfEmpty(b.CoverURL), b.Source,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert book %q: %w", b.ExternalID, err)
	}
	return id, nil
}

// replaceAuthors rewrites a book's author list wholesale. Merging would keep an
// author the provider has since corrected away, and the lists are small enough
// that a delete-and-insert costs nothing.
func replaceAuthors(ctx context.Context, tx *sql.Tx, bookID int64, authors []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM book_authors WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("clear authors for book %d: %w", bookID, err)
	}

	const q = `INSERT INTO book_authors (book_id, name, position) VALUES (?, ?, ?)`
	for i, name := range authors {
		if _, err := tx.ExecContext(ctx, q, bookID, name, i); err != nil {
			return fmt.Errorf("insert author for book %d: %w", bookID, err)
		}
	}
	return nil
}

// nullIfEmpty writes NULL for an absent optional field, so "unknown" has one
// representation in the database rather than two.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
