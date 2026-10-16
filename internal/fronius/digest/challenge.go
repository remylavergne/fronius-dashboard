// Digest challenge (401 header) — parsing.
//
// The parsing reproduces that of the official client (bundle module `xeXE`,
// references/fronius-original.js l. 182391-182440): split on the comma, then on
// each fragment the pattern /(\w+)=["']?([^'"]+)["']?/, then strip the quotes.
// The scheme that precedes it ("Digest") is never read: the pattern is not
// anchored, it finds the first key=value pair wherever it is.
package digest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Challenge is the challenge extracted from a 401 response.
type Challenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Qop       string // first qop= token; "" if the challenge carries none
	Algorithm string // "" or "MD5"
	Stale     bool
}

var (
	// ErrNoChallenge: the 401 response carries no challenge header.
	ErrNoChallenge = errors.New("digest: no challenge in the 401 response")
	// ErrIncompleteChallenge: realm and nonce are required (the bundle then
	// produces an empty header rather than authenticating).
	ErrIncompleteChallenge = errors.New("digest: incomplete challenge (realm and nonce are required)")
	// ErrUnsupportedAlgorithm: only MD5 is specified by the official client.
	ErrUnsupportedAlgorithm = errors.New("digest: unsupported algorithm")
)

// authKeyValueRE is the AUTH_KEY_VALUE_RE pattern from the bundle.
var authKeyValueRE = regexp.MustCompile(`(\w+)=["']?([^'"]+)["']?`)

// quoteStripper strips the quotes from values, like `.replace(/["']/g,"")`.
var quoteStripper = strings.NewReplacer(`"`, "", `'`, "")

// ParseChallenge parses a challenge header.
//
// An empty header or one without a key=value pair yields ErrNoChallenge. An
// empty realm or nonce yields ErrIncompleteChallenge. An algorithm other than
// empty or MD5 yields ErrUnsupportedAlgorithm, naming the received value.
func ParseChallenge(header string) (Challenge, error) {
	if strings.TrimSpace(header) == "" {
		return Challenge{}, ErrNoChallenge
	}

	values := map[string]string{}
	for _, part := range strings.Split(header, ",") {
		m := authKeyValueRE.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		// The bundle stores the key as-is; we fold it to lower case, which can
		// only accept a more permissive server.
		values[strings.ToLower(m[1])] = quoteStripper.Replace(m[2])
	}
	if len(values) == 0 {
		return Challenge{}, ErrNoChallenge
	}

	ch := Challenge{
		Realm:     values["realm"],
		Nonce:     values["nonce"],
		Opaque:    values["opaque"],
		Qop:       values["qop"],
		Algorithm: values["algorithm"],
		Stale:     strings.EqualFold(values["stale"], "true"),
	}
	if ch.Realm == "" || ch.Nonce == "" {
		return Challenge{}, ErrIncompleteChallenge
	}
	if ch.Algorithm != "" && !strings.EqualFold(ch.Algorithm, "MD5") {
		return Challenge{}, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, ch.Algorithm)
	}
	// qop may list several tokens (qop="auth,auth-int"): the bundle keeps only
	// the first (l = l.split(",")[0]).
	if i := strings.Index(ch.Qop, ","); i >= 0 {
		ch.Qop = ch.Qop[:i]
	}
	return ch, nil
}
