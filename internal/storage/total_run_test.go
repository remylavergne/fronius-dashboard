// The "since monitoring" baseline follows the counter's continuity: a decrease
// (device reset, another inverter writing to the same database) starts a new
// run, because the energy before it cannot be recovered from the counter.
package storage

import (
	"context"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

func insertRunSample(t *testing.T, s *Store, ts time.Time, wh float64, note bool) {
	t.Helper()
	sm := model.Sample{
		TS:            ts,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		EnergyTotalWh: &wh,
	}
	if err := s.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample(%s): %v", ts, err)
	}
	if note {
		if err := s.NoteTotalRun(context.Background(), sm); err != nil {
			t.Fatalf("NoteTotalRun(%s): %v", ts, err)
		}
	}
}

func TestSinceBaselineFindsTheLastCounterDecrease(t *testing.T) {
	s := openStore(t, time.UTC)
	ctx := context.Background()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	reset := base.Add(2 * time.Hour)

	insertRunSample(t, s, base, 17_000_000, false)
	insertRunSample(t, s, base.Add(time.Hour), 17_000_500, false)
	insertRunSample(t, s, reset, 15_000_000, false)
	insertRunSample(t, s, base.Add(3*time.Hour), 15_000_250, false)

	wh, ts, ok, err := s.SinceBaseline(ctx)
	if err != nil {
		t.Fatalf("SinceBaseline: %v", err)
	}
	if !ok {
		t.Fatal("a baseline must exist")
	}
	if wh != 15_000_000 || !ts.Equal(reset) {
		t.Fatalf("baseline = (%v, %s), want (15000000, %s)", wh, ts, reset)
	}
}

func TestNoteTotalRunMovesTheBaselineWhenTheCounterDrops(t *testing.T) {
	s := openStore(t, time.UTC)
	ctx := context.Background()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	reset := base.Add(2 * time.Hour)

	insertRunSample(t, s, base, 17_000_000, true)
	insertRunSample(t, s, base.Add(time.Hour), 17_000_500, true)

	wh, ts, ok, err := s.SinceBaseline(ctx)
	if err != nil || !ok {
		t.Fatalf("SinceBaseline: (%v, %v, %v)", wh, ok, err)
	}
	if wh != 17_000_000 || !ts.Equal(base) {
		t.Fatalf("baseline = (%v, %s), want the first sample", wh, ts)
	}

	insertRunSample(t, s, reset, 15_000_000, true)

	wh, ts, ok, err = s.SinceBaseline(ctx)
	if err != nil || !ok {
		t.Fatalf("SinceBaseline after the reset: (%v, %v, %v)", wh, ok, err)
	}
	if wh != 15_000_000 || !ts.Equal(reset) {
		t.Fatalf("baseline = (%v, %s), want the reset sample", wh, ts)
	}
}

func TestNoteTotalRunIgnoresBackfilledSamples(t *testing.T) {
	s := openStore(t, time.UTC)
	ctx := context.Background()
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	reset := base.Add(2 * time.Hour)

	insertRunSample(t, s, base, 17_000_000, true)
	insertRunSample(t, s, reset, 15_000_000, true)
	insertRunSample(t, s, base.Add(3*time.Hour), 15_000_250, true)

	// A backfill inserts an older sample with a lower total: it is history, not
	// a reset the collector observed, and must not move the run baseline.
	insertRunSample(t, s, base.Add(30*time.Minute), 5, true)

	wh, ts, ok, err := s.SinceBaseline(ctx)
	if err != nil || !ok {
		t.Fatalf("SinceBaseline: (%v, %v, %v)", wh, ok, err)
	}
	if wh != 15_000_000 || !ts.Equal(reset) {
		t.Fatalf("baseline = (%v, %s), want the reset sample untouched", wh, ts)
	}
}
