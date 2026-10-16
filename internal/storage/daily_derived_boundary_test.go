// Boundary and adversarial tests of the per-day energy derived from the stored
// total (daily_derived.go).
//
// The neighbouring daily_derived_test.go fixes the happy path and the coarse
// refusals; these cases pin the edges a rewrite would silently move: the span of
// a 23 h and a 25 h local day, the exact tolerance boundary, the refusals by
// their kind (not their wording), the boundary instant itself, ties and
// out-of-order writes, the purge, and the agreement with the day KPI computed
// from the device counter.
package storage

import (
	"context"
	"testing"
	"time"
)

// totalSample inserts one sample carrying a total energy, and optionally the
// device's own day counter as well.
func totalSample(t *testing.T, s *Store, ts time.Time, total float64, eDay *float64) {
	t.Helper()
	sm := sample(ts, 1000)
	sm.EnergyTotalWh = new(total)
	sm.EnergyDayWh = eDay
	mustInsert(t, s, sm)
}

// derivedDay returns the entry of a derived-day slice by local date.
func derivedDay(t *testing.T, got []DailyDerivedEnergy, date string) DailyDerivedEnergy {
	t.Helper()
	for _, d := range got {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("no day %s in %+v", date, got)
	return DailyDerivedEnergy{}
}

// wantDerived asserts that a day was measured, at the exact value.
func wantDerived(t *testing.T, got []DailyDerivedEnergy, date string, wh float64) {
	t.Helper()
	d := derivedDay(t, got, date)
	if d.Wh == nil {
		t.Fatalf("day %s: want %v, got no value (reason %q)", date, wh, d.Reason)
	}
	if *d.Wh != wh {
		t.Fatalf("day %s = %v, want %v", date, *d.Wh, wh)
	}
}

// wantRefused asserts that a day carries no value and does carry a cause —
// never a number, and never a value without its reason.
func wantRefused(t *testing.T, got []DailyDerivedEnergy, date string) {
	t.Helper()
	d := derivedDay(t, got, date)
	if d.Wh != nil {
		t.Fatalf("day %s = %v, want no value", date, *d.Wh)
	}
	if d.Reason == "" {
		t.Fatalf("day %s: no value and no reason", date)
	}
}

// TestLocalMidnightSpansFollowTheOffsetChange pins the two transition days
// directly on the boundary arithmetic: the day boundary is a local wall clock,
// so it is 23 h once a year and 25 h once a year — never a fixed 24 h.
func TestLocalMidnightSpansFollowTheOffsetChange(t *testing.T) {
	s := openStore(t, brussels(t))

	// 2026-03-29: the clocks jump from 02:00 to 03:00 (CET -> CEST).
	spring := s.localMidnight(time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC); !spring.Equal(want) {
		t.Errorf("spring midnight = %v, want %v", spring, want)
	}
	if span := s.nextLocalMidnight(spring).Sub(spring); span != 23*time.Hour {
		t.Errorf("spring-forward day = %v, want 23h", span)
	}

	// 2026-10-25: the clocks fall from 03:00 back to 02:00 (CEST -> CET).
	autumn := s.localMidnight(time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
	if want := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC); !autumn.Equal(want) {
		t.Errorf("autumn midnight = %v, want %v", autumn, want)
	}
	if span := s.nextLocalMidnight(autumn).Sub(autumn); span != 25*time.Hour {
		t.Errorf("fall-back day = %v, want 25h", span)
	}
}

// TestDailyEnergyFromTotalSpansTheShortLocalDay: on the 23 h spring-forward day
// the end reading sits one minute before a midnight that comes an hour early.
// Read as a fixed 24 h window, that reading would look an hour stale and the day
// would be refused as if samples were missing.
func TestDailyEnergyFromTotalSpansTheShortLocalDay(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC) // 2026-03-29 00:00 +01:00
	end := time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC)   // 2026-03-30 00:00 +02:00
	if span := end.Sub(start); span != 23*time.Hour {
		t.Fatalf("fixture day = %v, want 23h", span)
	}

	totalSample(t, s, start.Add(-time.Minute), 1000, nil)
	totalSample(t, s, end.Add(-time.Minute), 1300, nil) // 23:59 local +02:00

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("days = %+v, want one", got)
	}
	wantDerived(t, got, "2026-03-29", 300)
	if got[0].Partial {
		t.Errorf("the finished 23 h day is marked partial")
	}
}

