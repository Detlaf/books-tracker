package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/collections"
)

// collectionRequest carries the name for both create and rename. It has no
// binding tag: an empty or whitespace-only name is Service's job to reject,
// the same way updateLibraryRequest leaves validation to library.Service.
type collectionRequest struct {
	Name string `json:"name"`
}

// addBookRequest identifies the book by local ID only, the same contract
// addLibraryRequest uses.
type addBookRequest struct {
	BookID int64 `json:"book_id" binding:"required,gt=0"`
}

type collectionResponse struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	BookIDs   []int64 `json:"book_ids"`
	CreatedAt string  `json:"created_at"`
}

func newCollectionResponse(c collections.Collection) collectionResponse {
	ids := c.BookIDs
	if ids == nil {
		ids = []int64{}
	}
	return collectionResponse{
		ID:        c.ID,
		Name:      c.Name,
		BookIDs:   ids,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// respondCollectionsError maps service errors to status codes. A missing
// collection and another user's collection are both 404, mirroring
// respondLibraryError's treatment of library entries.
func respondCollectionsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, collections.ErrEmptyName):
		respondError(c, http.StatusBadRequest, "collection name must not be empty")
	case errors.Is(err, collections.ErrNotFound):
		respondError(c, http.StatusNotFound, "collection not found")
	case errors.Is(err, collections.ErrUnknownBook):
		respondError(c, http.StatusNotFound, "no book with that id")
	case errors.Is(err, collections.ErrBookNotInCollection):
		respondError(c, http.StatusNotFound, "book not in that collection")
	default:
		log.Printf("collections: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

// collectionID reads the :id path parameter.
func collectionID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		respondError(c, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) handleCollectionsList(c *gin.Context) {
	items, err := s.collections.List(c.Request.Context(), userID(c))
	if err != nil {
		respondCollectionsError(c, err)
		return
	}

	resp := make([]collectionResponse, 0, len(items))
	for _, i := range items {
		resp = append(resp, newCollectionResponse(i))
	}
	c.JSON(http.StatusOK, gin.H{"items": resp})
}

func (s *Server) handleCollectionsCreate(c *gin.Context) {
	var req collectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	created, err := s.collections.Create(c.Request.Context(), userID(c), req.Name)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newCollectionResponse(created))
}

func (s *Server) handleCollectionsRename(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	var req collectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.collections.Rename(c.Request.Context(), userID(c), id, req.Name)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newCollectionResponse(updated))
}

func (s *Server) handleCollectionsDelete(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	if err := s.collections.Delete(c.Request.Context(), userID(c), id); err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleCollectionsAddBook(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}

	var req addBookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	updated, err := s.collections.AddBook(c.Request.Context(), userID(c), id, req.BookID)
	if err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.JSON(http.StatusCreated, newCollectionResponse(updated))
}

func (s *Server) handleCollectionsRemoveBook(c *gin.Context) {
	id, ok := collectionID(c)
	if !ok {
		return
	}
	bookID, ok := libraryBookID(c)
	if !ok {
		return
	}

	if err := s.collections.RemoveBook(c.Request.Context(), userID(c), id, bookID); err != nil {
		respondCollectionsError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
