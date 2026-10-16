// Command fronius-sim serves a local imitation of the Fronius Solar API v1.
//
// Usage:
//
//	go run ./cmd/fronius-sim [flags]
//
// The simulator sends no outgoing request and writes nothing to disk.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
)

const powerFlowPath = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"

// faultModes lists the injectable faults. The last six concern only
// the internal paths (Digest authentication); the first nine
// apply to all endpoints.
var faultModes = []string{
	"", "500", "503", "401", "404", "timeout", "invalid_json", "empty", "status7",
	faultNoChallenge, faultStandardOnly, faultNoQop, faultOpaque, faultStaleNonce, faultBadDigest,
}

type options struct {
	addr      string
	seed      uint64
	nominalW  float64
	fault     string
	withMeter bool
	tz        string
	logLevel  string

	// Internal paths (Digest authentication).
	apiUser               string
	apiPassword           string
	withBattery           bool
	deviceEvents          int
	emitStandardChallenge bool
	challengeRealm        string
}

type simulator struct {
	gen          *generator
	opt          options
	log          *slog.Logger
	requests     atomic.Int64
	faultTimeout time.Duration // duration of the fake timeout, overridable in tests
	auth         *digestAuth
}

func main() {
	var opt options
	flag.StringVar(&opt.addr, "addr", "127.0.0.1:8081", "listen address")
	flag.Uint64Var(&opt.seed, "seed", 42, "seed of the synthetic curve")
	flag.Float64Var(&opt.nominalW, "nominal-w", 3680, "peak power of the PV array, in W")
	fault := flag.String("fault", "", "global fault to inject ("+strings.Join(faultModes[1:], ", ")+")")
	flag.BoolVar(&opt.withMeter, "with-meter", false, "simulates a Fronius Smart Meter (P_Grid, P_Load, rel_* filled in; P_Akku stays null)")
	flag.StringVar(&opt.tz, "tz", "Europe/Brussels", "local time zone of the installation")
	flag.StringVar(&opt.logLevel, "log-level", "info", "log level (debug, info, warn, error)")
	flag.StringVar(&opt.apiUser, "api-user", "", "user of the internal paths; empty = no authentication required")
	flag.StringVar(&opt.apiPassword, "api-password", "", "Digest password of the internal paths")
	flag.BoolVar(&opt.withBattery, "with-battery", false, "simulates a battery (/components/BatteryManagementSystem/readable)")
	flag.IntVar(&opt.deviceEvents, "device-events", 1, "number of active events simulated in /status/activeEvents")
	flag.BoolVar(&opt.emitStandardChallenge, "emit-standard-challenge", false, "also emits the Digest challenge in the standard www-authenticate header (for curl --digest)")
	flag.StringVar(&opt.challengeRealm, "challenge-realm", "Webinterface area", "realm of the Digest challenge")
	flag.Parse()
	opt.fault = *fault

	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, "fronius-sim:", err)
		os.Exit(1)
	}
}

