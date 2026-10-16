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

	// The measurements and the timestamp must survive intact.
	for _, keep := range []string{"1902.4", "14236126.1", "produce-only", "2026-06-21T12:00:00+02:00"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("legitimate value %q removed:\n%s", keep, out)
		}
	}
	// No identifying data must remain.
	for _, gone := range []string{"33319328", "192.0.2.10", "fronius-web.local", "aa:bb:cc:dd:ee:ff", "abc123", "hunter2"} {
		if strings.Contains(out, gone) {
			t.Fatalf("sensitive data %q not redacted:\n%s", gone, out)
		}
	}
	// The result remains usable JSON.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("redacted body not decodable: %v", err)
	}
	if strings.Count(out, redactedValue) < 6 {
		t.Fatalf("too few redacted values (%d):\n%s", strings.Count(out, redactedValue), out)
	}
}

func TestRedactJSONNestedAndArrays(t *testing.T) {
	body := []byte(`{"Inverters":{"1":{"P":10.5,"Serial":"12345"}},"Liste":[{"ip":"10.0.0.1"},{"P":3}]}`)
	out := string(redactBody(body))

	if strings.Contains(out, "10.0.0.1") || strings.Contains(out, "12345") {
		t.Fatalf("sensitive data not redacted:\n%s", out)
	}
	if !strings.Contains(out, "10.5") || !strings.Contains(out, "3") {
		t.Fatalf("measurements removed:\n%s", out)
	}
}

