// Command froniusd is the supervision service: periodic collection of
// measurements, SQLite persistence, REST API for consultation.
//
// Security: the service only issues GETs towards the inverter (guaranteed by
// fronius.ReadOnly) and refuses to expose itself beyond the loopback without
// FRONIUS_ALLOW_REMOTE=1 (see docs/adr/0005). No hardware modification command
// exists in this binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"fronius-supervisor/internal/api"
	"fronius-supervisor/internal/collector"
	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/detect"
	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/digest"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/maintenance"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// shutdownTimeout bounds the clean shutdown of the HTTP server.
const shutdownTimeout = 10 * time.Second

func main() {
	var (
		backfillDays  = flag.Int("backfill-days", 0, "fills the database with N days of simulator history before starting")
		forceBackfill = flag.Bool("force-backfill", false, "allows a history backfill towards a non-local address (not recommended)")
	)
	flag.Parse()

	if err := run(*backfillDays, *forceBackfill); err != nil {
		fmt.Fprintln(os.Stderr, "froniusd:", err)
		os.Exit(1)
	}
}

func run(backfillDays int, forceBackfill bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.CheckListenAddress(); err != nil {
		return err
	}
	loc, err := cfg.Location()
	if err != nil {
		return fmt.Errorf("time zone %q: %w", cfg.TZ, err)
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	store, err := storage.Open(cfg.DBPath, loc)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("database close failed", "error", err)
		}
	}()

	// All clients go through the read-only guard: a non-GET request
	// is refused before reaching the network. The Digest transport is placed
	// beneath this guard and beneath the two clients: it only adds an
	// Authorization header in response to a 401, and never towards another host.
	digestTransport, err := digest.New(&http.Client{}, cfg.SolarAPIURL,
		digest.Credentials{Username: cfg.DeviceUser, Password: cfg.DevicePassword}, logger)
	if err != nil {
		return err
	}
	guarded := fronius.ReadOnly{Inner: digestTransport}

	client, err := solarapi.New(cfg.SolarAPIURL, guarded, cfg.HTTPTimeout, logger)
	if err != nil {
		return err
	}

	internalClient, err := internalapi.New(cfg.SolarAPIURL, guarded, cfg.HTTPTimeout, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if backfillDays > 0 {
		if err := backfill(ctx, client, store, cfg, backfillDays, forceBackfill, logger); err != nil {
			return err
		}
	}

	if err := store.InsertEvent(ctx, model.Event{
		TS:       time.Now().UTC(),
		Kind:     model.EventStartup,
		Severity: model.SeverityInfo,
		Message:  "collector started",
		Detail:   cfg.SolarAPIURL,
	}); err != nil {
		logger.Warn("startup event not recorded", "error", err)
	}

	col := &collector.Collector{
		Source: client,
		Sink:   store,
		// The internal flow measurement completes the day and year energies that
		// the Solar API returns as null on firmware 1.19.7-1. It is only used for
		// that: a measured Solar API value is never overwritten.
		Fallback: internalClient,
		Log:      logger,
		Interval: cfg.PollInterval,
		Jitter:   cfg.PollJitter,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := col.Run(ctx); err != nil {
			logger.Error("collector stopped on error", "error", err)
		}
	}()

	// The detector always starts, with or without credentials: it is the only
	// way to know that the device requires authentication, and the bundle
	// proves that some internal paths respond without it (module yTJA).
	det := &detect.Detector{
		Source:                internalClient,
		Sink:                  store,
		Log:                   logger,
		Interval:              cfg.DetectInterval,
		CredentialsConfigured: !cfg.DevicePassword.IsZero(),
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := det.Run(ctx); err != nil {
			logger.Error("detector stopped on error", "error", err)
		}
	}()

	// Maintenance (hourly aggregation, retention purge) starts after the
	// history backfill: its first pass then aggregates what the
	// backfill has just written.
	maint := &maintenance.Runner{
		Store:         store,
		Loc:           loc,
		RetentionDays: cfg.RetentionDays,
		Log:           logger,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := maint.Run(ctx); err != nil {
			logger.Error("maintenance stopped on error", "error", err)
		}
	}()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(store, cfg, version(), logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	logger.Info("froniusd started",
		"api_addr", cfg.HTTPAddr,
		"solarapi_url", cfg.SolarAPIURL,
		"db_path", cfg.DBPath,
		"poll_interval", cfg.PollInterval,
		"tz", cfg.TZ,
		"retention_days", cfg.RetentionDays,
		"web_dir", cfg.WebDir)

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown failed", "error", err)
	}
	wg.Wait()
	logger.Info("shutdown complete")
	return nil
}

// backfill rebuilds a history by querying the simulator with its
// non-standard "at" parameter.
//
// By default, the target must be local: hammering the real inverter with tens
// of thousands of requests would be unacceptable. --force-backfill lifts
// this refusal knowingly.
func backfill(ctx context.Context, client *solarapi.Client, store *storage.Store, cfg config.Config, days int, force bool, logger *slog.Logger) error {
	if !force {
		u, err := url.Parse(cfg.SolarAPIURL)
		if err != nil {
			return fmt.Errorf("Solar API URL %q: %w", cfg.SolarAPIURL, err)
		}
		if !config.IsLoopbackHost(u.Hostname()) {
			return fmt.Errorf(
				"refusal: the history backfill would query %s; "+
					"the local simulator is expected (use --force-backfill to override)",
				cfg.SolarAPIURL)
		}
	}

	step := cfg.PollInterval
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	planned := int(end.Sub(start) / step)

	logger.Info("backfill started", "days", days, "step", step, "planned_requests", planned)

	var inserted, failed int
	for ts, i := start, 0; ts.Before(end); ts, i = ts.Add(step), i+1 {
		if err := ctx.Err(); err != nil {
			logger.Warn("backfill interrupted", "inserted", inserted, "failed", failed)
			return nil
		}
		env, err := client.GetPowerFlowRealtimeDataAt(ctx, ts)
		if err != nil {
			failed++
			if failed == 1 {
				logger.Warn("backfill: first request failed", "at", ts, "error", err)
			}
			continue
		}
		if err := store.InsertSample(ctx, model.FromPowerFlow(env, ts)); err != nil {
			return err
		}
		inserted++
		if i%2000 == 1999 {
			logger.Info("backfill in progress", "inserted", inserted, "at", ts)
		}
	}

	logger.Info("backfill finished", "inserted", inserted, "failed", failed)
	return nil
}

func newLogger(cfg config.Config) (*slog.Logger, error) {
	level, err := parseLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts)), nil
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts)), nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}

// version returns the binary version: the module version if it is
// known, otherwise the VCS revision, otherwise "dev".
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return "dev"
}
