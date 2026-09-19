package collections

import "errors"

var (
	// ErrNotFound means the collection id has no row, or belongs to another
	// user — the two must be indistinguishable to the caller.
	ErrNotFound = errors.New("collection not found")
	// ErrEmptyName means the name was empty after trimming whitespace.
	ErrEmptyName = errors.New("collection name must not be empty")
	// ErrUnknownBook means the book_id has no row in books.
	ErrUnknownBook = errors.New("unknown book")
	// ErrBookNotInCollection means the caller tried to remove a book that is
	// not a member. Unlike adding, removing is not idempotent.
	ErrBookNotInCollection = errors.New("book not in collection")
)
