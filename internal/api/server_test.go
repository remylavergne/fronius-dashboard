package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/detect"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/secret"
	"fronius-supervisor/internal/storage"
)

type respEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Meta  json.RawMessage `json:"meta"`
	Error *apiError       `json:"error"`
}

type respMeta struct {
	GeneratedAt string   `json:"generated_at"`
	SampledAt   *string  `json:"sampled_at"`
	Stale       *bool    `json:"stale"`
	StaleAfterS *float64 `json:"stale_after_s"`
	StepS       *float64 `json:"step_s"`
}

func testConfig() config.Config {
	return config.Config{
		SolarAPIURL:   "http://127.0.0.1:8081",
		HTTPAddr:      "127.0.0.1:8080",
		DBPath:        "./data/fronius.db",
		TZ:            "Europe/Brussels",
		PollInterval:  5 * time.Second,
		PollJitter:    500 * time.Millisecond,
		HTTPTimeout:   5 * time.Second,
		RetentionDays: 0,
		LogLevel:      "error",
		LogFormat:     "text",
		StaleAfter:    30 * time.Second,
	}
}

func newTestServer(t *testing.T, tweak func(*config.Config)) (*Server, *httptest.Server, *storage.Store) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), loc)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := testConfig()
	if tweak != nil {
		tweak(&cfg)
	}
	srv := New(store, cfg, "test-version", slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return srv, httpSrv, store
}

func doRequest(t *testing.T, srv *httptest.Server, method, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, resp.Header, body
}

func decodeEnvelope(t *testing.T, body []byte) respEnvelope {
	t.Helper()
	var env respEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("Unmarshal(%s): %v", body, err)
	}
	return env
}

func decodeMeta(t *testing.T, env respEnvelope) respMeta {
	t.Helper()
	var m respMeta
	if err := json.Unmarshal(env.Meta, &m); err != nil {
		t.Fatalf("Unmarshal(meta): %v", err)
	}
	if m.GeneratedAt == "" {
		t.Fatal("meta.generated_at missing")
	}
	if _, err := time.Parse(apiTimeLayout, m.GeneratedAt); err != nil {
		t.Fatalf("meta.generated_at = %q : %v", m.GeneratedAt, err)
	}
	return m
}

func insertSample(t *testing.T, store *storage.Store, ts time.Time, pv float64, eDay float64) {
	t.Helper()
	sm := model.Sample{
		TS:            ts,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		Mode:          "produce-only",
		PowerPVW:      new(pv),
		EnergyDayWh:   new(eDay),
		EnergyYearWh:  new(float64(1000)),
		EnergyTotalWh: new(float64(2000)),
		Inverters:     []model.InverterSample{{ID: "1", DT: 1, PowerW: new(pv)}},
	}
	if err := store.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample: %v", err)
	}
}

