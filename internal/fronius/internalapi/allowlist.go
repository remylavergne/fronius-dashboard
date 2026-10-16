// Allowlist of the internal Fronius Webinterface paths.
//
// It is the executable permission matrix of phase 2: a path absent from this
// table is not callable (the client refuses before building the request), and no
// entry is of class MUTATION or DANGEROUS. The 79 paths catalogued in
// docs/ENDPOINTS.md stay out of the list: they only serve to know what must not
// be called.
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
		Evidence: "bundle references/fronius-original.js: polled registry l. 76042 (module aYRN), 401 retry l. 75961-75963; headerless path l. 183050 (module yTJA); fields serialNumber l. 45243, numberOfPhases and devicegroup l. 183034-183035. Verified by cmd/fronius-sim (/status/version).",
	},
	{
		Path:     ActiveEventsPath,
		Class:    ClassRead,
		Envelope: EnvelopeDefault,
		Digest:   true,
		Purpose:  "active events reported by the device",
		Evidence: "bundle: polled registry l. 76030 (module aYRN, interval 10,000 ms). Body shape NOT established (docs/UNKNOWNS.md); the raw detail is kept. Verified by cmd/fronius-sim (/status/activeEvents).",
	},
	{
		Path:     PowerMeterPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "presence of a Fronius Smart Meter",
		Evidence: "bundle: polled registry l. 76031 with Cgi envelope l. 75958 (module aYRN); Body.Data decoding l. 183011 (module yTJA). No named field is proven: only the presence is inferred. Verified by cmd/fronius-sim (/components/PowerMeter/readable).",
	},
	{
		Path:     BatteryPath,
		Class:    ClassRead,
		Envelope: EnvelopeCgi,
		Digest:   true,
		Purpose:  "presence of a battery",
		Evidence: "bundle: polled registry l. 76032 with Cgi envelope (module aYRN). No named field is proven. Verified by cmd/fronius-sim (/components/BatteryManagementSystem/readable).",
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
