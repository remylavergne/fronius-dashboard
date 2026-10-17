package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clearFroniusEnv removes all FRONIUS_* variables for the duration of the test.
func clearFroniusEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(key, "FRONIUS_") {
			continue
		}
		old, had := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(key, old)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	clearFroniusEnv(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		SolarAPIURL:    "http://127.0.0.1:8081",
		HTTPAddr:       "127.0.0.1:8080",
		AllowRemote:    false,
		DBPath:         "./data/fronius.db",
		TZ:             "Europe/Brussels",
		PollInterval:   5 * time.Second,
		PollJitter:     500 * time.Millisecond,
		HTTPTimeout:    5 * time.Second,
		RetentionDays:  0,
		LogLevel:       "info",
		LogFormat:      "text",
		WebDir:         "",
		StaleAfter:     30 * time.Second,
		DeviceUser:     "",
		DevicePassword: "",
		DetectInterval: 60 * time.Second,
	}
	if got != want {
		t.Fatalf("wrong defaults\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_SOLARAPI_URL", "https://inverter.local:443")
	t.Setenv("FRONIUS_HTTP_ADDR", "0.0.0.0:9000")
	t.Setenv("FRONIUS_ALLOW_REMOTE", "1")
	t.Setenv("FRONIUS_DB_PATH", "/tmp/x.db")
	t.Setenv("FRONIUS_TZ", "UTC")
	t.Setenv("FRONIUS_POLL_INTERVAL", "10s")
	t.Setenv("FRONIUS_POLL_JITTER", "0s")
	t.Setenv("FRONIUS_HTTP_TIMEOUT", "1500ms")
	t.Setenv("FRONIUS_RETENTION_DAYS", "365")
	t.Setenv("FRONIUS_LOG_LEVEL", "DEBUG")
	t.Setenv("FRONIUS_LOG_FORMAT", "JSON")
	t.Setenv("FRONIUS_WEB_DIR", "/srv/web")
	t.Setenv("FRONIUS_STALE_AFTER", "2m")
	t.Setenv("FRONIUS_DEVICE_USER", "installer")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "password")
	t.Setenv("FRONIUS_DETECT_INTERVAL", "5m")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SolarAPIURL != "https://inverter.local:443" || got.HTTPAddr != "0.0.0.0:9000" ||
		!got.AllowRemote || got.DBPath != "/tmp/x.db" || got.TZ != "UTC" ||
		got.PollInterval != 10*time.Second || got.PollJitter != 0 ||
		got.HTTPTimeout != 1500*time.Millisecond || got.RetentionDays != 365 ||
		got.LogLevel != "debug" || got.LogFormat != "json" || got.WebDir != "/srv/web" ||
		got.StaleAfter != 2*time.Minute ||
		got.DeviceUser != "installer" || got.DevicePassword.Reveal() != "password" ||
		got.DetectInterval != 5*time.Minute {
		t.Fatalf("overrides not applied: %+v", got)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantSub string
	}{
		{"interval too short", map[string]string{"FRONIUS_POLL_INTERVAL": "500ms"}, "FRONIUS_POLL_INTERVAL"},
		{"unparsable interval", map[string]string{"FRONIUS_POLL_INTERVAL": "five seconds"}, "Go duration expected"},
		{"negative jitter", map[string]string{"FRONIUS_POLL_JITTER": "-1s"}, "FRONIUS_POLL_JITTER"},
		{"zero timeout", map[string]string{"FRONIUS_HTTP_TIMEOUT": "0s"}, "FRONIUS_HTTP_TIMEOUT"},
		{"negative retention", map[string]string{"FRONIUS_RETENTION_DAYS": "-1"}, "FRONIUS_RETENTION_DAYS"},
		{"non-integer retention", map[string]string{"FRONIUS_RETENTION_DAYS": "one year"}, "integer expected"},
		{"invalid boolean", map[string]string{"FRONIUS_ALLOW_REMOTE": "maybe"}, "FRONIUS_ALLOW_REMOTE"},
		{"unknown timezone", map[string]string{"FRONIUS_TZ": "Mars/Olympus"}, "FRONIUS_TZ"},
		{"URL without scheme", map[string]string{"FRONIUS_SOLARAPI_URL": "127.0.0.1:8081"}, "absolute http(s) URL"},
		{"exotic scheme URL", map[string]string{"FRONIUS_SOLARAPI_URL": "ftp://x/y"}, "absolute http(s) URL"},
		{"address without port", map[string]string{"FRONIUS_HTTP_ADDR": "localhost"}, "host:port"},
		{"unknown log level", map[string]string{"FRONIUS_LOG_LEVEL": "verbose"}, "FRONIUS_LOG_LEVEL"},
		{"unknown log format", map[string]string{"FRONIUS_LOG_FORMAT": "xml"}, "FRONIUS_LOG_FORMAT"},
		{"zero stale", map[string]string{"FRONIUS_STALE_AFTER": "0s"}, "FRONIUS_STALE_AFTER"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearFroniusEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("Load should have failed")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestLoadAccumulatesErrors(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_POLL_INTERVAL", "1ms")
	t.Setenv("FRONIUS_LOG_FORMAT", "yaml")

	_, err := Load()
	if err == nil {
		t.Fatal("Load should have failed")
	}
	for _, want := range []string{"FRONIUS_POLL_INTERVAL", "FRONIUS_LOG_FORMAT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestLoadRejectsPartialCredentials(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		wantSubs []string
	}{
		{
			name:     "user only",
			env:      map[string]string{"FRONIUS_DEVICE_USER": "installer"},
			wantSubs: []string{"FRONIUS_DEVICE_USER", "FRONIUS_DEVICE_PASSWORD"},
		},
		{
			name:     "password only",
			env:      map[string]string{"FRONIUS_DEVICE_PASSWORD": "password"},
			wantSubs: []string{"FRONIUS_DEVICE_USER", "FRONIUS_DEVICE_PASSWORD"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearFroniusEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("an incomplete credential pair must be refused")
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not name %q", err, want)
				}
			}
		})
	}

	// Both set: accepted.
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER", "installer")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "password")
	if _, err := Load(); err != nil {
		t.Fatalf("a complete pair must be accepted: %v", err)
	}
}