func TestUnknownPathAndMethod(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/unknown")
	if code != http.StatusNotFound {
		t.Fatalf("unknown path: status = %d", code)
	}
	env := decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeNotFound {
		t.Fatalf("body = %s", body)
	}

	// A method not allowed on a known path.
	code, hdr, body := doRequest(t, srv, http.MethodPost, "/api/v1/health")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/health : status = %d", code)
	}
	if allow := hdr.Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q", allow)
	}
	env = decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeMethodNotAllowed {
		t.Fatalf("corps = %s", body)
	}
	// An error response carries no data field.
	if strings.Contains(string(body), `"data"`) {
		t.Fatalf("the error must not carry a data field: %s", body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestHealth(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	// No data, no cycle: nominal state.
	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var health healthDTO
	env := decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "ok" || !health.DB.OK || health.Version != "test-version" {
		t.Fatalf("health = %+v", health)
	}
	if health.Collector.SamplesTotal != 0 || health.Collector.LastError != "" {
		t.Fatalf("collector = %+v", health.Collector)
	}
	decodeMeta(t, env)

	// Samples + successful cycle.
	insertSample(t, store, time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), 100, 500)
	if err := store.SetState(ctx, "last_success_utc", "2026-10-16T07:00:00Z"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := store.InsertRun(ctx, storage.RunRecord{
		StartedUTC: time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), DurationMS: 42, OK: true, HTTPStatus: 200,
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("status = %q", health.Status)
	}
	if health.Collector.SamplesTotal != 1 || health.Collector.LastRunMS != 42 {
		t.Fatalf("collector = %+v", health.Collector)
	}
	if health.Collector.LastSuccessUTC != "2026-10-16T07:00:00Z" {
		t.Fatalf("last_success_utc = %q", health.Collector.LastSuccessUTC)
	}

	// A failing cycle degrades the state without failing the endpoint.
	if err := store.InsertRun(ctx, storage.RunRecord{
		StartedUTC: time.Date(2026, 10, 16, 7, 5, 0, 0, time.UTC), DurationMS: 5001, OK: false,
		HTTPStatus: 503, ErrorKind: "server", Error: "service unavailable",
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := store.SetState(ctx, "last_error", "service unavailable"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("status = %q, wanted degraded", health.Status)
	}
	if health.Collector.LastError == "" || health.Collector.LastRunMS != 5001 {
		t.Fatalf("collector = %+v", health.Collector)
	}
	if health.UptimeS < 0 {
		t.Fatalf("uptime_s = %d", health.UptimeS)
	}
	_ = srv
}

func TestSystemWithoutDeviceRow(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	env := decodeEnvelope(t, body)

	var sys systemDTO
	if err := json.Unmarshal(env.Data, &sys); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// No device row: "unknown" is not an empty object.
	if sys.Device != nil {
		t.Fatalf("device must be null without a successful read: %+v", sys.Device)
	}
	if sys.SolarAPIURL != "http://127.0.0.1:8081" || sys.TZ != "Europe/Brussels" ||
		sys.PollIntervalS != 5 || sys.StaleAfterS != 30 || sys.APIAddr != "127.0.0.1:8080" {
		t.Fatalf("system = %+v", sys)
	}
	if sys.GoVersion == "" || sys.Version != "test-version" {
		t.Fatalf("versions = %+v", sys)
	}
	decodeMeta(t, env)

	if sys.Detection == nil {
		t.Fatal("detection must always be present")
	}
	for name, res := range map[string]detectionResourceDTO{
		"meter": sys.Detection.Meter, "battery": sys.Detection.Battery,
		"internal api": sys.Detection.InternalAPI, "events": sys.Detection.DeviceEvents,
	} {
		if res.State != string(model.StateUnknown) {
			t.Fatalf("%s: state = %q, wanted unknown", name, res.State)
		}
		if res.Reason == "" {
			t.Fatalf("%s: an unknown state must carry a reason", name)
		}
	}
	if sys.Detection.ReadAtUTC != nil {
		t.Fatalf("read_at_utc must be null without a read: %v", *sys.Detection.ReadAtUTC)
	}
}

func TestSystemWithDeviceRow(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()
	readAt := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)

	record := storage.DeviceRecord{
		ReadAtUTC:          readAt,
		Source:             internalapi.VersionPath,
		Model:              "Fronius Primo GEN24 3.6 Plus",
		DeviceName:         "Primo GEN24 3.6 Plus",
		ArticleNumber:      "4,210,141,002",
		Firmware:           "1.24.3",
		SerialNumber:       "SIMULATED-0001",
		IdentityState:      model.StatePresent,
		MeterState:         model.StatePresent,
		MeterSource:        internalapi.PowerMeterPath,
		BatteryState:       model.StatePresent,
		BatterySource:      internalapi.BatteryPath,
		DeviceEventsState:  model.StateAbsent,
		DeviceEventsReason: "unreadable body",
	}
	if err := store.SaveDevice(ctx, record); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}
	if err := store.SetState(ctx, detect.StateLastError, "fake detect_last_error"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	env := decodeEnvelope(t, body)
	var sys systemDTO
	if err := json.Unmarshal(env.Data, &sys); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if sys.Device == nil {
		t.Fatal("device must be populated after a read")
	}
	if sys.Device.Model == nil || *sys.Device.Model != "Fronius Primo GEN24 3.6 Plus" {
		t.Fatalf("model = %v", sys.Device.Model)
	}
	if sys.Device.Firmware == nil || *sys.Device.Firmware != "1.24.3" {
		t.Fatalf("firmware = %v", sys.Device.Firmware)
	}
	if sys.Device.SerialNumber == nil || *sys.Device.SerialNumber != "SIMULATED-0001" {
		t.Fatalf("serial_number = %v", sys.Device.SerialNumber)
	}
	if sys.Device.ReadAtUTC != "2026-10-16T07:00:00.000Z" {
		t.Fatalf("read_at_utc = %q", sys.Device.ReadAtUTC)
	}
	if sys.Device.Source != internalapi.VersionPath {
		t.Fatalf("source = %q", sys.Device.Source)
	}

	d := sys.Detection
	if d == nil {
		t.Fatal("detection missing")
	}
	if d.Meter.State != "present" || d.Meter.Reason != "" {
		t.Fatalf("meter = %+v", d.Meter)
	}
	if d.Battery.State != "present" {
		t.Fatalf("battery = %+v", d.Battery)
	}
	// "absent" is not "unknown": the state and the reason differ.
	if d.DeviceEvents.State != "absent" || d.DeviceEvents.Reason != "unreadable body" {
		t.Fatalf("events = %+v", d.DeviceEvents)
	}
	if d.InternalAPI.State != "present" {
		t.Fatalf("internal api = %+v", d.InternalAPI)
	}
	if d.LastError != "fake detect_last_error" {
		t.Fatalf("last_error = %q", d.LastError)
	}
	if d.ReadAtUTC == nil || *d.ReadAtUTC != "2026-10-16T07:00:00.000Z" {
		t.Fatalf("detection.read_at_utc = %v", d.ReadAtUTC)
	}

	// The capabilities follow the same detection.
	_, _, capBody := doRequest(t, httpSrv, http.MethodGet, "/api/v1/capabilities")
	capEnv := decodeEnvelope(t, capBody)
	var caps []capabilityDTO
	if err := json.Unmarshal(capEnv.Data, &caps); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	byID := map[string]capabilityDTO{}
	for _, c := range caps {
		byID[c.ID] = c
	}
	for _, id := range []string{"grid.import_export", "load.consumption", "autoconsumption.ratio"} {
		if !byID[id].Available {
			t.Fatalf("%s must be available with a detected meter (reason %q)", id, byID[id].Reason)
		}
	}
	if byID["device.identity"].Available != true {
		t.Fatalf("device.identity must be available (reason %q)", byID["device.identity"].Reason)
	}
	if byID["device.events"].Available {
		t.Fatalf("device.events must not be available on an \"absent\" state (reason %q)", byID["device.events"].Reason)
	}
	battery := byID["battery.soc"]
	if battery.Available {
		t.Fatal("battery.soc must never be available in phase 2")
	}
	if battery.Reason != model.ReasonBatteryReadout {
		t.Fatalf("battery.soc: reason = %q", battery.Reason)
	}
	if byID["device.identity"].Reason != "" {
		t.Fatalf("device.identity: unexpected reserve %q", byID["device.identity"].Reason)
	}
}

// TestSystemDeviceEmptyStringsBecomeNull: an empty string in the database is not a
// value: it arrives as null, never as "" nor "unknown".
func TestSystemDeviceEmptyStringsBecomeNull(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)

	// Identity read but no identity field found in the response.
	if err := store.SaveDevice(context.Background(), storage.DeviceRecord{
		ReadAtUTC:         time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC),
		Source:            internalapi.VersionPath,
		IdentityState:     model.StatePresent,
		MeterState:        model.StateAbsent,
		BatteryState:      model.StateAbsent,
		DeviceEventsState: model.StateAbsent,
	}); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	env := decodeEnvelope(t, body)
	var sys systemDTO
	if err := json.Unmarshal(env.Data, &sys); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if sys.Device == nil {
		t.Fatal("device must be present")
	}
	if sys.Device.Model != nil || sys.Device.Firmware != nil || sys.Device.SerialNumber != nil ||
		sys.Device.DeviceName != nil || sys.Device.ArticleNumber != nil {
		t.Fatalf("empty fields must be null: %+v", sys.Device)
	}
	raw := string(body)
	for _, forbidden := range []string{`"model":""`, `"firmware":""`, `"serial_number":""`, `"device_name":""`, `"article_number":""`} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the response contains %s: %s", forbidden, raw)
		}
	}

	// `unknown` is a legitimate state of this response since the clock and the
	// service blocks exist: an unread clock is `unknown` with its cause, never a
	// fabricated time. The scan is therefore scoped to what this test is about —
	// the identity and the four presence detections — where an `unknown` would
	// still mean a state that was never read.
	var scan struct {
		Device    json.RawMessage            `json:"device"`
		Detection map[string]json.RawMessage `json:"detection"`
	}
	if err := json.Unmarshal(env.Data, &scan); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	scanned := map[string]json.RawMessage{"device": scan.Device}
	for _, name := range []string{"meter", "battery", "internal_api", "device_events"} {
		scanned[name] = scan.Detection[name]
	}
	for name, blob := range scanned {
		if strings.Contains(string(blob), "unknown") {
			t.Fatalf("%s contains unknown: %s", name, blob)
		}
	}
}

