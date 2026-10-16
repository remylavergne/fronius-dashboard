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
// Vérificateur indépendant : n'utilise PAS le code de production.
// ---------------------------------------------------------------------------

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// parseParams découpe un en-tête d'authentification en paires clé/valeur.
//
// Implémentation volontairement indépendante de ParseChallenge : elle sert à
// vérifier l'en-tête produit, une vérification circulaire n'aurait aucune valeur.
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

// expectedResponse recalcule la réponse attendue d'après l'en-tête reçu.
func expectedResponse(method, user, password, realm string, params map[string]string) string {
	ha1 := md5hex(user + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + params["uri"])
	input := ha1 + ":" + params["nonce"]
	if qop := params["qop"]; qop != "" {
		input += ":" + params["nc"] + ":" + params["cnonce"] + ":" + qop
	}
	return md5hex(input + ":" + ha2)
}

// device est un serveur de test qui vérifie le Digest et compte les requêtes.
type device struct {
	user, password, realm string
	// monotonic active la vérification « nc strictement croissant », comme un
	// serveur réel protégé contre le rejeu. Désactivé pour le test concurrent :
	// l'ordre d'arrivée des requêtes y est indéterminé.
	monotonic bool
	mu        sync.Mutex
	requests  int
	// authenticated compte les requêtes dont le Digest a été accepté.
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

// nonce retourne le nonce courant du serveur de test.
func (d *device) nonce() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.currentNonce
}

// setNonce remplace le nonce courant : toute requête portant l'ancien est
// refusée comme périmée.
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

// accept vérifie l'en-tête Authorization d'une requête déjà authentifiée.
//
// Rejoue les vérifications d'un serveur réel : réponse recalculée, et compteur
// de répétition strictement croissant pour un même nonce.
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

	// Sans qop, l'en-tête suit la forme RFC 2069 : ni nc ni cnonce.
	d.mu.Lock()
	defer d.mu.Unlock()
	if nc := params["nc"]; nc != "" {
		v, err := parseUint(nc)
		if err != nil {
			return false
		}
		if last, ok := d.lastNC[params["nonce"]]; d.monotonic && ok && v <= last {
			return false // rejeu : le compteur doit croître strictement
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
		return 0, errors.New("vide")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non décimal: %q", s)
		}
		v = v*10 + uint64(r-'0')
	}
	return v, nil
}

// challenge écrit un défi 401 dans l'en-tête non standard du fabricant.
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
// Vecteur RFC 2617 : l'ancre de correction du calcul.
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
		t.Fatalf("en-tête =\n%s\nattendu\n%s", got, want)
	}
}

// TestRFC2069Vector : sans qop, la réponse est celle de la forme RFC 2069
// (MD5(HA1:nonce:HA2)), et ni qop, ni nc, ni cnonce n'apparaissent.
func TestRFC2069Vector(t *testing.T) {
	cred := Credentials{Username: "Mufasa", Password: secret.New("Circle Of Life")}
	ch := Challenge{Realm: "testrealm@host.com", Nonce: "dcd98b7102dd2f0e8b11d0f600bfb0c093"}

	got := headerValue(ch, "GET", "/dir/index.html", cred, 1, "0a4f113b")
	want := md5hex(md5hex("Mufasa:testrealm@host.com:Circle Of Life") +
		":dcd98b7102dd2f0e8b11d0f600bfb0c093:" +
		md5hex("GET:/dir/index.html"))

	if !strings.Contains(got, `response="`+want+`"`) {
		t.Fatalf("en-tête = %s\nréponse attendue %s", got, want)
	}
	for _, absent := range []string{"qop", "nc=", "cnonce"} {
		if strings.Contains(got, absent) {
			t.Fatalf("%q ne doit pas apparaître sans qop : %s", absent, got)
		}
	}
}

