package server

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Server struct {
	db     *sql.DB
	router *gin.Engine
}

func New(db *sql.DB) *Server {
	s := &Server{
		db:     db,
		router: gin.Default(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
