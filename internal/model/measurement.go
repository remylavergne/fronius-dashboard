// Package model contient le modèle normalisé de la supervision.
//
// Invariant central : une valeur absente est nil, jamais 0. L'absence de Smart
// Meter ou de batterie ne doit jamais produire de consommation, d'import/export
// ni de taux d'autoconsommation nuls — ces grandeurs seraient fausses et
// indiscernables d'une vraie mesure nulle. Les capacités (capabilities.go)
// portent l'explication destinée à l'utilisateur.
package model

import (
	"sort"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
)

// Quality qualifie la provenance d'une valeur ou d'un échantillon.
type Quality string

const (
	// QualityMeasured : valeur issue de l'appareil, horodatée par l'appareil.
	QualityMeasured Quality = "measured"
	// QualityEstimated : valeur mesurée mais horodatée par le collecteur,
	// faute d'horodatage exploitable dans la réponse.
	QualityEstimated Quality = "estimated"
	// QualityMissing : aucune valeur exploitable pour la grandeur considérée.
	QualityMissing Quality = "missing"
)

// Origine de l'horodatage de l'échantillon.
const (
	TSSourceDevice    = "device"
	TSSourceCollector = "collector"
)

// SourceSolarAPI identifie les échantillons acquis via la Solar API.
const SourceSolarAPI = "solar_api"

// InverterSample est la mesure normalisée d'un onduleur.
type InverterSample struct {
	ID          string
	DT          int
	PowerW      *float64
	EnergyDayWh *float64
}

// Sample est un échantillon normalisé, prêt pour la persistance et l'API.
//
// Unités : puissances en W, énergies en Wh, ratios en pourcents — unités de
// base SI, conservées telles quelles en base. La conversion vers kW et kWh est
// faite à la frontière d'affichage (units.go).
type Sample struct {
	TS       time.Time // toujours en UTC
	TSSource string    // TSSourceDevice ou TSSourceCollector
	Source   string    // SourceSolarAPI
	Quality  Quality
	Mode     string

	PowerPVW      *float64
	EnergyDayWh   *float64
	EnergyYearWh  *float64
	EnergyTotalWh *float64

	PowerGridW    *float64
	PowerLoadW    *float64
	PowerBatteryW *float64

	RelAutonomyPct        *float64
	RelSelfConsumptionPct *float64

	Inverters []InverterSample
}

// FromPowerFlow normalise une réponse de GetPowerFlowRealtimeData.fcgi.
//
// now n'est utilisé que si l'appareil ne fournit pas d'horodatage exploitable ;
// l'échantillon est alors marqué QualityEstimated pour qu'aucun graphique ne
// présente une heure reconstruite comme une heure mesurée.
func FromPowerFlow(env solarapi.PowerFlowEnvelope, now time.Time) Sample {
	site := env.Body.Data.Site
	s := Sample{
		Source:                SourceSolarAPI,
		Mode:                  site.Mode,
		PowerPVW:              site.P_PV,
		EnergyDayWh:           site.E_Day,
		EnergyYearWh:          site.E_Year,
		EnergyTotalWh:         site.E_Total,
		PowerGridW:            site.P_Grid,
		PowerLoadW:            site.P_Load,
		PowerBatteryW:         site.P_Akku,
		RelAutonomyPct:        site.Rel_Autonomy,
		RelSelfConsumptionPct: site.Rel_SelfConsumption,
	}

	if ts, err := time.Parse(time.RFC3339, env.Head.Timestamp); err == nil && !ts.IsZero() {
		s.TS = ts.UTC()
		s.TSSource = TSSourceDevice
		s.Quality = QualityMeasured
		if !s.hasAnyValue() {
			// L'appareil répond mais ne fournit aucune mesure : un échantillon
			// vide n'est pas « mesuré ».
			s.Quality = QualityMissing
		}
	} else {
		s.TS = now.UTC()
		s.TSSource = TSSourceCollector
		s.Quality = QualityEstimated
	}

	if n := len(env.Body.Data.Inverters); n > 0 {
		s.Inverters = make([]InverterSample, 0, n)
		for id, inv := range env.Body.Data.Inverters {
			s.Inverters = append(s.Inverters, InverterSample{
				ID:          id,
				DT:          inv.DT,
				PowerW:      inv.P,
				EnergyDayWh: inv.E_Day,
			})
		}
		// Ordre stable : les identifiants sont des chaînes du protocole
		// (« 1 », « 2 », …), triées lexicographiquement pour être déterministes.
		sort.Slice(s.Inverters, func(i, j int) bool { return s.Inverters[i].ID < s.Inverters[j].ID })
	}

	return s
}

// hasAnyValue indique si au moins une mesure du site est présente.
func (s Sample) hasAnyValue() bool {
	for _, v := range []*float64{
		s.PowerPVW, s.EnergyDayWh, s.EnergyYearWh, s.EnergyTotalWh,
		s.PowerGridW, s.PowerLoadW, s.PowerBatteryW,
		s.RelAutonomyPct, s.RelSelfConsumptionPct,
	} {
		if v != nil {
			return true
		}
	}
	return false
}
