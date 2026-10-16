package model

import (
	"reflect"
	"strings"
	"testing"
)

// wantOrder is the expected stable order of the capabilities.
var wantOrder = []string{
	"production.power",
	"production.energy_day",
	"production.energy_total",
	"grid.import_export",
	"load.consumption",
	"autoconsumption.ratio",
	"battery.soc",
	"device.identity",
	"device.events",
	"system.collector",
}

// detectionWith builds an explicit detection for the tests.
func detectionWith(meter, battery, identity, events DetectionState) Detection {
	return Detection{
		Meter:        ResourceDetection{State: meter, Source: "/components/PowerMeter/readable", Reason: "meter cause"},
		Battery:      ResourceDetection{State: battery, Source: "/components/BatteryManagementSystem/readable", Reason: "battery cause"},
		Identity:     ResourceDetection{State: identity, Source: "/status/version", Reason: "identity cause"},
		DeviceEvents: ResourceDetection{State: events, Source: "/status/activeEvents", Reason: "events cause"},
	}
}

// allUnknown is the state of a startup with no successful read.
func allUnknown() Detection {
	return Detection{
		Identity:     ResourceDetection{State: StateUnknown, Reason: "no identity read has succeeded yet"},
		Meter:        ResourceDetection{State: StateUnknown, Reason: "no identity read has succeeded yet"},
		Battery:      ResourceDetection{State: StateUnknown, Reason: "no identity read has succeeded yet"},
		DeviceEvents: ResourceDetection{State: StateUnknown, Reason: "no identity read has succeeded yet"},
	}
}

func TestCapabilitiesOrderIsStable(t *testing.T) {
	caps := Capabilities(allUnknown())
	if len(caps) != len(wantOrder) {
		t.Fatalf("len = %d, want %d", len(caps), len(wantOrder))
	}
	for i, want := range wantOrder {
		if caps[i].ID != want {
			t.Fatalf("position %d = %q, want %q", i, caps[i].ID, want)
		}
	}
}

func TestCapabilitiesAlwaysAvailable(t *testing.T) {
	// production.* and system.collector depend on no detection.
	for _, d := range []Detection{allUnknown(), detectionWith(StatePresent, StatePresent, StatePresent, StatePresent)} {
		caps := byID(t, Capabilities(d))
		for _, id := range []string{"production.power", "production.energy_day", "production.energy_total", "system.collector"} {
			if !caps[id].Available {
				t.Fatalf("%s should be available (reason=%q)", id, caps[id].Reason)
			}
			if caps[id].Quality == QualityMissing {
				t.Fatalf("%s: an available capability cannot have quality missing", id)
			}
		}
		if !caps["system.collector"].Verified {
			t.Fatal("system.collector must be verified (property of the application)")
		}
		for _, id := range []string{"production.power", "production.energy_day", "production.energy_total"} {
			if caps[id].Verified {
				t.Fatalf("%s cannot be verified before a hardware capture", id)
			}
			if !strings.Contains(caps[id].Reason, "not validated") {
				t.Fatalf("%s: Reason = %q must signal the unvalidated schema", id, caps[id].Reason)
			}
		}
	}
}

// TestMeterCapabilitiesMatrix: the three meter states, with the exact reason.
func TestMeterCapabilitiesMatrix(t *testing.T) {
	meterIDs := []string{"grid.import_export", "load.consumption", "autoconsumption.ratio"}

	cases := []struct {
		state     DetectionState
		available bool
		reason    string
	}{
		{StatePresent, true, ""},
		{StateAbsent, false, ReasonNoSmartMeter},
		{StateUnknown, false, "Smart Meter presence not determined: meter cause"},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(tc.state, StateUnknown, StateUnknown, StateUnknown)))
			for _, id := range meterIDs {
				cap := caps[id]
				if cap.Available != tc.available {
					t.Fatalf("%s: available = %v, want %v", id, cap.Available, tc.available)
				}
				if cap.Reason != tc.reason {
					t.Fatalf("%s: reason = %q, want %q", id, cap.Reason, tc.reason)
				}
				if !tc.available && cap.Quality != QualityMissing {
					t.Fatalf("%s: quality = %q", id, cap.Quality)
				}
				if len(cap.Requires) == 0 {
					t.Fatalf("%s: Requires must name the prerequisite", id)
				}
			}
		})
	}
}

// TestBatterySocIsNeverAvailableInPhase2: presence detection does not prove
// that a state-of-charge measurement exists on the API side.
func TestBatterySocIsNeverAvailableInPhase2(t *testing.T) {
	cases := []struct {
		state  DetectionState
		reason string
	}{
		{StatePresent, ReasonBatteryReadout},
		{StateAbsent, ReasonNoBattery},
		{StateUnknown, "Battery presence not determined: battery cause"},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(StateUnknown, tc.state, StateUnknown, StateUnknown)))
			cap := caps["battery.soc"]
			if cap.Available {
				t.Fatal("battery.soc must never be available in phase 2")
			}
			if cap.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", cap.Reason, tc.reason)
			}
			if cap.Quality != QualityMissing {
				t.Fatalf("quality = %q", cap.Quality)
			}
		})
	}
}

