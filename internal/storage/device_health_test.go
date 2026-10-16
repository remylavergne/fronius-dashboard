// Tests of the last health reading (device clock + grid-limits self-test
// state).
//
// The point of these tests is the distinction the schema carries: "unread" is
// not a fabricated timestamp, and a genuine zero drift is not an unread clock.
package storage

import (
	"context"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

func healthReading(ts time.Time) DeviceHealth {
	return DeviceHealth{
		ReadAtUTC:      ts,
		ClockState:     model.StatePresent,
		ClockDeviceUTC: ts.Add(2 * time.Second),
		ClockDriftMS:   2000,
		ServiceState:   model.StatePresent,
		ServiceValue:   "ready",
	}
}

func TestDeviceHealthRoundTrip(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	// The device clock can lag: a negative drift is a real reading, not an
	// error, and must survive the round trip.
	behind := DeviceHealth{
		ReadAtUTC:      base,
		ClockState:     model.StatePresent,
		ClockDeviceUTC: base.Add(-45 * time.Second),
		ClockDriftMS:   -45_000,
		ServiceState:   model.StatePresent,
		ServiceValue:   "passed",
	}
	if err := s.SaveDeviceHealth(ctx, behind); err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}
	got, ok, err := s.DeviceHealth(ctx)
	if err != nil || !ok {
		t.Fatalf("DeviceHealth: ok=%v err=%v", ok, err)
	}
	if !got.ReadAtUTC.Equal(base) {
		t.Errorf("ReadAtUTC = %v, want %v", got.ReadAtUTC, base)
	}
	if got.ClockState != model.StatePresent {
		t.Errorf("ClockState = %q, want present", got.ClockState)
	}
	if !got.ClockDeviceUTC.Equal(base.Add(-45 * time.Second)) {
		t.Errorf("ClockDeviceUTC = %v, want %v", got.ClockDeviceUTC, base.Add(-45*time.Second))
	}
	if got.ClockDriftMS != -45_000 {
		t.Errorf("ClockDriftMS = %d, want -45000", got.ClockDriftMS)
	}
	if got.ServiceState != model.StatePresent || got.ServiceValue != "passed" {
		t.Errorf("service = (%q, %q), want (present, passed)", got.ServiceState, got.ServiceValue)
	}

	// A drift of exactly 0 with a valid device time is a measurement, not an
	// unread clock: the valid timestamp is what tells them apart.
	onTime := healthReading(base.Add(time.Hour))
	onTime.ClockDeviceUTC = base.Add(time.Hour)
	onTime.ClockDriftMS = 0
	if err := s.SaveDeviceHealth(ctx, onTime); err != nil {
		t.Fatalf("SaveDeviceHealth (zero drift): %v", err)
	}
	got, ok, err = s.DeviceHealth(ctx)
	if err != nil || !ok {
		t.Fatalf("DeviceHealth (zero drift): ok=%v err=%v", ok, err)
	}
	if got.ClockDriftMS != 0 {
		t.Errorf("ClockDriftMS = %d, want a stored 0", got.ClockDriftMS)
	}
	if got.ClockDeviceUTC.IsZero() {
		t.Error("a valid device time came back zero: an unread clock is not a zero drift")
	}
	if !got.ClockDeviceUTC.Equal(base.Add(time.Hour)) {
		t.Errorf("ClockDeviceUTC = %v, want %v", got.ClockDeviceUTC, base.Add(time.Hour))
	}
}

func TestDeviceHealthNeverRead(t *testing.T) {
	s := openStore(t, brussels(t))
	got, ok, err := s.DeviceHealth(context.Background())
	if err != nil {
		t.Fatalf("DeviceHealth: %v", err)
	}
	if ok {
		t.Fatalf("DeviceHealth on a fresh database = %+v, want not found", got)
	}
}

