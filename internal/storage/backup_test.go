// Tests of the backup and restore of the database.
//
// A backup is only worth having if it can be read back: these tests check the
// snapshot on a second store (not on the file's bytes), restore it into a
// database that already holds other data, and pin the refusals — an existing
// destination, a file that is not a database, a schema that does not match the
// running code.
package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

// seededStore fills a store with one row in every table a restore must carry.
func seededStore(t *testing.T, loc *time.Location, base time.Time) *Store {
	t.Helper()
	s := openStore(t, loc)
	ctx := context.Background()

	for i := range 3 {
		sm := sample(base.Add(time.Duration(i)*time.Hour), 1000+float64(i))
		sm.EnergyTotalWh = new(float64(15_000_000 + float64(i)*1000))
		mustInsert(t, s, sm)
	}
	if err := s.InsertEvent(ctx, model.Event{
		TS:       base.Add(30 * time.Minute),
		Kind:     "collector_failed",
		Severity: model.SeverityWarning,
		Message:  "simulator refused the connection",
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	dev := deviceRecord(base.Add(time.Hour), "Primo GEN24 3.6 Plus")
	dev.IdentityReason = "captured on hardware"
	if err := s.SaveDevice(ctx, dev); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}
	if _, err := s.MarkDeviceEventSeen(ctx, "evt-1", base); err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if err := s.SetState(ctx, "last_success_utc", formatTS(base.Add(time.Hour))); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// Two closed hours plus the open one: the rollup writes agg_hour rows and
	// advances the watermark, which lives in collector_state and must travel
	// with the aggregates.
	if _, err := s.Rollup(ctx, base.Add(4*time.Hour)); err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	return s
}

func TestBackupCanBeReadBackAndHoldsEverything(t *testing.T) {
	loc := brussels(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	src := seededStore(t, loc, base)
	ctx := context.Background()

	dest := filepath.Join(t.TempDir(), "fronius-backup.db")
	if err := src.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if err := VerifyBackup(ctx, dest); err != nil {
		t.Fatalf("VerifyBackup: %v", err)
	}

	// No temporary file is left behind next to the published backup.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(dest) {
			t.Errorf("unexpected leftover %q beside the backup", e.Name())
		}
	}

	// The snapshot stands on its own: a second store reads it, migrations
	// included (the tables are there and no migration has to be applied).
	copyStore, err := Open(dest, loc)
	if err != nil {
		t.Fatalf("Open(%s): %v", dest, err)
	}
	t.Cleanup(func() { _ = copyStore.Close() })

	n, err := copyStore.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if n != 3 {
		t.Errorf("samples in the copy = %d, want 3", n)
	}
	latest, err := copyStore.LatestSample(ctx)
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if !latest.TS.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("latest sample in the copy = %v, want %v", latest.TS, base.Add(2*time.Hour))
	}
	events, err := copyStore.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "collector_failed" {
		t.Errorf("events in the copy = %+v, want the recorded failure", events)
	}
	dev, found, err := copyStore.Device(ctx)
	if err != nil || !found {
		t.Fatalf("Device: found=%v err=%v", found, err)
	}
	if dev.SerialNumber != "SIMULATED-0001" || dev.IdentityReason != "captured on hardware" {
		t.Errorf("device in the copy = %+v", dev)
	}
	state, err := copyStore.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state["last_success_utc"] != formatTS(base.Add(time.Hour)) {
		t.Errorf("last_success_utc in the copy = %q", state["last_success_utc"])
	}
	if state[aggStateKey] == "" {
		t.Error("the aggregate watermark did not travel with the copy")
	}
	var hours int
	if err := copyStore.DB().QueryRowContext(ctx, `SELECT count(*) FROM agg_hour`).Scan(&hours); err != nil {
		t.Fatalf("counting agg_hour: %v", err)
	}
	if hours != 3 {
		t.Errorf("agg_hour rows in the copy = %d, want the 3 hours carrying samples", hours)
	}
	keys, err := copyStore.DeviceEventsSeenCount(ctx)
	if err != nil {
		t.Fatalf("DeviceEventsSeenCount: %v", err)
	}
	if keys != 1 {
		t.Errorf("device event keys in the copy = %d, want 1", keys)
	}
}

