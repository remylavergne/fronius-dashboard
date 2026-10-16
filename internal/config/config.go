// Package config charge la configuration de froniusd depuis l'environnement.
//
// La phase 1 n'utilise aucun fichier de configuration : toutes les valeurs
// viennent de variables d'environnement préfixées FRONIUS_ (voir .env.example
// et docs/DEVELOPMENT.md). Aucune valeur secrète n'est lue ici.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Valeurs par défaut.
const (
	defaultSolarAPIURL  = "http://127.0.0.1:8081"
	defaultHTTPAddr     = "127.0.0.1:8080"
	defaultDBPath       = "./data/fronius.db"
	defaultTZ           = "Europe/Brussels"
	defaultPollInterval = 5 * time.Second
	defaultPollJitter   = 500 * time.Millisecond
	defaultHTTPTimeout  = 5 * time.Second
	defaultStaleAfter   = 30 * time.Second
	defaultLogLevel     = "info"
	defaultLogFormat    = "text"
	minPollInterval     = 1 * time.Second
	authADRPath         = "docs/adr/0005-authentification-et-acces-distant.md"
	allowRemoteEnvVar   = "FRONIUS_ALLOW_REMOTE"
	solarAPIURLEnvVar   = "FRONIUS_SOLARAPI_URL"
	httpAddrEnvVar      = "FRONIUS_HTTP_ADDR"
	dbPathEnvVar        = "FRONIUS_DB_PATH"
	tzEnvVar            = "FRONIUS_TZ"
	pollIntervalEnvVar  = "FRONIUS_POLL_INTERVAL"
	pollJitterEnvVar    = "FRONIUS_POLL_JITTER"
	httpTimeoutEnvVar   = "FRONIUS_HTTP_TIMEOUT"
	retentionDaysEnvVar = "FRONIUS_RETENTION_DAYS"
	logLevelEnvVar      = "FRONIUS_LOG_LEVEL"
	logFormatEnvVar     = "FRONIUS_LOG_FORMAT"
	webDirEnvVar        = "FRONIUS_WEB_DIR"
	staleAfterEnvVar    = "FRONIUS_STALE_AFTER"
)

// Config est la configuration effective de l'application.
type Config struct {
	SolarAPIURL   string        // FRONIUS_SOLARAPI_URL      défaut http://127.0.0.1:8081
	HTTPAddr      string        // FRONIUS_HTTP_ADDR         défaut 127.0.0.1:8080
	AllowRemote   bool          // FRONIUS_ALLOW_REMOTE      défaut false
	DBPath        string        // FRONIUS_DB_PATH           défaut ./data/fronius.db
	TZ            string        // FRONIUS_TZ                défaut Europe/Brussels
	PollInterval  time.Duration // FRONIUS_POLL_INTERVAL     défaut 5s
	PollJitter    time.Duration // FRONIUS_POLL_JITTER       défaut 500ms
	HTTPTimeout   time.Duration // FRONIUS_HTTP_TIMEOUT      défaut 5s
	RetentionDays int           // FRONIUS_RETENTION_DAYS    défaut 0 (= conservation)
	LogLevel      string        // FRONIUS_LOG_LEVEL         défaut info
	LogFormat     string        // FRONIUS_LOG_FORMAT        défaut text (ou json)
	WebDir        string        // FRONIUS_WEB_DIR           défaut "" (pas de statique)
	StaleAfter    time.Duration // FRONIUS_STALE_AFTER       défaut 30s
}

