package model

import (
	"math"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
)

// pf retourne un pointeur vers v.
//
// La forme Go 1.26 `new(expr)` n'est pas disponible ici : le module cible
// go 1.24 (voir go.mod), donc l'aide locale reste nécessaire.
func pf(v float64) *float64 { return &v }

func pi(v int) *int { return &v }

func envelope(ts string, site solarapi.SiteData, inverters map[string]solarapi.InverterPF) solarapi.PowerFlowEnvelope {
	env := solarapi.PowerFlowEnvelope{
		Head: solarapi.Head{
			Timestamp:        ts,
			RequestArguments: map[string]any{},
			Status:           solarapi.HeadStatus{Code: 0},
		},
	}
	env.Body.Data.Site = site
	env.Body.Data.Inverters = inverters
	return env
}

func TestFromPowerFlowDeviceTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	site := solarapi.SiteData{
		Mode:                   "produce-only",
		P_PV:                   pf(1902.4),
		E_Day:                  pf(12345.6),
		E_Year:                 pf(4800000),
		E_Total:                pf(12000000),
		Meter_Location_Current: pi(0),
	}
	got := FromPowerFlow(envelope("2026-10-16T09:17:43+02:00", site, nil), now)

	// TS toujours en UTC : 09:17:43+02:00 == 07:17:43Z.
	want := time.Date(2026, 10, 16, 7, 17, 43, 0, time.UTC)
	if !got.TS.Equal(want) {
		t.Fatalf("TS = %v, attendu %v", got.TS, want)
	}
	if got.TS.Location() != time.UTC {
		t.Fatalf("TS doit être en UTC, reçu %v", got.TS.Location())
	}
	if got.TSSource != TSSourceDevice {
		t.Fatalf("TSSource = %q", got.TSSource)
	}
	if got.Quality != QualityMeasured {
		t.Fatalf("Quality = %q", got.Quality)
	}
	if got.Source != SourceSolarAPI {
		t.Fatalf("Source = %q", got.Source)
	}
	if got.Mode != "produce-only" {
		t.Fatalf("Mode = %q", got.Mode)
	}
	if got.PowerPVW == nil || *got.PowerPVW != 1902.4 {
		t.Fatalf("PowerPVW = %v", got.PowerPVW)
	}
	if got.EnergyDayWh == nil || *got.EnergyDayWh != 12345.6 {
		t.Fatalf("EnergyDayWh = %v", got.EnergyDayWh)
	}
}

func TestFromPowerFlowTimestampFallback(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	site := solarapi.SiteData{P_PV: pf(100)}

	cases := []struct {
		name string
		ts   string
	}{
		{"horodatage vide", ""},
		{"horodatage non RFC3339", "16/10/2026 09:17"},
		{"horodatage sans fuseau", "2026-10-16T09:17:43"},
		{"horodatage numérique", "1760000000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromPowerFlow(envelope(tc.ts, site, nil), now)
			if !got.TS.Equal(now.UTC()) {
				t.Fatalf("TS = %v, attendu %v (horloge du collecteur)", got.TS, now.UTC())
			}
			if got.TS.Location() != time.UTC {
				t.Fatalf("TS doit être en UTC, reçu %v", got.TS.Location())
			}
			if got.TSSource != TSSourceCollector {
				t.Fatalf("TSSource = %q, attendu %q", got.TSSource, TSSourceCollector)
			}
			if got.Quality != QualityEstimated {
				t.Fatalf("Quality = %q, attendu %q", got.Quality, QualityEstimated)
			}
		})
	}
}

