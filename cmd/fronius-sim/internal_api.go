// Internal endpoints of the Fronius Webinterface, served by the simulator.
//
// Two distinct HTTP routes lead to the same internal paths (see
// docs/SOURCES.md, modules `aYRN` and `yTJA`): the polled registry of the
// official client sends an Authorization header, the status service reads the same
// paths without a header. The simulator reproduces both: without --api-user the
// four paths respond without authentication, with --api-user they require
// a verified Digest and reject replay (strictly increasing nc).
//
// All the bodies are deterministic: the nonce appears only in the header of the
// challenge, never in the body, and no timestamp appears there.
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

// Internal paths served. The list is strictly equal to that of the client
// (internal/fronius/internalapi): a path that is not here does not exist.
const (
	versionPath      = "/status/version"
	activeEventsPath = "/status/activeEvents"
	powerMeterPath   = "/components/PowerMeter/readable"
	batteryPath      = "/components/BatteryManagementSystem/readable"
)

// internalPaths is the frozen list of internal paths.
var internalPaths = []string{versionPath, activeEventsPath, powerMeterPath, batteryPath}

// Challenge headers.
const (
	nonStandardChallengeHeader = "x-www-authenticate"
	standardChallengeHeader    = "www-authenticate"
)

// simulatedSerial is an obviously fake identifier: the serial number
// observed on the real installation must never enter the repository
// (internal/fronius/solarapi/contract_test.go forbids it).
const simulatedSerial = "SIMULATED-0001"

// Faults specific to the internal paths.
const (
	faultNoChallenge  = "401-no-challenge"
	faultStandardOnly = "401-standard-only"
	faultNoQop        = "401-no-qop"
	faultOpaque       = "401-opaque"
	faultStaleNonce   = "401-stale-nonce"
	faultBadDigest    = "401-bad-digest"
	// recentNonceWindow bounds the memory of the nonces issued.
	recentNonceWindow = 8
)

// digestAuth carries the state of the simulator's Digest authentication.
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

// registerInternal registers the four internal paths.
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
// Response bodies
// ---------------------------------------------------------------------------

// versionJSON is the shape of /status/version.
//
// `serialNumber`, `devicegroup` and `numberOfPhases` are the only keys that the
// bundle establishes (docs/SOURCES.md); `firmwareVersion` and `modelName` are
// CANDIDATES specific to the simulator, with obviously fake values, that
// exist only to exercise the client's tolerant reading.
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
		ModelName:       "Fronius Primo GEN24 3.6 Plus (simulated)",
	}
}

// cgiJSON is the `Cgi` envelope: the payload is Body.Data, an object
// indexed by component id.
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

// componentData returns the content of Body.Data for a component.
//
// The indexing key is that of the component (here "0"): it is the only shape
// that the client exploits, and an empty map means "component absent".
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
			Message:  fmt.Sprintf("Simulated event %d", i),
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Digest authentication
// ---------------------------------------------------------------------------

// challengeOptions describes the challenge to write.
type challengeOptions struct {
	header string // header that carries the challenge
	qop    string // "" = no qop (RFC 2069 form)
	opaque string
	stale  bool
}

// authorize checks the Authorization header and writes a 401 otherwise.
//
// Returns true when the request can be served.
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
		// Any header is rejected, even a correct one.
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

// writeChallenge writes a 401 carrying a fresh challenge.
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
		// Serves to authenticate a third-party client (curl --digest), which reads
		// only the standard header.
		w.Header().Set(standardChallengeHeader, b.String())
	}
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "Unauthorized")
}

// nextNonce issues a fresh nonce and remembers that it was issued.
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

// acceptNonceCount requires a strictly increasing counter for a same nonce:
// that is the protection against replay, and it genuinely exercises the client's
// counter.
func (s *simulator) acceptNonceCount(params map[string]string) bool {
	nc := params["nc"]
	if nc == "" {
		return true // RFC 2069 form, without a counter
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

// issueStaleChallenge returns true only once: the first authenticated
// attempt receives a 401 stale=true, the following ones are accepted.
func (s *simulator) issueStaleChallenge() bool {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	if s.auth.staleIssued {
		return false
	}
	s.auth.staleIssued = true
	return true
}

// verifyDigest recomputes the expected response and compares it.
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

// parseDigestHeader splits an Authorization header into key/value pairs.
//
// Local implementation deliberately independent of
// internal/fronius/digest: the simulator must verify the client, not share
// its parsing — otherwise a format error would go unnoticed on both sides.
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
