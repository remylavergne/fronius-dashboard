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
