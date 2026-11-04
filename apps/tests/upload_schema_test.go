package tests

import (
	"database/sql"
	"github.com/ez-gen/ez-gen-api/apps/models"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/gorm/schema"
	"net/url"
	"os"
	"sync"
	"testing"
)

// Opt in only with an explicitly selected isolated local database. This checks
// the migrated schema without modifying rows or applying migrations itself.
func TestUploadModelMatchesMigratedColumns(t *testing.T) {
	dsn := os.Getenv("UPLOAD_SCHEMA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPLOAD_SCHEMA_TEST_DATABASE_URL not set; requires isolated migrated PostgreSQL")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		t.Fatal("upload schema test requires a local isolated database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='uploads'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	mapping, err := schema.Parse(&models.Upload{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range mapping.Fields {
		if field.DBName != "" && !columns[field.DBName] {
			t.Errorf("Upload.%s maps to missing uploads.%s; apply the additive schema repair", field.Name, field.DBName)
		}
	}
}
