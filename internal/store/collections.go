package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/kate/book-tracking/internal/collections"
)

func (s *SQLite) Create(ctx context.Context, userID int64, name string) (collections.Collection, error) {
	const q = `INSERT INTO collections (user_id, name, created_at) VALUES (?, ?, ?)`

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, q, userID, name, now)
	if err != nil {
		return collections.Collection{}, fmt.Errorf("create collection: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return collections.Collection{}, fmt.Errorf("create collection: %w", err)
	}
	return collections.Collection{
		ID: id, UserID: userID, Name: name, BookIDs: []int64{}, CreatedAt: now,
	}, nil
}

func (s *SQLite) List(ctx context.Context, userID int64) ([]collections.Collection, error) {
	const q = `SELECT id, user_id, name, created_at FROM collections
	           WHERE user_id = ? ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()

	var out []collections.Collection
	var ids []int64
	for rows.Next() {
		var c collections.Collection
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("list collections: %w", err)
		}
		c.CreatedAt = c.CreatedAt.UTC()
		out = append(out, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}

	books, err := s.booksFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].BookIDs = booksOrEmpty(books[out[i].ID])
	}
	return out, nil
}

// get loads one collection scoped to userID, including its books. It is the
// shared "mutate, then re-read" tail for Rename, AddBook, and RemoveBook.
func (s *SQLite) get(ctx context.Context, userID, id int64) (collections.Collection, error) {
	const q = `SELECT id, user_id, name, created_at FROM collections
	           WHERE id = ? AND user_id = ?`

	var c collections.Collection
	row := s.db.QueryRowContext(ctx, q, id, userID)
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return collections.Collection{}, collections.ErrNotFound
	}
	if err != nil {
		return collections.Collection{}, fmt.Errorf("get collection: %w", err)
	}
	c.CreatedAt = c.CreatedAt.UTC()

	books, err := s.booksFor(ctx, []int64{c.ID})
	if err != nil {
		return collections.Collection{}, err
	}
	c.BookIDs = booksOrEmpty(books[c.ID])
	return c, nil
}

func (s *SQLite) Rename(ctx context.Context, userID, id int64, name string) (collections.Collection, error) {
	const q = `UPDATE collections SET name = ? WHERE id = ? AND user_id = ?`

	res, err := s.db.ExecContext(ctx, q, name, id, userID)
	if err != nil {
		return collections.Collection{}, fmt.Errorf("rename collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return collections.Collection{}, fmt.Errorf("rename collection: %w", err)
	}
	if n == 0 {
		return collections.Collection{}, collections.ErrNotFound
	}
	return s.get(ctx, userID, id)
}

func (s *SQLite) Delete(ctx context.Context, userID, id int64) error {
	const q = `DELETE FROM collections WHERE id = ? AND user_id = ?`

	res, err := s.db.ExecContext(ctx, q, id, userID)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if n == 0 {
		return collections.ErrNotFound
	}
	return nil
}

// AddBook is INSERT OR IGNORE: adding a book already in the collection is a
// no-op, not an error, and the response always reflects the current state.
func (s *SQLite) AddBook(ctx context.Context, userID, id, bookID int64) (collections.Collection, error) {
	if _, err := s.get(ctx, userID, id); err != nil {
		return collections.Collection{}, err
	}

	const q = `INSERT OR IGNORE INTO collection_books (collection_id, book_id) VALUES (?, ?)`
	if _, err := s.db.ExecContext(ctx, q, id, bookID); err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintForeignKey {
			return collections.Collection{}, collections.ErrUnknownBook
		}
		return collections.Collection{}, fmt.Errorf("add book to collection: %w", err)
	}
	return s.get(ctx, userID, id)
}

func (s *SQLite) RemoveBook(ctx context.Context, userID, id, bookID int64) error {
	if _, err := s.get(ctx, userID, id); err != nil {
		return err
	}

	const q = `DELETE FROM collection_books WHERE collection_id = ? AND book_id = ?`
	res, err := s.db.ExecContext(ctx, q, id, bookID)
	if err != nil {
		return fmt.Errorf("remove book from collection: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove book from collection: %w", err)
	}
	if n == 0 {
		return collections.ErrBookNotInCollection
	}
	return nil
}

// booksFor loads every member book id for a page of collections in one
// query, keyed back by collection ID in Go — the same shape as
// (*SQLite).authorsFor in library.go.
func (s *SQLite) booksFor(ctx context.Context, collectionIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(collectionIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(collectionIDs)), ",")
	q := `SELECT collection_id, book_id FROM collection_books
	      WHERE collection_id IN (` + placeholders + `)
	      ORDER BY collection_id, book_id`

	args := make([]any, 0, len(collectionIDs))
	for _, id := range collectionIDs {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load collection books: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var collectionID, bookID int64
		if err := rows.Scan(&collectionID, &bookID); err != nil {
			return nil, fmt.Errorf("load collection books: %w", err)
		}
		out[collectionID] = append(out[collectionID], bookID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load collection books: %w", err)
	}
	return out, nil
}

func booksOrEmpty(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ collections.Store = (*SQLite)(nil)
