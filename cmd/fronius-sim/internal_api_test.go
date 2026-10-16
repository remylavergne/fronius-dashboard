package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// authOpts construit des options avec authentification Digest.
func authOpts() options {
	o := baseOpts()
	o.apiUser = "sim"
	o.apiPassword = "simpass"
	o.challengeRealm = "Webinterface area"
	return o
}

// simMD5Test recalcule indépendamment le digest attendu.
func simMD5Test(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// authHeader construit un en-tête Authorization valide pour le simulateur.
func authHeader(user, password, realm, nonce, method, uri string, nc int, cnonce string) string {
	qop := "auth"
	ha1 := simMD5Test(user + ":" + realm + ":" + password)
	ha2 := simMD5Test(method + ":" + uri)
	ncValue := fmt.Sprintf("%08d", nc)
	response := simMD5Test(ha1 + ":" + nonce + ":" + ncValue + ":" + cnonce + ":" + qop + ":" + ha2)
	return fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", qop=%s, nc=%s, cnonce="%s"`,
		user, realm, nonce, uri, response, qop, ncValue, cnonce)
}

// paramOf extrait une valeur d'un en-tête de défi.
func paramOf(header, key string) string {
	for _, part := range strings.Split(header, ", ") {
		k, v, ok := strings.Cut(strings.TrimPrefix(part, "Digest "), "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// TestInternalPathsWithoutAuth : sans --api-user, les quatre chemins répondent
// sans authentification — c'est le comportement de la phase 1 et celui du
// service d'état du client officiel (module yTJA).
func TestInternalPathsWithoutAuth(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	for _, path := range internalPaths {
		code, hdr, body := get(t, srv, path, nil)
		if code != http.StatusOK {
			t.Fatalf("%s : status = %d (%s)", path, code, body)
		}
		if hdr.Get(nonStandardChallengeHeader) != "" || hdr.Get(standardChallengeHeader) != "" {
			t.Fatalf("%s : aucun défi ne doit être émis", path)
		}
	}
}

func TestVersionBody(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	code, _, body := get(t, srv, versionPath, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var got versionJSON
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SerialNumber != simulatedSerial || got.DeviceGroup != "GEN24PRIMO" || got.NumberOfPhases != 3 {
		t.Fatalf("corps = %+v", got)
	}
	if got.ModelName == "" || got.FirmwareVersion == "" {
		t.Fatalf("les clés candidates doivent être présentes : %+v", got)
	}
	// Le numéro de série réel de l'installation ne doit jamais apparaître.
	if strings.Contains(string(body), "33319328") {
		t.Fatalf("numéro de série réel présent : %s", body)
	}
	// Déterminisme.
	_, _, again := get(t, srv, versionPath, nil)
	if string(again) != string(body) {
		t.Fatalf("corps non déterministe :\n%s\n%s", body, again)
	}
}

// TestComponentPresenceBodies : Body.Data vide signifie « composant absent ».
func TestComponentPresenceBodies(t *testing.T) {
	cases := []struct {
		name        string
		withMeter   bool
		withBattery bool
		wantMeter   bool
		wantBattery bool
	}{
		{name: "aucun composant"},
		{name: "compteur seul", withMeter: true, wantMeter: true},
		{name: "batterie seule", withBattery: true, wantBattery: true},
		{name: "les deux", withMeter: true, withBattery: true, wantMeter: true, wantBattery: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := baseOpts()
			opt.withMeter = tc.withMeter
			opt.withBattery = tc.withBattery
			sim := testSim(t, opt)
			srv := httptest.NewServer(sim.Handler())
			defer srv.Close()

			for path, want := range map[string]bool{powerMeterPath: tc.wantMeter, batteryPath: tc.wantBattery} {
				code, _, body := get(t, srv, path, nil)
				if code != http.StatusOK {
					t.Fatalf("%s : status = %d", path, code)
				}
				var env struct {
					Body struct {
						Data map[string]any `json:"Data"`
					} `json:"Body"`
				}
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("%s : Unmarshal: %v", path, err)
				}
				if env.Body.Data == nil {
					t.Fatalf("%s : Body.Data doit être un objet, pas null : %s", path, body)
				}
				if got := len(env.Body.Data) > 0; got != want {
					t.Fatalf("%s : présence = %v, attendue %v (%s)", path, got, want, body)
				}
			}
		})
	}
}

func TestActiveEventsBodies(t *testing.T) {
	cases := []struct {
		count int
	}{
		{count: 0}, {count: 1}, {count: 2},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d événements", tc.count), func(t *testing.T) {
			opt := baseOpts()
			opt.deviceEvents = tc.count
			sim := testSim(t, opt)
			srv := httptest.NewServer(sim.Handler())
			defer srv.Close()

			code, _, body := get(t, srv, activeEventsPath, nil)
			if code != http.StatusOK {
				t.Fatalf("status = %d", code)
			}
			var env struct {
				Body struct {
					Data map[string]deviceEventJSON `json:"Data"`
				} `json:"Body"`
			}
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(env.Body.Data) != tc.count {
				t.Fatalf("événements = %d, attendu %d (%s)", len(env.Body.Data), tc.count, body)
			}
			for i := range tc.count {
				ev, ok := env.Body.Data[fmt.Sprint(i)]
				if !ok {
					t.Fatalf("clé %d absente : %s", i, body)
				}
				if ev.Code != fmt.Sprintf("SIM-%d", i) || ev.State != "active" || ev.Message == "" {
					t.Fatalf("événement %d = %+v", i, ev)
				}
			}
		})
	}
}