func TestDeviceHealthIsASingleReplacedRow(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	if err := s.SaveDeviceHealth(ctx, healthReading(base)); err != nil {
		t.Fatalf("first SaveDeviceHealth: %v", err)
	}
	second := healthReading(base.Add(time.Minute))
	second.ClockReason = ""
	second.ServiceValue = "running"
	if err := s.SaveDeviceHealth(ctx, second); err != nil {
		t.Fatalf("second SaveDeviceHealth: %v", err)
	}

	var rows int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM device_health`).Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("device_health rows = %d, want a single replaced row", rows)
	}

	got, ok, err := s.DeviceHealth(ctx)
	if err != nil || !ok {
		t.Fatalf("DeviceHealth: ok=%v err=%v", ok, err)
	}
	if !got.ReadAtUTC.Equal(base.Add(time.Minute)) {
		t.Errorf("ReadAtUTC = %v, want the second save to win", got.ReadAtUTC)
	}
	if got.ServiceValue != "running" {
		t.Errorf("ServiceValue = %q, want the second save to win", got.ServiceValue)
	}
}

func TestDeviceHealthUnmeasuredReading(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	// The clock read failed: no device time, no drift, but the cause is kept.
	// The service side answered, so it keeps its value.
	failed := DeviceHealth{
		ReadAtUTC:    base,
		ClockState:   model.StateUnknown,
		ClockReason:  "read /commands/GetUTCTime: device answered 401",
		ServiceState: model.StatePresent,
		ServiceValue: "ready",
	}
	if err := s.SaveDeviceHealth(ctx, failed); err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	got, ok, err := s.DeviceHealth(ctx)
	if err != nil || !ok {
		t.Fatalf("DeviceHealth: ok=%v err=%v", ok, err)
	}
	if got.ClockState != model.StateUnknown {
		t.Errorf("ClockState = %q, want unknown", got.ClockState)
	}
	if got.ClockReason != failed.ClockReason {
		t.Errorf("ClockReason = %q, want %q", got.ClockReason, failed.ClockReason)
	}
	if !got.ClockDeviceUTC.IsZero() {
		t.Errorf("ClockDeviceUTC = %v, want the zero time (unread, not fabricated)", got.ClockDeviceUTC)
	}
	if got.ClockDriftMS != 0 {
		t.Errorf("ClockDriftMS = %d, want 0 (no drift invented)", got.ClockDriftMS)
	}
	// The NULL in the row is what says "unread": it is not a stored zero.
	var deviceUTC *string
	var drift *int64
	if err := s.DB().QueryRowContext(ctx,
		`SELECT clock_device_utc, clock_drift_ms FROM device_health`).Scan(&deviceUTC, &drift); err != nil {
		t.Fatalf("reading raw columns: %v", err)
	}
	if deviceUTC != nil || drift != nil {
		t.Errorf("stored (device_utc=%v, drift=%v), want NULL/NULL for an unread clock", deviceUTC, drift)
	}
	if got.ServiceValue != "ready" {
		t.Errorf("ServiceValue = %q, want ready", got.ServiceValue)
	}

	// An empty service value is stored as the empty string, not as NULL: it is
	// simply a string the device did not report.
	blank := failed
	blank.ServiceState = model.StateUnknown
	blank.ServiceReason = "read /commands/GetServiceState: timeout"
	blank.ServiceValue = ""
	if err := s.SaveDeviceHealth(ctx, blank); err != nil {
		t.Fatalf("SaveDeviceHealth (blank service): %v", err)
	}
	got, ok, err = s.DeviceHealth(ctx)
	if err != nil || !ok {
		t.Fatalf("DeviceHealth (blank service): ok=%v err=%v", ok, err)
	}
	if got.ServiceValue != "" || got.ServiceReason != blank.ServiceReason {
		t.Errorf("service = (%q, %q), want the empty value and its reason", got.ServiceValue, got.ServiceReason)
	}
}

func TestDeviceHealthLandsOnAPopulatedDatabase(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	// Existing rows of the older tables, written before the health table is
	// touched: the new reading must not disturb them.
	mustInsert(t, s, sample(base, 1000))
	dev := deviceRecord(base, "Primo GEN24 3.6 Plus")
	if err := s.SaveDevice(ctx, dev); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	if err := s.SaveDeviceHealth(ctx, healthReading(base)); err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	if n, err := s.SampleCount(ctx); err != nil || n != 1 {
		t.Errorf("SampleCount = %d (err=%v), want the existing 1", n, err)
	}
	if got, ok, err := s.Device(ctx); err != nil || !ok || got.SerialNumber != dev.SerialNumber {
		t.Errorf("Device after health write = %+v (ok=%v err=%v)", got, ok, err)
	}
	if _, ok, err := s.DeviceHealth(ctx); err != nil || !ok {
		t.Errorf("DeviceHealth: ok=%v err=%v", ok, err)
	}
}
