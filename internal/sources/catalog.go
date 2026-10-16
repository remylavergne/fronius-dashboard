// Package sources is the map of what the inverter exposes and what this
// application does with it.
//
// It exists because the two questions an operator asks — "what else does my
// device answer?" and "why is this number not on screen?" — were only answerable
// by reading three registers at once (`docs/SOURCES.md`, `docs/ENDPOINTS.md`,
// `internal/model/capabilities.go`). Here they are one list, machine-readable,
// with the same provenance rules the rest of the project uses: a fact carries
// the source it comes from and the firmware version that produced it, and a
// field is only marked as exposed when something actually reads it.
//
// Nothing in this package touches the network or the database: it is data. The
// catalogue is checked against the registers by catalog_test.go, so a path
// cannot be added to one without the other.
//
// It is also the single definition of the paths: cmd/fronius-probe builds its
// allowlist from it (the sources the project uses, plus the ones already
// captured, for its default run; the whole catalogue for --survey), so a path
// cannot be probed without appearing here, and the two cannot drift.
package sources

import (
	"sort"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
)

// Transport identifies the HTTP surface a source belongs to.
type Transport string

const (
	// SolarAPI is the documented, unauthenticated Solar API v1.
	SolarAPI Transport = "solar_api"
	// InternalAPI is the manufacturer's web interface API, Digest-protected,
	// reachable only through the code allowlist.
	InternalAPI Transport = "internal_api"
)

// State says what the application does with a source.
type State string

const (
	// StateCalled: the application reads it today.
	StateCalled State = "called"
	// StateCaptured: the application does not call it, but a redacted answer
	// was captured on the reference hardware. The shape is known, the path is
	// not in the allowlist.
	StateCaptured State = "captured"
	// StateCandidate: documented or read by the manufacturer's own interface,
	// never observed on this firmware.
	StateCandidate State = "candidate"
	// StateArchived: it was called once, its capture is kept, and it is no
	// longer used (a clean cutover, no shim).
	StateArchived State = "archived"
)

// FieldStatus says what happens to one field of a source. The distinction
// matters: "ignored" is a decision, "unverified" is an absence of evidence.
type FieldStatus string

const (
	// FieldDisplayed: read into the model and shown by the interface.
	FieldDisplayed FieldStatus = "displayed"
	// FieldStored: read and persisted, whether or not it is displayed.
	FieldStored FieldStatus = "stored"
	// FieldValidated: read to accept or reject the payload, never shown.
	FieldValidated FieldStatus = "validated"
	// FieldIgnored: present in the answer and deliberately not used.
	FieldIgnored FieldStatus = "ignored"
	// FieldUnverified: never observed carrying a value on the reference
	// installation (typically null), or never observed at all.
	FieldUnverified FieldStatus = "unverified"
)

// Field is one value of a source, named by the path it has in the answer.
type Field struct {
	Path   string      `json:"path"`
	Type   string      `json:"type"`
	Status FieldStatus `json:"status"`
	// Why explains an ignored or unverified field. It is required for those two
	// statuses: an unexplained omission is how knowledge gets lost.
	Why string `json:"why,omitempty"`
	// Capability is the capability id the field feeds or would feed. An id that
	// exists in internal/model is available today; a "would unlock" id is a
	// candidate and is named as such by the source.
	Capability string `json:"capability,omitempty"`
}

// Proof is the provenance of a claim about a source.
type Proof struct {
	// Tag is the register's own marker: [VERIFIED], [SOURCE-JS], [DOC],
	// [OBSERVED-CONVERSATION], [HYPOTHESIS].
	Tag string `json:"tag"`
	// Firmware is the version that produced the evidence, empty when the proof
	// does not come from the device.
	Firmware string `json:"firmware,omitempty"`
	Detail   string `json:"detail"`
}

