package model

import (
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
