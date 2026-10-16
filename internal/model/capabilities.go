package model

import "strings"

// Hardware or software prerequisites of a capability.
const (
	RequiresSmartMeter  = "smart_meter"
	RequiresBattery     = "battery"
	RequiresInternalAPI = "adapter:internalapi"
)

// Evidence levels associated with a capability.
const (
	// EvidenceConversation: value reported during the analysis, not replayed.
	EvidenceConversation = "conversation"
	// EvidenceSimulator: behaviour reproduced by the local simulator.
	EvidenceSimulator = "simulator"
	// EvidenceLocal: property of the application itself, verifiable here.
	EvidenceLocal = "local"
	// EvidenceBundle: path and envelope extracted from the official client
	// (references/fronius-original.js). This is NOT a firmware permission:
	// it is the proof that a path exists and in what form.
	EvidenceBundle = "bundle"
)

// Reasons and caveats, reused as-is by the API and the interface.
const (
	// ReasonUnverifiedSchema is a permanent caveat: it signals that the
	// JSON shape has not been observed on the installation's firmware.
	ReasonUnverifiedSchema = "Schema not validated on firmware 1.19.7-1"
	ReasonNoSmartMeter     = "No Fronius Smart Meter detected"
	ReasonNoBattery        = "No battery installed"
	ReasonBatteryReadout   = "Battery detected: state-of-charge readout not implemented (later phase)"
	ReasonUnavailable      = "Unavailable"
	// causeInconnue is the fallback when the detection could not name its
	// cause: the reason stays explicit and never ends with ":".
	causeInconnue = "unknown cause"
)

// Capability describes what the supervision can really restitute.
//
// An unavailable capability is never represented by a zero numeric
// value: it carries Available = false and a non-empty Reason, which the
// /api/v1/capabilities endpoints and the interface cards reflect.
//
// Reason has two distinct uses, never simultaneous: an unavailability reason
// when Available is false, a caveat when the capability is available but
// not verified on the hardware (production.*).
type Capability struct {
	ID        string
	Label     string
	Available bool
	Quality   Quality
	Requires  []string
	ReadOnly  bool
	Verified  bool
	Evidence  []string
	Reason    string
}

// capSpec describes a capability before arbitration between reason and caveat.
type capSpec struct {
	id, label string
	// available decides from the real detections: no capability is
	// derived from a configuration boolean any more.
	available func(Detection) bool
	requires  []string
	// reason returns the applicable reason: unavailability reason if the
	// capability is unavailable, caveat if it is available.
	reason   func(Detection) string
	verified bool
	evidence []string
}

// Capabilities returns the state of the capabilities, in a stable order.
//
// The decision relies on the internal detection (internal/detect): `unknown`
// is not `absent`, and an unavailable capability always carries
// the reason that explains it.
func Capabilities(d Detection) []Capability {
	specs := []capSpec{
		{
			id: "production.power", label: "Instantaneous production",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_day", label: "Daily energy",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "production.energy_total", label: "Total energy",
			available: always, reason: alwaysReason(ReasonUnverifiedSchema),
			evidence: []string{EvidenceConversation, EvidenceSimulator},
		},
		{
			id: "grid.import_export", label: "Grid import / export",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Smart Meter presence not determined",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			id: "load.consumption", label: "Consumption",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Smart Meter presence not determined",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			id: "autoconsumption.ratio", label: "Self-consumption ratio",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Meter }),
			requires:  []string{RequiresSmartMeter},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Meter },
				"Smart Meter presence not determined",
				ReasonNoSmartMeter),
			evidence: []string{EvidenceConversation},
		},
		{
			// Detecting a battery does not prove that a state-of-charge
			// measurement exists on the API side: announcing it as available
			// without a source would be exactly the opposite of the
			// "no invented zero" invariant.
			id: "battery.soc", label: "Battery state of charge",
			available: never,
			requires:  []string{RequiresBattery},
			reason:    batteryReason(),
			evidence:  []string{EvidenceConversation},
		},
		{
			id: "device.identity", label: "Inverter identity",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.Identity }),
			requires:  []string{RequiresInternalAPI},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.Identity },
				"Inverter identity not determined",
				""),
			evidence: []string{EvidenceBundle, EvidenceSimulator},
		},
		{
			id: "device.events", label: "Device events",
			available: detectionIs(StatePresent, func(d Detection) ResourceDetection { return d.DeviceEvents }),
			requires:  []string{RequiresInternalAPI},
			reason: detectionReason(
				func(d Detection) ResourceDetection { return d.DeviceEvents },
				"Device events not determined",
				""),
			evidence: []string{EvidenceBundle, EvidenceSimulator},
		},
		{
			id: "system.collector", label: "Local collector",
			available: always, reason: alwaysReason(""),
			verified: true, evidence: []string{EvidenceLocal},
		},
	}

	caps := make([]Capability, 0, len(specs))
	for _, spec := range specs {
		available := spec.available(d)
		c := Capability{
			ID:        spec.id,
			Label:     spec.label,
			Available: available,
			Requires:  spec.requires,
			ReadOnly:  true,
			Verified:  spec.verified,
			Evidence:  spec.evidence,
		}
		if available {
			c.Quality = QualityMeasured
		} else {
			c.Quality = QualityMissing
		}
		c.Reason = spec.reason(d)
		if c.Reason == "" && !available {
			c.Reason = ReasonUnavailable
		}
		caps = append(caps, c)
	}
	return caps
}

func always(Detection) bool { return true }

func never(Detection) bool { return false }

// detectionIs builds a predicate "this resource is in the desired state".
func detectionIs(state DetectionState, pick func(Detection) ResourceDetection) func(Detection) bool {
	return func(d Detection) bool { return pick(d).State == state }
}

// alwaysReason returns a constant reason, independent of the detections.
func alwaysReason(reason string) func(Detection) string {
	return func(Detection) string { return reason }
}

// detectionReason builds the reason of a capability tied to a detection.
//
// present  → "" (or absentReason if the read is not implemented)
// absent   → absentReason, or the prefix followed by the cause if the cause says more
// unknown  → prefix followed by the cause
func detectionReason(pick func(Detection) ResourceDetection, prefix, absentReason string) func(Detection) string {
	return func(d Detection) string {
		r := pick(d)
		switch r.State {
		case StatePresent:
			return ""
		case StateAbsent:
			if absentReason != "" {
				return absentReason
			}
			return withCause(prefix, r.Reason)
		default:
			return withCause(prefix, r.Reason)
		}
	}
}

// batteryReason: the detected battery does not unlock the state-of-charge
// readout, which is not implemented in phase 2. The reason says so explicitly.
func batteryReason() func(Detection) string {
	return func(d Detection) string {
		switch d.Battery.State {
		case StatePresent:
			return ReasonBatteryReadout
		case StateAbsent:
			return ReasonNoBattery
		default:
			return withCause("Battery presence not determined", d.Battery.Reason)
		}
	}
}

// withCause assembles a reason and its cause without ever producing a reason
// that ends with ":".
func withCause(prefix, cause string) string {
	cause = strings.TrimSpace(cause)
	if cause == "" {
		cause = causeInconnue
	}
	return prefix + ": " + cause
}