func run(opt options) error {
	if !slices.Contains(faultModes, opt.fault) {
		return fmt.Errorf("unknown fault %q (values: %s)", opt.fault, strings.Join(nonEmpty(faultModes), ", "))
	}
	if opt.nominalW < 0 {
		return fmt.Errorf("--nominal-w must be positive, got %v", opt.nominalW)
	}
	if opt.deviceEvents < 0 {
		return fmt.Errorf("--device-events must be positive or zero, got %d", opt.deviceEvents)
	}
	if strings.TrimSpace(opt.challengeRealm) == "" {
		return fmt.Errorf("--challenge-realm cannot be empty")
	}
	if opt.apiUser == "" && opt.apiPassword != "" {
		return fmt.Errorf("--api-password is set without --api-user: both go together")
	}

	loc, err := time.LoadLocation(opt.tz)
	if err != nil {
		return fmt.Errorf("time zone %q: %w", opt.tz, err)
	}

	level, err := parseLevel(opt.logLevel)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	sim := newSimulator(opt, loc, logger)

	srv := &http.Server{
		Addr:              opt.addr,
		Handler:           sim.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("fronius-sim started",
		"addr", opt.addr, "seed", opt.seed, "nominal_w", opt.nominalW,
		"tz", opt.tz, "fault", opt.fault, "with_meter", opt.withMeter,
		"with_battery", opt.withBattery, "device_events", opt.deviceEvents,
		"auth_required", opt.apiUser != "", "challenge_realm", opt.challengeRealm)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func newSimulator(opt options, loc *time.Location, log *slog.Logger) *simulator {
	return &simulator{
		gen:          newGenerator(opt.seed, opt.nominalW, loc),
		opt:          opt,
		log:          log,
		faultTimeout: 30 * time.Second,
		auth:         newDigestAuth(),
	}
}

// Handler returns the simulator's router.
func (s *simulator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(powerFlowPath, s.logged(s.handlePowerFlow))
	mux.HandleFunc(solarapi.InverterRealtimePath, s.logged(s.handleInverterRealtime))
	mux.HandleFunc(solarapi.InverterInfoPath, s.logged(s.handleInverterInfo))
	mux.HandleFunc(solarapi.ActiveDeviceInfoPath, s.logged(s.handleActiveDeviceInfo))
	mux.HandleFunc(solarapi.APIVersionPath, s.logged(s.handleAPIVersion))
	mux.HandleFunc("/sim/health", s.logged(s.handleHealth))
	s.registerInternal(mux)
	return mux
}

// genericFault applies the faults common to all endpoints and returns
// true when the request has been handled.
//
// "status7" is excluded from it: this fault modifies the Solar API envelope,
// it makes no sense on an internal endpoint.
func (s *simulator) genericFault(w http.ResponseWriter, r *http.Request, fault string) bool {
	switch fault {
	case "timeout":
		// Sleeps then closes without a response.
		select {
		case <-time.After(s.faultTimeout):
		case <-r.Context().Done():
		}
		return true
	case "401":
		w.Header().Set(nonStandardChallengeHeader,
			`Digest realm="Webinterface area", nonce="sim-nonce-1", qop="auth"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return true
	case "404":
		http.NotFound(w, r)
		return true
	case "500":
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return true
	case "503":
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return true
	case "empty":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	case "invalid_json":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Truncated body: JSON decoding impossible on the client side.
		_, _ = w.Write([]byte(`{"Body":{"Data":{"Site":{"P_PV":`))
		return true
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (s *simulator) logged(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next(sw, r)
		s.log.Info("request",
			"method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
			"status", sw.status, "duration_ms", time.Since(start).Milliseconds())
	}
}

func (s *simulator) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"seed":     s.opt.seed,
		"requests": s.requests.Load(),
		"tz":       s.opt.tz,
	})
}

func (s *simulator) handlePowerFlow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.requests.Add(1)

	if s.genericFault(w, r, s.faultFor(r)) {
		return
	}

	now := time.Now()
	if at := r.URL.Query().Get("at"); at != "" {
		parsed, err := time.Parse(time.RFC3339, at)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid 'at' parameter: RFC3339 format expected",
			})
			return
		}
		now = parsed
	}

	status, reason := 0, ""
	if s.faultFor(r) == "status7" {
		status, reason = 7, "Simulated device error"
	}

	body, err := marshalEnvelope(s.gen.buildEnvelope(now, s.opt.withMeter, status, reason))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleInverterRealtime serves the Solar API realtime-data endpoint, one answer
// per DataCollection.
//
// The shapes are the ones observed on firmware 1.19.7-1 on 2026-10-16, not the
// documented guess. What the capture and the survey established:
//
//   - the answer is **FLAT**: the measurements sit directly under `Body.Data`,
//     there is no `CumulationInverterData` (nor `CommonInverterData`) object,
//     contrary to the documentation;
//   - a measurement is a `{"Unit": ..., "Value": ...}` pair, not a bare number —
//     so "the type of PAC" is an object, not a float (docs/SOURCES.md);
//   - `Day_ENERGY` and `YEAR_ENERGY` come back `null` on this firmware, and the
//     accumulation also carries `PAC`;
//   - `DeviceStatus` (`InverterState`, `StatusCode`, `ErrorCode`) travels beside
//     the measurements: it is the observed source of the inverter state;
//   - `3PInverterData` answers an **empty** `Body.Data` (the Primo is
//     single-phase), and `CommonInverterData` carries thirteen measurements
//     (`DAY_ENERGY`, `IAC`, `IDC`, `IDC_2`, `IDC_3`, `PAC`, `SAC`,
//     `TOTAL_ENERGY`, `UAC`, `UDC`, `UDC_2`, `UDC_3`, `YEAR_ENERGY`) — with
//     `IDC_3`/`UDC_3` null, as observed.
//
// The values remain the simulator's own (its contract is "values from its own
// curve"); only the shape is the device's. A rehearsal against this handler now
// describes what the device describes.
//
// `cmd/fronius-sim` is not a substitute for the device: the day and year
// counters are answered here as numbers whereas the reference installation
// answers them null (its flow endpoint does the same, a synthetic choice kept
// for development).
func (s *simulator) handleInverterRealtime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.requests.Add(1)

	if s.genericFault(w, r, s.faultFor(r)) {
		return
	}

	snap := s.gen.snapshotAt(time.Now())
	collection := r.URL.Query().Get("DataCollection")

	data := map[string]any{}
	switch collection {
	case "CumulationInverterData":
		data["DAY_ENERGY"] = measurement("Wh", snap.EDayWh)
		data["YEAR_ENERGY"] = measurement("Wh", snap.EYearWh)
		data["TOTAL_ENERGY"] = measurement("Wh", snap.ETotalWh)
		// PAC is part of the accumulation answer, as captured.
		data["PAC"] = measurement("W", snap.PowerW)
	case "CommonInverterData":
		// The thirteen measurements the survey observed, in the Unit/Value form.
		data["DAY_ENERGY"] = measurement("Wh", snap.EDayWh)
		data["YEAR_ENERGY"] = measurement("Wh", snap.EYearWh)
		data["TOTAL_ENERGY"] = measurement("Wh", snap.ETotalWh)
		data["PAC"] = measurement("W", snap.PowerW)
		data["SAC"] = measurement("VA", snap.PowerW)
		// A DC input at 400 V and an AC output at 230 V: the simulator's own
		// electrical model, consistent with the power it reports.
		data["UDC"] = measurement("V", 400.0)
		data["IDC"] = measurement("A", round1(snap.PowerW/400.0))
		data["UDC_2"] = measurement("V", 400.0)
		data["IDC_2"] = measurement("A", 0.0)
		// The third DC input and the third string are not connected on the
		// reference installation: the survey observed null, and null is what the
		// simulator answers — never a zero.
		data["UDC_3"] = measurement("V", nil)
		data["IDC_3"] = measurement("A", nil)
		data["UAC"] = measurement("V", 231.4)
		data["IAC"] = measurement("A", round1(snap.PowerW/231.4))
	case "3PInverterData":
		// Empty Body.Data: the single-phase Primo answers an empty object, which
		// the probe records as `empty` and the application would read as "no
		// measurement for this component".
	default:
		// The simulator does not model this collection. Answering a made-up
		// shape would be worse than answering nothing: a rehearsal must not
		// teach a shape nothing observed.
		http.NotFound(w, r)
		return
	}

	// The inverter state travels beside the measurements, as captured — but only
	// where there are measurements: the empty answer of the three-phase
	// collection carries nothing at all, not even a state (observed: the probe
	// classifies it `empty`, which is not "an answer with no fields").
	if len(data) > 0 {
		// The simulator is producing, so it reports a running state; the
		// reference device answered "Sleeping" with status code 13.
		data["DeviceStatus"] = map[string]any{
			"InverterState": "Running",
			"StatusCode":    7,
			"ErrorCode":     0,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"Body": map[string]any{
			"Data": data,
		},
		"Head": map[string]any{
			"RequestArguments": map[string]any{
				"Scope":          r.URL.Query().Get("Scope"),
				"DeviceId":       r.URL.Query().Get("DeviceId"),
				"DataCollection": r.URL.Query().Get("DataCollection"),
			},
			"Status":    map[string]any{"Code": 0, "Reason": "", "UserMessage": ""},
			"Timestamp": time.Now().In(s.gen.loc).Format(time.RFC3339),
		},
	})
}

// faultFor returns the applicable fault: the X-Sim-Fault header takes precedence over the flag.
func (s *simulator) faultFor(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Sim-Fault")); v != "" {
		return v
	}
	return s.opt.fault
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(append(b, '\n'))
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// handleInverterInfo serves GetInverterInfo, whose shape the survey observed on
// firmware 1.19.7-1: Body.Data keyed by inverter index, with the state as both a
// word and a code, a serial in UniqueID and a custom name. The values are the
// simulator's own; only the shape is the device's.
func (s *simulator) handleInverterInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.requests.Add(1)
	if s.genericFault(w, r, s.faultFor(r)) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"Body": map[string]any{
			"Data": map[string]any{
				"1": map[string]any{
					"CustomName":    "simulated-inverter",
					"DT":            1,
					"ErrorCode":     0,
					"InverterState": "Running",
					"PVPower":       3680,
					"Show":          1,
					"StatusCode":    7,
					"UniqueID":      simulatedSerial,
				},
			},
		},
		"Head": map[string]any{
			"Status":    map[string]any{"Code": 0, "Reason": "", "UserMessage": ""},
			"Timestamp": time.Now().In(s.gen.loc).Format(time.RFC3339),
		},
	})
}

// handleActiveDeviceInfo serves GetActiveDeviceInfo, grouped by device class as
// §4.7.5 describes. A class the installation does not have is answered empty
// rather than omitted: that is how the device states "supported, nothing online",
// and the fallback of internal/detect distinguishes it from a class the answer
// never mentions.
func (s *simulator) handleActiveDeviceInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.requests.Add(1)
	if s.genericFault(w, r, s.faultFor(r)) {
		return
	}

	data := map[string]any{
		"Inverter": map[string]any{"1": map[string]any{"DT": 1, "Serial": simulatedSerial}},
	}
	if s.opt.withMeter {
		data["Meter"] = map[string]any{
			"0": map[string]any{"DT": -1, "Serial": "SIMULATED-METER"},
		}
	}
	if s.opt.withBattery {
		data["Storage"] = map[string]any{
			"0": map[string]any{"DT": -1, "Serial": "SIMULATED-BATTERY"},
		}
	}
	if _, ok := data["Storage"]; !ok {
		data["Storage"] = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"Body": map[string]any{"Data": data},
		"Head": map[string]any{
			"RequestArguments": map[string]any{"DeviceClass": r.URL.Query().Get("DeviceClass")},
			"Status":           map[string]any{"Code": 0, "Reason": "", "UserMessage": ""},
			"Timestamp":        time.Now().In(s.gen.loc).Format(time.RFC3339),
		},
	})
}

// handleAPIVersion serves GetAPIVersion: the one answer of the family that is not
// enveloped (§2.4.1).
func (s *simulator) handleAPIVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.requests.Add(1)
	if s.genericFault(w, r, s.faultFor(r)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"APIVersion":         1,
		"BaseURL":            "/solar_api/v1/",
		"CompatibilityRange": "1.5-9",
	})
}
