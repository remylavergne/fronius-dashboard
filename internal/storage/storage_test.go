package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

func brussels(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func openStore(t *testing.T, loc *time.Location) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fronius.db")
	s, err := Open(path, loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sample(ts time.Time, pv float64) model.Sample {
	return model.Sample{
		TS:       ts,
		TSSource: model.TSSourceDevice,
		Source:   model.SourceSolarAPI,
		Quality:  model.QualityMeasured,
		Mode:     "produce-only",
		PowerPVW: new(pv),
	}
}

func mustInsert(t *testing.T, s *Store, sm model.Sample) {
	t.Helper()
	if err := s.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample(%s): %v", sm.TS, err)
	}
}

func TestSampleKeepsItsFallbackSource(t *testing.T) {
	s := openStore(t, brussels(t))
	ts := time.Date(2026, 10, 16, 15, 17, 23, 0, time.UTC)

	sm := sample(ts, 228.35)
	sm.EnergyDayWh = new(4123.4)
	sm.FallbackSource = "/status/powerflow"
	mustInsert(t, s, sm)

	got, err := s.LatestSample(context.Background())
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.FallbackSource != "/status/powerflow" {
		t.Fatalf("fallback_source = %q, want the internal path", got.FallbackSource)
	}

	// A sample the primary source fills entirely carries no fallback: the empty
	// string is a fact ("nothing was completed"), not a missing value.
	plain := sample(ts.Add(time.Second), 100)
	mustInsert(t, s, plain)
	got, err = s.LatestSample(context.Background())
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.FallbackSource != "" {
		t.Fatalf("fallback_source = %q, want empty", got.FallbackSource)
	}
}

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	loc := brussels(t)
	path := filepath.Join(t.TempDir(), "nested", "dir", "fronius.db")

	first, err := Open(path, loc)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	var version, rows int
	if err := first.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("counting the migrations: %v", err)
	}
	if version != 4 || rows != 4 {
		t.Fatalf("schema_migrations = (version=%d, rows=%d), wanted (4, 4)", version, rows)
	}
	// The WAL mode must be active (proof that the DSN was actually applied).
	var journalMode string
	if err := first.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, wanted wal", journalMode)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Second opening: no migration must be replayed.
	second, err := Open(path, loc)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if err := second.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("counting after second Open: %v", err)
	}
	if rows != 4 {
		t.Fatalf("migrations replayed: %d rows", rows)
	}
	// The tables remain usable.
	if _, err := second.SampleCount(context.Background()); err != nil {
		t.Fatalf("SampleCount after reopening: %v", err)
	}

}

func TestUpsertIsIdempotent(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	ts := time.Date(2026, 10, 16, 7, 17, 43, 0, time.UTC)

	first := sample(ts, 100)
	first.EnergyDayWh = new(1000.0)
	first.Inverters = []model.InverterSample{{ID: "1", DT: 1, PowerW: new(100.0)}}
	mustInsert(t, s, first)

	second := sample(ts, 250)
	second.Quality = model.QualityEstimated
	second.Mode = "produce-only"
	second.EnergyDayWh = new(2000.0)
	second.PowerGridW = new(0.0)
	second.Inverters = []model.InverterSample{{ID: "1", DT: 1, PowerW: new(250.0), EnergyDayWh: new(7.0)}}
	mustInsert(t, s, second)

	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("SampleCount = %d, wanted 1 (upsert on (ts_utc, source))", count)
	}

	got, err := s.LatestSample(ctx)
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.PowerPVW == nil || *got.PowerPVW != 250 {
		t.Fatalf("PowerPVW = %v, wanted 250 (updated)", got.PowerPVW)
	}
	if got.EnergyDayWh == nil || *got.EnergyDayWh != 2000 {
		t.Fatalf("EnergyDayWh = %v", got.EnergyDayWh)
	}
	// An explicit 0 must survive the round-trip (no confusion with NULL).
	if got.PowerGridW == nil || *got.PowerGridW != 0 {
		t.Fatalf("PowerGridW = %v, wanted explicit 0", got.PowerGridW)
	}
	if got.Quality != model.QualityEstimated {
		t.Fatalf("Quality = %q", got.Quality)
	}
	// A field never set must stay nil.
	if got.PowerLoadW != nil || got.PowerBatteryW != nil || got.RelAutonomyPct != nil {
		t.Fatalf("absent fields became non-nil: %+v", got)
	}
	if len(got.Inverters) != 1 || got.Inverters[0].PowerW == nil || *got.Inverters[0].PowerW != 250 {
		t.Fatalf("inverters = %+v", got.Inverters)
	}
	if got.Inverters[0].EnergyDayWh == nil || *got.Inverters[0].EnergyDayWh != 7 {
		t.Fatalf("inverter E_Day = %v", got.Inverters[0].EnergyDayWh)
	}
	if !got.TS.Equal(ts) {
		t.Fatalf("TS = %v, wanted %v", got.TS, ts)
	}
}

