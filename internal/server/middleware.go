package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/auth"
)

// userIDKey is the gin context key holding the authenticated caller's ID.
const userIDKey = "userID"

// RequireAuth verifies the bearer access token and stores the caller's ID in
// the request context. Verification is signature-only: access tokens are
// stateless, so this never touches the database.
func RequireAuth(signer *auth.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || token == "" {
			respondError(c, http.StatusUnauthorized, "missing or malformed authorization header")
			return
		}
		id, err := signer.ParseAccess(token)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		c.Set(userIDKey, id)
		c.Next()
	}
}

// userID returns the authenticated caller's ID. It is only valid inside a
// handler behind RequireAuth, which is the single place the value is set —
// handlers use this instead of repeating an unchecked type assertion.
func userID(c *gin.Context) int64 {
	id, _ := c.MustGet(userIDKey).(int64)
	return id
}
