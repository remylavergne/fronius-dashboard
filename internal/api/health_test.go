// Tests of the clock and service blocks of /api/v1/system.
//
// The contract the interface consumes: `clock` and `service` always exist, with
// state, source and reason; `device_utc`, `drift_s` and `value` are ABSENT —
// not 0, not a 1970 timestamp, not an empty string — when nothing was read. An
// unread clock has no drift, and the test asserts the absence of the key, not
// its zero value.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fronius-supervisor/internal/detect"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// systemDetection fetches /api/v1/system and returns the raw detection block,
// so that an absent key can be told apart from a zero value.
func systemDetection(t *testing.T, srv *httptest.Server) map[string]map[string]any {
	t.Helper()
	status, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/system")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var payload struct {
		Detection map[string]json.RawMessage `json:"detection"`
	}
	if err := json.Unmarshal(decodeEnvelope(t, body).Data, &payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if payload.Detection == nil {
		t.Fatal("detection must be published")
	}

	// Only the object members: `read_at_utc` is a timestamp and `last_error` a
	// string, and they carry no key whose absence matters here.
	blocks := map[string]map[string]any{}
	for name, raw := range payload.Detection {
		if name == "read_at_utc" || name == "last_error" {
			continue
		}
		var block map[string]any
		if err := json.Unmarshal(raw, &block); err != nil {
			t.Fatalf("decoding detection.%s: %v", name, err)
		}
		blocks[name] = block
	}
	return blocks
}

func TestSystemWithoutAnyHealthRead(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	detection := systemDetection(t, httpSrv)

	for _, name := range []string{"clock", "service"} {
		block, ok := detection[name]
		if !ok {
			t.Fatalf("%s must always be published", name)
		}
		if block["state"] != string(model.StateUnknown) {
			t.Fatalf("%s.state = %v, want unknown", name, block["state"])
		}
		if block["reason"] != detect.ReasonNoHealthRead {
			t.Fatalf("%s.reason = %v", name, block["reason"])
		}
		if block["source"] == "" {
			t.Fatalf("%s.source must name the path", name)
		}
		for _, key := range []string{"device_utc", "drift_s", "value"} {
			if _, present := block[key]; present {
				t.Fatalf("%s.%s = %v, want the key to be absent", name, key, block[key])
			}
		}
	}

	if got := detection["clock"]["source"]; got != internalapi.UTCTimePath {
		t.Fatalf("clock.source = %v, want %q", got, internalapi.UTCTimePath)
	}
	if got := detection["service"]["source"]; got != internalapi.ServiceStatePath {
		t.Fatalf("service.source = %v, want %q", got, internalapi.ServiceStatePath)
	}
}

func TestSystemPublishesClockReading(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	readAt := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	deviceUTC := readAt.Add(42 * time.Second)
	err := store.SaveDeviceHealth(ctx, storage.DeviceHealth{
		ReadAtUTC:      readAt,
		ClockState:     model.StatePresent,
		ClockDeviceUTC: deviceUTC,
		ClockDriftMS:   (42 * time.Second).Milliseconds(),
		ServiceState:   model.StatePresent,
		ServiceValue:   "IDLE",
	})
	if err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	detection := systemDetection(t, httpSrv)

	clock := detection["clock"]
	if clock["state"] != string(model.StatePresent) || clock["reason"] != "" {
		t.Fatalf("clock = %v", clock)
	}
	if clock["source"] != internalapi.UTCTimePath {
		t.Fatalf("clock.source = %v", clock["source"])
	}
	if got := clock["device_utc"]; got != "2026-10-16T07:00:42.000Z" {
		t.Fatalf("clock.device_utc = %v, want the device time", got)
	}
	if got, ok := clock["drift_s"].(float64); !ok || got != 42 {
		t.Fatalf("clock.drift_s = %v, want 42", clock["drift_s"])
	}

	service := detection["service"]
	if service["state"] != string(model.StatePresent) || service["reason"] != "" {
		t.Fatalf("service = %v", service)
	}
	if service["source"] != internalapi.ServiceStatePath {
		t.Fatalf("service.source = %v", service["source"])
	}
	if got := service["value"]; got != "IDLE" {
		t.Fatalf("service.value = %v, want IDLE", got)
	}
}

// TestSystemPublishesNegativeDriftWithItsSign: a device behind the host is not a
// device on time.
func TestSystemPublishesNegativeDriftWithItsSign(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	readAt := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	err := store.SaveDeviceHealth(ctx, storage.DeviceHealth{
		ReadAtUTC:      readAt,
		ClockState:     model.StatePresent,
		ClockDeviceUTC: readAt.Add(-61500 * time.Millisecond),
		ClockDriftMS:   -61500,
		ServiceState:   model.StatePresent,
		ServiceValue:   "IDLE",
	})
	if err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	clock := systemDetection(t, httpSrv)["clock"]
	if got := clock["device_utc"]; got != "2026-10-16T06:58:58.500Z" {
		t.Fatalf("clock.device_utc = %v", got)
	}
	got, ok := clock["drift_s"].(float64)
	if !ok || got != -61.5 {
		t.Fatalf("clock.drift_s = %v, want -61.5", clock["drift_s"])
	}
}

// TestSystemUnreadClockOmitsDeviceTimeAndDrift: the reading exists, it failed.
// The state and the cause are published; the device time, the drift and the
// value are not — the previous reading is NOT kept (the detector overwrites its
// single row).
func TestSystemUnreadClockOmitsDeviceTimeAndDrift(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	readAt := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	err := store.SaveDeviceHealth(ctx, storage.DeviceHealth{
		ReadAtUTC:     readAt,
		ClockState:    model.StateUnknown,
		ClockReason:   "unreadable body",
		ServiceState:  model.StateUnknown,
		ServiceReason: detect.ReasonEndpointAbsent,
	})
	if err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	detection := systemDetection(t, httpSrv)

	clock := detection["clock"]
	if clock["state"] != string(model.StateUnknown) || clock["reason"] != "unreadable body" {
		t.Fatalf("clock = %v", clock)
	}
	for _, key := range []string{"device_utc", "drift_s"} {
		if v, present := clock[key]; present {
			t.Fatalf("clock.%s = %v, want the key to be absent", key, v)
		}
	}

	service := detection["service"]
	if service["state"] != string(model.StateUnknown) || service["reason"] != detect.ReasonEndpointAbsent {
		t.Fatalf("service = %v", service)
	}
	if v, present := service["value"]; present {
		t.Fatalf("service.value = %v, want the key to be absent", v)
	}
}

// TestSystemMeasuredZeroDriftIsPublished: a drift of 0 s that was measured must
// stay distinguishable from a clock that was not read.
func TestSystemMeasuredZeroDriftIsPublished(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	readAt := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	err := store.SaveDeviceHealth(ctx, storage.DeviceHealth{
		ReadAtUTC:      readAt,
		ClockState:     model.StatePresent,
		ClockDeviceUTC: readAt,
		ClockDriftMS:   0,
		ServiceState:   model.StatePresent,
		ServiceValue:   "IDLE",
	})
	if err != nil {
		t.Fatalf("SaveDeviceHealth: %v", err)
	}

	clock := systemDetection(t, httpSrv)["clock"]
	got, ok := clock["drift_s"].(float64)
	if !ok || got != 0 {
		t.Fatalf("clock.drift_s = %v, want a measured 0", clock["drift_s"])
	}
	if got := clock["device_utc"]; got != "2026-10-16T07:00:00.000Z" {
		t.Fatalf("clock.device_utc = %v", got)
	}
}
