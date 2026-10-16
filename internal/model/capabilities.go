package model

import "strings"

// Prérequis matériels ou logiciels d'une capacité.
const (
	RequiresSmartMeter  = "smart_meter"
	RequiresBattery     = "battery"
	RequiresInternalAPI = "adapter:internalapi"
)

// Niveaux de preuve associés à une capacité.
const (
	// EvidenceConversation : valeur rapportée lors de l'analyse, non rejouée.
	EvidenceConversation = "conversation"
	// EvidenceSimulator : comportement reproduit par le simulateur local.
	EvidenceSimulator = "simulator"
	// EvidenceLocal : propriété de l'application elle-même, vérifiable ici.
	EvidenceLocal = "local"
	// EvidenceBundle : chemin et enveloppe relevés dans le client officiel
	// (references/fronius-original.js). Ce n'est PAS une permission firmware :
	// c'est la preuve qu'un chemin existe et sous quelle forme.
	EvidenceBundle = "bundle"
)

// Motifs et réserves, réutilisés tels quels par l'API et l'interface.
const (
	// ReasonUnverifiedSchema est une réserve permanente : elle signale que la
	// forme du JSON n'a pas été observée sur le firmware de l'installation.
	ReasonUnverifiedSchema = "Schéma non validé sur firmware 1.19.7-1"
	ReasonNoSmartMeter     = "Aucun Fronius Smart Meter détecté"
	ReasonNoBattery        = "Aucune batterie installée"
	ReasonBatteryReadout   = "Batterie détectée : lecture de l'état de charge non implémentée (phase ultérieure)"
	ReasonUnavailable      = "Indisponible"
	// causeInconnue est le repli lorsque la détection n'a pas su nommer sa
	// cause : le motif reste explicite et ne se termine jamais par « : ».
	causeInconnue = "cause inconnue"
)

// Capability décrit ce que la supervision peut réellement restituer.
//
// Une capacité indisponible n'est jamais représentée par une valeur numérique
// nulle : elle porte Available = false et un Reason non vide, ce que reflètent
// les endpoints /api/v1/capabilities et les cartes de l'interface.
//
// Reason a deux usages distincts, jamais simultanés : un motif d'indisponibilité
// quand Available est faux, une réserve quand la capacité est disponible mais
// non vérifiée sur le matériel (production.*).
type Capability struct {
	ID        string
	Label     string
	Available bool
	Quality   Quality
	Requires  []string
	ReadOnly  bool
	Verified  bool
	Evidence  []string
	Reason    string
}

// capSpec décrit une capacité avant arbitrage entre motif et réserve.
type capSpec struct {
	id, label string
	// available décide à partir des détections réelles : aucune capacité n'est
	// plus déduite d'un booléen de configuration.
	available func(Detection) bool
	requires  []string
	// reason retourne le motif applicable : motif d'indisponibilité si la
	// capacité est indisponible, réserve si elle est disponible.
	reason   func(Detection) string
	verified bool
	evidence []string
}

// Capabilities retourne l'état des capacités, dans un ordre stable.
//
// La décision repose sur la détection interne (internal/detect) : « non
// déterminé » n'est pas « absent », et une capacité indisponible porte toujours
// le motif qui l'explique.
func Capabilities(d Detection) []Capability {
	specs := []capSpec{
		{
			id: "production.power", label: "Production instantanée",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_day", label: "Énergie du jour",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_total", label: "Énergie totale",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "grid.import_export", label: "Import / export réseau",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Présence d'un Smart Meter non déterminée",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			id: "load.consumption", label: "Consommation",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Présence d'un Smart Meter non déterminée",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			id: "autoconsumption.ratio", label: "Taux d'autoconsommation",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Présence d'un Smart Meter non déterminée",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			// Détecter une batterie ne prouve pas qu'une mesure d'état de charge
			// existe côté API : l'annoncer disponible sans source serait
			// exactement l'inverse de l'invariant « pas de zéro inventé ».
			id: "battery.soc", label: "État de charge de la batterie",
			available: never,
			requires:  []string{RequiresBattery},
			reason:    batteryReason(),
			evidence:  []string{EvidenceConversation},
		},
		{
			id: "device.identity", label: "Identité de l'onduleur",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Identity }),
			requires:  []string{RequiresInternalAPI},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Identity },
				"Identité de l'onduleur non déterminée",
				""),
			evidence: []string{EvidenceBundle, EvidenceSimulator},
		},
		{
			id: "device.events", label: "Événements de l'appareil",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.DeviceEvents }),
			requires:  []string{RequiresInternalAPI},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.DeviceEvents },
				"Événements de l'appareil non déterminés",
				""),
			evidence: []string{EvidenceBundle, EvidenceSimulator},
		},
		{
			id: "system.collector", label: "Collecteur local",
			available: always, reason: alwaysReason(""),
			verified: true, evidence: []string{EvidenceLocal},
		},
	}

	caps := make([]Capability, 0, len(specs))
	for _, spec := range specs {
		available := spec.available(d)
		c := Capability{
			ID:        spec.id,
			Label:     spec.label,
			Available: available,
			Requires:  spec.requires,
			ReadOnly:  true,
			Verified:  spec.verified,
			Evidence:  spec.evidence,
		}
		if available {
			c.Quality = QualityMeasured
		} else {
			c.Quality = QualityMissing
		}
		c.Reason = spec.reason(d)
		if c.Reason == "" && !available {
			c.Reason = ReasonUnavailable
		}
		caps = append(caps, c)
	}
	return caps
}

func always(Detection) bool { return true }

func never(Detection) bool { return false }

// detectionIs fabrique un prédicat « cette ressource est dans l'état voulu ».
func detectionIs(state DetectionState, pick func(Detection) ResourceDetection) func(Detection) bool {
	return func(d Detection) bool { return pick(d).State == state }
}

// alwaysReason retourne un motif constant, indépendant des détections.
func alwaysReason(reason string) func(Detection) string {
	return func(Detection) string { return reason }
}

// detectionReason construit le motif d'une capacité liée à une détection.
//
// présent  → "" (ou absentReason si la lecture n'est pas implémentée)
// absent   → absentReason, ou le préfixe suivi de la cause si celle-ci en dit plus
// inconnu   → préfixe suivi de la cause
func detectionReason(pick func(Detection) ResourceDetection, prefix, absentReason string) func(Detection) string {
	return func(d Detection) string {
		r := pick(d)
		switch r.State {
		case StatePresent:
			return ""
		case StateAbsent:
			if absentReason != "" {
				return absentReason
			}
			return withCause(prefix, r.Reason)
		default:
			return withCause(prefix, r.Reason)
		}
	}
}

// batteryReason : la batterie détectée ne débloque pas la lecture de l'état de
// charge, qui n'est pas implémentée en phase 2. Le motif le dit explicitement.
func batteryReason() func(Detection) string {
	return func(d Detection) string {
		switch d.Battery.State {
		case StatePresent:
			return ReasonBatteryReadout
		case StateAbsent:
			return ReasonNoBattery
		default:
			return withCause("Présence d'une batterie non déterminée", d.Battery.Reason)
		}
	}
}

// withCause assemble un motif et sa cause sans jamais produire un motif qui se
// termine par « : ».
func withCause(prefix, cause string) string {
	cause = strings.TrimSpace(cause)
	if cause == "" {
		cause = causeInconnue
	}
	return prefix + " : " + cause
}
