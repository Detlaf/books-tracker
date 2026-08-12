package books

import "errors"

var (
	// ErrInvalidISBN means the input was not a checksum-valid ISBN-10 or -13.
	ErrInvalidISBN = errors.New("invalid isbn")
	// ErrBlankQuery means a search arrived with an empty or whitespace-only q.
	ErrBlankQuery = errors.New("search query must not be blank")
	// ErrNotFound means the provider matched no volume.
	ErrNotFound = errors.New("no book found")
	// ErrUpstream covers every provider failure a client cannot act on: a 5xx,
	// a timeout, or a body that will not parse. The detail is wrapped for logs
	// and must not reach a response body.
	ErrUpstream = errors.New("book metadata provider unavailable")
	// ErrRateLimited is the provider's 429, kept separate because the client
	// can act on it by backing off.
	ErrRateLimited = errors.New("book metadata provider rate limit exceeded")
)