func TestInverterAbsentValuesRoundTrip(t *testing.T) {
	s := openStore(t, brussels(t))
	ts := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)

	sm := sample(ts, 10)
	sm.Inverters = []model.InverterSample{
		{ID: "1", DT: 1, PowerW: new(10.0)},     // EnergyDayWh absent
		{ID: "2", DT: 1, EnergyDayWh: new(3.0)}, // PowerW absent
	}
	mustInsert(t, s, sm)

	got, err := s.LatestSample(context.Background())
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if len(got.Inverters) != 2 {
		t.Fatalf("inverters = %+v", got.Inverters)
	}
	if got.Inverters[0].EnergyDayWh != nil {
		t.Fatalf("absent E_Day became %v", *got.Inverters[0].EnergyDayWh)
	}
	if got.Inverters[1].PowerW != nil {
		t.Fatalf("absent P became %v", *got.Inverters[1].PowerW)
	}
}

func TestEmptyStoreReturnsErrNoData(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	if _, err := s.LatestSample(ctx); !errors.Is(err, ErrNoData) {
		t.Fatalf("LatestSample = %v, wanted ErrNoData", err)
	}
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{
		From: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC),
		Step: time.Hour,
	})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("buckets = %d, wanted 0", len(buckets))
	}
	daily, err := s.DailyEnergy(ctx, time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(daily) != 0 {
		t.Fatalf("daily = %d, wanted 0", len(daily))
	}
	events, err := s.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, wanted 0", len(events))
	}
	if _, ok, err := s.LatestRun(ctx); err != nil || ok {
		t.Fatalf("LatestRun = (%v, %v), wanted (empty, nil)", ok, err)
	}
}

func TestTimeSeriesBucketBoundsAndGap(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Origin aligned on a UTC minute.
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	// Bucket 10:00 → 3 samples (100, 200, 300 W).
	for i, pv := range []float64{100, 200, 300} {
		mustInsert(t, s, sample(base.Add(time.Duration(i)*10*time.Second), pv))
	}
	// 10:01 is deliberately left empty (gap).
	// Bucket 10:02 → 1 sample without a PV power value.
	missing := sample(base.Add(2*time.Minute+5*time.Second), 0)
	missing.PowerPVW = nil
	mustInsert(t, s, missing)
	// Exactly at the `to` bound: must be excluded.
	mustInsert(t, s, sample(base.Add(3*time.Minute), 999))

	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{
		From: base,
		To:   base.Add(3 * time.Minute),
		Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("buckets = %d, wanted 2 (the empty bucket must not be emitted)", len(buckets))
	}

	first := buckets[0]
	if !first.Start.Equal(base) {
		t.Fatalf("first bucket = %v, wanted %v", first.Start, base)
	}
	if first.Start.Unix()%60 != 0 {
		t.Fatalf("bucket not aligned on the epoch: %v", first.Start)
	}
	if first.PowerPVAvgW == nil || *first.PowerPVAvgW != 200 {
		t.Fatalf("AVG = %v, wanted 200", first.PowerPVAvgW)
	}
	if first.PowerPVMinW == nil || *first.PowerPVMinW != 100 {
		t.Fatalf("MIN = %v", first.PowerPVMinW)
	}
	if first.PowerPVMaxW == nil || *first.PowerPVMaxW != 300 {
		t.Fatalf("MAX = %v", first.PowerPVMaxW)
	}
	if first.Count != 3 || first.Missing != 0 {
		t.Fatalf("Count=%d Missing=%d, wanted 3 and 0", first.Count, first.Missing)
	}

	second := buckets[1]
	if !second.Start.Equal(base.Add(time.Minute * 2)) {
		t.Fatalf("second bucket = %v, wanted %v (bucket 10:01 absent)", second.Start, base.Add(2*time.Minute))
	}
	if second.PowerPVAvgW != nil || second.PowerPVMinW != nil || second.PowerPVMaxW != nil {
		t.Fatalf("a bucket without a PV value must stay nil: %+v", second)
	}
	if second.Count != 1 || second.Missing != 1 {
		t.Fatalf("Count=%d Missing=%d, wanted 1 and 1", second.Count, second.Missing)
	}
}

