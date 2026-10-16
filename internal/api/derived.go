// Derived energies: the day and year energies COMPUTED from our own stored total.
//
// Why it exists: the reference firmware publishes neither the day nor the year
// counter on any local endpoint — `GetPowerFlowRealtimeData.fcgi`,
// `/status/powerflow` and the documented accumulation collection all answer them
// as null while the total is live (docs/UNKNOWNS.md #13). The total, however, is
// recorded on every sample, so the two missing quantities can be measured as the
// difference between the current sample and the one that precedes the local
// boundary.
//
// The documentation now says the same thing the firmware does (Solar API V1,
// `42,0410,2012,EN 02115052025`, §4.1.5): on GEN24/Tauro/Verto `DAY_ENERGY` and
// `YEAR_ENERGY` "will always report null", while `TOTAL_ENERGY` is "supported
// since 1.14 (updated every 5 min)". So the derived value cannot be fresher than
// five minutes — it is a difference of two readings of a counter the device moves
// on that cadence, and the interface must not pretend otherwise.
//
// Invariants:
//   - the measured fields keep their meaning: `energy_day_wh` and `energy_year_wh`
//     stay null when the device does not report them, and the client chooses
//     between a measurement and this derivation — a computed value is never
//     presented as a measurement;
//   - every derivation says what it was measured from, and why it is absent when
//     it is: an absent value with a reason beats a wrong value;
//   - the collector outage case is explicit: a baseline too far from the boundary
//     would silently include production from before it, so it produces no value.
package api

import (
	"context"
	"fmt"
	"time"

	"fronius-supervisor/internal/model"
)

// derivedBaselineTolerance bounds how far from a local boundary the baseline
// sample may sit. Beyond it the collector was down or the device silent, and a
// delta measured from an older total would include production from before the
// boundary: no value is published then.
const derivedBaselineTolerance = 10 * time.Minute

// totalEnergyField names the recorded quantity the derivations are measured from.
const totalEnergyField = "energy_total_wh"

// derivedEnergyFromTotal computes the day and year energies of the installation
// from the stored total energy.
//
// The boundaries are the local ones: midnight for the day, 1 January for the year
// — the two quantities a person expects when reading "energy of the day" on their
// own installation.
func (s *Server) derivedEnergyFromTotal(ctx context.Context, sm model.Sample) *derivedEnergyDTO {
	loc := s.store.Location()
	local := sm.TS.In(loc)

	out := &derivedEnergyDTO{From: totalEnergyField}
	out.DayWh, out.DayBaselineUTC, out.DayReason = s.deriveSince(
		ctx, sm, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc))
	out.YearWh, out.YearBaselineUTC, out.YearReason = s.deriveSince(
		ctx, sm, time.Date(local.Year(), time.January, 1, 0, 0, 0, 0, loc))
	return out
}

// deriveSince returns the total energy produced since the local boundary, the
// timestamp of the baseline it was measured from, and — when it could not be
// established — the reason, in the same shape as a detection state.
func (s *Server) deriveSince(ctx context.Context, sm model.Sample, boundary time.Time) (*float64, string, string) {
	if sm.EnergyTotalWh == nil {
		return nil, "", "the current sample carries no total energy"
	}

	base, baseTS, ok, err := s.store.TotalEnergyAt(ctx, boundary)
	if err != nil {
		s.log.Error("reading the total energy baseline failed", "error", err, "boundary", boundary.Format(time.RFC3339))
		return nil, "", "the baseline could not be read"
	}
	if !ok {
		return nil, "", "no sample carries a total energy at or before " + boundary.Format(time.RFC3339)
	}
	if gap := boundary.Sub(baseTS); gap > derivedBaselineTolerance {
		return nil, "", fmt.Sprintf("no sample within %s of the boundary: the closest one is %s earlier",
			derivedBaselineTolerance, gap.Round(time.Second))
	}
	if *sm.EnergyTotalWh < base {
		return nil, "", "the total energy counter went backwards since the boundary"
	}

	value := *sm.EnergyTotalWh - base
	return &value, baseTS.UTC().Format(time.RFC3339), ""
}