// TestDailyEnergyFromTotalGivesTheLastHourToTheLongDay: on the 25 h fall-back
// day the last hour runs up to a midnight that comes an hour late. A fixed 24 h
// window would cut the day at 22:00Z and hand the 22:55Z reading — 23:55 local —
// to the next day.
func TestDailyEnergyFromTotalGivesTheLastHourToTheLongDay(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC) // 2026-10-25 00:00 +02:00
	end := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)   // 2026-10-26 00:00 +01:00
	if span := end.Sub(start); span != 25*time.Hour {
		t.Fatalf("fixture day = %v, want 25h", span)
	}

	totalSample(t, s, start.Add(-time.Minute), 2000, nil)
	totalSample(t, s, start.Add(12*time.Hour), 2300, nil) // 22:00Z, 23:00 local (+02:00)
	totalSample(t, s, end.Add(-5*time.Minute), 2600, nil)
	// 22:55Z is 23:55 local (+01:00): the last reading, in the day's final hour.

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("days = %+v, want one", got)
	}
	// 2600 - 2000: the final-hour reading is the end of THIS day.
	wantDerived(t, got, "2026-10-25", 600)
}

// TestDailyEnergyFromTotalToleranceBoundary pins the edge itself: a baseline (and
// an end reading) exactly `tolerance` away is accepted, one second beyond it is
// refused with a cause. The boundary is asserted, not the message.
func TestDailyEnergyFromTotalToleranceBoundary(t *testing.T) {
	ctx := context.Background()
	const tolerance = 10 * time.Minute
	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)

	// Exactly at the edge, on both sides of the day: accepted.
	atEdge := openStore(t, brussels(t))
	totalSample(t, atEdge, start.Add(-tolerance), 1000, nil)
	totalSample(t, atEdge, end.Add(-tolerance), 1500, nil)
	got, err := atEdge.DailyEnergyFromTotal(ctx, start, end, end, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantDerived(t, got, "2026-10-16", 500)

	// One second too old, at the start: refused.
	staleBase := openStore(t, brussels(t))
	totalSample(t, staleBase, start.Add(-tolerance-time.Second), 1000, nil)
	totalSample(t, staleBase, end.Add(-time.Minute), 1500, nil)
	got, err = staleBase.DailyEnergyFromTotal(ctx, start, end, end, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantRefused(t, got, "2026-10-16")

	// One second too old, at the end: refused.
	staleEnd := openStore(t, brussels(t))
	totalSample(t, staleEnd, start.Add(-time.Minute), 1000, nil)
	totalSample(t, staleEnd, end.Add(-tolerance-time.Second), 1500, nil)
	got, err = staleEnd.DailyEnergyFromTotal(ctx, start, end, end, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantRefused(t, got, "2026-10-16")
}

// TestDailyEnergyFromTotalOwnsTheBoundaryReading: a reading at exactly the next
// local midnight lies one instant after the day's end and is yet the day's end
// reading (the comparison is inclusive); it is also the baseline of the day that
// begins there, and the two usages do not double-count.
func TestDailyEnergyFromTotalOwnsTheBoundaryReading(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	nextEnd := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)

	totalSample(t, s, start.Add(-time.Minute), 1000, nil)
	totalSample(t, s, end, 1900, nil) // exactly at the boundary instant
	totalSample(t, s, nextEnd.Add(-time.Minute), 2100, nil)

	got, err := s.DailyEnergyFromTotal(ctx, start, nextEnd, nextEnd, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("days = %+v, want two", got)
	}
	wantDerived(t, got, "2026-10-16", 900) // 1900 - 1000, end reading at 22:00Z
	wantDerived(t, got, "2026-10-17", 200) // 2100 - 1900, same reading as baseline
}

// TestDailyEnergyFromTotalRefusesABackwardsTotalByKind asserts the refusal by its
// kind rather than by its wording, and fixes the two sides of the comparison: a
// counter that dropped yields nothing, a counter that merely stood still yields a
// legitimate zero.
func TestDailyEnergyFromTotalRefusesABackwardsTotalByKind(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)

	backwards := openStore(t, brussels(t))
	totalSample(t, backwards, start.Add(-time.Minute), 5000, nil)
	totalSample(t, backwards, end.Add(-time.Minute), 10, nil)
	got, err := backwards.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantRefused(t, got, "2026-10-16")

	still := openStore(t, brussels(t))
	totalSample(t, still, start.Add(-time.Minute), 5000, nil)
	totalSample(t, still, end.Add(-time.Minute), 5000, nil)
	got, err = still.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantDerived(t, got, "2026-10-16", 0)
}

