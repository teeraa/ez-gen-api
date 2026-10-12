// Command migrate runs all pending SQL migration files in migrations/ against
// the database configured in DATABASE_URL.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	// Ensure migrations tracking table exists
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Fatalf("create schema_migrations: %v", err)
	}

	// Find applied migrations
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		log.Fatalf("query applied: %v", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			log.Fatalf("scan: %v", err)
		}
		applied[v] = true
	}
	rows.Close()

	// Collect migration files
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		log.Fatalf("glob: %v", err)
	}
	sort.Strings(files)

	ran := 0
	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), ".sql")
		if applied[version] {
			fmt.Printf("  skip  %s\n", version)
			continue
		}

		content, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("read %s: %v", f, err)
		}

		tx, err := db.Begin()
		if err != nil {
			log.Fatalf("begin tx: %v", err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			log.Fatalf("apply %s: %v", version, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES($1)`, version); err != nil {
			tx.Rollback()
			log.Fatalf("record %s: %v", version, err)
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("commit %s: %v", version, err)
		}

		fmt.Printf("  apply %s\n", version)
		ran++
	}

	if ran == 0 {
		fmt.Println("all migrations already applied")
	} else {
		fmt.Printf("applied %d migration(s)\n", ran)
	}
}
