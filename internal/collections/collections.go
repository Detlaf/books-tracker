// Package collections keeps each user's named groupings of library books.
package collections

import "time"

// Collection is one user's named group of books. BookIDs is never nil: an
// empty collection reports []int64{}, matching how library.Entry's slices
// are never nil either.
type Collection struct {
	ID        int64
	UserID    int64
	Name      string
	BookIDs   []int64
	CreatedAt time.Time
}
