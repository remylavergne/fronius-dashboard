// Package config loads the froniusd configuration from the environment.
//
// No configuration file is used: all values come from environment variables
// prefixed with FRONIUS_ (see .env.example and docs/DEVELOPMENT.md). The device
// credentials (FRONIUS_DEVICE_USER, FRONIUS_DEVICE_PASSWORD) are read here and
// kept in a secret.String: no other package receives them in clear. Each one
// also accepts a file variant (FRONIUS_DEVICE_USER_FILE,
// FRONIUS_DEVICE_PASSWORD_FILE), so Docker secrets work without a secret in an
// environment variable or a compose file.
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

	"fronius-supervisor/internal/secret"
)

// Default values.
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
	// defaultDetectInterval: the official client queries the internal paths
	// every 5,000 ms (bundle module aYRN); 60 s is enough for an identity and a
	// presence that rarely change.
	defaultDetectInterval = 60 * time.Second
	minDetectInterval     = 5 * time.Second
	authADRPath           = "docs/adr/0005-authentication-and-remote-access.md"
	allowRemoteEnvVar     = "FRONIUS_ALLOW_REMOTE"
	solarAPIURLEnvVar     = "FRONIUS_SOLARAPI_URL"
	httpAddrEnvVar        = "FRONIUS_HTTP_ADDR"
	dbPathEnvVar          = "FRONIUS_DB_PATH"
	tzEnvVar              = "FRONIUS_TZ"
	pollIntervalEnvVar    = "FRONIUS_POLL_INTERVAL"
	pollJitterEnvVar      = "FRONIUS_POLL_JITTER"
	httpTimeoutEnvVar     = "FRONIUS_HTTP_TIMEOUT"
	retentionDaysEnvVar   = "FRONIUS_RETENTION_DAYS"
	logLevelEnvVar        = "FRONIUS_LOG_LEVEL"
	logFormatEnvVar       = "FRONIUS_LOG_FORMAT"
	webDirEnvVar          = "FRONIUS_WEB_DIR"
	staleAfterEnvVar      = "FRONIUS_STALE_AFTER"
	// The device credentials are read by froniusd and fronius-probe, never by
	// the frontend nor by the REST API.
	deviceUserEnvVar     = "FRONIUS_DEVICE_USER"
	devicePasswordEnvVar = "FRONIUS_DEVICE_PASSWORD"
	// File variants of the credentials, for Docker secrets.
	deviceUserFileEnvVar     = "FRONIUS_DEVICE_USER_FILE"
	devicePasswordFileEnvVar = "FRONIUS_DEVICE_PASSWORD_FILE"
	detectIntervalEnvVar     = "FRONIUS_DETECT_INTERVAL"
)

// Config is the effective configuration of the application.
type Config struct {
	SolarAPIURL   string        // FRONIUS_SOLARAPI_URL      default http://127.0.0.1:8081
	HTTPAddr      string        // FRONIUS_HTTP_ADDR         default 127.0.0.1:8080
	AllowRemote   bool          // FRONIUS_ALLOW_REMOTE      default false
	DBPath        string        // FRONIUS_DB_PATH           default ./data/fronius.db
	TZ            string        // FRONIUS_TZ                default Europe/Brussels
	PollInterval  time.Duration // FRONIUS_POLL_INTERVAL     default 5s
	PollJitter    time.Duration // FRONIUS_POLL_JITTER       default 500ms
	HTTPTimeout   time.Duration // FRONIUS_HTTP_TIMEOUT      default 5s
	RetentionDays int           // FRONIUS_RETENTION_DAYS    default 0 (= retention)
	LogLevel      string        // FRONIUS_LOG_LEVEL         default info
	LogFormat     string        // FRONIUS_LOG_FORMAT        default text (or json)
	WebDir        string        // FRONIUS_WEB_DIR           default "" (no static files)
	StaleAfter    time.Duration // FRONIUS_STALE_AFTER       default 30s
	// Device identity: the two variables below are read together or not at all
	// (see validate). Each also accepts a file variant
	// (FRONIUS_DEVICE_USER_FILE, FRONIUS_DEVICE_PASSWORD_FILE): when that
	// variable is set, the file content is used and takes precedence over the
	// inline variable — the Docker-secrets convention — with the trailing CR/LF
	// Docker writes stripped; an unreadable file is a startup error, and an
	// empty file means "no credential". The password never leaves this type
	// without Reveal().
	DeviceUser     string        // FRONIUS_DEVICE_USER       default ""
	DevicePassword secret.String // FRONIUS_DEVICE_PASSWORD   default ""
	DetectInterval time.Duration // FRONIUS_DETECT_INTERVAL   default 60s
}

