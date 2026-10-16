// Command fronius-sim sert une imitation locale de la Fronius Solar API v1.
//
// Usage :
//
//	go run ./cmd/fronius-sim [flags]
//
// Le simulateur n'émet aucune requête sortante et n'écrit rien sur disque.
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

// faultModes liste les pannes injectables.
var faultModes = []string{"", "500", "503", "401", "404", "timeout", "invalid_json", "empty", "status7"}

type options struct {
	addr      string
	seed      uint64
	nominalW  float64
	fault     string
	withMeter bool
	tz        string
	logLevel  string
}

type simulator struct {
	gen          *generator
	opt          options
	log          *slog.Logger
	requests     atomic.Int64
	faultTimeout time.Duration // durée du faux timeout, surchargeable en test
}

func main() {
	var opt options
	flag.StringVar(&opt.addr, "addr", "127.0.0.1:8081", "adresse d'écoute")
	flag.Uint64Var(&opt.seed, "seed", 42, "graine de la courbe synthétique")
	flag.Float64Var(&opt.nominalW, "nominal-w", 3680, "puissance crête du champ PV, en W")
	fault := flag.String("fault", "", "panne globale à injecter ("+strings.Join(faultModes[1:], ", ")+")")
	flag.BoolVar(&opt.withMeter, "with-meter", false, "simule un Fronius Smart Meter (P_Grid, P_Load, rel_* renseignés ; P_Akku reste null)")
	flag.StringVar(&opt.tz, "tz", "Europe/Brussels", "fuseau local de l'installation")
	flag.StringVar(&opt.logLevel, "log-level", "info", "niveau de log (debug, info, warn, error)")
	flag.Parse()
	opt.fault = *fault

	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, "fronius-sim:", err)
		os.Exit(1)
	}
}

func run(opt options) error {
	if !slices.Contains(faultModes, opt.fault) {
		return fmt.Errorf("panne inconnue %q (valeurs : %s)", opt.fault, strings.Join(nonEmpty(faultModes), ", "))
	}
	if opt.nominalW < 0 {
		return fmt.Errorf("--nominal-w doit être positif, reçu %v", opt.nominalW)
	}

	loc, err := time.LoadLocation(opt.tz)
	if err != nil {
		return fmt.Errorf("fuseau %q: %w", opt.tz, err)
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
	logger.Info("fronius-sim démarré",
		"addr", opt.addr, "seed", opt.seed, "nominal_w", opt.nominalW,
		"tz", opt.tz, "fault", opt.fault, "with_meter", opt.withMeter)

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
	}
}

// Handler retourne le routeur du simulateur.
func (s *simulator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(powerFlowPath, s.logged(s.handlePowerFlow))
	mux.HandleFunc("/sim/health", s.logged(s.handleHealth))
	return mux
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
		s.log.Info("requête",
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

	switch fault := s.faultFor(r); fault {
	case "timeout":
		// Dort puis ferme sans réponse.
		select {
		case <-time.After(s.faultTimeout):
		case <-r.Context().Done():
		}
		return
	case "401":
		w.Header().Set("x-www-authenticate",
			`Digest realm="Webinterface area", nonce="sim-nonce-1", qop="auth"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	case "404":
		http.NotFound(w, r)
		return
	case "500":
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	case "503":
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	case "empty":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	case "invalid_json":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Corps tronqué : décodage JSON impossible côté client.
		_, _ = w.Write([]byte(`{"Body":{"Data":{"Site":{"P_PV":`))
		return
	}

	now := time.Now()
	if at := r.URL.Query().Get("at"); at != "" {
		parsed, err := time.Parse(time.RFC3339, at)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "paramètre 'at' invalide : format RFC3339 attendu",
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

// faultFor retourne la panne applicable : l'en-tête X-Sim-Fault prime sur le flag.
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
		return 0, fmt.Errorf("niveau de log inconnu %q", s)
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
