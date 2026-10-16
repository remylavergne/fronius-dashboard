// Endpoints internes du Webinterface Fronius, servis par le simulateur.
//
// Deux voies HTTP distinctes mènent aux mêmes chemins internes (voir
// docs/SOURCES.md, modules `aYRN` et `yTJA`) : le registre pollé du client
// officiel envoie un en-tête Authorization, le service d'état lit les mêmes
// chemins sans en-tête. Le simulateur reproduit les deux : sans --api-user les
// quatre chemins répondent sans authentification, avec --api-user ils exigent
// un Digest vérifié et rejettent le rejeu (nc strictement croissant).
//
// Tous les corps sont déterministes : le nonce ne figure que dans l'en-tête du
// défi, jamais dans le corps, et aucun horodatage n'y apparaît.
package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Chemins internes servis. La liste est strictement égale à celle du client
// (internal/fronius/internalapi) : un chemin qui n'est pas ici n'existe pas.
const (
	versionPath      = "/status/version"
	activeEventsPath = "/status/activeEvents"
	powerMeterPath   = "/components/PowerMeter/readable"
	batteryPath      = "/components/BatteryManagementSystem/readable"
)

// internalPaths est la liste figée des chemins internes.
var internalPaths = []string{versionPath, activeEventsPath, powerMeterPath, batteryPath}

// En-têtes de défi.
const (
	nonStandardChallengeHeader = "x-www-authenticate"
	standardChallengeHeader    = "www-authenticate"
)

// simulatedSerial est un identifiant visiblement faux : le numéro de série
// observé sur l'installation réelle ne doit jamais entrer dans le dépôt
// (internal/fronius/solarapi/contract_test.go l'interdit).
const simulatedSerial = "SIMULATED-0001"

// Pannes propres aux chemins internes.
const (
	faultNoChallenge  = "401-no-challenge"
	faultStandardOnly = "401-standard-only"
	faultNoQop        = "401-no-qop"
	faultOpaque       = "401-opaque"
	faultStaleNonce   = "401-stale-nonce"
	faultBadDigest    = "401-bad-digest"
	// recentNonceWindow borne la mémoire des nonces émis.
	recentNonceWindow = 8
)

// digestAuth porte l'état de l'authentification Digest du simulateur.
type digestAuth struct {
	mu           sync.Mutex
	nonceSeq     int
	recentNonces []string
	lastNC       map[string]uint64
	staleIssued  bool
}

func newDigestAuth() *digestAuth {
	return &digestAuth{lastNC: map[string]uint64{}}
}

// registerInternal enregistre les quatre chemins internes.
func (s *simulator) registerInternal(mux *http.ServeMux) {
	for _, path := range internalPaths {
		mux.HandleFunc(path, s.logged(s.internalHandler(path)))
	}
}

func (s *simulator) internalHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		s.requests.Add(1)

		if s.genericFault(w, r, s.faultFor(r)) {
			return
		}
		if s.opt.apiUser != "" && !s.authorize(w, r) {
			return
		}

		switch path {
		case versionPath:
			writeJSON(w, http.StatusOK, s.versionBody())
		case activeEventsPath:
			writeJSON(w, http.StatusOK, cgiBody(s.activeEventsData()))
		case powerMeterPath, batteryPath:
			writeJSON(w, http.StatusOK, cgiBody(s.componentData(path)))
		default:
			http.NotFound(w, r)
		}
	}
}

// ---------------------------------------------------------------------------
// Corps des réponses
// ---------------------------------------------------------------------------

// versionJSON est la forme de /status/version.
//
// `serialNumber`, `devicegroup` et `numberOfPhases` sont les seules clés que le
// bundle établit (docs/SOURCES.md) ; `firmwareVersion` et `modelName` sont des
// CANDIDATS propres au simulateur, avec des valeurs visiblement fausses, qui
// n'existent que pour exercer la tolérance de lecture du client.
type versionJSON struct {
	SerialNumber    string `json:"serialNumber"`
	DeviceGroup     string `json:"devicegroup"`
	NumberOfPhases  int    `json:"numberOfPhases"`
	FirmwareVersion string `json:"firmwareVersion"`
	ModelName       string `json:"modelName"`
}

