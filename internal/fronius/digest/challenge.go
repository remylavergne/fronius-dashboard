// Défi Digest (en-tête 401) — analyse.
//
// L'analyse reproduit celle du client officiel (module `xeXE` du bundle,
// references/fronius-original.js l. 182391-182440) : découpage sur la virgule,
// puis sur chaque fragment le motif /(\w+)=["']?([^'"]+)["']?/, puis retrait
// des guillemets. Le schéma qui précède (« Digest ») n'est jamais lu : le motif
// n'est pas ancré, il trouve la première paire clé=valeur où qu'elle soit.
package digest

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Challenge est le défi extrait d'une réponse 401.
type Challenge struct {
	Realm     string
	Nonce     string
	Opaque    string
	Qop       string // premier jeton de qop= ; "" si le défi n'en porte pas
	Algorithm string // "" ou "MD5"
	Stale     bool
}

var (
	// ErrNoChallenge : la réponse 401 ne porte aucun en-tête de défi.
	ErrNoChallenge = errors.New("digest: aucun défi dans la réponse 401")
	// ErrIncompleteChallenge : realm et nonce sont obligatoires (le bundle
	// produit alors un en-tête vide plutôt que d'authentifier).
	ErrIncompleteChallenge = errors.New("digest: défi incomplet (realm et nonce sont obligatoires)")
	// ErrUnsupportedAlgorithm : seul MD5 est spécifié par le client officiel.
	ErrUnsupportedAlgorithm = errors.New("digest: algorithme non pris en charge")
)

// authKeyValueRE est le motif AUTH_KEY_VALUE_RE du bundle.
var authKeyValueRE = regexp.MustCompile(`(\w+)=["']?([^'"]+)["']?`)

// quoteStripper retire les guillemets des valeurs, comme `.replace(/["']/g,"")`.
var quoteStripper = strings.NewReplacer(`"`, "", `'`, "")

// ParseChallenge analyse un en-tête de défi.
//
// Un en-tête vide ou sans paire clé=valeur donne ErrNoChallenge. Un realm ou un
// nonce vide donne ErrIncompleteChallenge. Un algorithm autre que vide ou MD5
// donne ErrUnsupportedAlgorithm, en nommant la valeur reçue.
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
		// Le bundle stocke la clé telle quelle ; on la replie en minuscules,
		// ce qui ne peut qu'accepter un serveur plus permissif.
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
	// qop peut lister plusieurs jetons (qop="auth,auth-int") : le bundle ne
	// retient que le premier (l = l.split(",")[0]).
	if i := strings.Index(ch.Qop, ","); i >= 0 {
		ch.Qop = ch.Qop[:i]
	}
	return ch, nil
}
