package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/library"
)

// addLibraryRequest identifies the book by local ID only. The client is
// expected to have hit /books/search or /books/isbn/:isbn first, both of which
// upsert the book and return its ID — which keeps provider calls, quota spend,
// and 502/429 failure modes off the library write path entirely.
//
// Status is required: defaulting it would make the most common mistake,
// omitting it, invisible.
type addLibraryRequest struct {
	BookID     int64   `json:"book_id"     binding:"required"`
	Status     string  `json:"status"      binding:"required"`
	FinishedAt *string `json:"finished_at"`
}

// updateLibraryRequest has no required fields at the binding layer; the
// service rejects a body with neither as ErrEmptyUpdate, so the message is the
// same whichever field the client forgot.
type updateLibraryRequest struct {
	Status     *string `json:"status"`
	FinishedAt *string `json:"finished_at"`
}

// libraryEntryResponse reuses bookResponse unchanged. FinishedAt is a pointer
// without omitempty so it is always present and explicitly null when unset: a
// client distinguishing "not finished" from "field absent" should not have to
// guess.
type libraryEntryResponse struct {
	Book       bookResponse `json:"book"`
	Status     string       `json:"status"`
	FinishedAt *string      `json:"finished_at"`
	AddedAt    string       `json:"added_at"`
}

func newLibraryEntryResponse(e library.Entry) libraryEntryResponse {
	var finishedAt *string
	if e.FinishedAt != nil {
		s := e.FinishedAt.UTC().Format(time.RFC3339)
		finishedAt = &s
	}
	return libraryEntryResponse{
		Book:       newBookResponse(e.Book),
		Status:     string(e.Status),
		FinishedAt: finishedAt,
		AddedAt:    e.AddedAt.UTC().Format(time.RFC3339),
	}
}

// respondLibraryError maps service errors to status codes. A missing entry and
// another user's entry are both 404: an entry the caller does not own must be
// indistinguishable from one that does not exist.
func respondLibraryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, library.ErrInvalidStatus):
		respondError(c, http.StatusBadRequest, "status must be backlog, reading, or read")
	case errors.Is(err, library.ErrInvalidSort):
		respondError(c, http.StatusBadRequest,
			"sort must be added_at, finished_at, or title, optionally prefixed with -")
	case errors.Is(err, library.ErrFutureFinishedAt):
		respondError(c, http.StatusBadRequest, "finished_at must not be in the future")
	case errors.Is(err, library.ErrFinishedAtNotRead):
		respondError(c, http.StatusBadRequest, "finished_at is only valid with status read")
	case errors.Is(err, library.ErrEmptyUpdate):
		respondError(c, http.StatusBadRequest, "update must set status or finished_at")
	case errors.Is(err, library.ErrUnknownBook):
		respondError(c, http.StatusNotFound, "no book with that id")
	case errors.Is(err, library.ErrNotInLibrary):
		respondError(c, http.StatusNotFound, "book not in your library")
	case errors.Is(err, library.ErrAlreadyInLibrary):
		respondError(c, http.StatusConflict, "book already in your library")
	default:
		log.Printf("library: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

// parseFinishedAt turns an optional RFC 3339 string into an optional time. A
// nil in means the client did not send the field.
func parseFinishedAt(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		return nil, err
	}
	utc := t.UTC()
	return &utc, nil
}

// libraryBookID reads the :book_id path parameter.
func libraryBookID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("book_id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "book_id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) handleLibraryList(c *gin.Context) {
	// An unrecognized status is a 400 rather than an empty list: a typo that
	// returns 200 [] reads as "you have no books".
	var status *library.Status
	if raw := c.Query("status"); raw != "" {
		parsed, err := library.ParseStatus(raw)
		if err != nil {
			respondLibraryError(c, err)
			return
		}
		status = &parsed
	}

	sort, err := library.ParseSort(c.Query("sort"))
	if err != nil {
		respondLibraryError(c, err)
		return
	}

	page := positiveQuery(c, "page", 1)
	limit := positiveQuery(c, "limit", library.DefaultListLimit)
	if limit > library.MaxListLimit {
		limit = library.MaxListLimit
	}

	entries, err := s.library.List(c.Request.Context(), library.ListParams{
		UserID: userID(c),
		Status: status,
		Sort:   sort,
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		respondLibraryError(c, err)
		return
	}

	items := make([]libraryEntryResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, newLibraryEntryResponse(e))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "page": page, "limit": limit})
}

func (s *Server) handleLibraryAdd(c *gin.Context) {
	var req addLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	status, err := library.ParseStatus(req.Status)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	finishedAt, err := parseFinishedAt(req.FinishedAt)
	if err != nil {
		respondError(c, http.StatusBadRequest, "finished_at must be an RFC 3339 timestamp")
		return
	}

	entry, err := s.library.Add(c.Request.Context(), userID(c), req.BookID, status, finishedAt)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newLibraryEntryResponse(entry))
}

func (s *Server) handleLibraryUpdate(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	var req updateLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	var status *library.Status
	if req.Status != nil {
		parsed, err := library.ParseStatus(*req.Status)
		if err != nil {
			respondLibraryError(c, err)
			return
		}
		status = &parsed
	}
	finishedAt, err := parseFinishedAt(req.FinishedAt)
	if err != nil {
		respondError(c, http.StatusBadRequest, "finished_at must be an RFC 3339 timestamp")
		return
	}

	entry, err := s.library.Update(c.Request.Context(), userID(c), bookID, status, finishedAt)
	if err != nil {
		respondLibraryError(c, err)
		return
	}
	c.JSON(http.StatusOK, newLibraryEntryResponse(entry))
}

func (s *Server) handleLibraryDelete(c *gin.Context) {
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	if err := s.library.Remove(c.Request.Context(), userID(c), bookID); err != nil {
		respondLibraryError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
