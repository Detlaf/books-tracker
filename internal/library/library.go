// Package library keeps each user's books and the status they are in.
package library

import (
	"time"

	"github.com/kate/book-tracking/internal/books"
)

// Status is where a book sits in a user's library. The values match the CHECK
// constraint on user_books, which stays as a backstop: validating here means a
// typo is a 400 with a message rather than a constraint violation surfacing as
// a 500.
type Status string

const (
	StatusBacklog Status = "backlog"
	StatusReading Status = "reading"
	StatusRead    Status = "read"
)

// ParseStatus validates a client-supplied status.
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusBacklog:
		return StatusBacklog, nil
	case StatusReading:
		return StatusReading, nil
	case StatusRead:
		return StatusRead, nil
	default:
		return "", ErrInvalidStatus
	}
}

// Sort is an allowlisted list ordering. The store maps each value to a fixed
// ORDER BY fragment; the client's string never reaches SQL.
type Sort string

const (
	SortAddedAt        Sort = "added_at"
	SortAddedAtDesc    Sort = "-added_at"
	SortFinishedAt     Sort = "finished_at"
	SortFinishedAtDesc Sort = "-finished_at"
	SortTitle          Sort = "title"
	SortTitleDesc      Sort = "-title"

	// DefaultSort shows the most recently added book first, which is what a
	// user who just added one expects to see.
	DefaultSort = SortAddedAtDesc
)

// ParseSort validates a client-supplied sort. An empty value is the default;
// anything unrecognized is an error rather than a silent fallback, so a typo
// does not masquerade as a deliberate ordering.
func ParseSort(s string) (Sort, error) {
	if s == "" {
		return DefaultSort, nil
	}
	switch Sort(s) {
	case SortAddedAt, SortAddedAtDesc,
		SortFinishedAt, SortFinishedAtDesc,
		SortTitle, SortTitleDesc:
		return Sort(s), nil
	default:
		return "", ErrInvalidSort
	}
}

// Entry is one book in one user's library. FinishedAt is a pointer because
// "not finished" is a real state the API reports as an explicit null.
type Entry struct {
	Book       books.Book
	Status     Status
	FinishedAt *time.Time
	AddedAt    time.Time
}

const (
	// DefaultListLimit is the page size when a caller does not choose one.
	DefaultListLimit = 20
	// MaxListLimit caps a page. Google's ceiling of 40 does not apply: this
	// is a local join, not a quota-metered upstream call.
	MaxListLimit = 100
)

// ListParams is one page of one user's library. A nil Status means every
// status; Page is 1-based, matching /books/search.
type ListParams struct {
	UserID int64
	Status *Status
	Sort   Sort
	Page   int
	Limit  int
}
