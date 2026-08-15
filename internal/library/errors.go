package library

import "errors"

var (
	// ErrUnknownBook means the book_id has no row in books. The client is
	// expected to have obtained the ID from /books/search or /books/isbn.
	ErrUnknownBook = errors.New("unknown book")
	// ErrAlreadyInLibrary means the caller already has this book. POST adds
	// and PATCH changes; a silent upsert would let a stale client reset a
	// deliberately-set read back to backlog.
	ErrAlreadyInLibrary = errors.New("book already in library")
	// ErrNotInLibrary means the caller has no entry for this book. It also
	// covers another user's entry, which must be indistinguishable from one
	// that does not exist.
	ErrNotInLibrary = errors.New("book not in library")
	// ErrInvalidStatus means the status was not backlog, reading, or read.
	ErrInvalidStatus = errors.New("status must be backlog, reading, or read")
	// ErrInvalidSort means the sort key was not in the allowlist.
	ErrInvalidSort = errors.New("invalid sort key")
	// ErrFutureFinishedAt means a book was marked finished after now.
	ErrFutureFinishedAt = errors.New("finished_at must not be in the future")
	// ErrFinishedAtNotRead means a finished_at arrived with a status other
	// than read, which has no meaning.
	ErrFinishedAtNotRead = errors.New("finished_at is only valid with status read")
	// ErrEmptyUpdate means a PATCH carried neither status nor finished_at.
	// That is always a client bug, never a meaningful no-op.
	ErrEmptyUpdate = errors.New("update must set status or finished_at")
)