// TestNoRouteLeaksDeviceCredentials: no route must return the password
// value, even when the configuration carries one.
func TestNoRouteLeaksDeviceCredentials(t *testing.T) {
	const fake = "fake-password-42"

	_, httpSrv, store := newTestServer(t, func(c *config.Config) {
		c.DeviceUser = "installer"
		c.DevicePassword = secret.New(fake)
	})
	if err := store.SaveDevice(context.Background(), storage.DeviceRecord{
		ReadAtUTC:         time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC),
		Source:            internalapi.VersionPath,
		Model:             "Primo",
		IdentityState:     model.StatePresent,
		MeterState:        model.StatePresent,
		BatteryState:      model.StateAbsent,
		DeviceEventsState: model.StatePresent,
	}); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	routes := []string{
		"/api/v1/health",
		"/api/v1/system",
		"/api/v1/live",
		"/api/v1/timeseries?from=2026-10-14T00:00:00Z&to=2026-10-16T00:00:00Z",
		"/api/v1/energy/daily",
		"/api/v1/events",
		"/api/v1/capabilities",
	}
	if len(routes) != 7 {
		t.Fatalf("the test must cover the seven routes, %d listed", len(routes))
	}
	for _, route := range routes {
		code, _, body := doRequest(t, httpSrv, http.MethodGet, route)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d", route, code)
		}
		if strings.Contains(string(body), fake) {
			t.Fatalf("%s leaks the password: %s", route, body)
		}
	}
}

