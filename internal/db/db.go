package db

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// connParams are appended to every DSN.
//
// _busy_timeout makes a writer wait for a competing write rather than failing
// immediately with SQLITE_BUSY, and _journal_mode=WAL lets readers proceed
// during a write. Both matter now that requests write concurrently.
const connParams = "_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL"

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", withConnParams(dsn))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return db, nil
}

// withConnParams joins connParams onto a DSN that may already carry a query
// string of its own; concatenating "?..." unconditionally would corrupt it.
func withConnParams(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + connParams
}

func runMigrations(db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite3", driver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
