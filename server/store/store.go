// Package store owns the reference implementation's persistence:
// SQLite via database/sql, deliberately driver-agnostic (D-191). The
// shipping driver is modernc.org/sqlite (pure Go — preserves the
// single-static-binary story, D-41); this repository currently
// validates against mattn/go-sqlite3 because the build sandbox cannot
// reach the modernc vanity import; the swap is one import line.
package store

import (
	"database/sql"
	"embed"
	"fmt"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path and applies
// pending migrations, tracked via PRAGMA user_version.
func Open(driver, path string) (*sql.DB, error) {
	db, err := sql.Open(driver, path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies migrations newer than the current user_version.
func Migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	files := []string{"migrations/0001_init.sql"} // append in order
	for i, f := range files {
		version := i + 1
		if version <= v {
			continue
		}
		sqlBytes, err := migrations.ReadFile(f)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", f, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