// Load reads the environment and returns the validated configuration.
//
// All errors are accumulated: a configuration with several faults produces a
// message that lists them all, rather than revealing only one.
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

		DetectInterval: defaultDetectInterval,
	}

	var errs []error

	user, err := credential(deviceUserEnvVar, deviceUserFileEnvVar)
	if err != nil {
		errs = append(errs, err)
	} else {
		c.DeviceUser = user
	}
	password, err := credential(devicePasswordEnvVar, devicePasswordFileEnvVar)
	if err != nil {
		errs = append(errs, err)
	} else {
		c.DevicePassword = secret.New(password)
	}

	if v, ok := lookup(allowRemoteEnvVar); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: boolean expected (1/0, true/false)", allowRemoteEnvVar, v))
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
		{detectIntervalEnvVar, &c.DetectInterval},
	}
	for _, tgt := range durationTargets {
		v, ok := lookup(tgt.key)
		if !ok {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: Go duration expected (e.g. 5s, 500ms)", tgt.key, v))
			continue
		}
		*tgt.dst = d
	}

	if v, ok := lookup(retentionDaysEnvVar); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s=%q: integer expected", retentionDaysEnvVar, v))
		} else {
			c.RetentionDays = n
		}
	}

	errs = append(errs, c.validate()...)
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return c, nil
}

func (c Config) validate() []error {
	var errs []error

	if c.PollInterval < minPollInterval {
		errs = append(errs, fmt.Errorf("%s=%v: minimum %v", pollIntervalEnvVar, c.PollInterval, minPollInterval))
	}
	if c.PollJitter < 0 {
		errs = append(errs, fmt.Errorf("%s=%v: must be positive or zero", pollJitterEnvVar, c.PollJitter))
	}
	if c.HTTPTimeout <= 0 {
		errs = append(errs, fmt.Errorf("%s=%v: must be strictly positive", httpTimeoutEnvVar, c.HTTPTimeout))
	}
	if c.StaleAfter <= 0 {
		errs = append(errs, fmt.Errorf("%s=%v: must be strictly positive", staleAfterEnvVar, c.StaleAfter))
	}
	if c.RetentionDays < 0 {
		errs = append(errs, fmt.Errorf("%s=%d: must be positive or zero (0 = unlimited retention)", retentionDaysEnvVar, c.RetentionDays))
	}
	if c.DetectInterval < minDetectInterval {
		errs = append(errs, fmt.Errorf("%s=%v: minimum %v", detectIntervalEnvVar, c.DetectInterval, minDetectInterval))
	}
	// Partial credentials: the two variables go together. The message only
	// names the variable names, never the password value.
	if c.DeviceUser == "" && !c.DevicePassword.IsZero() {
		errs = append(errs, fmt.Errorf("%s is set but %s is empty: the two go together", devicePasswordEnvVar, deviceUserEnvVar))
	}
	if c.DeviceUser != "" && c.DevicePassword.IsZero() {
		errs = append(errs, fmt.Errorf("%s is set but %s is empty: the two go together", deviceUserEnvVar, devicePasswordEnvVar))
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("%s=%q: allowed values debug, info, warn, error", logLevelEnvVar, c.LogLevel))
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("%s=%q: allowed values text, json", logFormatEnvVar, c.LogFormat))
	}

	if _, err := c.Location(); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q: %w", tzEnvVar, c.TZ, err))
	}
	if u, err := url.Parse(c.SolarAPIURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, fmt.Errorf("%s=%q: absolute http(s) URL expected", solarAPIURLEnvVar, c.SolarAPIURL))
	}
	if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("%s=%q: host:port address expected", httpAddrEnvVar, c.HTTPAddr))
	}

	return errs
}

// Location resolves the configured timezone.
func (c Config) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.TZ)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

// CheckListenAddress refuses a non-local listen as long as remote access has
// not been explicitly allowed: the REST API has no authentication (see
// authADRPath). The device credentials change nothing here: they authenticate
// the supervision towards the inverter, not the client towards the API.
func (c Config) CheckListenAddress() error {
	if c.AllowRemote {
		return nil
	}
	host, _, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", httpAddrEnvVar, c.HTTPAddr, err)
	}
	if IsLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf(
		"refusal: %s is not a local address; set %s=1 after reading %s",
		c.HTTPAddr, allowRemoteEnvVar, authADRPath)
}

// IsLoopbackHost reports whether a host designates the local machine:
// "localhost" or any loopback address (127.0.0.0/8, ::1).
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

// env returns the variable if it is set and non-empty, otherwise the fallback value.
func env(key, fallback string) string {
	if v, ok := lookup(key); ok {
		return v
	}
	return fallback
}

// credential resolves a device credential, preferring the file named by
// fileKey over the inline value in valueKey (the Docker-secrets convention).
//
// The file's trailing CR/LF are stripped: Docker secret files end with a
// newline. Nothing else is trimmed, so a password may legitimately contain
// spaces. An unreadable file is an error naming the variable and the path,
// never the content; an empty file yields an empty credential, which validation
// treats as "no credential".
func credential(valueKey, fileKey string) (string, error) {
	path, ok := lookup(fileKey)
	if !ok {
		v, _ := lookup(valueKey)
		return v, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s=%q: unreadable credentials file: %w", fileKey, path, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// lookup returns the variable if it is set and non-empty.
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
