package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
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
	mu          sync.Mutex
	results     []result
	idx         int
	calls       int
	realtime    solarapi.InverterRealtime
	detailErr   error
	detailCalls int
}

// InverterRealtime answers the second read of a cycle: the inverter's state and
// electrical detail. The zero value is an empty answer, which is what the
// collector stores when nothing configured it.
func (f *fakeSource) InverterRealtime(context.Context, string) (solarapi.InverterRealtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detailCalls++
	if f.detailErr != nil {
		return solarapi.InverterRealtime{}, f.detailErr
	}
	return f.realtime, nil
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
	mu        sync.Mutex
	details   []storage.Detail
	detailErr error
	samples   []model.Sample
	events    []model.Event
	runs      []storage.RunRecord
	state     map[string]string
	insertErr error
	eventErr  error
	runErr    error
}

func newFakeSink() *fakeSink { return &fakeSink{state: map[string]string{}} }

func (f *fakeSink) SaveDetails(_ context.Context, details []storage.Detail) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.detailErr != nil {
		return f.detailErr
	}
	f.details = append(f.details, details...)
	return nil
}

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

func (l *loopingSource) InverterRealtime(context.Context, string) (solarapi.InverterRealtime, error) {
	return solarapi.InverterRealtime{}, nil
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

func (l *loopingFailureSource) InverterRealtime(context.Context, string) (solarapi.InverterRealtime, error) {
	return solarapi.InverterRealtime{}, nil
}

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

// TestTheInverterDetailIsReadOnEveryOtherCycle: the Solar API specification
// (§2.6) allows at most two realtime requests in parallel and prescribes four
// seconds between two consecutive calls. One collection cycle already asks for
// the flow; asking for the detail in the same breath would put two requests back
// to back, so it is read on every other cycle — which, at the default
// five-second interval, keeps consecutive requests five seconds apart.
func TestTheInverterDetailIsReadOnEveryOtherCycle(t *testing.T) {
	env := okEnvelope("2026-10-16T09:17:43+02:00", 100)
	src := &fakeSource{results: []result{{env: env}, {env: env}, {env: env}, {env: env}}}
	sink := newFakeSink()
	c := newCollector(src, sink)

	for i := range 4 {
		if _, err := c.CollectOnce(context.Background()); err != nil {
			t.Fatalf("cycle %d: %v", i+1, err)
		}
	}

	src.mu.Lock()
	got := src.detailCalls
	src.mu.Unlock()
	if got != 2 {
		t.Errorf("detail reads over four cycles = %d, want 2 (every other cycle)", got)
	}
	if n := len(sink.details); n != 2 {
		t.Errorf("recorded families = %d, want one per read", n)
	}
}

// ---------------------------------------------------------------------------
// Fault taxonomy against a real HTTP server.
//
// The fault classes are not fabricated here: a real solarapi.Client, guarded by
// fronius.ReadOnly like production, points at an httptest server and the
// collector consumes it through its Source. What is pinned is therefore the
// whole chain — status/transport/decode classification, the run's ErrorKind, the
// warning event and the resulting availability state (last_error filled,
// last_success_utc untouched, no sample).

// httpFlowBody is a valid GetPowerFlowRealtimeData answer, minimal but complete
// enough for model.FromPowerFlow to produce a measurement.
const httpFlowBody = `{
  "Body": {"Data": {
    "Site": {"Mode":"produce-only","P_PV":1902.4,"P_Grid":null,"P_Load":null,"P_Akku":null,
             "E_Day":12345.6,"E_Year":4800000.0,"E_Total":12000000.0,
             "rel_Autonomy":null,"rel_SelfConsumption":null,"Meter_Location":"unknown"},
    "Inverters": {"1": {"DT":1,"P":1902.4,"E_Day":12345.6,"E_Year":4800000.0,"E_Total":12000000.0}}
  }},
  "Head": {"RequestArguments":{},"Status":{"Code":0},"Timestamp":"2026-10-16T09:17:43+02:00"}
}`

// httpRealtimeBody is a valid GetInverterRealtimeData answer (the detail read).
const httpRealtimeBody = `{
  "Body": {"Data": {
    "DeviceStatus": {"InverterState":"Running","StatusCode":0,"ErrorCode":0},
    "VOLTAGE_DC_1": {"Unit":"V","Value":600.0}
  }},
  "Head": {"Status":{"Code":0},"Timestamp":"2026-10-16T09:17:43+02:00"}
}`

// httpSource adapts a production solarapi.Client to the collector's Source, so
// that the fault classes are classified by the real code path.
type httpSource struct{ client *solarapi.Client }

func (s httpSource) GetPowerFlowRealtimeData(ctx context.Context) (solarapi.PowerFlowEnvelope, error) {
	return s.client.GetPowerFlowRealtimeData(ctx)
}

func (s httpSource) InverterRealtime(ctx context.Context, query string) (solarapi.InverterRealtime, error) {
	return s.client.InverterRealtime(ctx, query)
}

// newHTTPCollector builds a collector reading through a real client aimed at
// baseURL, like production (ReadOnly + timeout), with a frozen clock.
func newHTTPCollector(t *testing.T, baseURL string, timeout time.Duration, sink Sink) *Collector {
	t.Helper()
	client, err := solarapi.New(baseURL, fronius.ReadOnly{Inner: &http.Client{}}, timeout, testLogger())
	if err != nil {
		t.Fatalf("solarapi.New: %v", err)
	}
	c := &Collector{
		Source: httpSource{client}, Sink: sink, Log: testLogger(),
		Interval: time.Hour, Jitter: 0,
		Now: func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) },
	}
	return c
}

