// Tests of the read-only connectivity test (/api/v1/connectivity).
//
// The contract the interface consumes: two probes in order, a state per probe
// mapped from the classified error, an overall verdict, a 10 s cache driven by
// the injectable clock, and never a secret in the body. Every case uses a
// counting origin server, so "the probe was skipped" is proven by zero
// requests, not by the absence of a state.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/secret"
)

const (
	powerflowOKBody = `{"Head":{"Status":{"Code":0}},"Body":{"Data":{"Site":{},"Inverters":{}}}}`
	versionOKBody   = `{"swrevisions":{"GEN24":"1.19.7-1"}}`
	testUser        = "installer-secret-user"
	testPassword    = "S3cr3t-Passw0rd"
)

// deviceStub is an origin server that counts the requests it receives per path.
type deviceStub struct {
	srv    *httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

func newDeviceStub(t *testing.T, handle func(http.ResponseWriter, *http.Request)) *deviceStub {
	t.Helper()
	d := &deviceStub{counts: map[string]int{}}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.counts[r.URL.Path]++
		d.mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *deviceStub) count(path string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.counts[path]
}

// challenge sends the Digest challenge the reference firmware sends.
func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Digest realm="Webinterface", nonce="0123456789abcdef"`)
	w.WriteHeader(http.StatusUnauthorized)
}

// serveVersion serves /status/version to authenticated requests only.
func serveVersion(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		challenge(w)
		return
	}
	_, _ = w.Write([]byte(versionOKBody))
}

// okDevice answers both probes successfully on the nominal path.
func okDevice(t *testing.T) *deviceStub {
	t.Helper()
	return newDeviceStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case solarapi.PowerFlowPath:
			_, _ = w.Write([]byte(powerflowOKBody))
		case internalapi.VersionPath:
			serveVersion(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// connectivityFor starts the API in front of an origin server.
func connectivityFor(t *testing.T, origin string, creds bool, tweak func(*config.Config)) (*Server, *httptest.Server) {
	t.Helper()
	srv, httpSrv, _ := newTestServer(t, func(c *config.Config) {
		c.SolarAPIURL = origin
		if creds {
			c.DeviceUser = testUser
			c.DevicePassword = secret.New(testPassword)
		}
		if tweak != nil {
			tweak(c)
		}
	})
	return srv, httpSrv
}

func fetchConnectivity(t *testing.T, srv *httptest.Server) (connectivityDTO, http.Header, []byte) {
	t.Helper()
	status, header, body := doRequest(t, srv, http.MethodGet, "/api/v1/connectivity")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	var out connectivityDTO
	if err := json.Unmarshal(decodeEnvelope(t, body).Data, &out); err != nil {
		t.Fatalf("decoding connectivity: %v", err)
	}
	if len(out.Probes) != 2 {
		t.Fatalf("probes = %d, want 2", len(out.Probes))
	}
	return out, header, body
}

func TestConnectivityAllOK(t *testing.T) {
	device := okDevice(t)
	_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	out, header, _ := fetchConnectivity(t, httpSrv)

	if !out.CredentialsConfigured {
		t.Fatal("credentials_configured must be true")
	}
	if out.Cached {
		t.Fatal("the first test is never cached")
	}
	if out.Overall != overallOK {
		t.Fatalf("overall = %q, want ok", out.Overall)
	}
	if out.TestedAt == "" {
		t.Fatal("tested_at must be present")
	}

	powerflow, version := out.Probes[0], out.Probes[1]
	if powerflow.Name != probeNamePowerFlow || powerflow.Path != solarapi.PowerFlowPath {
		t.Fatalf("probe 0 = %+v", powerflow)
	}
	if powerflow.State != probeStateOK {
		t.Fatalf("powerflow state = %q, want ok (%s)", powerflow.State, powerflow.Reason)
	}
	if powerflow.HTTPStatus == nil || *powerflow.HTTPStatus != http.StatusOK {
		t.Fatalf("powerflow http_status = %v, want 200", powerflow.HTTPStatus)
	}
	if powerflow.Detail != "" {
		t.Fatalf("powerflow detail = %q, the probe invents no measurement", powerflow.Detail)
	}

	if version.Name != probeNameVersion || version.Path != internalapi.VersionPath {
		t.Fatalf("probe 1 = %+v", version)
	}
	if version.State != probeStateOK {
		t.Fatalf("version state = %q, want ok (%s)", version.State, version.Reason)
	}
	if version.HTTPStatus == nil || *version.HTTPStatus != http.StatusOK {
		t.Fatalf("version http_status = %v, want 200", version.HTTPStatus)
	}
	if version.Detail != "firmware 1.19.7-1" {
		t.Fatalf("version detail = %q", version.Detail)
	}

	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

// TestConnectivityUnauthorized proves a 401 is reported as `unauthorized`, not
// as a generic failure, and that a reachable device makes the verdict partial.
func TestConnectivityUnauthorized(t *testing.T) {
	device := newDeviceStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case solarapi.PowerFlowPath:
			_, _ = w.Write([]byte(powerflowOKBody))
		case internalapi.VersionPath:
			challenge(w)
		default:
			http.NotFound(w, r)
		}
	})
	_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	out, _, _ := fetchConnectivity(t, httpSrv)

	version := out.Probes[1]
	if version.State != probeStateUnauthorized {
		t.Fatalf("version state = %q, want unauthorized", version.State)
	}
	if version.Reason != "unauthorized" {
		t.Fatalf("version reason = %q, want the classification", version.Reason)
	}
	if version.HTTPStatus == nil || *version.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("version http_status = %v, want 401", version.HTTPStatus)
	}
	if out.Overall != overallPartial {
		t.Fatalf("overall = %q, want partial", out.Overall)
	}
}

func TestConnectivityAbsent(t *testing.T) {
	device := newDeviceStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case solarapi.PowerFlowPath:
			_, _ = w.Write([]byte(powerflowOKBody))
		default:
			http.NotFound(w, r)
		}
	})
	_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	out, _, _ := fetchConnectivity(t, httpSrv)

	version := out.Probes[1]
	if version.State != probeStateAbsent {
		t.Fatalf("version state = %q, want absent", version.State)
	}
	if version.Reason != "not_found" {
		t.Fatalf("version reason = %q, want not_found", version.Reason)
	}
	if version.HTTPStatus == nil || *version.HTTPStatus != http.StatusNotFound {
		t.Fatalf("version http_status = %v, want 404", version.HTTPStatus)
	}
	if out.Overall != overallPartial {
		t.Fatalf("overall = %q, want partial", out.Overall)
	}
}

// TestConnectivityTimeout: a device that never answers is a timeout, bounded
// by cfg.HTTPTimeout, and the verdict is failed.
func TestConnectivityTimeout(t *testing.T) {
	device := newDeviceStub(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	_, httpSrv := connectivityFor(t, device.srv.URL, false, func(c *config.Config) {
		c.HTTPTimeout = 50 * time.Millisecond
	})

	out, _, _ := fetchConnectivity(t, httpSrv)

	powerflow := out.Probes[0]
	if powerflow.State != probeStateTimeout {
		t.Fatalf("powerflow state = %q, want timeout (%s)", powerflow.State, powerflow.Reason)
	}
	if powerflow.Reason != "timeout" {
		t.Fatalf("powerflow reason = %q, want timeout", powerflow.Reason)
	}
	if powerflow.HTTPStatus != nil {
		t.Fatalf("powerflow http_status = %v, no response was received", *powerflow.HTTPStatus)
	}
	if out.Overall != overallFailed {
		t.Fatalf("overall = %q, want failed", out.Overall)
	}
	if out.Probes[1].State != probeStateSkipped {
		t.Fatalf("version state = %q, want skipped without credentials", out.Probes[1].State)
	}
}

func TestConnectivityUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	_, httpSrv := connectivityFor(t, deadURL, false, nil)

	out, _, _ := fetchConnectivity(t, httpSrv)

	powerflow := out.Probes[0]
	if powerflow.State != probeStateUnreachable {
		t.Fatalf("powerflow state = %q, want unreachable (%s)", powerflow.State, powerflow.Reason)
	}
	if powerflow.Reason != "transport" {
		t.Fatalf("powerflow reason = %q, want transport", powerflow.Reason)
	}
	if powerflow.HTTPStatus != nil {
		t.Fatalf("powerflow http_status = %v, no response was received", *powerflow.HTTPStatus)
	}
	if out.Overall != overallFailed {
		t.Fatalf("overall = %q, want failed", out.Overall)
	}
}

// TestConnectivityDecodeError: a 200 with an unreadable body is `error`, not a
// success and not a timeout.
func TestConnectivityDecodeError(t *testing.T) {
	device := newDeviceStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case solarapi.PowerFlowPath:
			_, _ = w.Write([]byte("not json"))
		default:
			serveVersion(w, r)
		}
	})
	_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	out, _, _ := fetchConnectivity(t, httpSrv)

	powerflow := out.Probes[0]
	if powerflow.State != probeStateError {
		t.Fatalf("powerflow state = %q, want error (%s)", powerflow.State, powerflow.Reason)
	}
	if powerflow.Reason != "decode" {
		t.Fatalf("powerflow reason = %q, want decode", powerflow.Reason)
	}
	if powerflow.HTTPStatus == nil || *powerflow.HTTPStatus != http.StatusOK {
		t.Fatalf("powerflow http_status = %v, a response was received", powerflow.HTTPStatus)
	}
	if out.Overall != overallFailed {
		t.Fatalf("overall = %q, want failed", out.Overall)
	}
}

// TestConnectivitySkippedWithoutCredentials: no pair, no request — not even an
// unauthenticated one — and the device answering the Solar API keeps the
// verdict ok.
func TestConnectivitySkippedWithoutCredentials(t *testing.T) {
	device := okDevice(t)
	_, httpSrv := connectivityFor(t, device.srv.URL, false, nil)

	out, _, _ := fetchConnectivity(t, httpSrv)

	if out.CredentialsConfigured {
		t.Fatal("credentials_configured must be false")
	}
	version := out.Probes[1]
	if version.State != probeStateSkipped {
		t.Fatalf("version state = %q, want skipped", version.State)
	}
	if version.Reason != reasonNoCredentials {
		t.Fatalf("version reason = %q, want %q", version.Reason, reasonNoCredentials)
	}
	if got := device.count(internalapi.VersionPath); got != 0 {
		t.Fatalf("%d request(s) reached /status/version without credentials, want 0", got)
	}
	if out.Overall != overallOK {
		t.Fatalf("overall = %q, want ok", out.Overall)
	}
}

// TestConnectivityCache proves the 10 s window with the injectable clock: two
// calls inside it run the probes once, and the second says cached with the
// original tested_at.
func TestConnectivityCache(t *testing.T) {
	device := okDevice(t)
	apiSrv, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	now := time.Date(2026, 10, 17, 9, 12, 4, 0, time.UTC)
	apiSrv.now = func() time.Time { return now }

	first, _, _ := fetchConnectivity(t, httpSrv)
	if first.Cached {
		t.Fatal("the first test is never cached")
	}
	if first.TestedAt != formatTime(now) {
		t.Fatalf("tested_at = %q, want %q", first.TestedAt, formatTime(now))
	}
	if got := device.count(solarapi.PowerFlowPath); got != 1 {
		t.Fatalf("powerflow requests = %d, want 1", got)
	}

	// Just before the window closes: reused, same instant.
	now = now.Add(9 * time.Second)
	second, _, _ := fetchConnectivity(t, httpSrv)
	if !second.Cached {
		t.Fatal("a call inside the 10 s window must be served from the cache")
	}
	if second.TestedAt != first.TestedAt {
		t.Fatalf("cached tested_at = %q, want the original %q", second.TestedAt, first.TestedAt)
	}
	if got := device.count(solarapi.PowerFlowPath); got != 1 {
		t.Fatalf("powerflow requests = %d after a cached call, want 1", got)
	}

	// At 10 s the window is closed: the probes run again.
	now = now.Add(time.Second)
	third, _, _ := fetchConnectivity(t, httpSrv)
	if third.Cached {
		t.Fatal("the result at 10 s must not be cached")
	}
	if third.TestedAt != formatTime(now) {
		t.Fatalf("tested_at = %q, want %q", third.TestedAt, formatTime(now))
	}
	if got := device.count(solarapi.PowerFlowPath); got != 2 {
		t.Fatalf("powerflow requests = %d after the window, want 2", got)
	}
}

// TestConnectivityConcurrent runs simultaneous presses: the probes must run
// once, and every response must be well formed.
func TestConnectivityConcurrent(t *testing.T) {
	device := okDevice(t)
	_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(httpSrv.URL + "/api/v1/connectivity")
			if err != nil {
				t.Errorf("GET: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	if got := device.count(solarapi.PowerFlowPath); got != 1 {
		t.Fatalf("powerflow requests = %d under concurrency, want 1", got)
	}
}

// TestConnectivityNoSecret asserts the response body contains neither the
// username nor the password, on the success and on the failure path.
func TestConnectivityNoSecret(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter, r *http.Request){
		"success": func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case solarapi.PowerFlowPath:
				_, _ = w.Write([]byte(powerflowOKBody))
			default:
				serveVersion(w, r)
			}
		},
		"failure": func(w http.ResponseWriter, r *http.Request) {
			// No challenge: the transport reports the 401 as an authorization
			// failure and never echoes the request headers.
			w.WriteHeader(http.StatusUnauthorized)
		},
	}

	for name, handle := range cases {
		t.Run(name, func(t *testing.T) {
			device := newDeviceStub(t, handle)
			_, httpSrv := connectivityFor(t, device.srv.URL, true, nil)

			_, _, body := fetchConnectivity(t, httpSrv)
			text := string(body)
			for _, secretValue := range []string{testUser, testPassword, "Authorization", "Digest"} {
				if strings.Contains(text, secretValue) {
					t.Fatalf("the response contains %q: %s", secretValue, text)
				}
			}
		})
	}
}

// TestCacheControlHeader: JSON responses are never cached by the browser; the
// static assets keep their own headers.
func TestCacheControlHeader(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	device := okDevice(t)
	_, httpSrv := connectivityFor(t, device.srv.URL, true, func(c *config.Config) {
		c.WebDir = webDir
	})

	_, header, _ := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("API Cache-Control = %q, want no-store", got)
	}

	status, staticHeader, _ := doRequest(t, httpSrv, http.MethodGet, "/app.js")
	if status != http.StatusOK {
		t.Fatalf("GET /app.js = %d", status)
	}
	if got := staticHeader.Get("Cache-Control"); got == "no-store" {
		t.Fatal("the static handler must keep its own headers")
	}
}

// TestSystemEffectiveSettings covers the three settings the page gained.
func TestSystemEffectiveSettings(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, func(c *config.Config) {
		c.DetectInterval = 90 * time.Second
		c.HTTPTimeout = 2500 * time.Millisecond
		c.DeviceUser = testUser
		c.DevicePassword = secret.New(testPassword)
	})

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	var sys map[string]any
	if err := json.Unmarshal(decodeEnvelope(t, body).Data, &sys); err != nil {
		t.Fatalf("decoding system: %v", err)
	}

	if got := sys["detect_interval_s"]; got != float64(90) {
		t.Fatalf("detect_interval_s = %v, want 90", got)
	}
	if got := sys["http_timeout_s"]; got != 2.5 {
		t.Fatalf("http_timeout_s = %v, want 2.5", got)
	}
	if got := sys["credentials_configured"]; got != true {
		t.Fatalf("credentials_configured = %v, want true", got)
	}
	if strings.Contains(string(body), testPassword) || strings.Contains(string(body), testUser) {
		t.Fatal("the system response must carry no credential value")
	}
}

func TestSystemCredentialsNotConfigured(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	var sys map[string]any
	if err := json.Unmarshal(decodeEnvelope(t, body).Data, &sys); err != nil {
		t.Fatalf("decoding system: %v", err)
	}
	if got, ok := sys["credentials_configured"]; !ok || got != false {
		t.Fatalf("credentials_configured = %v (present %v), want false", got, ok)
	}
}
