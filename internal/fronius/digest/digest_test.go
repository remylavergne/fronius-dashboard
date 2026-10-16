package digest

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/secret"
)

const (
	testUser     = "sim"
	testPassword = "simpass"
	testRealm    = "Webinterface area"
	testNonce    = "sim-nonce-1"
)

// ---------------------------------------------------------------------------
// Independent verifier: does NOT use the production code.
// ---------------------------------------------------------------------------

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// parseParams splits an authentication header into key/value pairs.
//
// Implementation deliberately independent from ParseChallenge: it is used to
// verify the produced header, a circular check would have no value.
func parseParams(header string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(header, ", ") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "Digest "))
		out[key] = strings.Trim(value, `"'`)
	}
	return out
}

// expectedResponse recomputes the expected response from the received header.
func expectedResponse(method, user, password, realm string, params map[string]string) string {
	ha1 := md5hex(user + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + params["uri"])
	input := ha1 + ":" + params["nonce"]
	if qop := params["qop"]; qop != "" {
		input += ":" + params["nc"] + ":" + params["cnonce"] + ":" + qop
	}
	return md5hex(input + ":" + ha2)
}

// device is a test server that verifies the Digest and counts the requests.
type device struct {
	user, password, realm string
	// monotonic enables the "strictly increasing nc" check, like a real server
	// protected against replay. Disabled for the concurrent test: the arrival
	// order of the requests there is undetermined.
	monotonic bool
	mu        sync.Mutex
	requests  int
	// authenticated counts the requests whose Digest was accepted.
	authenticated int
	ncs           []string
	cnonces       []string
	lastNC        map[string]uint64
	currentNonce  string
	srv           *httptest.Server
}

func newDevice(t *testing.T, user, password, realm string) *device {
	t.Helper()
	d := &device{user: user, password: password, realm: realm, lastNC: map[string]uint64{}}
	t.Cleanup(func() {
		if d.srv != nil {
			d.srv.Close()
		}
	})
	return d
}

// nonce returns the current nonce of the test server.
func (d *device) nonce() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentNonce
}

// setNonce replaces the current nonce: any request carrying the old one is
// refused as stale.
func (d *device) setNonce(n string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.currentNonce = n
}

func (d *device) start(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	if d.currentNonce == "" {
		d.currentNonce = testNonce
	}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.requests++
		d.mu.Unlock()
		handler(w, r)
	}))
}

func (d *device) counts() (total, authenticated int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests, d.authenticated
}

func (d *device) nonceCounts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.ncs...)
}

// accept verifies the Authorization header of an already authenticated request.
//
// Replays the checks of a real server: recomputed response, and strictly
// increasing retry counter for a same nonce.
func (d *device) accept(t *testing.T, r *http.Request) bool {
	t.Helper()
	header := r.Header.Get("Authorization")
	if header == "" {
		return false
	}
	params := parseParams(header)
	if params["username"] != d.user || params["realm"] != d.realm {
		return false
	}
	if got := expectedResponse(r.Method, d.user, d.password, d.realm, params); got != params["response"] {
		return false
	}

	// Without qop, the header follows the RFC 2069 form: neither nc nor cnonce.
	d.mu.Lock()
	defer d.mu.Unlock()
	if nc := params["nc"]; nc != "" {
		v, err := parseUint(nc)
		if err != nil {
			return false
		}
		if last, ok := d.lastNC[params["nonce"]]; d.monotonic && ok && v <= last {
			return false // replay: the counter must increase strictly
		}
		d.lastNC[params["nonce"]] = v
		d.ncs = append(d.ncs, nc)
	}
	d.authenticated++
	d.cnonces = append(d.cnonces, params["cnonce"])
	return true
}

func parseUint(s string) (uint64, error) {
	var v uint64
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-decimal: %q", s)
		}
		v = v*10 + uint64(r-'0')
	}
	return v, nil
}

// challenge writes a 401 challenge in the vendor's non-standard header.
func challenge(w http.ResponseWriter, headerName, realm, nonce string, qop, opaque string, stale bool) {
	var b strings.Builder
	fmt.Fprintf(&b, `Digest realm="%s", nonce="%s"`, realm, nonce)
	if qop != "" {
		fmt.Fprintf(&b, `, qop="%s"`, qop)
	}
	if opaque != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, opaque)
	}
	if stale {
		b.WriteString(`, stale=true`)
	}
	w.Header().Set(headerName, b.String())
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "Unauthorized")
}