func TestLiveWithEmptyStoreReturnsNoData(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":null`) {
		t.Fatalf("data must be null: %s", body)
	}
	env := decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeNoData {
		t.Fatalf("error = %+v", env.Error)
	}
	decodeMeta(t, env)
}

func TestLiveAndStaleFlag(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)

	deviceTS := time.Date(2026, 10, 16, 7, 17, 43, 0, time.UTC)
	insertSample(t, store, deviceTS, 1902.4, 12345.6)

	// Clock frozen 1 s after the measurement: fresh data.
	srv.now = func() time.Time { return deviceTS.Add(time.Second) }

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var live sampleDTO
	if err := json.Unmarshal(env.Data, &live); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if live.PowerPVW == nil || *live.PowerPVW != 1902.4 {
		t.Fatalf("power_pv_w = %v", live.PowerPVW)
	}
	if live.EnergyDayWh == nil || *live.EnergyDayWh != 12345.6 {
		t.Fatalf("energy_day_wh = %v", live.EnergyDayWh)
	}
	if live.TS != "2026-10-16T07:17:43.000Z" {
		t.Fatalf("ts = %q", live.TS)
	}
	// Quantities that are not measured stay explicitly null.
	for name, v := range map[string]*float64{
		"power_grid_w":     live.PowerGridW,
		"power_load_w":     live.PowerLoadW,
		"power_battery_w":  live.PowerBatteryW,
		"rel_autonomy_pct": live.RelAutonomyPct,
	} {
		if v != nil {
			t.Fatalf("%s = %v, wanted null", name, *v)
		}
	}
	if len(live.Inverters) != 1 || live.Inverters[0].ID != "1" {
		t.Fatalf("inverters = %+v", live.Inverters)
	}

	m := decodeMeta(t, env)
	if m.Stale == nil || *m.Stale {
		t.Fatalf("stale = %v, wanted false", m.Stale)
	}
	if m.SampledAt == nil || *m.SampledAt != "2026-10-16T07:17:43.000Z" {
		t.Fatalf("sampled_at = %v", m.SampledAt)
	}
	if m.StaleAfterS == nil || *m.StaleAfterS != 30 {
		t.Fatalf("stale_after_s = %v", m.StaleAfterS)
	}

	// Same database, clock 60 s later (threshold configured at 30 s).
	srv.now = func() time.Time { return deviceTS.Add(time.Minute) }
	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	env = decodeEnvelope(t, body)
	m = decodeMeta(t, env)
	if m.Stale == nil || !*m.Stale {
		t.Fatalf("stale = %v, wanted true", m.Stale)
	}
}

