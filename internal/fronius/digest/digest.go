// HTTP Digest transport (RFC 2617), read-only, in response to a 401.
//
// MD5 NOTE: `crypto/md5` is imposed by the protocol, not chosen. The vendor's
// official client computes exactly HA1/HA2/response with MD5 (bundle module
// `xeXE`); it is the only function the device accepts. So this is not a
// weakness introduced here, and there is no alternative.
//
// Invariants (see docs/ARCHITECTURE.md):
//   - no Authorization header is sent preemptively: the transport first sends
//     the bare request, and only authenticates in response to a 401;
//   - the header is never sent to a host other than that of the configured
//     base URL;
//   - the password leaves the secret.String type only for the HA1 computation;
//   - the body of any abandoned response is closed (otherwise the connection
//     leaks).
package digest

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/secret"
)

// ErrForeignHost: the request does not target the base URL host. A programming
// guard, not a device failure.
var ErrForeignHost = errors.New("digest: host outside the configured scope")

// ErrRetryExhausted: the second authenticated attempt again received a 401.
// No third attempt is made.
var ErrRetryExhausted = errors.New("digest: authentication refused after the second attempt")

// challengeHeaders lists the headers to look for the challenge in, in order.
//
// The non-standard "x-www-authenticate" header comes first: it is the one the
// official client reads (seven occurrences in the bundle, none of
// "www-authenticate" alone).
var challengeHeaders = []string{"x-www-authenticate", "www-authenticate"}

// Credentials is a pair of device credentials.
type Credentials struct {
	Username string
	Password secret.String
}

// IsZero reports that no authentication can be attempted: an incomplete pair
// authenticates nothing, it is treated as absent rather than emitting a header
// doomed to a 401.
func (c Credentials) IsZero() bool { return c.Username == "" || c.Password.IsZero() }

// nonceEntry is an accepted challenge, with its retry counter.
type nonceEntry struct {
	challenge Challenge
	nc        uint32
}

// Transport authenticates requests with Digest after a first 401.
type Transport struct {
	Inner fronius.Doer
	Cred  Credentials
	Log   *slog.Logger
	// Rand feeds the cnonce. Default: crypto/rand.Reader.
	Rand io.Reader

	baseHost string

	mu    sync.Mutex
	cache map[string]nonceEntry // key: host + "\x00" + realm
}

// New builds the transport. inner may be nil (a default HTTP client is then
// used); baseURL sets the only host to which the Authorization header may be
// sent.
func New(inner fronius.Doer, baseURL string, cred Credentials, log *slog.Logger) (*Transport, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("base URL %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL %q: http(s) scheme expected", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("base URL %q: missing host", baseURL)
	}
	if inner == nil {
		inner = &http.Client{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Transport{
		Inner:    inner,
		Cred:     cred,
		Log:      log,
		Rand:     rand.Reader,
		baseHost: u.Host,
		cache:    map[string]nonceEntry{},
	}, nil
}

// Do sends req, adding the Authorization header only in response to a 401.
//
// req must be a GET request without a body: the body is read only once per
// send, and the re-send reuses the same request. This constraint is guaranteed
// by fronius.ReadOnly, placed in front of this transport.
func (t *Transport) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("digest: nil request")
	}
	if req.URL == nil || req.URL.Host != t.baseHost {
		host := ""
		if req.URL != nil {
			host = req.URL.Host
		}
		return nil, fmt.Errorf("%w: %q, scope %q", ErrForeignHost, host, t.baseHost)
	}
	if t.Cred.IsZero() {
		// Phase 1 preserved: no credentials, no header, no retry.
		return t.Inner.Do(req)
	}

	// Attempt with the already-known challenge, if there is one for this host.
	if entry, key, ok := t.cachedEntry(req.URL.Host); ok {
		resp, err := t.sendWithChallenge(req, entry)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		// Stale nonce: we discard this response and start over on the full path
		// (bare request → 401 → fresh challenge → single re-send).
		drain(resp)
		t.forget(key)
	}

	// Bare request: this is the normal case for internal paths that respond
	// without authentication (bundle module yTJA).
	resp, err := t.Inner.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	header := ""
	for _, name := range challengeHeaders {
		if v := resp.Header.Get(name); strings.TrimSpace(v) != "" {
			header = v
			break
		}
	}
	drain(resp)

	if header == "" {
		return nil, &froniuserr.Error{
			Kind:       froniuserr.KindUnauthorized,
			HTTPStatus: http.StatusUnauthorized,
			Endpoint:   req.URL.Path,
			Err:        ErrNoChallenge,
		}
	}
	ch, err := ParseChallenge(header)
	if err != nil {
		return nil, &froniuserr.Error{
			Kind:       froniuserr.KindUnauthorized,
			HTTPStatus: http.StatusUnauthorized,
			Endpoint:   req.URL.Path,
			Err:        err,
		}
	}

	entry := t.remember(req.URL.Host, ch)
	retried, err := t.sendWithChallenge(req, entry)
	if err != nil {
		return nil, err
	}
	if retried.StatusCode == http.StatusUnauthorized {
		drain(retried)
		t.forget(cacheKey(req.URL.Host, ch.Realm))
		return nil, &froniuserr.Error{
			Kind:       froniuserr.KindUnauthorized,
			HTTPStatus: http.StatusUnauthorized,
			Endpoint:   req.URL.Path,
			Err:        ErrRetryExhausted,
		}
	}
	// The device challenged us and then refused the authenticated request with
	// another client status. The path exists (a bare 404 challenges nobody), so
	// the final status alone would read as "absent endpoint" or as a generic
	// failure: the sequence is the fact to report.
	if retried.StatusCode >= 400 && retried.StatusCode < 500 {
		status := retried.StatusCode
		drain(retried)
		return nil, &froniuserr.Error{
			Kind:       froniuserr.KindUnauthorized,
			HTTPStatus: status,
			Endpoint:   req.URL.Path,
			Err:        fmt.Errorf("%w: HTTP %d after the challenge", froniuserr.ErrRefusedAfterChallenge, status),
		}
	}
	return retried, nil
}