// singleStatusServer answers every request with one status and body.
func singleStatusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// closedServerURL returns the URL of a server that is already closed: every
// request is refused, which is a transport failure.
func closedServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

// slowServerURL answers only once the request context is done: with a short
// client timeout, the request expires — a timeout, not a transport failure.
func slowServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestFaultClasses pins one fault class per case: the recorded run's ErrorKind
// and HTTPStatus, the warning event and the availability state the operator
// ends up seeing. The error kinds are the ones the code actually emits
// (froniuserr.Kind*), never invented ones.
func TestFaultClasses(t *testing.T) {
	cases := []struct {
		name       string
		base       func(*testing.T) string
		timeout    time.Duration
		wantKind   froniuserr.Kind
		wantStatus int
	}{
		{
			name: "401", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusUnauthorized, "denied").URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindUnauthorized, wantStatus: 401,
		},
		{
			name: "403", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusForbidden, "denied").URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindUnauthorized, wantStatus: 403,
		},
		{
			name: "404", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusNotFound, "no such path").URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindNotFound, wantStatus: 404,
		},
		{
			name: "429", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusTooManyRequests, "slow down").URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindServer, wantStatus: 429,
		},
		{
			name: "500", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusInternalServerError, "boom").URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindServer, wantStatus: 500,
		},
		{
			name: "503", base: func(t *testing.T) string {
				return singleStatusServer(t, http.StatusServiceUnavailable, "unavailable").URL
			},
			timeout: 2 * time.Second, wantKind: froniuserr.KindServer, wantStatus: 503,
		},
		{
			name: "transport", base: closedServerURL,
			timeout: 2 * time.Second, wantKind: froniuserr.KindTransport, wantStatus: 0,
		},
		{
			name: "timeout", base: slowServerURL,
			timeout: 30 * time.Millisecond, wantKind: froniuserr.KindTimeout, wantStatus: 0,
		},
		{
			name: "malformed json", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusOK, `{"Body":{"Data":`).URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindDecode, wantStatus: 200,
		},
		{
			name: "empty body", base: func(t *testing.T) string { return singleStatusServer(t, http.StatusOK, `{}`).URL },
			timeout: 2 * time.Second, wantKind: froniuserr.KindDecode, wantStatus: 200,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := newFakeSink()
			c := newHTTPCollector(t, tc.base(t), tc.timeout, sink)

			if _, err := c.CollectOnce(context.Background()); err == nil {
				t.Fatal("a fault must be reported as an error")
			} else if kind, ok := froniuserr.KindOf(err); !ok || kind != tc.wantKind {
				t.Fatalf("KindOf = (%q, %v), want %q", kind, ok, tc.wantKind)
			}

			if len(sink.samples) != 0 {
				t.Fatalf("a fault must record no sample (%d inserted)", len(sink.samples))
			}
			if len(sink.runs) != 1 {
				t.Fatalf("runs = %d, want 1", len(sink.runs))
			}
			run := sink.runs[0]
			if run.OK {
				t.Fatal("the run must be recorded as failed")
			}
			if run.ErrorKind != string(tc.wantKind) {
				t.Fatalf("run.ErrorKind = %q, want %q", run.ErrorKind, tc.wantKind)
			}
			if run.HTTPStatus != tc.wantStatus {
				t.Fatalf("run.HTTPStatus = %d, want %d", run.HTTPStatus, tc.wantStatus)
			}
			if run.Error == "" {
				t.Fatal("the run must keep the cause")
			}

			if len(sink.events) != 1 {
				t.Fatalf("events = %d, want 1: %+v", len(sink.events), sink.events)
			}
			ev := sink.events[0]
			if ev.Kind != model.EventCollectorError || ev.Severity != model.SeverityWarning {
				t.Fatalf("event kind/severity = %q/%q, want %q/%q",
					ev.Kind, ev.Severity, model.EventCollectorError, model.SeverityWarning)
			}
			if ev.Detail == "" {
				t.Fatal("the event must keep the cause")
			}

			// Availability: last_error carries the fault, last_success_utc is
			// untouched (never a fabricated success instant).
			if sink.state[StateLastError] == "" {
				t.Fatal("last_error must be filled in")
			}
			if sink.state[StateLastSuccess] != "" {
				t.Fatalf("last_success_utc = %q, want untouched", sink.state[StateLastSuccess])
			}
		})
	}
}

