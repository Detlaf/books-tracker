package main

import (
	"log"

	"github.com/kate/book-tracking/internal/config"
	"github.com/kate/book-tracking/internal/db"
	"github.com/kate/book-tracking/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	database, err := db.Open(cfg.DSN)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.New(database, cfg)
	log.Printf("listening on %s", cfg.Addr)
	if err := srv.Run(cfg.Addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
