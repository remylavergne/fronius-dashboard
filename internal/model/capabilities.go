package model

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
)

// Motifs et réserves, réutilisés tels quels par l'API et l'interface.
const (
	// ReasonUnverifiedSchema est une réserve permanente : elle signale que la
	// forme du JSON n'a pas été observée sur le firmware de l'installation.
	ReasonUnverifiedSchema = "Schéma non validé sur firmware 1.19.7-1"
	ReasonNoSmartMeter     = "Aucun Fronius Smart Meter détecté"
	ReasonNoBattery        = "Aucune batterie installée"
	ReasonNoInternalAPI    = "Adaptateur API interne non implémenté (phase 2)"
	ReasonUnavailable      = "Indisponible"
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
	available bool
	requires  []string
	// unavailableReason n'est appliqué que si la capacité est indisponible.
	unavailableReason string
	// caveat est une réserve permanente, appliquée quand la capacité est
	// disponible.
	caveat   string
	verified bool
	evidence []string
}

// Capabilities retourne l'état des capacités, dans un ordre stable.
//
// hasMeter, hasBattery et hasInternalAPI proviennent de la configuration ou
// d'une détection future. En phase 1, aucun des trois n'est prouvé : la
// détection dynamique est une inconnue documentée (docs/UNKNOWNS.md).
func Capabilities(hasMeter, hasBattery, hasInternalAPI bool) []Capability {
	specs := []capSpec{
		{
			id: "production.power", label: "Production instantanée", available: true,
			caveat:   ReasonUnverifiedSchema,
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_day", label: "Énergie du jour", available: true,
			caveat:   ReasonUnverifiedSchema,
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_total", label: "Énergie totale", available: true,
			caveat:   ReasonUnverifiedSchema,
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "grid.import_export", label: "Import / export réseau", available: hasMeter,
			requires: []string{RequiresSmartMeter}, unavailableReason: ReasonNoSmartMeter,
			evidence: []string{EvidenceConversation},
		},
		{
			id: "load.consumption", label: "Consommation", available: hasMeter,
			requires: []string{RequiresSmartMeter}, unavailableReason: ReasonNoSmartMeter,
			evidence: []string{EvidenceConversation},
		},
		{
			id: "autoconsumption.ratio", label: "Taux d'autoconsommation", available: hasMeter,
			requires: []string{RequiresSmartMeter}, unavailableReason: ReasonNoSmartMeter,
			evidence: []string{EvidenceConversation},
		},
		{
			id: "battery.soc", label: "État de charge de la batterie", available: hasBattery,
			requires: []string{RequiresBattery}, unavailableReason: ReasonNoBattery,
			evidence: []string{EvidenceConversation},
		},
		{
			id: "device.identity", label: "Identité de l'onduleur", available: hasInternalAPI,
			requires: []string{RequiresInternalAPI}, unavailableReason: ReasonNoInternalAPI,
		},
		{
			id: "system.collector", label: "Collecteur local", available: true,
			verified: true, evidence: []string{EvidenceLocal},
		},
	}

	caps := make([]Capability, 0, len(specs))
	for _, spec := range specs {
		c := Capability{
			ID:        spec.id,
			Label:     spec.label,
			Available: spec.available,
			Requires:  spec.requires,
			ReadOnly:  true,
			Verified:  spec.verified,
			Evidence:  spec.evidence,
		}
		if spec.available {
			c.Quality = QualityMeasured
			c.Reason = spec.caveat
		} else {
			c.Quality = QualityMissing
			c.Reason = spec.unavailableReason
			if c.Reason == "" {
				c.Reason = ReasonUnavailable
			}
		}
		caps = append(caps, c)
	}
	return caps
}