// writeCredentialFile writes content to a fresh file and returns its path.
func writeCredentialFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestLoadCredentialsFromFiles: the _FILE variables are read and their trailing
// newline (Docker writes one, here also a CR) is stripped, nothing else.
func TestLoadCredentialsFromFiles(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER_FILE", writeCredentialFile(t, "user", "installer\n"))
	t.Setenv("FRONIUS_DEVICE_PASSWORD_FILE", writeCredentialFile(t, "password", "password\r\n"))

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DeviceUser != "installer" {
		t.Fatalf("DeviceUser = %q, want %q", got.DeviceUser, "installer")
	}
	if pw := got.DevicePassword.Reveal(); pw != "password" {
		t.Fatalf("DevicePassword = %q, want %q", pw, "password")
	}
}

// TestCredentialFilesWinOverInline: when both forms are set, the file decides —
// the Docker-secrets convention.
func TestCredentialFilesWinOverInline(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER", "inline-user")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "inline-password")
	t.Setenv("FRONIUS_DEVICE_USER_FILE", writeCredentialFile(t, "user", "file-user"))
	t.Setenv("FRONIUS_DEVICE_PASSWORD_FILE", writeCredentialFile(t, "password", "file-password"))

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DeviceUser != "file-user" {
		t.Fatalf("DeviceUser = %q, want the file value %q", got.DeviceUser, "file-user")
	}
	if pw := got.DevicePassword.Reveal(); pw != "file-password" {
		t.Fatalf("DevicePassword = %q, want the file value %q", pw, "file-password")
	}
}

// TestLoadRejectsUnreadableCredentialFile: a missing file is a startup error
// naming the variable and the path, never the other credential's value, and no
// half-built configuration is returned.
func TestLoadRejectsUnreadableCredentialFile(t *testing.T) {
	clearFroniusEnv(t)
	missing := filepath.Join(t.TempDir(), "absent")
	t.Setenv("FRONIUS_DEVICE_USER", "installer")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "very-secret-value-42")
	t.Setenv("FRONIUS_DEVICE_USER_FILE", missing)

	got, err := Load()
	if err == nil {
		t.Fatal("an unreadable credentials file must be refused")
	}
	for _, want := range []string{"FRONIUS_DEVICE_USER_FILE", missing} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "very-secret-value-42") {
		t.Fatalf("the error message leaked the password: %v", err)
	}
	if got != (Config{}) {
		t.Fatalf("Load returned a partially built configuration: %+v", got)
	}
}

// TestEmptyCredentialFilesMeanNoCredential: an empty file (Docker writes at
// least a newline) wins over the inline variable and means "no credential";
// the daemon then runs credential-less through the existing empty path.
func TestEmptyCredentialFilesMeanNoCredential(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER", "inline-user")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "inline-password")
	t.Setenv("FRONIUS_DEVICE_USER_FILE", writeCredentialFile(t, "user", "\n"))
	t.Setenv("FRONIUS_DEVICE_PASSWORD_FILE", writeCredentialFile(t, "password", "\r\n"))

	got, err := Load()
	if err != nil {
		t.Fatalf("credential-less configuration must be accepted: %v", err)
	}
	if got.DeviceUser != "" || !got.DevicePassword.IsZero() {
		t.Fatalf("empty files must yield no credential, got user %q and a %v password", got.DeviceUser, got.DevicePassword.IsZero())
	}
}

