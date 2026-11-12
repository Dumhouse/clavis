// Package store is Clavis' persistence layer. It wraps database/sql with
// sqlc-style explicit queries and goose migrations. All SQL lives in this
// package so that adding PostgreSQL in M3 stays a drop-in change behind the
// same methods.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// timeFormat is a fixed-width UTC layout so SQLite text comparisons sort
// chronologically. Never store timestamps with a variable number of fractional
// digits.
const timeFormat = "2006-01-02T15:04:05.000000000Z"

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrations embed.FS

// Store owns the database handle.
type Store struct {
	db      *sql.DB
	dialect string
	dsn     string
}

// Open opens (but does not migrate) a SQLite database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: resolve database path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	// SQLite allows one writer; a small pool is plenty and avoids lock churn.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}
	return &Store{db: db, dialect: "sqlite"}, nil
}

// OpenPostgres opens a Postgres database. Queries use SQLite-style ?
// placeholders and are rebound by the driver.
func OpenPostgres(ctx context.Context, dsn string) (*Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("store: postgres DSN is required")
	}
	db, err := openPostgresDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping postgres: %w", err)
	}
	return &Store{db: db, dialect: "postgres", dsn: dsn}, nil
}

// IsPostgres reports whether the store uses Postgres.
func (s *Store) IsPostgres() bool { return s.dialect == "postgres" }

// DB exposes the underlying handle for health checks and advanced queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping verifies the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Migrate applies all pending migrations.
func (s *Store) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if s.dialect == "postgres" {
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("store: set dialect: %w", err)
		}
		if err := goose.UpContext(ctx, s.db, "migrations/postgres"); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		return nil
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("store: set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, s.db, "migrations/sqlite"); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration. Used by tests only.
func (s *Store) MigrateDown(ctx context.Context) error {
	if s.dialect == "postgres" {
		return fmt.Errorf("store: migrate down is not supported on postgres")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("store: set dialect: %w", err)
	}
	if err := goose.DownContext(ctx, s.db, "migrations/sqlite"); err != nil {
		return fmt.Errorf("store: migrate down: %w", err)
	}
	return nil
}

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when a uniqueness constraint is violated.
var ErrConflict = errors.New("store: conflict")

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeFormat, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: parse time %q: %w", s, err)
	}
	return t, nil
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func parseNullableTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