// TestIdenticalFaultsDoNotSpamEvents: N consecutive identical failures write N
// runs but a single event (the collector deduplicates on the error message).
func TestIdenticalFaultsDoNotSpamEvents(t *testing.T) {
	srv := singleStatusServer(t, http.StatusServiceUnavailable, "unavailable")
	sink := newFakeSink()
	c := newHTTPCollector(t, srv.URL, 2*time.Second, sink)
	ctx := context.Background()

	for i := range 4 {
		if _, err := c.CollectOnce(ctx); err == nil {
			t.Fatalf("cycle %d: an error was expected", i+1)
		}
	}

	if len(sink.runs) != 4 {
		t.Fatalf("runs = %d, want 4 (every cycle is traced)", len(sink.runs))
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1 (anti-duplication): %+v", len(sink.events), sink.events)
	}
}

// TestRecoveryIsRecordedOnceAndRestoresAvailability: after a series of
// failures, the first success writes exactly one recovery event and clears
// last_error, whatever the number of following successes.
func TestRecoveryIsRecordedOnceAndRestoresAvailability(t *testing.T) {
	var flowRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == solarapi.InverterRealtimePath {
			_, _ = io.WriteString(w, httpRealtimeBody)
			return
		}
		// The first flow request fails, every later one succeeds.
		if flowRequests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, httpFlowBody)
	}))
	t.Cleanup(srv.Close)

	sink := newFakeSink()
	c := newHTTPCollector(t, srv.URL, 2*time.Second, sink)
	ctx := context.Background()

	if _, err := c.CollectOnce(ctx); err == nil {
		t.Fatal("the first cycle must fail")
	}
	for i := range 3 {
		if _, err := c.CollectOnce(ctx); err != nil {
			t.Fatalf("success cycle %d: %v", i+1, err)
		}
	}

	if len(sink.samples) != 3 {
		t.Fatalf("samples = %d, want 3 (one per successful cycle)", len(sink.samples))
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2 (one failure, one recovery): %+v", len(sink.events), sink.events)
	}
	if sink.events[0].Kind != model.EventCollectorError {
		t.Fatalf("first event = %+v", sink.events[0])
	}
	recovery := sink.events[1]
	if recovery.Kind != model.EventCollectorRecovered || recovery.Severity != model.SeverityInfo {
		t.Fatalf("recovery event = %+v", recovery)
	}
	if sink.state[StateLastError] != "" {
		t.Fatalf("last_error = %q, want cleared", sink.state[StateLastError])
	}
	if sink.state[StateLastSuccess] == "" {
		t.Fatal("last_success_utc must be restored")
	}
}

