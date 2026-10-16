// Tests of the two /commands/* reads: the device's own clock and the state of
// its grid-limits self-test.
//
// What is frozen here: the drift is the device's answer minus the host instant
// of the same read, a failed read stores an unknown state with its cause and NO
// device time (a drift computed from a clock that did not answer would be
// invented), and the drift events are emitted per transition, never per cycle.
package detect

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// fakeHealth is a HealthReader whose device clock is the host instant of the
// read plus a controlled offset. The detector's injected Now is fixed, so the
// offset read back as the drift is exactly the one set here.
type fakeHealth struct {
	host       time.Time
	offset     time.Duration
	clockErr   error
	state      string
	serviceErr error

	mu           sync.Mutex
	clockCalls   int
	serviceCalls int
	serviceIDs   []int
}

func newFakeHealth(d *Detector) *fakeHealth {
	return &fakeHealth{host: d.Now(), state: "IDLE"}
}

// setOffset changes the device clock offset for the following reads.
func (f *fakeHealth) setOffset(offset time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset = offset
}

func (f *fakeHealth) UTCTime(context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clockCalls++
	if f.clockErr != nil {
		return time.Time{}, f.clockErr
	}
	return f.host.Add(f.offset), nil
}

func (f *fakeHealth) ServiceState(_ context.Context, serviceID int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serviceCalls++
	f.serviceIDs = append(f.serviceIDs, serviceID)
	if f.serviceErr != nil {
		return "", f.serviceErr
	}
	return f.state, nil
}

func (f *fakeHealth) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clockCalls, f.serviceCalls
}

// TestDetectOnceStoresClockAndService drives one cycle with a device clock 42 s
// ahead of the host: the reading is stored with its drift, and 42 s is within
// tolerance so no event is published.
func TestDetectOnceStoresClockAndService(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.setOffset(42 * time.Second)
	d.Health = health

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	got, ok := sink.savedHealth()
	if !ok {
		t.Fatal("no health reading was recorded")
	}
	if !got.ReadAtUTC.Equal(d.Now()) {
		t.Fatalf("read_at_utc = %v, want the cycle instant %v", got.ReadAtUTC, d.Now())
	}
	if got.ClockState != model.StatePresent || got.ClockReason != "" {
		t.Fatalf("clock = %q / %q", got.ClockState, got.ClockReason)
	}
	if want := d.Now().Add(42 * time.Second); !got.ClockDeviceUTC.Equal(want) {
		t.Fatalf("clock_device_utc = %v, want %v", got.ClockDeviceUTC, want)
	}
	if got.ClockDriftMS != 42000 {
		t.Fatalf("clock_drift_ms = %d, want 42000", got.ClockDriftMS)
	}
	if got.ServiceState != model.StatePresent || got.ServiceValue != "IDLE" {
		t.Fatalf("service = %q / %q", got.ServiceState, got.ServiceValue)
	}

	_, events, _ := sink.snapshot()
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none: 42 s is within tolerance", events)
	}

	// The service is asked by id, and the id is the extracted constant: the
	// state of the grid-limits self-test.
	if len(health.serviceIDs) != 1 || health.serviceIDs[0] != internalapi.ServiceGridlimitsAutotest {
		t.Fatalf("service ids = %v, want [%d]", health.serviceIDs, internalapi.ServiceGridlimitsAutotest)
	}
}

// TestDetectOnceFailedClockReadStoresNoDrift: a read that fails stores an
// unknown clock with its classified cause and no device time. The previous row
// must not survive as if it were fresh — its state is overwritten.
func TestDetectOnceFailedClockReadStoresNoDrift(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.setOffset(90 * time.Second)
	d.Health = health

	// First cycle: a good reading, drifting beyond tolerance.
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	good, _ := sink.savedHealth()
	if good.ClockDriftMS != 90000 {
		t.Fatalf("first reading drift = %d ms", good.ClockDriftMS)
	}

	// Second cycle: the clock path answers an unreadable body.
	health.mu.Lock()
	health.clockErr = &froniuserr.Error{Kind: froniuserr.KindDecode, Endpoint: internalapi.UTCTimePath, Err: errors.New("bad epoch")}
	health.mu.Unlock()

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	got, ok := sink.savedHealth()
	if !ok {
		t.Fatal("no health reading was recorded")
	}
	if got.ClockState != model.StateUnknown {
		t.Fatalf("clock_state = %q, want unknown", got.ClockState)
	}
	if got.ClockReason == "" {
		t.Fatal("an unknown clock must carry its cause")
	}
	if !got.ClockDeviceUTC.IsZero() {
		t.Fatalf("clock_device_utc = %v, want the zero time", got.ClockDeviceUTC)
	}
	if got.ClockDriftMS != 0 {
		t.Fatalf("clock_drift_ms = %d, want 0 (no drift without a device time)", got.ClockDriftMS)
	}
	if got.ServiceState != model.StatePresent || got.ServiceValue != "IDLE" {
		t.Fatalf("service must still be read: %q / %q", got.ServiceState, got.ServiceValue)
	}
}

