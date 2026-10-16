package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if len(whitelist) != 5 {
		t.Fatalf("liste blanche = %d entrées, attendu 5", len(whitelist))
	}
	seen := map[string]bool{}
	for _, ep := range whitelist {
		if ep.method != http.MethodGet {
			t.Fatalf("%s : méthode %q, seul GET est autorisé", ep.path, ep.method)
		}
		if ep.classification != "READ" {
			t.Fatalf("%s : classification %q", ep.path, ep.classification)
		}
		if ep.note == "" {
			t.Fatalf("%s : la note est obligatoire", ep.path)
		}
		if seen[ep.path] {
			t.Fatalf("%s : chemin en double", ep.path)
		}
		seen[ep.path] = true
	}

	// Les quatre chemins internes exigent un Digest côté client officiel ;
	// l'endpoint Solar API n'envoie jamais d'en-tête d'authentification.
	for _, ep := range whitelist {
		wantDigest := ep.path != "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"
		if ep.digest != wantDigest {
			t.Fatalf("%s : digest = %v, attendu %v", ep.path, ep.digest, wantDigest)
		}
	}

	// Les noms de fichiers dérivés des chemins doivent être distincts : deux
	// chemins internes se terminent tous deux par « readable ».
	names := map[string]bool{}
	for _, ep := range whitelist {
		name := saveName(ep.path)
		if names[name] {
			t.Fatalf("%s : nom de fichier en collision (%s)", ep.path, name)
		}
		names[name] = true
	}
	if got := saveName("/components/PowerMeter/readable"); got != "components_powermeter_readable" {
		t.Fatalf("saveName = %q", got)
	}
	if got := saveName("/status/version"); got != "status_version" {
		t.Fatalf("saveName = %q", got)
	}
}

func TestEndpointSelection(t *testing.T) {
	// « all » et la chaîne vide sélectionnent toute la liste.
	for _, name := range []string{"all", ""} {
		got, err := selectEndpoints(name)
		if err != nil {
			t.Fatalf("selectEndpoints(%q): %v", name, err)
		}
		if len(got) != len(whitelist) {
			t.Fatalf("selectEndpoints(%q) = %d entrées", name, len(got))
		}
	}

	got, err := selectEndpoints("/status/version")
	if err != nil {
		t.Fatalf("selectEndpoints: %v", err)
	}
	if len(got) != 1 || got[0].path != "/status/version" {
		t.Fatalf("sélection = %+v", got)
	}

	// Tout chemin hors liste est refusé.
	for _, name := range []string{"/config/modbus", "/status/events", "http://192.168.1.5/status/version", "version"} {
		if _, err := selectEndpoints(name); err == nil {
			t.Fatalf("%q devait être refusé", name)
		} else if !errors.Is(err, errNotWhitelisted) {
			t.Fatalf("%q : erreur = %v", name, err)
		}
	}
}

// TestCredentialsComeFromEnvironment : aucun mot de passe ne passe par un
// argument de ligne de commande.
func TestCredentialsComeFromEnvironment(t *testing.T) {
	t.Setenv("FRONIUS_DEVICE_USER", "installateur")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "depuis-l-environnement")

	cred, err := readCredentials(options{})
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if cred.user != "installateur" || cred.password.Reveal() != "depuis-l-environnement" {
		t.Fatalf("identifiants = %+v", cred)
	}

	// --password-stdin remplace le mot de passe de l'environnement.
	cred, err = readCredentials(options{passwordStdin: true, stdin: strings.NewReader("depuis-stdin\nsuite\n")})
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if cred.password.Reveal() != "depuis-stdin" {
		t.Fatalf("mot de passe = %q", cred.password.Reveal())
	}

	// Une paire incomplète est refusée.
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "")
	if _, err := readCredentials(options{}); err == nil {
		t.Fatal("un utilisateur sans mot de passe doit être refusé")
	}
}

