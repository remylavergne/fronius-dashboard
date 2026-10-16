package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

type result struct {
	env solarapi.PowerFlowEnvelope
	err error
}

type fakeSource struct {
	results []result
	idx     int
	calls   int
}

func (f *fakeSource) GetPowerFlowRealtimeData(context.Context) (solarapi.PowerFlowEnvelope, error) {
	f.calls++
	if f.idx < len(f.results) {
		r := f.results[f.idx]
		f.idx++
		return r.env, r.err
	}
	// Any iteration beyond the program fails: a test must not loop
	// indefinitely on an implicit success.
	return solarapi.PowerFlowEnvelope{}, errors.New("source: no more programmed result")
}

type fakeSink struct {
	samples   []model.Sample
	events    []model.Event
	runs      []storage.RunRecord
	state     map[string]string
	insertErr error
	eventErr  error
	runErr    error
}

func newFakeSink() *fakeSink { return &fakeSink{state: map[string]string{}} }

func (f *fakeSink) InsertSample(_ context.Context, sm model.Sample) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.samples = append(f.samples, sm)
	return nil
}

func (f *fakeSink) InsertEvent(_ context.Context, e model.Event) error {
	if f.eventErr != nil {
		return f.eventErr
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeSink) InsertRun(_ context.Context, r storage.RunRecord) error {
	if f.runErr != nil {
		return f.runErr
	}
	f.runs = append(f.runs, r)
	return nil
}

func (f *fakeSink) SetState(_ context.Context, k, v string) error {
	f.state[k] = v
	return nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func okEnvelope(ts string, pv float64) solarapi.PowerFlowEnvelope {
	env := solarapi.PowerFlowEnvelope{
		Head: solarapi.Head{
			Timestamp: ts,
			Status:    solarapi.HeadStatus{Code: 0},
		},
	}
	env.Body.Data.Site = solarapi.SiteData{Mode: "produce-only", P_PV: new(pv)}
	return env
}

func newCollector(src Source, sink Sink) *Collector {
	return &Collector{
		Source:   src,
		Sink:     sink,
		Log:      testLogger(),
		Interval: 5 * time.Second,
		Jitter:   500 * time.Millisecond,
	}
}

func TestCollectOnceSuccess(t *testing.T) {
	src := &fakeSource{results: []result{{env: okEnvelope("2026-10-16T09:17:43+02:00", 1902.4)}}}
	sink := newFakeSink()
	c := newCollector(src, sink)

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if sm.PowerPVW == nil || *sm.PowerPVW != 1902.4 {
		t.Fatalf("sample = %+v", sm)
	}
	if len(sink.samples) != 1 {
		t.Fatalf("inserted samples = %d, want 1", len(sink.samples))
	}
	if len(sink.runs) != 1 || !sink.runs[0].OK || sink.runs[0].HTTPStatus != 200 {
		t.Fatalf("run = %+v", sink.runs)
	}
	// last_success_utc is the collector clock (instant of the successful cycle),
	// not the device timestamp.
	gotSuccess := sink.state[StateLastSuccess]
	if _, err := time.Parse(time.RFC3339, gotSuccess); err != nil {
		t.Fatalf("last_success_utc = %q, want RFC3339: %v", gotSuccess, err)
	}
	if sink.samples[0].TS.Format(time.RFC3339) != "2026-10-16T07:17:43Z" {
		t.Fatalf("sample TS = %v (device timestamp expected)", sink.samples[0].TS)
	}
	if v, ok := sink.state[StateLastError]; !ok || v != "" {
		t.Fatalf("last_error = %q (present=%v)", v, ok)
	}
	if len(sink.events) != 0 {
		t.Fatalf("no event expected on the first success: %+v", sink.events)
	}
}

func TestCollectOnceFailureInsertsNoSample(t *testing.T) {
	failure := &froniuserr.Error{Kind: froniuserr.KindServer, Endpoint: "/x", HTTPStatus: 503}
	src := &fakeSource{results: []result{{err: failure}}}
	sink := newFakeSink()
	c := newCollector(src, sink)

	if _, err := c.CollectOnce(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("CollectOnce = %v, want the source error", err)
	}
	if len(sink.samples) != 0 {
		t.Fatalf("a failure must insert no sample (%d inserted)", len(sink.samples))
	}
	if len(sink.runs) != 1 {
		t.Fatalf("recorded runs = %d, want 1", len(sink.runs))
	}
	run := sink.runs[0]
	if run.OK || run.HTTPStatus != 503 || run.ErrorKind != "server" || run.Error == "" {
		t.Fatalf("run = %+v", run)
	}
	if sink.state[StateLastError] == "" {
		t.Fatal("last_error must be filled in")
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	if sink.events[0].Severity != model.SeverityWarning || sink.events[0].Kind != model.EventCollectorError {
		t.Fatalf("event = %+v", sink.events[0])
	}
}

// TestDeviceStatusErrorIsNotPersisted: a device that answers 200 but reports
// an application error produces no measurement.
func TestDeviceStatusErrorIsNotPersisted(t *testing.T) {
	env := okEnvelope("2026-10-16T09:17:43+02:00", 0)
	env.Head.Status = solarapi.HeadStatus{Code: 7, Reason: "Simulated device error"}
	statusErr := &froniuserr.Error{
		Kind: froniuserr.KindStatus, Endpoint: "/x", HTTPStatus: 200, Code: 7, Reason: "Simulated device error",
	}

	src := &fakeSource{results: []result{{env: env, err: statusErr}}}
	sink := newFakeSink()
	if _, err := newCollector(src, sink).CollectOnce(context.Background()); err == nil {
		t.Fatal("an error was expected")
	}
	if len(sink.samples) != 0 {
		t.Fatalf("inserted samples = %d", len(sink.samples))
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	if sink.runs[0].ErrorKind != string(froniuserr.KindStatus) {
		t.Fatalf("run = %+v", sink.runs[0])
	}
}

func TestFailureEventIsNotDuplicated(t *testing.T) {
	same := &froniuserr.Error{Kind: froniuserr.KindTimeout, Endpoint: "/x"}
	other := &froniuserr.Error{Kind: froniuserr.KindServer, Endpoint: "/x", HTTPStatus: 500}
	src := &fakeSource{results: []result{
		{err: same}, {err: same}, {err: same},
		{err: other}, {err: other},
		{env: okEnvelope("2026-10-16T09:17:43+02:00", 1)},
	}}
	sink := newFakeSink()
	c := newCollector(src, sink)
	ctx := context.Background()

	for range 5 {
		if _, err := c.CollectOnce(ctx); err == nil {
			t.Fatal("an error was expected")
		}
	}
	// Three identical failures → a single event; a different message → a
	// second event.
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2 (anti-duplication): %+v", len(sink.events), sink.events)
	}
	// All cycles are however traced individually.
	if len(sink.runs) != 5 {
		t.Fatalf("runs = %d, want 5", len(sink.runs))
	}

	// The recovery inserts an event and resets the error state to zero.
	if _, err := c.CollectOnce(ctx); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if len(sink.events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(sink.events), sink.events)
	}
	recovery := sink.events[2]
	if recovery.Kind != model.EventCollectorRecovered || recovery.Severity != model.SeverityInfo {
		t.Fatalf("recovery event = %+v", recovery)
	}
	if sink.state[StateLastError] != "" {
		t.Fatalf("last_error = %q, want empty", sink.state[StateLastError])
	}

	// A new failure identical to the previous message must again produce an
	// event, since the series of failures was interrupted by a success.
	src.results = append(src.results, result{err: other})
	if _, err := c.CollectOnce(ctx); err == nil {
		t.Fatal("an error was expected")
	}
	if len(sink.events) != 4 {
		t.Fatalf("events = %d, want 4", len(sink.events))
	}
}

func TestInsertSampleErrorIsPropagated(t *testing.T) {
	src := &fakeSource{results: []result{{env: okEnvelope("2026-10-16T09:17:43+02:00", 1)}}}
	sink := newFakeSink()
	sink.insertErr = errors.New("disk full")
	c := newCollector(src, sink)

	_, err := c.CollectOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v", err)
	}
	// A persistence failure is not a device failure: neither a run nor a
	// failure event must be written.
	if len(sink.runs) != 0 || len(sink.events) != 0 {
		t.Fatalf("runs=%d events=%d", len(sink.runs), len(sink.events))
	}
}

func TestSuccessDelayWithinJitterBounds(t *testing.T) {
	c := newCollector(&fakeSource{}, newFakeSink())
	c.Interval = 5 * time.Second

	c.Jitter = 0
	for range 10 {
		if got := c.successDelay(); got != 5*time.Second {
			t.Fatalf("without jitter: delay = %v, want 5s", got)
		}
	}

	c.Jitter = 500 * time.Millisecond
	c.Rand = rand.New(rand.NewPCG(1, 2))
	lowBound, highBound := 5*time.Second-c.Jitter, 5*time.Second+c.Jitter
	observedMin, observedMax := time.Duration(1<<62), time.Duration(0)
	below, above := false, false
	for range 2000 {
		d := c.successDelay()
		if d < lowBound || d > highBound {
			t.Fatalf("delay %v out of bounds [%v, %v]", d, lowBound, highBound)
		}
		observedMin = min(observedMin, d)
		observedMax = max(observedMax, d)
		if d < c.Interval {
			below = true
		}
		if d > c.Interval {
			above = true
		}
	}
	// The jitter must really vary and be centered: over 2000 draws, the
	// observed range must cover a large part of the amplitude, and values
	// must appear on both sides of the nominal interval.
	if observedMax-observedMin < c.Jitter/2 {
		t.Fatalf("jitter too concentrated: observed range %v", observedMax-observedMin)
	}
	if !below || !above {
		t.Fatalf("delays not centered on %v: min=%v max=%v", c.Interval, observedMin, observedMax)
	}
}

func TestBackoffSchedule(t *testing.T) {
	c := newCollector(&fakeSource{}, newFakeSink())
	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, time.Minute, time.Minute, time.Minute,
	}
	for i, w := range want {
		if got := c.backoff(i + 1); got != w {
			t.Fatalf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	// Robustness: an absurd attempt stays capped (no shift
	// overflow, no negative delay).
	for _, attempt := range []int{0, -5, 31, 64, 1000} {
		if got := c.backoff(attempt); got <= 0 || got > maxBackoff {
			t.Fatalf("backoff(%d) = %v, want within ]0, %v]", attempt, got, maxBackoff)
		}
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	src := &loopingSource{}
	sink := newFakeSink()
	c := newCollector(src, sink)
	c.Interval = time.Millisecond
	c.Jitter = 0

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run = %v, want nil on cancellation", err)
	}
	if len(sink.samples) == 0 {
		t.Fatal("no sample collected before cancellation")
	}
	if len(sink.runs) != len(sink.samples) {
		t.Fatalf("runs=%d samples=%d", len(sink.runs), len(sink.samples))
	}
	if !src.sawGet {
		t.Fatal("the source was not polled")
	}
}

// loopingSource succeeds indefinitely: indispensable to test a loop.
type loopingSource struct {
	sawGet bool
}

func (l *loopingSource) GetPowerFlowRealtimeData(context.Context) (solarapi.PowerFlowEnvelope, error) {
	l.sawGet = true
	return okEnvelope("2026-10-16T09:17:43+02:00", 100), nil
}

func TestRunDoesNotInsertSamplesWhileFailing(t *testing.T) {
	src := &loopingFailureSource{}
	sink := newFakeSink()
	c := newCollector(src, sink)
	c.Interval = time.Millisecond
	c.Jitter = 0

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if src.calls == 0 {
		t.Fatal("the source was not polled")
	}
	if len(sink.samples) != 0 {
		t.Fatalf("samples inserted on failure = %d", len(sink.samples))
	}
	if len(sink.runs) != src.calls {
		t.Fatalf("runs = %d, calls = %d", len(sink.runs), src.calls)
	}
	// The backoff (1 s minimum) limits the number of attempts over 50 ms.
	if src.calls > 3 {
		t.Fatalf("calls = %d: the backoff does not seem applied", src.calls)
	}
	// A single event despite several identical failures.
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
}

type loopingFailureSource struct{ calls int }

func (l *loopingFailureSource) GetPowerFlowRealtimeData(context.Context) (solarapi.PowerFlowEnvelope, error) {
	l.calls++
	return solarapi.PowerFlowEnvelope{}, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: "/x"}
}

func TestRunStopsImmediatelyWhenContextAlreadyCancelled(t *testing.T) {
	src := &fakeSource{}
	sink := newFakeSink()
	c := newCollector(src, sink)
	c.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.Run(ctx); err != nil {
		t.Fatalf("Run = %v", err)
	}
}

func TestNowIsInjectable(t *testing.T) {
	fixed := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	src := &fakeSource{results: []result{{env: solarapi.PowerFlowEnvelope{}}}}
	sink := newFakeSink()
	c := newCollector(src, sink)
	c.Now = func() time.Time { return fixed }

	if _, err := c.CollectOnce(context.Background()); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if sink.state[StateLastSuccess] != "2026-10-16T07:00:00Z" {
		t.Fatalf("last_success_utc = %q", sink.state[StateLastSuccess])
	}
	// Without a device timestamp, the sample is timestamped by the collector.
	if len(sink.samples) != 1 || !sink.samples[0].TS.Equal(fixed) {
		t.Fatalf("sample = %+v", sink.samples)
	}
	if sink.samples[0].TSSource != model.TSSourceCollector || sink.samples[0].Quality != model.QualityEstimated {
		t.Fatalf("origin = %q quality = %q", sink.samples[0].TSSource, sink.samples[0].Quality)
	}
}