// TestDetectOnceFailedServiceReadIsClassified: the service path is independent
// of the clock path.
func TestDetectOnceFailedServiceReadIsClassified(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.serviceErr = &froniuserr.Error{Kind: froniuserr.KindNotFound, HTTPStatus: 404, Endpoint: internalapi.ServiceStatePath}
	d.Health = health

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	got, _ := sink.savedHealth()
	if got.ServiceState != model.StateUnknown || got.ServiceReason != ReasonEndpointAbsent {
		t.Fatalf("service = %q / %q", got.ServiceState, got.ServiceReason)
	}
	if got.ServiceValue != "" {
		t.Fatalf("service_value = %q, want empty", got.ServiceValue)
	}
	if got.ClockState != model.StatePresent {
		t.Fatalf("clock_state = %q, want present", got.ClockState)
	}
}

// TestClockDriftBoundary: exactly 60 s is within tolerance, 61 s is not, and the
// events are emitted once per transition.
func TestClockDriftBoundary(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	d.Health = health

	cycle := func(offset time.Duration) {
		t.Helper()
		health.setOffset(offset)
		if _, err := d.DetectOnce(context.Background()); err != nil {
			t.Fatalf("DetectOnce(%s): %v", offset, err)
		}
	}
	events := func() []model.Event {
		t.Helper()
		_, all, _ := sink.snapshot()
		return all
	}

	// Exactly 60 s: the boundary itself does not raise.
	cycle(60 * time.Second)
	if got := events(); len(got) != 0 {
		t.Fatalf("events = %+v, want none at exactly 60 s", got)
	}

	// 61 s: one warning, and only one.
	cycle(61 * time.Second)
	if got := events(); len(got) != 1 {
		t.Fatalf("events = %+v, want exactly one", got)
	} else if got[0].Kind != model.EventClockDrift || got[0].Severity != model.SeverityWarning {
		t.Fatalf("event = %+v, want a clock_drift warning", got[0])
	}

	// Two more cycles, still drifting: no new event.
	cycle(61 * time.Second)
	cycle(120 * time.Second)
	if got := events(); len(got) != 1 {
		t.Fatalf("events = %+v, want still exactly one: a transition, not a cycle", got)
	}

	// Back within tolerance: one recovery, and only one.
	cycle(59 * time.Second)
	if got := events(); len(got) != 2 {
		t.Fatalf("events = %+v, want two (drift then recovery)", got)
	} else if got[1].Kind != model.EventClockDriftRecovered || got[1].Severity != model.SeverityInfo {
		t.Fatalf("event = %+v, want a clock_drift_recovered info", got[1])
	}

	cycle(0)
	cycle(-90 * time.Second) // beyond tolerance again, negative side
	if got := events(); len(got) != 3 {
		t.Fatalf("events = %+v, want three", got)
	} else if got[2].Kind != model.EventClockDrift || got[2].Severity != model.SeverityWarning {
		t.Fatalf("event = %+v, want a clock_drift warning", got[2])
	}

	// The detail keeps the measurement, not just the verdict.
	all := events()
	last := all[len(all)-1]
	if last.Detail == "" {
		t.Fatal("the drift event must carry its measurement")
	}
	if !last.TS.Equal(d.Now()) {
		t.Fatalf("event timestamp = %v, want the cycle instant %v", last.TS, d.Now())
	}
}

