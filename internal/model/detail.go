package model

import (
	"encoding/json"
	"sort"

	"fronius-supervisor/internal/fronius/solarapi"
)

// The detail snapshots.
//
// These types are the shape the API publishes for the device's diagnostic
// detail, and the shape the collector and the detector persist. Keeping one
// shape avoids a second mapping at the API boundary — and a mapping is exactly
// where a value would get lost or invented.
//
// Every nullable field follows the invariant of the project: a pointer means
// the device answered nothing (or was never asked), and a caller must render
// the absence rather than a zero.

// The detail families. A family is the unit that succeeds or fails together:
// one path, one read, one outcome, and one row in the store.
const (
	FamilyInverter = "inverter"
	FamilySystem   = "system"
	FamilyNetwork  = "network"
	FamilyDevices  = "devices"
	FamilyEmrs     = "emrs"
	FamilyCache    = "cache"
)

// MeasurementDetail is one measurement of the realtime-data family: a unit and
// a value, the pair the device itself answers.
type MeasurementDetail struct {
	Unit  string   `json:"unit"`
	Value *float64 `json:"value"`
}

// InverterStateDetail is the state the realtime-data family carries beside the
// measurements: the observed source of the inverter state.
type InverterStateDetail struct {
	InverterState string `json:"inverter_state"`
	StatusCode    *int   `json:"status_code"`
	ErrorCode     *int   `json:"error_code"`
}

// InverterEnergies mirrors the three energy counters of the accumulation
// collection, which are null on the reference firmware.
type InverterEnergies struct {
	DayWh   *float64 `json:"day_wh"`
	YearWh  *float64 `json:"year_wh"`
	TotalWh *float64 `json:"total_wh"`
}

// InverterDetail is the published snapshot of the inverter family.
type InverterDetail struct {
	State        InverterStateDetail          `json:"state"`
	Measurements map[string]MeasurementDetail `json:"measurements"`
	Energies     InverterEnergies             `json:"energies"`
}

// SystemAccount is the pair of account kinds the device declares.
type SystemAccount struct {
	Customer   *bool `json:"customer"`
	Technician *bool `json:"technician"`
}

// SystemInterface is one interface as /status/common lists it.
type SystemInterface struct {
	Name *string `json:"name"`
	Type *string `json:"type"`
}

// SystemDetail is the published snapshot of the system family.
type SystemDetail struct {
	SystemName     *string           `json:"system_name"`
	ServiceState   *string           `json:"service_state"`
	UpdateRunning  *bool             `json:"update_running"`
	SupportEnabled *bool             `json:"support_enabled"`
	ConnectedViaAP *bool             `json:"connected_via_ap"`
	Accounts       SystemAccount     `json:"accounts"`
	Interfaces     []SystemInterface `json:"interfaces"`
}

// NetworkInterface is one interface as /status/network describes it.
type NetworkInterface struct {
	Name          string   `json:"name"`
	IP            *string  `json:"ip"`
	Mask          *string  `json:"mask"`
	Gateway       *string  `json:"gateway"`
	DNS           []string `json:"dns"`
	Configured    *string  `json:"configured"`
	InterfaceMode *string  `json:"interface_mode"`
	Status        *string  `json:"status"`
	SSID          *string  `json:"ssid"`
	WPSState      *string  `json:"wps_state"`
}

// NetworkDetail is the published snapshot of the network family.
type NetworkDetail struct {
	FDMPConnected *bool              `json:"fdmp_connected"`
	Interfaces    []NetworkInterface `json:"interfaces"`
}

// DeviceDetailEntry is one element of the /status/devices inventory.
type DeviceDetailEntry struct {
	Type          *string `json:"type"`
	Status        *int    `json:"status"`
	StatusMessage *string `json:"status_message"`
}

// DevicesDetail is the published snapshot of the devices family.
type DevicesDetail struct {
	Devices []DeviceDetailEntry `json:"devices"`
}

