package server

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/store"
)

type Server struct {
	db     *sql.DB
	router *gin.Engine
	auth   *auth.Service
	signer *auth.Signer
}

func New(db *sql.DB, cfg config.Config) *Server {
	signer := auth.NewSigner(cfg.JWTSecret, cfg.AccessTTL)
	s := &Server{
		db:     db,
		router: gin.Default(),
		auth:   auth.NewService(store.New(db), signer, cfg.RefreshTTL),
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

	// Milestones 4-7 hang their routes off this group.
	authed := s.router.Group("/", RequireAuth(s.signer))
	authed.GET("/me", s.handleMe)
}

// Handler exposes the router for tests and for embedding behind another mux.
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) Run(addr string) error { return s.router.Run(addr) }
