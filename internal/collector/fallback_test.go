// Tests of the internal fallback: /status/powerflow completes the day and year
// energies that the Solar API returns as null (docs/UNKNOWNS.md #13).
//
// The scenario of the first test is the reference installation itself: the
// promoted capture of 2026-10-16, whose Site carries E_Day and E_Year as null
// while E_Total is measured.
package collector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
)

// fakePowerFlow is a PowerFlowSource that counts its calls.
type fakePowerFlow struct {
	pf    internalapi.PowerFlow
	err   error
	calls int
}

func (f *fakePowerFlow) PowerFlow(context.Context) (internalapi.PowerFlow, error) {
	f.calls++
	return f.pf, f.err
}

// capturedEnvelope replays the promoted hardware capture: E_Day and E_Year are
// null, E_Total is measured, P_PV is live.
func capturedEnvelope(t *testing.T) solarapi.PowerFlowEnvelope {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "solarapi", "powerflow_produce_only.json"))
	if err != nil {
		t.Fatalf("reading the capture: %v", err)
	}
	var env solarapi.PowerFlowEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decoding the capture: %v", err)
	}
	return env
}

// powerFlowClient serves a /status/powerflow body through the real internal
// client: the fallback is exercised as it is in production, allowlist lookup and
// Default-envelope decoding included.
func powerFlowClient(t *testing.T, body []byte) *internalapi.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != internalapi.PowerFlowPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c, err := internalapi.New(srv.URL, fronius.ReadOnly{Inner: &http.Client{}}, 2*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("internalapi.New: %v", err)
	}
	return c
}

// capturedPowerFlowClient serves the promoted capture of /status/powerflow.
func capturedPowerFlowClient(t *testing.T) *internalapi.Client {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "internalapi", "status_powerflow.json"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return powerFlowClient(t, body)
}

// TestFallbackFillsNothingWhenTheDeviceNullsItToo is the OBSERVED case: on the
// reference installation, `/status/powerflow` returns `site.E_Day` and
// `site.E_Year` as null, exactly like the Solar API (captured 2026-10-16). The
// sample therefore keeps its energies absent, records no fallback source, and is
// still persisted — the fallback must not turn an absent value into anything.
func TestFallbackFillsNothingWhenTheDeviceNullsItToo(t *testing.T) {
	sink := newFakeSink()
	c := newCollector(&fakeSource{results: []result{{env: capturedEnvelope(t)}}}, sink)
	c.Fallback = capturedPowerFlowClient(t)

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if sm.EnergyDayWh != nil || sm.EnergyYearWh != nil {
		t.Fatalf("energies = (%v, %v), want absent: the device nulls them on both paths",
			sm.EnergyDayWh, sm.EnergyYearWh)
	}
	if sm.FallbackSource != "" {
		t.Fatalf("fallback_source = %q, want empty when nothing was filled", sm.FallbackSource)
	}
	if len(sink.samples) != 1 {
		t.Fatalf("inserted samples = %d, want 1", len(sink.samples))
	}
	if sm.EnergyTotalWh == nil || *sm.EnergyTotalWh != 15089488.925277777 {
		t.Fatalf("energy_total_wh = %v, want the Solar API value untouched", sm.EnergyTotalWh)
	}
}

