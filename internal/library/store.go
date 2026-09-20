package library

import (
	"context"
	"time"
)

// Store persists library entries. *store.SQLite implements it; the interface
// keeps this package free of database/sql and lets the transition rules be
// tested against a fake.
//
// Every method takes userID and scopes by it. An entry belonging to another
// user must come back as ErrNotInLibrary, never as someone else's row.
type Store interface {
	// AddEntry inserts a new entry. It returns ErrAlreadyInLibrary when the
	// caller already has the book and ErrUnknownBook when no such book exists.
	AddEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error)
	// Entry returns one entry, or ErrNotInLibrary.
	Entry(ctx context.Context, userID, bookID int64) (Entry, error)
	// UpdateEntry overwrites status and finished_at, and returns
	// ErrNotInLibrary when there is no such row.
	UpdateEntry(ctx context.Context, userID, bookID int64, status Status, finishedAt *time.Time) (Entry, error)
	// DeleteEntry removes an entry, or returns ErrNotInLibrary.
	DeleteEntry(ctx context.Context, userID, bookID int64) error
	// ListEntries returns one page. Page and Limit are already normalized.
	ListEntries(ctx context.Context, p ListParams) ([]Entry, error)
	// SetRating upserts a rating. It does not validate the score or the
	// entry's status — that is Service's job — but it does not create an
	// entry that does not exist either; a foreign key violation on user_id
	// or book_id would only happen if the caller already passed a bad
	// (userID, bookID) pair, which Service rules out by loading the entry
	// first.
	SetRating(ctx context.Context, userID, bookID int64, score int) error
	// ClearRating deletes a rating if one exists. It is a no-op, not an
	// error, when there is none.
	ClearRating(ctx context.Context, userID, bookID int64) error
}
