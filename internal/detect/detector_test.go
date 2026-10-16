package detect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// fakeReader is an in-memory detection source.
type fakeReader struct {
	version    internalapi.Version
	versionErr error

	components map[string]bool
	compErr    map[string]error

	events    []json.RawMessage
	eventsErr error

	mu           sync.Mutex
	versionCalls int
	compCalls    map[string]int
	eventCalls   int
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		components: map[string]bool{},
		compErr:    map[string]error{},
		compCalls:  map[string]int{},
	}
}

func (f *fakeReader) Version(context.Context) (internalapi.Version, error) {
	f.mu.Lock()
	f.versionCalls++
	f.mu.Unlock()
	return f.version, f.versionErr
}

func (f *fakeReader) ComponentPresent(_ context.Context, path string) (bool, error) {
	f.mu.Lock()
	f.compCalls[path]++
	err := f.compErr[path]
	f.mu.Unlock()
	if err != nil {
		return false, err
	}
	return f.components[path], nil
}

func (f *fakeReader) ActiveEvents(context.Context) ([]json.RawMessage, error) {
	f.mu.Lock()
	f.eventCalls++
	f.mu.Unlock()
	return f.events, f.eventsErr
}

func (f *fakeReader) calls() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.versionCalls, f.compCalls[internalapi.PowerMeterPath], f.eventCalls
}

// fakeSink is an in-memory store.
type fakeSink struct {
	mu        sync.Mutex
	devices   []storage.DeviceRecord
	events    []model.Event
	seen      map[string]bool
	state     map[string]string
	saveErr   error
	stateErr  error
	insertErr error
}

func newFakeSink() *fakeSink {
	return &fakeSink{seen: map[string]bool{}, state: map[string]string{}}
}

func (s *fakeSink) SaveDevice(_ context.Context, d storage.DeviceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.devices = append(s.devices, d)
	return nil
}

func (s *fakeSink) MarkDeviceEventSeen(_ context.Context, key string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

func (s *fakeSink) InsertEvent(_ context.Context, e model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return s.insertErr
	}
	s.events = append(s.events, e)
	return nil
}

func (s *fakeSink) SetState(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stateErr != nil {
		return s.stateErr
	}
	s.state[k] = v
	return nil
}

func (s *fakeSink) snapshot() ([]storage.DeviceRecord, []model.Event, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]model.Event(nil), s.events...)
	devices := append([]storage.DeviceRecord(nil), s.devices...)
	state := map[string]string{}
	for k, v := range s.state {
		state[k] = v
	}
	return devices, events, state
}

// fullyDetected configures a source where everything is present.
func fullyDetected(events ...string) *fakeReader {
	f := newFakeReader()
	f.version = internalapi.Version{
		SerialNumber:   new("SIMULATED-0001"),
		DeviceName:     new("Primo GEN24 3.6 Plus (simulated)"),
		ArticleNumber:  new("4,210,141,002"),
		Model:          new("Primo GEN24"),
		Firmware:       new("1.24.3-sim"),
		NumberOfPhases: new(3),
	}
	f.components[internalapi.PowerMeterPath] = true
	f.components[internalapi.BatteryPath] = false
	for _, raw := range events {
		f.events = append(f.events, json.RawMessage(raw))
	}
	return f
}

func newDetector(t *testing.T, reader Reader, sink Sink) *Detector {
	t.Helper()
	return &Detector{
		Source:   reader,
		Sink:     sink,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval: time.Minute,
		Now:      func() time.Time { return time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC) },
	}
}

