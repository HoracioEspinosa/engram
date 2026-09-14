package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// The core schema is created with CREATE TABLE IF NOT EXISTS and CREATE INDEX IF
// NOT EXISTS, which are cheap to re-run and stay outside the ledger. What does
// not belong on every startup is the other half of a migration: the row rewrites
// that give existing data the shape the new schema assumes. Those are full-table
// writes, they are idempotent only in the sense that running them again changes
// nothing, and a database that has already been through them pays for the scan
// anyway. The ledger records the ones that ran so they run exactly once.
//
// PRAGMA user_version is not used here. It is a single integer and the
// engram-projects schema already owns it (ProjectsSchemaVersion), so the core
// needs a record of its own that can hold more than one id.
const (
	// migrationLedgerID records the creation of the ledger in the ledger, so a
	// database carries the fact that it has one.
	migrationLedgerID = "core-0001-schema-migrations"
	// migrationBackfillID covers the row rewrites that used to run on every open.
	migrationBackfillID = "core-0002-backfill-once"
)

// ensureMigrationLedger creates schema_migrations and records its own id. It is
// the one migration that cannot be guarded by the ledger, so it is written to be
// safe to repeat.
func (s *Store) ensureMigrationLedger() error {
	if _, err := s.execHook(s.db, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id         TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now')),
			checksum   TEXT
		);
	`); err != nil {
		return fmt.Errorf("engram: create migration ledger: %w", err)
	}
	return s.recordMigration(migrationLedgerID)
}

// once runs fn unless the ledger already records id, and records id when fn
// succeeds. A failing fn leaves no row, so the next open retries it.
func (s *Store) once(id string, fn func() error) error {
	applied, err := s.migrationApplied(id)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}
	if err := fn(); err != nil {
		return fmt.Errorf("engram: migration %s: %w", id, err)
	}
	return s.recordMigration(id)
}

// migrationApplied reports whether the ledger already records id.
func (s *Store) migrationApplied(id string) (bool, error) {
	var found string
	err := s.db.QueryRow(`SELECT id FROM schema_migrations WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("engram: read migration ledger: %w", err)
	}
	return true, nil
}

// recordMigration stamps id as applied. The checksum column is reserved for
// migrations whose body is worth verifying against the one that ran.
func (s *Store) recordMigration(id string) error {
	if _, err := s.execHook(s.db,
		`INSERT OR IGNORE INTO schema_migrations (id, applied_at) VALUES (?, datetime('now'))`, id,
	); err != nil {
		return fmt.Errorf("engram: record migration %s: %w", id, err)
	}
	return nil
}
