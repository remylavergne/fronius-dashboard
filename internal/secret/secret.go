// Package secret transporte une valeur sensible sans jamais l'exposer.
//
// Un secret ne sort en clair que par Reveal(). Toute autre mise en forme —
// fmt (%v, %s, %q, %+v, %#v, y compris pour un champ imbriqué dans une
// structure), slog et JSON — produit « <redacted> ». Une fuite dans une trace
// ou dans une réponse HTTP devient donc impossible par construction, et non
// seulement par vigilance : oublier de filtrer un champ ne suffit plus à
// divulguer la valeur.
//
// Invariant : aucun identifiant de l'appareil ne circule sous forme de string
// nue dans l'application. Le seul appel à Reveal() est le calcul de HA1 du
// Digest (internal/fronius/digest).
package secret

import "log/slog"

// redactedText est la seule représentation non révélée d'un secret.
const redactedText = "<redacted>"

// String est une chaîne sensible.
//
// Le type est comparable (comme string), donc utilisable directement dans une
// comparaison de structure : la configuration garde sa comparabilité.
type String string

// New construit un secret à partir d'une valeur en clair.
func New(v string) String { return String(v) }

// Reveal retourne la valeur en clair. C'est la seule sortie non masquée.
func (s String) Reveal() string { return string(s) }

// String implémente fmt.Stringer : toute mise en forme affiche <redacted>.
func (s String) String() string { return redactedText }

// GoString implémente fmt.GoStringer : « %#v » ne doit pas contourner String.
func (s String) GoString() string { return redactedText }

// LogValue implémente slog.LogValuer : une trace ne peut pas contenir la valeur.
func (s String) LogValue() slog.Value { return slog.StringValue(redactedText) }

// MarshalJSON masque la valeur : un secret ne peut pas être sérialisé en clair.
func (s String) MarshalJSON() ([]byte, error) { return []byte(`"` + redactedText + `"`), nil }

// IsZero indique si le secret est vide. À utiliser pour tester l'absence
// d'identifiants : comparer ne doit jamais exiger de révéler la valeur.
func (s String) IsZero() bool { return s == "" }
