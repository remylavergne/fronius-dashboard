package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

// clearFroniusEnv retire toutes les variables FRONIUS_* pour la durée du test.
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
		t.Fatalf("défauts incorrects\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_SOLARAPI_URL", "https://onduleur.local:443")
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
	t.Setenv("FRONIUS_DEVICE_USER", "installateur")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "mot-de-passe")
	t.Setenv("FRONIUS_DETECT_INTERVAL", "5m")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SolarAPIURL != "https://onduleur.local:443" || got.HTTPAddr != "0.0.0.0:9000" ||
		!got.AllowRemote || got.DBPath != "/tmp/x.db" || got.TZ != "UTC" ||
		got.PollInterval != 10*time.Second || got.PollJitter != 0 ||
		got.HTTPTimeout != 1500*time.Millisecond || got.RetentionDays != 365 ||
		got.LogLevel != "debug" || got.LogFormat != "json" || got.WebDir != "/srv/web" ||
		got.StaleAfter != 2*time.Minute ||
		got.DeviceUser != "installateur" || got.DevicePassword.Reveal() != "mot-de-passe" ||
		got.DetectInterval != 5*time.Minute {
		t.Fatalf("surcharges non appliquées : %+v", got)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantSub string
	}{
		{"intervalle trop court", map[string]string{"FRONIUS_POLL_INTERVAL": "500ms"}, "FRONIUS_POLL_INTERVAL"},
		{"intervalle non parsable", map[string]string{"FRONIUS_POLL_INTERVAL": "cinq secondes"}, "durée Go attendue"},
		{"jitter négatif", map[string]string{"FRONIUS_POLL_JITTER": "-1s"}, "FRONIUS_POLL_JITTER"},
		{"timeout nul", map[string]string{"FRONIUS_HTTP_TIMEOUT": "0s"}, "FRONIUS_HTTP_TIMEOUT"},
		{"rétention négative", map[string]string{"FRONIUS_RETENTION_DAYS": "-1"}, "FRONIUS_RETENTION_DAYS"},
		{"rétention non entière", map[string]string{"FRONIUS_RETENTION_DAYS": "un an"}, "entier attendu"},
		{"booléen invalide", map[string]string{"FRONIUS_ALLOW_REMOTE": "peut-être"}, "FRONIUS_ALLOW_REMOTE"},
		{"fuseau inconnu", map[string]string{"FRONIUS_TZ": "Mars/Olympus"}, "FRONIUS_TZ"},
		{"URL sans schéma", map[string]string{"FRONIUS_SOLARAPI_URL": "127.0.0.1:8081"}, "URL http(s) absolue"},
		{"URL schéma exotique", map[string]string{"FRONIUS_SOLARAPI_URL": "ftp://x/y"}, "URL http(s) absolue"},
		{"adresse sans port", map[string]string{"FRONIUS_HTTP_ADDR": "localhost"}, "hôte:port"},
		{"niveau de log inconnu", map[string]string{"FRONIUS_LOG_LEVEL": "verbose"}, "FRONIUS_LOG_LEVEL"},
		{"format de log inconnu", map[string]string{"FRONIUS_LOG_FORMAT": "xml"}, "FRONIUS_LOG_FORMAT"},
		{"stale nul", map[string]string{"FRONIUS_STALE_AFTER": "0s"}, "FRONIUS_STALE_AFTER"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearFroniusEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("Load devait échouer")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("erreur %q ne mentionne pas %q", err, tc.wantSub)
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
		t.Fatal("Load devait échouer")
	}
	for _, want := range []string{"FRONIUS_POLL_INTERVAL", "FRONIUS_LOG_FORMAT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erreur %q ne mentionne pas %q", err, want)
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
			name:     "utilisateur seul",
			env:      map[string]string{"FRONIUS_DEVICE_USER": "installateur"},
			wantSubs: []string{"FRONIUS_DEVICE_USER", "FRONIUS_DEVICE_PASSWORD"},
		},
		{
			name:     "mot de passe seul",
			env:      map[string]string{"FRONIUS_DEVICE_PASSWORD": "mot-de-passe"},
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
				t.Fatal("une paire d'identifiants incomplète doit être refusée")
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("erreur %q ne nomme pas %q", err, want)
				}
			}
		})
	}

	// Les deux renseignés : accepté.
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_USER", "installateur")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "mot-de-passe")
	if _, err := Load(); err != nil {
		t.Fatalf("une paire complète doit être acceptée : %v", err)
	}
}