// TestCredentialsFromFilesStayMasked: a file-sourced password is masked by fmt
// and by JSON, like an inline one.
func TestCredentialsFromFilesStayMasked(t *testing.T) {
	const password = "file-secret-value-42"

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER_FILE", writeCredentialFile(t, "user", "installer\n"))
	t.Setenv("FRONIUS_DEVICE_PASSWORD_FILE", writeCredentialFile(t, "password", password+"\n"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out := fmt.Sprintf("%+v", cfg); strings.Contains(out, password) {
		t.Fatalf("fmt leaked the file-sourced password: %s", out)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), password) {
		t.Fatalf("JSON leaked the file-sourced password: %s", b)
	}
}

func TestDetectIntervalBounds(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "4s")
	_, err := Load()
	if err == nil {
		t.Fatal("a too-short detect interval must be refused")
	}
	if !strings.Contains(err.Error(), "FRONIUS_DETECT_INTERVAL") {
		t.Fatalf("error %q does not name the variable", err)
	}

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "every minute")
	_, err = Load()
	if err == nil {
		t.Fatal("an unreadable duration must be refused")
	}
	if !strings.Contains(err.Error(), "Go duration expected") {
		t.Fatalf("error %q does not explain the expected format", err)
	}

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "5s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("the exact lower bound must be accepted: %v", err)
	}
	if cfg.DetectInterval != 5*time.Second {
		t.Fatalf("detect interval = %v", cfg.DetectInterval)
	}
}

// TestErrorsNeverContainPassword: a configuration error message must never
// quote the password value, even when that value is at fault.
func TestErrorsNeverContainPassword(t *testing.T) {
	const fake = "very-secret-value-42"

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_PASSWORD", fake) // user absent → error
	t.Setenv("FRONIUS_DETECT_INTERVAL", "1s") // second fault, for a composite message

	_, err := Load()
	if err == nil {
		t.Fatal("Load should have failed")
	}
	if strings.Contains(err.Error(), fake) {
		t.Fatalf("the error message leaked the password: %v", err)
	}
	if !strings.Contains(err.Error(), "FRONIUS_DEVICE_PASSWORD") {
		t.Fatalf("the message must name the faulty variable: %v", err)
	}
}

func TestLocation(t *testing.T) {
	c := Config{TZ: "Europe/Brussels"}
	loc, err := c.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	// Winter time: UTC+1; summer time: UTC+2.
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, loc)
	if _, off := winter.Zone(); off != 3600 {
		t.Fatalf("winter offset = %d s, want 3600", off)
	}
	if _, off := summer.Zone(); off != 7200 {
		t.Fatalf("summer offset = %d s, want 7200", off)
	}

	if _, err := (Config{TZ: "Mars/Olympus"}).Location(); err == nil {
		t.Fatal("unknown timezone should have failed")
	}
}

// refusalMessageParts: elements expected in the non-local listen refusal message.
var refusalMessageParts = []string{"refusal", "FRONIUS_ALLOW_REMOTE", "0005"}

func TestCheckListenAddress(t *testing.T) {
	cases := []struct {
		name        string
		addr        string
		allowRemote bool
		wantSubs    []string // empty = no error expected
	}{
		{name: "loopback IPv4", addr: "127.0.0.1:8080"},
		{name: "loopback IPv6", addr: "[::1]:8080"},
		{name: "localhost", addr: "localhost:8080"},
		{name: "all interfaces", addr: ":8080", wantSubs: refusalMessageParts},
		{name: "0.0.0.0", addr: "0.0.0.0:8080", wantSubs: refusalMessageParts},
		{name: "inverter IP", addr: "192.0.2.10:8080", wantSubs: refusalMessageParts},
		{name: "remote allowed", addr: "192.0.2.10:8080", allowRemote: true},
		{name: "invalid address", addr: "not-an-address", wantSubs: []string{"FRONIUS_HTTP_ADDR", "missing port"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{HTTPAddr: tc.addr, AllowRemote: tc.allowRemote}
			err := c.CheckListenAddress()
			if len(tc.wantSubs) == 0 {
				if err != nil {
					t.Fatalf("CheckListenAddress refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("CheckListenAddress should have refused")
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("message %q does not mention %q", err, want)
				}
			}
		})
	}
}