func TestTimeSeriesValidation(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)
	base := "2026-10-14T00:00:00Z"
	end := "2026-10-16T00:00:00Z"

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
	}{
		{"from missing", "?to=" + end, http.StatusBadRequest, CodeInvalidRange},
		{"to missing", "?from=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"from unreadable", "?from=yesterday&to=" + end, http.StatusBadRequest, CodeInvalidRange},
		{"inverted range", "?from=" + end + "&to=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"zero range", "?from=" + base + "&to=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"range too wide", "?from=2020-01-01T00:00:00Z&to=2026-01-01T00:00:00Z", http.StatusBadRequest, CodeInvalidRange},
		{"unknown step", "?from=" + base + "&to=" + end + "&step=17s", http.StatusBadRequest, CodeInvalidStep},
		{"explicit empty step", "?from=" + base + "&to=" + end + "&step=0s", http.StatusBadRequest, CodeInvalidStep},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/timeseries"+tc.query)
			if code != tc.wantStatus {
				t.Fatalf("status = %d, body %s", code, body)
			}
			env := decodeEnvelope(t, body)
			if env.Error == nil || env.Error.Code != tc.wantCode {
				t.Fatalf("error = %+v, body %s", env.Error, body)
			}
		})
	}
}

func TestTimeSeriesReturnsBucketsAndEmptyRange(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)

	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for i := range 6 {
		insertSample(t, store, base.Add(time.Duration(i)*10*time.Minute), float64(100*(i+1)), float64(i))
	}

	from := base.Format(time.RFC3339)
	to := base.Add(time.Hour).Format(time.RFC3339)
	code, _, body := doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/timeseries?from="+from+"&to="+to+"&step=1h")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env := decodeEnvelope(t, body)
	var buckets []bucketDTO
	if err := json.Unmarshal(env.Data, &buckets); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("buckets = %d, wanted 1", len(buckets))
	}
	b := buckets[0]
	if b.Count != 6 || b.Missing != 0 {
		t.Fatalf("bucket = %+v", b)
	}
	if b.PowerPVAvgW == nil || *b.PowerPVAvgW != 350 {
		t.Fatalf("power_pv_avg_w = %v, wanted 350", b.PowerPVAvgW)
	}
	if b.PowerPVMinW == nil || *b.PowerPVMinW != 100 || b.PowerPVMaxW == nil || *b.PowerPVMaxW != 600 {
		t.Fatalf("min/max = %v / %v", b.PowerPVMinW, b.PowerPVMaxW)
	}
	if b.Start != "2026-10-14T10:00:00.000Z" {
		t.Fatalf("start = %q", b.Start)
	}
	decodeMeta(t, env)

	// Range without data: empty array, status 200, never null.
	emptyFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	emptyTo := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/timeseries?from="+emptyFrom+"&to="+emptyTo+"&step=1h")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":[]`) {
		t.Fatalf("an empty range must produce an empty array: %s", body)
	}
}

// TestTimeSeriesExposesResolvedStep: the client cannot reliably deduce the step
// (a missing bucket skews the gap between two points), so the
// server must give it to it.
func TestTimeSeriesExposesResolvedStep(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	insertSample(t, store, base, 100, 1)

	cases := []struct {
		step string
		want float64
	}{
		{"1h", 3600},
		{"5m", 300},
		{"auto", 60}, // 1 minute over a one-hour window
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			code, _, body := doRequest(t, httpSrv, http.MethodGet,
				"/api/v1/timeseries?from="+base.Format(time.RFC3339)+"&to="+base.Add(time.Hour).Format(time.RFC3339)+"&step="+tc.step)
			if code != http.StatusOK {
				t.Fatalf("status = %d (%s)", code, body)
			}
			env := decodeEnvelope(t, body)
			m := decodeMeta(t, env)
			if m.StepS == nil {
				t.Fatal("meta.step_s missing")
			}
			if *m.StepS != tc.want {
				t.Fatalf("step_s = %v, wanted %v", *m.StepS, tc.want)
			}
		})
	}
}

func TestParseStepAutoBounded(t *testing.T) {
	if got, err := parseStep("17s", time.Hour); err == nil || got != 0 {
		t.Fatalf("parseStep(17s) = (%v, %v)", got, err)
	}

	cases := []struct {
		span time.Duration
		want time.Duration
	}{
		{time.Hour, time.Minute},
		{24 * time.Hour, time.Minute},
		{2 * 24 * time.Hour, 5 * time.Minute},
		{7 * 24 * time.Hour, 15 * time.Minute},
		{30 * 24 * time.Hour, time.Hour},
		{366 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, tc := range cases {
		got, err := parseStep("auto", tc.span)
		if err != nil {
			t.Fatalf("parseStep(auto, %v): %v", tc.span, err)
		}
		if got != tc.want {
			t.Fatalf("span %v: step = %v, wanted %v", tc.span, got, tc.want)
		}
		if buckets := tc.span / got; buckets > autoMaxBuckets {
			t.Fatalf("span %v: %d buckets exceeds the bound %d", tc.span, buckets, autoMaxBuckets)
		}
	}

	// Each named step is accepted and resolved to its duration.
	for _, opt := range stepOptions {
		got, err := parseStep(opt.name, time.Hour)
		if err != nil || got != opt.d {
			t.Fatalf("parseStep(%q) = (%v, %v)", opt.name, got, err)
		}
	}
}

func TestDailyEnergyEndpoint(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)

	// 14 and 15 October local, with a meter reset between the two.
	insertSample(t, store, time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC), 500, 20000) // 14/10 01:00 local
	insertSample(t, store, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), 900, 20000) // 14/10 14:00 local
	insertSample(t, store, time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC), 0, 0)      // 15/10 00:30 local
	insertSample(t, store, time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC), 10, 1000)  // 15/10 01:30 local

	srv.now = func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/energy/daily")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env := decodeEnvelope(t, body)
	var daily []dailyEnergyDTO
	if err := json.Unmarshal(env.Data, &daily); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// The payload now holds one entry per local day of the range — the spine the
	// aggregates view reads — with the device's own counter where it reported one
	// and the derived value (or its cause) everywhere else.
	byDate := map[string]dailyEnergyDTO{}
	for _, d := range daily {
		byDate[d.Date] = d
	}
	fourteen, ok := byDate["2026-10-14"]
	if !ok {
		t.Fatalf("14/10 is missing from the payload: %+v", daily)
	}
	if fourteen.EnergyWh == nil || *fourteen.EnergyWh != 20000 {
		t.Fatalf("14/10 measured = %v", fourteen.EnergyWh)
	}
	// No sample precedes that day's start, so nothing can be derived: the entry
	// says why instead of showing a number.
	if fourteen.DerivedWh != nil || fourteen.DerivedReason == "" {
		t.Fatalf("14/10 derived = %+v", fourteen)
	}
	fifteen, ok := byDate["2026-10-15"]
	if !ok {
		t.Fatalf("15/10 is missing from the payload: %+v", daily)
	}
	if fifteen.EnergyWh == nil || *fifteen.EnergyWh != 1000 {
		t.Fatalf("15/10 measured = %v", fifteen.EnergyWh)
	}
	// The closest sample before that day's start is ten hours old: refused, with
	// the age in the cause.
	if fifteen.DerivedWh != nil || !strings.Contains(fifteen.DerivedReason, "before the day's start") {
		t.Fatalf("15/10 derived = %+v", fifteen)
	}
	decodeMeta(t, env)

	// Explicit bounds: the window applies to ts_utc, while the
	// grouping is done by local date. A local day of which only the
	// end (local hour) falls in the window therefore appears with a partial
	// maximum — this is the documented behaviour of the endpoint.
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/energy/daily?from=2026-10-14T00:00:00Z&to=2026-10-15T00:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &daily); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	bounded := map[string]dailyEnergyDTO{}
	for _, d := range daily {
		bounded[d.Date] = d
	}
	if len(bounded) != 2 {
		t.Fatalf("a bounded window must publish the two local days it covers: %+v", daily)
	}
	if d := bounded["2026-10-14"]; d.EnergyWh == nil || *d.EnergyWh != 20000 {
		t.Fatalf("14/10 = %+v", d)
	}
	if d := bounded["2026-10-15"]; d.EnergyWh == nil || *d.EnergyWh != 1000 {
		t.Fatalf("15/10 = %+v", d)
	}

	// Window covering only 13 October UTC: no local day.
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/energy/daily?from=2026-10-12T00:00:00Z&to=2026-10-13T12:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":[]`) {
		t.Fatalf("a range without data must produce []: %s", body)
	}

	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/energy/daily?from=not-a-date")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
	env = decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeInvalidRange {
		t.Fatalf("error = %+v", env.Error)
	}
}