func TestHeaderShape(t *testing.T) {
	cred := Credentials{Username: "sim", Password: secret.New("p")}
	ch := Challenge{Realm: "r", Nonce: "n", Qop: "auth", Opaque: "op"}

	got := headerValue(ch, "GET", "/status/version", cred, 7, "0123456789abcdef")

	// Ordre exact des champs.
	order := []string{`username="sim"`, `realm="r"`, `nonce="n"`, `uri="/status/version"`, `response="`, `opaque="op"`, `qop=auth`, `nc=00000007`, `cnonce="0123456789abcdef"`}
	last := -1
	for _, field := range order {
		i := strings.Index(got, field)
		if i < 0 {
			t.Fatalf("champ %q absent de %s", field, got)
		}
		if i < last {
			t.Fatalf("champ %q hors ordre dans %s", field, got)
		}
		last = i
	}
	// qop et nc sans guillemets, cnonce entre guillemets.
	if strings.Contains(got, `qop="`) || strings.Contains(got, `nc="`) {
		t.Fatalf("qop et nc ne doivent pas être entre guillemets : %s", got)
	}
	if !strings.Contains(got, `cnonce="`) {
		t.Fatalf("cnonce doit être entre guillemets : %s", got)
	}
	// « algorithm » n'est jamais émis.
	if strings.Contains(strings.ToLower(got), "algorithm") {
		t.Fatalf("algorithm ne doit pas être émis : %s", got)
	}
	// nc : huit chiffres décimaux zéro-complétés.
	if !strings.Contains(got, "nc=00000007") {
		t.Fatalf("nc mal formé : %s", got)
	}
}

func TestHeaderValueStripsQuotes(t *testing.T) {
	cred := Credentials{Username: `sim"x`, Password: secret.New("p")}
	ch := Challenge{Realm: `re"alm`, Nonce: `no'nce`, Qop: "auth"}

	got := headerValue(ch, "GET", "/x", cred, 1, "abc")
	for _, bad := range []string{`re"alm`, `no'nce`, `sim"x`} {
		if strings.Contains(got, bad) {
			t.Fatalf("guillemet non retiré : %s", got)
		}
	}
	if strings.Count(got, `"`)%2 != 0 {
		t.Fatalf("guillemets déséquilibrés : %s", got)
	}
}

// ---------------------------------------------------------------------------
// Comportement réseau
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
		t.Fatalf("corps = %s", body)
	}

	total, auth := d.counts()
	if total != 2 || auth != 1 {
		t.Fatalf("requêtes = %d dont %d authentifiées, attendu 2 dont 1", total, auth)
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
		t.Fatalf("seconde requête : %d envois, attendu 1 (nonce en cache)", after-before)
	}
	if auth != 2 {
		t.Fatalf("requêtes authentifiées = %d, attendu 2", auth)
	}

	// Le compteur de répétition augmente de 1 à chaque envoi authentifié.
	ncs := d.nonceCounts()
	if len(ncs) != 2 || ncs[0] != "00000001" || ncs[1] != "00000002" {
		t.Fatalf("nc observés = %v, attendu [00000001 00000002]", ncs)
	}
}

// TestStaleCachedNonceRecoversInOneCall : le nonce en cache est refusé par
// l'appareil ; le transport repart de la requête nue, obtient un défi frais et
// réussit, sans troisième tentative.
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
			// Nonce périmé : le serveur en émet un nouveau avec stale=true.
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

	// Premier appel : défi frais, nonce-1 mis en cache.
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("premier appel : status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)

	// Le serveur tourne son nonce : le nonce en cache devient périmé.
	d.setNonce(freshNonce)

	before, _ := d.counts()
	resp, err = get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second appel : status = %d", resp.StatusCode)
	}
	_ = readBody(t, resp)
	after, _ := d.counts()

	if after-before != 3 {
		t.Fatalf("envois = %d, attendu 3 (cache refusé, requête nue, défi frais)", after-before)
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
		t.Fatal("un 401 sans défi doit produire une erreur")
	}
	if kind, ok := froniuserr.KindOf(err); !ok || kind != froniuserr.KindUnauthorized {
		t.Fatalf("nature = %q (ok=%v), attendu %q", kind, ok, froniuserr.KindUnauthorized)
	}
	if !errors.Is(err, ErrNoChallenge) {
		t.Fatalf("erreur = %v, attendu ErrNoChallenge", err)
	}
	if total, _ := d.counts(); total != 1 {
		t.Fatalf("envois = %d, attendu 1 (aucun retry sans défi)", total)
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
		t.Fatal("un 401 persistant doit produire une erreur")
	}
	if !errors.Is(err, ErrRetryExhausted) {
		t.Fatalf("erreur = %v, attendu ErrRetryExhausted", err)
	}
	if total, _ := d.counts(); total != 2 {
		t.Fatalf("envois = %d, attendu 2 (aucune troisième tentative)", total)
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
		t.Fatalf("requêtes = %d dont %d authentifiées", total, auth)
	}
}