// TestTransientFailureKeepsPriorHistory: a fault does not erase what was
// recorded before it — the samples and runs already in the sink survive.
func TestTransientFailureKeepsPriorHistory(t *testing.T) {
	var flowRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == solarapi.InverterRealtimePath {
			_, _ = io.WriteString(w, httpRealtimeBody)
			return
		}
		// First two flow requests succeed, the third fails, the rest succeed.
		switch flowRequests.Add(1) {
		case 3:
			w.WriteHeader(http.StatusInternalServerError)
		default:
			_, _ = io.WriteString(w, httpFlowBody)
		}
	}))
	t.Cleanup(srv.Close)

	sink := newFakeSink()
	c := newHTTPCollector(t, srv.URL, 2*time.Second, sink)
	ctx := context.Background()

	for range 2 {
		if _, err := c.CollectOnce(ctx); err != nil {
			t.Fatalf("pre-fault cycle: %v", err)
		}
	}
	before := len(sink.samples)
	if before != 2 {
		t.Fatalf("pre-fault samples = %d, want 2", before)
	}

	if _, err := c.CollectOnce(ctx); err == nil {
		t.Fatal("the third cycle must fail")
	}
	if len(sink.samples) != before {
		t.Fatalf("samples after the fault = %d, want %d untouched", len(sink.samples), before)
	}
	if sink.samples[0].PowerPVW == nil || *sink.samples[0].PowerPVW != 1902.4 {
		t.Fatalf("the historical sample was altered: %+v", sink.samples[0])
	}
	if len(sink.runs) != 3 {
		t.Fatalf("runs = %d, want the 2 successes plus the failure", len(sink.runs))
	}
	if !sink.runs[0].OK || !sink.runs[1].OK || sink.runs[2].OK {
		t.Fatalf("runs = %+v", sink.runs)
	}

	// Recovery: the next success appends without disturbing the history.
	if _, err := c.CollectOnce(ctx); err != nil {
		t.Fatalf("recovery cycle: %v", err)
	}
	if len(sink.samples) != before+1 {
		t.Fatalf("samples = %d, want %d", len(sink.samples), before+1)
	}
}

// TestRateLimitAndServerErrorsBackOff: a 429 and a 5xx are both classified as
// server faults and share the same backoff. The collector's Run loop offers no
// injectable clock or timer, so the observable effect is the bounded number of
// attempts inside a fixed deadline (the pure schedule is pinned by
// TestBackoffSchedule) — no wall-clock sleep is added here.
func TestRateLimitAndServerErrorsBackOff(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)

			sink := newFakeSink()
			c := newHTTPCollector(t, srv.URL, 2*time.Second, sink)
			c.Interval = time.Millisecond
			c.Jitter = 0

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := c.Run(ctx); err != nil {
				t.Fatalf("Run = %v", err)
			}

			// The 1 s minimum backoff limits the attempts over 50 ms.
			if n := calls.Load(); n < 1 || n > 3 {
				t.Fatalf("attempts = %d: the backoff does not seem applied", n)
			}
			if len(sink.runs) == 0 {
				t.Fatal("no run recorded")
			}
			if sink.runs[0].ErrorKind != string(froniuserr.KindServer) {
				t.Fatalf("ErrorKind = %q, want %q", sink.runs[0].ErrorKind, froniuserr.KindServer)
			}
			if len(sink.events) != 1 {
				t.Fatalf("events = %d, want 1 (anti-duplication)", len(sink.events))
			}
		})
	}
}

