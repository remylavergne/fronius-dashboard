package storage

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

// fill inserts samples from from (inclusive) to to (exclusive) by step.
// mk receives the index and the instant; a sample with a zero timestamp is
// skipped, which allows creating a gap.
func fill(t *testing.T, s *Store, from, to time.Time, step time.Duration, mk func(i int, ts time.Time) model.Sample) {
	t.Helper()
	i := 0
	for ts := from; ts.Before(to); ts, i = ts.Add(step), i+1 {
		sm := mk(i, ts)
		if sm.TS.IsZero() {
			continue
		}
		mustInsert(t, s, sm)
	}
}

// pvSample builds a PV power sample, without daily energy.
func pvSample(ts time.Time, pv *float64) model.Sample {
	sm := sample(ts, 0)
	sm.PowerPVW = pv
	return sm
}

// seriesFixture fills a database around a local hour transition, with
// gaps and absent values:
//
//   - 5-minute step over [2026-03-28T21:00Z, 2026-03-29T06:30Z[ ;
//   - a whole hour without any measurement (04:00Z to 05:00Z) ;
//   - absent power values over a 20-minute window ;
//   - a sawtooth daily energy, to test the rule of the maximum
//     per hour then per local date.
//
// The transition to Brussels summer time takes place on 2026-03-29 at 02:00 local
// (01:00Z): the window covers the 23 h night.
func seriesFixture(t *testing.T, s *Store) time.Time {
	t.Helper()
	base := time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)
	const step = 5 * time.Minute

	fill(t, s, base, end, step, func(i int, ts time.Time) model.Sample {
		if ts.Hour() == 4 {
			return model.Sample{} // gap of one whole hour
		}
		sm := sample(ts, 0)
		// Absent values over a 20-minute window.
		if ts.Hour() == 3 && ts.Minute() >= 10 && ts.Minute() < 30 {
			sm.PowerPVW = nil
		} else {
			sm.PowerPVW = new(float64(i%37) * 13.5)
		}
		e := float64(ts.Hour())*100 + float64(ts.Minute())
		sm.EnergyDayWh = &e
		return sm
	})
	return end
}

func closeFloat(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	scale := math.Max(1, math.Max(math.Abs(*a), math.Abs(*b)))
	return math.Abs(*a-*b) <= 1e-9*scale
}

// assertSameBuckets compares two series tolerating the reassociation of
// floats (the sum of hourly aggregates is not summed in the same order
// as the raw sum), but requiring exact equality of the counts.
func assertSameBuckets(t *testing.T, label string, got, want []Bucket) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d buckets, wanted %d\ngot : %+v\nwant: %+v", label, len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.Start.Equal(w.Start) {
			t.Fatalf("%s: bucket %d at %v, wanted %v", label, i, g.Start, w.Start)
		}
		if g.Count != w.Count {
			t.Fatalf("%s: bucket %v, count %d, wanted %d", label, g.Start, g.Count, w.Count)
		}
		if g.Missing != w.Missing {
			t.Fatalf("%s: bucket %v, missing %d, wanted %d", label, g.Start, g.Missing, w.Missing)
		}
		if !closeFloat(g.PowerPVAvgW, w.PowerPVAvgW) {
			t.Fatalf("%s: bucket %v, average %v, wanted %v", label, g.Start, deref(g.PowerPVAvgW), deref(w.PowerPVAvgW))
		}
		if !closeFloat(g.PowerPVMinW, w.PowerPVMinW) {
			t.Fatalf("%s: bucket %v, minimum %v, wanted %v", label, g.Start, deref(g.PowerPVMinW), deref(w.PowerPVMinW))
		}
		if !closeFloat(g.PowerPVMaxW, w.PowerPVMaxW) {
			t.Fatalf("%s: bucket %v, maximum %v, wanted %v", label, g.Start, deref(g.PowerPVMaxW), deref(w.PowerPVMaxW))
		}
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// seriesRanges lists the ranges exercising each branch of the split: a fully
// closed range, a header in the middle of an hour, a tail in the open hour,
// a range without an eligible whole hour, and a range covering an empty hour.
func seriesRanges() []struct {
	name     string
	from, to time.Time
} {
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, time.UTC) }
	return []struct {
		name     string
		from, to time.Time
	}{
		{"all closed", at(0, 0), at(5, 0)},
		{"partial header", at(2, 30), at(5, 0)},
		{"open tail", at(2, 0), at(6, 30)},
		{"no whole hour", at(2, 30), at(3, 30)},
		{"with empty hour", at(3, 0), at(6, 0)},
		{"a single minute", at(2, 30), at(2, 31)},
	}
}

