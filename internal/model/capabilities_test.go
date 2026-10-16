package model

import (
	"reflect"
	"strings"
	"testing"
)

// wantOrder est l'ordre stable attendu des capacités.
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

// detectionWith construit une détection explicite pour les tests.
func detectionWith(meter, battery, identity, events DetectionState) Detection {
	return Detection{
		Meter:        ResourceDetection{State: meter, Source: "/components/PowerMeter/readable", Reason: "cause compteur"},
		Battery:      ResourceDetection{State: battery, Source: "/components/BatteryManagementSystem/readable", Reason: "cause batterie"},
		Identity:     ResourceDetection{State: identity, Source: "/status/version", Reason: "cause identité"},
		DeviceEvents: ResourceDetection{State: events, Source: "/status/activeEvents", Reason: "cause événements"},
	}
}

// allUnknown est l'état d'un démarrage sans aucune lecture aboutie.
func allUnknown() Detection {
	return Detection{
		Identity:     ResourceDetection{State: StateUnknown, Reason: "aucune lecture d'identité n'a encore abouti"},
		Meter:        ResourceDetection{State: StateUnknown, Reason: "aucune lecture d'identité n'a encore abouti"},
		Battery:      ResourceDetection{State: StateUnknown, Reason: "aucune lecture d'identité n'a encore abouti"},
		DeviceEvents: ResourceDetection{State: StateUnknown, Reason: "aucune lecture d'identité n'a encore abouti"},
	}
}

func TestCapabilitiesOrderIsStable(t *testing.T) {
	caps := Capabilities(allUnknown())
	if len(caps) != len(wantOrder) {
		t.Fatalf("len = %d, attendu %d", len(caps), len(wantOrder))
	}
	for i, want := range wantOrder {
		if caps[i].ID != want {
			t.Fatalf("position %d = %q, attendu %q", i, caps[i].ID, want)
		}
	}
}

func TestCapabilitiesAlwaysAvailable(t *testing.T) {
	// production.* et system.collector ne dépendent d'aucune détection.
	for _, d := range []Detection{allUnknown(), detectionWith(StatePresent, StatePresent, StatePresent, StatePresent)} {
		caps := byID(t, Capabilities(d))
		for _, id := range []string{"production.power", "production.energy_day", "production.energy_total", "system.collector"} {
			if !caps[id].Available {
				t.Fatalf("%s devrait être disponible (reason=%q)", id, caps[id].Reason)
			}
			if caps[id].Quality == QualityMissing {
				t.Fatalf("%s : une capacité disponible ne peut pas être de qualité missing", id)
			}
		}
		if !caps["system.collector"].Verified {
			t.Fatal("system.collector doit être vérifié (propriété de l'application)")
		}
		for _, id := range []string{"production.power", "production.energy_day", "production.energy_total"} {
			if caps[id].Verified {
				t.Fatalf("%s ne peut pas être vérifié avant capture matérielle", id)
			}
			if !strings.Contains(caps[id].Reason, "non validé") {
				t.Fatalf("%s : Reason = %q doit signaler le schéma non validé", id, caps[id].Reason)
			}
		}
	}
}

// TestMeterCapabilitiesMatrix : les trois états du compteur, avec le motif exact.
func TestMeterCapabilitiesMatrix(t *testing.T) {
	meterIDs := []string{"grid.import_export", "load.consumption", "autoconsumption.ratio"}

	cases := []struct {
		state     DetectionState
		available bool
		reason    string
	}{
		{StatePresent, true, ""},
		{StateAbsent, false, ReasonNoSmartMeter},
		{StateUnknown, false, "Présence d'un Smart Meter non déterminée : cause compteur"},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(tc.state, StateUnknown, StateUnknown, StateUnknown)))
			for _, id := range meterIDs {
				cap := caps[id]
				if cap.Available != tc.available {
					t.Fatalf("%s : disponible = %v, attendu %v", id, cap.Available, tc.available)
				}
				if cap.Reason != tc.reason {
					t.Fatalf("%s : motif = %q, attendu %q", id, cap.Reason, tc.reason)
				}
				if !tc.available && cap.Quality != QualityMissing {
					t.Fatalf("%s : qualité = %q", id, cap.Quality)
				}
				if len(cap.Requires) == 0 {
					t.Fatalf("%s : Requires doit nommer le prérequis", id)
				}
			}
		})
	}
}