// EmrsEntry is one flattened value of the EMRS pins.
//
// The family is published as a flat path/value list because its shape is a set
// of device-defined pins: modelling keys nobody observed would be inventing
// them.
type EmrsEntry struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// EmrsDetail is the published snapshot of the EMRS family.
type EmrsDetail struct {
	Entries []EmrsEntry `json:"entries"`
}

// measurementOrder is the display order of the realtime-data measurements: the
// order the survey observed on the reference device, so the interface does not
// reshuffle itself between two reads. A key that is not here is appended in
// alphabetical order — the set is device-defined and must never be truncated.
var measurementOrder = []string{
	"PAC", "SAC", "UAC", "IAC",
	"UDC", "IDC", "UDC_2", "IDC_2", "UDC_3", "IDC_3",
	"DAY_ENERGY", "YEAR_ENERGY", "TOTAL_ENERGY",
}

// MeasuredKeys returns the measurement keys of a detail, in display order.
func (d InverterDetail) MeasuredKeys() []string {
	out := make([]string, 0, len(d.Measurements))
	seen := make(map[string]bool, len(d.Measurements))
	for _, key := range measurementOrder {
		if _, ok := d.Measurements[key]; ok {
			out = append(out, key)
			seen[key] = true
		}
	}
	rest := make([]string, 0, len(d.Measurements))
	for key := range d.Measurements {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// FromInverterRealtime turns a realtime-data answer into the published
// snapshot. The measurements are kept as the device named them: the survey
// established that set by observation, and a key nobody documented must still
// reach the interface.
func FromInverterRealtime(rt solarapi.InverterRealtime) InverterDetail {
	out := InverterDetail{
		State: InverterStateDetail{
			InverterState: rt.Status.InverterState,
			StatusCode:    rt.Status.StatusCode,
			ErrorCode:     rt.Status.ErrorCode,
		},
		Measurements: make(map[string]MeasurementDetail, len(rt.Measurements)),
	}
	for key, m := range rt.Measurements {
		out.Measurements[key] = MeasurementDetail{Unit: m.Unit, Value: m.Value}
	}

	// The three counters are the ones the interface shows as energies: they are
	// read from the measurements, and stay nil when the device answers null.
	if m, ok := out.Measurements["DAY_ENERGY"]; ok {
		out.Energies.DayWh = m.Value
	}
	if m, ok := out.Measurements["YEAR_ENERGY"]; ok {
		out.Energies.YearWh = m.Value
	}
	if m, ok := out.Measurements["TOTAL_ENERGY"]; ok {
		out.Energies.TotalWh = m.Value
	}
	return out
}

// CacheDetail is the published snapshot of the cache family: the readable
// register set of the device, grouped by node.
//
// The device names its own registers, so the names are kept verbatim — including
// the type suffix the device itself appends (`_F32`, `_U64`, `_U16`), which is
// published separately as `kind`. A register the device identifies only by a bare
// `<id>` keeps that identifier: nothing observed so far explains it, and naming
// it would be inventing a meaning.
type CacheDetail struct {
	Nodes []CacheNodeDetail `json:"nodes"`
}

// CacheNodeDetail is one node of the cache.
type CacheNodeDetail struct {
	// ID is the device's node identifier (393216 on the reference unit).
	ID         string           `json:"id"`
	Attributes []CacheAttribute `json:"attributes"`
	Channels   []CacheChannel   `json:"channels"`
}

// CacheAttribute is one attribute of a node: the device labels these, so the
// value stays the text it answered.
type CacheAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CacheChannel is one register.
//
// Value is the device's own JSON value, unparsed: an energy counter written as
// a 64-bit integer does not survive a float64, and rounding it would be
// inventing a number the device did not report.
type CacheChannel struct {
	Key   string          `json:"key"`
	Name  string          `json:"name,omitempty"`
	Kind  string          `json:"kind,omitempty"`
	Type  string          `json:"type"`
	Unit  string          `json:"unit,omitempty"`
	Value json.RawMessage `json:"value"`
}
