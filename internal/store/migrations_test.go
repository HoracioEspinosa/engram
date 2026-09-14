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

// TestObservationProjectFilterUsesIndex pins the reason the functional indexes
// exist: every read that narrows observations to one project must find its rows
// through an index instead of reading the whole table. The statements are the
// ones the store itself runs, captured through the hooks, so a rewrite of the
// filter that silently loses the index fails here.
func TestObservationProjectFilterUsesIndex(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-index", "koi-garden", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-index", Type: "discovery", Project: "koi-garden",
		TopicKey: "koi/garden/pond", Title: "pond", Content: "the pond needs a filter",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	type statement struct {
		sql  string
		args []any
	}
	var captured []statement
	orig := s.hooks.queryIt
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		captured = append(captured, statement{sql: query, args: args})
		return orig(db, query, args...)
	}
	if _, err := s.Search("koi/garden/pond", SearchOptions{Project: "koi-garden", Limit: 10}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := s.RecentObservations("koi-garden", "project", 10); err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	s.hooks.queryIt = orig

	explain := func(t *testing.T, st statement) string {
		t.Helper()
		rows, err := s.db.Query("EXPLAIN QUERY PLAN "+st.sql, st.args...)
		if err != nil {
			t.Fatalf("explain query plan: %v", err)
		}
		defer rows.Close()
		var plan []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatalf("scan plan row: %v", err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read plan: %v", err)
		}
		return strings.Join(plan, " | ")
	}

	// Every statement that reads observations directly and narrows them to a
	// project. The FTS statements are excluded: they enter through the index and
	// reach observations by rowid, so no project index applies to them.
	filtered := 0
	usesProjectIndex := false
	for _, st := range captured {
		lowered := strings.ToLower(st.sql)
		if !strings.Contains(lowered, "from observations\n") && !strings.Contains(lowered, "from observations o\n") {
			continue
		}
		if !strings.Contains(lowered, "lower(project) = ?") && !strings.Contains(lowered, "lower(o.project) = ?") {
			continue
		}
		filtered++
		plan := explain(t, st)
		if strings.Contains(plan, "SCAN observations") {
			t.Fatalf("a project-scoped read still scans the observations table: %s\nquery: %s", plan, st.sql)
		}
		if strings.Contains(plan, "idx_obs_project_lower") {
			usesProjectIndex = true
		}
	}
	if filtered < 2 {
		t.Fatalf("expected at least two project-scoped observation reads, captured %d", filtered)
	}
	if !usesProjectIndex {
		t.Fatal("no project-scoped read uses idx_obs_project_lower")
	}

	// The counters mem_project_card shows read the same rows through the same index.
	counts := statement{
		sql:  "SELECT COUNT(*) FROM observations WHERE " + observationsByProjectPredicate,
		args: []any{"koi-garden"},
	}
	plan := explain(t, counts)
	if !strings.Contains(plan, "idx_obs_project_lower") || strings.Contains(plan, "SCAN observations") {
		t.Fatalf("the card counter query does not use idx_obs_project_lower: %s", plan)
	}
}
