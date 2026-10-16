// Allowlist of the internal Fronius Webinterface paths.
//
// It is the executable permission matrix of phase 2: a path absent from this
// table is not callable (the client refuses before building the request), and no
// entry is of class MUTATION or DANGEROUS. The 79 paths catalogued in
// docs/ENDPOINTS.md stay out of the list: they only serve to know what must not
// be called. Every entry here is justified by a metric the Solar API does not
// provide (docs/adr/0008-exposed-data-scope.md).
package internalapi

import "errors"

// Class classifies an endpoint by the effect it may have.
type Class string

const (
	// ClassRead: read-only, no side effect.
	ClassRead Class = "read"
	// ClassMutation: modifies a device setting.
	ClassMutation Class = "mutation"
	// ClassDangerous: operational command (shutdown, self-test, service).
	ClassDangerous Class = "dangerous"
)

// Envelope describes the shape of the response payload.
type Envelope string

const (
	// EnvelopeDefault: the whole body is the payload.
	EnvelopeDefault Envelope = "default"
	// EnvelopeCgi: the payload is Body.Data, an object indexed by component id
	// (possibly empty).
	EnvelopeCgi Envelope = "cgi"
)

// Endpoint describes an allowed path.
type Endpoint struct {
	Path     string
	Class    Class
	Envelope Envelope
	// Digest: the official client reaches this path via callGet$ (GET + Digest,
	// one retry on 401). true does not mean "the path requires authentication":
	// module yTJA proves that some of these paths also respond without a header.
	Digest bool
	// Purpose: why the application calls this path.
	Purpose string
	// Evidence: bundle line and simulator verification.
	Evidence string
}

// Allowed paths.
const (
	VersionPath      = "/status/version"
	ActiveEventsPath = "/status/activeEvents"
	PowerMeterPath   = "/components/PowerMeter/readable"
	BatteryPath      = "/components/BatteryManagementSystem/readable"
	// The diagnostic paths added after the survey of 2026-10-16 observed them
	// (docs/SOURCES.md § First survey of the reference installation). Each
	// answers a bare JSON body: the polled registry reads them with the Default
	// envelope, not the Cgi one.
	CommonPath  = "/status/common"
	NetworkPath = "/status/network"
	DevicesPath = "/status/devices"
	EmrsPath    = "/status/emrs"
)

// ErrNotAllowed is returned by Get for any path outside the allowlist. It is a
// default-deny: the caller cannot target an arbitrary URL, even by building the
// path dynamically.
var ErrNotAllowed = errors.New("internalapi: path outside the allowlist")

// allowlist is the table, in a stable order.
var allowlist = []Endpoint{
	{
		Path:     VersionPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "device identity: serial number, device group, number of phases",
		Evidence: "bundle references/fronius-original.js: headerless path l. 183050 (module yTJA) — this path is NOT in the polled aYRN registry; fields serialNumber l. 45243, numberOfPhases and devicegroup l. 183034-183035, firmware swrevisions.GEN24 l. 128882. Captured 2026-10-16 on firmware 1.19.7-1: 200 without Authorization. Verified by cmd/fronius-sim.",
	},
	{
		Path:     ActiveEventsPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "active events reported by the device",
		Evidence: "bundle: polled registry l. 76030 (module aYRN, interval 10,000 ms); field mapping l. 135689-135701. Captured 2026-10-16 on firmware 1.19.7-1: root array, integer severity, uuid identity. Verified by cmd/fronius-sim.",
	},
	{
		Path:     PowerMeterPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "presence of a Fronius Smart Meter",
		Evidence: "bundle: polled registry l. 76035 with Cgi envelope l. 75958 (module aYRN); Body.Data decoding l. 183011 (module yTJA). Presence is inferred from a non-empty Body.Data. Captured 2026-10-16 on firmware 1.19.7-1 (empty Body.Data: no meter, Content-Type text/javascript). Verified by cmd/fronius-sim.",
	},
	{
		Path:     BatteryPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "presence of a battery",
		Evidence: "bundle: polled registry l. 76032 with Cgi envelope (module aYRN). Captured 2026-10-16 on firmware 1.19.7-1 (empty Body.Data: no battery). Verified by cmd/fronius-sim.",
	},
	{
		Path:     CommonPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "device-wide state: system name, service state, update running, support enabled, declared account kinds",
		Evidence: "bundle: polled registry l. 76041 (module aYRN, interval 5,000 ms, Default envelope). Surveyed 2026-10-16 on firmware 1.19.7-1: 200 without Authorization, 349 bytes, 10 fields (systemName, serviceState, updateRunning, supportEnabled, networkState.connectedViaAP, networkState.interfaces[], passwordState.customer, passwordState.technician). The account booleans are what the DEVICE declares, never a permission granted to this application (docs/UNKNOWNS.md #6).",
	},
	{
		Path:     NetworkPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "network interfaces: addressing, mode and radio state",
		Evidence: "bundle: polled registry l. 76028 (module aYRN, Default envelope). Surveyed 2026-10-16 on firmware 1.19.7-1: 200 without Authorization, 768 bytes, 38 fields — one entry per interface (eth0, eth1) with ip.{ip,mask,gateway,dns[],configured} and network.{interface_mode,status,ssid,wps_state}, plus isFDMPConnected.",
	},
	{
		Path:     DevicesPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "inventory of the devices the device knows, with their status",
		Evidence: "bundle: polled registry l. 76027 (module aYRN, Default envelope). Surveyed 2026-10-16 on firmware 1.19.7-1: 200 without Authorization, 65 bytes — a bare JSON array whose entries carry type, status and statusMessage.",
	},
	{
		Path:     EmrsPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "EMRS pin states",
		Evidence: "bundle: polled registry l. 76029 (module aYRN, Default envelope). Surveyed 2026-10-16 on firmware 1.19.7-1: 200 without Authorization, 214 bytes, 8 fields — four pins (pin1…pin4), each with state and its reason.",
	},
}

// Allowlist returns a copy of the allowlist, in a stable order.
func Allowlist() []Endpoint {
	out := make([]Endpoint, len(allowlist))
	copy(out, allowlist)
	return out
}

// Lookup returns the entry for a path.
func Lookup(path string) (Endpoint, bool) {
	for _, ep := range allowlist {
		if ep.Path == path {
			return ep, true
		}
	}
	return Endpoint{}, false
}