// TestDailyEnergyFromTotalMaterialisesAGap pins the contract the API relies on: a
// day without a single sample, inside a range where other days do have data, is
// still emitted — with no value and its cause — while the device-counter path
// yields nothing for that same day.
func TestDailyEnergyFromTotalMaterialisesAGap(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC) // 2026-10-16 local
	end := time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC)   // 2026-10-19 local

	totalSample(t, s, start.Add(-time.Minute), 1000, nil)
	totalSample(t, s, start.Add(24*time.Hour-time.Minute), 1500, nil) // end of 10-16
	// No sample at all during 2026-10-17.

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("days = %d (%+v), want 3", len(got), got)
	}
	wantDerived(t, got, "2026-10-16", 500)
	// 2026-10-17 has no reading at all, yet it is emitted with its cause; the
	// day after it cannot derive either, since its baseline would be a day old.
	wantRefused(t, got, "2026-10-17")
	wantRefused(t, got, "2026-10-18")

	// The device-counter path only emits days it has energy for: the same range
	// holds no E_Day here, so it stays empty — the empty state the API states.
	measured, err := s.DailyEnergy(ctx, start, end)
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(measured) != 0 {
		t.Errorf("device-counter path = %+v, want empty", measured)
	}
}

// TestDailyEnergyFromTotalMaterialisesWithoutAnySample: a valid range holding no
// measurement at all is still walked day by day (each day refused for lack of a
// baseline), while an inverted range is rejected outright.
func TestDailyEnergyFromTotalMaterialisesWithoutAnySample(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("days = %+v, want the two days of the range", got)
	}
	wantRefused(t, got, "2026-10-16")
	wantRefused(t, got, "2026-10-17")

	if got, err := s.DailyEnergyFromTotal(ctx, start, start, start, 10*time.Minute); err == nil || got != nil {
		t.Fatalf("empty range = (%+v, %v), want (nil, error)", got, err)
	}
}

// TestDailyEnergyFromTotalTakesTheLatestAtEachBoundary writes samples out of
// chronological order and replays a timestamp: the boundary must resolve to the
// latest reading at or before it, whatever the insertion order, and a duplicate
// timestamp must not shadow the corrected value.
func TestDailyEnergyFromTotalTakesTheLatestAtEachBoundary(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)

	totalSample(t, s, end.Add(-time.Minute), 2000, nil)      // end reading, written first
	totalSample(t, s, start.Add(-time.Minute), 999, nil)     // baseline, first value
	totalSample(t, s, start.Add(-30*time.Minute), 1000, nil) // older than the baseline
	totalSample(t, s, start.Add(-time.Minute), 1200, nil)    // replay: corrects the baseline

	base, ts, ok, err := s.TotalEnergyAt(ctx, start)
	if err != nil || !ok || base != 1200 || !ts.Equal(start.Add(-time.Minute)) {
		t.Fatalf("baseline = (%v, %v, %v, %v), want (1200, %v, true, nil)",
			base, ts, ok, err, start.Add(-time.Minute))
	}

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantDerived(t, got, "2026-10-16", 800) // 2000 - 1200
}