func TestDetectOnceSuccess(t *testing.T) {
	reader := fullyDetected(`{"code":"SIM-0","state":"active","severity":"warning","message":"Simulated event 0"}`)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	if got.Identity.State != model.StatePresent || got.Identity.Source != internalapi.VersionPath {
		t.Fatalf("identity = %+v", got.Identity)
	}
	if got.Meter.State != model.StatePresent {
		t.Fatalf("meter = %+v", got.Meter)
	}
	if got.Battery.State != model.StateAbsent {
		t.Fatalf("battery = %+v", got.Battery)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("events = %+v", got.DeviceEvents)
	}

	devices, events, state := sink.snapshot()
	if len(devices) != 1 {
		t.Fatalf("SaveDevice called %d times, want 1", len(devices))
	}
	rec := devices[0]
	if rec.Model != "Primo GEN24" || rec.Firmware != "1.24.3-sim" || rec.SerialNumber != "SIMULATED-0001" {
		t.Fatalf("record = %+v", rec)
	}
	// The name and the article number travel with the identity; the name is not
	// the model, which stays whatever the payload reported (here a candidate).
	if rec.DeviceName != "Primo GEN24 3.6 Plus (simulated)" || rec.ArticleNumber != "4,210,141,002" {
		t.Fatalf("record identity = %+v", rec)
	}
	if rec.MeterState != model.StatePresent || rec.BatteryState != model.StateAbsent ||
		rec.DeviceEventsState != model.StatePresent || rec.IdentityState != model.StatePresent {
		t.Fatalf("recorded states = %+v", rec)
	}
	if rec.Source != internalapi.VersionPath ||
		rec.MeterSource != internalapi.PowerMeterPath ||
		rec.BatterySource != internalapi.BatteryPath {
		t.Fatalf("sources = %+v", rec)
	}
	if !rec.ReadAtUTC.Equal(d.Now()) {
		t.Fatalf("read_at_utc = %v", rec.ReadAtUTC)
	}

	if len(events) != 1 || events[0].Kind != model.EventDevice {
		t.Fatalf("inserted events = %+v", events)
	}
	if events[0].Severity != model.SeverityWarning || events[0].Message != "Simulated event 0" {
		t.Fatalf("event = %+v", events[0])
	}
	if !strings.Contains(events[0].Detail, "SIM-0") {
		t.Fatalf("the detail must keep the raw JSON: %q", events[0].Detail)
	}
	if state[StateLastError] != "" {
		t.Fatalf("detect_last_error = %q", state[StateLastError])
	}
	if state[StateLastReadUTC] == "" {
		t.Fatal("detect_last_read_utc must be filled in")
	}
}

func TestDetectOnceUnauthorizedWithCredentials(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.VersionPath}
	reader.compErr[internalapi.PowerMeterPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.PowerMeterPath}
	reader.compErr[internalapi.BatteryPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.BatteryPath}
	reader.eventsErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.ActiveEventsPath}

	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	d.CredentialsConfigured = true

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	for name, r := range map[string]model.ResourceDetection{
		"identity": got.Identity, "meter": got.Meter, "battery": got.Battery, "events": got.DeviceEvents,
	} {
		if r.State != model.StateUnknown {
			t.Fatalf("%s: state = %q, want unknown", name, r.State)
		}
		if r.Reason != ReasonUnauthorized {
			t.Fatalf("%s: reason = %q", name, r.Reason)
		}
	}

	// With credentials, the state is a real doubt: the paths remain polled
	// on every cycle (no abandonment).
	before, _, eventsBefore := reader.calls()
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	after, _, eventsAfter := reader.calls()
	if after != before+1 || eventsAfter != eventsBefore+1 {
		t.Fatalf("the paths must remain polled (version %d→%d, events %d→%d)",
			before, after, eventsBefore, eventsAfter)
	}
	_, _, state := sink.snapshot()
	if !strings.Contains(state[StateLastError], "401") {
		t.Fatalf("detect_last_error = %q", state[StateLastError])
	}
}

// TestDetectOnceUnauthorizedWithoutCredentialsStopsPolling: a 401 received without
// credentials must not be re-emitted on every cycle.
func TestDetectOnceUnauthorizedWithoutCredentialsStopsPolling(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.VersionPath}
	reader.compErr[internalapi.PowerMeterPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.PowerMeterPath}
	reader.compErr[internalapi.BatteryPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.BatteryPath}
	reader.eventsErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.ActiveEventsPath}

	sink := newFakeSink()
	d := newDetector(t, reader, sink) // CredentialsConfigured = false

	first, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	for name, r := range map[string]model.ResourceDetection{
		"identity": first.Identity, "meter": first.Meter, "battery": first.Battery, "events": first.DeviceEvents,
	} {
		if r.State != model.StateUnknown || r.Reason != ReasonNoCredentials {
			t.Fatalf("%s: %+v, want unknown with the missing-credentials reason", name, r)
		}
	}

	// Second cycle: no more requests.
	before, beforeMeter, beforeEvents := reader.calls()
	second, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	after, afterMeter, afterEvents := reader.calls()
	if after != before || afterMeter != beforeMeter || afterEvents != beforeEvents {
		t.Fatalf("requests were re-emitted (version %d→%d, meter %d→%d, events %d→%d)",
			before, after, beforeMeter, afterMeter, beforeEvents, afterEvents)
	}
	if second.Identity.State != model.StateUnknown || second.Identity.Reason != ReasonNoCredentials {
		t.Fatalf("second identity = %+v", second.Identity)
	}
}

