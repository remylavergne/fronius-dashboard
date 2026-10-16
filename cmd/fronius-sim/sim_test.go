package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testSim(t *testing.T, opt options) *simulator {
	t.Helper()
	loc, err := time.LoadLocation(opt.tz)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", opt.tz, err)
	}
	if opt.logLevel == "" {
		opt.logLevel = "error"
	}
	return newSimulator(opt, loc, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func baseOpts() options {
	return options{seed: 42, nominalW: 3680, tz: "Europe/Brussels"}
}

func get(t *testing.T, srv *httptest.Server, path string, headers map[string]string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, resp.Header, body
}

func TestDeterminism(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// Deux appels séparés dans le temps doivent produire des corps différents
	// (la courbe avance), donc l'instant est fixé par ?at=.
	at := "?at=2026-06-21T12:00:00%2B02:00"
	_, _, first := get(t, srv, powerFlowPath+at, nil)
	time.Sleep(10 * time.Millisecond)
	_, _, second := get(t, srv, powerFlowPath+at, nil)

	if string(first) != string(second) {
		t.Fatalf("corps non déterministe :\n%s\n---\n%s", first, second)
	}

	// Une seconde graine doit produire un corps différent.
	other := testSim(t, options{seed: 43, nominalW: 3680, tz: "Europe/Brussels"})
	osrv := httptest.NewServer(other.Handler())
	defer osrv.Close()
	_, _, third := get(t, osrv, powerFlowPath+at, nil)
	if string(first) == string(third) {
		t.Fatal("seed différent mais corps identique")
	}
}

func TestDeterminismAcrossInstances(t *testing.T) {
	at := "?at=2026-03-29T03:30:00%2B01:00"
	a := testSim(t, baseOpts())
	b := testSim(t, baseOpts())
	sa := httptest.NewServer(a.Handler())
	defer sa.Close()
	sb := httptest.NewServer(b.Handler())
	defer sb.Close()

	_, _, ba := get(t, sa, powerFlowPath+at, nil)
	_, _, bb := get(t, sb, powerFlowPath+at, nil)
	if string(ba) != string(bb) {
		t.Fatal("deux instances identiques produisent des corps différents")
	}
}

func TestEnvelopeShape(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	code, hdr, body := get(t, srv, powerFlowPath+"?at=2026-06-21T12:00:00%2B02:00", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if ct := hdr.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if env.Head.Status.Code != 0 || env.Head.Status.Reason != "" {
		t.Fatalf("Head.Status inattendu : %+v", env.Head.Status)
	}
	if env.Head.Timestamp != "2026-06-21T12:00:00+02:00" {
		t.Fatalf("Head.Timestamp = %q", env.Head.Timestamp)
	}
	if env.Head.RequestArguments == nil {
		t.Fatal("RequestArguments doit être un objet vide, pas null")
	}
	if len(env.Body.Data.Inverters) != 1 {
		t.Fatalf("Inverters = %v", env.Body.Data.Inverters)
	}
	site := env.Body.Data.Site
	if site.Mode != "produce-only" {
		t.Fatalf("Mode = %q", site.Mode)
	}
	if site.PPV == nil || *site.PPV <= 0 {
		t.Fatalf("P_PV = %v (production attendue > 0 à midi solaire)", site.PPV)
	}
	if *site.PPV > 3680 {
		t.Fatalf("P_PV = %v dépasse la puissance nominale", *site.PPV)
	}
	// Sans Smart Meter ni batterie : null, jamais 0.
	for name, v := range map[string]*float64{
		"P_Grid": site.PGrid, "P_Load": site.PLoad, "P_Akku": site.PAkku,
		"rel_Autonomy": site.RelAutonomy, "rel_SelfConsumption": site.RelSelfConsumption,
	} {
		if v != nil {
			t.Fatalf("%s = %v, attendu null", name, *v)
		}
	}
	if site.MeterLocationCurrent == nil || *site.MeterLocationCurrent != 0 {
		t.Fatalf("Meter_Location_Current = %v", site.MeterLocationCurrent)
	}
	if site.EDay == nil || *site.EDay <= 0 || site.EYear == nil || site.ETotal == nil {
		t.Fatalf("compteurs d'énergie manquants : %+v", site)
	}
	if *site.EYear < *site.EDay {
		t.Fatalf("E_Year (%v) < E_Day (%v)", *site.EYear, *site.EDay)
	}
	if *site.ETotal < *site.EYear {
		t.Fatalf("E_Total (%v) < E_Year (%v)", *site.ETotal, *site.EYear)
	}

	// Le JSON doit contenir explicitement les noms de champs supposés.
	for _, want := range []string{`"P_PV"`, `"E_Day"`, `"E_Year"`, `"E_Total"`, `"rel_Autonomy"`, `"Meter_Location_Current"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("champ %s absent du corps", want)
		}
	}
}

func TestNightProducesZero(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// 02:00 locales : hors plage de production.
	_, _, body := get(t, srv, powerFlowPath+"?at=2026-06-21T02:00:00%2B02:00", nil)
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if env.Body.Data.Site.PPV == nil || *env.Body.Data.Site.PPV != 0 {
		t.Fatalf("P_PV nocturne = %v, attendu 0", env.Body.Data.Site.PPV)
	}
	if env.Body.Data.Site.EDay == nil || *env.Body.Data.Site.EDay != 0 {
		t.Fatalf("E_Day à 02:00 = %v, attendu 0", env.Body.Data.Site.EDay)
	}
}

func TestWithMeterCoherence(t *testing.T) {
	opt := baseOpts()
	opt.withMeter = true
	sim := testSim(t, opt)
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	for _, at := range []string{
		"2026-06-21T12:00:00%2B02:00",
		"2026-06-21T20:00:00%2B02:00", // production nulle, consommation seule
		"2026-01-15T10:00:00%2B01:00",
	} {
		_, _, body := get(t, srv, powerFlowPath+"?at="+at, nil)
		var env envelope
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("Unmarshal(%s): %v", at, err)
		}
		site := env.Body.Data.Site
		if site.PGrid == nil || site.PLoad == nil {
			t.Fatalf("at=%s : P_Grid/P_Load doivent être renseignés", at)
		}
		if site.PAkku != nil {
			t.Fatalf("at=%s : P_Akku doit rester null (aucune batterie modélisée)", at)
		}
		if got, want := *site.PGrid, round1(*site.PPV-*site.PLoad); got != want {
			t.Fatalf("at=%s : P_Grid = %v, attendu P_PV−P_Load = %v", at, got, want)
		}
		// Un ratio n'est émis que si son dénominateur est non nul ; jamais 0 inventé.
		if (*site.PPV > 0) != (site.RelSelfConsumption != nil) {
			t.Fatalf("at=%s : rel_SelfConsumption = %v incohérent avec P_PV = %v", at, site.RelSelfConsumption, *site.PPV)
		}
		if (*site.PLoad > 0) != (site.RelAutonomy != nil) {
			t.Fatalf("at=%s : rel_Autonomy = %v incohérent avec P_Load = %v", at, site.RelAutonomy, *site.PLoad)
		}
		for name, v := range map[string]*float64{"rel_Autonomy": site.RelAutonomy, "rel_SelfConsumption": site.RelSelfConsumption} {
			if v != nil && (*v < 0 || *v > 100) {
				t.Fatalf("at=%s : %s = %v hors [0,100]", at, name, *v)
			}
		}
	}
}

func TestFaultModes(t *testing.T) {
	fault := ""
	sim := testSim(t, baseOpts())
	sim.opt.fault = fault
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()
	defer srv.CloseClientConnections()

	cases := []struct {
		name       string
		fault      string
		wantStatus int
		check      func(t *testing.T, hdr http.Header, body []byte)
	}{
		{name: "500", fault: "500", wantStatus: http.StatusInternalServerError},
		{name: "503", fault: "503", wantStatus: http.StatusServiceUnavailable},
		{name: "404", fault: "404", wantStatus: http.StatusNotFound},
		{
			name: "401", fault: "401", wantStatus: http.StatusUnauthorized,
			check: func(t *testing.T, hdr http.Header, _ []byte) {
				t.Helper()
				challenge := hdr.Get("x-www-authenticate")
				if !strings.Contains(challenge, `realm="Webinterface area"`) ||
					!strings.Contains(challenge, `nonce="sim-nonce-1"`) ||
					!strings.Contains(challenge, `qop="auth"`) {
					t.Fatalf("défi Digest inattendu : %q", challenge)
				}
				if v := hdr.Get("WWW-Authenticate"); v != "" {
					t.Fatalf("le simulateur doit utiliser l'en-tête non standard x-www-authenticate, reçu aussi %q", v)
				}
			},
		},
		{
			name: "empty", fault: "empty", wantStatus: http.StatusOK,
			check: func(t *testing.T, _ http.Header, body []byte) {
				t.Helper()
				if len(body) != 0 {
					t.Fatalf("corps attendu vide, reçu %q", body)
				}
			},
		},
		{
			name: "invalid_json", fault: "invalid_json", wantStatus: http.StatusOK,
			check: func(t *testing.T, _ http.Header, body []byte) {
				t.Helper()
				var env envelope
				if err := json.Unmarshal(body, &env); err == nil {
					t.Fatal("le corps tronqué doit échouer au décodage JSON")
				}
			},
		},
		{
			name: "status7", fault: "status7", wantStatus: http.StatusOK,
			check: func(t *testing.T, _ http.Header, body []byte) {
				t.Helper()
				var env envelope
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("Unmarshal: %v", err)
				}
				if env.Head.Status.Code != 7 || env.Head.Status.Reason != "Simulated device error" {
					t.Fatalf("Head.Status = %+v", env.Head.Status)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, hdr, body := get(t, srv, powerFlowPath, map[string]string{"X-Sim-Fault": tc.fault})
			if code != tc.wantStatus {
				t.Fatalf("status = %d, attendu %d", code, tc.wantStatus)
			}
			if tc.check != nil {
				tc.check(t, hdr, body)
			}
		})
	}
}

func TestFaultFromFlag(t *testing.T) {
	opt := baseOpts()
	opt.fault = "503"
	sim := testSim(t, opt)
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	if code, _, _ := get(t, srv, powerFlowPath, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, attendu 503", code)
	}
	// L'en-tête doit pouvoir neutraliser la panne globale.
	sim.opt.fault = ""
	if code, _, _ := get(t, srv, powerFlowPath, map[string]string{"X-Sim-Fault": "0"}); code != http.StatusOK {
		t.Fatalf("status = %d, attendu 200 (panne non reconnue, ignorée)", code)
	}
}

func TestTimeoutFault(t *testing.T) {
	sim := testSim(t, baseOpts())
	sim.faultTimeout = 30 * time.Millisecond
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, srv.URL+powerFlowPath, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Sim-Fault", "timeout")
	if _, err := client.Do(req); err == nil {
		t.Fatal("le client devait expirer")
	}
}

func TestMethodAndPathHandling(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+powerFlowPath, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, attendu 405", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q", allow)
	}

	if code, _, _ := get(t, srv, "/inconnu", nil); code != http.StatusNotFound {
		t.Fatalf("chemin inconnu status = %d, attendu 404", code)
	}
}

func TestInvalidAtParameter(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	code, _, body := get(t, srv, powerFlowPath+"?at=hier", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, attendu 400", code)
	}
	if !strings.Contains(string(body), "RFC3339") {
		t.Fatalf("message inattendu : %s", body)
	}
}

func TestHealthEndpoint(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	for range 3 {
		get(t, srv, powerFlowPath, nil)
	}
	code, _, body := get(t, srv, "/sim/health", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var health struct {
		OK       bool   `json:"ok"`
		Seed     uint64 `json:"seed"`
		Requests int64  `json:"requests"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !health.OK || health.Seed != 42 || health.Requests != 3 {
		t.Fatalf("health = %+v", health)
	}
}