// sendWithChallenge clones req, authenticates it and sends it.
func (t *Transport) sendWithChallenge(req *http.Request, entry nonceEntry) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", t.headerFor(clone, entry))
	return t.Inner.Do(clone)
}

// headerFor builds the Authorization header and increments the retry counter
// of the entry under lock.
func (t *Transport) headerFor(req *http.Request, entry nonceEntry) string {
	uri := req.URL.RequestURI()
	cnonce := t.newCnonce()

	key := cacheKey(req.URL.Host, entry.challenge.Realm)

	t.mu.Lock()
	if current, ok := t.cache[key]; ok {
		entry = current
	}
	entry.nc++
	t.cache[key] = entry
	t.mu.Unlock()

	return headerValue(entry.challenge, req.Method, uri, t.Cred, entry.nc, cnonce)
}

// cachedEntry returns the known entry for this host, if it exists.
//
// The host is the only search axis: baseHost is the only allowed host, so the
// index contains at most one live challenge, and a realm change on the device
// side results in a 401 followed by a re-authentication.
func (t *Transport) cachedEntry(host string) (nonceEntry, string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prefix := host + "\x00"
	for key, entry := range t.cache {
		if strings.HasPrefix(key, prefix) {
			return entry, key, true
		}
	}
	return nonceEntry{}, "", false
}

// remember stores a fresh challenge and returns the entry to use.
//
// The retry counter is kept only if the nonce is unchanged: it counts the sends
// made with THIS nonce, and resetting it on a fresh nonce avoids reusing a
// number already used with another challenge.
func (t *Transport) remember(host string, ch Challenge) nonceEntry {
	key := cacheKey(host, ch.Realm)
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.cache[key]
	if !ok || entry.challenge.Nonce != ch.Nonce {
		entry = nonceEntry{challenge: ch}
	} else {
		entry.challenge = ch
	}
	t.cache[key] = entry
	return entry
}

func (t *Transport) forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cache, key)
}

func (t *Transport) newCnonce() string {
	buf := make([]byte, 8)
	if _, err := io.ReadFull(t.Rand, buf); err != nil {
		// Never a constant cnonce: a failure of the random source is a failure,
		// not a reason to emit a fixed value.
		t.Log.Error("digest: cannot read the random generator", "error", err)
		return ""
	}
	return hex.EncodeToString(buf)
}

// cacheKey composes the key of a cache entry.
func cacheKey(host, realm string) string { return host + "\x00" + realm }

// drain empties and closes a response body that is being abandoned.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// headerValue builds the value of the Authorization header.
//
// Pure function: it alone fixes the exact form of the header, identical to that
// of the official client (module xeXE, l. 182425-182428):
//
//	Digest username="U", realm="R", nonce="N", uri="URI", response="RESP"
//	[, opaque="O"] [, qop=QOP, nc=NC, cnonce="CNONCE"]
//
// qop and nc are not quoted, cnonce is, and "algorithm" is never emitted — the
// device does not ask for it and the official client does not send it either.
// nc is decimal zero-padded to eight digits, like the official client's counter
// (String(++NC) padded by NC_PAD).
func headerValue(ch Challenge, method, uri string, cred Credentials, nc uint32, cnonce string) string {
	ha1 := md5Hex(cred.Username + ":" + ch.Realm + ":" + cred.Password.Reveal())
	ha2 := md5Hex(strings.ToUpper(method) + ":" + uri)

	input := ha1 + ":" + ch.Nonce
	ncValue := fmt.Sprintf("%08d", nc)
	if ch.Qop != "" {
		input += ":" + ncValue + ":" + cnonce + ":" + ch.Qop
	}
	response := md5Hex(input + ":" + ha2)

	var b strings.Builder
	b.WriteString(`Digest username="`)
	b.WriteString(withoutQuotes(cred.Username))
	b.WriteString(`", realm="`)
	b.WriteString(withoutQuotes(ch.Realm))
	b.WriteString(`", nonce="`)
	b.WriteString(withoutQuotes(ch.Nonce))
	b.WriteString(`", uri="`)
	b.WriteString(withoutQuotes(uri))
	b.WriteString(`", response="`)
	b.WriteString(response)
	b.WriteString(`"`)
	if ch.Opaque != "" {
		b.WriteString(`, opaque="`)
		b.WriteString(withoutQuotes(ch.Opaque))
		b.WriteString(`"`)
	}
	if ch.Qop != "" {
		b.WriteString(", qop=")
		b.WriteString(withoutQuotes(ch.Qop))
		b.WriteString(", nc=")
		b.WriteString(ncValue)
		b.WriteString(`, cnonce="`)
		b.WriteString(withoutQuotes(cnonce))
		b.WriteString(`"`)
	}
	return b.String()
}

// withoutQuotes strips the quotes from a value inserted between quotes: a quote
// in a realm or a nonce would break the header. This is the cleanup the bundle
// already applies to the challenge values.
func withoutQuotes(s string) string { return quoteStripper.Replace(s) }

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
