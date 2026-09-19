package collections

import "context"

// Store persists collections. *store.SQLite implements it; the interface
// keeps this package free of database/sql and lets name validation be
// tested against a fake.
//
// Every method takes userID and scopes by it. A collection belonging to
// another user must come back as ErrNotFound, never as someone else's row.
type Store interface {
	// Create inserts a new collection. name arrives already trimmed and
	// non-empty.
	Create(ctx context.Context, userID int64, name string) (Collection, error)
	// List returns every collection the caller owns.
	List(ctx context.Context, userID int64) ([]Collection, error)
	// Rename overwrites name, or returns ErrNotFound. name arrives already
	// trimmed and non-empty.
	Rename(ctx context.Context, userID, id int64, name string) (Collection, error)
	// Delete removes a collection, or returns ErrNotFound.
	Delete(ctx context.Context, userID, id int64) error
	// AddBook is idempotent: adding a book already in the collection is a
	// no-op that still returns the current collection. It returns
	// ErrNotFound for a bad collection id and ErrUnknownBook for a bad
	// book id.
	AddBook(ctx context.Context, userID, id, bookID int64) (Collection, error)
	// RemoveBook is not idempotent: removing a book that is not a member
	// returns ErrBookNotInCollection. It returns ErrNotFound for a bad
	// collection id.
	RemoveBook(ctx context.Context, userID, id, bookID int64) error
}
