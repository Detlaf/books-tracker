package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/kate/book-tracking/internal/library"
)

// entryColumns is the shared projection behind Entry and ListEntries. The
// COALESCEs turn the nullable optional columns into the empty strings
// books.Book uses, so "unknown" has one representation in Go.
const entryColumns = `b.id, b.external_id,
       COALESCE(b.isbn, ''), b.title,
       COALESCE(b.language, ''), COALESCE(b.cover_url, ''),
       COALESCE(b.metadata_source, ''),
       ub.status, ub.finished_at, ub.created_at`

// orderBy maps an already-validated Sort to a fixed fragment. The client's
// string is never interpolated: an unknown key cannot reach here, and if one
// did it would fall back to the default rather than reach SQL.
//
// "finished_at IS NULL" sorts 0 before 1 in both directions, which keeps an
// unfinished book from displacing a finished one at the top of the list. Each
// fragment ends in book_id so a page boundary is stable between requests.
var orderBy = map[library.Sort]string{
	library.SortAddedAt:        `ub.created_at ASC, ub.book_id ASC`,
	library.SortAddedAtDesc:    `ub.created_at DESC, ub.book_id DESC`,
	library.SortFinishedAt:     `ub.finished_at IS NULL, ub.finished_at ASC, ub.book_id ASC`,
	library.SortFinishedAtDesc: `ub.finished_at IS NULL, ub.finished_at DESC, ub.book_id DESC`,
	library.SortTitle:          `b.title COLLATE NOCASE ASC, ub.book_id ASC`,
	library.SortTitleDesc:      `b.title COLLATE NOCASE DESC, ub.book_id DESC`,
}

func (s *SQLite) AddEntry(ctx context.Context, userID, bookID int64, status library.Status, finishedAt *time.Time) (library.Entry, error) {
	const q = `INSERT INTO user_books (user_id, book_id, status, finished_at, created_at, updated_at)
	           VALUES (?, ?, ?, ?, ?, ?)`

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, q, userID, bookID, string(status), utcOrNil(finishedAt), now, now)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) {
			switch sqliteErr.ExtendedCode {
			case sqlite3.ErrConstraintPrimaryKey, sqlite3.ErrConstraintUnique:
				return library.Entry{}, library.ErrAlreadyInLibrary
			case sqlite3.ErrConstraintForeignKey:
				return library.Entry{}, library.ErrUnknownBook
			}
		}
		return library.Entry{}, fmt.Errorf("add library entry: %w", err)
	}
	return s.Entry(ctx, userID, bookID)
}

