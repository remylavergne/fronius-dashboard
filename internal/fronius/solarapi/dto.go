// Package solarapi implements the read-only client of the Fronius Solar API v1.
//
// WARNING — provenance of the shape described here. The JavaScript bundle of
// the original interface contains NO Solar API path: the string "solar_api" only
// appears there for the configuration endpoint "/config/solar_api". The
// structure below is therefore a [HYPOTHESIS] based on the public documentation
// of the Solar API v1 and on reported observations, not on a capture of firmware
// 1.19.7-1.
//
// Consequences:
//   - decoding is tolerant (no DisallowUnknownFields): a more recent firmware
//     can add fields without breaking the client;
//   - any absent value stays nil, never 0 (see internal/model);
//   - the test/fixtures/solarapi fixtures are synthetic until a real redacted
//     capture (cmd/fronius-probe) replaces them.
//
// This file is the ONLY place where the shape of the Fronius JSON is encoded.
package solarapi

// PowerFlowEnvelope is the response of GetPowerFlowRealtimeData.fcgi.
type PowerFlowEnvelope struct {
	Body PowerFlowBody `json:"Body"`
	Head Head          `json:"Head"`
}

// PowerFlowBody wraps the measurement data.
type PowerFlowBody struct {
	Data PowerFlowData `json:"Data"`
}

// PowerFlowData contains the site then the inverters.
type PowerFlowData struct {
	Site      SiteData              `json:"Site"`
	Inverters map[string]InverterPF `json:"Inverters"`
}

// SiteData aggregates the site measurement point.
//
// Field names aligned on the protocol names (P_PV, E_Day…) so that any tag
// divergence jumps out when rereading.
type SiteData struct {
	Mode                string   `json:"Mode"`
	P_PV                *float64 `json:"P_PV"`
	P_Grid              *float64 `json:"P_Grid"`
	P_Load              *float64 `json:"P_Load"`
	P_Akku              *float64 `json:"P_Akku"`
	E_Day               *float64 `json:"E_Day"`
	E_Year              *float64 `json:"E_Year"`
	E_Total             *float64 `json:"E_Total"`
	Rel_Autonomy        *float64 `json:"rel_Autonomy"`
	Rel_SelfConsumption *float64 `json:"rel_SelfConsumption"`
	// Meter_Location is the meter position reported by the device. Captured on
	// firmware 1.19.7-1 as the string "unknown"; the earlier [HYPOTHESIS]
	// guessed an integer field named `Meter_Location_Current`.
	Meter_Location *string `json:"Meter_Location"`
}

// InverterPF is the measurement of an inverter, indexed by its number ("1", "2"…).
type InverterPF struct {
	DT      int      `json:"DT"`
	P       *float64 `json:"P"`
	E_Day   *float64 `json:"E_Day"`
	E_Year  *float64 `json:"E_Year"`
	E_Total *float64 `json:"E_Total"`
}

// Head carries the response metadata.
type Head struct {
	RequestArguments map[string]any `json:"RequestArguments"`
	Status           HeadStatus     `json:"Status"`
	Timestamp        string         `json:"Timestamp"`
}

// HeadStatus is the application state reported by the device.
type HeadStatus struct {
	Code        int    `json:"Code"`
	Reason      string `json:"Reason"`
	UserMessage string `json:"UserMessage"`
}

// rawEnvelope only serves to detect the absence of Body or Body.Data,
// indistinguishable from an empty object after decoding into a value struct.
type rawEnvelope struct {
	Body *struct {
		Data *PowerFlowData `json:"Data"`
	} `json:"Body"`
	Head Head `json:"Head"`
}