func TestTimeSeriesRejectsInvalidStep(t *testing.T) {
	s := openStore(t, brussels(t))
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	for _, step := range []time.Duration{0, -time.Minute, 500 * time.Millisecond} {
		if _, err := s.TimeSeries(context.Background(), TimeSeriesQuery{From: base, To: base.Add(time.Hour), Step: step}); err == nil {
			t.Fatalf("step=%v must be rejected", step)
		}
	}
}

// dailyFixture fills each local day of [from, to[ with one sample
// per real local hour, whose E_Day is the energy accumulated since local
// midnight. The walk is done by adding absolute hours starting from local
// midnight: the DST-transition days therefore count 23 or 25 samples.
//
// The function returns the number of samples inserted per local date.
func dailyFixture(t *testing.T, s *Store, loc *time.Location, from, to time.Time) map[string]int {
	t.Helper()
	perDate := map[string]int{}

	for dayStart := from; dayStart.Before(to); {
		dayEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day()+1, 0, 0, 0, 0, loc)
		date := dayStart.Format("2006-01-02")
		for ts := dayStart; ts.Before(dayEnd); ts = ts.Add(time.Hour) {
			elapsedHours := ts.Sub(dayStart).Hours()
			sm := sample(ts, 1000)
			sm.EnergyDayWh = new(elapsedHours * 1000)
			mustInsert(t, s, sm)
			perDate[date]++
		}
		dayStart = dayEnd
	}
	return perDate
}

func TestDailyEnergyAcrossDSTTransitions(t *testing.T) {
	loc := brussels(t)

	cases := []struct {
		name      string
		from, to  time.Time
		wantHours map[string]int
	}{
		{
			name: "transition to summer time (23 h)",
			from: time.Date(2026, 3, 28, 0, 0, 0, 0, loc),
			to:   time.Date(2026, 3, 31, 0, 0, 0, 0, loc),
			wantHours: map[string]int{
				"2026-03-28": 24,
				"2026-03-29": 23, // 02:00 → 03:00 local do not exist
				"2026-03-30": 24,
			},
		},
		{
			name: "transition to winter time (25 h)",
			from: time.Date(2026, 10, 24, 0, 0, 0, 0, loc),
			to:   time.Date(2026, 10, 27, 0, 0, 0, 0, loc),
			wantHours: map[string]int{
				"2026-10-24": 24,
				"2026-10-25": 25, // 03:00 → 02:00 local: one hour duplicated
				"2026-10-26": 24,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t, loc)
			ctx := context.Background()
			perDate := dailyFixture(t, s, loc, tc.from, tc.to)

			// The fixture must really exercise the DST transition.
			for date, want := range tc.wantHours {
				if perDate[date] != want {
					t.Fatalf("fixture: %s = %d samples, wanted %d", date, perDate[date], want)
				}
			}

			daily, err := s.DailyEnergy(ctx, tc.from, tc.to)
			if err != nil {
				t.Fatalf("DailyEnergy: %v", err)
			}
			if len(daily) != len(tc.wantHours) {
				t.Fatalf("days = %d (%v), wanted %d", len(daily), daily, len(tc.wantHours))
			}

			seen := map[string]int{}
			for _, d := range daily {
				seen[d.Date]++
				if d.EnergyWh == nil {
					t.Fatalf("%s: energy_wh is nil", d.Date)
				}
				// The day's maximum is reached at the second-to-last hourly
				// sample of the local day, that is (hours − 1) × 1000 Wh.
				wantWh := float64(tc.wantHours[d.Date]-1) * 1000
				if *d.EnergyWh != wantWh {
					t.Fatalf("%s: energy_wh = %v, wanted %v", d.Date, *d.EnergyWh, wantWh)
				}
			}
			for date, n := range seen {
				if n != 1 {
					t.Fatalf("day %s emitted %d times", date, n)
				}
			}
			for date := range tc.wantHours {
				if seen[date] == 0 {
					t.Fatalf("day %s missing", date)
				}
			}
			// Increasing order, without a gap.
			for i := 1; i < len(daily); i++ {
				if daily[i-1].Date >= daily[i].Date {
					t.Fatalf("day order not increasing: %v", daily)
				}
			}
		})
	}
}

