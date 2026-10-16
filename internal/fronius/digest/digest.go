// Transport HTTP Digest (RFC 2617) en lecture seule, en réponse à un 401.
//
// NOTE MD5 : `crypto/md5` est imposé par le protocole, pas choisi. Le client
// officiel du fabricant calcule exactement HA1/HA2/response avec MD5 (module
// `xeXE` du bundle) ; c'est la seule fonction que l'appareil accepte. Ce n'est
// donc pas une faiblesse introduite ici, et il n'existe aucune alternative.
//
// Invariants (voir docs/ARCHITECTURE.md) :
//   - aucun en-tête Authorization n'est envoyé préemptivement : le transport
//     envoie d'abord la requête nue, et ne s'authentifie qu'en réponse à un 401 ;
//   - l'en-tête n'est jamais envoyé vers un hôte autre que celui de l'URL de
//     base configurée ;
//   - le mot de passe ne sort du type secret.String que pour le calcul de HA1 ;
//   - le corps de toute réponse abandonnée est fermé (sinon la connexion fuit).
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

// ErrForeignHost : la requête ne vise pas l'hôte de l'URL de base. Garde de
// programmation, pas panne d'appareil.
var ErrForeignHost = errors.New("digest: hôte hors du périmètre configuré")

// ErrRetryExhausted : la seconde tentative authentifiée a encore reçu un 401.
// Aucune troisième tentative n'est faite.
var ErrRetryExhausted = errors.New("digest: authentification refusée après la seconde tentative")

// challengeHeaders liste les en-têtes où chercher le défi, dans l'ordre.
//
// L'en-tête non standard « x-www-authenticate » vient en premier : c'est celui
// que lit le client officiel (sept occurrences dans le bundle, aucune de
// « www-authenticate » seul).
var challengeHeaders = []string{"x-www-authenticate", "www-authenticate"}

// Credentials est une paire d'identifiants de l'appareil.
type Credentials struct {
	Username string
	Password secret.String
}

// IsZero indique qu'aucune authentification ne peut être tentée : une paire
// incomplète n'authentifie rien, elle est traitée comme absente plutôt que
// d'émettre un en-tête voué au 401.
func (c Credentials) IsZero() bool { return c.Username == "" || c.Password.IsZero() }

// nonceEntry est un défi accepté, avec son compteur de répétition.
type nonceEntry struct {
	challenge Challenge
	nc        uint32
}

// Transport authentifie les requêtes en Digest après un premier 401.
type Transport struct {
	Inner fronius.Doer
	Cred  Credentials
	Log   *slog.Logger
	// Rand alimente le cnonce. Défaut : crypto/rand.Reader.
	Rand io.Reader

	baseHost string

	mu    sync.Mutex
	cache map[string]nonceEntry // clé : hôte + "\x00" + realm
}

// New construit le transport. inner peut être nil (un client HTTP par défaut
// est alors utilisé) ; baseURL fixe le seul hôte vers lequel l'en-tête
// Authorization peut être émis.
func New(inner fronius.Doer, baseURL string, cred Credentials, log *slog.Logger) (*Transport, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("URL de base %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL de base %q : schéma http(s) attendu", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL de base %q : hôte manquant", baseURL)
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

// Do envoie req, en n'ajoutant l'en-tête Authorization qu'en réponse à un 401.
//
// req doit être une requête GET sans corps : le corps n'est lu qu'une fois par
// envoi, et la réémission réutilise la même requête. Cette contrainte est
// garantie par fronius.ReadOnly, placé devant ce transport.
func (t *Transport) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("digest: requête nulle")
	}
	if req.URL == nil || req.URL.Host != t.baseHost {
		host := ""
		if req.URL != nil {
			host = req.URL.Host
		}
		return nil, fmt.Errorf("%w : %q, périmètre %q", ErrForeignHost, host, t.baseHost)
	}
	if t.Cred.IsZero() {
		// Phase 1 préservée : aucun identifiant, aucun en-tête, aucun retry.
		return t.Inner.Do(req)
	}

	// Tentative avec le défi déjà connu, s'il y en a un pour cet hôte.
	if entry, key, ok := t.cachedEntry(req.URL.Host); ok {
		resp, err := t.sendWithChallenge(req, entry)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		// Nonce périmé : on jette cette réponse et on repart sur le chemin
		// complet (requête nue → 401 → défi frais → réémission unique).
		drain(resp)
		t.forget(key)
	}

	// Requête nue : c'est le cas normal des chemins internes qui répondent
	// sans authentification (module yTJA du bundle).
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
	return retried, nil
}

// sendWithChallenge clone req, l'authentifie et l'envoie.
func (t *Transport) sendWithChallenge(req *http.Request, entry nonceEntry) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", t.headerFor(clone, entry))
	return t.Inner.Do(clone)
}

// headerFor construit l'en-tête Authorization et incrémente le compteur de
// répétition de l'entrée sous verrou.
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

// cachedEntry retourne l'entrée connue pour cet hôte, si elle existe.
//
// L'hôte est le seul axe de recherche : baseHost est le seul hôte autorisé, donc
// l'index contient au plus un défi vivant, et un changement de realm côté
// appareil se traduit par un 401 puis une réauthentification.
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

// remember mémorise un défi frais et retourne l'entrée à utiliser.
//
// Le compteur de répétition n'est conservé que si le nonce est inchangé : il
// compte les envois faits avec CE nonce, et le remettre à zéro sur un nonce
// neuf évite de réutiliser un numéro déjà employé auprès d'un autre défi.
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
		// Jamais de cnonce constant : un échec de la source aléatoire est une
		// panne, pas une raison d'émettre une valeur fixe.
		t.Log.Error("digest: lecture du générateur aléatoire impossible", "error", err)
		return ""
	}
	return hex.EncodeToString(buf)
}

// cacheKey compose la clé d'une entrée de cache.
func cacheKey(host, realm string) string { return host + "\x00" + realm }

// drain vide et ferme un corps de réponse qu'on abandonne.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// headerValue construit la valeur de l'en-tête Authorization.
//
// Fonction pure : elle seule fixe la forme exacte de l'en-tête, identique à
// celle du client officiel (module xeXE, l. 182425-182428) :
//
//	Digest username="U", realm="R", nonce="N", uri="URI", response="RESP"
//	[, opaque="O"] [, qop=QOP, nc=NC, cnonce="CNONCE"]
//
// qop et nc ne sont pas entre guillemets, cnonce l'est, et « algorithm » n'est
// jamais émis — l'appareil ne le demande pas et le client officiel ne l'envoie
// pas non plus. nc est décimal zéro-complété sur huit chiffres, comme le
// compteur du client officiel (String(++NC) complété par NC_PAD).
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

// withoutQuotes retire les guillemets d'une valeur insérée entre guillemets :
// un guillemet dans un realm ou un nonce casserait l'en-tête. C'est le nettoyage
// que le bundle applique déjà aux valeurs du défi.
func withoutQuotes(s string) string { return quoteStripper.Replace(s) }

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
