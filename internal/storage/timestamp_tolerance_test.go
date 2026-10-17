package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

// captureWarnings swaps the default logger for a text handler writing to a
// buffer, so a test can assert the exact number and shape of the data-quality
// warnings. The previous logger is restored when the test ends.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// assertSkipWarning checks that exactly one skip warning was emitted, naming
// the table and the number of rows it cost.
func assertSkipWarning(t *testing.T, buf *bytes.Buffer, table string, skipped int) {
	t.Helper()
	const message = "skipping rows with an unreadable timestamp"
	out := buf.String()
	if got := strings.Count(out, message); got != 1 {
		t.Fatalf("%d skip warnings, want exactly 1:\n%s", got, out)
	}
	if !strings.Contains(out, "table="+table) {
		t.Fatalf("warning does not name table=%s:\n%s", table, out)
	}
	if want := fmt.Sprintf("skipped=%d", skipped); !strings.Contains(out, want) {
		t.Fatalf("warning does not carry %s:\n%s", want, out)
	}
}

// TestParseTSAcceptsEveryValidLayout pins the tolerant policy: the canonical
// layout, a timestamp without milliseconds and an offset-carrying timestamp
// are all valid data; anything else is an error.
func TestParseTSAcceptsEveryValidLayout(t *testing.T) {
	canonical := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"canonical", "2026-10-17T10:00:00.000Z", canonical},
		{"without milliseconds", "2026-10-17T10:00:00Z", canonical},
		{"with an offset", "2026-10-17T12:00:00+02:00", canonical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTS(tc.raw)
			if err != nil {
				t.Fatalf("parseTS(%q): %v", tc.raw, err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("parseTS(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("parseTS(%q) is not in UTC: %v", tc.raw, got.Location())
			}
		})
	}

	for _, raw := range []string{
		"",
		"garbage",
		"2026-10-17 10:00:00",
		"2026-10-17T10:00:00", // no zone: not RFC 3339
	} {
		if _, err := parseTS(raw); err == nil {
			t.Fatalf("parseTS(%q) accepted an unreadable value", raw)
		}
	}
}

// TestEventsSkipsUnreadableTimestamp inserts one malformed row directly with
// SQL, bypassing the writer that only produces the canonical layout: the good
// events must still come back, with one warning for the whole query.
func TestEventsSkipsUnreadableTimestamp(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

	for i, message := range []string{"first", "second"} {
		if err := s.InsertEvent(ctx, model.Event{
			TS:       base.Add(time.Duration(i) * time.Minute),
			Kind:     "test",
			Severity: model.SeverityInfo,
			Message:  message,
		}); err != nil {
			t.Fatalf("InsertEvent(%s): %v", message, err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO events (ts_utc, kind, severity, message) VALUES (?,?,?,?)`,
		"not-a-timestamp", "test", "info", "unreadable"); err != nil {
		t.Fatalf("inserting the malformed event: %v", err)
	}

	buf := captureWarnings(t)
	events, err := s.Events(ctx, EventsQuery{})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("%d events, want the 2 readable ones: %+v", len(events), events)
	}
	for _, e := range events {
		if e.Message == "unreadable" {
			t.Fatal("the row with an unreadable timestamp was returned")
		}
	}
	assertSkipWarning(t, buf, "events", 1)
}

// TestEventsAcceptsTimestampWithoutMilliseconds pins the other half of the
// policy: a hand-written timestamp without milliseconds is data, not damage,
// so it is returned and produces no warning.
func TestEventsAcceptsTimestampWithoutMilliseconds(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	want := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO events (ts_utc, kind, severity, message) VALUES (?,?,?,?)`,
		"2026-10-17T10:00:00Z", "test", "info", "no milliseconds"); err != nil {
		t.Fatalf("inserting the event: %v", err)
	}

	buf := captureWarnings(t)
	events, err := s.Events(ctx, EventsQuery{})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("%d events, want 1: %+v", len(events), events)
	}
	if !events[0].TS.Equal(want) {
		t.Fatalf("event timestamp = %v, want %v", events[0].TS, want)
	}
	if buf.Len() != 0 {
		t.Fatalf("a readable timestamp produced a warning:\n%s", buf.String())
	}
}

// TestLatestSampleSkipsUnreadableTimestamp pins the sample path: the newest
// row carries a malformed timestamp, the latest readable sample is returned.
func TestLatestSampleSkipsUnreadableTimestamp(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

	mustInsert(t, s, sample(base, 10))
	mustInsert(t, s, sample(base.Add(time.Minute), 20))
	// "zzz" sorts after every canonical timestamp, so this row is examined
	// first by the descending order.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO samples (ts_utc, source, ts_source, quality) VALUES (?,?,?,?)`,
		"zzz-unreadable", "solar_api", "device", "measured"); err != nil {
		t.Fatalf("inserting the malformed sample: %v", err)
	}

	buf := captureWarnings(t)
	got, err := s.LatestSample(ctx)
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if want := base.Add(time.Minute); !got.TS.Equal(want) {
		t.Fatalf("LatestSample = %v, want the latest readable sample %v", got.TS, want)
	}
	assertSkipWarning(t, buf, "samples", 1)
}