func newTransport(t *testing.T, baseURL string, cred Credentials) *Transport {
	t.Helper()
	tr, err := New(&http.Client{}, baseURL, cred, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

func get(t *testing.T, tr *Transport, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return tr.Do(req)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// RFC 2617 vector: the correctness anchor of the computation.
// ---------------------------------------------------------------------------

func TestRFC2617Vector(t *testing.T) {
	cred := Credentials{Username: "Mufasa", Password: secret.New("Circle Of Life")}
	ch := Challenge{
		Realm: "testrealm@host.com",
		Nonce: "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		Qop:   "auth",
	}

	got := headerValue(ch, "GET", "/dir/index.html", cred, 1, "0a4f113b")

	const want = `Digest username="Mufasa", realm="testrealm@host.com", ` +
		`nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", uri="/dir/index.html", ` +
		`response="6629fae49393a05397450978507c4ef1", qop=auth, nc=00000001, cnonce="0a4f113b"`
	if got != want {
		t.Fatalf("header =\n%s\nwant\n%s", got, want)
	}
}

// TestRFC2069Vector: without qop, the response is that of the RFC 2069 form
// (MD5(HA1:nonce:HA2)), and neither qop, nor nc, nor cnonce appear.
func TestRFC2069Vector(t *testing.T) {
	cred := Credentials{Username: "Mufasa", Password: secret.New("Circle Of Life")}
	ch := Challenge{Realm: "testrealm@host.com", Nonce: "dcd98b7102dd2f0e8b11d0f600bfb0c093"}

	got := headerValue(ch, "GET", "/dir/index.html", cred, 1, "0a4f113b")
	want := md5hex(md5hex("Mufasa:testrealm@host.com:Circle Of Life") +
		":dcd98b7102dd2f0e8b11d0f600bfb0c093:" +
		md5hex("GET:/dir/index.html"))

	if !strings.Contains(got, `response="`+want+`"`) {
		t.Fatalf("header = %s\nexpected response %s", got, want)
	}
	for _, absent := range []string{"qop", "nc=", "cnonce"} {
		if strings.Contains(got, absent) {
			t.Fatalf("%q must not appear without qop: %s", absent, got)
		}
	}
}

func TestHeaderShape(t *testing.T) {
	cred := Credentials{Username: "sim", Password: secret.New("p")}
	ch := Challenge{Realm: "r", Nonce: "n", Qop: "auth", Opaque: "op"}

	got := headerValue(ch, "GET", "/status/version", cred, 7, "0123456789abcdef")

	// Exact order of the fields.
	order := []string{`username="sim"`, `realm="r"`, `nonce="n"`, `uri="/status/version"`, `response="`, `opaque="op"`, `qop=auth`, `nc=00000007`, `cnonce="0123456789abcdef"`}
	last := -1
	for _, field := range order {
		i := strings.Index(got, field)
		if i < 0 {
			t.Fatalf("field %q absent from %s", field, got)
		}
		if i < last {
			t.Fatalf("field %q out of order in %s", field, got)
		}
		last = i
	}
	// qop and nc without quotes, cnonce between quotes.
	if strings.Contains(got, `qop="`) || strings.Contains(got, `nc="`) {
		t.Fatalf("qop and nc must not be quoted: %s", got)
	}
	if !strings.Contains(got, `cnonce="`) {
		t.Fatalf("cnonce must be quoted: %s", got)
	}
	// "algorithm" is never emitted.
	if strings.Contains(strings.ToLower(got), "algorithm") {
		t.Fatalf("algorithm must not be emitted: %s", got)
	}
	// nc: eight decimal digits, zero-padded.
	if !strings.Contains(got, "nc=00000007") {
		t.Fatalf("malformed nc: %s", got)
	}
}

func TestHeaderValueStripsQuotes(t *testing.T) {
	cred := Credentials{Username: `sim"x`, Password: secret.New("p")}
	ch := Challenge{Realm: `re"alm`, Nonce: `no'nce`, Qop: "auth"}

	got := headerValue(ch, "GET", "/x", cred, 1, "abc")
	for _, bad := range []string{`re"alm`, `no'nce`, `sim"x`} {
		if strings.Contains(got, bad) {
			t.Fatalf("quote not stripped: %s", got)
		}
	}
	if strings.Count(got, `"`)%2 != 0 {
		t.Fatalf("unbalanced quotes: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Network behaviour
// ---------------------------------------------------------------------------

func TestFreshChallengeCostsTwoRequests(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if !d.accept(t, r) {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		_, _ = io.WriteString(w, `{"serialNumber":"SIMULATED-0001"}`)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "SIMULATED-0001") {
		t.Fatalf("body = %s", body)
	}

	total, auth := d.counts()
	if total != 2 || auth != 1 {
		t.Fatalf("requests = %d of which %d authenticated, want 2 of which 1", total, auth)
	}
}

func TestCachedNonceCostsOneRequest(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.monotonic = true
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if !d.accept(t, r) {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	if _, err := get(t, tr, d.srv.URL+"/status/version"); err != nil {
		t.Fatalf("Do: %v", err)
	}

	before, _ := d.counts()
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = readBody(t, resp)
	after, auth := d.counts()

	if after-before != 1 {
		t.Fatalf("second request: %d sends, want 1 (nonce in cache)", after-before)
	}
	if auth != 2 {
		t.Fatalf("authenticated requests = %d, want 2", auth)
	}

	// The retry counter increases by 1 on each authenticated send.
	ncs := d.nonceCounts()
	if len(ncs) != 2 || ncs[0] != "00000001" || ncs[1] != "00000002" {
		t.Fatalf("observed nc = %v, want [00000001 00000002]", ncs)
	}
}

// TestStaleCachedNonceRecoversInOneCall: the nonce in cache is refused by the
// device; the transport starts over from the bare request, obtains a fresh
// challenge and succeeds, without a third attempt.
func TestStaleCachedNonceRecoversInOneCall(t *testing.T) {
	const freshNonce = "sim-nonce-2"

	d := newDevice(t, testUser, testPassword, testRealm)
	d.monotonic = true

	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		if parseParams(header)["nonce"] != d.nonce() {
			// Stale nonce: the server emits a new one with stale=true.
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", true)
			return
		}
		if !d.accept(t, r) {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})

	// First call: fresh challenge, nonce-1 put in cache.
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first call: status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)

	// The server rotates its nonce: the nonce in cache becomes stale.
	d.setNonce(freshNonce)

	before, _ := d.counts()
	resp, err = get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second call: status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)
	after, _ := d.counts()

	if after-before != 3 {
		t.Fatalf("sends = %d, want 3 (cache refused, bare request, fresh challenge)", after-before)
	}
}

func TestUnauthorizedWithoutChallengeIsNotRetried(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	_, err := get(t, tr, d.srv.URL+"/status/version")
	if err == nil {
		t.Fatal("a 401 without challenge must produce an error")
	}
	if kind, ok := froniuserr.KindOf(err); !ok || kind != froniuserr.KindUnauthorized {
		t.Fatalf("nature = %q (ok=%v), want %q", kind, ok, froniuserr.KindUnauthorized)
	}
	if !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("error = %v, want ErrNoChallenge", err)
	}
	if total, _ := d.counts(); total != 1 {
		t.Fatalf("sends = %d, want 1 (no retry without challenge)", total)
	}
}

func TestPersistentUnauthorizedStopsAfterTwoRequests(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	_, err := get(t, tr, d.srv.URL+"/status/version")
	if err == nil {
		t.Fatal("a persistent 401 must produce an error")
	}
	if !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("error = %v, want ErrRetryExhausted", err)
	}
	if total, _ := d.counts(); total != 2 {
		t.Fatalf("sends = %d, want 2 (no third attempt)", total)
	}
}

// TestRefusedAfterChallengeIsNotReportedAsAbsent: the device challenges the bare
// request, then answers 404 to the authenticated one. The path exists (a bare
// 404 challenges nobody), so the final status must not read as "endpoint
// absent": the sequence itself is the fact to report.
func TestRefusedAfterChallengeIsNotReportedAsAbsent(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if !d.accept(t, r) {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		http.NotFound(w, r)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	_, err := get(t, tr, d.srv.URL+"/status/version")
	if err == nil {
		t.Fatal("a refusal after the challenge must produce an error")
	}
	if !errors.Is(err, froniuserr.ErrRefusedAfterChallenge) {
		t.Fatalf("error = %v, want ErrRefusedAfterChallenge", err)
	}
	var typed *froniuserr.Error
	if !errors.As(err, &typed) {
		t.Fatalf("errors.As did not find *froniuserr.Error: %v", err)
	}
	if typed.Kind != froniuserr.KindUnauthorized || typed.HTTPStatus != http.StatusNotFound {
		t.Fatalf("error = %+v, want KindUnauthorized with HTTP 404", typed)
	}
	if total, _ := d.counts(); total != 2 {
		t.Fatalf("sends = %d, want 2 (bare, then authenticated)", total)
	}
}

// TestBareNotFoundIsPassedThrough: without any challenge, a 404 is a genuine
// absent endpoint and must reach the caller untouched, with no retry.
func TestBareNotFoundIsPassedThrough(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("a bare 404 must not be turned into an error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if total, _ := d.counts(); total != 1 {
		t.Fatalf("sends = %d, want 1 (no challenge, hence no retry)", total)
	}
}

func TestStandardChallengeHeaderIsRead(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if !d.accept(t, r) {
			challenge(w, "www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)
	if total, auth := d.counts(); total != 2 || auth != 1 {
		t.Fatalf("requests = %d of which %d authenticated", total, auth)
	}
}

func TestNonStandardHeaderWins(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			_, _ = io.WriteString(w, "ok")
			return
		}
		// The non-standard challenge is valid; the standard one is incomplete.
		w.Header().Set("x-www-authenticate", `Digest realm="`+testRealm+`", nonce="`+testNonce+`", qop="auth"`)
		w.Header().Set("www-authenticate", `Digest realm="other"`)
		w.WriteHeader(http.StatusUnauthorized)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = readBody(t, resp)
	if total, _ := d.counts(); total != 2 {
		t.Fatalf("sends = %d, want 2 (non-standard challenge kept)", total)
	}
}

func TestOpaqueIsEchoed(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "sim-opaque", false)
			return
		}
		if got := parseParams(r.Header.Get("Authorization"))["opaque"]; got != "sim-opaque" {
			t.Errorf("returned opaque = %q", got)
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = readBody(t, resp)
}

func TestNoQopChallengeUsesRFC2069(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "", "", false)
			return
		}
		header := r.Header.Get("Authorization")
		if strings.Contains(header, "qop") || strings.Contains(header, "nc=") {
			t.Errorf("challenge without qop: the header must carry neither qop nor nc: %s", header)
		}
		if !d.accept(t, r) {
			t.Errorf("digest refused: %s", header)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)
}

func TestNoCredentialsSendsRequestUnchanged(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header emitted without credentials: %q", got)
		}
		w.Header().Set("x-www-authenticate", `Digest realm="r", nonce="n"`)
		w.WriteHeader(http.StatusUnauthorized)
	})

	tr := newTransport(t, d.srv.URL, Credentials{})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 passed through as-is", resp.StatusCode)
	}
	// Without credentials: a single send, no authentication, no retry.
	if total, auth := d.counts(); total != 1 || auth != 0 {
		t.Fatalf("requests = %d of which %d authenticated, want 1 of which 0", total, auth)
	}
}

