package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/books"
)

// bookResponse is the wire shape of a book. It omits metadata_source and the
// provider's own fields a client cannot use, and always emits authors so a
// client can iterate without a null check.
type bookResponse struct {
	ID         int64    `json:"id"`
	ExternalID string   `json:"external_id"`
	ISBN       string   `json:"isbn,omitempty"`
	Title      string   `json:"title"`
	Authors    []string `json:"authors"`
	Language   string   `json:"language,omitempty"`
	CoverURL   string   `json:"cover_url,omitempty"`
}

func newBookResponse(b books.Book) bookResponse {
	authors := b.Authors
	if authors == nil {
		authors = []string{}
	}
	return bookResponse{
		ID:         b.ID,
		ExternalID: b.ExternalID,
		ISBN:       b.ISBN,
		Title:      b.Title,
		Authors:    authors,
		Language:   b.Language,
		CoverURL:   b.CoverURL,
	}
}

// respondBooksError maps service errors to status codes. Upstream detail is
// logged rather than returned: a provider error can echo the request URL, and
// the request URL carries the API key.
func respondBooksError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, books.ErrBlankQuery):
		respondError(c, http.StatusBadRequest, "query parameter q must not be blank")
	case errors.Is(err, books.ErrInvalidISBN):
		respondError(c, http.StatusBadRequest, "invalid isbn")
	case errors.Is(err, books.ErrNotFound):
		respondError(c, http.StatusNotFound, "no book found for that isbn")
	case errors.Is(err, books.ErrRateLimited):
		log.Printf("books: provider rate limited: %v", err)
		respondError(c, http.StatusTooManyRequests, "book metadata provider rate limit exceeded")
	case errors.Is(err, books.ErrUpstream):
		log.Printf("books: upstream error: %v", err)
		respondError(c, http.StatusBadGateway, "book metadata provider unavailable")
	default:
		log.Printf("books: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func (s *Server) handleBookSearch(c *gin.Context) {
	page := positiveQuery(c, "page", 1)
	limit := positiveQuery(c, "limit", books.DefaultSearchLimit)
	if limit > books.MaxSearchLimit {
		limit = books.MaxSearchLimit
	}

	found, err := s.books.Search(c.Request.Context(), c.Query("q"), page, limit)
	if err != nil {
		respondBooksError(c, err)
		return
	}

	items := make([]bookResponse, 0, len(found))
	for _, b := range found {
		items = append(items, newBookResponse(b))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "limit": limit})
}

func (s *Server) handleBookByISBN(c *gin.Context) {
	book, err := s.books.ByISBN(c.Request.Context(), c.Param("isbn"))
	if err != nil {
		respondBooksError(c, err)
		return
	}
	c.JSON(http.StatusOK, newBookResponse(book))
}

// positiveQuery reads a positive integer parameter, falling back to def when it
// is missing, unparseable, or below 1. A nonsense page number is not worth a
// 400 when a sane default exists, and it keeps paging bugs in a client from
// surfacing as errors a user sees.
func positiveQuery(c *gin.Context, key string, def int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil || v < 1 {
		return def
	}
	return v
}
