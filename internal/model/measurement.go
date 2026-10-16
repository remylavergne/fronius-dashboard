// Package model contains the normalized model of the supervision.
//
// Central invariant: an absent value is nil, never 0. The absence of a Smart
// Meter or of a battery must never produce zero consumption, zero import/export
// or a zero self-consumption ratio — these quantities would be false and
// indistinguishable from a real zero measurement. The capabilities
// (capabilities.go) carry the explanation intended for the user.
package model

import (
	"sort"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
)

// Quality qualifies the provenance of a value or a sample.
type Quality string

const (
	// QualityMeasured: value from the device, timestamped by the device.
	QualityMeasured Quality = "measured"
	// QualityEstimated: measured value but timestamped by the collector,
	// for lack of a usable timestamp in the response.
	QualityEstimated Quality = "estimated"
	// QualityMissing: no usable value for the quantity considered.
	QualityMissing Quality = "missing"
)

// Origin of the sample timestamp.
const (
	TSSourceDevice    = "device"
	TSSourceCollector = "collector"
)

// SourceSolarAPI identifies the samples acquired via the Solar API.
const SourceSolarAPI = "solar_api"

// InverterSample is the normalized measurement of an inverter.
type InverterSample struct {
	ID          string
	DT          int
	PowerW      *float64
	EnergyDayWh *float64
}

// Sample is a normalized sample, ready for persistence and the API.
//
// Units: powers in W, energies in Wh, ratios in percent — base SI units, kept
// as they are in the database. Conversion to kW and kWh is done at the display
// boundary (units.go).
type Sample struct {
	TS       time.Time // always in UTC
	TSSource string    // TSSourceDevice or TSSourceCollector
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

// FromPowerFlow normalizes a GetPowerFlowRealtimeData.fcgi response.
//
// now is only used if the device does not provide a usable timestamp; the
// sample is then marked QualityEstimated so that no chart presents a
// reconstructed time as a measured time.
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
			// The device answers but provides no measurement: an empty sample
			// is not `measured`.
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
		// Stable order: the identifiers are protocol strings
		// ("1", "2", …), sorted lexicographically to be deterministic.
		sort.Slice(s.Inverters, func(i, j int) bool { return s.Inverters[i].ID < s.Inverters[j].ID })
	}

	return s
}

// hasAnyValue indicates whether at least one site measurement is present.
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
