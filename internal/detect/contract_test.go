// Contract test on the hardware captures.
//
// The test/fixtures/internalapi fixtures are redacted captures of firmware
// 1.19.7-1 taken with cmd/fronius-probe (see docs/PROBE-READONLY.md and
// docs/SOURCES.md). They freeze the shapes the device really sends:
//
//   - /status/version: a raw object (no envelope), firmware under swrevisions.GEN24,
//     and NO model key at all;
//   - /status/activeEvents: a ROOT ARRAY, severity as an integer, identity as a uuid;
//   - /components/*: the Cgi envelope, empty Body.Data when the component is
//     absent, answered with Content-Type text/javascript.
//
// The test drives the real client and the real detector against those bodies, so
// a shape drift breaks here before it breaks the daemon. Nothing here needs
// credentials: the capture proves the device answers all of these paths without
// a Digest challenge.
package detect

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
)

// capturedFixture serves one redacted capture, reproducing the Content-Type the
// device really uses (text/javascript for the component paths).
func capturedFixture(t *testing.T, contentType string) *httptest.Server {
	t.Helper()

	files := map[string]string{
		internalapi.VersionPath:      "status_version.json",
		internalapi.ActiveEventsPath: "status_activeevents.json",
		internalapi.PowerMeterPath:   "components_powermeter_readable.json",
		internalapi.BatteryPath:      "components_batterymanagementsystem_readable.json",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "internalapi", name))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			http.Error(w, "fixture", http.StatusInternalServerError)
			return
		}
		ct := "application/json"
		if r.URL.Path == internalapi.PowerMeterPath || r.URL.Path == internalapi.BatteryPath {
			ct = contentType
		}
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func capturedClient(t *testing.T, srv *httptest.Server) *internalapi.Client {
	t.Helper()
	c, err := internalapi.New(srv.URL, fronius.ReadOnly{Inner: &http.Client{}}, 2*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("internalapi.New: %v", err)
	}
	return c
}

// TestCapturedDeviceContract replays the captures through detection.
func TestCapturedDeviceContract(t *testing.T) {
	srv := capturedFixture(t, "text/javascript")
	sink := newFakeSink()
	d := newDetector(t, capturedClient(t, srv), sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	// Identity: the capture has no model key, so model stays empty; firmware comes
	// from swrevisions.GEN24, the key the vendor interface displays.
	if got.Identity.State != model.StatePresent {
		t.Fatalf("identity = %+v", got.Identity)
	}
	// The reference installation has neither a Smart Meter nor a battery: the
	// device answers with an empty Body.Data, which is an ABSENCE, not a doubt.
	if got.Meter.State != model.StateAbsent {
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
	if rec.Firmware != "1.19.7-1" {
		t.Fatalf("firmware = %q, want the captured swrevisions.GEN24", rec.Firmware)
	}
	if rec.Model != "" {
		t.Fatalf("model = %q: the capture has no model key, so it must stay empty", rec.Model)
	}
	if rec.SerialNumber != "<redacted>" {
		t.Fatalf("serial = %q, want the redaction marker of the fixture", rec.SerialNumber)
	}

	// The captured event: integer severity 3 -> info, uuid identity, state-code
	// label as text.
	if len(events) != 1 {
		t.Fatalf("inserted events = %+v", events)
	}
	ev := events[0]
	if ev.Kind != model.EventDevice {
		t.Fatalf("kind = %q", ev.Kind)
	}
	if ev.Severity != model.SeverityInfo {
		t.Fatalf("severity = %q, want info for the captured integer 3", ev.Severity)
	}
	if ev.Message != "LicensedFeatureDisabled" {
		t.Fatalf("message = %q, want the captured label", ev.Message)
	}
	if !strings.Contains(ev.Detail, "56b077da-0d11-45be-8b24-7b09d53ad558") {
		t.Fatalf("the detail must keep the raw event: %q", ev.Detail)
	}
	if state[StateLastError] != "" {
		t.Fatalf("detect_last_error = %q", state[StateLastError])
	}

	// The capture does not change between cycles: the uuid keys the event, so a
	// second detection inserts nothing.
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("second DetectOnce: %v", err)
	}
	if _, again, _ := sink.snapshot(); len(again) != 1 {
		t.Fatalf("inserted events after a second cycle = %d, want 1", len(again))
	}
}
