// Tests of the derived day and year energies.
//
// The scenario under test is the reference installation: the firmware publishes no
// day and no year counter (docs/UNKNOWNS.md #13), only a live total, so the two
// quantities are measured from our own stored total.
package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// almostEqual compares two energies: the derivation subtracts two floats, so the
// last bits of a decimal value are not reproducible (15089627.58 - 15089400).
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// insertTotal records one sample carrying a total energy.
func insertTotal(t *testing.T, store *storage.Store, ts time.Time, total float64) {
	t.Helper()
	sm := model.Sample{
		TS:            ts,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		Mode:          "produce-only",
		EnergyTotalWh: new(total),
	}
	if err := store.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample(%s): %v", ts, err)
	}
}

// liveDerived fetches /api/v1/live and returns the carried energy_derived object.
func liveDerived(t *testing.T, srv *httptest.Server) derivedEnergyDTO {
	t.Helper()
	return *liveSample(t, srv).EnergyDerived
}

// liveSample fetches the live payload with the fields the derivations touch.
func liveSample(t *testing.T, srv *httptest.Server) struct {
	EnergyDayWh   *float64          `json:"energy_day_wh"`
	EnergyYearWh  *float64          `json:"energy_year_wh"`
	EnergyDerived *derivedEnergyDTO `json:"energy_derived"`
} {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/live")
	if err != nil {
		t.Fatalf("GET /api/v1/live: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body struct {
		Data struct {
			EnergyDayWh   *float64          `json:"energy_day_wh"`
			EnergyYearWh  *float64          `json:"energy_year_wh"`
			EnergyDerived *derivedEnergyDTO `json:"energy_derived"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if body.Data.EnergyDerived == nil {
		t.Fatal("energy_derived must be present on /api/v1/live")
	}
	return body.Data
}

func TestDerivedDayAndYearEnergiesFromTheStoredTotal(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	// The boundaries are the local ones: the store's location is Europe/Brussels.
	loc := store.Location()
	dayStart := time.Date(2026, 10, 16, 0, 0, 0, 0, loc)
	yearStart := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	now := dayStart.Add(17 * time.Hour)

	// The baselines are the samples just BEFORE each boundary: the derivation looks
	// for the most recent sample at or before it.
	insertTotal(t, store, yearStart.Add(-time.Second), 15_000_000)
	insertTotal(t, store, dayStart.Add(-time.Second), 15_089_400)
	insertTotal(t, store, now, 15_089_627.58)

	got := liveDerived(t, srv)
	if got.From != "energy_total_wh" {
		t.Fatalf("from = %q", got.From)
	}
	if got.DayWh == nil || !almostEqual(*got.DayWh, 227.58) {
		t.Fatalf("day_wh = %v, want 227.58", got.DayWh)
	}
	if got.DayReason != "" || got.YearReason != "" {
		t.Fatalf("reasons = (%q, %q), want empty", got.DayReason, got.YearReason)
	}
	if got.YearWh == nil || !almostEqual(*got.YearWh, 89_627.58) {
		t.Fatalf("year_wh = %v, want 89627.58", got.YearWh)
	}
	if got.DayBaselineUTC == "" || got.YearBaselineUTC == "" {
		t.Fatalf("the baselines must be named: %+v", got)
	}
	// Since monitoring: measured from the very first stored total, with no
	// boundary tolerance — the baseline is wherever monitoring began.
	if got.SinceWh == nil || !almostEqual(*got.SinceWh, 89_627.58) {
		t.Fatalf("since_wh = %v, want 89627.58", got.SinceWh)
	}
	if got.SinceBaselineUTC == "" || got.SinceReason != "" {
		t.Fatalf("since = (%q, %q), want a baseline and no reason", got.SinceBaselineUTC, got.SinceReason)
	}
}

// TestMeasuredEnergiesAreLeftAlone: the derivation never touches the measured
// fields, which keep their own value and stay null when the device says nothing.
func TestMeasuredEnergiesAreLeftAlone(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	now := time.Date(2026, 10, 16, 17, 0, 0, 0, store.Location())

	insertTotal(t, store, now.Add(-time.Hour), 1000)
	// The current sample carries both a measured day energy and a total.
	sm := model.Sample{
		TS:            now,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		EnergyDayWh:   new(12.5),
		EnergyTotalWh: new(float64(1100)),
	}
	if err := store.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample: %v", err)
	}

	got := liveSample(t, srv)
	if got.EnergyDayWh == nil || *got.EnergyDayWh != 12.5 {
		t.Fatalf("energy_day_wh = %v, want the measured value untouched", got.EnergyDayWh)
	}
	if got.EnergyDerived == nil {
		t.Fatal("the derivation is still published alongside the measurement")
	}
}

// TestDerivationRefusesABaselineTooFarFromTheBoundary: a collector outage around
// midnight must not turn yesterday's production into today's.
func TestDerivationRefusesABaselineTooFarFromTheBoundary(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	loc := store.Location()
	now := time.Date(2026, 10, 16, 17, 0, 0, 0, loc)

	// The only sample before midnight sits two hours older than the boundary...
	insertTotal(t, store, time.Date(2026, 10, 15, 22, 0, 0, 0, loc), 15_000_000)
	insertTotal(t, store, now, 15_000_500)

	got := liveDerived(t, srv)
	if got.DayWh != nil {
		t.Fatalf("day_wh = %v, want absent", *got.DayWh)
	}
	if got.DayReason == "" || got.DayBaselineUTC != "" {
		t.Fatalf("day = (%v, %q), want a reason and no baseline", got.DayWh, got.DayReason)
	}
	// The year boundary is 1 January and nothing precedes it here, so the year
	// value is refused as well — for lack of a baseline rather than for its age.
	if got.YearWh != nil {
		t.Fatalf("year_wh = %v, want absent", *got.YearWh)
	}
	if got.YearReason == "" {
		t.Fatalf("year_reason must say why: %+v", got)
	}
}

// TestDerivationWithoutAnyBaseline: a fresh installation has no total before the
// boundary, and says so instead of inventing a zero.
func TestDerivationWithoutAnyBaseline(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	now := time.Date(2026, 10, 16, 17, 0, 0, 0, store.Location())

	insertTotal(t, store, now, 15_000_000)

	got := liveDerived(t, srv)
	if got.DayWh != nil || got.YearWh != nil {
		t.Fatalf("both values must be absent: %+v", got)
	}
	if got.DayReason == "" || got.YearReason == "" {
		t.Fatalf("both reasons must be given: %+v", got)
	}
	// A fresh installation still knows what it produced since monitoring began:
	// the first stored sample is its own baseline.
	if got.SinceWh == nil || *got.SinceWh != 0 || got.SinceReason != "" {
		t.Fatalf("since = (%v, %q), want 0 without a reason", got.SinceWh, got.SinceReason)
	}
}

// TestDerivationRefusesACounterGoingBackwards: a device reset must not produce a
// negative energy.
func TestDerivationRefusesACounterGoingBackwards(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	dayStart := time.Date(2026, 10, 16, 0, 0, 0, 0, store.Location())

	insertTotal(t, store, dayStart.Add(-time.Second), 15_089_000)
	insertTotal(t, store, dayStart.Add(17*time.Hour), 900)

	got := liveDerived(t, srv)
	if got.DayWh != nil {
		t.Fatalf("day_wh = %v, want absent after a counter reset", *got.DayWh)
	}
	if got.DayReason != "the total energy counter went backwards since the boundary" {
		t.Fatalf("day_reason = %q", got.DayReason)
	}
	// Since monitoring is measured from the run that started at the reset, so it
	// is a value rather than a refusal — the current sample is its baseline.
	if got.SinceWh == nil || *got.SinceWh != 0 || got.SinceReason != "" {
		t.Fatalf("since = (%v, %q), want 0 from the reset without a reason", got.SinceWh, got.SinceReason)
	}
}

// TestDerivedSinceRestartsAtTheLastCounterDecrease: the stored series may hold
// more than one counter run (a simulator in the same database, then the device;
// or a device reset). The run's first sample is the baseline, so the KPI reflects
// what the current counter has produced.
func TestDerivedSinceRestartsAtTheLastCounterDecrease(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	loc := store.Location()
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, loc)
	reset := base.Add(2 * time.Hour)

	insertTotal(t, store, base, 17_000_000)
	insertTotal(t, store, base.Add(time.Hour), 17_000_500)
	insertTotal(t, store, reset, 15_000_000)
	insertTotal(t, store, base.Add(3*time.Hour), 15_000_250)

	got := liveDerived(t, srv)
	if got.SinceWh == nil || !almostEqual(*got.SinceWh, 250) {
		t.Fatalf("since_wh = %v, want 250 measured from the reset", got.SinceWh)
	}
	if got.SinceBaselineUTC != reset.UTC().Format(time.RFC3339) {
		t.Fatalf("since_baseline_utc = %q, want the reset instant", got.SinceBaselineUTC)
	}
	if got.SinceReason != "" {
		t.Fatalf("since_reason = %q, want empty", got.SinceReason)
	}
}