// TestProbeSendsNoCredentialsPreemptively : l'en-tête Authorization n'est émis
// qu'en réponse à un 401, et il n'apparaît jamais dans la sortie ni dans le
// fichier écrit.
func TestProbeSendsNoCredentialsPreemptively(t *testing.T) {
	const password = "mot-de-passe-de-sonde"

	var (
		mu          sync.Mutex
		seen        []string // en-têtes Authorization reçus, dans l'ordre
		pathsSeen   []string // chemins reçus, dans le même ordre
		hitsPerPath = map[string]int{}
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		pathsSeen = append(pathsSeen, r.URL.Path)
		hitsPerPath[r.URL.Path]++
		mu.Unlock()

		// Seuls les chemins internes exigent un Digest ; l'endpoint Solar API
		// répond sans en-tête (deux voies distinctes, voir docs/SOURCES.md).
		if r.Header.Get("Authorization") == "" && r.URL.Path != "/solar_api/v1/GetPowerFlowRealtimeData.fcgi" {
			w.Header().Set("x-www-authenticate", `Digest realm="Webinterface area", nonce="sim-nonce-1", qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "Unauthorized")
			return
		}
		_, _ = io.WriteString(w, `{"Body":{"Data":{"0":{"Serial":"SIM"}}}`)
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	t.Setenv("FRONIUS_DEVICE_USER", "sim")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", password)

	dir := t.TempDir()
	var buf bytes.Buffer
	if err := run(options{baseURL: srv.URL, saveDir: dir, timeout: 2 * time.Second, out: &buf}); err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Chemin Solar API : une seule requête, sans en-tête.
	if hitsPerPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"] != 1 {
		t.Fatalf("solar api : %d requêtes, attendu 1", hitsPerPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"])
	}
	// Premier chemin interne : requête nue (401), puis requête authentifiée.
	// Les suivants réutilisent le nonce mis en cache : une seule requête chacun.
	if hitsPerPath["/status/version"] != 2 {
		t.Fatalf("version : %d requêtes, attendu 2 (nue puis authentifiée)", hitsPerPath["/status/version"])
	}
	for _, ep := range whitelist {
		if ep.path == "/status/version" || !ep.digest {
			continue
		}
		if hitsPerPath[ep.path] != 1 {
			t.Fatalf("%s : %d requêtes, attendu 1 (nonce en cache)", ep.path, hitsPerPath[ep.path])
		}
	}

	// Aucune authentification n'est envoyée avant un 401 : la seule requête nue
	// vers un chemin interne est la première, et les suivantes portent le Digest
	// issu du défi déjà reçu.
	byPath := map[string][]string{}
	for i, p := range pathsSeen {
		byPath[p] = append(byPath[p], seen[i])
	}
	if got := byPath["/status/version"]; len(got) != 2 || got[0] != "" || !strings.HasPrefix(got[1], "Digest ") {
		t.Fatalf("version : en-têtes = %q", got)
	}
	if got := byPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"]; len(got) != 1 || got[0] != "" {
		t.Fatalf("solar api : en-têtes = %q", got)
	}
	unauthenticated := 0
	for _, header := range seen {
		if header == "" {
			unauthenticated++
		}
	}
	if unauthenticated != 2 {
		t.Fatalf("requêtes sans en-tête = %d, attendu 2 (solar api + premier défi)", unauthenticated)
	}
	for _, header := range seen {
		if header != "" && !strings.HasPrefix(header, "Digest ") {
			t.Fatalf("en-tête inattendu : %q", header)
		}
		if strings.Contains(header, password) {
			t.Fatalf("le mot de passe apparaît dans l'en-tête : %q", header)
		}
	}

	// Le mot de passe n'apparaît ni dans la sortie, ni dans les fichiers écrits.
	if strings.Contains(buf.String(), password) {
		t.Fatalf("le mot de passe apparaît dans la sortie :\n%s", buf.String())
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != len(whitelist) {
		t.Fatalf("fichiers écrits = %v (err=%v), attendu %d", files, err, len(whitelist))
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("lecture de %s: %v", f, err)
		}
		if strings.Contains(string(body), password) {
			t.Fatalf("%s contient le mot de passe", f)
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
	// Un seul endpoint Solar API, et les quatre chemins internes.
	if n := strings.Count(out, "/solar_api/"); n != 1 {
		t.Fatalf("la liste blanche doit citer un seul chemin Solar API (%d) :\n%s", n, out)
	}
	for _, want := range []string{"/status/version", "/status/activeEvents", "/components/PowerMeter/readable",
		"/components/BatteryManagementSystem/readable", "digest=true", "FRONIUS_DEVICE_USER", "--password-stdin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sortie --list incomplète (%q manquant) :\n%s", want, out)
		}
	}
}

func TestProbeSendsGetAndSavesRedactedBody(t *testing.T) {
	var (
		mu      sync.Mutex
		methods []string
		paths   []string
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
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

	if len(paths) != len(whitelist) {
		t.Fatalf("requêtes émises = %d, attendu %d (une par endpoint)", len(paths), len(whitelist))
	}
	for i, ep := range whitelist {
		if methods[i] != http.MethodGet {
			t.Fatalf("%s : méthode émise = %q, attendu GET", ep.path, methods[i])
		}
		if paths[i] != ep.path {
			t.Fatalf("chemin appelé = %q, attendu %q", paths[i], ep.path)
		}
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

	files, err := filepath.Glob(filepath.Join(dir, "probe_solar_api_v1_getpowerflowrealtimedata_*.json"))
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