// TestDailyEnergyCounterReset: the reset of E_Day at local midnight must
// not produce negative energy nor a duplicated day.
//
// In summer, local midnight corresponds to 22:00 UTC the previous day: the
// instants below are chosen on either side of that boundary.
func TestDailyEnergyCounterReset(t *testing.T) {
	loc := brussels(t)
	s := openStore(t, loc)
	ctx := context.Background()

	inserts := []struct {
		ts   time.Time
		eDay float64
		note string
	}{
		{time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC), 20000, "10-15 01:00 local"},
		{time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC), 20000, "10-15 14:00 local"},
		{time.Date(2026, 10, 15, 22, 30, 0, 0, time.UTC), 0, "10-16 00:30 local (meter reset)"},
		{time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC), 1000, "10-16 01:30 local"},
	}
	for _, ins := range inserts {
		sm := sample(ins.ts, 500)
		sm.EnergyDayWh = new(ins.eDay)
		mustInsert(t, s, sm)
	}

	daily, err := s.DailyEnergy(ctx,
		time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(daily) != 2 {
		t.Fatalf("days = %+v, wanted 2 (15 and 16 October local)", daily)
	}
	if daily[0].Date != "2026-10-15" || daily[1].Date != "2026-10-16" {
		t.Fatalf("dates = %v", daily)
	}
	if daily[0].EnergyWh == nil || *daily[0].EnergyWh != 20000 {
		t.Fatalf("10-15: %v, wanted 20000", daily[0].EnergyWh)
	}
	if daily[1].EnergyWh == nil || *daily[1].EnergyWh != 1000 {
		t.Fatalf("10-16: %v, wanted 1000 (maximum after reset)", daily[1].EnergyWh)
	}
}

func TestEventsAreOrderedAndLimited(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

	for i := range 5 {
		err := s.InsertEvent(ctx, model.Event{
			TS:       base.Add(time.Duration(i) * time.Minute),
			Kind:     model.EventCollectorError,
			Severity: model.SeverityWarning,
			Message:  "failure",
			Detail:   string(rune('a' + i)),
		})
		if err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	got, err := s.Events(ctx, EventsQuery{Limit: 3})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limit ignored: %d events", len(got))
	}
	if !got[0].TS.Equal(base.Add(4 * time.Minute)) {
		t.Fatalf("the most recent must come first: %v", got[0].TS)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].TS.Before(got[i].TS) {
			t.Fatalf("decreasing order expected: %v", got)
		}
	}

	// Range bounds.
	inRange, err := s.Events(ctx, EventsQuery{
		Limit: 10,
		From:  base.Add(time.Minute),
		To:    base.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Events bounded: %v", err)
	}
	if len(inRange) != 2 {
		t.Fatalf("events in range = %d, wanted 2", len(inRange))
	}
}

func TestRunRecords(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

	// No cycle recorded.
	if _, ok, err := s.LatestRun(ctx); err != nil || ok {
		t.Fatalf("initial LatestRun = (%v, %v)", ok, err)
	}

	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base, DurationMS: 12, OK: true, HTTPStatus: 200,
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base.Add(time.Minute), DurationMS: 5003, OK: false,
		HTTPStatus: 503, ErrorKind: "server", Error: "service unavailable",
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	got, ok, err := s.LatestRun(ctx)
	if err != nil || !ok {
		t.Fatalf("LatestRun = (%v, %v)", ok, err)
	}
	if got.OK || got.HTTPStatus != 503 || got.ErrorKind != "server" || got.Error != "service unavailable" {
		t.Fatalf("last cycle = %+v", got)
	}
	if got.DurationMS != 5003 {
		t.Fatalf("DurationMS = %d", got.DurationMS)
	}
	if !got.StartedUTC.Equal(base.Add(time.Minute)) {
		t.Fatalf("StartedUTC = %v", got.StartedUTC)
	}

	// An absent HTTP status stays 0 (NULL column) without breaking the read.
	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base.Add(2 * time.Minute), DurationMS: 1, OK: false,
		ErrorKind: "transport", Error: "connection refused",
	}); err != nil {
		t.Fatalf("InsertRun without status: %v", err)
	}
	got, _, err = s.LatestRun(ctx)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if got.HTTPStatus != 0 || got.ErrorKind != "transport" {
		t.Fatalf("cycle without status = %+v", got)
	}
}

