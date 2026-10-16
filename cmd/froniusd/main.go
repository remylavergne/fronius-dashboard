// Command froniusd est le service de supervision : collecte périodique des
// mesures, persistance SQLite, API REST de consultation.
//
// Sécurité : le service n'émet que des GET vers l'onduleur (garanti par
// fronius.ReadOnly) et refuse de s'exposer au-delà de la boucle locale sans
// FRONIUS_ALLOW_REMOTE=1 (voir docs/adr/0005). Aucune commande de modification
// du matériel n'existe dans ce binaire.
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
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// shutdownTimeout borne l'arrêt propre du serveur HTTP.
const shutdownTimeout = 10 * time.Second

func main() {
	var (
		backfillDays  = flag.Int("backfill-days", 0, "remplit la base avec N jours d'historique du simulateur avant de démarrer")
		forceBackfill = flag.Bool("force-backfill", false, "autorise un remplissage d'historique vers une adresse non locale (déconseillé)")
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
		return fmt.Errorf("fuseau %q: %w", cfg.TZ, err)
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
			logger.Error("fermeture de la base impossible", "error", err)
		}
	}()

	// Tous les clients passent par la garde lecture seule : une requête non-GET
	// est refusée avant d'atteindre le réseau. Le transport Digest est placé
	// sous cette garde et sous les deux clients : il n'ajoute d'en-tête
	// Authorization qu'en réponse à un 401, et jamais vers un autre hôte.
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

	if cfg.RetentionDays > 0 {
		if err := purge(ctx, store, loc, cfg.RetentionDays, logger); err != nil {
			return err
		}
	}

	if backfillDays > 0 {
		if err := backfill(ctx, client, store, cfg, backfillDays, forceBackfill, logger); err != nil {
			return err
		}
	}

	if err := store.InsertEvent(ctx, model.Event{
		TS:       time.Now().UTC(),
		Kind:     model.EventStartup,
		Severity: model.SeverityInfo,
		Message:  "collecteur démarré",
		Detail:   cfg.SolarAPIURL,
	}); err != nil {
		logger.Warn("événement de démarrage non enregistré", "error", err)
	}

	col := &collector.Collector{
		Source:   client,
		Sink:     store,
		Log:      logger,
		Interval: cfg.PollInterval,
		Jitter:   cfg.PollJitter,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := col.Run(ctx); err != nil {
			logger.Error("collecteur arrêté sur erreur", "error", err)
		}
	}()

	// Le détecteur démarre toujours, avec ou sans identifiants : c'est la seule
	// façon de savoir que l'appareil exige une authentification, et le bundle
	// prouve que certains chemins internes répondent sans elle (module yTJA).
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
			logger.Error("détecteur arrêté sur erreur", "error", err)
		}
	}()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(store, cfg, version(), logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()

	logger.Info("froniusd démarré",
		"api_addr", cfg.HTTPAddr,
		"solarapi_url", cfg.SolarAPIURL,
		"db_path", cfg.DBPath,
		"poll_interval", cfg.PollInterval,
		"tz", cfg.TZ,
		"retention_days", cfg.RetentionDays,
		"web_dir", cfg.WebDir)

	select {
	case <-ctx.Done():
		logger.Info("arrêt demandé")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serveur HTTP: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("arrêt du serveur HTTP impossible", "error", err)
	}
	wg.Wait()
	logger.Info("arrêt terminé")
	return nil
}

// purge applique la rétention une fois au démarrage. Aucune purge planifiée
// n'est active en phase 1 (ADR 0006) : elle n'a lieu que si l'opérateur a
// explicitement fixé FRONIUS_RETENTION_DAYS.
func purge(ctx context.Context, store *storage.Store, loc *time.Location, days int, logger *slog.Logger) error {
	now := time.Now().In(loc)
	limit := time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, loc)
	deleted, err := store.PurgeBefore(ctx, limit)
	if err != nil {
		return err
	}
	logger.Info("purge de rétention terminée", "before", limit, "samples_deleted", deleted, "retention_days", days)
	return store.InsertEvent(ctx, model.Event{
		TS:       time.Now().UTC(),
		Kind:     model.EventPurge,
		Severity: model.SeverityInfo,
		Message:  "purge de rétention exécutée",
		Detail:   fmt.Sprintf("%d échantillons supprimés avant %s", deleted, limit.Format(time.RFC3339)),
	})
}

// backfill reconstitue un historique en interrogeant le simulateur avec son
// paramètre non standard « at ».
//
// Par défaut, la cible doit être locale : marteler l'onduleur réel avec des
// dizaines de milliers de requêtes serait inacceptable. --force-backfill lève
// ce refus en connaissance de cause.
func backfill(ctx context.Context, client *solarapi.Client, store *storage.Store, cfg config.Config, days int, force bool, logger *slog.Logger) error {
	if !force {
		u, err := url.Parse(cfg.SolarAPIURL)
		if err != nil {
			return fmt.Errorf("URL Solar API %q: %w", cfg.SolarAPIURL, err)
		}
		if !config.IsLoopbackHost(u.Hostname()) {
			return fmt.Errorf(
				"refus: le remplissage d'historique interrogerait %s ; "+
					"le simulateur local est attendu (utiliser --force-backfill pour passer outre)",
				cfg.SolarAPIURL)
		}
	}

	step := cfg.PollInterval
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	planned := int(end.Sub(start) / step)

	logger.Info("backfill démarré", "days", days, "step", step, "planned_requests", planned)

	var inserted, failed int
	for ts, i := start, 0; ts.Before(end); ts, i = ts.Add(step), i+1 {
		if err := ctx.Err(); err != nil {
			logger.Warn("backfill interrompu", "inserted", inserted, "failed", failed)
			return nil
		}
		env, err := client.GetPowerFlowRealtimeDataAt(ctx, ts)
		if err != nil {
			failed++
			if failed == 1 {
				logger.Warn("backfill: première requête en échec", "at", ts, "error", err)
			}
			continue
		}
		if err := store.InsertSample(ctx, model.FromPowerFlow(env, ts)); err != nil {
			return err
		}
		inserted++
		if i%2000 == 1999 {
			logger.Info("backfill en cours", "inserted", inserted, "at", ts)
		}
	}

	logger.Info("backfill terminé", "inserted", inserted, "failed", failed)
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
		return 0, fmt.Errorf("niveau de log inconnu %q", s)
	}
}

// version retourne la version du binaire : version du module si elle est
// connue, sinon révision VCS, sinon « dev ».
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