func (s *simulator) versionBody() versionJSON {
	return versionJSON{
		SerialNumber:    simulatedSerial,
		DeviceGroup:     "GEN24PRIMO",
		NumberOfPhases:  3,
		FirmwareVersion: "1.24.3-sim",
		ModelName:       "Fronius Primo GEN24 3.6 Plus (simulé)",
	}
}

// cgiJSON est l'enveloppe de type `Cgi` : la charge utile est Body.Data, objet
// indexé par identifiant de composant.
type cgiJSON struct {
	Body struct {
		Data map[string]any `json:"Data"`
	} `json:"Body"`
}

func cgiBody(data map[string]any) cgiJSON {
	var out cgiJSON
	out.Body.Data = data
	return out
}

// componentData retourne le contenu de Body.Data pour un composant.
//
// La clé d'index est celle du composant (ici « 0 ») : c'est la seule forme que
// le client exploite, et une carte vide signifie « composant absent ».
func (s *simulator) componentData(path string) map[string]any {
	present, serial := false, ""
	switch path {
	case powerMeterPath:
		present, serial = s.opt.withMeter, "SIM-METER-0"
	case batteryPath:
		present, serial = s.opt.withBattery, "SIM-BATTERY-0"
	}
	if !present {
		return map[string]any{}
	}
	return map[string]any{"0": map[string]any{"Serial": serial}}
}

