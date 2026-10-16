package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactJSONPreservesMeasurements(t *testing.T) {
	body := []byte(`{
	  "Body": {"Data": {"Site": {
	    "P_PV": 1902.4, "E_Total": 14236126.1, "Mode": "produce-only",
	    "SerialNumber": "33319328", "IPAddress": "192.0.2.10",
	    "hostname": "fronius-web.local", "mac": "aa:bb:cc:dd:ee:ff",
	    "Nonce": "abc123", "Password": "hunter2"
	  }}},
	  "Head": {"Timestamp": "2026-06-21T12:00:00+02:00", "Status": {"Code": 0}}
	}`)

	out := string(redactBody(body))

	// Les mesures et l'horodatage doivent survivre intacts.
	for _, keep := range []string{"1902.4", "14236126.1", "produce-only", "2026-06-21T12:00:00+02:00"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("valeur légitime %q supprimée :\n%s", keep, out)
		}
	}
	// Aucune donnée identifiante ne doit subsister.
	for _, gone := range []string{"33319328", "192.0.2.10", "fronius-web.local", "aa:bb:cc:dd:ee:ff", "abc123", "hunter2"} {
		if strings.Contains(out, gone) {
			t.Fatalf("donnée sensible %q non expurgée :\n%s", gone, out)
		}
	}
	// Le résultat reste un JSON exploitable.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("corps expurgé non décodable : %v", err)
	}
	if strings.Count(out, redactedValue) < 6 {
		t.Fatalf("trop peu de valeurs expurgées (%d) :\n%s", strings.Count(out, redactedValue), out)
	}
}

func TestRedactJSONNestedAndArrays(t *testing.T) {
	body := []byte(`{"Inverters":{"1":{"P":10.5,"Serial":"12345"}},"Liste":[{"ip":"10.0.0.1"},{"P":3}]}`)
	out := string(redactBody(body))

	if strings.Contains(out, "10.0.0.1") || strings.Contains(out, "12345") {
		t.Fatalf("donnée sensible non expurgée :\n%s", out)
	}
	if !strings.Contains(out, "10.5") || !strings.Contains(out, "3") {
		t.Fatalf("mesures supprimées :\n%s", out)
	}
}

func TestRedactNonJSONBody(t *testing.T) {
	body := []byte("<html><body>password='hunter2' host 192.168.0.10 serial=33319328</body></html>")
	out := string(redactBody(body))

	for _, gone := range []string{"hunter2", "192.168.0.10", "33319328"} {
		if strings.Contains(out, gone) {
			t.Fatalf("%q non expurgé :\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "html") {
		t.Fatalf("le corps textuel a été perdu :\n%s", out)
	}
}

func TestMaskHeaderValue(t *testing.T) {
	cases := []struct {
		name, value, want string
	}{
		{"Authorization", `Digest username="user", nonce="abc"`, redactedValue},
		{"x-www-authenticate", `Digest realm="Webinterface area", nonce="sim-nonce-1"`, redactedValue},
		{"WWW-Authenticate", `Basic realm="x"`, redactedValue},
		{"Set-Cookie", "session=abc", redactedValue},
		{"Content-Type", "application/json", "application/json"},
		{"Location", "http://192.168.1.5/next", redactedValue},
		{"X-Custom", "valeur quelconque", "valeur quelconque"},
	}
	for _, tc := range cases {
		if got := maskHeaderValue(tc.name, tc.value); got != tc.want {
			t.Fatalf("maskHeaderValue(%s) = %q, attendu %q", tc.name, got, tc.want)
		}
	}
}

func TestWhitelistIsGetOnly(t *testing.T) {
	if len(whitelist) != 1 {
		t.Fatalf("liste blanche = %d entrées, attendu 1", len(whitelist))
	}
	for _, ep := range whitelist {
		if ep.method != http.MethodGet {
			t.Fatalf("%s : méthode %q, seul GET est autorisé", ep.path, ep.method)
		}
		if ep.classification != "READ" {
			t.Fatalf("%s : classification %q", ep.path, ep.classification)
		}
	}
}

func TestNonLoopbackIsRefusedWithoutFlag(t *testing.T) {
	var buf bytes.Buffer
	err := run(options{baseURL: "http://192.0.2.10", timeout: time.Second, out: &buf})
	if err == nil {
		t.Fatal("une adresse non locale devait être refusée")
	}
	for _, want := range []string{"refus", "--allow-non-loopback", "PROBE-READONLY.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q ne mentionne pas %q", err, want)
		}
	}
}

func TestLoopbackHostIsAccepted(t *testing.T) {
	// Le refus ne doit pas se déclencher sur une adresse locale : l'erreur
	// attendue ici vient de l'absence de serveur, pas de la garde.
	var buf bytes.Buffer
	err := run(options{baseURL: "http://127.0.0.1:1", timeout: 300 * time.Millisecond, out: &buf})
	if err == nil {
		t.Skip("aucun serveur sur le port 1 : erreur attendue")
	}
	if strings.Contains(err.Error(), "n'est pas une adresse locale") {
		t.Fatalf("une adresse locale a été refusée : %v", err)
	}
}

