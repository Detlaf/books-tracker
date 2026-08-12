// Package books searches an external metadata provider and gives every result
// a stable local identity.
package books

// SourceGoogleBooks is the metadata_source value for rows this package writes.
const SourceGoogleBooks = "google_books"

// Book is one volume, both as a provider returns it and as it is stored. ID is
// the local books.id and stays zero until the row has been upserted, so a
// caller can tell a fetched book from a persisted one.
type Book struct {
	ID         int64
	ExternalID string
	ISBN       string
	Title      string
	Authors    []string
	Language   string
	CoverURL   string
	Source     string
}
