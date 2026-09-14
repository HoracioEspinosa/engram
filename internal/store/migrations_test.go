package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// countingExecHooks records every statement handed to execHook and then runs it
// unchanged, so what a migration writes can be counted without changing it.
func countingExecHooks(statements *[]string) storeHooks {
	hooks := defaultStoreHooks()
	inner := hooks.exec
	hooks.exec = func(db execer, query string, args ...any) (sql.Result, error) {
		*statements = append(*statements, query)
		return inner(db, query, args...)
	}
	return hooks
}

// countUpdates counts the recorded statements that rewrite existing rows. The
// FTS trigger bodies mention UPDATE too, but they are CREATE TRIGGER statements,
// so matching on the leading keyword keeps them out.
func countUpdates(statements []string) int {
	n := 0
	for _, q := range statements {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(q)), "UPDATE ") {
			n++
		}
	}
	return n
}

// openMigrationTestStore opens the database under dir the way New does but stops
// short of migrating, so the caller decides when migrate runs and watches it
// through the hooks.
func openMigrationTestStore(t *testing.T, dir string, hooks storeHooks) *Store {
	t.Helper()
	db, err := openDB("sqlite", filepath.Join(dir, "engram.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("pragma %q: %v", p, err)
		}
	}
	cfg := mustDefaultConfig(t)
	cfg.DataDir = dir
	s := &Store{db: db, cfg: cfg, hooks: hooks}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestMigrationLedgerRunsBackfillOnce pins what the ledger buys: the row
// rewrites every command used to run on startup happen on the first open and
// never again, so opening an existing database costs no full-table scan.
func TestMigrationLedgerRunsBackfillOnce(t *testing.T) {
	dir := t.TempDir()

	var first []string
	s1 := openMigrationTestStore(t, dir, countingExecHooks(&first))
	if err := s1.migrate(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if countUpdates(first) == 0 {
		t.Fatal("expected the first migration to run the backfills")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	var second []string
	s2 := openMigrationTestStore(t, dir, countingExecHooks(&second))
	if err := s2.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if n := countUpdates(second); n != 0 {
		t.Fatalf("second migration ran %d backfill statement(s); want 0", n)
	}

	for _, id := range []string{migrationLedgerID, migrationBackfillID} {
		var applied int
		if err := s2.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE id = ?`, id).Scan(&applied); err != nil {
			t.Fatalf("read ledger row %q: %v", id, err)
		}
		if applied != 1 {
			t.Fatalf("schema_migrations rows for %q = %d; want 1", id, applied)
		}
	}

	// The ledger is the core's own record; projects keeps using user_version.
	var userVersion int
	if err := s2.db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if userVersion != ProjectsSchemaVersion {
		t.Fatalf("user_version = %d; want %d, untouched by the core ledger", userVersion, ProjectsSchemaVersion)
	}
}