func TestDetectIntervalBounds(t *testing.T) {
	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "4s")
	_, err := Load()
	if err == nil {
		t.Fatal("un intervalle de détection trop court doit être refusé")
	}
	if !strings.Contains(err.Error(), "FRONIUS_DETECT_INTERVAL") {
		t.Fatalf("erreur %q ne nomme pas la variable", err)
	}

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "chaque minute")
	_, err = Load()
	if err == nil {
		t.Fatal("une durée illisible doit être refusée")
	}
	if !strings.Contains(err.Error(), "durée Go attendue") {
		t.Fatalf("erreur %q n'explique pas le format attendu", err)
	}

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DETECT_INTERVAL", "5s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("la borne basse exacte doit être acceptée : %v", err)
	}
	if cfg.DetectInterval != 5*time.Second {
		t.Fatalf("detect interval = %v", cfg.DetectInterval)
	}
}

// TestErrorsNeverContainPassword : un message d'erreur de configuration ne doit
// jamais citer la valeur du mot de passe, même lorsque celle-ci est en cause.
func TestErrorsNeverContainPassword(t *testing.T) {
	const fake = "valeur-très-secrète-42"

	clearFroniusEnv(t)
	t.Setenv("FRONIUS_DEVICE_PASSWORD", fake) // utilisateur absent → erreur
	t.Setenv("FRONIUS_DETECT_INTERVAL", "1s") // seconde faute, pour un message composite

	_, err := Load()
	if err == nil {
		t.Fatal("Load devait échouer")
	}
	if strings.Contains(err.Error(), fake) {
		t.Fatalf("le message d'erreur a divulgué le mot de passe : %v", err)
	}
	if !strings.Contains(err.Error(), "FRONIUS_DEVICE_PASSWORD") {
		t.Fatalf("le message doit nommer la variable fautive : %v", err)
	}
}

func TestLocation(t *testing.T) {
	c := Config{TZ: "Europe/Brussels"}
	loc, err := c.Location()
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	// Heure d'hiver : UTC+1 ; heure d'été : UTC+2.
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, loc)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, loc)
	if _, off := winter.Zone(); off != 3600 {
		t.Fatalf("décalage hiver = %d s, attendu 3600", off)
	}
	if _, off := summer.Zone(); off != 7200 {
		t.Fatalf("décalage été = %d s, attendu 7200", off)
	}

	if _, err := (Config{TZ: "Mars/Olympus"}).Location(); err == nil {
		t.Fatal("fuseau inconnu devait échouer")
	}
}

// refusalMessageParts : éléments attendus dans le message de refus d'écoute non locale.
var refusalMessageParts = []string{"refus", "FRONIUS_ALLOW_REMOTE", "0005"}

func TestCheckListenAddress(t *testing.T) {
	cases := []struct {
		name        string
		addr        string
		allowRemote bool
		wantSubs    []string // vide = aucune erreur attendue
	}{
		{name: "loopback IPv4", addr: "127.0.0.1:8080"},
		{name: "loopback IPv6", addr: "[::1]:8080"},
		{name: "localhost", addr: "localhost:8080"},
		{name: "toutes interfaces", addr: ":8080", wantSubs: refusalMessageParts},
		{name: "0.0.0.0", addr: "0.0.0.0:8080", wantSubs: refusalMessageParts},
		{name: "IP du onduleur", addr: "192.0.2.10:8080", wantSubs: refusalMessageParts},
		{name: "distant autorisé", addr: "192.0.2.10:8080", allowRemote: true},
		{name: "adresse invalide", addr: "pas-une-adresse", wantSubs: []string{"FRONIUS_HTTP_ADDR", "missing port"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{HTTPAddr: tc.addr, AllowRemote: tc.allowRemote}
			err := c.CheckListenAddress()
			if len(tc.wantSubs) == 0 {
				if err != nil {
					t.Fatalf("CheckListenAddress a refusé : %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("CheckListenAddress devait refuser")
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("message %q ne mentionne pas %q", err, want)
				}
			}
		})
	}
}