func TestEventsEndpoint(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	for i := range 3 {
		if err := store.InsertEvent(ctx, model.Event{
			TS:       time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
			Kind:     model.EventCollectorError,
			Severity: model.SeverityWarning,
			Message:  "collection failure",
			Detail:   "fronius: server | HTTP 503",
		}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/events")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var events []eventDTO
	if err := json.Unmarshal(env.Data, &events); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d", len(events))
	}
	if events[0].TS != "2026-10-16T08:02:00.000Z" || events[0].Severity != "warning" {
		t.Fatalf("first event = %+v", events[0])
	}

	// Limit applied.
	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/events?limit=2")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &events); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("limit ignored: %d", len(events))
	}

	for _, bad := range []string{"0", "-1", "501", "many"} {
		code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/events?limit="+bad)
		if code != http.StatusBadRequest {
			t.Fatalf("limit=%s: status = %d", bad, code)
		}
		env := decodeEnvelope(t, body)
		if env.Error == nil || env.Error.Code != CodeInvalidRange {
			t.Fatalf("limit=%s: error = %+v", bad, env.Error)
		}
	}
}

func TestCapabilitiesEndpoint(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/capabilities")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var caps []capabilityDTO
	if err := json.Unmarshal(env.Data, &caps); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var unavailable int
	for _, c := range caps {
		if c.Available {
			continue
		}
		unavailable++
		if c.Reason == "" {
			t.Fatalf("%s: reason missing", c.ID)
		}
		if c.Quality != string(model.QualityMissing) {
			t.Fatalf("%s: quality %q", c.ID, c.Quality)
		}
	}
	// With no successful identity read, the six capabilities tied to the
	// hardware or to the internal paths are unavailable: grid.import_export,
	// load.consumption, autoconsumption.ratio, battery.soc, device.identity,
	// device.events. The other four are available.
	if unavailable != 6 {
		t.Fatalf("unavailable capabilities = %d, wanted 6", unavailable)
	}
	if len(caps) != 10 {
		t.Fatalf("capabilities = %d, wanted 10", len(caps))
	}

	// Empty arrays are serialised as [] and not null.
	if strings.Contains(string(body), `"requires":null`) || strings.Contains(string(body), `"evidence":null`) {
		t.Fatalf("an array must be serialised as []: %s", body)
	}
	// No invented null numeric value in the full response.
	for _, needle := range []string{`":0,`, `":0}`, `":0.0,`, `":0.0}`} {
		if strings.Contains(string(body), needle) {
			t.Fatalf("null numeric value detected (%s): %s", needle, body)
		}
	}
	decodeMeta(t, env)
}