// TestInverterDetailFailureIsRecorded: the second read of the cycle failing
// (here a 503 on the realtime path) does not fail the cycle; it records the
// inverter family as unavailable with the cause, and no fabricated data.
func TestInverterDetailFailureIsRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == solarapi.InverterRealtimePath {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, httpFlowBody)
	}))
	t.Cleanup(srv.Close)

	sink := newFakeSink()
	c := newHTTPCollector(t, srv.URL, 2*time.Second, sink)

	sm, err := c.CollectOnce(context.Background())
	if err != nil {
		t.Fatalf("a detail failure must not fail the cycle: %v", err)
	}
	if sm.PowerPVW == nil {
		t.Fatalf("the sample was not collected: %+v", sm)
	}
	if len(sink.details) != 1 {
		t.Fatalf("details = %d, want 1", len(sink.details))
	}
	d := sink.details[0]
	if d.Family != model.FamilyInverter {
		t.Fatalf("family = %q", d.Family)
	}
	if d.Available {
		t.Fatal("a failed read must be recorded unavailable")
	}
	if d.Reason == "" {
		t.Fatal("the cause must be recorded")
	}
	if string(d.Data) != "{}" {
		t.Fatalf("data = %q, want the empty object", d.Data)
	}
	if !d.ReadAtUTC.Equal(c.Now()) {
		t.Fatalf("read_at = %v, want the injected instant", d.ReadAtUTC)
	}
}

// TestInverterDetailReasonIsBounded: an over-long cause is truncated to
// maxDetailReason rather than stored whole.
func TestInverterDetailReasonIsBounded(t *testing.T) {
	long := strings.Repeat("x", maxDetailReason+100)
	src := &fakeSource{
		results:   []result{{env: okEnvelope("2026-10-16T09:17:43+02:00", 1)}},
		detailErr: errors.New(long),
	}
	sink := newFakeSink()
	c := newCollector(src, sink)

	if _, err := c.CollectOnce(context.Background()); err != nil {
		t.Fatalf("CollectOnce: %v", err)
	}
	if len(sink.details) != 1 {
		t.Fatalf("details = %d, want 1", len(sink.details))
	}
	if got := sink.details[0].Reason; got != long[:maxDetailReason] {
		t.Fatalf("reason length = %d, want %d", len(got), maxDetailReason)
	}
}

// TestInverterDetailSaveFailureDoesNotFailTheCycle: losing the snapshot write
// is a warning, not a collection failure.
func TestInverterDetailSaveFailureDoesNotFailTheCycle(t *testing.T) {
	src := &fakeSource{results: []result{{env: okEnvelope("2026-10-16T09:17:43+02:00", 1)}}}
	sink := newFakeSink()
	sink.detailErr = errors.New("disk full")
	c := newCollector(src, sink)

	if _, err := c.CollectOnce(context.Background()); err != nil {
		t.Fatalf("CollectOnce = %v, want nil", err)
	}
	if len(sink.details) != 0 {
		t.Fatalf("details = %d, want none recorded", len(sink.details))
	}
	if len(sink.samples) != 1 {
		t.Fatalf("samples = %d, want the cycle to have succeeded", len(sink.samples))
	}
}

// TestUnserialisableSnapshotIsUnavailableNotFatal: a measurement JSON cannot
// carry (a non-finite value) makes the snapshot serialisation fail; the family
// is then recorded unavailable with an explicit cause, never with the previous
// data and never by failing the cycle.
func TestUnserialisableSnapshotIsUnavailableNotFatal(t *testing.T) {
	nan := math.NaN()
	src := &fakeSource{
		results: []result{{env: okEnvelope("2026-10-16T09:17:43+02:00", 1)}},
		realtime: solarapi.InverterRealtime{
			Measurements: map[string]solarapi.Measurement{"VOLTAGE_DC_1": {Unit: "V", Value: &nan}},
		},
	}
	sink := newFakeSink()
	c := newCollector(src, sink)

	if _, err := c.CollectOnce(context.Background()); err != nil {
		t.Fatalf("CollectOnce = %v, want nil", err)
	}
	if len(sink.details) != 1 {
		t.Fatalf("details = %d, want 1", len(sink.details))
	}
	d := sink.details[0]
	if d.Available {
		t.Fatal("an unserialisable snapshot must be unavailable")
	}
	if d.Reason != "the snapshot could not be serialised" {
		t.Fatalf("reason = %q", d.Reason)
	}
	if string(d.Data) != "{}" {
		t.Fatalf("data = %q, want the empty object", d.Data)
	}
}
