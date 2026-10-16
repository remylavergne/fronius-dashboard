package storage

import (
	"context"
	"fmt"
	"time"
)

// DailyDerivedEnergy is the energy of one local day, measured from our own
// stored total rather than from the device's `E_Day`.
//
// Why it exists: on the reference firmware `E_Day` is always null
// (docs/UNKNOWNS.md #13, and the Solar API specification §4.1.5 says so in
// writing), so `DailyEnergy` has nothing to report and the aggregates view would
// be empty on an installation that is producing. The total, however, is recorded
// on every sample: the energy of a day is the difference between the last
// reading of that day and the reading at its start.
//
// The rules are the ones the derived day energy already applies, because a
// wrong number is worse than no number:
//   - the baseline is the most recent sample at or before the local midnight,
//     and it must be within `tolerance` of it — a baseline from hours earlier
//     would attribute the missing hours to this day;
//   - the end reading must also be within `tolerance` of the day's end, for the
//     same reason in the other direction;
//   - a counter that went backwards (a device reset) yields no value.
type DailyDerivedEnergy struct {
	// Date is the local day, YYYY-MM-DD.
	Date string
	// Wh is the day's energy, nil when it cannot be established.
	Wh *float64
	// Reason carries the cause when Wh is nil, and stays empty otherwise.
	Reason string
	// Partial marks the day in progress: its value grows until the day ends.
	Partial bool
}

// DailyEnergyFromTotal computes the energy of every local day of [from, to)
// from the stored total.
//
// `now` bounds the last day: the day in progress is reported as partial rather
// than refused. The caller passes the tolerance it wants, so that this package
// does not have to know the API's opinion on how far a baseline may be.
func (s *Store) DailyEnergyFromTotal(ctx context.Context, from, to, now time.Time,
	tolerance time.Duration) ([]DailyDerivedEnergy, error) {

	if !to.After(from) {
		return nil, fmt.Errorf("storage: [%s, %s) is empty", formatTS(from), formatTS(to))
	}

	// The days are the device's local days: the day boundary is a wall-clock
	// one, and it moves twice a year with the offset.
	day := s.localMidnight(from)
	out := make([]DailyDerivedEnergy, 0, int(to.Sub(from).Hours()/24)+1)

	for day.Before(to) {
		next := s.nextLocalMidnight(day)
		// The date is a LOCAL one: the boundary instant is held in UTC, but the
		// day it names is the device's day (22:00Z is 00:00 +02:00 the next day).
		entry := DailyDerivedEnergy{Date: day.In(s.loc).Format("2006-01-02")}

		partial := day.After(now) == false && next.After(now)
		entry.Partial = partial

		switch {
		case day.After(now):
			// A day that has not begun: no value, and no cause worth a sentence.
			entry.Reason = "the day has not begun yet"
		default:
			entry.Wh, entry.Reason = s.dayEnergyFromTotal(ctx, day, next, now, tolerance)
		}

		out = append(out, entry)
		day = next
	}
	return out, nil
}

// dayEnergyFromTotal derives one day: its end reading minus its baseline.
func (s *Store) dayEnergyFromTotal(ctx context.Context, start, end, now time.Time,
	tolerance time.Duration) (*float64, string) {

	baseline, baselineAt, ok, err := s.TotalEnergyAt(ctx, start)
	if err != nil {
		return nil, "the baseline could not be read"
	}
	if !ok {
		return nil, "no sample carries a total at or before the day's start"
	}
	if gap := start.Sub(baselineAt); gap > tolerance {
		return nil, fmt.Sprintf("the closest sample before the day's start is %s old", gap.Round(time.Minute))
	}

	// The day in progress is read up to now; a finished day up to its end.
	limit := end
	if now.Before(limit) {
		limit = now
	}
	latest, latestAt, ok, err := s.TotalEnergyAt(ctx, limit)
	if err != nil {
		return nil, "the last reading of the day could not be read"
	}
	if !ok {
		return nil, "no sample carries a total inside the day"
	}
	if gap := limit.Sub(latestAt); gap > tolerance {
		return nil, fmt.Sprintf("the last sample of the day is %s old", gap.Round(time.Minute))
	}

	if latest < baseline {
		return nil, "the total energy counter went backwards since the day's start"
	}
	value := latest - baseline
	return &value, ""
}

// localMidnight returns the start of the local day containing t.
func (s *Store) localMidnight(t time.Time) time.Time {
	local := t.In(s.loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.loc).UTC()
}

// nextLocalMidnight returns the start of the day after the one that starts at
// midnight: 23, 24 or 25 hours later, depending on the offset change.
func (s *Store) nextLocalMidnight(midnight time.Time) time.Time {
	local := midnight.In(s.loc)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.loc).UTC()
}
