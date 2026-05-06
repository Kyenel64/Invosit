package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// migrationLockKey is an arbitrary bigint shared by every Invosit instance.
// pg_advisory_lock blocks until acquired, so concurrent Migrate calls
// (e.g. two replicas starting at once) serialise instead of racing.
const migrationLockKey = 4242

// Migrate applies any *.sql files in dir that haven't been recorded in
// schema_migrations yet, in lexical order. Each file runs in its own
// transaction so a failure rolls back cleanly.
//
// Filename without the .sql extension is the version string.
func Migrate(database *sql.DB, dir string) error {
	if _, err := database.Exec("SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}
	defer database.Exec("SELECT pg_advisory_unlock($1)", migrationLockKey)

	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading migrations dir %q: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	applied := map[string]bool{}
	rows, err := database.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("loading applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		sqlBytes, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying %s: %w", name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("applied migration: %s", name)
	}

	return nil
}
