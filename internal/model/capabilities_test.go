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
	"system.collector",
}

func TestCapabilitiesOrderIsStable(t *testing.T) {
	caps := Capabilities(false, false, false)
	if len(caps) != len(wantOrder) {
		t.Fatalf("len = %d, attendu %d", len(caps), len(wantOrder))
	}
	for i, want := range wantOrder {
		if caps[i].ID != want {
			t.Fatalf("position %d = %q, attendu %q", i, caps[i].ID, want)
		}
	}
}

func TestCapabilitiesWithoutHardware(t *testing.T) {
	caps := byID(t, Capabilities(false, false, false))

	// Disponibles sans matériel supplémentaire.
	for _, id := range []string{"production.power", "production.energy_day", "production.energy_total", "system.collector"} {
		if !caps[id].Available {
			t.Fatalf("%s devrait être disponible", id)
		}
	}
	// Indisponibles faute de matériel ou d'adaptateur.
	wantReason := map[string]string{
		"grid.import_export":    ReasonNoSmartMeter,
		"load.consumption":      ReasonNoSmartMeter,
		"autoconsumption.ratio": ReasonNoSmartMeter,
		"battery.soc":           ReasonNoBattery,
		"device.identity":       ReasonNoInternalAPI,
	}
	for id, reason := range wantReason {
		cap := caps[id]
		if cap.Available {
			t.Fatalf("%s devrait être indisponible sans matériel", id)
		}
		if cap.Reason != reason {
			t.Fatalf("%s : Reason = %q, attendu %q", id, cap.Reason, reason)
		}
		if cap.Quality != QualityMissing {
			t.Fatalf("%s : Quality = %q, attendu %q", id, cap.Quality, QualityMissing)
		}
		if len(cap.Requires) == 0 {
			t.Fatalf("%s : Requires doit nommer le prérequis manquant", id)
		}
	}

	if !caps["system.collector"].Verified {
		t.Fatal("system.collector doit être vérifié (propriété de l'application)")
	}
	for _, id := range []string{"production.power", "production.energy_day", "production.energy_total"} {
		cap := caps[id]
		if cap.Verified {
			t.Fatalf("%s ne peut pas être vérifié avant capture matérielle", id)
		}
		if !strings.Contains(cap.Reason, "non validé") {
			t.Fatalf("%s : Reason = %q doit signaler le schéma non validé", id, cap.Reason)
		}
	}
}

func TestCapabilitiesWithHardware(t *testing.T) {
	caps := byID(t, Capabilities(true, true, true))
	for _, id := range wantOrder {
		cap := caps[id]
		if !cap.Available {
			t.Fatalf("%s devrait être disponible quand le matériel l'est (reason=%q)", id, cap.Reason)
		}
		if cap.Quality == QualityMissing {
			t.Fatalf("%s : une capacité disponible ne peut pas être de qualité missing", id)
		}
	}

	// Le compteur seul ne débloque pas la batterie, et réciproquement.
	onlyMeter := byID(t, Capabilities(true, false, false))
	if onlyMeter["battery.soc"].Available {
		t.Fatal("battery.soc ne doit pas dépendre du Smart Meter")
	}
	if !onlyMeter["grid.import_export"].Available {
		t.Fatal("grid.import_export doit être disponible avec un Smart Meter")
	}
	if onlyMeter["device.identity"].Available {
		t.Fatal("device.identity ne doit pas dépendre du Smart Meter")
	}
}

// TestCapabilitiesEveryUnavailableHasReason : invariant porté jusqu'à l'API.
func TestCapabilitiesEveryUnavailableHasReason(t *testing.T) {
	for _, hasMeter := range []bool{false, true} {
		for _, hasBattery := range []bool{false, true} {
			for _, hasAPI := range []bool{false, true} {
				for _, cap := range Capabilities(hasMeter, hasBattery, hasAPI) {
					if cap.Available {
						continue
					}
					if cap.Reason == "" {
						t.Fatalf("capacité %q indisponible sans motif (meter=%v battery=%v api=%v)",
							cap.ID, hasMeter, hasBattery, hasAPI)
					}
					if cap.Quality != QualityMissing {
						t.Fatalf("capacité %q indisponible avec qualité %q", cap.ID, cap.Quality)
					}
				}
			}
		}
	}
}

// TestCapabilitiesReturnsFreshSlice : l'appelant ne doit pas pouvoir corrompre
// l'état renvoyé par un appel suivant.
func TestCapabilitiesReturnsFreshSlice(t *testing.T) {
	first := Capabilities(false, false, false)
	first[0].Available = false
	first[0].Requires = append(first[0].Requires, "corrompu")

	second := Capabilities(false, false, false)
	if !second[0].Available {
		t.Fatal("le premier appel a modifié l'état d'un appel ultérieur")
	}
	if !reflect.DeepEqual(second[0].Requires, Capabilities(false, false, false)[0].Requires) {
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
			t.Fatalf("%s doit être en lecture seule en phase 1", c.ID)
		}
		out[c.ID] = c
	}
	return out
}
