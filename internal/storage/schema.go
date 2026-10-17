// Package storage provides the SQLite persistence for the supervision.
//
// A single connection is opened for writing (volume: one sample every
// 5 s); the WAL mode remains useful to allow a concurrent read from an
// external tool (sqlite3) without blocking the collector.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure Go driver: CGO_ENABLED=0 remains possible
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// tsLayout is the storage format for timestamps: UTC, millisecond,
// Z suffix. Every write goes through formatTS, every read through parseTS.
const tsLayout = "2006-01-02T15:04:05.000Z"

// Store is the access point to the database.
type Store struct {
	db  *sql.DB
	loc *time.Location
}

// Open opens (or creates) the database, applies the missing migrations and
// returns the store. loc is the installation's timezone, used for the
// grouping by local day.
func Open(path string, loc *time.Location) (*Store, error) {
	if loc == nil {
		loc = time.UTC
	}
	if path == "" {
		return nil, fmt.Errorf("empty database path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// A single writer: avoids SQLITE_BUSY and keeps a simple write order.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, loc: loc}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Location returns the installation's timezone.
func (s *Store) Location() *time.Location { return s.loc }

// DB exposes the underlying connection (tests and diagnostic tools).
func (s *Store) DB() *sql.DB { return s.db }

// migrate applies the migrations not yet recorded.
func (s *Store) migrate(ctx context.Context) error {
	const createVersions = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := s.db.ExecContext(ctx, createVersions); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // lexicographic order follows the numeric prefix

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, version, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int, name, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	for i, stmt := range splitStatements(body) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration %s, statement %d: %w", name, i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, formatTS(time.Now())); err != nil {
		return fmt.Errorf("recording migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration %s: %w", name, err)
	}
	return nil
}

// splitStatements splits a migration file into statements.
//
// Comments are removed BEFORE splitting: a semicolon present in a comment (for
// example "(W, Wh, %); no conversion") would otherwise split the comment line
// in two and leave an un-prefixed comment fragment in the middle of a
// statement.
//
// The migration files are written and versioned in this repository: they
// contain neither trigger nor literal with a semicolon, so this splitting is
// safe here. It avoids depending on the driver's multi-statement handling.
func splitStatements(body string) []string {
	stripped := stripSQLComments(body)
	var out []string
	for _, raw := range strings.Split(stripped, ";") {
		stmt := strings.TrimSpace(raw)
		if stmt == "" {
			continue
		}
		out = append(out, stmt)
	}
	return out
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func migrationVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, ".sql")
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration name %q: numeric prefix and '_' expected", name)
	}
	v, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration name %q: %w", name, err)
	}
	return v, nil
}

// formatTS serialises a timestamp in UTC, to the millisecond.
func formatTS(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format(tsLayout)
}

// parseTS reads a stored timestamp.
//
// The canonical layout is tried first: it is what formatTS writes. A
// timestamp without milliseconds, or with an offset instead of the Z suffix,
// is still valid data (hand-written row, older data) and is accepted through
// time.RFC3339Nano, normalised to UTC. Only a value that neither layout
// accepts is an error: the caller then decides whether it costs the row.
func parseTS(s string) (time.Time, error) {
	if t, err := time.Parse(tsLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}