func TestNonStandardHeaderWins(t *testing.T) {
	d := newDevice(t, testUser, testPassword, testRealm)
	d.start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			_, _ = io.WriteString(w, "ok")
			return
		}
		// Le défi non standard est valide ; celui du standard est incomplet.
		w.Header().Set("x-www-authenticate", `Digest realm="`+testRealm+`", nonce="`+testNonce+`", qop="auth"`)
		w.Header().Set("www-authenticate", `Digest realm="autre"`)
		w.WriteHeader(http.StatusUnauthorized)
	})

	tr := newTransport(t, d.srv.URL, Credentials{Username: testUser, Password: secret.New(testPassword)})
	resp, err := get(t, tr, d.srv.URL+"/status/version")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = readBody(t, resp)
	if total, _ := d.counts(); total != 2 {
		t.Fatalf("envois = %d, attendu 2 (défi non standard retenu)", total)
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
			t.Errorf("opaque renvoyé = %q", got)
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
			t.Errorf("défi sans qop : l'en-tête ne doit porter ni qop ni nc : %s", header)
		}
		if !d.accept(t, r) {
			t.Errorf("digest refusé : %s", header)
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
			t.Errorf("en-tête Authorization émis sans identifiants : %q", got)
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
		t.Fatalf("status = %d, attendu 401 transmis tel quel", resp.StatusCode)
	}
	// Sans identifiants : un seul envoi, aucune authentification, aucun retry.
	if total, auth := d.counts(); total != 1 || auth != 0 {
		t.Fatalf("requêtes = %d dont %d authentifiées, attendu 1 dont 0", total, auth)
	}
}

func TestForeignHostIsRefusedWithoutSending(t *testing.T) {
	var called bool
	tr, err := New(doerFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("ne doit pas être appelé")
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
		t.Fatalf("erreur = %v, attendu ErrForeignHost", err)
	}
	if called {
		t.Fatal("le transport sous-jacent ne doit pas être appelé pour un hôte étranger")
	}
	// Une garde de programmation n'est pas une panne d'appareil : elle n'est pas
	// classée (KindOf rend ok=false), et c'est documenté.
	if _, ok := froniuserr.KindOf(err); ok {
		t.Fatalf("ErrForeignHost ne doit pas être une froniuserr.Error : %v", err)
	}
}

func TestNilRequestIsRejected(t *testing.T) {
	tr := newTransport(t, "http://127.0.0.1:8081", Credentials{Username: "sim", Password: secret.New("x")})
	if _, err := tr.Do(nil); err == nil {
		t.Fatal("une requête nulle doit être refusée")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestCnonceIsRandomHex(t *testing.T) {
	tr := newTransport(t, "http://127.0.0.1:8081", Credentials{Username: "sim", Password: secret.New("x")})

	first := tr.newCnonce()
	second := tr.newCnonce()

	if first == "NaN" || second == "NaN" {
		t.Fatal("le bug cnonce=\"NaN\" du client officiel ne doit pas être reproduit")
	}
	if len(first) != 16 || len(second) != 16 {
		t.Fatalf("cnonce = %q / %q, attendu 16 caractères hexadécimaux", first, second)
	}
	for _, c := range first + second {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("cnonce non hexadécimal : %q", first+second)
		}
	}
	if first == second {
		t.Fatal("deux cnonce identiques : la source aléatoire n'est pas utilisée")
	}
}

// TestConcurrentRequests : le compteur de répétition et le cache sont partagés ;
// ce test est conçu pour être exécuté sous -race.
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
		t.Fatalf("requête concurrente: %v", err)
	}

	ncs := d.nonceCounts()
	if len(ncs) == 0 {
		t.Fatal("aucune requête authentifiée")
	}
	seen := map[string]bool{}
	for _, nc := range ncs {
		if seen[nc] {
			t.Fatalf("nc réutilisé : %s (les compteurs doivent être uniques)", nc)
		}
		seen[nc] = true
	}
}