func TestBackupReportsWhatItHolds(t *testing.T) {
	loc := brussels(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	src := seededStore(t, loc, base)
	ctx := context.Background()

	dest := filepath.Join(t.TempDir(), "fronius-backup.db")
	if err := src.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	info, err := Inspect(ctx, dest)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	switch {
	case info.Size <= 0:
		t.Errorf("size = %d, want a non-empty file", info.Size)
	case info.Samples != 3:
		t.Errorf("samples = %d, want 3", info.Samples)
	case info.Events != 1:
		t.Errorf("events = %d, want 1", info.Events)
	case info.Hours != 3:
		t.Errorf("hours = %d, want 3", info.Hours)
	case info.Migrations != 7:
		t.Errorf("migrations = %d, want the seven migrations of this schema", info.Migrations)
	case !info.HasFirst || !info.FirstSample.Equal(base):
		t.Errorf("first sample = %v (present=%v), want %v", info.FirstSample, info.HasFirst, base)
	case !info.HasLast || !info.LastSample.Equal(base.Add(2*time.Hour)):
		t.Errorf("last sample = %v (present=%v)", info.LastSample, info.HasLast)
	}
}

func TestBackupNeverOverwritesAnExistingFile(t *testing.T) {
	src := openStore(t, brussels(t))
	mustInsert(t, src, sample(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), 42))

	dest := filepath.Join(t.TempDir(), "keep-me.db")
	const sentinel = "the copy the operator is protecting"
	if err := os.WriteFile(dest, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := src.Backup(context.Background(), dest)
	if !errors.Is(err, ErrBackupExists) {
		t.Fatalf("Backup over an existing file = %v, want ErrBackupExists", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != sentinel {
		t.Errorf("the existing file was modified: %q", body)
	}
}

func TestBackupFailureLeavesNothingBehind(t *testing.T) {
	src := openStore(t, brussels(t))
	mustInsert(t, src, sample(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), 42))

	dir := t.TempDir()
	dest := filepath.Join(dir, "missing", "fronius-backup.db")
	if err := src.Backup(context.Background(), dest); err == nil {
		t.Fatal("Backup into a missing directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed backup left %d entries behind", len(entries))
	}
}

func TestRestoreReplacesTheWholeState(t *testing.T) {
	loc := brussels(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	src := seededStore(t, loc, base)
	ctx := context.Background()

	dest := filepath.Join(t.TempDir(), "fronius-backup.db")
	if err := src.Backup(ctx, dest); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// The target holds other data before the restore: everything of it must be
	// gone afterwards, not merged.
	target := openStore(t, loc)
	other := sample(base.Add(72*time.Hour), 7)
	other.EnergyTotalWh = new(float64(99))
	mustInsert(t, target, other)
	if err := target.InsertEvent(ctx, model.Event{
		TS: base.Add(72 * time.Hour), Kind: "collector_recovered", Severity: model.SeverityInfo, Message: "other",
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	if err := target.Restore(ctx, dest); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	n, err := target.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if n != 3 {
		t.Errorf("samples after the restore = %d, want the 3 of the backup", n)
	}
	latest, err := target.LatestSample(ctx)
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if !latest.TS.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("latest sample after the restore = %v, want %v", latest.TS, base.Add(2*time.Hour))
	}
	events, err := target.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "collector_failed" {
		t.Errorf("events after the restore = %+v, want only the backup's", events)
	}
	if keys, err := target.DeviceEventsSeenCount(ctx); err != nil || keys != 1 {
		t.Errorf("device event keys after the restore = %d (err=%v), want 1", keys, err)
	}
	dev, found, err := target.Device(ctx)
	if err != nil || !found || dev.SerialNumber != "SIMULATED-0001" {
		t.Errorf("device after the restore = %+v (found=%v, err=%v)", dev, found, err)
	}

	// The watermark travelled: the next rollup picks up where the backup
	// stopped instead of recomputing the whole history. It is read here, before
	// the write below — a measurement older than the watermark lowers it on
	// purpose.
	state, err := target.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	srcState, err := src.State(ctx)
	if err != nil {
		t.Fatalf("State (source): %v", err)
	}
	if state[aggStateKey] != srcState[aggStateKey] {
		t.Errorf("watermark after the restore = %q, want %q", state[aggStateKey], srcState[aggStateKey])
	}

	// The restored database is a working one: the collector can write to it and
	// the derived energy still finds its baseline.
	if err := target.InsertSample(ctx, sample(base.Add(3*time.Hour), 1000)); err != nil {
		t.Fatalf("InsertSample after the restore: %v", err)
	}
	if _, _, ok, err := target.TotalEnergyAt(ctx, base.Add(2*time.Hour)); err != nil || !ok {
		t.Errorf("TotalEnergyAt after the restore: ok=%v err=%v", ok, err)
	}

}

func TestRestoreRefusesWhatItCannotTrust(t *testing.T) {
	ctx := context.Background()
	loc := brussels(t)

	t.Run("a file that is not a database", func(t *testing.T) {
		target := openStore(t, loc)
		mustInsert(t, target, sample(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), 1))

		junk := filepath.Join(t.TempDir(), "not-a-db.db")
		if err := os.WriteFile(junk, []byte("this is not a SQLite database\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		err := target.Restore(ctx, junk)
		if err == nil {
			t.Fatal("Restore of a file that is not a database succeeded")
		}
		if !strings.Contains(err.Error(), "not a database") && !strings.Contains(err.Error(), "integrity") {
			t.Errorf("error = %v, want a diagnosis naming the file", err)
		}
		// The target is untouched and usable.
		if n, err := target.SampleCount(ctx); err != nil || n != 1 {
			t.Errorf("after the refusal: n=%d err=%v, want the original row", n, err)
		}
	})

	t.Run("a database with another schema", func(t *testing.T) {
		target := openStore(t, loc)
		mustInsert(t, target, sample(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), 1))

		// A store that stopped at the first migration: the schema the running
		// code writes no longer matches it.
		old := filepath.Join(t.TempDir(), "old.db")
		db := target.DB()
		if _, err := db.ExecContext(ctx, `ATTACH DATABASE ? AS old`, old); err != nil {
			t.Fatalf("ATTACH: %v", err)
		}
		// A minimal stand-in: the migrations table of an older database.
		for _, stmt := range []string{
			`CREATE TABLE old.schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_utc TEXT NOT NULL)`,
			`INSERT INTO old.schema_migrations (version, name, applied_utc) VALUES (1, 'init', '2026-10-16T00:00:00.000Z')`,
			`CREATE TABLE old.samples (ts_utc TEXT NOT NULL, source TEXT NOT NULL PRIMARY KEY)`,
		} {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("preparing the old database (%s): %v", stmt, err)
			}
		}
		if _, err := db.ExecContext(ctx, `DETACH DATABASE old`); err != nil {
			t.Fatalf("DETACH: %v", err)
		}
		err := target.Restore(ctx, old)
		if err == nil {
			t.Fatal("Restore of an older schema succeeded")
		}
		if !strings.Contains(err.Error(), "migration") {
			t.Errorf("error = %v, want a diagnosis naming the migrations", err)
		}
		if n, err := target.SampleCount(ctx); err != nil || n != 1 {
			t.Errorf("after the refusal: n=%d err=%v, want the original row", n, err)
		}
	})

	t.Run("an absent file", func(t *testing.T) {
		target := openStore(t, loc)
		err := target.Restore(ctx, filepath.Join(t.TempDir(), "absent.db"))
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Restore of an absent file = %v, want os.ErrNotExist", err)
		}
	})
}
