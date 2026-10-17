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

	"fronius-supervisor/internal/sources"
)

// defaultTargets is what a default run actually sends: the default allowlist
// minus the entries whose query is a documentation placeholder, which is
// skipped rather than sent.
func defaultTargets() []endpoint {
	out := make([]endpoint, 0, len(whitelist))
	for _, ep := range whitelist {
		if !ep.hasPlaceholderQuery() {
			out = append(out, ep)
		}
	}
	return out
}

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
	// The probe's default set is derived from the source catalogue (the sources
	// this project reads, plus the ones already captured): the expectation is
	// counted from that register rather than written down, so adding a source
	// cannot leave a stale number here.
	// The default set is the sources the project reads plus the ones whose body
	// has been promoted as a fixture. A source the survey merely observed (an
	// empty answer on an endpoint with no component behind it) is reachable by
	// name but must not inflate the default run.
	want := 0
	for _, src := range sources.Catalog() {
		if src.State == sources.StateCalled || src.Fixture != "" {
			want++
		}
	}
	if len(whitelist) != want {
		t.Fatalf("allowlist = %d entries, want the %d called-or-fixtured sources", len(whitelist), want)
	}
	if want == 0 {
		t.Fatal("the catalogue announces no source to probe: the check above proves nothing")
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
		// Two collection variants share the realtime-data path, so the identity
		// of an entry is the path WITH its query — which is also what
		// --endpoint accepts.
		if seen[ep.displayPath()] {
			t.Fatalf("%s: duplicate entry", ep.displayPath())
		}
		seen[ep.displayPath()] = true
	}

	// The five internal paths are reached with a Digest by the official client;
	// the Solar API endpoint never sends an authentication header. (The flag
	// describes the official client, not this device: the capture of 2026-10-16
	// shows that firmware 1.19.7-1 answers all of them without a challenge.)
	for _, ep := range whitelist {
		wantDigest := !strings.HasPrefix(ep.path, "/solar_api/")
		if ep.digest != wantDigest {
			t.Fatalf("%s: digest = %v, want %v", ep.path, ep.digest, wantDigest)
		}
	}

	// The file names derived from the paths must be distinct: two
	// internal paths both end with "readable".
	names := map[string]bool{}
	for _, ep := range whitelist {
		name := saveLabel(ep)
		if names[name] {
			t.Fatalf("%s: file name collision (%s)", ep.displayPath(), name)
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

		// Only the internal paths require a Digest; the Solar API endpoints
		// respond without a header (two distinct routes, see docs/SOURCES.md).
		if r.Header.Get("Authorization") == "" && !strings.HasPrefix(r.URL.Path, "/solar_api/") {
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
	for _, ep := range defaultTargets() {
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
	// One bare internal attempt (the first; the nonce is reused afterwards) plus
	// one bare request per non-digest entry. Counted from the list rather than
	// written down: adding a solar path must not leave a stale number here.
	wantBare := 1
	for _, ep := range whitelist {
		if !ep.digest {
			wantBare++
		}
	}
	if unauthenticated != wantBare {
		t.Fatalf("requests without header = %d, want %d", unauthenticated, wantBare)
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
	if err != nil || len(files) != len(defaultTargets()) {
		t.Fatalf("written files = %v (err=%v), want %d", files, err, len(defaultTargets()))
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
	// Every Solar API entry of the default set is cited, counted from the list
	// rather than written down.
	wantSolar := 0
	for _, ep := range whitelist {
		if strings.HasPrefix(ep.path, "/solar_api/") {
			wantSolar++
		}
	}
	if n := strings.Count(out, "/solar_api/"); n != wantSolar {
		t.Fatalf("the allowlist cites %d Solar API entries, want %d:\n%s", n, wantSolar, out)
	}
	if !strings.Contains(out, "DataCollection=CumulationInverterData") {
		t.Fatalf("the accumulation entry must show its frozen query:\n%s", out)
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

	targets := defaultTargets()
	if len(paths) != len(targets) {
		t.Fatalf("requests sent = %d, want %d (one per sendable endpoint)", len(paths), len(targets))
	}
	for i, ep := range targets {
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

// recordingServer answers every request with 200 and records the URLs it
// received, so a test can assert what the probe actually sent (and what it did
// not send).
func recordingServer(t *testing.T, body string) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.String())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// TestQueryOverrideSendsExactlyTheValueOnTheAllowlistedPath: --query replaces
// the catalogue's placeholder, and nothing else.
func TestQueryOverrideSendsExactlyTheValueOnTheAllowlistedPath(t *testing.T) {
	srv, requests := recordingServer(t, `{"resultData":{"state":"ok"},"success":true}`)

	var buf bytes.Buffer
	err := run(options{
		baseURL:     srv.URL,
		endpoint:    "/commands/GetServiceState",
		endpointSet: true,
		query:       "serviceId=21",
		querySet:    true,
		timeout:     2 * time.Second,
		out:         &buf,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	got := requests()
	if len(got) != 1 {
		t.Fatalf("requests sent = %d, want exactly 1: %q", len(got), got)
	}
	if want := "/commands/GetServiceState?serviceId=21"; got[0] != want {
		t.Fatalf("request URL = %q, want %q", got[0], want)
	}
	if strings.Contains(buf.String(), "<id>") {
		t.Fatalf("the placeholder appears in the output:\n%s", buf.String())
	}
}

// TestQueryOnAllIsRefused: an override has no meaning for a walk.
func TestQueryOnAllIsRefused(t *testing.T) {
	srv, requests := recordingServer(t, `{}`)

	var buf bytes.Buffer
	err := run(options{
		baseURL:     srv.URL,
		endpoint:    "all",
		endpointSet: true,
		query:       "serviceId=21",
		querySet:    true,
		timeout:     2 * time.Second,
		out:         &buf,
	})
	if err == nil {
		t.Fatal("--query with --endpoint all must be refused")
	}
	for _, want := range []string{"--query", "single --endpoint"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("refused run sent %d request(s): %q", len(got), got)
	}
}

// TestQueryOnArgumentLessEntryIsRefused: an override must not invent an
// argument on a path that declares none.
func TestQueryOnArgumentLessEntryIsRefused(t *testing.T) {
	var buf bytes.Buffer
	err := run(options{
		baseURL:     "http://127.0.0.1:8081",
		endpoint:    "/status/version",
		endpointSet: true,
		query:       "serviceId=21",
		querySet:    true,
		timeout:     time.Second,
		out:         &buf,
	})
	if err == nil {
		t.Fatal("--query on an argument-less entry must be refused")
	}
	for _, want := range []string{"--query", "declares no query"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// TestPlaceholderIsRefusedWithoutOverrideAndSendsNothing: a single explicit
// placeholder target fails, before any request exists.
func TestPlaceholderIsRefusedWithoutOverrideAndSendsNothing(t *testing.T) {
	srv, requests := recordingServer(t, `{}`)

	var buf bytes.Buffer
	err := run(options{
		baseURL:     srv.URL,
		endpoint:    "/commands/GetServiceState",
		endpointSet: true,
		timeout:     2 * time.Second,
		out:         &buf,
	})
	if err == nil {
		t.Fatal("a placeholder target without --query must be refused")
	}
	for _, want := range []string{"serviceId=<id>", "--query"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if got := requests(); len(got) != 0 {
		t.Fatalf("the refused target sent %d request(s): %q", len(got), got)
	}
}

// TestSurveySkipsThePlaceholderOnceAndSendsNothing: a walk cannot supply a
// value, so the entry is left out with one notice.
func TestSurveySkipsThePlaceholderOnceAndSendsNothing(t *testing.T) {
	srv, requests := recordingServer(t, `{}`)

	var buf bytes.Buffer
	err := run(options{
		baseURL: srv.URL,
		survey:  true,
		delay:   0,
		timeout: 2 * time.Second,
		out:     &buf,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, got := range requests() {
		if strings.HasPrefix(got, "/commands/GetServiceState") {
			t.Fatalf("the survey sent the placeholder entry: %q", got)
		}
		if strings.Contains(got, "<") || strings.Contains(got, ">") {
			t.Fatalf("the survey sent a placeholder marker: %q", got)
		}
	}
	if n := strings.Count(buf.String(), "Skipping /commands/GetServiceState?serviceId=<id>"); n != 1 {
		t.Fatalf("the skip notice appears %d times, want 1:\n%s", n, buf.String())
	}
}

// TestQueryRejectsPlaceholderMarkerAndEmptyValues: the override is a value,
// not a second placeholder.
func TestQueryRejectsPlaceholderMarkerAndEmptyValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"a marker", "serviceId=<id>"},
		{"a greater-than marker", "serviceId=21>1"},
		{"an encoded marker", "serviceId=%3Cid%3E"},
		{"empty", ""},
		{"blank", "   "},
		{"keyless", "=21"},
		{"separator only", "?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := run(options{
				baseURL:     "http://127.0.0.1:8081",
				endpoint:    "/commands/GetServiceState",
				endpointSet: true,
				query:       tc.query,
				querySet:    true,
				timeout:     time.Second,
				out:         &buf,
			})
			if err == nil {
				t.Fatalf("--query %q must be refused", tc.query)
			}
			if !strings.Contains(err.Error(), "--query") {
				t.Fatalf("error %q does not mention the flag", err)
			}
		})
	}
}

// TestDefaultRunSkipsThePlaceholderWithOneNotice pins the default walk: no
// request carries the placeholder, and the operator is told once.
func TestDefaultRunSkipsThePlaceholderWithOneNotice(t *testing.T) {
	srv, requests := recordingServer(t, `{}`)

	var buf bytes.Buffer
	if err := run(options{baseURL: srv.URL, timeout: 2 * time.Second, out: &buf}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, got := range requests() {
		if strings.Contains(got, "<") || strings.Contains(got, ">") {
			t.Fatalf("the default run sent a placeholder: %q", got)
		}
	}
	if n := strings.Count(buf.String(), "Skipping /commands/GetServiceState?serviceId=<id>"); n != 1 {
		t.Fatalf("the skip notice appears %d times, want 1:\n%s", n, buf.String())
	}
}