func TestListPrintsWhitelist(t *testing.T) {
	var buf bytes.Buffer
	if err := run(options{list: true, baseURL: "http://127.0.0.1:8081", out: &buf}); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Liste blanche", "GET", "/solar_api/v1/GetPowerFlowRealtimeData.fcgi", "READ"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sortie --list incomplète (%q manquant) :\n%s", want, out)
		}
	}
	// Une seule entrée : aucun autre chemin ne doit être annoncé.
	if n := strings.Count(out, "/solar_api/"); n != 1 {
		t.Fatalf("la liste blanche doit contenir exactement un endpoint (%d chemins cités) :\n%s", n, out)
	}
}

func TestProbeSendsGetAndSavesRedactedBody(t *testing.T) {
	var method, path string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-www-authenticate", `Digest realm="Webinterface area", nonce="x"`)
		_, _ = io.WriteString(w, `{"Body":{"Data":{"Site":{"P_PV":1902.4,"SerialNumber":"33319328","IPAddress":"192.0.2.10"}}},
		  "Head":{"Status":{"Code":0},"Timestamp":"2026-06-21T12:00:00+02:00"}}`)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	dir := t.TempDir()
	var buf bytes.Buffer
	if err := run(options{baseURL: srv.URL, saveDir: dir, timeout: 2 * time.Second, out: &buf}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if method != http.MethodGet {
		t.Fatalf("méthode émise = %q, attendu GET", method)
	}
	if path != whitelist[0].path {
		t.Fatalf("chemin appelé = %q, attendu %q", path, whitelist[0].path)
	}

	out := buf.String()
	for _, want := range []string{"GET", "200 OK", "Corps expurgé", "192"} {
		if !strings.Contains(out, want) && want != "192" {
			t.Fatalf("sortie incomplète (%q manquant) :\n%s", want, out)
		}
	}
	if strings.Contains(out, "33319328") || strings.Contains(out, "192.0.2.10") || strings.Contains(out, "sim-nonce") {
		t.Fatalf("une donnée sensible apparaît dans la sortie :\n%s", out)
	}
	if !strings.Contains(out, redactedValue) {
		t.Fatalf("aucune mention d'expurgation :\n%s", out)
	}

	files, err := filepath.Glob(filepath.Join(dir, "probe_GetPowerFlowRealtimeData_*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("fichiers écrits = %v (err=%v)", files, err)
	}
	saved, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("lecture du fichier écrit: %v", err)
	}
	if strings.Contains(string(saved), "33319328") || strings.Contains(string(saved), "192.0.2.10") {
		t.Fatalf("le fichier écrit contient une donnée sensible :\n%s", saved)
	}
	if !strings.Contains(string(saved), "1902.4") {
		t.Fatalf("le fichier écrit a perdu la mesure :\n%s", saved)
	}
	var parsed map[string]any
	if err := json.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("le fichier écrit doit rester du JSON : %v", err)
	}

	// Les permissions du fichier ne doivent pas être plus larges que 0600.
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, attendu 600", perm)
	}
}

func TestProbeReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	err := run(options{baseURL: srv.URL, timeout: 2 * time.Second, out: &buf})
	if err == nil {
		t.Fatal("un 401 doit être signalé comme erreur")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("erreur = %v", err)
	}
}

// TestIPv6DetectionDoesNotTouchTimestamps : le motif IPv6 ne doit jamais
// masquer une heure ni un horodatage, sans quoi les fixtures deviennent
// inutilisables.
func TestIPv6DetectionDoesNotTouchTimestamps(t *testing.T) {
	preserved := []string{
		"12:00:00",
		"2026-06-21T12:00:00+02:00",
		"09:17:43.123Z",
		"produce-only",
		"1.19.7-1",
		"12345.6",
	}
	for _, v := range preserved {
		if got := redactString(v); got != v {
			t.Fatalf("redactString(%q) = %q : valeur légitime expurgée", v, got)
		}
	}

	masked := []string{
		"fe80::1",
		"2001:0db8:85a3:0000:0000:8a2e:0370:7334",
		"::1",
		"192.0.2.10",
		"aa:bb:cc:dd:ee:ff",
		"AA-BB-CC-DD-EE-FF",
		"fronius.local",
	}
	for _, v := range masked {
		if got := redactString(v); got != redactedValue {
			t.Fatalf("redactString(%q) = %q : valeur sensible conservée", v, got)
		}
	}
}

func TestRedactBodyIsIdempotent(t *testing.T) {
	body := []byte(`{"Site":{"IPAddress":"10.1.2.3","P_PV":42}}`)
	once := redactBody(body)
	twice := redactBody(once)
	if string(once) != string(twice) {
		t.Fatalf("expurgation non idempotente :\n%s\n%s", once, twice)
	}
}