// TestTimeSeriesIdenticalWithAndWithoutAggregates is the phase-3 equivalence test:
// the same request must return the same result whether it is
// served by the hourly aggregates or by the raw measurements.
func TestTimeSeriesIdenticalWithAndWithoutAggregates(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	end := seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	steps := []time.Duration{time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour}
	ranges := seriesRanges()

	// Reference: no watermark exists yet, everything is read raw.
	before := map[string][]Bucket{}
	for _, st := range steps {
		for _, rg := range ranges {
			b, err := s.TimeSeries(ctx, TimeSeriesQuery{From: rg.from, To: rg.to, Step: st})
			if err != nil {
				t.Fatalf("raw TimeSeries (%s, %s): %v", st, rg.name, err)
			}
			before[st.String()+"/"+rg.name] = b
		}
	}

	res, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.To.Sub(res.From) <= 0 {
		t.Fatalf("Rollup aggregated nothing: %+v", res)
	}
	var aggRows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agg_hour`).Scan(&aggRows); err != nil {
		t.Fatalf("counting the aggregates: %v", err)
	}
	if aggRows == 0 {
		t.Fatal("no aggregate row: the test would compare the raw data to itself")
	}

	closed, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("watermark absent after aggregation (ok=%v, err=%v)", ok, err)
	}
	if !closed.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("watermark = %v, wanted %v", closed, now.Truncate(time.Hour))
	}
	// Anti-degenerate-test guard: at least one of the ranges must really be
	// served by the aggregates.
	usedAggregates := false
	for _, rg := range ranges {
		if _, _, ok := aggSplit(rg.from, rg.to, closed, true); ok {
			usedAggregates = true
		}
	}
	if !usedAggregates {
		t.Fatal("no range takes the aggregated path")
	}

	for _, st := range steps {
		for _, rg := range ranges {
			got, err := s.TimeSeries(ctx, TimeSeriesQuery{From: rg.from, To: rg.to, Step: st})
			if err != nil {
				t.Fatalf("TimeSeries after aggregation (%s, %s): %v", st, rg.name, err)
			}
			assertSameBuckets(t, st.String()+"/"+rg.name, got, before[st.String()+"/"+rg.name])
		}
	}

	// The "open tail" range covers the one-hour gap and the last
	// measurement: the empty hour must produce no bucket (no
	// interpolation), and the last bucket must precede the end of the measurements.
	open := before["1h0m0s/open tail"]
	if len(open) != 4 {
		t.Fatalf("%d buckets over the open range, wanted 4: %+v", len(open), open)
	}
	if last := open[len(open)-1].Start; !last.Before(end) {
		t.Fatalf("last bucket %v beyond the last measurement %v", last, end)
	}
}

// TestDailyEnergyIdenticalWithAndWithoutAggregates checks the equivalence of the
// daily energy computation, including over a range split in the middle of a
// local day and over the 23 h day.
func TestDailyEnergyIdenticalWithAndWithoutAggregates(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	loc := brussels(t)
	ranges := []struct {
		name     string
		from, to time.Time
	}{
		{"whole days", time.Date(2026, 3, 28, 0, 0, 0, 0, loc), time.Date(2026, 3, 30, 0, 0, 0, 0, loc)},
		{"cut in the middle of the day", time.Date(2026, 3, 28, 12, 30, 0, 0, loc), time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
		{"open hour included", time.Date(2026, 3, 29, 0, 0, 0, 0, loc), now.Add(30 * time.Minute)},
		{"before any measurement", time.Date(2026, 3, 20, 0, 0, 0, 0, loc), time.Date(2026, 3, 21, 0, 0, 0, 0, loc)},
	}

	before := map[string][]DailyEnergy{}
	for _, rg := range ranges {
		d, err := s.DailyEnergy(ctx, rg.from, rg.to)
		if err != nil {
			t.Fatalf("raw DailyEnergy (%s): %v", rg.name, err)
		}
		before[rg.name] = d
	}
	if len(before["whole days"]) == 0 {
		t.Fatal("no aggregated day: pointless test")
	}

	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	// Anti-degenerate-test guard: the main range must really be
	// served by the aggregates.
	if _, _, ok := aggSplit(ranges[0].from, ranges[0].to, now.Truncate(time.Hour), true); !ok {
		t.Fatal("the reference range does not take the aggregated path")
	}
	for _, rg := range ranges {
		got, err := s.DailyEnergy(ctx, rg.from, rg.to)
		if err != nil {
			t.Fatalf("aggregated DailyEnergy (%s): %v", rg.name, err)
		}
		want := before[rg.name]
		if len(got) != len(want) {
			t.Fatalf("%s: %d days, wanted %d (%v vs %v)", rg.name, len(got), len(want), got, want)
		}
		for i := range want {
			if got[i].Date != want[i].Date {
				t.Fatalf("%s: day %d = %q, wanted %q", rg.name, i, got[i].Date, want[i].Date)
			}
			if !closeFloat(got[i].EnergyWh, want[i].EnergyWh) {
				t.Fatalf("%s: %s = %v, wanted %v", rg.name, got[i].Date,
					deref(got[i].EnergyWh), deref(want[i].EnergyWh))
			}
		}
	}
}

// TestRollupIsIdempotent checks that a second pass over the same range changes
// nothing, and that a resume after another pass does not either.
func TestRollupIsIdempotent(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	first, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("first Rollup: %v", err)
	}
	dump := func(before string) string {
		q := `SELECT bucket_utc, count_all, count_pv, sum_pv_w, min_pv_w, max_pv_w, max_energy_day_wh
			FROM agg_hour`
		var args []any
		if before != "" {
			q += ` WHERE bucket_utc < ?`
			args = append(args, before)
		}
		q += ` ORDER BY bucket_utc`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			t.Fatalf("reading the aggregates: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var (
				bucket            string
				countAll, countPV int
				sum               float64
				min, max, eDay    *float64
			)
			if err := rows.Scan(&bucket, &countAll, &countPV, &sum, &min, &max, &eDay); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, fmt.Sprintf("%s|%d|%d|%v|%v|%v|%v",
				bucket, countAll, countPV, sum, deref(min), deref(max), deref(eDay)))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating the aggregates: %v", err)
		}
		return strings.Join(out, "\n")
	}
	before := dump("")
	if len(before) == 0 {
		t.Fatal("no aggregate written")
	}
	closedAt := formatTS(now.Truncate(time.Hour))

	// Same instant: the pass no longer covers anything and must rewrite nothing.
	second, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("second Rollup: %v", err)
	}
	if second.Buckets != 0 {
		t.Fatalf("second Rollup wrote %d buckets, wanted 0", second.Buckets)
	}
	if got := dump(""); got != before {
		t.Fatalf("aggregates changed by a pass with no work:\n%s\nvs\n%s", got, before)
	}

	// Later with no new reading: only the hour still open at the
	// first pass (06:00Z, which already contains six readings) becomes closed.
	// The already aggregated hours are not rewritten.
	third, err := s.Rollup(ctx, now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("third Rollup: %v", err)
	}
	if third.Buckets != 1 {
		t.Fatalf("third Rollup wrote %d buckets, wanted 1 (closing the open hour)", third.Buckets)
	}
	if want := now.Add(3 * time.Hour).Truncate(time.Hour); !third.Watermark.Equal(want) {
		t.Fatalf("watermark = %v, wanted %v", third.Watermark, want)
	}
	if got := dump(closedAt); got != before {
		t.Fatalf("closed hours rewritten with no new readings:\n%s\nvs\n%s", got, before)
	}
	if first.Buckets == 0 {
		t.Fatal("the first pass wrote nothing")
	}
}

// TestRollupCatchesUpDataInsertedBelowWatermark reproduces the import of a
// history after a first pass: measurements older than the watermark
// must be aggregated, and therefore visible to the aggregated reads.
func TestRollupCatchesUpDataInsertedBelowWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)

	// First day: aggregated immediately.
	fill(t, s, base, base.Add(2*time.Hour), 10*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(1000+i)))
	})
	now := base.Add(4 * time.Hour)
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("first Rollup: %v", err)
	}
	wm, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("watermark absent (ok=%v, err=%v)", ok, err)
	}
	if !wm.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("watermark = %v, wanted %v", wm, now.Truncate(time.Hour))
	}

	// Late import of the previous day (backfill): the measurements are older
	// than the watermark.
	old := base.Add(-3 * time.Hour)
	fill(t, s, old, old.Add(2*time.Hour), 10*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(500+i)))
	})
	after, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("watermark disappeared (ok=%v, err=%v)", ok, err)
	}
	if !after.Before(wm) {
		t.Fatalf("watermark not lowered: %v (before %v)", after, wm)
	}
	if !after.Equal(old.Truncate(time.Hour)) {
		t.Fatalf("watermark = %v, wanted %v", after, old.Truncate(time.Hour))
	}

	// Before the next pass, the aggregated read must nevertheless remain
	// correct: the range is not closed beyond the new watermark.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: old, To: now, Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	raw := 0
	for _, b := range buckets {
		raw += b.Count
	}
	if want := 4 * 6; raw != want { // 4 hours × 6 10-minute readings
		t.Fatalf("samples covered = %d, wanted %d", raw, want)
	}

	// After the pass, the aggregates cover the whole history again.
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("catch-up Rollup: %v", err)
	}
	after, _, err = s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("watermark: %v", err)
	}
	if !after.Equal(wm) {
		t.Fatalf("watermark = %v, wanted %v after catch-up", after, wm)
	}
	var aggCount, rawCount int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(count_all), 0) FROM agg_hour`).Scan(&aggCount); err != nil {
		t.Fatalf("counting the aggregates: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&rawCount); err != nil {
		t.Fatalf("counting the measurements: %v", err)
	}
	if aggCount != rawCount {
		t.Fatalf("aggregates cover %d measurements, %d retained", aggCount, rawCount)
	}
}

