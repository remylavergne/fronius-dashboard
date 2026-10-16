// Tests of the per-day energy measured from the stored total.
//
// This is the data behind the aggregates view: the device never reports `E_Day`
// on this firmware, so the day's energy is the difference between two readings
// of the total. The invariants are the ones the day KPI already applies — a
// baseline too far, a missing reading or a counter that went backwards yields NO
// value with its cause, never a number that would be wrong.
package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

// daySample inserts one sample carrying a total at an instant.
func daySample(t *testing.T, s *Store, ts time.Time, total float64) {
	t.Helper()
	sm := sample(ts, 1000)
	sm.EnergyTotalWh = new(total)
	mustInsert(t, s, sm)
}

func TestDailyEnergyFromTotalMeasuresEachDay(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	tolerance := 10 * time.Minute

	// Two complete local days: the total rises by a known amount in each.
	// October 2026 is CEST (UTC+2), so a local day starts at 22:00 UTC the day
	// before.
	day1Start := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC) // 2026-10-16 00:00 +02:00
	day2Start := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	daySample(t, s, day1Start.Add(-time.Minute), 1_000_000)
	daySample(t, s, day1Start.Add(30*time.Minute), 1_000_500)
	// The last reading of a day sits just before its end: with a five-second
	// collector that is what the store holds, and the tolerance is ten minutes.
	daySample(t, s, day2Start.Add(-time.Minute), 1_001_500) // 1500 Wh produced on day 1
	daySample(t, s, day2Start.Add(time.Hour), 1_001_800)
	daySample(t, s, day2Start.Add(24*time.Hour-time.Minute), 1_002_000) // 500 Wh on day 2

	got, err := s.DailyEnergyFromTotal(ctx, day1Start, now, now, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("days = %d (%+v), want 3", len(got), got)
	}

	// Day 1: 1_001_500 - 1_000_000.
	if got[0].Date != "2026-10-16" || got[0].Wh == nil || *got[0].Wh != 1500 {
		t.Errorf("day 1 = %+v, want 1500 Wh", got[0])
	}
	// Day 2: 1_002_000 - 1_001_500.
	if got[1].Date != "2026-10-17" || got[1].Wh == nil || *got[1].Wh != 500 {
		t.Errorf("day 2 = %+v, want 500 Wh", got[1])
	}
	// Day 3 is TODAY (2026-10-18 local starts at 22:00Z on the 17th): it is
	// partial by definition, and its value is refused here because the last
	// reading is twelve hours old — the same rule, applied to a day in progress.
	if got[2].Date != "2026-10-18" {
		t.Errorf("day 3 = %+v, want today", got[2])
	}
	if !got[2].Partial {
		t.Error("the day in progress is not marked partial")
	}
	if got[2].Wh != nil || got[2].Reason == "" {
		t.Errorf("day 3 = %+v, want no value with a reason", got[2])
	}
	for _, d := range got[:2] {
		if d.Partial {
			t.Errorf("%s: a finished day is not partial", d.Date)
		}
	}
}

func TestDailyEnergyFromTotalRefusesWhatItCannotMeasure(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	tolerance := 10 * time.Minute
	dayStart := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)

	// 1. A baseline hours before the day's start would attribute those hours to
	//    this day: refused.
	daySample(t, s, dayStart.Add(-4*time.Hour), 500)
	daySample(t, s, dayEnd.Add(-time.Minute), 900)
	got, err := s.DailyEnergyFromTotal(ctx, dayStart, dayEnd, dayEnd, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if got[0].Wh != nil || !strings.Contains(got[0].Reason, "before the day's start") {
		t.Errorf("stale baseline: %+v", got[0])
	}

	// 2. A last reading hours before the day's end would drop those hours into
	//    the next day: refused as well.
	fresh := openStore(t, brussels(t))
	daySample(t, fresh, dayStart.Add(-time.Minute), 500)
	daySample(t, fresh, dayEnd.Add(-5*time.Hour), 900)
	got, err = fresh.DailyEnergyFromTotal(ctx, dayStart, dayEnd, dayEnd, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if got[0].Wh != nil || !strings.Contains(got[0].Reason, "last sample") {
		t.Errorf("stale end: %+v", got[0])
	}

	// 3. A counter that went backwards (a device reset) is not a negative day.
	backwards := openStore(t, brussels(t))
	daySample(t, backwards, dayStart.Add(-time.Minute), 5_000)
	daySample(t, backwards, dayEnd.Add(-time.Minute), 10)
	got, err = backwards.DailyEnergyFromTotal(ctx, dayStart, dayEnd, dayEnd, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if got[0].Wh != nil || !strings.Contains(got[0].Reason, "backwards") {
		t.Errorf("backwards counter: %+v", got[0])
	}

	// 4. No sample at or before the day's start at all.
	empty := openStore(t, brussels(t))
	daySample(t, empty, dayStart.Add(time.Hour), 700)
	got, err = empty.DailyEnergyFromTotal(ctx, dayStart, dayEnd, dayEnd, tolerance)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if got[0].Wh != nil || !strings.Contains(got[0].Reason, "at or before") {
		t.Errorf("no baseline: %+v", got[0])
	}
}

func TestDailyEnergyFromTotalMarksTheDayInProgress(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	dayStart := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	now := dayStart.Add(10 * time.Hour)

	daySample(t, s, dayStart.Add(-time.Minute), 1_000)
	daySample(t, s, now.Add(-time.Minute), 1_750)

	got, err := s.DailyEnergyFromTotal(ctx, dayStart, dayStart.Add(48*time.Hour), now, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("days = %+v", got)
	}
	if !got[0].Partial {
		t.Error("the day in progress is not marked partial")
	}
	if got[0].Wh == nil || *got[0].Wh != 750 {
		t.Errorf("today = %+v, want 750 Wh so far", got[0])
	}
	// A day that has not begun carries neither a value nor a misleading cause.
	if got[1].Wh != nil || got[1].Reason == "" {
		t.Errorf("future day = %+v", got[1])
	}
}

func TestDailyEnergyFromTotalFollowsTheLocalDayAcrossTheOffsetChange(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	// The night of 2026-10-24 to 2026-10-25 is the end of summer time in the
	// EU: that local day is 25 hours long, and the next one 24.
	from := time.Date(2026, 10, 23, 22, 0, 0, 0, time.UTC) // 2026-10-24 00:00 +02:00
	to := time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)

	for _, ts := range []time.Time{
		time.Date(2026, 10, 23, 21, 59, 0, 0, time.UTC),
		time.Date(2026, 10, 25, 23, 59, 0, 0, time.UTC),
		time.Date(2026, 10, 26, 22, 59, 0, 0, time.UTC),
	} {
		daySample(t, s, ts, 1000)
	}

	got, err := s.DailyEnergyFromTotal(ctx, from, to, to, 10*time.Minute)
	if err != nil {
		t.Fatalf("DailyEnergyFromTotal: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("days = %d (%+v)", len(got), got)
	}
	// The dates are consecutive local days, whatever the offset did.
	for i, want := range []string{"2026-10-24", "2026-10-25", "2026-10-26"} {
		if got[i].Date != want {
			t.Errorf("day %d = %q, want %q", i, got[i].Date, want)
		}
	}
}