func TestInternalPathsRequireDigest(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	for _, path := range internalPaths {
		code, hdr, _ := get(t, srv, path, nil)
		if code != http.StatusUnauthorized {
			t.Fatalf("%s : status = %d, attendu 401", path, code)
		}
		ch := hdr.Get(nonStandardChallengeHeader)
		if ch == "" {
			t.Fatalf("%s : défi absent de %s", path, nonStandardChallengeHeader)
		}
		if paramOf(ch, "realm") != "Webinterface area" {
			t.Fatalf("%s : realm = %q", path, paramOf(ch, "realm"))
		}
		if !strings.HasPrefix(paramOf(ch, "nonce"), "sim-nonce-") {
			t.Fatalf("%s : nonce = %q", path, paramOf(ch, "nonce"))
		}
		if paramOf(ch, "qop") != "auth" {
			t.Fatalf("%s : qop = %q", path, paramOf(ch, "qop"))
		}
		// L'en-tête standard n'est pas émis par défaut.
		if v := hdr.Get(standardChallengeHeader); v != "" {
			t.Fatalf("%s : %s ne doit pas être émis par défaut (%q)", path, standardChallengeHeader, v)
		}
	}
}

func TestDigestAccepted(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// Défi.
	code, hdr, _ := get(t, srv, versionPath, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, attendu 401", code)
	}
	challenge := hdr.Get(nonStandardChallengeHeader)
	nonce := paramOf(challenge, "nonce")

	auth := authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "0123456789abcdef")
	code, _, body := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s), attendu 200", code, body)
	}
	var got versionJSON
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SerialNumber != simulatedSerial {
		t.Fatalf("corps = %+v", got)
	}
}

func TestDigestRejectsReplayAndBadSecrets(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	_, hdr, _ := get(t, srv, versionPath, nil)
	nonce := paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")

	cases := []struct {
		name   string
		header string
	}{
		{"mot de passe erroné", authHeader("sim", "mauvais", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"utilisateur inconnu", authHeader("autre", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"realm différent", authHeader("sim", "simpass", "autre realm", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"nonce inconnu", authHeader("sim", "simpass", "Webinterface area", "sim-nonce-999", http.MethodGet, versionPath, 1, "aaaa")},
		{"uri incohérente", authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, "/autre/chemin", 1, "aaaa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": tc.header})
			if code != http.StatusUnauthorized {
				t.Fatalf("status = %d, attendu 401", code)
			}
		})
	}
}

