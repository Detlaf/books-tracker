package server

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
	"github.com/kate/book-tracking/internal/books"
	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/store"
)

type Server struct {
	db     *sql.DB
	router *gin.Engine
	auth   *auth.Service
	books  *books.Service
	signer *auth.Signer
}

func New(db *sql.DB, cfg config.Config) *Server {
	signer := auth.NewSigner(cfg.JWTSecret, cfg.AccessTTL)
	sqlStore := store.New(db)
	s := &Server{
		db:     db,
		router: gin.Default(),
		auth:   auth.NewService(sqlStore, signer, cfg.RefreshTTL),
		books:  books.NewService(books.NewGoogleBooks(cfg.GoogleBooksAPIKey), sqlStore),
		signer: signer,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authGroup := s.router.Group("/auth")
	authGroup.POST("/register", s.handleRegister)
	authGroup.POST("/login", s.handleLogin)
	authGroup.POST("/refresh", s.handleRefresh)
	authGroup.POST("/logout", s.handleLogout)

	// Milestones 5-7 hang their routes off this group.
	authed := s.router.Group("/", RequireAuth(s.signer))
	authed.GET("/me", s.handleMe)
	// Behind auth because every call spends API quota and writes book rows.
	authed.GET("/books/search", s.handleBookSearch)
	authed.GET("/books/isbn/:isbn", s.handleBookByISBN)
}

// Handler exposes the router for tests and for embedding behind another mux.
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Run(addr string) error { return s.router.Run(addr) }