func TestIdentityAndEventsMatrix(t *testing.T) {
	cases := []struct {
		name      string
		state     DetectionState
		available bool
		identity  string
		events    string
	}{
		{
			name: "present", state: StatePresent, available: true,
		},
		{
			name: "absent", state: StateAbsent, available: false,
			identity: "Inverter identity not determined: identity cause",
			events:   "Device events not determined: events cause",
		},
		{
			name: "unknown", state: StateUnknown, available: false,
			identity: "Inverter identity not determined: identity cause",
			events:   "Device events not determined: events cause",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(StateUnknown, StateUnknown, tc.state, tc.state)))
			if got := caps["device.identity"].Available; got != tc.available {
				t.Fatalf("device.identity available = %v", got)
			}
			if got := caps["device.events"].Available; got != tc.available {
				t.Fatalf("device.events available = %v", got)
			}
			if caps["device.identity"].Reason != tc.identity {
				t.Fatalf("identity reason = %q, want %q", caps["device.identity"].Reason, tc.identity)
			}
			if caps["device.events"].Reason != tc.events {
				t.Fatalf("events reason = %q, want %q", caps["device.events"].Reason, tc.events)
			}
		})
	}
}

// TestCapabilitiesIndependence: one detection does not unlock another.
func TestCapabilitiesIndependence(t *testing.T) {
	onlyMeter := byID(t, Capabilities(detectionWith(StatePresent, StateUnknown, StateUnknown, StateUnknown)))
	if !onlyMeter["grid.import_export"].Available {
		t.Fatal("grid.import_export must be available with a detected Smart Meter")
	}
	if onlyMeter["battery.soc"].Available {
		t.Fatal("battery.soc must not depend on the Smart Meter")
	}
	if onlyMeter["device.identity"].Available {
		t.Fatal("device.identity must not depend on the Smart Meter")
	}

	onlyBattery := byID(t, Capabilities(detectionWith(StateUnknown, StatePresent, StateUnknown, StateUnknown)))
	if onlyBattery["grid.import_export"].Available {
		t.Fatal("grid.import_export must not depend on the battery")
	}

	onlyIdentity := byID(t, Capabilities(detectionWith(StateUnknown, StateUnknown, StatePresent, StateUnknown)))
	if onlyIdentity["device.events"].Available {
		t.Fatal("device.events must not depend on the identity")
	}
}

// TestCapabilitiesEveryUnavailableHasReason: invariant carried through to the API.
func TestCapabilitiesEveryUnavailableHasReason(t *testing.T) {
	states := []DetectionState{StateUnknown, StateAbsent, StatePresent}
	for _, meter := range states {
		for _, battery := range states {
			for _, identity := range states {
				for _, events := range states {
					d := detectionWith(meter, battery, identity, events)
					for _, cap := range Capabilities(d) {
						if cap.Available {
							continue
						}
						if cap.Reason == "" {
							t.Fatalf("capability %q unavailable without reason (meter=%s battery=%s identity=%s events=%s)",
								cap.ID, meter, battery, identity, events)
						}
						if cap.Quality != QualityMissing {
							t.Fatalf("capability %q unavailable with quality %q", cap.ID, cap.Quality)
						}
					}
				}
			}
		}
	}
}

// TestReasonsNeverEndWithSeparator: a reason that ends with ":" announces
// a cause it does not name.
func TestReasonsNeverEndWithSeparator(t *testing.T) {
	states := []DetectionState{StateUnknown, StateAbsent, StatePresent}

	// Detection whose causes are deliberately empty: the fallback must
	// produce a complete reason, never an orphan separator.
	empty := Detection{
		Meter:        ResourceDetection{State: StateUnknown},
		Battery:      ResourceDetection{State: StateUnknown},
		Identity:     ResourceDetection{State: StateUnknown},
		DeviceEvents: ResourceDetection{State: StateUnknown},
	}

	for _, meter := range states {
		for _, battery := range states {
			for _, identity := range states {
				for _, events := range states {
					for _, d := range []Detection{detectionWith(meter, battery, identity, events), empty} {
						for _, cap := range Capabilities(d) {
							if cap.Reason == "" {
								continue
							}
							if strings.HasSuffix(cap.Reason, ": ") || strings.HasSuffix(cap.Reason, ":") {
								t.Fatalf("capability %q: truncated reason %q", cap.ID, cap.Reason)
							}
						}
					}
				}
			}
		}
	}
}

// TestCapabilitiesReturnsFreshSlice: the caller must not be able to corrupt
// the state returned by a following call.
func TestCapabilitiesReturnsFreshSlice(t *testing.T) {
	first := Capabilities(allUnknown())
	first[0].Available = false
	first[3].Requires = append(first[3].Requires, "corrupted")

	second := Capabilities(allUnknown())
	if !second[0].Available {
		t.Fatal("the first call modified the state of a later call")
	}
	if !reflect.DeepEqual(second[3].Requires, Capabilities(allUnknown())[3].Requires) {
		t.Fatal("the Requires slices are shared between calls")
	}
}

func byID(t *testing.T, caps []Capability) map[string]Capability {
	t.Helper()
	out := make(map[string]Capability, len(caps))
	for _, c := range caps {
		if _, dup := out[c.ID]; dup {
			t.Fatalf("capability %q duplicated", c.ID)
		}
		if c.ID == "" || c.Label == "" {
			t.Fatalf("incomplete capability: %+v", c)
		}
		if !c.ReadOnly {
			t.Fatalf("%s must be read-only", c.ID)
		}
		if len(c.Evidence) == 0 {
			t.Fatalf("%s: the provenance must be filled in", c.ID)
		}
		out[c.ID] = c
	}
	return out
}