func TestSetStateAndState(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	state, err := s.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state == nil || len(state) != 0 {
		t.Fatalf("initial State = %v, wanted a non-nil empty map", state)
	}

	if err := s.SetState(ctx, "last_success_utc", "2026-10-16T08:00:00.000Z"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, "last_error", "first"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, "last_error", "second"); err != nil {
		t.Fatalf("SetState (update): %v", err)
	}

	state, err = s.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(state) != 2 {
		t.Fatalf("State = %v, wanted 2 entries", state)
	}
	if state["last_error"] != "second" {
		t.Fatalf("last_error = %q", state["last_error"])
	}
	if state["last_success_utc"] != "2026-10-16T08:00:00.000Z" {
		t.Fatalf("last_success_utc = %q", state["last_success_utc"])
	}
}

func TestPurgeBefore(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

	for i := range 3 {
		mustInsert(t, s, sample(base.Add(time.Duration(i)*time.Hour), float64(i)))
		if err := s.InsertEvent(ctx, model.Event{
			TS: base.Add(time.Duration(i) * time.Hour), Kind: "test", Severity: model.SeverityInfo, Message: "m",
		}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
		if err := s.InsertRun(ctx, RunRecord{StartedUTC: base.Add(time.Duration(i) * time.Hour), OK: true}); err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
	}

	deleted, err := s.PurgeBefore(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("purged samples = %d, wanted 1", deleted)
	}

	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("remaining samples = %d, wanted 2", count)
	}
	events, err := s.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("remaining events = %d, wanted 2", len(events))
	}
	latest, _, err := s.LatestRun(ctx)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if !latest.StartedUTC.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("cycles not purged: %v", latest.StartedUTC)
	}

	// Purge of an empty range: no error, no effect.
	deleted, err = s.PurgeBefore(ctx, base)
	if err != nil {
		t.Fatalf("empty PurgeBefore: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("purged samples = %d, wanted 0", deleted)
	}
}

func TestTimestampFormattingIsStable(t *testing.T) {
	// Two instants less than a millisecond apart share the same key:
	// the upsert merges them, the stored precision being the millisecond.
	ts := time.Date(2026, 10, 16, 7, 17, 43, 123456789, time.UTC)
	if got, want := formatTS(ts), "2026-10-16T07:17:43.123Z"; got != want {
		t.Fatalf("formatTS = %q, wanted %q", got, want)
	}
	back, err := parseTS("2026-10-16T07:17:43.123Z")
	if err != nil {
		t.Fatalf("parseTS: %v", err)
	}
	if !back.Equal(ts.Truncate(time.Millisecond)) {
		t.Fatalf("round-trip = %v", back)
	}
	if back.Location() != time.UTC {
		t.Fatalf("the re-read timestamp must be in UTC, got %v", back.Location())
	}
	// The lexicographic order follows the chronological order.
	if formatTS(ts.Add(time.Second)) <= formatTS(ts) {
		t.Fatal("the lexicographic order does not follow the chronological order")
	}
	if _, err := parseTS("2026-10-16 07:17:43"); err == nil {
		t.Fatal("a non-conforming format must be rejected")
	}
}