// Source is one endpoint or one query variant of an endpoint.
type Source struct {
	ID        string    `json:"id"`
	Transport Transport `json:"transport"`
	Path      string    `json:"path"`
	Query     string    `json:"query,omitempty"`
	Method    string    `json:"method"`
	// Provides is what the source carries, in one line.
	Provides string `json:"provides"`
	State    State  `json:"state"`
	// Called tells whether the application itself reads it (a probe entry is
	// not a call: the probe is run by the user, deliberately).
	Called bool `json:"called"`
	// Fixture is the promoted capture, empty when there is none.
	Fixture string `json:"fixture,omitempty"`
	// Capability is the capability id this source feeds today.
	Capability string `json:"capability,omitempty"`
	// WouldUnlock names the capability a candidate would fill once wired. It is
	// a candidate id: by definition it does not exist in internal/model yet.
	WouldUnlock string  `json:"would_unlock,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
	Proofs      []Proof `json:"proofs"`
	// Docs points at the registers describing the source.
	Docs []string `json:"docs,omitempty"`
	Note string   `json:"note,omitempty"`
}

// Summary counts the catalogue, for the head of the interface.
type Summary struct {
	Sources          int `json:"sources"`
	Called           int `json:"called"`
	Captured         int `json:"captured"`
	Candidates       int `json:"candidates"`
	Archived         int `json:"archived"`
	Fields           int `json:"fields"`
	FieldsDisplayed  int `json:"fields_displayed"`
	FieldsStored     int `json:"fields_stored"`
	FieldsValidated  int `json:"fields_validated"`
	FieldsIgnored    int `json:"fields_ignored"`
	FieldsUnverified int `json:"fields_unverified"`
}

// Summarize counts a catalogue.
func Summarize(list []Source) Summary {
	var s Summary
	s.Sources = len(list)
	for _, src := range list {
		switch src.State {
		case StateCalled:
			s.Called++
		case StateCaptured:
			s.Captured++
		case StateCandidate:
			s.Candidates++
		case StateArchived:
			s.Archived++
		}
		for _, f := range src.Fields {
			s.Fields++
			switch f.Status {
			case FieldDisplayed:
				s.FieldsDisplayed++
			case FieldStored:
				s.FieldsStored++
			case FieldValidated:
				s.FieldsValidated++
			case FieldIgnored:
				s.FieldsIgnored++
			case FieldUnverified:
				s.FieldsUnverified++
			}
		}
	}
	return s
}

// Catalog returns every known source: the Solar API first, then the internal
// API, each in the order of the registers they come from. The slice is rebuilt
// on every call, so a caller cannot reorder the package's data.
func Catalog() []Source {
	out := make([]Source, 0, len(solarAPISources)+len(internalAPISources))
	out = append(out, solarAPISources...)
	out = append(out, internalAPISources...)
	for i := range out {
		out[i].Fields = append([]Field(nil), out[i].Fields...)
		out[i].Proofs = append([]Proof(nil), out[i].Proofs...)
		out[i].Docs = append([]string(nil), out[i].Docs...)
	}
	return out
}

// Called returns the sources the application reads today. A test keeps this set
// equal to the code allowlist: it is the executable authorization of the
// project, and the catalogue must not suggest otherwise.
func Called() []Source {
	var out []Source
	for _, s := range Catalog() {
		if s.Called {
			out = append(out, s)
		}
	}
	return out
}

// ByID finds one source.
func ByID(id string) (Source, bool) {
	for _, s := range Catalog() {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}

// IDs returns every identifier, sorted: the stable key of the catalogue.
func IDs() []string {
	out := make([]string, 0, len(solarAPISources)+len(internalAPISources))
	for _, s := range append(append([]Source(nil), solarAPISources...), internalAPISources...) {
		out = append(out, s.ID)
	}
	sort.Strings(out)
	return out
}

// registeredFirmware is the version every [VERIFIED] claim of this catalogue was
// captured on. A claim captured on another firmware must carry its own version
// in its Proof.
const registeredFirmware = "1.19.7-1"

const (
	verifiedTag = "[VERIFIED]"
	bundleTag   = "[SOURCE-JS]"
	docTag      = "[DOC]"
)

// solarAPISources is the documented Solar API v1 surface, as far as it is known
// today: one called path, one captured collection, and the families that remain
// candidates.
var solarAPISources = []Source{
	{
		ID:         "solarapi.powerflow",
		Transport:  SolarAPI,
		Path:       solarapi.PowerFlowPath,
		Method:     "GET",
		Provides:   "Site power flow and the total energy: the only source the collector reads",
		State:      StateCalled,
		Called:     true,
		Fixture:    "test/fixtures/solarapi/powerflow_produce_only.json",
		Capability: "production.power",
		Fields: []Field{
			{Path: "Body.Data.Site.P_PV", Type: "float", Status: FieldDisplayed, Capability: "production.power"},
			{Path: "Body.Data.Site.E_Total", Type: "float", Status: FieldStored, Capability: "production.energy_total"},
			{Path: "Body.Data.Site.E_Day", Type: "null", Status: FieldUnverified,
				Why: "null on this firmware; the day energy is derived from E_Total (unknown #13)", Capability: "production.energy_day"},
			{Path: "Body.Data.Site.E_Year", Type: "null", Status: FieldUnverified,
				Why: "null on this firmware; the year energy is derived from E_Total (unknown #13)"},
			{Path: "Body.Data.Site.Mode", Type: "string", Status: FieldDisplayed,
				Why: "shown as the installation mode"},
			{Path: "Body.Data.Site.P_Grid", Type: "null", Status: FieldUnverified,
				Why: "null without a Smart Meter; no meter is installed (unknown #4)", Capability: "grid.import_export"},
			{Path: "Body.Data.Site.P_Load", Type: "null", Status: FieldUnverified,
				Why: "null without a measurement; consumption is never computed (unknown #4)", Capability: "load.consumption"},
			{Path: "Body.Data.Site.rel_SelfConsumption", Type: "null", Status: FieldUnverified,
				Why: "null without a Smart Meter (unknown #4)", Capability: "autoconsumption.ratio"},
			{Path: "Body.Data.Site.rel_Autonomy", Type: "null", Status: FieldUnverified,
				Why: "null without a Smart Meter (unknown #4)", Capability: "autoconsumption.ratio"},
			{Path: "Body.Data.Site.P_Akku", Type: "null", Status: FieldUnverified,
				Why: "null without a battery; none is installed (unknown #5)", Capability: "battery.soc"},
			{Path: "Body.Data.Site.Meter_Location", Type: "string", Status: FieldIgnored,
				Why: "meter presence comes from the internal API, which answers a body rather than a location"},
			{Path: "Body.Data.Site.BackupMode", Type: "bool", Status: FieldIgnored,
				Why: "tolerated by the decoder, no display use"},
			{Path: "Body.Data.Site.BatteryStandby", Type: "bool", Status: FieldIgnored,
				Why: "tolerated by the decoder, no display use"},
			{Path: `Body.Data.Inverters."1".P`, Type: "float", Status: FieldDisplayed},
			{Path: `Body.Data.Inverters."1".DT`, Type: "int", Status: FieldStored,
				Why: "kept with the inverter sample (inverters_json)"},
			{Path: `Body.Data.Inverters."1".E_Day`, Type: "null", Status: FieldUnverified,
				Why: "null on this firmware, on the site and on the inverter (unknown #13)"},
			{Path: `Body.Data.Inverters."1".E_Year`, Type: "null", Status: FieldUnverified,
				Why: "null on this firmware (unknown #13)"},
			{Path: `Body.Data.Inverters."1".E_Total`, Type: "float", Status: FieldIgnored,
				Why: "not retained: the site total is the reference for the derived energies"},
			{Path: "Body.Data.Smartloads.Ohmpilots", Type: "object", Status: FieldIgnored,
				Why: "empty on this installation: no Ohmpilot"},
			{Path: "Body.Data.Version", Type: "string", Status: FieldIgnored,
				Why: "protocol version of the payload (\"12\"), not used"},
			{Path: "Head.Status.Code", Type: "int", Status: FieldValidated,
				Why: "a non-zero code rejects the answer instead of storing a wrong sample"},
			{Path: "Head.Timestamp", Type: "string", Status: FieldValidated,
				Why: "carries the sample timestamp; an unreadable value falls back to the collector clock (estimated)"},
			{Path: "Head.RequestArguments", Type: "object", Status: FieldIgnored,
				Why: "echo of the request, no measurement"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "captured 2026-10-16: 200 OK, application/json, 1074 bytes; every key above is the capture's"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.11.3 url `.fcgi` — the ONE request of the family that is `.fcgi`; §4.11.1 ALWAYS on GEN24/Tauro/Verto"},
		},
		Docs: []string{"docs/SOURCES.md § Shape of the Solar API v1 response", "docs/ENDPOINTS.md"},
	},
	{
		ID:          "solarapi.inverter.cumulation",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetInverterRealtimeData.cgi",
		Query:       "Scope=Device&DeviceId=1&DataCollection=CumulationInverterData",
		Method:      "GET",
		Provides:    "The inverter's cumulative energies and its device status",
		State:       StateCaptured,
		Fixture:     "test/fixtures/solarapi/inverter_realtime_accumulation.json",
		WouldUnlock: "inverter.state",
		Fields: []Field{
			// The captured answer is FLAT — the measurements sit directly under
			// Body.Data, and each is a Unit/Value pair; the documented wrapper
			// object does not exist (fixture: test/fixtures/solarapi/
			// inverter_realtime_accumulation.json).
			{Path: "Body.Data.DAY_ENERGY", Type: "object (Unit, Value)", Status: FieldUnverified,
				Why:        "captured 2026-10-16: Value is null on this firmware; the day energy is derived from the total (unknown #13)",
				Capability: "production.energy_day"},
			{Path: "Body.Data.YEAR_ENERGY", Type: "object (Unit, Value)", Status: FieldUnverified,
				Why: "captured: Value is null on this firmware (unknown #13)"},
			{Path: "Body.Data.TOTAL_ENERGY", Type: "object (Unit, Value)", Status: FieldIgnored,
				Why: "duplicate of Body.Data.Site.E_Total; the application does not call this path"},
			{Path: "Body.Data.PAC", Type: "object (Unit, Value)", Status: FieldIgnored,
				Why: "instantaneous AC power, part of the accumulation answer as captured; unread"},
			{Path: "Body.Data.DeviceStatus.InverterState", Type: "string", Status: FieldIgnored,
				Why:        "captured as \"Sleeping\": unread, no inverter-state capability exists yet",
				Capability: "inverter.state"},
			{Path: "Body.Data.DeviceStatus.StatusCode", Type: "int", Status: FieldIgnored,
				Why: "captured as 13; unread", Capability: "inverter.state"},
			{Path: "Body.Data.DeviceStatus.ErrorCode", Type: "int", Status: FieldIgnored,
				Why: "captured as 1175; unread", Capability: "inverter.state"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "captured 2026-10-16: 200, 883 bytes; FLAT Body.Data — DAY_ENERGY/YEAR_ENERGY null, TOTAL_ENERGY and PAC live, DeviceStatus present (InverterState Sleeping, StatusCode 13, ErrorCode 1175); the fixture is the promoted body"},
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "surveyed 2026-10-16 by cmd/fronius-probe --survey: class ok, 883 bytes, 18 fields"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.1.3 url `.cgi`; §4.1.2 CumulationInverterData: supported on GEN24; §4.1.5 DAY_ENERGY and YEAR_ENERGY \"will always report null\" on GEN24/Tauro/Verto, TOTAL_ENERGY \"supported since 1.14 (updated every 5 min)\""},
		},
		Docs: []string{"docs/UNKNOWNS.md #13", "docs/PROBE-READONLY.md"},
		Note: "reachable through cmd/fronius-probe only: it is in the probe's allowlist, not in the application's.",
	},
	{
		ID:        "solarapi.inverter.common",
		Transport: SolarAPI,
		Path:      "/solar_api/v1/GetInverterRealtimeData.cgi",
		Query:     "Scope=Device&DeviceId=1&DataCollection=CommonInverterData",
		Method:    "GET",
		Provides:  "Per-inverter electrical detail: AC and DC voltage, current, power — read by the collector",
		State:     StateCalled,
		Called:    true,
		Fields: []Field{
			// The survey of 2026-10-16 observed the thirteen measurements below,
			// each a Unit/Value pair directly under Body.Data (no wrapper), with
			// IDC_3/UDC_3 null (inputs not connected) and the two energy counters
			// null. The documented key names were right; the nesting was not.
			{Path: "Body.Data.DAY_ENERGY", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "observed 2026-10-16: null Value observed"},
			{Path: "Body.Data.IAC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.IDC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.IDC_2", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.IDC_3", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.PAC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.SAC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.TOTAL_ENERGY", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.UAC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.UDC", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.UDC_2", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.UDC_3", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.YEAR_ENERGY", Type: "object (Unit, Value)", Status: FieldDisplayed,
				Why: "shown on the device detail page; read every collector cycle"},
			{Path: "Body.Data.DeviceStatus.InverterState", Type: "string", Status: FieldDisplayed,
				Why: "observed by the survey; unread — no inverter-state capability exists yet"},
			{Path: "Body.Data.DeviceStatus.StatusCode", Type: "int", Status: FieldDisplayed,
				Why: "observed by the survey; unread", Capability: "inverter.state"},
			{Path: "Body.Data.DeviceStatus.ErrorCode", Type: "int", Status: FieldDisplayed,
				Why: "observed by the survey; unread", Capability: "inverter.state"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "surveyed 2026-10-16 by cmd/fronius-probe --survey: class ok, 1680 bytes, 36 fields — the electrical detail exists on this firmware, with DeviceStatus beside it"},
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "read every collector cycle (internal/collector, query solarapi.CommonQuery) and published by GET /api/v1/detail, which the device detail page renders"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.1.3 url `.cgi`; §4.1.2 CommonInverterData: supported on GEN24"},
		},
		Docs: []string{"docs/SOURCES.md § First survey of the reference installation"},
		Note: "the richest unmapped candidate: it answers the DC/AC detail the flow endpoint never carries, plus the inverter state. One capture (single-path probe) would promote its body as a fixture.",
	},
	{
		ID:          "solarapi.inverter.3p",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetInverterRealtimeData.cgi",
		Query:       "Scope=Device&DeviceId=1&DataCollection=3PInverterData",
		Method:      "GET",
		Provides:    "Per-phase AC measurements of a three-phase inverter",
		State:       StateCaptured,
		WouldUnlock: "inverter.electrical.phases",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "surveyed 2026-10-16: class empty — the collection answers an empty Body.Data on this single-phase Primo. The documented per-phase keys were never observed carrying a value"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.1.3 url `.cgi`; §4.1.2 3PInverterData: \"Only on 3 phase inverters\"; MinMaxInverterData: NO on GEN24 — this file's 3P is therefore expected to answer nothing on a single-phase Primo"},
		},
		Docs: []string{"docs/SOURCES.md § First survey of the reference installation"},
		Note: "an empty answer here is the answer: a single-phase inverter has no per-phase measurements. Nothing to wire.",
	},
	{
		ID:          "solarapi.inverter_info",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetInverterInfo.cgi",
		Method:      "GET",
		Provides:    "Identity of each inverter: model, serial number, custom name",
		State:       StateCaptured,
		Capability:  "device.identity",
		WouldUnlock: "device.identity.solarapi",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class ok, 537 bytes, 12 fields — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.6.2 url `.cgi` (NOT `.fcgi`); §4.6.1 ALWAYS on GEN24/Tauro/Verto; §4.6.5 StatusCode 13 = Sleeping"}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "would source the identity without the Digest-protected internal API — one fewer authenticated path.",
	},
	{
		ID:        "solarapi.logger_info",
		Transport: SolarAPI,
		Path:      "/solar_api/v1/GetLoggerInfo.cgi",
		Method:    "GET",
		Provides:  "Identity of the datalogger / web interface itself",
		State:     StateCaptured,
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 288 bytes — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.4.2 url `.cgi` (NOT `.fcgi`); §4.4.1 ALWAYS on GEN24/Tauro/Verto but \"API is available but returns an error\""}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
	},
	{
		ID:          "solarapi.meter_realtime",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetMeterRealtimeData.cgi",
		Method:      "GET",
		Provides:    "Smart Meter measurements: grid power, energy counters, per-phase values",
		State:       StateCaptured,
		Capability:  "grid.import_export",
		WouldUnlock: "meter.realtime",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 276 bytes — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.8.2 url `.cgi` (NOT `.fcgi`); §4.8.1 GEN24/Tauro/Verto \"planned in 1.13\""}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "no meter is installed here (the internal readable path answered an empty body): this candidate would stay empty, and the survey records that as a fact rather than a guess.",
	},
	{
		ID:          "solarapi.storage_realtime",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetStorageRealtimeData.cgi",
		Method:      "GET",
		Provides:    "Battery: state of charge, power, capacity",
		State:       StateCaptured,
		Capability:  "battery.soc",
		WouldUnlock: "battery.realtime",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 276 bytes — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.9.4 url `.cgi` (NOT `.fcgi`); §4.9.1 GEN24/Tauro/Verto since 1.13"}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "no battery is installed here (unknown #5); the internal readable path answered an empty body.",
	},
	{
		ID:        "solarapi.active_device_info",
		Transport: SolarAPI,
		Path:      "/solar_api/v1/GetActiveDeviceInfo.cgi",
		Method:    "GET",
		Provides:  "Inventory of the active devices of the system (inverters, meters, storage, sensors)",
		State:     StateCaptured,
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class ok, 494 bytes, 7 fields — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.7.2 url `.cgi` (NOT `.fcgi`); §4.7.1 ALWAYS on GEN24/Tauro/Verto"}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
	},
	{
		ID:          "solarapi.string_realtime",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetStringRealtimeData.cgi",
		Method:      "GET",
		Provides:    "Per-string DC measurements: voltage, current, power",
		State:       StateCaptured,
		WouldUnlock: "strings.detail",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 296 bytes — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.3.2 url `.cgi` (NOT `.fcgi`); §4.3.1 ALWAYS on GEN24/Tauro/Verto but \"String Control does not exist for GEN24/Tauro/Verto\""}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "the granularity a Primo exposes for its DC inputs; never observed on this firmware.",
	},
	{
		ID:          "solarapi.sensor_realtime",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetSensorRealtimeData.cgi",
		Method:      "GET",
		Provides:    "External sensor channels (irradiance, temperature) when a sensor card is fitted",
		State:       StateCaptured,
		WouldUnlock: "sensors.realtime",
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 296 bytes — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.2.2 url `.cgi` (NOT `.fcgi`); §4.2.1 ALWAYS on GEN24/Tauro/Verto but \"API is available but returns an error\""}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
	},
	{
		ID:        "solarapi.logger_led_info",
		Transport: SolarAPI,
		Path:      "/solar_api/v1/GetLoggerLEDInfo.cgi",
		Method:    "GET",
		Provides:  "State and colour of the LEDs of the device that serves the API",
		State:     StateCaptured,
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 291 bytes — recorded in source_probe and visible on the Data sources page"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.5.2 url `.cgi`; §4.5.1 ALWAYS on GEN24/Tauro/Verto but \"API is available but returns an error\""},
		},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "missing from the catalogue until the specification was read: the earlier candidate list was a guess.",
	},
	{
		ID:        "solarapi.ohmpilot_realtime",
		Transport: SolarAPI,
		Path:      "/solar_api/v1/GetOhmPilotRealtimeData.cgi",
		Method:    "GET",
		Provides:  "Ohmpilot: state code, error code, consumed energy, temperature and power",
		State:     StateCaptured,
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class empty, 276 bytes — recorded in source_probe and visible on the Data sources page"},
			{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §4.10.2 url `.cgi`, with a capital P (`GetOhmPilot`); §4.10.1 GEN24/Tauro/Verto since 1.13"},
		},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note: "no Ohmpilot is installed here (the internal readable path answers an empty body), so an empty answer is the expected finding.",
	},
	{
		ID:          "solarapi.archive",
		Transport:   SolarAPI,
		Path:        "/solar_api/v1/GetArchiveData.cgi",
		Method:      "GET",
		Provides:    "Historical time series held by the device itself (energy, power) over a requested range",
		State:       StateCandidate,
		WouldUnlock: "history.device",
		Proofs:      []Proof{{Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §5 call `GetArchiveData.cgi?<query>`; §5.1.1 GEN24/Tauro/Verto: **NEVER** — \"does not provide access to history data\"; the answer is therefore a 404 on this firmware, not a missing capture"}},
		Docs:        []string{"docs/SOURCES.md § Other Solar API endpoints"},
		Note:        "the only candidate that could fill the daily chart from the device: agg_hour.max_energy_day_wh is null on this firmware, so the chart is empty today.",
	},
	{
		ID:        "solarapi.api_version",
		Transport: SolarAPI,
		Path:      "/solar_api/GetAPIVersion.cgi",
		Method:    "GET",
		Provides:  "The Solar API version the device exposes",
		State:     StateCaptured,
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "surveyed 2026-10-16 with the path the specification gives (`42,0410,2012`): class ok, 92 bytes, 3 fields — recorded in source_probe and visible on the Data sources page"}, {Tag: docTag, Detail: "official Solar API V1 documentation, `42,0410,2012,EN 02115052025` (references/42,0410,2012.pdf); §2.4.1 url `/solar_api/GetAPIVersion.cgi` (the `.cgi` is part of the path)"}},
		Docs: []string{"docs/SOURCES.md § Other Solar API endpoints"},
	},
}

// internalAPISources is the manufacturer's web interface API: four allowlisted
// paths the application reads, the five captures that were promoted, the path
// that was retired, and the read-only candidates the bundle extraction found.
var internalAPISources = []Source{
	{
		ID:         "internalapi.version",
		Transport:  InternalAPI,
		Path:       internalapi.VersionPath,
		Method:     "GET",
		Provides:   "Device identity: serial number, article number, device name, firmware",
		State:      StateCalled,
		Called:     true,
		Fixture:    "test/fixtures/internalapi/status_version.json",
		Capability: "device.identity",
		Fields: []Field{
			{Path: "Body.Data.serialNumber", Type: "string", Status: FieldDisplayed, Capability: "device.identity"},
			{Path: "Body.Data.devicename", Type: "string", Status: FieldDisplayed,
				Why: "shown as the device name (\"Primo GEN24 3.6 Plus\")"},
			{Path: "Body.Data.articleNumber", Type: "string", Status: FieldDisplayed},
			{Path: "Body.Data.swrevisions.GEN24", Type: "string", Status: FieldDisplayed,
				Why: "the proven firmware key (\"1.19.7-1\"); flat fallbacks remain accepted"},
			{Path: "Body.Data.swrevisions.[10 others]", Type: "object", Status: FieldIgnored,
				Why: "the capture lists eleven embedded components (CoyoteControl, CoyoteCore, DEVICEGROUP, GEN24PRIMO, GEN24ROW, Kronos, P6RW-pilot, Rhea, Zeus, imx6sx-pilot): only GEN24 is read as the firmware version"},
			{Path: "Body.Data.hwrevisions", Type: "object", Status: FieldIgnored,
				Why: "three board revisions of the capture (1PN6K, PILOT, ROX-L) with their serial|article|revision strings: support material, not read"},
			{Path: "Body.Data.apiversions", Type: "object", Status: FieldIgnored,
				Why: "the versions of the interface's own services (commandsApi 4.0.0, configApi 4.0.0, setupAppApi 2.2.0, statusApi 2.6.0, updateApi 1.0.0): not read"},
			{Path: "Body.Data.commonName", Type: "string", Status: FieldIgnored,
				Why: "internal build name of the device group (\"pilot-0.6a-…\"): not read"},
			{Path: "Body.Data.hardwareId", Type: "string", Status: FieldIgnored,
				Why: "internal hardware identifier: not read"},
			{Path: "Body.Data.minAppVersions", Type: "object", Status: FieldIgnored,
				Why: "minimum versions of the mobile applications (minAndroidVersion, minIOSVersion, minWinVersion): not read"},
			{Path: "Body.Data.devicegroup", Type: "string", Status: FieldIgnored,
				Why: "read by the extractor (\"P6RW\" here), not persisted today"},
			{Path: "Body.Data.numberOfPhases", Type: "int", Status: FieldIgnored,
				Why: "read by the extractor (1 here), not persisted today"},
			{Path: "Body.Data.modelName", Type: "string", Status: FieldUnverified,
				Why: "absent from the capture: no model key exists in this payload, so the model stays null rather than mimicking the device name"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "captured 2026-10-16; the identity panel reads it"},
			{Tag: bundleTag, Detail: "the official client reads it too (module yTJA); Digest \"Customer\", also answerable without a header"},
		},
		Docs: []string{"docs/ENDPOINTS.md", "docs/SOURCES.md § Hardware captures"},
	},
	{
		ID:         "internalapi.active_events",
		Transport:  InternalAPI,
		Path:       internalapi.ActiveEventsPath,
		Method:     "GET",
		Provides:   "Active hardware events, republished in full on every poll",
		State:      StateCalled,
		Called:     true,
		Fixture:    "test/fixtures/internalapi/status_activeevents.json",
		Capability: "device.events",
		Fields: []Field{
			{Path: "[].uuid", Type: "string", Status: FieldValidated,
				Why: "instance identity: the first-priority deduplication key", Capability: "device.events"},
			{Path: "[].prefix + [].eventID", Type: "string/int", Status: FieldValidated,
				Why: "second-priority key (\"GEN24-1260\" in the capture)"},
			{Path: "[].severity", Type: "int", Status: FieldDisplayed,
				Why: "device severity integer, mapped to the local event severity (3 here)"},
			{Path: "[].label", Type: "string", Status: FieldDisplayed,
				Why: "shown as the event message (\"LicensedFeatureDisabled\" here)"},
			{Path: "[].code", Type: "string", Status: FieldUnverified,
				Why: "absent from the capture; kept as a deduplication fallback"},
			{Path: "[].state", Type: "string", Status: FieldUnverified,
				Why: "absent from the capture; kept as a deduplication fallback"},
			{Path: "[].timestamp", Type: "int", Status: FieldStored,
				Why: "epoch integer (1792131898 here), kept in the raw detail: the local event carries the collector's clock, not the device's"},
			{Path: "[].category, [].confirmable, [].subID, [].type, [].viewer, [].sourceID", Type: "mixed", Status: FieldStored,
				Why: "kept verbatim in the event's raw detail, never interpreted: filtering on them would need an observed rule, not a guess"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "captured 2026-10-16 and promoted as the fixture: the body is a root JSON array of event objects (the decoder accepts Body.Data first, then the root array)"},
			{Tag: bundleTag, Detail: "the key priority (uuid, then prefix-eventID) comes from the official client (l. 135742, 164699)"},
		},
		Docs: []string{"docs/ENDPOINTS.md", "docs/UNKNOWNS.md"},
		Note: "the payload is not an envelope: it is the bare array. One event was active on the reference installation at capture time.",
	},
	{
		ID:         "internalapi.powermeter",
		Transport:  InternalAPI,
		Path:       internalapi.PowerMeterPath,
		Method:     "GET",
		Provides:   "Presence and channels of the Fronius Smart Meter",
		State:      StateCalled,
		Called:     true,
		Fixture:    "test/fixtures/internalapi/components_powermeter_readable.json",
		Capability: "grid.import_export",
		Fields: []Field{
			{Path: "Body.Data", Type: "object", Status: FieldValidated,
				Why: "non-empty ⇒ present, empty ⇒ absent, unreadable ⇒ undetermined (never \"absent\")"},
			{Path: "Body.Data.channels[SMARTMETER_*]", Type: "array", Status: FieldUnverified,
				Why:        "23 documented meter signals, unread: no meter is installed here. Would unlock measured consumption if one were fitted",
				Capability: "load.consumption"},
			{Path: "Body.Data.attributes.addr", Type: "string", Status: FieldUnverified,
				Why: "meter address, unread: no meter is installed here"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "captured 2026-10-16: 200 with an empty Body.Data ⇒ no meter (unknown #4)"},
			{Tag: bundleTag, Detail: "the official client reads 23 SMARTMETER_* signals in channels"},
		},
		Docs: []string{"docs/ENDPOINTS.md", "docs/UNKNOWNS.md #4"},
	},
	{
		ID:         "internalapi.battery",
		Transport:  InternalAPI,
		Path:       internalapi.BatteryPath,
		Method:     "GET",
		Provides:   "Presence and channels of the battery management system",
		State:      StateCalled,
		Called:     true,
		Fixture:    "test/fixtures/internalapi/components_batterymanagementsystem_readable.json",
		Capability: "battery.soc",
		Fields: []Field{
			{Path: "Body.Data", Type: "object", Status: FieldValidated,
				Why: "same presence rule as the meter"},
			{Path: "Body.Data.channels[BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16]", Type: "array", Status: FieldUnverified,
				Why:        "the relative state of charge the official client reads, in %: unread here, no battery is installed (unknown #5)",
				Capability: "battery.soc"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware, Detail: "captured 2026-10-16: 200 with an empty Body.Data ⇒ no battery (unknown #5)"},
			{Tag: bundleTag, Detail: "the signal name comes from the official client (l. 128986)"},
		},
		Docs: []string{"docs/ENDPOINTS.md", "docs/UNKNOWNS.md #5"},
	},
	{
		ID:        "internalapi.powerflow",
		Transport: InternalAPI,
		Path:      "/status/powerflow",
		Method:    "GET",
		Provides:  "The internal counterpart of the Solar API flow answer (site + inverters)",
		State:     StateArchived,
		Fixture:   "test/fixtures/internalapi/status_powerflow.json",
		Fields: []Field{
			{Path: "Body.Data.site.E_Day", Type: "null", Status: FieldUnverified,
				Why: "null on this firmware — the reason the path was retired", Capability: "production.energy_day"},
			{Path: "Body.Data.site.E_Total", Type: "float", Status: FieldUnverified,
				Why: "would duplicate the Solar API total; the path is no longer called"},
			{Path: "Body.Data.inverters", Type: "array", Status: FieldUnverified,
				Why: "array, unlike the Solar API map; unread since the cutover"},
		},
		Proofs: []Proof{
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "captured 2026-10-16: 200, digest_required false, both counters null ⇒ it filled nothing"},
		},
		Docs: []string{"docs/UNKNOWNS.md #13", "docs/ENDPOINTS.md"},
		Note: "removed from the allowlist on purpose (clean cutover): the capture is kept as evidence, the path is not called.",
	},
	{
		ID:        "internalapi.common",
		Transport: InternalAPI,
		Path:      "/status/common",
		Method:    "GET",
		Provides:  "System-wide state: updates running, service state, configuration flags",
		State:     StateCalled,
		Called:    true,
		Fields: []Field{
			{Path: "Body.Data.updateRunning", Type: "bool", Status: FieldUnverified,
				Why: bundleTag + ": read by the official client's route guard (l. 81353), never called here",
			},
		},
		Proofs: []Proof{{Tag: bundleTag, Detail: "polled registry entry (l. 76041), interval 5 s, envelope Default"}},
		Docs:   []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
	{
		ID:        "internalapi.devices",
		Transport: InternalAPI,
		Path:      "/status/devices",
		Method:    "GET",
		Provides:  "The devices the system knows, with their state",
		State:     StateCalled,
		Called:    true,
		Proofs:    []Proof{{Tag: bundleTag, Detail: "polled registry entry (l. 76027), interval 5 s"}},
		Docs:      []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
	{
		ID:        "internalapi.network",
		Transport: InternalAPI,
		Path:      "/status/network",
		Method:    "GET",
		Provides:  "Interface state and addressing",
		State:     StateCalled,
		Called:    true,
		Proofs:    []Proof{{Tag: bundleTag, Detail: "polled registry entry (l. 76028)"}},
		Docs:      []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
	{
		ID:        "internalapi.emrs",
		Transport: InternalAPI,
		Path:      "/status/emrs",
		Method:    "GET",
		Provides:  "Energy management / grid-code state",
		State:     StateCalled,
		Called:    true,
		Proofs:    []Proof{{Tag: bundleTag, Detail: "polled registry entry (l. 76029)"}},
		Docs:      []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
	{
		ID:        "internalapi.cache",
		Transport: InternalAPI,
		Path:      "/components/cache/readable",
		Method:    "GET",
		Provides:  "Inverter readable cache: the richest per-device signal set of the interface",
		State:     StateCalled,
		Called:    true,
		Proofs: []Proof{
			{Tag: bundleTag, Detail: "polled registry entry (l. 76033), envelope Cgi"},
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "surveyed 2026-10-16: class ok, 4897 bytes, 99 values — one node (393216), ten attributes, 81 registers: 44 named by the device with its own type suffix (F32, U64, U16), 37 bare `<id>` identifiers"},
			{Tag: verifiedTag, Firmware: registeredFirmware,
				Detail: "read by the detector each cycle and published by GET /api/v1/detail, which the device detail page renders as the Cache block"},
		},
		Docs: []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
		Note: "read now that the survey observed it: the named registers give back the electrical detail the flow endpoint never carried (PV, DC link, AC bridge, module temperatures, isolation resistance) and the bare ids are shown as they are. No unit is asserted from a name: the device states the name and the type suffix, not the unit.",
	},
	{
		ID:        "internalapi.ohmpilot",
		Transport: InternalAPI,
		Path:      "/components/Ohmpilot/readable",
		Method:    "GET",
		Provides:  "Ohmpilot (hot-water load) signals",
		State:     StateCandidate,
		Proofs:    []Proof{{Tag: bundleTag, Detail: "polled registry entry (l. 76034), envelope Cgi"}},
		Docs:      []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
	{
		ID:        "internalapi.utc_time",
		Transport: InternalAPI,
		Path:      "/commands/GetUTCTime",
		Method:    "GET",
		Provides:  "The device clock in UTC",
		State:     StateCandidate,
		Proofs: []Proof{
			{Tag: bundleTag, Detail: "polled registry entry (l. 76037), interval 15 s, envelope CommandResult, the single entry declaring user: \"customer\""},
		},
		Docs: []string{"docs/ENDPOINTS.md § Declared roles (client only)"},
		Note: "would let the collector compare its clock with the device's — a diagnostic, not a measurement.",
	},
	{
		ID:        "internalapi.service_state",
		Transport: InternalAPI,
		Path:      "/commands/GetServiceState",
		Query:     "serviceId=<id>",
		Method:    "GET",
		Provides:  "State of one system service (the grid-code autotest is the declared use)",
		State:     StateCandidate,
		Proofs: []Proof{
			{Tag: bundleTag, Detail: "registry entry (l. 76043) with the only argument of the registry: serviceId from T_SystemServiceId"},
		},
		Docs: []string{"docs/ENDPOINTS.md § Method, arguments and DTO per path"},
	},
}