type deviceEventJSON struct {
	Code     string `json:"code"`
	State    string `json:"state"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func (s *simulator) activeEventsData() map[string]any {
	out := map[string]any{}
	for i := range s.opt.deviceEvents {
		out[strconv.Itoa(i)] = deviceEventJSON{
			Code:     fmt.Sprintf("SIM-%d", i),
			State:    "active",
			Severity: "warning",
			Message:  fmt.Sprintf("Événement simulé %d", i),
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Authentification Digest
// ---------------------------------------------------------------------------

// challengeOptions décrit le défi à écrire.
type challengeOptions struct {
	header string // en-tête qui porte le défi
	qop    string // "" = pas de qop (forme RFC 2069)
	opaque string
	stale  bool
}

// authorize vérifie l'en-tête Authorization et écrit un 401 sinon.
//
// Retourne true quand la requête peut être servie.
func (s *simulator) authorize(w http.ResponseWriter, r *http.Request) bool {
	fault := s.faultFor(r)

	switch fault {
	case faultNoChallenge:
		w.WriteHeader(http.StatusUnauthorized)
		return false
	case faultStandardOnly:
		s.writeChallenge(w, challengeOptions{header: standardChallengeHeader, qop: "auth"})
		return false
	case faultBadDigest:
		// Tout en-tête est rejeté, même correct.
		s.writeChallenge(w, challengeOptions{header: nonStandardChallengeHeader, qop: "auth"})
		return false
	}

	opts := challengeOptions{header: nonStandardChallengeHeader, qop: "auth"}
	switch fault {
	case faultNoQop:
		opts.qop = ""
	case faultOpaque:
		opts.opaque = "sim-opaque"
	}

	header := r.Header.Get("Authorization")
	if header == "" {
		s.writeChallenge(w, opts)
		return false
	}
	params := parseDigestHeader(header)
	if !s.verifyDigest(r, params) || !s.acceptNonceCount(params) {
		s.writeChallenge(w, opts)
		return false
	}
	if fault == faultStaleNonce && s.issueStaleChallenge() {
		opts.stale = true
		s.writeChallenge(w, opts)
		return false
	}
	return true
}

// writeChallenge écrit un 401 portant un défi neuf.
func (s *simulator) writeChallenge(w http.ResponseWriter, opts challengeOptions) {
	nonce := s.nextNonce()

	var b strings.Builder
	fmt.Fprintf(&b, `Digest realm="%s", nonce="%s"`, s.opt.challengeRealm, nonce)
	if opts.qop != "" {
		fmt.Fprintf(&b, `, qop="%s"`, opts.qop)
	}
	if opts.opaque != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, opts.opaque)
	}
	if opts.stale {
		b.WriteString(`, stale=true`)
	}
	w.Header().Set(opts.header, b.String())
	if s.opt.emitStandardChallenge && opts.header != standardChallengeHeader {
		// Sert à faire authentifier un client tiers (curl --digest), qui ne lit
		// que l'en-tête standard.
		w.Header().Set(standardChallengeHeader, b.String())
	}
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "Unauthorized")
}

// nextNonce émet un nonce neuf et mémorise qu'il a été émis.
func (s *simulator) nextNonce() string {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	s.auth.nonceSeq++
	nonce := fmt.Sprintf("sim-nonce-%d", s.auth.nonceSeq)
	s.auth.recentNonces = append(s.auth.recentNonces, nonce)
	if len(s.auth.recentNonces) > recentNonceWindow {
		s.auth.recentNonces = s.auth.recentNonces[len(s.auth.recentNonces)-recentNonceWindow:]
	}
	return nonce
}

func (s *simulator) nonceWasIssued(nonce string) bool {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	for _, n := range s.auth.recentNonces {
		if n == nonce {
			return true
		}
	}
	return false
}

// acceptNonceCount exige un compteur strictement croissant pour un même nonce :
// c'est la protection contre le rejeu, et elle exerce réellement le compteur du
// client.
func (s *simulator) acceptNonceCount(params map[string]string) bool {
	nc := params["nc"]
	if nc == "" {
		return true // forme RFC 2069, sans compteur
	}
	value, err := strconv.ParseUint(nc, 10, 64)
	if err != nil {
		return false
	}
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	if last, ok := s.auth.lastNC[params["nonce"]]; ok && value <= last {
		return false
	}
	s.auth.lastNC[params["nonce"]] = value
	return true
}

// issueStaleChallenge retourne true une seule fois : la première tentative
// authentifiée reçoit un 401 stale=true, les suivantes sont acceptées.
func (s *simulator) issueStaleChallenge() bool {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	if s.auth.staleIssued {
		return false
	}
	s.auth.staleIssued = true
	return true
}

// verifyDigest recalcule la réponse attendue et la compare.
func (s *simulator) verifyDigest(r *http.Request, params map[string]string) bool {
	if params["username"] != s.opt.apiUser || params["realm"] != s.opt.challengeRealm {
		return false
	}
	if !s.nonceWasIssued(params["nonce"]) {
		return false
	}
	if params["uri"] != r.URL.RequestURI() {
		return false
	}
	if params["qop"] != "" && params["qop"] != "auth" {
		return false
	}

	ha1 := simMD5(s.opt.apiUser + ":" + params["realm"] + ":" + s.opt.apiPassword)
	ha2 := simMD5(r.Method + ":" + params["uri"])

	input := ha1 + ":" + params["nonce"] + ":" + ha2
	if params["qop"] != "" {
		input = ha1 + ":" + params["nonce"] + ":" + params["nc"] + ":" +
			params["cnonce"] + ":" + params["qop"] + ":" + ha2
	}
	return simMD5(input) == params["response"]
}

// parseDigestHeader découpe un en-tête Authorization en paires clé/valeur.
//
// Implémentation locale et volontairement indépendante de
// internal/fronius/digest : le simulateur doit vérifier le client, non partager
// son analyse — sinon une erreur de format passerait inaperçue des deux côtés.
func parseDigestHeader(header string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(header, ", ") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(strings.TrimPrefix(key, "Digest "))] = strings.Trim(value, `"'`)
	}
	return out
}

func simMD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