func TestSplitStatements(t *testing.T) {
	// The semicolon inside the comment is the case that actually
	// broke the migration application: it must be removed with the rest
	// of the comment.
	body := "-- comment\n-- units (W, Wh, %); no conversion\nCREATE TABLE a (x INTEGER);\n\n-- other\nCREATE INDEX b ON a(x);\n"
	got := splitStatements(body)
	if len(got) != 2 {
		t.Fatalf("statements = %d (%q), wanted 2", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE a") {
		t.Fatalf("unexpected first statement: %q", got[0])
	}
	if !strings.HasPrefix(got[1], "CREATE INDEX b") {
		t.Fatalf("unexpected second statement: %q", got[1])
	}
	for _, stmt := range got {
		if strings.Contains(stmt, "no conversion") {
			t.Fatalf("a comment fragment survived: %q", stmt)
		}
	}
}

// ---------------------------------------------------------------------------
// Migration 0002: identity, detection, anti-duplication
// ---------------------------------------------------------------------------

func deviceRecord(ts time.Time, modelName string) DeviceRecord {
	return DeviceRecord{
		ReadAtUTC:         ts,
		Source:            "/status/version",
		Model:             modelName,
		Firmware:          "1.24.3-sim",
		SerialNumber:      "SIMULATED-0001",
		IdentityState:     model.StatePresent,
		MeterState:        model.StatePresent,
		MeterSource:       "/components/PowerMeter/readable",
		BatteryState:      model.StateAbsent,
		BatterySource:     "/components/BatteryManagementSystem/readable",
		DeviceEventsState: model.StatePresent,
	}
}

func TestDeviceIsASingleReplacedRow(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Fresh database: no row, hence "unknown" — not a disguised zero.
	if got, ok, err := s.Device(ctx); err != nil {
		t.Fatalf("Device: %v", err)
	} else if ok {
		t.Fatalf("a fresh database must contain no row: %+v", got)
	}

	first := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	if err := s.SaveDevice(ctx, deviceRecord(first, "Primo GEN24")); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	// Second write: the row is replaced, not duplicated.
	second := first.Add(time.Minute)
	rec := deviceRecord(second, "Primo GEN24 3.6")
	rec.BatteryState = model.StatePresent
	rec.DeviceEventsState = model.StateUnknown
	if err := s.SaveDevice(ctx, rec); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	got, ok, err := s.Device(ctx)
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if !ok {
		t.Fatal("a row is expected after two writes")
	}
	if !got.ReadAtUTC.Equal(second) || got.Model != "Primo GEN24 3.6" {
		t.Fatalf("record = %+v", got)
	}
	if got.MeterState != model.StatePresent || got.BatteryState != model.StatePresent {
		t.Fatalf("states = %+v", got)
	}
	// "unknown" must survive the round-trip: it never becomes
	// "absent".
	if got.DeviceEventsState != model.StateUnknown || got.IdentityState != model.StatePresent {
		t.Fatalf("states = %+v", got)
	}
	if got.SerialNumber != "SIMULATED-0001" || got.Firmware != "1.24.3-sim" {
		t.Fatalf("identity = %+v", got)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM device`).Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Fatalf("rows in the database = %d, wanted 1", count)
	}

	// The CHECK (id = 1) constraint forbids a second row.
	if _, err := s.db.Exec(`INSERT INTO device (id, read_at_utc, source) VALUES (2, '2026-01-01T00:00:00.000Z', 'x')`); err == nil {
		t.Fatal("a second device row must be rejected")
	}
}

func TestMarkDeviceEventSeen(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	ts := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)

	fresh, err := s.MarkDeviceEventSeen(ctx, "device:ABC", ts)
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if !fresh {
		t.Fatal("a new key must be reported as such")
	}

	again, err := s.MarkDeviceEventSeen(ctx, "device:ABC", ts.Add(time.Minute))
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if again {
		t.Fatal("an already seen key must not be reported as new")
	}

	other, err := s.MarkDeviceEventSeen(ctx, "device:DEF", ts)
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if !other {
		t.Fatal("another key must be new")
	}

	count, err := s.DeviceEventsSeenCount(ctx)
	if err != nil {
		t.Fatalf("DeviceEventsSeenCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("remembers keys = %d, wanted 2", count)
	}
}

func TestPurgeRemovesSeenEventMarks(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	old := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	marks := map[string]time.Time{"device:old": old, "device:recent": recent}
	for key, ts := range marks {
		if _, err := s.MarkDeviceEventSeen(ctx, key, ts); err != nil {
			t.Fatalf("MarkDeviceEventSeen: %v", err)
		}
	}

	// Exclusive bound: the mark exactly at the bound survives.
	limit := recent
	if _, err := s.PurgeBefore(ctx, limit); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}

	count, err := s.DeviceEventsSeenCount(ctx)
	if err != nil {
		t.Fatalf("DeviceEventsSeenCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("retained keys = %d, wanted 1 (exclusive upper bound)", count)
	}
}
