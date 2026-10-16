// Detection of the presence of a component or a resource.
//
// Three states, and only three: `absent` is a conclusion (the device has
// answered and the component is not there), `unknown` is the absence of a
// conclusion. Confusing them would mean asserting an absence without proof.
package model

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