func TestRedactNonJSONBody(t *testing.T) {
	body := []byte("<html><body>password='hunter2' host 192.168.0.10 serial=33319328</body></html>")
	out := string(redactBody(body))

	for _, gone := range []string{"hunter2", "192.168.0.10", "33319328"} {
		if strings.Contains(out, gone) {
			t.Fatalf("%q not redacted:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "html") {
		t.Fatalf("the textual body was lost:\n%s", out)
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
		{"X-Custom", "arbitrary value", "arbitrary value"},
	}
	for _, tc := range cases {
		if got := maskHeaderValue(tc.name, tc.value); got != tc.want {
			t.Fatalf("maskHeaderValue(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWhitelistIsGetOnly(t *testing.T) {
	if len(whitelist) != 5 {
		t.Fatalf("allowlist = %d entries, want 5", len(whitelist))
	}
	seen := map[string]bool{}
	for _, ep := range whitelist {
		if ep.method != http.MethodGet {
			t.Fatalf("%s: method %q, only GET is allowed", ep.path, ep.method)
		}
		if ep.classification != "READ" {
			t.Fatalf("%s: classification %q", ep.path, ep.classification)
		}
		if ep.note == "" {
			t.Fatalf("%s: the note is mandatory", ep.path)
		}
		if seen[ep.path] {
			t.Fatalf("%s: duplicate path", ep.path)
		}
		seen[ep.path] = true
	}

	// The four internal paths require a Digest on the official client side;
	// the Solar API endpoint never sends an authentication header.
	for _, ep := range whitelist {
		wantDigest := ep.path != "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"
		if ep.digest != wantDigest {
			t.Fatalf("%s: digest = %v, want %v", ep.path, ep.digest, wantDigest)
		}
	}

	// The file names derived from the paths must be distinct: two
	// internal paths both end with "readable".
	names := map[string]bool{}
	for _, ep := range whitelist {
		name := saveName(ep.path)
		if names[name] {
			t.Fatalf("%s: file name collision (%s)", ep.path, name)
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
	// "all" and the empty string select the whole list.
	for _, name := range []string{"all", ""} {
		got, err := selectEndpoints(name)
		if err != nil {
			t.Fatalf("selectEndpoints(%q): %v", name, err)
		}
		if len(got) != len(whitelist) {
			t.Fatalf("selectEndpoints(%q) = %d entries", name, len(got))
		}
	}

	got, err := selectEndpoints("/status/version")
	if err != nil {
		t.Fatalf("selectEndpoints: %v", err)
	}
	if len(got) != 1 || got[0].path != "/status/version" {
		t.Fatalf("selection = %+v", got)
	}

	// Any path outside the list is refused.
	for _, name := range []string{"/config/modbus", "/status/events", "http://192.168.1.5/status/version", "version"} {
		if _, err := selectEndpoints(name); err == nil {
			t.Fatalf("%q should have been refused", name)
		} else if !errors.Is(err, errNotWhitelisted) {
			t.Fatalf("%q: error = %v", name, err)
		}
	}
}

// TestCredentialsComeFromEnvironment: no password goes through a
// command-line argument.
func TestCredentialsComeFromEnvironment(t *testing.T) {
	t.Setenv("FRONIUS_DEVICE_USER", "installer")
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "from-the-environment")

	cred, err := readCredentials(options{})
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if cred.user != "installer" || cred.password.Reveal() != "from-the-environment" {
		t.Fatalf("credentials = %+v", cred)
	}

	// --password-stdin replaces the environment password.
	cred, err = readCredentials(options{passwordStdin: true, stdin: strings.NewReader("from-stdin\nnext\n")})
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if cred.password.Reveal() != "from-stdin" {
		t.Fatalf("password = %q", cred.password.Reveal())
	}

	// An incomplete pair is refused.
	t.Setenv("FRONIUS_DEVICE_PASSWORD", "")
	if _, err := readCredentials(options{}); err == nil {
		t.Fatal("a user without a password must be refused")
	}
}

// TestProbeSendsNoCredentialsPreemptively: the Authorization header is sent
// only in response to a 401, and it never appears in the output nor in the
// written file.
func TestProbeSendsNoCredentialsPreemptively(t *testing.T) {
	const password = "probe-password"

	var (
		mu          sync.Mutex
		seen        []string // Authorization headers received, in order
		pathsSeen   []string // paths received, in the same order
		hitsPerPath = map[string]int{}
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		pathsSeen = append(pathsSeen, r.URL.Path)
		hitsPerPath[r.URL.Path]++
		mu.Unlock()

		// Only the internal paths require a Digest; the Solar API endpoint
		// responds without a header (two distinct routes, see docs/SOURCES.md).
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

	// Solar API path: a single request, without a header.
	if hitsPerPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"] != 1 {
		t.Fatalf("solar api: %d requests, want 1", hitsPerPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"])
	}
	// First internal path: bare request (401), then authenticated request.
	// The following ones reuse the cached nonce: a single request each.
	if hitsPerPath["/status/version"] != 2 {
		t.Fatalf("version: %d requests, want 2 (bare then authenticated)", hitsPerPath["/status/version"])
	}
	for _, ep := range whitelist {
		if ep.path == "/status/version" || !ep.digest {
			continue
		}
		if hitsPerPath[ep.path] != 1 {
			t.Fatalf("%s: %d requests, want 1 (nonce cached)", ep.path, hitsPerPath[ep.path])
		}
	}

	// No authentication is sent before a 401: the only bare request
	// towards an internal path is the first, and the following ones carry the Digest
	// from the challenge already received.
	byPath := map[string][]string{}
	for i, p := range pathsSeen {
		byPath[p] = append(byPath[p], seen[i])
	}
	if got := byPath["/status/version"]; len(got) != 2 || got[0] != "" || !strings.HasPrefix(got[1], "Digest ") {
		t.Fatalf("version: headers = %q", got)
	}
	if got := byPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"]; len(got) != 1 || got[0] != "" {
		t.Fatalf("solar api: headers = %q", got)
	}
	unauthenticated := 0
	for _, header := range seen {
		if header == "" {
			unauthenticated++
		}
	}
	if unauthenticated != 2 {
		t.Fatalf("requests without header = %d, want 2 (solar api + first challenge)", unauthenticated)
	}
	for _, header := range seen {
		if header != "" && !strings.HasPrefix(header, "Digest ") {
			t.Fatalf("unexpected header: %q", header)
		}
		if strings.Contains(header, password) {
			t.Fatalf("the password appears in the header: %q", header)
		}
	}

	// The password appears neither in the output, nor in the written files.
	if strings.Contains(buf.String(), password) {
		t.Fatalf("the password appears in the output:\n%s", buf.String())
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != len(whitelist) {
		t.Fatalf("written files = %v (err=%v), want %d", files, err, len(whitelist))
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if strings.Contains(string(body), password) {
			t.Fatalf("%s contains the password", f)
		}
	}
}

func TestNonLoopbackIsRefusedWithoutFlag(t *testing.T) {
	var buf bytes.Buffer
	err := run(options{baseURL: "http://192.0.2.10", timeout: time.Second, out: &buf})
	if err == nil {
		t.Fatal("a non-local address should have been refused")
	}
	for _, want := range []string{"refusal", "--allow-non-loopback", "PROBE-READONLY.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q does not mention %q", err, want)
		}
	}
}

func TestLoopbackHostIsAccepted(t *testing.T) {
	// The refusal must not trigger on a local address: the error
	// expected here comes from the absence of a server, not from the guard.
	var buf bytes.Buffer
	err := run(options{baseURL: "http://127.0.0.1:1", timeout: 300 * time.Millisecond, out: &buf})
	if err == nil {
		t.Skip("no server on port 1: error expected")
	}
	if strings.Contains(err.Error(), "is not a local address") {
		t.Fatalf("a local address was refused: %v", err)
	}
}

func TestListPrintsWhitelist(t *testing.T) {
	var buf bytes.Buffer
	if err := run(options{list: true, baseURL: "http://127.0.0.1:8081", out: &buf}); err != nil {
		t.Fatalf("run: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"allowlist", "GET", "/solar_api/v1/GetPowerFlowRealtimeData.fcgi", "READ"} {
		if !strings.Contains(out, want) {
			t.Fatalf("--list output incomplete (%q missing):\n%s", want, out)
		}
	}
	// A single Solar API endpoint, and the four internal paths.
	if n := strings.Count(out, "/solar_api/"); n != 1 {
		t.Fatalf("the allowlist must cite a single Solar API path (%d):\n%s", n, out)
	}
	for _, want := range []string{"/status/version", "/status/activeEvents", "/components/PowerMeter/readable",
		"/components/BatteryManagementSystem/readable", "digest=true", "FRONIUS_DEVICE_USER", "--password-stdin"} {
		if !strings.Contains(out, want) {
			t.Fatalf("--list output incomplete (%q missing):\n%s", want, out)
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
		t.Fatalf("requests sent = %d, want %d (one per endpoint)", len(paths), len(whitelist))
	}
	for i, ep := range whitelist {
		if methods[i] != http.MethodGet {
			t.Fatalf("%s: method sent = %q, want GET", ep.path, methods[i])
		}
		if paths[i] != ep.path {
			t.Fatalf("path called = %q, want %q", paths[i], ep.path)
		}
	}

	out := buf.String()
	for _, want := range []string{"GET", "200 OK", "Redacted body", "192"} {
		if !strings.Contains(out, want) && want != "192" {
			t.Fatalf("output incomplete (%q missing):\n%s", want, out)
		}
	}
	if strings.Contains(out, "33319328") || strings.Contains(out, "192.0.2.10") || strings.Contains(out, "sim-nonce") {
		t.Fatalf("sensitive data appears in the output:\n%s", out)
	}
	if !strings.Contains(out, redactedValue) {
		t.Fatalf("no mention of redaction:\n%s", out)
	}

	files, err := filepath.Glob(filepath.Join(dir, "probe_solar_api_v1_getpowerflowrealtimedata_*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("written files = %v (err=%v)", files, err)
	}
	saved, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading the written file: %v", err)
	}
	if strings.Contains(string(saved), "33319328") || strings.Contains(string(saved), "192.0.2.10") {
		t.Fatalf("the written file contains sensitive data:\n%s", saved)
	}
	if !strings.Contains(string(saved), "1902.4") {
		t.Fatalf("the written file lost the measurement:\n%s", saved)
	}
	var parsed map[string]any
	if err := json.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("the written file must remain JSON: %v", err)
	}

	// The file permissions must not be wider than 0600.
	info, err := os.Stat(files[0])
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permissions = %o, want 600", perm)
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
		t.Fatal("a 401 must be reported as an error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v", err)
	}
}

// TestIPv6DetectionDoesNotTouchTimestamps: the IPv6 pattern must never
// mask a time nor a timestamp, otherwise the fixtures become
// unusable.
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
			t.Fatalf("redactString(%q) = %q: legitimate value redacted", v, got)
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
			t.Fatalf("redactString(%q) = %q: sensitive value kept", v, got)
		}
	}
}

func TestRedactBodyIsIdempotent(t *testing.T) {
	body := []byte(`{"Site":{"IPAddress":"10.1.2.3","P_PV":42}}`)
	once := redactBody(body)
	twice := redactBody(once)
	if string(once) != string(twice) {
		t.Fatalf("redaction not idempotent:\n%s\n%s", once, twice)
	}
}