// Load lit l'environnement et retourne la configuration validée.
//
// Toutes les erreurs sont accumulées : une configuration à plusieurs fautes
// produit un message qui les liste toutes, plutôt que de n'en révéler qu'une.
func Load() (Config, error) {
	c := Config{
		SolarAPIURL:  env(solarAPIURLEnvVar, defaultSolarAPIURL),
		HTTPAddr:     env(httpAddrEnvVar, defaultHTTPAddr),
		DBPath:       env(dbPathEnvVar, defaultDBPath),
		TZ:           env(tzEnvVar, defaultTZ),
		LogLevel:     strings.ToLower(env(logLevelEnvVar, defaultLogLevel)),
		LogFormat:    strings.ToLower(env(logFormatEnvVar, defaultLogFormat)),
		WebDir:       env(webDirEnvVar, ""),
		AllowRemote:  false,
		PollInterval: defaultPollInterval,
		PollJitter:   defaultPollJitter,
		HTTPTimeout:  defaultHTTPTimeout,
		StaleAfter:   defaultStaleAfter,
	}

	var errs []error

	if v, ok := lookup(allowRemoteEnvVar); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q : booléen attendu (1/0, true/false)", allowRemoteEnvVar, v))
		} else {
			c.AllowRemote = b
		}
	}

	durationTargets := []struct {
		key string
		dst *time.Duration
	}{
		{pollIntervalEnvVar, &c.PollInterval},
		{pollJitterEnvVar, &c.PollJitter},
		{httpTimeoutEnvVar, &c.HTTPTimeout},
		{staleAfterEnvVar, &c.StaleAfter},
	}
	for _, tgt := range durationTargets {
		v, ok := lookup(tgt.key)
		if !ok {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q : durée Go attendue (ex. 5s, 500ms)", tgt.key, v))
			continue
		}
		*tgt.dst = d
	}

	if v, ok := lookup(retentionDaysEnvVar); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q : entier attendu", retentionDaysEnvVar, v))
		} else {
			c.RetentionDays = n
		}
	}

	errs = append(errs, c.validate()...)
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("configuration invalide : %w", errors.Join(errs...))
	}
	return c, nil
}

func (c Config) validate() []error {
	var errs []error

	if c.PollInterval < minPollInterval {
		errs = append(errs, fmt.Errorf("%s=%v : minimum %v", pollIntervalEnvVar, c.PollInterval, minPollInterval))
	}
	if c.PollJitter < 0 {
		errs = append(errs, fmt.Errorf("%s=%v : doit être positif ou nul", pollJitterEnvVar, c.PollJitter))
	}
	if c.HTTPTimeout <= 0 {
		errs = append(errs, fmt.Errorf("%s=%v : doit être strictement positif", httpTimeoutEnvVar, c.HTTPTimeout))
	}
	if c.StaleAfter <= 0 {
		errs = append(errs, fmt.Errorf("%s=%v : doit être strictement positif", staleAfterEnvVar, c.StaleAfter))
	}
	if c.RetentionDays < 0 {
		errs = append(errs, fmt.Errorf("%s=%d : doit être positif ou nul (0 = conservation illimitée)", retentionDaysEnvVar, c.RetentionDays))
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("%s=%q : valeurs admises debug, info, warn, error", logLevelEnvVar, c.LogLevel))
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("%s=%q : valeurs admises text, json", logFormatEnvVar, c.LogFormat))
	}

	if _, err := c.Location(); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q : %w", tzEnvVar, c.TZ, err))
	}
	if u, err := url.Parse(c.SolarAPIURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, fmt.Errorf("%s=%q : URL http(s) absolue attendue", solarAPIURLEnvVar, c.SolarAPIURL))
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q : adresse hôte:port attendue", httpAddrEnvVar, c.HTTPAddr))
	}

	return errs
}

// Location résout le fuseau configuré.
func (c Config) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.TZ)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

// CheckListenAddress refuse une écoute non locale tant que l'accès distant n'a
// pas été explicitement autorisé : l'API de la phase 1 n'a aucune
// authentification (voir authADRPath).
func (c Config) CheckListenAddress() error {
	if c.AllowRemote {
		return nil
	}
	host, _, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return fmt.Errorf("%s=%q : %w", httpAddrEnvVar, c.HTTPAddr, err)
	}
	if isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf(
		"refus: %s n'est pas une adresse locale ; définir %s=1 après avoir lu %s",
		c.HTTPAddr, allowRemoteEnvVar, authADRPath)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

// env retourne la variable si elle est définie et non vide, sinon la valeur par défaut.
func env(key, fallback string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return fallback
}

// lookup retourne la variable si elle est définie et non vide.
func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}