// TestFallbackCompletesTheNulledEnergies covers the fill logic itself, with a
// HYPOTHETICAL internal answer: no observed firmware populates these two counters,
// so the body below is fabricated to keep the code path honest. If a firmware ever
// reports them, this is the behaviour that takes over.
func TestFallbackCompletesTheNulledEnergies(t *testing.T) {
	const hypothetical = `{"site": {"E_Day": 4123.4, "E_Year": 4215488.4, "E_Total": 15089488.9}}`

	src := &fakeSource{results: []result{{env: capturedEnvelope(t)}}}
	sink := newFakeSink()
	c := newCollector(src, sink)
	c.Fallback = powerFlowClient(t, []byte(hypothetical))

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if sm.EnergyDayWh == nil || *sm.EnergyDayWh != 4123.4 {
		t.Fatalf("energy_day_wh = %v, want the internal value", sm.EnergyDayWh)
	}
	if sm.EnergyYearWh == nil || *sm.EnergyYearWh != 4215488.4 {
		t.Fatalf("energy_year_wh = %v, want the internal value", sm.EnergyYearWh)
	}
	if sm.FallbackSource != internalapi.PowerFlowPath {
		t.Fatalf("fallback_source = %q, want %q", sm.FallbackSource, internalapi.PowerFlowPath)
	}
	// Nothing else is taken from the fallback: E_Total and P_PV stay what the
	// Solar API measured.
	if sm.EnergyTotalWh == nil || *sm.EnergyTotalWh != 15089488.925277777 {
		t.Fatalf("energy_total_wh = %v, want the Solar API value", sm.EnergyTotalWh)
	}
	if sm.PowerPVW == nil || *sm.PowerPVW != 228.35735321044922 {
		t.Fatalf("power_pv_w = %v, want the Solar API value", sm.PowerPVW)
	}
}

// TestFallbackIsNotCalledWhenNothingIsMissing: a complete Solar API response
// triggers no extra request — the fallback is a gap filler, not a second poll.
func TestFallbackIsNotCalledWhenNothingIsMissing(t *testing.T) {
	env := okEnvelope("2026-10-16T09:17:43+02:00", 1902.4)
	env.Body.Data.Site.E_Day = new(9064.1)
	env.Body.Data.Site.E_Year = new(4215488.4)

	sink := newFakeSink()
	c := newCollector(&fakeSource{results: []result{{env: env}}}, sink)
	fb := &fakePowerFlow{pf: internalapi.PowerFlow{Site: internalapi.PowerFlowSite{E_Day: new(1.0), E_Year: new(2.0)}}}
	c.Fallback = fb

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if fb.calls != 0 {
		t.Fatalf("fallback calls = %d, want 0 when the Solar API fills everything", fb.calls)
	}
	if sm.FallbackSource != "" {
		t.Fatalf("fallback_source = %q, want empty", sm.FallbackSource)
	}
	if *sm.EnergyDayWh != 9064.1 {
		t.Fatalf("energy_day_wh = %v, want the measured value untouched", *sm.EnergyDayWh)
	}
}

// TestFallbackFailureKeepsTheSample: an unreachable internal path never fails a
// cycle and never invents a value — the energies stay absent.
func TestFallbackFailureKeepsTheSample(t *testing.T) {
	sink := newFakeSink()
	c := newCollector(&fakeSource{results: []result{{env: capturedEnvelope(t)}}}, sink)
	c.Fallback = &fakePowerFlow{err: errors.New("transport: connection refused")}

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("CollectOnce must not fail on a fallback error: %v", err)
	}
	if sm.EnergyDayWh != nil || sm.EnergyYearWh != nil {
		t.Fatalf("energies = (%v, %v), want absent", sm.EnergyDayWh, sm.EnergyYearWh)
	}
	if sm.FallbackSource != "" {
		t.Fatalf("fallback_source = %q, want empty when nothing was filled", sm.FallbackSource)
	}
	if len(sink.samples) != 1 {
		t.Fatalf("inserted samples = %d, want 1", len(sink.samples))
	}
	// A fallback failure is not a device failure: no warning event, and the run
	// is recorded as successful.
	if len(sink.events) != 0 {
		t.Fatalf("no event expected: %+v", sink.events)
	}
	if len(sink.runs) != 1 || !sink.runs[0].OK {
		t.Fatalf("run = %+v", sink.runs)
	}
	if sink.state[StateLastError] != "" {
		t.Fatalf("last_error = %q, want empty", sink.state[StateLastError])
	}
}
