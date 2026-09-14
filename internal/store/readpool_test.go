package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// seedObservations inserts enough rows for a cursor to stay open across a
// single Next(): a query that returns nothing is exhausted immediately and
// database/sql hands its connection straight back to the pool, which would
// make every test below pass for the wrong reason.
func seedObservations(t *testing.T, s *Store, n int) {
	t.Helper()
	for i := range n {
		sessionID := fmt.Sprintf("session-%d", i)
		if err := s.CreateSession(sessionID, "readpool", "/tmp/readpool"); err != nil {
			t.Fatalf("CreateSession(%s): %v", sessionID, err)
		}
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: sessionID,
			Type:      "discovery",
			Title:     fmt.Sprintf("observation %d", i),
			Content:   fmt.Sprintf("content %d", i),
			Project:   "readpool",
		}); err != nil {
			t.Fatalf("AddObservation(%d): %v", i, err)
		}
	}
}

// runWithin reports whether fn finished inside d. It leaves the goroutine
// running when it does not, so the caller has to release whatever fn is
// blocked on before returning.
func runWithin(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestNestedReadQueriesDoNotDeadlock holds one cursor open and issues a second
// read while it is still held.
//
// The writer pool is capped at a single connection, so an undrained cursor
// owns the only way into the database. Every read that shares that pool then
// waits in database/sql's connection queue — not on SQLite's busy_timeout,
// which never comes into play because no lock is ever requested — and waits
// there for as long as the cursor stays open. Reads served from their own
// pool are unaffected, which is what this asserts.
func TestNestedReadQueriesDoNotDeadlock(t *testing.T) {
	cases := []struct {
		name string
		open func(s *Store) (*sql.Rows, error)
	}{
		{
			name: "cursor held on the writer pool",
			open: func(s *Store) (*sql.Rows, error) {
				return s.DB().Query(`SELECT id FROM observations ORDER BY id`)
			},
		},
		{
			name: "cursor held on the read pool",
			open: func(s *Store) (*sql.Rows, error) {
				return s.readDB().Query(`SELECT id FROM observations ORDER BY id`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			seedObservations(t, s, 5)

			rows, err := tc.open(s)
			if err != nil {
				t.Fatalf("open cursor: %v", err)
			}
			// Closed only at the very end: an early close would hand the
			// connection back and dissolve the condition under test.
			defer rows.Close()
			if !rows.Next() {
				t.Fatalf("expected the cursor to hold at least one row: %v", rows.Err())
			}

			if !runWithin(2*time.Second, func() {
				if _, err := s.Stats(); err != nil {
					t.Errorf("Stats while a cursor is held: %v", err)
				}
			}) {
				t.Fatal("Stats did not return within 2s while a cursor was held")
			}
		})
	}
}

// TestReadPoolAppliesForeignKeysPragma checks the pragmas on every connection
// in the pool, not just on the first one.
//
// A pragma applied with db.Exec after opening reaches whichever single
// connection the pool happened to hand out; the others are opened later and
// come up with SQLite's defaults — foreign keys off and no busy timeout.
// Carrying the pragmas on the DSN is what makes them per connection, and
// holding every connection at once is what proves it.
func TestReadPoolAppliesForeignKeysPragma(t *testing.T) {
	s := newTestStore(t)
	seedObservations(t, s, 2)

	if s.rdb == nil {
		t.Fatal("expected a read pool to be open")
	}

	size := readPoolSize()
	ctx := context.Background()
	conns := make([]*sql.Conn, 0, size)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	// Every connection is taken before any is released, so the pool is forced
	// to open `size` distinct ones rather than reusing a warm favourite.
	for i := range size {
		c, err := s.rdb.Conn(ctx)
		if err != nil {
			t.Fatalf("take read connection %d: %v", i, err)
		}
		conns = append(conns, c)
	}

	for i, c := range conns {
		var foreignKeys, busyTimeout int
		if err := c.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatalf("connection %d: read foreign_keys: %v", i, err)
		}
		if foreignKeys != 1 {
			t.Errorf("connection %d: foreign_keys = %d, want 1", i, foreignKeys)
		}
		if err := c.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatalf("connection %d: read busy_timeout: %v", i, err)
		}
		if busyTimeout != 5000 {
			t.Errorf("connection %d: busy_timeout = %d, want 5000", i, busyTimeout)
		}
	}
}

// TestReadPoolRejectsWrites pins the query_only pragma: a read connection that
// is handed a write must fail at the database rather than quietly succeed.
func TestReadPoolRejectsWrites(t *testing.T) {
	s := newTestStore(t)

	if s.rdb == nil {
		t.Fatal("expected a read pool to be open")
	}
	if _, err := s.rdb.Exec(`DELETE FROM observations`); err == nil {
		t.Fatal("expected a write through the read pool to be rejected")
	}
}

func TestReadPoolDisabledByEnv(t *testing.T) {
	t.Setenv("ENGRAM_READ_POOL", "0")

	cfg := mustDefaultConfig(t)
	cfg.DataDir = t.TempDir()
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if s.rdb != nil {
		t.Fatal("expected no read pool when ENGRAM_READ_POOL=0")
	}
	db, ok := s.readDB().(*sql.DB)
	if !ok {
		t.Fatalf("readDB returned %T, want *sql.DB", s.readDB())
	}
	if db != s.db {
		t.Fatal("expected readDB to fall back to the writer")
	}
}

// TestConcurrentReadDuringWrite keeps a write transaction open and asserts a
// plain read still completes.
//
// The transaction owns the writer pool's single connection for its whole
// lifetime, so a read sharing that pool cannot start until the commit. WAL
// lets readers see the pre-transaction snapshot without waiting, but only if
// they have a connection of their own to do it on.
func TestConcurrentReadDuringWrite(t *testing.T) {
	s := newTestStore(t)
	seedObservations(t, s, 3)

	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatalf("begin write transaction: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO sessions (id, project, directory) VALUES (?, ?, ?)`,
		"readpool-write", "readpool", "/tmp/readpool",
	); err != nil {
		t.Fatalf("write inside the transaction: %v", err)
	}

	if !runWithin(time.Second, func() {
		if _, err := s.Stats(); err != nil {
			t.Errorf("Stats during an open write transaction: %v", err)
		}
	}) {
		t.Fatal("Stats did not return within 1s while a write transaction was open")
	}
}
