package server

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
)

type credentialsRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// respondError writes the one error shape the API uses.
func respondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

// respondServiceError maps service errors to status codes. Anything
// unrecognized is a 500 with the detail logged, never returned.
func respondServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		respondError(c, http.StatusConflict, "email already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		respondError(c, http.StatusUnauthorized, "invalid email or password")
	case errors.Is(err, auth.ErrInvalidToken):
		respondError(c, http.StatusUnauthorized, "invalid or expired token")
	case errors.Is(err, auth.ErrPasswordLength):
		respondError(c, http.StatusBadRequest, "password must be between 8 and 72 bytes")
	default:
		log.Printf("auth: unexpected error: %v", err)
		respondError(c, http.StatusInternalServerError, "internal server error")
	}
}

func writeTokenPair(c *gin.Context, pair auth.TokenPair) {
	c.JSON(http.StatusOK, gin.H{
		"access_token":  pair.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    pair.ExpiresIn,
		"refresh_token": pair.RefreshToken,
	})
}

func (s *Server) handleRegister(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.auth.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": user.ID, "email": user.Email})
}

func (s *Server) handleLogin(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := s.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	writeTokenPair(c, pair)
}

func (s *Server) handleRefresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := s.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondServiceError(c, err)
		return
	}
	writeTokenPair(c, pair)
}

func (s *Server) handleLogout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.auth.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		respondServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// handleMe returns the caller's identity. It is the first consumer of the
// protected-route pattern and doubles as a client-side token check.
func (s *Server) handleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user_id": userID(c)})
}