// TestRollupNeverAggregatesOpenHour: an hour still being fed is never
// aggregated, otherwise a read would believe it complete while new
// measurements are still arriving.
func TestRollupNeverAggregatesOpenHour(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)

	fill(t, s, base, base.Add(30*time.Minute), time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	res, err := s.Rollup(ctx, base.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.Buckets != 0 {
		t.Fatalf("Rollup aggregated the open hour (%d buckets)", res.Buckets)
	}
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("watermark written without aggregate (ok=%v, err=%v)", ok, err)
	}

	// The read remains correct: it goes entirely through the raw data.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: base, To: base.Add(time.Hour), Step: time.Minute})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 30 {
		t.Fatalf("%d buckets, wanted 30", len(buckets))
	}
}

// TestRollupEmptyStoreWritesNoWatermark: without a measurement, no watermark must
// be written, otherwise a history imported later would be read as already
// aggregated.
func TestRollupEmptyStoreWritesNoWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	res, err := s.Rollup(ctx, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.Buckets != 0 {
		t.Fatalf("buckets = %d, wanted 0", res.Buckets)
	}
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("watermark written on an empty database (ok=%v, err=%v)", ok, err)
	}

	// A history imported afterwards must be aggregated normally.
	base := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	fill(t, s, base, base.Add(3*time.Hour), 15*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	if _, err := s.Rollup(ctx, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Rollup after import: %v", err)
	}
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: base, To: base.Add(3 * time.Hour), Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("%d buckets, wanted 3: %+v", len(buckets), buckets)
	}
}