func TestDetectOnceNotFound(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindNotFound, HTTPStatus: 404, Endpoint: internalapi.VersionPath}
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.Identity.State != model.StateUnknown || got.Identity.Reason != ReasonEndpointAbsent {
		t.Fatalf("identity = %+v", got.Identity)
	}
	if got.Identity.Reason == ReasonNoCredentials {
		t.Fatal("a 404 is not a credentials problem")
	}
}

func TestDetectOnceTransportError(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = errors.New("connection refused")
	reader.eventsErr = errors.New("deadline exceeded")
	reader.components[internalapi.PowerMeterPath] = true
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.Identity.State != model.StateUnknown || !strings.Contains(got.Identity.Reason, "connection refused") {
		t.Fatalf("identity = %+v", got.Identity)
	}
	if got.DeviceEvents.State != model.StateUnknown {
		t.Fatalf("events = %+v", got.DeviceEvents)
	}
	if got.Meter.State != model.StatePresent {
		t.Fatalf("the meter must remain independent of the failure: %+v", got.Meter)
	}

	devices, _, state := sink.snapshot()
	if len(devices) != 1 {
		t.Fatalf("SaveDevice called %d times", len(devices))
	}
	if state[StateLastError] == "" {
		t.Fatal("detect_last_error must be filled in")
	}
}

// TestEventDeduplication: the stream is republished in full on every cycle; an
// already-seen event produces no write.
func TestEventDeduplication(t *testing.T) {
	const first = `{"code":"SIM-0","state":"active","severity":"warning","message":"Simulated event 0"}`
	reader := fullyDetected(first)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("inserted events = %d, want 1 (anti-duplication)", len(events))
	}

	// A new event on the next cycle is inserted.
	reader.mu.Lock()
	reader.events = append(reader.events, json.RawMessage(`{"code":"SIM-1","state":"active","message":"Simulated event 1"}`))
	reader.mu.Unlock()

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	_, events, _ = sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("inserted events = %d, want 2", len(events))
	}
	if events[1].Severity != model.SeverityInfo {
		t.Fatalf("severity of the event without indication = %q", events[1].Severity)
	}
}

func TestEventWithoutTextOrCode(t *testing.T) {
	// Neither code, nor state, nor message: the key is the JSON fingerprint, and
	// the message is neutral.
	reader := fullyDetected(`{"state":{"nested":true}}`)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("events = %+v", got.DeviceEvents)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].Message != deviceEventMessage {
		t.Fatalf("message = %q", events[0].Message)
	}

	sink.mu.Lock()
	keys := len(sink.seen)
	sink.mu.Unlock()
	if keys != 1 {
		t.Fatalf("keys = %d", keys)
	}
}

func TestNoEventsIsPresentNotUnknown(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("no active event must not be a doubt: %+v", got.DeviceEvents)
	}
	devices, events, _ := sink.snapshot()
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0", len(events))
	}
	if devices[0].DeviceEventsState != model.StatePresent {
		t.Fatal("a successful read must be recorded `present`")
	}
}

func TestEventKeyPriorities(t *testing.T) {
	// Priority observed in the bundle (docs/ENDPOINTS.md): `uuid`, then
	// `prefix`-`eventID`, then `code`, then `state`, then the JSON fingerprint.
	key, err := eventKey(json.RawMessage(`{"uuid":"evt-1","code":"C1","state":"S1"}`))
	if err != nil || key != "device:evt-1" {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"prefix":"GEN24","eventID":21,"code":"C1"}`))
	if err != nil || key != "device:GEN24-21" {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	// Prefix absent: no invented composite key, fallback on the fingerprint.
	key, err = eventKey(json.RawMessage(`{"eventID":21}`))
	if err != nil || !strings.HasPrefix(key, "device:") || len(key) != len("device:")+16 {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"code":"C1","state":"S1"}`))
	if err != nil || key != "device:C1" {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"state":"S1","message":"m"}`))
	if err != nil || key != "device:S1" {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"message":"m","severity":2}`))
	if err != nil || !strings.HasPrefix(key, "device:") || len(key) != len("device:")+16 {
		t.Fatalf("key = %q (err=%v)", key, err)
	}
	if _, err := eventKey(json.RawMessage(`{not json`)); err == nil {
		t.Fatal("an unreadable JSON must produce an error")
	}
}

