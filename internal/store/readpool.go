package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
)

// The write pool is capped at a single connection because SQLite serialises
// writers anyway and a second writer connection buys nothing but SQLITE_BUSY.
// Reads pay for that cap: an open cursor owns the connection until it is
// drained, so any read issued while one is in flight waits in database/sql's
// queue behind it, and so does every read issued during a write transaction.
//
// A second pool, opened read-only against the same file, takes reads off that
// queue. WAL gives each reader a consistent snapshot without blocking the
// writer or being blocked by it, so the two pools never contend.
const (
	// readPoolMaxConns caps the read pool. Four is enough to absorb the
	// nesting the read paths actually do (a cursor plus its per-row counts)
	// without holding open more file descriptors than a local agent needs.
	readPoolMaxConns = 4

	// readPoolEnv disables the read pool when set to "0", sending every read
	// back through the writer. It exists as an escape hatch for diagnosing a
	// problem suspected to come from the split, not as a tuning knob.
	readPoolEnv = "ENGRAM_READ_POOL"
)

// dbQueryer is the read surface the store's pure-read paths need: the subset
// of *sql.DB and *sql.Tx that never mutates. Returning it from readDB keeps
// a write from being issued against the read-only pool by accident — such a
// call would not compile.
type dbQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// writePragmas are applied per connection through the DSN rather than through
// Exec after opening. Exec reaches whichever single connection the pool hands
// out; a DSN pragma is replayed on every connection the driver opens, which is
// what a pool of more than one needs.
//
// busy_timeout is listed first only for readability — modernc.org/sqlite sorts
// it to the front itself so a later pragma that has to wait for a lock already
// has a timeout to wait under.
var writePragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(1)",
}

// readPragmas mirror the writer's, minus the ones that only make sense for a
// writer, plus query_only: a read connection that tries to write fails at the
// database rather than silently succeeding.
var readPragmas = []string{
	"busy_timeout(5000)",
	"foreign_keys(1)",
	"query_only(1)",
}

// sqliteDSN renders a file: URI carrying the given pragmas. url.URL does the
// percent-escaping, so a data directory containing a '?' or a '#' resolves to
// the path it names instead of being cut short by SQLite's URI parser.
func sqliteDSN(dbPath string, pragmas []string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	u := url.URL{Scheme: "file", Path: dbPath, RawQuery: q.Encode()}
	return u.String()
}

// readPoolSize is the read pool's connection cap, never above the number of
// threads that could use them.
func readPoolSize() int {
	if n := runtime.GOMAXPROCS(0); n < readPoolMaxConns {
		return n
	}
	return readPoolMaxConns
}

// readPoolEnabled reports whether reads get their own pool.
func readPoolEnabled() bool {
	return strings.TrimSpace(os.Getenv(readPoolEnv)) != "0"
}

// openReadPool opens a read-only pool against an existing database file.
//
// journal_mode is absent from the DSN on purpose: it is a property of the file
// and the writer has already set it, and a query_only connection cannot change
// it anyway.
func openReadPool(dbPath string, size int) (*sql.DB, error) {
	if size < 1 {
		size = 1
	}

	db, err := openDB("sqlite", sqliteDSN(dbPath, readPragmas))
	if err != nil {
		return nil, fmt.Errorf("engram: open read pool: %w", err)
	}
	db.SetMaxOpenConns(size)
	// Matched to MaxOpenConns so a connection that has paid for its pragmas
	// is kept rather than reopened on the next read.
	db.SetMaxIdleConns(size)

	// sql.Open is lazy, so a bad DSN or an unreadable file would otherwise
	// surface at the first read rather than here.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("engram: open read pool: %w", err)
	}
	return db, nil
}

// readDB returns the handle pure reads go through: the read pool when one is
// open, the writer otherwise. Callers inside a transaction must keep using
// that transaction — the read pool cannot see its uncommitted rows.
func (s *Store) readDB() dbQueryer {
	if s.rdb != nil {
		return s.rdb
	}
	return s.db
}
