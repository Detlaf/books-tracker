package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/db"
)

// newTestServer returns a server backed by a throwaway SQLite database with
// migrations applied. Milestones 4-7 reuse this helper.
//
// It uses a temp file rather than ":memory:" because each pooled connection to
// an in-memory SQLite database gets its own empty schema, so migrations run on
// one connection are invisible to the next.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	return New(database, config.Config{
		JWTSecret:  []byte("0123456789abcdef0123456789abcdef"),
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 30 * 24 * time.Hour,
	})
}