// TestNonceCountMustIncrease : le simulateur rejoue la protection contre la
// répétition, ce qui exerce réellement le compteur du client.
func TestNonceCountMustIncrease(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	_, hdr, _ := get(t, srv, versionPath, nil)
	nonce := paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")

	auth := func(nc int) string {
		return authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, nc, "aaaaaaaaaaaaaaaa")
	}

	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(1)}); code != http.StatusOK {
		t.Fatalf("nc=1 : status = %d, attendu 200", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(1)}); code != http.StatusUnauthorized {
		t.Fatalf("nc=1 rejoué : status = %d, attendu 401", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(3)}); code != http.StatusOK {
		t.Fatalf("nc=3 : status = %d, attendu 200", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(2)}); code != http.StatusUnauthorized {
		t.Fatalf("nc=2 après nc=3 : status = %d, attendu 401", code)
	}
}

func TestStandardChallengeIsEmittedOnDemand(t *testing.T) {
	opt := authOpts()
	opt.emitStandardChallenge = true
	sim := testSim(t, opt)
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	code, hdr, _ := get(t, srv, versionPath, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d", code)
	}
	nonStandard := hdr.Get(nonStandardChallengeHeader)
	standard := hdr.Get(standardChallengeHeader)
	if nonStandard == "" || standard == "" {
		t.Fatalf("les deux en-têtes sont attendus : %q / %q", nonStandard, standard)
	}
	if nonStandard != standard {
		t.Fatalf("les deux défis doivent être identiques : %q / %q", nonStandard, standard)
	}
	if paramOf(standard, "nonce") == "" || paramOf(standard, "realm") != "Webinterface area" {
		t.Fatalf("défi standard incomplet : %q", standard)
	}
}

func TestInternalFaultModes(t *testing.T) {
	cases := []struct {
		name    string
		fault   string
		apiUser string
		check   func(t *testing.T, code int, hdr http.Header)
	}{
		{
			name: "401-no-challenge", fault: faultNoChallenge, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if hdr.Get(nonStandardChallengeHeader) != "" || hdr.Get(standardChallengeHeader) != "" {
					t.Fatalf("aucun défi ne doit être émis : %q / %q",
						hdr.Get(nonStandardChallengeHeader), hdr.Get(standardChallengeHeader))
				}
			},
		},
		{
			name: "401-standard-only", fault: faultStandardOnly, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if hdr.Get(nonStandardChallengeHeader) != "" {
					t.Fatalf("%s ne doit pas être émis", nonStandardChallengeHeader)
				}
				if hdr.Get(standardChallengeHeader) == "" {
					t.Fatalf("%s attendu", standardChallengeHeader)
				}
			},
		},
		{
			name: "401-no-qop", fault: faultNoQop, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				ch := hdr.Get(nonStandardChallengeHeader)
				if strings.Contains(ch, "qop") {
					t.Fatalf("défi sans qop attendu : %q", ch)
				}
				if paramOf(ch, "nonce") == "" {
					t.Fatalf("nonce manquant : %q", ch)
				}
			},
		},
		{
			name: "401-opaque", fault: faultOpaque, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if got := paramOf(hdr.Get(nonStandardChallengeHeader), "opaque"); got != "sim-opaque" {
					t.Fatalf("opaque = %q", got)
				}
			},
		},
		{
			// Le drapeau stale=true n'apparaît que sur la tentative
			// authentifiée : TestStaleNonceFaultRecovers le vérifie. Ici, la
			// requête nue reçoit le défi ordinaire.
			name: "401-stale-nonce", fault: faultStaleNonce, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if paramOf(hdr.Get(nonStandardChallengeHeader), "nonce") == "" {
					t.Fatalf("défi sans nonce : %q", hdr.Get(nonStandardChallengeHeader))
				}
			},
		},
		{
			name: "401-bad-digest", fault: faultBadDigest, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if hdr.Get(nonStandardChallengeHeader) == "" {
					t.Fatal("le défi doit accompagner le refus")
				}
			},
		},
		{
			name: "sans identifiants configurés, aucune panne", fault: faultBadDigest, apiUser: "",
			check: func(t *testing.T, code int, _ http.Header) {
				t.Helper()
				// Sans --api-user, l'authentification n'est pas exigée : la panne
				// Digest n'a pas de prise.
				if code != http.StatusOK {
					t.Fatalf("status = %d, attendu 200", code)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := baseOpts()
			opt.apiUser = tc.apiUser
			opt.apiPassword = "simpass"
			opt.challengeRealm = "Webinterface area"
			sim := testSim(t, opt)
			sim.opt.fault = tc.fault
			srv := httptest.NewServer(sim.Handler())
			defer srv.Close()

			code, hdr, _ := get(t, srv, versionPath, nil)
			tc.check(t, code, hdr)
		})
	}
}