// TestBatterySocIsNeverAvailableInPhase2 : la détection de présence ne prouve
// pas qu'une mesure d'état de charge existe côté API.
func TestBatterySocIsNeverAvailableInPhase2(t *testing.T) {
	cases := []struct {
		state  DetectionState
		reason string
	}{
		{StatePresent, ReasonBatteryReadout},
		{StateAbsent, ReasonNoBattery},
		{StateUnknown, "Présence d'une batterie non déterminée : cause batterie"},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(StateUnknown, tc.state, StateUnknown, StateUnknown)))
			cap := caps["battery.soc"]
			if cap.Available {
				t.Fatal("battery.soc ne doit jamais être disponible en phase 2")
			}
			if cap.Reason != tc.reason {
				t.Fatalf("motif = %q, attendu %q", cap.Reason, tc.reason)
			}
			if cap.Quality != QualityMissing {
				t.Fatalf("qualité = %q", cap.Quality)
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
			name: "présents", state: StatePresent, available: true,
		},
		{
			name: "absents", state: StateAbsent, available: false,
			identity: "Identité de l'onduleur non déterminée : cause identité",
			events:   "Événements de l'appareil non déterminés : cause événements",
		},
		{
			name: "non déterminés", state: StateUnknown, available: false,
			identity: "Identité de l'onduleur non déterminée : cause identité",
			events:   "Événements de l'appareil non déterminés : cause événements",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caps := byID(t, Capabilities(detectionWith(StateUnknown, StateUnknown, tc.state, tc.state)))
			if got := caps["device.identity"].Available; got != tc.available {
				t.Fatalf("device.identity disponible = %v", got)
			}
			if got := caps["device.events"].Available; got != tc.available {
				t.Fatalf("device.events disponible = %v", got)
			}
			if caps["device.identity"].Reason != tc.identity {
				t.Fatalf("motif identité = %q, attendu %q", caps["device.identity"].Reason, tc.identity)
			}
			if caps["device.events"].Reason != tc.events {
				t.Fatalf("motif événements = %q, attendu %q", caps["device.events"].Reason, tc.events)
			}
		})
	}
}

// TestCapabilitiesIndependence : une détection ne débloque pas une autre.
func TestCapabilitiesIndependence(t *testing.T) {
	onlyMeter := byID(t, Capabilities(detectionWith(StatePresent, StateUnknown, StateUnknown, StateUnknown)))
	if !onlyMeter["grid.import_export"].Available {
		t.Fatal("grid.import_export doit être disponible avec un Smart Meter détecté")
	}
	if onlyMeter["battery.soc"].Available {
		t.Fatal("battery.soc ne doit pas dépendre du Smart Meter")
	}
	if onlyMeter["device.identity"].Available {
		t.Fatal("device.identity ne doit pas dépendre du Smart Meter")
	}

	onlyBattery := byID(t, Capabilities(detectionWith(StateUnknown, StatePresent, StateUnknown, StateUnknown)))
	if onlyBattery["grid.import_export"].Available {
		t.Fatal("grid.import_export ne doit pas dépendre de la batterie")
	}

	onlyIdentity := byID(t, Capabilities(detectionWith(StateUnknown, StateUnknown, StatePresent, StateUnknown)))
	if onlyIdentity["device.events"].Available {
		t.Fatal("device.events ne doit pas dépendre de l'identité")
	}
}

// TestCapabilitiesEveryUnavailableHasReason : invariant porté jusqu'à l'API.
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
							t.Fatalf("capacité %q indisponible sans motif (meter=%s battery=%s identity=%s events=%s)",
								cap.ID, meter, battery, identity, events)
						}
						if cap.Quality != QualityMissing {
							t.Fatalf("capacité %q indisponible avec qualité %q", cap.ID, cap.Quality)
						}
					}
				}
			}
		}
	}
}

// TestReasonsNeverEndWithSeparator : un motif qui se termine par « : » annonce
// une cause qu'il ne nomme pas.
func TestReasonsNeverEndWithSeparator(t *testing.T) {
	states := []DetectionState{StateUnknown, StateAbsent, StatePresent}

	// Détection dont les causes sont volontairement vides : le repli doit
	// produire un motif complet, jamais un séparateur orphelin.
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
								t.Fatalf("capacité %q : motif tronqué %q", cap.ID, cap.Reason)
							}
						}
					}
				}
			}
		}
	}
}

// TestCapabilitiesReturnsFreshSlice : l'appelant ne doit pas pouvoir corrompre
// l'état renvoyé par un appel suivant.
func TestCapabilitiesReturnsFreshSlice(t *testing.T) {
	first := Capabilities(allUnknown())
	first[0].Available = false
	first[3].Requires = append(first[3].Requires, "corrompu")

	second := Capabilities(allUnknown())
	if !second[0].Available {
		t.Fatal("le premier appel a modifié l'état d'un appel ultérieur")
	}
	if !reflect.DeepEqual(second[3].Requires, Capabilities(allUnknown())[3].Requires) {
		t.Fatal("les tranches Requires sont partagées entre appels")
	}
}

func byID(t *testing.T, caps []Capability) map[string]Capability {
	t.Helper()
	out := make(map[string]Capability, len(caps))
	for _, c := range caps {
		if _, dup := out[c.ID]; dup {
			t.Fatalf("capacité %q en double", c.ID)
		}
		if c.ID == "" || c.Label == "" {
			t.Fatalf("capacité incomplète : %+v", c)
		}
		if !c.ReadOnly {
			t.Fatalf("%s doit être en lecture seule", c.ID)
		}
		if len(c.Evidence) == 0 {
			t.Fatalf("%s : la provenance doit être renseignée", c.ID)
		}
		out[c.ID] = c
	}
	return out
}
