// Tests of the fronius-backup command line: the three subcommands are driven
// through main1, so the exit codes, the produced files and the printed summary
// are checked the way an operator would see them.
package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// seedDatabase writes a small history into a database, so that a backup has
// something to carry.
func seedDatabase(t *testing.T, path string, base time.Time, samples int) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	s, err := storage.Open(path, loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	for i := range samples {
		sm := model.Sample{
			TS:       base.Add(time.Duration(i) * time.Hour),
			TSSource: model.TSSourceDevice,
			Source:   model.SourceSolarAPI,
			Quality:  model.QualityMeasured,
			Mode:     "produce-only",
			PowerPVW: new(1000 + float64(i)),
		}
		if err := s.InsertSample(context.Background(), sm); err != nil {
			t.Fatalf("InsertSample: %v", err)
		}
	}
}

// sampleCount reads the number of samples of a database through a fresh store.
func sampleCount(t *testing.T, path string) int64 {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	s, err := storage.Open(path, loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	n, err := s.SampleCount(context.Background())
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	return n
}

func TestSaveThenVerifyThenRestore(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "fronius.db")
	backupFile := filepath.Join(dir, "nightly.db")
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	seedDatabase(t, db, base, 3)

	// save
	var out, errOut bytes.Buffer
	if code := main1(&out, &errOut, []string{"save", "-db", db, "-out", backupFile}); code != 0 {
		t.Fatalf("save exited with %d: %s", code, errOut.String())
	}
	summary := out.String()
	for _, want := range []string{backupFile, "3 samples", "2026-10-16T10:00:00Z", "6 migrations"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the save summary does not mention %q:\n%s", want, summary)
		}
	}

	// verify
	out.Reset()
	errOut.Reset()
	if code := main1(&out, &errOut, []string{"verify", "-file", backupFile}); code != 0 {
		t.Fatalf("verify exited with %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "3 samples") {
		t.Errorf("the verify summary does not report the content:\n%s", out.String())
	}

	// The database moves on after the backup, then is restored: the rows written
	// after the snapshot must be gone.
	seedDatabase(t, db, base.Add(48*time.Hour), 2)
	if n := sampleCount(t, db); n != 5 {
		t.Fatalf("samples before the restore = %d, want 5", n)
	}

	out.Reset()
	errOut.Reset()
	if code := main1(&out, &errOut, []string{"restore", "-db", db, "-from", backupFile, "-yes"}); code != 0 {
		t.Fatalf("restore exited with %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "3 samples") {
		t.Errorf("the restore summary does not report the restored content:\n%s", out.String())
	}
	if n := sampleCount(t, db); n != 3 {
		t.Errorf("samples after the restore = %d, want the 3 of the backup", n)
	}

	// The database that was replaced is kept aside.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var kept []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".pre-restore-") {
			kept = append(kept, e.Name())
		}
	}
	if len(kept) != 1 {
		t.Fatalf("safety copies = %v, want exactly one", kept)
	}
	if n := sampleCount(t, filepath.Join(dir, kept[0])); n != 5 {
		t.Errorf("the safety copy holds %d samples, want the 5 that were replaced", n)
	}
}

func TestRestoreRefusesWithoutConfirmation(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "fronius.db")
	backupFile := filepath.Join(dir, "nightly.db")
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	seedDatabase(t, db, base, 1)
	seedDatabase(t, backupFile, base, 4)

	var out, errOut bytes.Buffer
	code := main1(&out, &errOut, []string{"restore", "-db", db, "-from", backupFile})
	if code != 1 {
		t.Fatalf("restore without -yes exited with %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "-yes") {
		t.Errorf("the refusal does not say how to confirm: %s", errOut.String())
	}
	if n := sampleCount(t, db); n != 1 {
		t.Errorf("the database was modified: %d samples, want the original 1", n)
	}
	// No safety copy is written either: nothing was attempted.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".pre-restore-") {
			t.Errorf("a refused restore wrote %s", e.Name())
		}
	}
}

func TestSaveNeverOverwritesAndVerifyRejectsJunk(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "fronius.db")
	seedDatabase(t, db, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC), 1)

	existing := filepath.Join(dir, "keep.db")
	const sentinel = "not a database: the operator's own copy"
	if err := os.WriteFile(existing, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := main1(&out, &errOut, []string{"save", "-db", db, "-out", existing}); code != 1 {
		t.Fatalf("save over an existing file exited with %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("the refusal does not name the reason: %s", errOut.String())
	}
	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != sentinel {
		t.Errorf("the existing file was replaced: %q", body)
	}

	// verify on something that is not a database
	out.Reset()
	errOut.Reset()
	if code := main1(&out, &errOut, []string{"verify", "-file", existing}); code != 1 {
		t.Fatalf("verify on junk exited with %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Error("verify on junk printed no diagnosis")
	}
}

func TestUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := main1(&out, &errOut, nil); code != 2 {
		t.Errorf("no argument exited with %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("no argument printed no usage: %s", errOut.String())
	}

	errOut.Reset()
	if code := main1(&out, &errOut, []string{"archive", "-out", "x"}); code != 2 {
		t.Errorf("unknown command exited with %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("the message does not name the problem: %s", errOut.String())
	}

	errOut.Reset()
	if code := main1(&out, &errOut, []string{"save"}); code != 2 {
		t.Errorf("save without -out exited with %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "-out") {
		t.Errorf("the message does not name the missing flag: %s", errOut.String())
	}
}