func TestForeignHostIsRefusedWithoutSending(t *testing.T) {
	var called bool
	tr, err := New(doerFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	}), "http://192.0.2.10", Credentials{Username: "sim", Password: secret.New("x")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8081/status/version", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	_, err = tr.Do(req)
	if !errors.Is(err, ErrForeignHost) {
		t.Fatalf("error = %v, want ErrForeignHost", err)
	}
	if called {
		t.Fatal("the underlying transport must not be called for a foreign host")
	}
	// A programming guard is not a device failure: it is not classified
	// (KindOf returns ok=false), and this is documented.
	if _, ok := froniuserr.KindOf(err); ok {
		t.Fatalf("ErrForeignHost must not be a froniuserr.Error: %v", err)
	}
}

func TestNilRequestIsRejected(t *testing.T) {
	tr := newTransport(t, "http://127.0.0.1:8081", Credentials{Username: "sim", Password: secret.New("x")})
	if _, err := tr.Do(nil); err == nil {
		t.Fatal("a nil request must be refused")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestCnonceIsRandomHex(t *testing.T) {
	tr := newTransport(t, "http://127.0.0.1:8081", Credentials{Username: "sim", Password: secret.New("x")})

	first := tr.newCnonce()
	second := tr.newCnonce()

	if first == "NaN" || second == "NaN" {
		t.Fatal("the cnonce=\"NaN\" bug of the official client must not be reproduced")
	}
	if len(first) != 16 || len(second) != 16 {
		t.Fatalf("cnonce = %q / %q, want 16 hexadecimal characters", first, second)
	}
	for _, c := range first + second {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("non-hexadecimal cnonce: %q", first+second)
		}
	}
	if first == second {
		t.Fatal("two identical cnonce: the random source is not used")
	}
}

// TestConcurrentRequests: the retry counter and the cache are shared; this test
// is designed to be run under -race.
func TestConcurrentRequests(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if !d.accept(t, r) {
			challenge(w, "x-www-authenticate", testRealm, d.nonce(), "auth", "", false)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})

	const workers = 50
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := get(t, tr, d.srv.URL+"/status/version")
			if err != nil {
				errs <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent request: %v", err)
	}

	ncs := d.nonceCounts()
	if len(ncs) == 0 {
		t.Fatal("no authenticated request")
	}
	seen := map[string]bool{}
	for _, nc := range ncs {
		if seen[nc] {
			t.Fatalf("nc reused: %s (counters must be unique)", nc)
		}
		seen[nc] = true
	}
}
