// Detection of the presence of a component or a resource.
//
// Three states, and only three: `absent` is a conclusion (the device has
// answered and the component is not there), `unknown` is the absence of a
// conclusion. Confusing them would mean asserting an absence without proof.
package model

import "time"

// DetectionState is the state of a detection.
type DetectionState string

const (
	// StateUnknown: the read did not succeed (missing credentials, 401, 404,
	// network failure, unreadable body). This is NOT an absence.
	StateUnknown DetectionState = "unknown"
	// StateAbsent: the device has answered and does not report this component.
	StateAbsent DetectionState = "absent"
	// StatePresent: the device reports this component.
	StatePresent DetectionState = "present"
)

// ResourceDetection describes the state of a detection, with its provenance.
type ResourceDetection struct {
	State DetectionState
	// Source: endpoint or mechanism that produced the state, empty if unknown.
	Source string
	// Reason: cause of the state when it is not StatePresent.
	Reason string
}

// Detection groups the four detections of phase 2.
type Detection struct {
	Identity     ResourceDetection
	Meter        ResourceDetection
	Battery      ResourceDetection
	DeviceEvents ResourceDetection
}

// DeviceHealth groups the two readings of the device's own /commands/* paths:
// its clock and the state of its grid-limits self-test.
//
// These are not component presences, so they are deliberately kept out of
// Detection: a clock has no "absent" answer, it either answered or did not.
type DeviceHealth struct {
	Clock   ClockReading
	Service ServiceReading
}

// ClockReading is the reading of the device's own clock
// (/commands/GetUTCTime).
type ClockReading struct {
	ResourceDetection
	// DeviceUTC is the time the device reported; the zero time when the
	// clock was not read.
	DeviceUTC time.Time
	// Drift is device minus host, measured at the same instant. It is 0
	// when the clock was not read — a zero drift is only meaningful
	// alongside a non-zero DeviceUTC.
	Drift time.Duration
}

// ServiceReading is the reading of one system service's state
// (/commands/GetServiceState).
type ServiceReading struct {
	ResourceDetection
	// Value is the state string the device reported; "" when nothing was
	// read.
	Value string
}
