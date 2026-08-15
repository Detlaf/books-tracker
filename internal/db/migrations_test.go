package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration5BackfillsAuthors writes a book under the pre-000005 schema and
// checks the author survives the move from a column to a table. The books table
// is empty in every real deployment today, but the backfill is the destructive
// part of this migration and is worth pinning down.
func TestMigration5BackfillsAuthors(t *testing.T) {
	database, err := sql.Open("sqlite3", withConnParams(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	m, err := migrator(database)
	if err != nil {
		t.Fatalf("migrator: %v", err)
	}
	if err := m.Migrate(4); err != nil {
		t.Fatalf("migrate to 4: %v", err)
	}

	res, err := database.Exec(`INSERT INTO books (title, author) VALUES ('Dune', 'Frank Herbert')`)
	if err != nil {
		t.Fatalf("insert legacy book: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	if err := m.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	var name string
	err = database.QueryRow(
		`SELECT name FROM book_authors WHERE book_id = ? AND position = 0`, id).Scan(&name)
	if err != nil {
		t.Fatalf("read backfilled author: %v", err)
	}
	if name != "Frank Herbert" {
		t.Fatalf("author = %q, want %q", name, "Frank Herbert")
	}
}

// TestMigration5ExternalIDIsUnique checks the dedupe key the store relies on.
// SQLite allows repeated NULLs under a UNIQUE index, which is what lets rows
// predating this column coexist.
func TestMigration5ExternalIDIsUnique(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	insert := `INSERT INTO books (external_id, title) VALUES (?, ?)`
	if _, err := database.Exec(insert, "vol-1", "Dune"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := database.Exec(insert, "vol-1", "Dune reissue"); err == nil {
		t.Fatal("expected a unique constraint violation on a repeated external_id")
	}
	if _, err := database.Exec(insert, nil, "No external id"); err != nil {
		t.Fatalf("first NULL external_id: %v", err)
	}
	if _, err := database.Exec(insert, nil, "Also no external id"); err != nil {
		t.Fatalf("NULL external_id must not collide: %v", err)
	}
}

func TestUserBooksUserStatusIndexExists(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	var name string
	err = database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`,
		"idx_user_books_user_status").Scan(&name)
	if err != nil {
		t.Fatalf("idx_user_books_user_status must exist after migrations: %v", err)
	}
}