// TestClockDriftNegativeIsReported: the sign does not hide a drift.
func TestClockDriftNegativeIsReported(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.setOffset(-61 * time.Second)
	d.Health = health

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	got, _ := sink.savedHealth()
	if got.ClockDriftMS != -61000 {
		t.Fatalf("clock_drift_ms = %d, want -61000", got.ClockDriftMS)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 1 || events[0].Kind != model.EventClockDrift {
		t.Fatalf("events = %+v, want one clock_drift", events)
	}
}

// TestFailedClockReadKeepsTheDriftVerdict: a read that fails is not a verdict, so
// it neither notifies again nor recovers.
func TestFailedClockReadKeepsTheDriftVerdict(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.setOffset(300 * time.Second)
	d.Health = health

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	health.mu.Lock()
	health.clockErr = errors.New("connection refused")
	health.mu.Unlock()
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %+v, want the single drift warning: an unread clock draws no conclusion", events)
	}
}

// TestDetectorWithoutHealthReaderKeepsWorking: the capability is optional, like
// the Solar API. No reader means no health read AND no health row.
func TestDetectorWithoutHealthReaderKeepsWorking(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink) // Health is nil

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if sink.healthWrites() != 0 {
		t.Fatalf("health writes = %d, want 0: nothing was read", sink.healthWrites())
	}
	devices, _, _ := sink.snapshot()
	if len(devices) != 1 {
		t.Fatalf("SaveDevice called %d times, want 1", len(devices))
	}
}

// TestHealthPersistenceErrorIsReportedNotFatal: the cycle still completes.
func TestHealthPersistenceErrorIsReportedNotFatal(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	sink.healthErr = errors.New("disk full")
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	d.Health = health

	_, err := d.DetectOnce(context.Background())
	if err == nil {
		t.Fatal("a persistence error must be reported")
	}
	if !errors.Is(err, sink.healthErr) {
		t.Fatalf("error = %v, want the health write failure", err)
	}
}

// TestClockAbandonedWithoutCredentials: a 401 received without credentials stops
// the two /commands/* reads instead of re-emitting them every cycle.
func TestClockAbandonedWithoutCredentials(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.clockErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.UTCTimePath}
	health.serviceErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.ServiceStatePath}
	d.Health = health

	_, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	got, _ := sink.savedHealth()
	if got.ClockState != model.StateUnknown || got.ClockReason != ReasonNoCredentials {
		t.Fatalf("clock = %q / %q", got.ClockState, got.ClockReason)
	}
	if got.ServiceState != model.StateUnknown || got.ServiceReason != ReasonNoCredentials {
		t.Fatalf("service = %q / %q", got.ServiceState, got.ServiceReason)
	}

	beforeClock, beforeService := health.calls()
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	afterClock, afterService := health.calls()
	if afterClock != beforeClock || afterService != beforeService {
		t.Fatalf("the abandoned paths were polled again (%d→%d, %d→%d)",
			beforeClock, afterClock, beforeService, afterService)
	}
}

// TestHealthRecordShapeIsStable pins the storage row the detector writes, so a
// rename or a reordering of the fields breaks here rather than in the database.
func TestHealthRecordShapeIsStable(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	health := newFakeHealth(d)
	health.setOffset(-2 * time.Second)
	d.Health = health

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	got, _ := sink.savedHealth()
	want := storage.DeviceHealth{
		ReadAtUTC:      d.Now(),
		ClockState:     model.StatePresent,
		ClockDeviceUTC: d.Now().Add(-2 * time.Second),
		ClockDriftMS:   -2000,
		ServiceState:   model.StatePresent,
		ServiceValue:   "IDLE",
	}
	if !got.ReadAtUTC.Equal(want.ReadAtUTC) || !got.ClockDeviceUTC.Equal(want.ClockDeviceUTC) {
		t.Fatalf("timestamps = %v / %v, want %v / %v",
			got.ReadAtUTC, got.ClockDeviceUTC, want.ReadAtUTC, want.ClockDeviceUTC)
	}
	got.ReadAtUTC, got.ClockDeviceUTC = time.Time{}, time.Time{}
	want.ReadAtUTC, want.ClockDeviceUTC = time.Time{}, time.Time{}
	if got != want {
		t.Fatalf("health row = %+v, want %+v", got, want)
	}
}
