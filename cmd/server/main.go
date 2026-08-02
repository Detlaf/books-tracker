package main

import (
	"log"
	"os"

	"github.com/kate/book-tracking/internal/db"
	"github.com/kate/book-tracking/internal/server"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "book_tracking.db"
	}

	database, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := server.New(database)
	log.Printf("listening on %s", addr)
	if err := srv.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
