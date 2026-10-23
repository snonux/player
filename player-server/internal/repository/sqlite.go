package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/snonux/player/internal/clock"

	// Register the pure-Go SQLite driver with database/sql.
	_ "modernc.org/sqlite"
)

// SQLite is a concrete Store implementation backed by SQLite.
type SQLite struct {
	db *sql.DB
	// clock is the server time of server-side edits and of clock clamping
	// in the episode bridge; WithClock sets the services' clock.
	clock clock.Clock
}

const sqliteBusyTimeoutMS = 5000

// New creates a SQLite store from an existing *sql.DB after initializing the schema.
//
// The connection must write its timestamps in UTC and enforce foreign keys
// on every pooled connection (see connectionDSN, which Open applies):
// queries compare and sort timestamps as text, and deletes rely on the
// schema's ON DELETE CASCADE.
func New(db *sql.DB) (*SQLite, error) {
	if err := initializeSchema(db); err != nil {
		return nil, fmt.Errorf("initialize schema: %w", err)
	}
	if err := initializeServerIdentity(db); err != nil {
		return nil, err
	}
	return &SQLite{db: db, clock: clock.RealClock{}}, nil
}

// Open opens a SQLite database at the given DSN and returns a connected Store.
func Open(dsn string) (*SQLite, error) {
	// UTC timestamps, foreign keys and the busy timeout are DSN options so
	// that they hold for every connection, including the ones database/sql
	// opens later to replace a discarded one; see connection.go.
	db, err := sql.Open("sqlite", connectionDSN(dsn))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s, err := New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// WithClock returns the store using c as server time. The copy shares the
// database; it lets the wiring give the store the same clock as the
// services, so clamping and server-time edits agree with them.
func (s *SQLite) WithClock(c clock.Clock) *SQLite {
	clocked := *s
	clocked.clock = c
	return &clocked
}

// now is the store's server time.
func (s *SQLite) now() time.Time {
	if s.clock == nil {
		return time.Now()
	}
	return s.clock.Now()
}

// Close closes the underlying database connection.
func (s *SQLite) Close() error {
	return s.db.Close()
}

// Ping checks the database connection health.
func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

var _ Store = (*SQLite)(nil)
var _ APITokenRepo = (*SQLite)(nil)
var _ PodcastRepo = (*SQLite)(nil)
var _ ProgressTransactionStore = (*SQLite)(nil)

type sqlScanner interface {
	Scan(dest ...any) error
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type sqlQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlExecQueryRower runs statements and single-row queries.
type sqlExecQueryRower interface {
	sqlExecer
	sqlQueryRower
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}