// TestDailyEnergyFromTotalNeverMeasuresAPurgedDay: once the baseline is gone the
// old day has no value and says why, while a recent day whose samples survive
// still derives its value — the purge never turns a missing baseline into a
// truncated, wrong number.
func TestDailyEnergyFromTotalNeverMeasuresAPurgedDay(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	oldStart := time.Date(2026, 10, 9, 22, 0, 0, 0, time.UTC)  // 2026-10-10 local
	oldEnd := time.Date(2026, 10, 10, 22, 0, 0, 0, time.UTC)   // 2026-10-11 local
	newStart := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC) // 2026-10-17 local
	newEnd := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)   // 2026-10-18 local

	totalSample(t, s, oldStart.Add(-time.Minute), 100, nil)
	totalSample(t, s, oldEnd.Add(-time.Minute), 400, nil)
	totalSample(t, s, newStart.Add(-time.Minute), 1000, nil)
	totalSample(t, s, newEnd.Add(-time.Minute), 1500, nil)

	deleted, err := s.PurgeBefore(ctx, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("purged samples = %d, want the two old ones", deleted)
	}

	got, err := s.DailyEnergyFromTotal(ctx, oldStart, newEnd, newEnd, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantRefused(t, got, "2026-10-10") // baseline purged: no value, never 300
	wantDerived(t, got, "2026-10-17", 500)
}

// TestDailyEnergyFromTotalMatchesTheDayKPI fixes the agreement between the two
// independent measures of a well-behaved day: the device's own counter (read
// through DailyEnergy) and the difference of our stored total
// (DailyEnergyFromTotal). They share no helper, so the fixture makes each sample
// carry both quantities and the two results must coincide.
func TestDailyEnergyFromTotalMatchesTheDayKPI(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)

	// E_Day counts the energy since local midnight; the total rises by the same
	// increments, as a device behaving as documented would.
	eDay := func(v float64) *float64 { return &v }
	totalSample(t, s, start.Add(-time.Minute), 1000, eDay(0))
	totalSample(t, s, start.Add(time.Hour), 1100, eDay(100))
	totalSample(t, s, start.Add(12*time.Hour), 2200, eDay(1200))
	totalSample(t, s, end.Add(-time.Minute), 3400, eDay(2400))

	measured, err := s.DailyEnergy(ctx, start, end)
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(measured) != 1 || measured[0].Date != "2026-10-16" {
		t.Fatalf("device-counter days = %+v, want one for 2026-10-16", measured)
	}
	if measured[0].EnergyWh == nil {
		t.Fatalf("device-counter day = %+v, want a value", measured[0])
	}

	derived, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	wantDerived(t, derived, "2026-10-16", 2400)
	if *measured[0].EnergyWh != *derived[0].Wh {
		t.Fatalf("day KPI = %v, derived = %v, want the same value",
			*measured[0].EnergyWh, *derived[0].Wh)
	}
}

// TestDailyEnergyFromTotalAcrossTheYearBoundary: a range spanning 31 December to
// 1 January attributes each sample to its own local day, so the two days straddle
// the year without a sample being lost or double-counted.
func TestDailyEnergyFromTotalAcrossTheYearBoundary(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	start := time.Date(2026, 12, 30, 23, 0, 0, 0, time.UTC) // 2026-12-31 00:00 +01:00
	end := time.Date(2027, 1, 1, 23, 0, 0, 0, time.UTC)     // 2027-01-02 00:00 +01:00

	totalSample(t, s, start.Add(-time.Minute), 5000, nil)
	totalSample(t, s, start.Add(24*time.Hour-time.Minute), 5500, nil) // end of 2026-12-31
	totalSample(t, s, end.Add(-time.Minute), 5800, nil)               // end of 2027-01-01

	got, err := s.DailyEnergyFromTotal(ctx, start, end, end, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("days = %+v, want two", got)
	}
	if got[0].Date != "2026-12-31" || got[1].Date != "2027-01-01" {
		t.Fatalf("dates = %q, %q, want the year boundary", got[0].Date, got[1].Date)
	}
	wantDerived(t, got, "2026-12-31", 500)
	wantDerived(t, got, "2027-01-01", 300)
}