// TestInsertSampleLowersWatermarkOnlyBelow: the lowering must occur
// only for a measurement really older than the watermark.
func TestInsertSampleLowersWatermarkOnlyBelow(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	// Without a watermark, an insertion does not create one.
	mustInsert(t, s, pvSample(now.Add(-5*time.Hour), new(1.0)))
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("watermark created by an insertion (ok=%v, err=%v)", ok, err)
	}

	if err := s.SetState(ctx, aggStateKey, formatTS(now)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// Later measurement: the watermark does not move.
	mustInsert(t, s, pvSample(now.Add(30*time.Minute), new(2.0)))
	wm, _, err := s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("aggWatermark: %v", err)
	}
	if !wm.Equal(now) {
		t.Fatalf("watermark = %v, wanted %v", wm, now)
	}
	// Earlier measurement: the watermark drops to the lower whole hour.
	mustInsert(t, s, pvSample(now.Add(-90*time.Minute), new(3.0)))
	wm, _, err = s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("aggWatermark: %v", err)
	}
	if want := now.Add(-2 * time.Hour); !wm.Equal(want) {
		t.Fatalf("watermark = %v, wanted %v", wm, want)
	}
}

// TestPurgeRemovesAggregatesAndRaisesWatermark checks that the retention leaves
// the two tables consistent: no aggregate row outlives its measurements.
func TestPurgeRemovesAggregatesAndRaisesWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	fill(t, s, base, base.AddDate(0, 0, 3), time.Hour, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	now := base.AddDate(0, 0, 3).Add(30 * time.Minute)
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	limit := base.AddDate(0, 0, 1)
	if _, err := s.PurgeBefore(ctx, limit); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	var before int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agg_hour WHERE bucket_utc < ?`, formatTS(limit)).Scan(&before); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if before != 0 {
		t.Fatalf("%d aggregates earlier than the purge bound", before)
	}
	wm, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("watermark absent after purge (ok=%v, err=%v)", ok, err)
	}
	if wm.Before(limit) {
		t.Fatalf("watermark %v earlier than the purge bound %v", wm, limit)
	}

	// The retained days remain readable via the aggregated path.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: limit, To: now, Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 48 {
		t.Fatalf("%d buckets, wanted 48", len(buckets))
	}
}

// TestAggSplitBoundaries fixes the split rule: only the whole hours
// contained in [from, to) and closed are served by the aggregates.
func TestAggSplitBoundaries(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, time.UTC) }
	closed := at(5, 0)

	cases := []struct {
		name             string
		from, to         time.Time
		enabled          bool
		wantFrom, wantTo time.Time
		wantOK           bool
	}{
		{"aligned closed range", at(2, 0), at(5, 0), true, at(2, 0), at(5, 0), true},
		{"header in the middle of an hour", at(2, 30), at(5, 0), true, at(3, 0), at(5, 0), true},
		{"open tail", at(2, 0), at(6, 30), true, at(2, 0), at(5, 0), true},
		{"no eligible whole hour", at(2, 30), at(3, 30), true, time.Time{}, time.Time{}, false},
		{"after the watermark", at(6, 0), at(7, 0), true, time.Time{}, time.Time{}, false},
		{"before the watermark", at(1, 0), at(1, 30), true, time.Time{}, time.Time{}, false},
		{"aggregates disabled", at(1, 0), at(5, 0), false, time.Time{}, time.Time{}, false},
		{"empty range", at(3, 0), at(3, 0), true, time.Time{}, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := aggSplit(tc.from, tc.to, closed, tc.enabled)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, wanted %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Fatalf("range = [%v, %v), wanted [%v, %v)", from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

// TestCeilHour covers the ceiling rounding used by the split.
func TestCeilHour(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 3, 29, h, m, s, 0, time.UTC) }
	cases := []struct{ in, want time.Time }{
		{at(3, 0, 0), at(3, 0, 0)},
		{at(3, 0, 1), at(4, 0, 0)},
		{at(3, 59, 59), at(4, 0, 0)},
	}
	for _, tc := range cases {
		if got := ceilHour(tc.in); !got.Equal(tc.want) {
			t.Fatalf("ceilHour(%v) = %v, wanted %v", tc.in, got, tc.want)
		}
	}
}