// TestFromPowerFlowNeverInventsZero : un champ absent reste nil, tandis qu'un
// zéro explicitement envoyé par l'appareil reste 0.
func TestFromPowerFlowNeverInventsZero(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	site := solarapi.SiteData{
		Mode:   "produce-only",
		P_PV:   pf(0), // explicitement nul (nuit)
		P_Grid: pf(0), // explicitement nul (compteur présent, échange nul)
		// P_Load, P_Akku, rel_* absents → null dans le protocole
	}
	got := FromPowerFlow(envelope("2026-10-16T09:17:43+02:00", site, nil), now)

	if got.PowerPVW == nil || *got.PowerPVW != 0 {
		t.Fatalf("PowerPVW = %v, attendu 0 explicite", got.PowerPVW)
	}
	if got.PowerGridW == nil || *got.PowerGridW != 0 {
		t.Fatalf("PowerGridW = %v, attendu 0 explicite", got.PowerGridW)
	}
	for name, v := range map[string]*float64{
		"PowerLoadW":            got.PowerLoadW,
		"PowerBatteryW":         got.PowerBatteryW,
		"EnergyDayWh":           got.EnergyDayWh,
		"EnergyYearWh":          got.EnergyYearWh,
		"EnergyTotalWh":         got.EnergyTotalWh,
		"RelAutonomyPct":        got.RelAutonomyPct,
		"RelSelfConsumptionPct": got.RelSelfConsumptionPct,
	} {
		if v != nil {
			t.Fatalf("%s = %v, attendu nil (champ absent)", name, *v)
		}
	}
	if got.Quality != QualityMeasured {
		t.Fatalf("Quality = %q (une mesure nulle explicite reste mesurée)", got.Quality)
	}
}

// TestFromPowerFlowEmptyResponseIsMissing : l'appareil répond avec un
// horodatage mais aucune valeur ⇒ l'échantillon n'est pas « mesuré ».
func TestFromPowerFlowEmptyResponseIsMissing(t *testing.T) {
	now := time.Now().UTC()
	got := FromPowerFlow(envelope("2026-10-16T09:17:43+02:00", solarapi.SiteData{Mode: "produce-only"}, nil), now)

	if got.Quality != QualityMissing {
		t.Fatalf("Quality = %q, attendu %q", got.Quality, QualityMissing)
	}
	if got.TSSource != TSSourceDevice {
		t.Fatalf("TSSource = %q (l'horodatage reste celui de l'appareil)", got.TSSource)
	}
}

func TestFromPowerFlowInverters(t *testing.T) {
	now := time.Now().UTC()
	inverters := map[string]solarapi.InverterPF{
		"2": {DT: 1, P: pf(10), E_Day: pf(1)},
		"1": {DT: 1, P: pf(20), E_Day: pf(2), E_Year: pf(3), E_Total: pf(4)},
	}
	got := FromPowerFlow(envelope("2026-10-16T09:17:43+02:00", solarapi.SiteData{P_PV: pf(30)}, inverters), now)

	if len(got.Inverters) != 2 {
		t.Fatalf("len(Inverters) = %d", len(got.Inverters))
	}
	if got.Inverters[0].ID != "1" || got.Inverters[1].ID != "2" {
		t.Fatalf("ordre des onduleurs non déterministe : %+v", got.Inverters)
	}
	first := got.Inverters[0]
	if first.DT != 1 || first.PowerW == nil || *first.PowerW != 20 {
		t.Fatalf("onduleur 1 = %+v", first)
	}
	if first.EnergyDayWh == nil || *first.EnergyDayWh != 2 {
		t.Fatalf("E_Day onduleur 1 = %v", first.EnergyDayWh)
	}
}

func TestFromPowerFlowNoInverters(t *testing.T) {
	got := FromPowerFlow(envelope("2026-10-16T09:17:43+02:00", solarapi.SiteData{P_PV: pf(1)}, nil), time.Now())
	if got.Inverters != nil {
		t.Fatalf("Inverters = %+v, attendu nil", got.Inverters)
	}
}

func TestUnitConversions(t *testing.T) {
	if got := WhToKWh(12345.6); math.Abs(got-12.3456) > 1e-9 {
		t.Fatalf("WhToKWh = %v", got)
	}
	if got := WToKW(3680); got != 3.68 {
		t.Fatalf("WToKW = %v", got)
	}
	if got := WhToKWh(0); got != 0 {
		t.Fatalf("WhToKWh(0) = %v", got)
	}
	if got := WToKW(0); got != 0 {
		t.Fatalf("WToKW(0) = %v", got)
	}
}
