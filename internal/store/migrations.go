package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	// migrationFunctionIndexesID adds the functional indexes that let a read
	// scoped to one project find its rows instead of scanning every observation.
	migrationFunctionIndexesID = "core-0003-fn-indexes"
)

// ensureFunctionIndexes builds the indexes behind observationsByProjectPredicate.
// Project names are stored lowercased, but rows written before that rule existed
// are not, so every project filter compares lower(project) — which an index on
// the bare column cannot serve. SQLite matches an index expression against the
// query expression, so a filter that stops spelling the call the same way stops
// using these; TestObservationProjectFilterUsesIndex is what catches that.
func (s *Store) ensureFunctionIndexes() error {
	return s.once(migrationFunctionIndexesID, func() error {
		_, err := s.execHook(s.db, `
			CREATE INDEX IF NOT EXISTS idx_obs_project_lower
				ON observations(lower(project), deleted_at);
			CREATE INDEX IF NOT EXISTS idx_obs_project_lower_created
				ON observations(lower(project), created_at DESC);
		`)
		return err
	})
}

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

// availableBytesFor is the seam the headroom check goes through, so a test can
// describe a filesystem with no room without having to fill one.
var availableBytesFor = availableBytes

// rebuildHeadroom is the free space a rebuilding migration demands, as a
// multiple of the database size: one copy for the backup, one for the table
// SQLite builds alongside the old one, and one so the filesystem is not left at
// zero when the migration finishes.
const rebuildHeadroom = 3

// rebuild runs a migration that rewrites a table rather than adding to it. A
// rebuild is the one migration a database cannot recover from on its own — the
// old table is gone by the time anything can fail — so the whole file is copied
// first, and the migration refuses to start when there is no room to copy it.
// The body runs at most once, like any other ledger entry.
func (s *Store) rebuild(id string, fn func() error) error {
	return s.once(id, func() error {
		if err := s.backupBeforeRebuild(id); err != nil {
			return err
		}
		return fn()
	})
}

// rebuildTable runs the DDL that replaces one table with a reshaped copy, with
// the guard rails a rebuild needs and an ordinary migration does not.
//
// Foreign keys go off before the transaction opens, not inside it: SQLite
// ignores the pragma while a transaction is active, and with keys enforced the
// DROP of the old table would cascade through every row that references it —
// the evidence and the observation links of every task. They come back on
// afterwards, and PRAGMA foreign_key_check runs before the commit so a rebuild
// that left a dangling reference is rolled back instead of stored.
func (s *Store) rebuildTable(ddl string) (err error) {
	if _, err := s.execHook(s.db, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("engram: disable foreign keys for rebuild: %w", err)
	}
	defer func() {
		if _, onErr := s.execHook(s.db, `PRAGMA foreign_keys = ON`); onErr != nil && err == nil {
			err = fmt.Errorf("engram: re-enable foreign keys after rebuild: %w", onErr)
		}
	}()

	tx, err := s.beginTxHook()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := s.execHook(tx, ddl); err != nil {
		return fmt.Errorf("engram: rebuild table: %w", err)
	}
	if err := foreignKeyCheckTx(tx); err != nil {
		return err
	}
	return s.commitHook(tx)
}

// foreignKeyCheckTx reports the first dangling reference PRAGMA
// foreign_key_check finds, or nil when the schema is whole.
func foreignKeyCheckTx(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("engram: check foreign keys after rebuild: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent sql.NullString
		var rowid, fkid sql.NullInt64
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("engram: read foreign key violation: %w", err)
		}
		return fmt.Errorf("engram: rebuild left %s row %d pointing at a missing %s row",
			table.String, rowid.Int64, parent.String)
	}
	return rows.Err()
}

// backupBeforeRebuild copies the database to engram.db.pre-<id>.bak. VACUUM INTO
// writes a consistent copy from inside SQLite, which a file copy cannot promise
// while the WAL holds committed pages the main file does not.
func (s *Store) backupBeforeRebuild(id string) error {
	dbPath := filepath.Join(s.cfg.DataDir, "engram.db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf("engram: stat database before rebuild: %w", err)
	}
	size := uint64(info.Size())

	free, err := availableBytesFor(s.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("engram: read free space before rebuild: %w", err)
	}
	if needed := size * rebuildHeadroom; free < needed {
		return fmt.Errorf(
			"engram: %s rebuilds a table and needs %d bytes free in %s, but only %d are available; free up space and run again",
			id, needed, s.cfg.DataDir, free)
	}

	backupPath := filepath.Join(s.cfg.DataDir, "engram.db.pre-"+id+".bak")
	// VACUUM INTO refuses to overwrite, so a backup left by an attempt that
	// failed after the copy is removed rather than turned into a hard stop.
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("engram: clear stale rebuild backup: %w", err)
	}
	if _, err := s.execHook(s.db, `VACUUM INTO ?`, backupPath); err != nil {
		return fmt.Errorf("engram: back up database before %s: %w", id, err)
	}
	return nil
}
