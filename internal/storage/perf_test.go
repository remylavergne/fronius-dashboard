package storage

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// TestOneYearTimeSeriesPerformance checks the exit criterion of ADR 0006:
// "a response time under 200 ms over one year of data".
//
// The test is disabled by default: it inserts ≈6.3 million measurements, which
// has no place in an ordinary test suite. It is run on demand:
//
//	FRONIUS_PERF_YEAR=1 go test ./internal/storage/ -run TestOneYearTimeSeriesPerformance -v -timeout 30m
func TestOneYearTimeSeriesPerformance(t *testing.T) {
	if os.Getenv("FRONIUS_PERF_YEAR") == "" {
		t.Skip("FRONIUS_PERF_YEAR not set: one-year measurement skipped")
	}

	const (
		step  = 5 * time.Second // collector cadence
		rows  = 365 * 24 * 3600 / 5
		chunk = 200_000
	)
	start := time.Date(2025, 10, 16, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)

	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Direct fill: the collector's insert loop (one watermark-lowering
	// statement per row) is not what is being measured here.
	insertStart := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO samples (`+sampleColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for i := range rows {
		ts := start.Add(time.Duration(i) * step)
		// Deterministic profile: a daily bell and a sawtooth energy.
		// The value hardly matters, only the volume does.
		secOfDay := float64(ts.Hour()*3600 + ts.Minute()*60 + ts.Second())
		pv := 5000 * (1 + math.Sin(secOfDay/86400*2*math.Pi)) / 2
		eDay := float64(ts.Hour()) * 1000
		if _, err := stmt.ExecContext(ctx,
			formatTS(ts), "solarapi", "device", "measured", "produce-only",
			pv, eDay, 0, 0, nil, nil, nil, nil, nil, "[]"); err != nil {
			t.Fatalf("inserting row %d: %v", i, err)
		}
		if i%chunk == chunk-1 {
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			tx, err = s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			stmt, err = tx.PrepareContext(ctx, `INSERT INTO samples (`+sampleColumns+`)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("final Commit: %v", err)
	}
	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	t.Logf("fill: %d measurements in %s", count, time.Since(insertStart).Round(time.Millisecond))

	// Phase-2 path: direct read of the raw measurements.
	rawHourly := timeQuery(t, s, start, end, time.Hour)
	t.Logf("BEFORE (raw measurements, 1 h step, one year): %s for %d buckets",
		rawHourly.duration.Round(time.Millisecond), rawHourly.buckets)
	rawDaily := dailyQuery(t, s, start, end)
	t.Logf("BEFORE (daily energy, one year): %s for %d days",
		rawDaily.duration.Round(time.Millisecond), rawDaily.buckets)

	rollupStart := time.Now()
	res, err := s.Rollup(ctx, end)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	t.Logf("aggregation of one year: %s (%d buckets)",
		time.Since(rollupStart).Round(time.Millisecond), res.Buckets)

	// Phase-3 path: hourly aggregates. The second measurement includes the open
	// hour, hence one hour of raw measurements, like a real request
	// "up to now".
	aggHourly := timeQuery(t, s, start, end, time.Hour)
	t.Logf("AFTER (aggregates, 1 h step, one year): %s for %d buckets",
		aggHourly.duration.Round(time.Millisecond), aggHourly.buckets)
	aggDaily := dailyQuery(t, s, start, end)
	t.Logf("AFTER (daily energy, one year): %s for %d days",
		aggDaily.duration.Round(time.Millisecond), aggDaily.buckets)
	openTail := timeQuery(t, s, start, end.Add(-time.Minute), time.Hour)
	t.Logf("AFTER with open hour: %s for %d buckets",
		openTail.duration.Round(time.Millisecond), openTail.buckets)
	aggDayStep := timeQuery(t, s, start, end, 24*time.Hour)
	t.Logf("AFTER (1 d step, one year): %s for %d buckets",
		aggDayStep.duration.Round(time.Millisecond), aggDayStep.buckets)

	if rawHourly.duration <= aggHourly.duration {
		t.Logf("warning: the raw path is not slower (%s vs %s)",
			rawHourly.duration.Round(time.Millisecond), aggHourly.duration.Round(time.Millisecond))
	}
	for _, m := range []struct {
		name string
		d    time.Duration
	}{{"1 h step", aggHourly.duration}, {"daily energy", aggDaily.duration}, {"open hour", openTail.duration}, {"1 d step", aggDayStep.duration}} {
		if m.d > 200*time.Millisecond {
			t.Errorf("%s: %s, beyond the 200 ms criterion (ADR 0006)", m.name, m.d.Round(time.Millisecond))
		}
	}
	if aggHourly.buckets != rawHourly.buckets {
		t.Fatalf("aggregated buckets = %d, raw = %d", aggHourly.buckets, rawHourly.buckets)
	}
	if aggDaily.buckets != rawDaily.buckets {
		t.Fatalf("aggregated days = %d, raw = %d", aggDaily.buckets, rawDaily.buckets)
	}
}

type measured struct {
	duration time.Duration
	buckets  int
}

func timeQuery(t *testing.T, s *Store, from, to time.Time, step time.Duration) measured {
	t.Helper()
	start := time.Now()
	buckets, err := s.TimeSeries(context.Background(), TimeSeriesQuery{From: from, To: to, Step: step})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	return measured{duration: time.Since(start), buckets: len(buckets)}
}

func dailyQuery(t *testing.T, s *Store, from, to time.Time) measured {
	t.Helper()
	start := time.Now()
	days, err := s.DailyEnergy(context.Background(), from, to)
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	return measured{duration: time.Since(start), buckets: len(days)}
}