func TestStaticWebSPAFallback(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, httpSrv, _ := newTestServer(t, func(c *config.Config) { c.WebDir = webDir })

	// Root → index.html.
	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/")
	if code != http.StatusOK || !strings.Contains(string(body), "app") {
		t.Fatalf("GET / = %d %s", code, body)
	}
	// Real file.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/app.js")
	if code != http.StatusOK || !strings.Contains(string(body), "console.log") {
		t.Fatalf("GET /app.js = %d %s", code, body)
	}
	// Unknown SPA route → index.html.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/capabilities")
	if code != http.StatusOK || !strings.Contains(string(body), "app") {
		t.Fatalf("GET /capabilities = %d %s", code, body)
	}
	// The API keeps control.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/unknown")
	if code != http.StatusNotFound || !strings.Contains(string(body), CodeNotFound) {
		t.Fatalf("GET /api/v1/unknown = %d %s", code, body)
	}
}

func TestNoStaticWebWhenUnset(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)
	code, _, _ := doRequest(t, httpSrv, http.MethodGet, "/")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, wanted 404 without FRONIUS_WEB_DIR", code)
	}
}

// TestResponsesUnchangedAfterRollup checks at the API level that the switchover
// to the hourly aggregates (phase 3) does not change the payload: the data
// body must be identical before and after the aggregation, whereas the
// first capture goes through the raw measurements and the second through agg_hour.
func TestResponsesUnchangedAfterRollup(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	base := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)

	// Values exactly representable in binary and exact sums: a
	// divergence can then only come from a bucket served differently, not
	// from a float reassociation.
	for i := range 48 { // 12 hours at 15 minutes
		insertSample(t, store, base.Add(time.Duration(i)*15*time.Minute), float64(i)*100, float64(i)*10)
	}

	paths := []string{
		"/api/v1/timeseries?step=1h&from=2026-10-15T22:00:00Z&to=2026-10-16T10:00:00Z",
		"/api/v1/timeseries?step=1d&from=2026-10-15T00:00:00Z&to=2026-10-16T10:00:00Z",
		"/api/v1/energy/daily?from=2026-10-14T00:00:00Z&to=2026-10-16T10:00:00Z",
	}
	capture := func() []string {
		out := make([]string, 0, len(paths))
		for _, p := range paths {
			code, _, body := doRequest(t, httpSrv, http.MethodGet, p)
			if code != http.StatusOK {
				t.Fatalf("%s : status = %d", p, code)
			}
			out = append(out, string(decodeEnvelope(t, body).Data))
		}
		return out
	}

	before := capture()
	if !strings.Contains(before[0], "power_pv_avg_w") || !strings.Contains(before[2], "energy_wh") {
		t.Fatalf("empty responses before aggregation: %v", before)
	}

	res, err := store.Rollup(context.Background(), time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.Buckets == 0 {
		t.Fatal("no aggregate written: the second capture would test nothing")
	}
	var aggRows int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM agg_hour`).Scan(&aggRows); err != nil {
		t.Fatalf("counting the aggregates: %v", err)
	}
	if aggRows < 12 {
		t.Fatalf("%d aggregated hours, wanted at least 12", aggRows)
	}
	after := capture()
	for i, p := range paths {
		if before[i] != after[i] {
			t.Fatalf("%s: payload changed by the aggregation\nbefore: %s\nafter: %s",
				p, before[i], after[i])
		}
	}
}

// TestDailyEnergyCarriesTheDerivedValueWhenTheDeviceReportsNone is the reference
// installation's case: `E_Day` is null on every endpoint, so the day's energy can
// only come from our own stored total. Without it, the aggregates view would be
// empty on an installation that is producing.
func TestDailyEnergyCarriesTheDerivedValueWhenTheDeviceReportsNone(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)

	// A day with a reading just before its local midnight and one at its end,
	// and a total that rose by 2 kWh in between. 2026-10-16 is CEST (UTC+2).
	// `E_Day` is null on every sample, as this firmware answers it.
	insert := func(ts time.Time, pv, total float64) {
		sm := model.Sample{
			TS: ts, TSSource: model.TSSourceDevice, Source: model.SourceSolarAPI,
			Quality: model.QualityMeasured, Mode: "produce-only",
			PowerPVW: new(pv), EnergyDayWh: nil, EnergyTotalWh: new(total),
		}
		if err := store.InsertSample(context.Background(), sm); err != nil {
			t.Fatalf("InsertSample: %v", err)
		}
	}
	insert(time.Date(2026, 10, 15, 21, 59, 0, 0, time.UTC), 0, 1_000_000)
	insert(time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC), 800, 1_001_000)
	insert(time.Date(2026, 10, 16, 21, 59, 0, 0, time.UTC), 0, 1_002_000)

	srv.now = func() time.Time { return time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC) }

	code, _, body := doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/energy/daily?from=2026-10-15T22:00:00Z&to=2026-10-16T22:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	var daily []dailyEnergyDTO
	if err := json.Unmarshal(decodeEnvelope(t, body).Data, &daily); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(daily) != 1 || daily[0].Date != "2026-10-16" {
		t.Fatalf("daily = %+v", daily)
	}
	// The device reported no counter, the derivation did.
	if daily[0].EnergyWh != nil {
		t.Errorf("energy_wh = %v, want null: this firmware never reports E_Day", daily[0].EnergyWh)
	}
	if daily[0].DerivedWh == nil || *daily[0].DerivedWh != 2000 {
		t.Errorf("derived_wh = %v, want 2000", daily[0].DerivedWh)
	}
	if daily[0].DerivedReason != "" {
		t.Errorf("a derived day carries no cause: %+v", daily[0])
	}
	if daily[0].Partial {
		t.Errorf("a finished day is not partial: %+v", daily[0])
	}
}
