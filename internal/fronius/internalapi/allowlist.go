// Liste blanche des chemins internes du Webinterface Fronius.
//
// C'est la matrice de permissions exécutable de la phase 2 : un chemin absent de
// cette table n'est pas appelable (le client refuse avant de construire la
// requête), et aucune entrée n'est de classe MUTATION ni DANGEROUS. Les 79
// chemins recensés dans docs/ENDPOINTS.md restent hors liste : ils ne servent
// qu'à savoir ce qu'il ne faut pas appeler.
package internalapi

import "errors"

// Class classe un endpoint selon l'effet qu'il peut avoir.
type Class string

const (
	// ClassRead : lecture seule, sans effet de bord.
	ClassRead Class = "read"
	// ClassMutation : modifie un réglage de l'appareil.
	ClassMutation Class = "mutation"
	// ClassDangerous : commande d'exploitation (arrêt, autotest, service).
	ClassDangerous Class = "dangerous"
)

// Envelope décrit la forme de la charge utile de la réponse.
type Envelope string

const (
	// EnvelopeDefault : le corps entier est la charge utile.
	EnvelopeDefault Envelope = "default"
	// EnvelopeCgi : la charge utile est Body.Data, objet indexé par
	// identifiant de composant (éventuellement vide).
	EnvelopeCgi Envelope = "cgi"
)

// Endpoint décrit un chemin autorisé.
type Endpoint struct {
	Path     string
	Class    Class
	Envelope Envelope
	// Digest : le client officiel atteint ce chemin via callGet$ (GET + Digest,
	// un retry sur 401). true ne signifie pas « le chemin exige une
	// authentification » : le module yTJA prouve que certains de ces chemins
	// répondent aussi sans en-tête.
	Digest bool
	// Purpose : pourquoi l'application appelle ce chemin.
	Purpose string
	// Evidence : ligne de bundle et vérification par le simulateur.
	Evidence string
}

// Chemins autorisés.
const (
	VersionPath      = "/status/version"
	ActiveEventsPath = "/status/activeEvents"
	PowerMeterPath   = "/components/PowerMeter/readable"
	BatteryPath      = "/components/BatteryManagementSystem/readable"
)

// ErrNotAllowed est retournée par Get pour tout chemin hors liste blanche.
// C'est une fermeture par défaut : l'appelant ne peut pas viser une URL
// arbitraire, même en construisant le chemin dynamiquement.
var ErrNotAllowed = errors.New("internalapi: chemin hors liste blanche")

// allowlist est la table, dans un ordre stable.
var allowlist = []Endpoint{
	{
		Path:     VersionPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "identité de l'appareil : numéro de série, groupe d'appareil, nombre de phases",
		Evidence: "bundle references/fronius-original.js : registre pollé l. 76042 (module aYRN), retry 401 l. 75961-75963 ; voie sans en-tête l. 183050 (module yTJA) ; champs serialNumber l. 45243, numberOfPhases et devicegroup l. 183034-183035. Vérifié par cmd/fronius-sim (/status/version).",
	},
	{
		Path:     ActiveEventsPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "événements actifs signalés par l'appareil",
		Evidence: "bundle : registre pollé l. 76030 (module aYRN, intervalle 10 000 ms). Forme du corps NON établie (docs/UNKNOWNS.md) ; le détail brut est conservé. Vérifié par cmd/fronius-sim (/status/activeEvents).",
	},
	{
		Path:     PowerMeterPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "présence d'un Fronius Smart Meter",
		Evidence: "bundle : registre pollé l. 76031 avec enveloppe Cgi l. 75958 (module aYRN) ; décodage Body.Data l. 183011 (module yTJA). Aucun champ nommé n'est prouvé : seule la présence est déduite. Vérifié par cmd/fronius-sim (/components/PowerMeter/readable).",
	},
	{
		Path:     BatteryPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "présence d'une batterie",
		Evidence: "bundle : registre pollé l. 76032 avec enveloppe Cgi (module aYRN). Aucun champ nommé n'est prouvé. Vérifié par cmd/fronius-sim (/components/BatteryManagementSystem/readable).",
	},
}

// Allowlist retourne une copie de la liste blanche, dans un ordre stable.
func Allowlist() []Endpoint {
	out := make([]Endpoint, len(allowlist))
	copy(out, allowlist)
	return out
}

// Lookup retourne l'entrée d'un chemin.
func Lookup(path string) (Endpoint, bool) {
	for _, ep := range allowlist {
		if ep.Path == path {
			return ep, true
		}
	}
	return Endpoint{}, false
}