func (s *SQLite) Entry(ctx context.Context, userID, bookID int64) (library.Entry, error) {
	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      WHERE ub.user_id = ? AND ub.book_id = ?`

	row := s.db.QueryRowContext(ctx, q, userID, bookID)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return library.Entry{}, library.ErrNotInLibrary
	}
	if err != nil {
		return library.Entry{}, fmt.Errorf("library entry: %w", err)
	}

	authors, err := s.authorsFor(ctx, []int64{bookID})
	if err != nil {
		return library.Entry{}, err
	}
	entry.Book.Authors = authorsOrEmpty(authors[bookID])
	return entry, nil
}

func (s *SQLite) UpdateEntry(ctx context.Context, userID, bookID int64, status library.Status, finishedAt *time.Time) (library.Entry, error) {
	// updated_at is set explicitly: the column default only covers insert.
	const q = `UPDATE user_books SET status = ?, finished_at = ?, updated_at = ?
	           WHERE user_id = ? AND book_id = ?`

	res, err := s.db.ExecContext(ctx, q,
		string(status), utcOrNil(finishedAt), time.Now().UTC(), userID, bookID)
	if err != nil {
		return library.Entry{}, fmt.Errorf("update library entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return library.Entry{}, fmt.Errorf("update library entry: %w", err)
	}
	if n == 0 {
		return library.Entry{}, library.ErrNotInLibrary
	}
	return s.Entry(ctx, userID, bookID)
}

// DeleteEntry removes the user_books row only. The books row is shared across
// users and stays.
func (s *SQLite) DeleteEntry(ctx context.Context, userID, bookID int64) error {
	const q = `DELETE FROM user_books WHERE user_id = ? AND book_id = ?`

	res, err := s.db.ExecContext(ctx, q, userID, bookID)
	if err != nil {
		return fmt.Errorf("delete library entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete library entry: %w", err)
	}
	if n == 0 {
		return library.ErrNotInLibrary
	}
	return nil
}

// ListEntries loads a page in two queries rather than N+1: one join for the
// entries, then one batched lookup for their authors. Both are bounded by
// Limit.
func (s *SQLite) ListEntries(ctx context.Context, p library.ListParams) ([]library.Entry, error) {
	order, ok := orderBy[p.Sort]
	if !ok {
		order = orderBy[library.DefaultSort]
	}

	args := []any{p.UserID}
	where := `ub.user_id = ?`
	if p.Status != nil {
		where += ` AND ub.status = ?`
		args = append(args, string(*p.Status))
	}
	args = append(args, p.Limit, (p.Page-1)*p.Limit)

	q := `SELECT ` + entryColumns + `
	      FROM user_books ub
	      JOIN books b ON b.id = ub.book_id
	      WHERE ` + where + `
	      ORDER BY ` + order + `
	      LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list library entries: %w", err)
	}
	defer rows.Close()

	entries := make([]library.Entry, 0, p.Limit)
	bookIDs := make([]int64, 0, p.Limit)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("list library entries: %w", err)
		}
		entries = append(entries, entry)
		bookIDs = append(bookIDs, entry.Book.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list library entries: %w", err)
	}

	authors, err := s.authorsFor(ctx, bookIDs)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Book.Authors = authorsOrEmpty(authors[entries[i].Book.ID])
	}
	return entries, nil
}

// rowScanner is what *sql.Row and *sql.Rows have in common, so one scan
// function serves both the single-entry and list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(sc rowScanner) (library.Entry, error) {
	var (
		e          library.Entry
		status     string
		finishedAt sql.NullTime
	)
	err := sc.Scan(
		&e.Book.ID, &e.Book.ExternalID, &e.Book.ISBN, &e.Book.Title,
		&e.Book.Language, &e.Book.CoverURL, &e.Book.Source,
		&status, &finishedAt, &e.AddedAt,
	)
	if err != nil {
		return library.Entry{}, err
	}
	e.Status = library.Status(status)
	if finishedAt.Valid {
		t := finishedAt.Time.UTC()
		e.FinishedAt = &t
	}
	e.AddedAt = e.AddedAt.UTC()
	return e, nil
}

// authorsFor loads every author for a page of books in one query, keyed back
// by book ID in Go.
func (s *SQLite) authorsFor(ctx context.Context, bookIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(bookIDs) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(bookIDs)), ",")
	q := `SELECT book_id, name FROM book_authors
	      WHERE book_id IN (` + placeholders + `)
	      ORDER BY book_id, position`

	args := make([]any, 0, len(bookIDs))
	for _, id := range bookIDs {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("load authors: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			bookID int64
			name   string
		)
		if err := rows.Scan(&bookID, &name); err != nil {
			return nil, fmt.Errorf("load authors: %w", err)
		}
		out[bookID] = append(out[bookID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load authors: %w", err)
	}
	return out, nil
}

// authorsOrEmpty keeps books.Book.Authors non-nil, matching what UpsertBooks
// hands back and what the JSON layer guarantees.
func authorsOrEmpty(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}

// utcOrNil writes NULL for an unset finished_at and normalizes the rest, so
// every stored timestamp is comparable without a location.
func utcOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

// compile-time guard: the SQL layer must keep satisfying what the service
// depends on.
var _ library.Store = (*SQLite)(nil)
