package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/stats"
)

// respondStatsError maps service errors to status codes. There is no 404
// case in this milestone — every route aggregates over whatever the caller
// has, including zero books.
func respondStatsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, stats.ErrInvalidScope):
		respondError(c, http.StatusBadRequest, `scope must be "all" or a four-digit year`)
	case errors.Is(err, stats.ErrInvalidYear):
		respondError(c, http.StatusBadRequest, "year must be a four-digit year")
	default:
		log.Printf("stats: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

type summaryResponse struct {
	TotalRead   int `json:"total_read"`
	Reading     int `json:"reading"`
	Backlog     int `json:"backlog"`
	ThisYear    int `json:"this_year"`
	LastYear    int `json:"last_year"`
	CurrentYear int `json:"current_year"`
	UndatedRead int `json:"undated_read"`
}

func newSummaryResponse(sm stats.Summary) summaryResponse {
	return summaryResponse{
		TotalRead:   sm.TotalRead,
		Reading:     sm.Reading,
		Backlog:     sm.Backlog,
		ThisYear:    sm.ThisYear,
		LastYear:    sm.LastYear,
		CurrentYear: sm.CurrentYear,
		UndatedRead: sm.UndatedRead,
	}
}

func (s *Server) handleStatsSummary(c *gin.Context) {
	sm, err := s.stats.Summary(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, newSummaryResponse(sm))
}

type yearCountResponse struct {
	Year  string `json:"year"`
	Count int    `json:"count"`
}

func (s *Server) handleStatsByYear(c *gin.Context) {
	years, undated, err := s.stats.ByYear(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]yearCountResponse, 0, len(years))
	for _, y := range years {
		items = append(items, yearCountResponse{Year: y.Year, Count: y.Count})
	}
	c.JSON(http.StatusOK, gin.H{"years": items, "undated": undated})
}

type monthCountResponse struct {
	Month int `json:"month"`
	Count int `json:"count"`
}

func (s *Server) handleStatsByMonth(c *gin.Context) {
	year, months, err := s.stats.ByMonth(c.Request.Context(), userID(c), c.Query("year"))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]monthCountResponse, 0, len(months))
	for _, m := range months {
		items = append(items, monthCountResponse{Month: m.Month, Count: m.Count})
	}
	c.JSON(http.StatusOK, gin.H{"year": year, "months": items})
}

type languageCountResponse struct {
	Language string `json:"language"`
	Count    int    `json:"count"`
}

func (s *Server) handleStatsByLanguage(c *gin.Context) {
	languages, err := s.stats.ByLanguage(c.Request.Context(), userID(c), c.Query("scope"))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]languageCountResponse, 0, len(languages))
	for _, l := range languages {
		items = append(items, languageCountResponse{Language: l.Language, Count: l.Count})
	}
	c.JSON(http.StatusOK, gin.H{"languages": items})
}

type authorCountResponse struct {
	Author string `json:"author"`
	Count  int    `json:"count"`
}

func (s *Server) handleStatsTopAuthors(c *gin.Context) {
	limit := positiveQuery(c, "limit", stats.DefaultTopAuthorsLimit)
	if limit > stats.MaxTopAuthorsLimit {
		limit = stats.MaxTopAuthorsLimit
	}

	authors, err := s.stats.TopAuthors(c.Request.Context(), userID(c), c.Query("scope"), limit)
	if err != nil {
		respondStatsError(c, err)
		return
	}
	items := make([]authorCountResponse, 0, len(authors))
	for _, a := range authors {
		items = append(items, authorCountResponse{Author: a.Author, Count: a.Count})
	}
	c.JSON(http.StatusOK, gin.H{"authors": items})
}

func (s *Server) handleStatsStreak(c *gin.Context) {
	months, err := s.stats.Streak(c.Request.Context(), userID(c))
	if err != nil {
		respondStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"months": months})
}
