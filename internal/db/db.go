package db

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

// Creates and verifies connection to postgres
func Open(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging db: %w", err)
	}

	log.Println("connected to postgres")
	return db, nil
}
