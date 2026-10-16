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

// authOpts builds options with Digest authentication.
func authOpts() options {
	o := baseOpts()
	o.apiUser = "sim"
	o.apiPassword = "simpass"
	o.challengeRealm = "Webinterface area"
	return o
}

// simMD5Test recomputes the expected digest independently.
func simMD5Test(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// authHeader builds a valid Authorization header for the simulator.
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

// paramOf extracts a value from a challenge header.
func paramOf(header, key string) string {
	for _, part := range strings.Split(header, ", ") {
		k, v, ok := strings.Cut(strings.TrimPrefix(part, "Digest "), "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// TestInternalPathsWithoutAuth: without --api-user, the four paths respond
// without authentication — that is the phase 1 behaviour and that of the
// status service of the official client (module yTJA).
func TestInternalPathsWithoutAuth(t *testing.T) {
	sim := testSim(t, baseOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	for _, path := range internalPaths {
		code, hdr, body := get(t, srv, path, nil)
		if code != http.StatusOK {
			t.Fatalf("%s: status = %d (%s)", path, code, body)
		}
		if hdr.Get(nonStandardChallengeHeader) != "" || hdr.Get(standardChallengeHeader) != "" {
			t.Fatalf("%s: no challenge must be issued", path)
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
		t.Fatalf("body = %+v", got)
	}
	if got.ModelName == "" || got.FirmwareVersion == "" {
		t.Fatalf("the candidate keys must be present: %+v", got)
	}
	// The real serial number of the installation must never appear.
	if strings.Contains(string(body), "33319328") {
		t.Fatalf("real serial number present: %s", body)
	}
	// Determinism.
	_, _, again := get(t, srv, versionPath, nil)
	if string(again) != string(body) {
		t.Fatalf("non-deterministic body:\n%s\n%s", body, again)
	}
}

// TestComponentPresenceBodies: an empty Body.Data means "component absent".
func TestComponentPresenceBodies(t *testing.T) {
	cases := []struct {
		name        string
		withMeter   bool
		withBattery bool
		wantMeter   bool
		wantBattery bool
	}{
		{name: "no component"},
		{name: "meter only", withMeter: true, wantMeter: true},
		{name: "battery only", withBattery: true, wantBattery: true},
		{name: "both", withMeter: true, withBattery: true, wantMeter: true, wantBattery: true},
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
					t.Fatalf("%s: status = %d", path, code)
				}
				var env struct {
					Body struct {
						Data map[string]any `json:"Data"`
					} `json:"Body"`
				}
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("%s: Unmarshal: %v", path, err)
				}
				if env.Body.Data == nil {
					t.Fatalf("%s: Body.Data must be an object, not null: %s", path, body)
				}
				if got := len(env.Body.Data) > 0; got != want {
					t.Fatalf("%s: presence = %v, want %v (%s)", path, got, want, body)
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
		t.Run(fmt.Sprintf("%d events", tc.count), func(t *testing.T) {
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
				t.Fatalf("events = %d, want %d (%s)", len(env.Body.Data), tc.count, body)
			}
			for i := range tc.count {
				ev, ok := env.Body.Data[fmt.Sprint(i)]
				if !ok {
					t.Fatalf("key %d absent: %s", i, body)
				}
				if ev.Code != fmt.Sprintf("SIM-%d", i) || ev.State != "active" || ev.Message == "" {
					t.Fatalf("event %d = %+v", i, ev)
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
			t.Fatalf("%s: status = %d, want 401", path, code)
		}
		ch := hdr.Get(nonStandardChallengeHeader)
		if ch == "" {
			t.Fatalf("%s: challenge absent from %s", path, nonStandardChallengeHeader)
		}
		if paramOf(ch, "realm") != "Webinterface area" {
			t.Fatalf("%s: realm = %q", path, paramOf(ch, "realm"))
		}
		if !strings.HasPrefix(paramOf(ch, "nonce"), "sim-nonce-") {
			t.Fatalf("%s: nonce = %q", path, paramOf(ch, "nonce"))
		}
		if paramOf(ch, "qop") != "auth" {
			t.Fatalf("%s: qop = %q", path, paramOf(ch, "qop"))
		}
		// The standard header is not issued by default.
		if v := hdr.Get(standardChallengeHeader); v != "" {
			t.Fatalf("%s: %s must not be issued by default (%q)", path, standardChallengeHeader, v)
		}
	}
}

func TestDigestAccepted(t *testing.T) {
	sim := testSim(t, authOpts())
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// Challenge.
	code, hdr, _ := get(t, srv, versionPath, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", code)
	}
	challenge := hdr.Get(nonStandardChallengeHeader)
	nonce := paramOf(challenge, "nonce")

	auth := authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "0123456789abcdef")
	code, _, body := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", code, body)
	}
	var got versionJSON
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SerialNumber != simulatedSerial {
		t.Fatalf("body = %+v", got)
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
		{"wrong password", authHeader("sim", "wrong", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"unknown user", authHeader("other", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"different realm", authHeader("sim", "simpass", "other realm", nonce, http.MethodGet, versionPath, 1, "aaaa")},
		{"unknown nonce", authHeader("sim", "simpass", "Webinterface area", "sim-nonce-999", http.MethodGet, versionPath, 1, "aaaa")},
		{"inconsistent uri", authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, "/other/path", 1, "aaaa")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": tc.header})
			if code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", code)
			}
		})
	}
}

// TestNonceCountMustIncrease: the simulator implements replay protection,
// which genuinely exercises the client's counter.
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
		t.Fatalf("nc=1: status = %d, want 200", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(1)}); code != http.StatusUnauthorized {
		t.Fatalf("nc=1 replayed: status = %d, want 401", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(3)}); code != http.StatusOK {
		t.Fatalf("nc=3: status = %d, want 200", code)
	}
	if code, _, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth(2)}); code != http.StatusUnauthorized {
		t.Fatalf("nc=2 after nc=3: status = %d, want 401", code)
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
		t.Fatalf("both headers are expected: %q / %q", nonStandard, standard)
	}
	if nonStandard != standard {
		t.Fatalf("both challenges must be identical: %q / %q", nonStandard, standard)
	}
	if paramOf(standard, "nonce") == "" || paramOf(standard, "realm") != "Webinterface area" {
		t.Fatalf("incomplete standard challenge: %q", standard)
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
					t.Fatalf("no challenge must be issued: %q / %q",
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
					t.Fatalf("%s must not be issued", nonStandardChallengeHeader)
				}
				if hdr.Get(standardChallengeHeader) == "" {
					t.Fatalf("%s expected", standardChallengeHeader)
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
					t.Fatalf("challenge without qop expected: %q", ch)
				}
				if paramOf(ch, "nonce") == "" {
					t.Fatalf("missing nonce: %q", ch)
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
			// The stale=true flag appears only on the authenticated attempt:
			// TestStaleNonceFaultRecovers verifies it. Here, the bare
			// request receives the ordinary challenge.
			name: "401-stale-nonce", fault: faultStaleNonce, apiUser: "sim",
			check: func(t *testing.T, code int, hdr http.Header) {
				t.Helper()
				if code != http.StatusUnauthorized {
					t.Fatalf("status = %d", code)
				}
				if paramOf(hdr.Get(nonStandardChallengeHeader), "nonce") == "" {
					t.Fatalf("challenge without nonce: %q", hdr.Get(nonStandardChallengeHeader))
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
					t.Fatal("the challenge must accompany the refusal")
				}
			},
		},
		{
			name: "no credentials configured, no fault", fault: faultBadDigest, apiUser: "",
			check: func(t *testing.T, code int, _ http.Header) {
				t.Helper()
				// Without --api-user, authentication is not required: the Digest
				// fault has no grip.
				if code != http.StatusOK {
					t.Fatalf("status = %d, want 200", code)
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

// TestStaleNonceFaultRecovers: the first authenticated attempt is refused
// with stale=true, the next one is accepted.
func TestStaleNonceFaultRecovers(t *testing.T) {
	opt := authOpts()
	sim := testSim(t, opt)
	sim.opt.fault = faultStaleNonce
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	// First cycle: challenge, then stale refusal.
	_, hdr, _ := get(t, srv, versionPath, nil)
	nonce := paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")
	auth := authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "cccc")
	code, hdr, _ := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusUnauthorized {
		t.Fatalf("first authenticated attempt: status = %d, want 401", code)
	}
	if ch := hdr.Get(nonStandardChallengeHeader); !strings.Contains(ch, "stale=true") {
		t.Fatalf("stale challenge expected after the first authenticated attempt: %q", ch)
	}

	// Second cycle: new challenge, digest accepted.
	_, hdr, _ = get(t, srv, versionPath, nil)
	nonce = paramOf(hdr.Get(nonStandardChallengeHeader), "nonce")
	auth = authHeader("sim", "simpass", "Webinterface area", nonce, http.MethodGet, versionPath, 1, "dddd")
	code, _, body := get(t, srv, versionPath, map[string]string{"Authorization": auth})
	if code != http.StatusOK {
		t.Fatalf("second attempt: status = %d (%s), want 200", code, body)
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
		t.Fatalf("POST %s: status = %d, want 405", versionPath, resp.StatusCode)
	}

	// An internal path that is not in the allowlist does not exist. The paths
	// the survey of 2026-10-16 justified are served now (/status/common,
	// /status/network, /status/devices, /status/emrs): this list holds the ones
	// that remain outside it.
	for _, path := range []string{
		"/status/events", "/status/powerflow", "/components/cache/readable",
		"/components/DataManager/readable",
	} {
		if code, _, _ := get(t, srv, path, nil); code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", path, code)
		}
	}
}

// TestInternalBodyIsDeterministicUnderConcurrentRequests: the body never
// carries the nonce; two simultaneous calls return the same byte.
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
		t.Fatal("no valid response")
	}
	for _, b := range bodies {
		if strings.Contains(b, "sim-nonce") {
			t.Fatalf("the nonce must not appear in the body: %s", b)
		}
		if b != bodies[0] {
			t.Fatalf("non-deterministic body:\n%s\n%s", bodies[0], b)
		}
	}
}

// TestFaultModesListIsComplete: the modes announced by the flag must
// all be injectable and recognised.
func TestFaultModesListIsComplete(t *testing.T) {
	required := []string{
		faultNoChallenge, faultStandardOnly, faultNoQop, faultOpaque, faultStaleNonce, faultBadDigest,
	}
	for _, want := range required {
		if !containsString(faultModes, want) {
			t.Fatalf("%q absent from faultModes", want)
		}
	}
	if opt := baseOpts(); opt.apiUser != "" {
		t.Fatal("baseOpts must not require authentication")
	}
	// An unknown mode stays refused.
	if err := run(options{tz: "Europe/Brussels", fault: "not-a-fault"}); err == nil {
		t.Fatal("an unknown fault must be refused")
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