// TestLatestSampleReportsNoDataWhenEveryTimestampIsUnreadable verifies that
// skipping never fabricates a sample: with nothing readable the result is the
// explicit absence.
func TestLatestSampleReportsNoDataWhenEveryTimestampIsUnreadable(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO samples (ts_utc, source, ts_source, quality) VALUES (?,?,?,?)`,
		"zzz-unreadable", "solar_api", "device", "measured"); err != nil {
		t.Fatalf("inserting the malformed sample: %v", err)
	}

	buf := captureWarnings(t)
	if _, err := s.LatestSample(ctx); !errors.Is(err, ErrNoData) {
		t.Fatalf("LatestSample = %v, want ErrNoData", err)
	}
	assertSkipWarning(t, buf, "samples", 1)
}

// TestTimeSeriesSkipsUnreadableAggregateBucket inserts one agg_hour row whose
// bucket_utc SQLite cannot read: the readable bucket must still be served.
func TestTimeSeriesSkipsUnreadableAggregateBucket(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if err := s.SetState(ctx, aggStateKey, formatTS(base.Add(24*time.Hour))); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	insertAggHour(t, s, formatTS(base.Add(time.Hour)), 2, 20, 5, 15, nil)
	insertAggHour(t, s, "2026-10-16T0A:00:00.000Z", 1, 10, 10, 10, nil)

	buf := captureWarnings(t)
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{
		From: base, To: base.Add(23 * time.Hour), Step: time.Hour,
	})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("%d buckets, want the 1 readable one: %+v", len(buckets), buckets)
	}
	if want := base.Add(time.Hour); !buckets[0].Start.Equal(want) {
		t.Fatalf("bucket start = %v, want %v", buckets[0].Start, want)
	}
	if buckets[0].Count != 2 {
		t.Fatalf("bucket count = %d, want 2", buckets[0].Count)
	}
	assertSkipWarning(t, buf, "agg_hour", 1)
}

// TestDailyEnergySkipsUnreadableAggregateHour inserts one agg_hour row whose
// bucket_utc does not even start with a valid UTC hour: the good day's energy
// must still be returned.
func TestDailyEnergySkipsUnreadableAggregateHour(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if err := s.SetState(ctx, aggStateKey, formatTS(base.Add(24*time.Hour))); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	insertAggHour(t, s, formatTS(base.Add(time.Hour)), 1, 10, 10, 10, new(500.0))
	insertAggHour(t, s, "2026-10-16T0A:00:00.000Z", 1, 10, 10, 10, new(900.0))

	buf := captureWarnings(t)
	days, err := s.DailyEnergy(ctx, base, base.Add(23*time.Hour))
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(days) != 1 {
		t.Fatalf("%d days, want the 1 readable one: %+v", len(days), days)
	}
	if days[0].Date != "2026-10-16" {
		t.Fatalf("day = %q, want 2026-10-16", days[0].Date)
	}
	if days[0].EnergyWh == nil || *days[0].EnergyWh != 500 {
		t.Fatalf("energy = %v, want 500", deref(days[0].EnergyWh))
	}
	assertSkipWarning(t, buf, "agg_hour", 1)
}

// TestDeviceHealthToleratesUnreadableTimestamp pins the single-row contract: a
// bad stored timestamp yields the zero value instead of failing the read, and
// does not warn.
func TestDeviceHealthToleratesUnreadableTimestamp(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO device_health (id, read_at_utc) VALUES (1, ?)`,
		"not-a-timestamp"); err != nil {
		t.Fatalf("inserting the malformed health row: %v", err)
	}

	buf := captureWarnings(t)
	health, ok, err := s.DeviceHealth(ctx)
	if err != nil {
		t.Fatalf("DeviceHealth: %v", err)
	}
	if !ok {
		t.Fatal("DeviceHealth reports no reading although the row exists")
	}
	if !health.ReadAtUTC.IsZero() {
		t.Fatalf("ReadAtUTC = %v, want the zero time", health.ReadAtUTC)
	}
	if buf.Len() != 0 {
		t.Fatalf("a single-row read warned:\n%s", buf.String())
	}
}

// insertAggHour writes one agg_hour row directly, bypassing Rollup so a test
// can seed the exact stored value it needs.
func insertAggHour(t *testing.T, s *Store, bucket string, countAll int, sum, min, max float64, energy *float64) {
	t.Helper()
	var e any
	if energy != nil {
		e = *energy
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO agg_hour (bucket_utc, count_all, count_pv, sum_pv_w, min_pv_w, max_pv_w, max_energy_day_wh)
		 VALUES (?,?,?,?,?,?,?)`,
		bucket, countAll, countAll, sum, min, max, e); err != nil {
		t.Fatalf("inserting agg_hour %q: %v", bucket, err)
	}
}