// Two active events with the same `code` but different instances must both be
// recorded: a deduplication key based on `code` alone lost one,
// since `code` designates an event *type*.
func TestSameCodeDistinctInstancesAreBothRecorded(t *testing.T) {
	payload := func(uuid string) string {
		return `{"uuid":"` + uuid + `","severity":2,"viewer":1,"timestamp":1789000000,` +
			`"activeUntil":0,"category":"GEN24","confirmable":true,` +
			`"label":"Module overtemperature","sourceID":65549,"prefix":"GEN24","eventID":21,` +
			`"code":"SIM-HEAT"}`
	}
	reader := fullyDetected(payload("evt-a"), payload("evt-b"))
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (distinct instances)", len(events))
	}
	for _, e := range events {
		if e.Severity != model.SeverityWarning {
			t.Fatalf("integer severity 2 → %q, want %q", e.Severity, model.SeverityWarning)
		}
		if e.Message != "Module overtemperature" {
			t.Fatalf("message = %q, want the `label` text", e.Message)
		}
		if !strings.Contains(e.Detail, `"confirmable":true`) {
			t.Fatalf("the raw detail must keep the payload: %q", e.Detail)
		}
	}

	sink.mu.Lock()
	_, hasA := sink.seen["device:evt-a"]
	_, hasB := sink.seen["device:evt-b"]
	sink.mu.Unlock()
	if !hasA || !hasB {
		t.Fatalf("recorded keys = %v, want device:evt-a and device:evt-b", sink.seen)
	}
}

func TestSeverityMapping(t *testing.T) {
	cases := map[string]model.Severity{
		// Real device shape: `severity` is an integer (bundle,
		// `getEventLevel` l. 135728-135733).
		`{"severity":1}`: model.SeverityError,
		`{"severity":2}`: model.SeverityWarning,
		`{"severity":3}`: model.SeverityInfo,
		// Integer out of range (0, 4, …) or non-integer: no invented severity.
		`{"severity":0}`:   model.SeverityInfo,
		`{"severity":4}`:   model.SeverityInfo,
		`{"severity":2.5}`: model.SeverityInfo,
		// Integer serialized as a string, by tolerance.
		`{"severity":"1"}`: model.SeverityError,
		`{"severity":"2"}`: model.SeverityWarning,
		// Text labels (simulator, fixtures).
		`{"severity":"warning"}`: model.SeverityWarning,
		`{"severity":"WARN"}`:    model.SeverityWarning,
		`{"severity":"error"}`:   model.SeverityError,
		`{"severity":"fault"}`:   model.SeverityError,
		`{"severity":"alert"}`:   model.SeverityError,
		`{"level":"warn"}`:       model.SeverityWarning,
		`{"level":1}`:            model.SeverityError,
		`{"severity":"info"}`:    model.SeverityInfo,
		`{"severity":"notice"}`:  model.SeverityInfo,
		`{}`:                     model.SeverityInfo,
		`{"severity":[]}`:        model.SeverityInfo,
		// The first interpretable key wins: `severity` before `level`.
		`{"severity":2,"level":"error"}`:       model.SeverityWarning,
		`{"severity":"notice","level":"warn"}`: model.SeverityInfo,
	}
	for body, want := range cases {
		var fields map[string]any
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatalf("Unmarshal(%s): %v", body, err)
		}
		if got := severityOf(fields); got != want {
			t.Fatalf("severityOf(%s) = %q, want %q", body, got, want)
		}
	}
}

func TestDetailIsBounded(t *testing.T) {
	long := `{"code":"SIM-LONG","padding":"` + strings.Repeat("Ж", 4000) + `"}`
	reader := fullyDetected(long)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	if len(events[0].Detail) > maxDetailBytes {
		t.Fatalf("detail = %d bytes", len(events[0].Detail))
	}
	if strings.ContainsRune(events[0].Detail, '\uFFFD') {
		t.Fatal("the truncation broke a UTF-8 character")
	}
}

func TestPersistenceErrorsAreReportedNotFatal(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	sink.saveErr = errors.New("disk full")
	d := newDetector(t, reader, sink)

	_, err := d.DetectOnce(context.Background())
	if err == nil {
		t.Fatal("a persistence error must be reported")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v", err)
	}
}

// TestRunStopsOnContextCancellation: stopping is a nominal case.
func TestRunStopsOnContextCancellation(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	d.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Let at least one cycle run.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	devices, _, _ := sink.snapshot()
	if len(devices) == 0 {
		t.Fatal("at least one cycle had to run")
	}
}

// TestRunSurvivesPersistenceErrors: an error in a cycle does not stop the loop.
func TestRunSurvivesPersistenceErrors(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	sink.saveErr = errors.New("disk full")
	d := newDetector(t, reader, sink)
	d.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	count, _, _ := reader.calls()
	if count < 2 {
		t.Fatalf("cycles executed = %d, want at least 2 (the loop must have stopped)", count)
	}
}