// TestStaleNonceFaultRecovers : la première tentative authentifiée est refusée
// avec stale=true, la suivante est acceptée.
func TestStaleNonceFaultRecovers(t *testing.T) {
	opt := authOpts()
	sim := testSim(t, opt)
	sim.opt.fault = faultStaleNonce
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// Premier cycle : défi, puis refus stale.
	_, hdr, _ := get(t, srv, versionPath, nil)
	nonce := paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")
	auth := authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "cccc")
	code, hdr, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusUnauthorized {
		t.Fatalf("première tentative authentifiée : status = %d, attendu 401", code)
	}
	if ch := hdr.Get(nonStandardChallengeHeader); !strings.Contains(ch, "stale=true") {
		t.Fatalf("défi stale attendu après la première tentative authentifiée : %q", ch)
	}

	// Second cycle : nouveau défi, digest accepté.
	_, hdr, _ = get(t, srv, versionPath, nil)
	nonce = paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")
	auth = authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "dddd")
	code, _, body := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusOK {
		t.Fatalf("seconde tentative : status = %d (%s), attendu 200", code, body)
	}
}

func TestInternalPathMethodAndUnknownPath(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+versionPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s : status = %d, attendu 405", versionPath, resp.StatusCode)
	}

	// Un chemin interne qui n'est pas dans la liste blanche n'existe pas.
	for _, path := range []string{"/status/events", "/status/devices", "/components/DataManager/readable"} {
		if code, _, _ := get(t, srv, path, nil); code != http.StatusNotFound {
			t.Fatalf("%s : status = %d, attendu 404", path, code)
		}
	}
}

// TestInternalBodyIsDeterministicUnderConcurrentRequests : le corps ne porte
// jamais le nonce ; deux appels simultanés renvoient le même octet.
func TestInternalBodyIsDeterministicUnderConcurrentRequests(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	_, hdr, _ := get(t, srv, versionPath, nil)
	nonce := paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")

	const workers = 16
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		bodies []string
	)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			auth := authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, i+1, "eeee")
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+versionPath, nil)
			if err != nil {
				return
			}
			req.Header.Set("Authorization", auth)
			resp, err := srv.Client().Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode == http.StatusOK {
				mu.Lock()
				bodies = append(bodies, string(body))
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if len(bodies) == 0 {
		t.Fatal("aucune réponse valide")
	}
	for _, b := range bodies {
		if strings.Contains(b, "sim-nonce") {
			t.Fatalf("le nonce ne doit pas figurer dans le corps : %s", b)
		}
		if b != bodies[0] {
			t.Fatalf("corps non déterministe :\n%s\n%s", bodies[0], b)
		}
	}
}

// TestFaultModesListIsComplete : les modes annoncés par le drapeau doivent
// tous être injectables et reconnus.
func TestFaultModesListIsComplete(t *testing.T) {
	required := []string{
		faultNoChallenge, faultStandardOnly, faultNoQop, faultOpaque, faultStaleNonce, faultBadDigest,
	}
	for _, want := range required {
		if !containsString(faultModes, want) {
			t.Fatalf("%q absent de faultModes", want)
		}
	}
	if opt := baseOpts(); opt.apiUser != "" {
		t.Fatal("baseOpts ne doit pas exiger d'authentification")
	}
	// Un mode inconnu reste refusé.
	if err := run(options{tz: "Europe/Brussels", fault: "pas-une-panne"}); err == nil {
		t.Fatal("une panne inconnue doit être refusée")
	}
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
