package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrBackupExists says the destination of a backup is already a file.
//
// A backup is the last line of defence: silently replacing it would destroy the
// very copy being protected, so the caller is told instead.
var ErrBackupExists = errors.New("storage: backup destination already exists")

// backupTables is the persisted state a restore replaces, in full.
//
// The order has no meaning for the copy (there is no foreign key between these
// tables, the schema only declares `foreign_keys(1)`): it is the list of
// everything the application owns, and a table missing from it would be
// silently left behind by a restore.
var backupTables = []string{
	"samples",
	"events",
	"collector_runs",
	"collector_state",
	"device",
	"device_events_seen",
	"agg_hour",
	"source_probe",
}

// Backup writes a consistent snapshot of the database to dest.
//
// The snapshot is SQLite's own VACUUM INTO: it is taken through the writer's
// connection, so it is consistent with a collector writing concurrently, it is
// compacted, and the result is a standalone database with no WAL sidecar to
// carry along. The file is built under a temporary name and renamed only once
// complete, so an interrupted backup never leaves a half-written file where a
// good one used to be; an existing dest is refused rather than overwritten
// (ErrBackupExists).
func (s *Store) Backup(ctx context.Context, dest string) error {
	if dest == "" {
		return errors.New("storage: empty backup path")
	}
	switch _, err := os.Stat(dest); {
	case err == nil:
		return fmt.Errorf("%w: %s", ErrBackupExists, dest)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("checking %s: %w", dest, err)
	}

	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, filepath.Base(dest)+".partial-*")
	if err != nil {
		return fmt.Errorf("creating the temporary backup next to %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	// VACUUM INTO writes the file itself and requires the target to be absent.
	if err := os.Remove(tmpName); err != nil {
		return fmt.Errorf("clearing %s: %w", tmpName, err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, tmpName); err != nil {
		return fmt.Errorf("snapshotting the database into %s: %w", tmpName, err)
	}
	if err := syncFile(tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("publishing the backup as %s: %w", dest, err)
	}
	complete = true
	syncDir(dir)
	return nil
}

// Restore replaces the whole persisted state with the one of the backup at src.
//
// The copy runs in a single transaction: either every table is replaced or the
// database is left exactly as it was. A backup whose schema differs from this
// store's is refused (the migrations recorded in it must be exactly the ones
// applied here), so a restore can neither drop a column the current code writes
// nor resurrect one it no longer knows. The row counts are compared inside the
// transaction before it commits: a restore that did not carry everything over
// rolls back rather than reporting success.
//
// The collector must not be running: this method rewrites the state another
// process would be writing at the same time (see docs/RUNBOOK.md).
func (s *Store) Restore(ctx context.Context, src string) error {
	if src == "" {
		return errors.New("storage: empty backup path")
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("reading backup %s: %w", src, err)
	}

	if _, err := s.db.ExecContext(ctx, `ATTACH DATABASE ? AS backup`, src); err != nil {
		return fmt.Errorf("attaching %s: %w", src, err)
	}
	// Detach even on the error paths: the connection is pooled (one connection),
	// so a surviving attachment would surface in the next statement.
	defer func() {
		_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE backup`)
	}()

	if err := checkIntegrity(ctx, s.db, "backup"); err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	if err := s.checkSameSchema(ctx); err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}

	want, err := s.tableCounts(ctx, "backup", nil)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("starting the restore: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range backupTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+table); err != nil {
			return fmt.Errorf("clearing %s: %w", table, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO main.`+table+` SELECT * FROM backup.`+table); err != nil {
			return fmt.Errorf("copying %s: %w", table, err)
		}
	}

	got, err := s.tableCounts(ctx, "main", tx)
	if err != nil {
		return err
	}
	for _, table := range backupTables {
		if got[table] != want[table] {
			return fmt.Errorf("restoring %s: %d rows copied, %d in the backup", table, got[table], want[table])
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the restore: %w", err)
	}
	return nil
}

// VerifyBackup opens the file at path read-only and runs SQLite's integrity
// check on it: the cheapest way for an operator to tell a usable backup from a
// truncated one before relying on it.
func VerifyBackup(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("storage: empty backup path")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("reading backup %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	return checkIntegrity(ctx, db, "main")
}

// checkIntegrity runs PRAGMA <schema>.integrity_check and reports the first
// complaint. SQLite answers "ok" when the file is sound; anything else — an
// empty result, "file is not a database" — is an error.
func checkIntegrity(ctx context.Context, db *sql.DB, schema string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA `+schema+`.integrity_check`)
	if err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var seen int
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("integrity check: %w", err)
		}
		seen++
		if line != "ok" {
			return fmt.Errorf("integrity check: %s", line)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if seen == 0 {
		return errors.New("integrity check: no result")
	}
	return nil
}

// checkSameSchema requires the attached backup to carry exactly the migrations
// this store has applied.
func (s *Store) checkSameSchema(ctx context.Context) error {
	mine, err := migrationVersions(ctx, s.db, "main")
	if err != nil {
		return err
	}
	theirs, err := migrationVersions(ctx, s.db, "backup")
	if err != nil {
		return err
	}
	if len(mine) != len(theirs) {
		return fmt.Errorf("the backup has %d migrations, this database %d", len(theirs), len(mine))
	}
	for i := range mine {
		if mine[i] != theirs[i] {
			return fmt.Errorf("the backup carries migration %d where this database has %d", theirs[i], mine[i])
		}
	}
	return nil
}

// migrationVersions lists the applied migration versions of one schema, oldest
// first.
func migrationVersions(ctx context.Context, db *sql.DB, schema string) ([]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM `+schema+`.schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("reading the migrations of %s: %w", schema, err)
	}
	defer func() { _ = rows.Close() }()

	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("reading the migrations of %s: %w", schema, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the migrations of %s: %w", schema, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s carries no migration: not a fronius-supervisor database", schema)
	}
	return out, nil
}

// tableCounts counts the rows of every restored table in one schema, either on
// the connection itself (q nil) or inside a transaction.
func (s *Store) tableCounts(ctx context.Context, schema string, tx *sql.Tx) (map[string]int64, error) {
	query := func(q string, args ...any) *sql.Row {
		if tx != nil {
			return tx.QueryRowContext(ctx, q, args...)
		}
		return s.db.QueryRowContext(ctx, q, args...)
	}

	out := make(map[string]int64, len(backupTables))
	for _, table := range backupTables {
		var n int64
		if err := query(`SELECT count(*) FROM ` + schema + `.` + table).Scan(&n); err != nil {
			return nil, fmt.Errorf("counting %s.%s: %w", schema, table, err)
		}
		out[table] = n
	}
	return out, nil
}

// syncFile flushes a freshly written file to the storage device.
func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s to flush it: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("flushing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

// syncDir flushes a directory entry, so the rename survives a crash. Best
// effort: several file systems refuse to open a directory for this, and the
// backup itself is already durable — only the name would be lost.
func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// BackupInfo describes a backup file. It is what an operator wants to see
// before trusting one: how big it is, when it was taken and what it holds.
type BackupInfo struct {
	Path        string
	Size        int64
	WrittenUTC  time.Time
	Migrations  int
	Samples     int64
	Events      int64
	Hours       int64
	FirstSample time.Time
	LastSample  time.Time
	HasFirst    bool
	HasLast     bool
}

// Inspect opens the backup at path read-only and reports what it contains.
//
// It is deliberately read-only — mode=ro — so that asking questions about a
// backup can never change it, and it refuses a file SQLite cannot read.
func Inspect(ctx context.Context, path string) (BackupInfo, error) {
	var info BackupInfo
	if path == "" {
		return info, errors.New("storage: empty backup path")
	}
	st, err := os.Stat(path)
	if err != nil {
		return info, fmt.Errorf("reading backup %s: %w", path, err)
	}
	info.Path = path
	info.Size = st.Size()
	info.WrittenUTC = st.ModTime().UTC()

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return info, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	if err := checkIntegrity(ctx, db, "main"); err != nil {
		return info, fmt.Errorf("%s: %w", path, err)
	}
	versions, err := migrationVersions(ctx, db, "main")
	if err != nil {
		return info, err
	}
	info.Migrations = len(versions)

	counts, err := (&Store{db: db}).tableCounts(ctx, "main", nil)
	if err != nil {
		return info, err
	}
	info.Samples = counts["samples"]
	info.Events = counts["events"]
	info.Hours = counts["agg_hour"]

	row := db.QueryRowContext(ctx, `SELECT min(ts_utc), max(ts_utc) FROM samples`)
	var first, last sql.NullString
	if err := row.Scan(&first, &last); err != nil {
		return info, fmt.Errorf("reading the sample range of %s: %w", path, err)
	}
	if first.Valid {
		if ts, err := parseTS(first.String); err == nil {
			info.FirstSample, info.HasFirst = ts, true
		}
	}
	if last.Valid {
		if ts, err := parseTS(last.String); err == nil {
			info.LastSample, info.HasLast = ts, true
		}
	}
	return info, nil
}
