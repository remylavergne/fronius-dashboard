package model

import (
	"reflect"
	"testing"

	"fronius-supervisor/internal/fronius/solarapi"
)

// TestFromInverterRealtimeCarriesState: the three state fields land in State,
// including an explicit 0 code (which must not be confused with an absent one).
func TestFromInverterRealtimeCarriesState(t *testing.T) {
	rt := solarapi.InverterRealtime{
		Status: solarapi.DeviceStatus{
			InverterState: "Running",
			StatusCode:    new(7),
			ErrorCode:     new(0),
		},
	}
	got := FromInverterRealtime(rt)

	if got.State.InverterState != "Running" {
		t.Fatalf("InverterState = %q", got.State.InverterState)
	}
	if got.State.StatusCode == nil || *got.State.StatusCode != 7 {
		t.Fatalf("StatusCode = %v", got.State.StatusCode)
	}
	if got.State.ErrorCode == nil || *got.State.ErrorCode != 0 {
		t.Fatalf("ErrorCode = %v (an explicit 0 must survive)", got.State.ErrorCode)
	}
	if got.Measurements == nil {
		t.Fatal("Measurements must be non-nil (a device-defined set, possibly empty)")
	}
}

// TestFromInverterRealtimeKeepsDeviceKeysUnitsAndNulls: every measurement keeps
// the device's own key, unit and value. A null value stays nil (never 0) and a
// key nobody documented still reaches the interface.
func TestFromInverterRealtimeKeepsDeviceKeysUnitsAndNulls(t *testing.T) {
	rt := solarapi.InverterRealtime{
		Measurements: map[string]solarapi.Measurement{
			"PAC":           {Unit: "W", Value: new(float64(1902.4))},
			"UDC_2":         {Unit: "V", Value: nil},
			"FUTURE_SENSOR": {Unit: "kPa", Value: new(float64(101.3))},
		},
	}
	got := FromInverterRealtime(rt)

	if len(got.Measurements) != 3 {
		t.Fatalf("len(Measurements) = %d, want 3 (a key nobody documented must survive)", len(got.Measurements))
	}
	pac, ok := got.Measurements["PAC"]
	if !ok || pac.Unit != "W" || pac.Value == nil || *pac.Value != 1902.4 {
		t.Fatalf("PAC = %+v", pac)
	}
	udc, ok := got.Measurements["UDC_2"]
	if !ok || udc.Unit != "V" || udc.Value != nil {
		t.Fatalf("UDC_2 = %+v (a null value must stay nil, not 0)", udc)
	}
	future, ok := got.Measurements["FUTURE_SENSOR"]
	if !ok || future.Unit != "kPa" || future.Value == nil || *future.Value != 101.3 {
		t.Fatalf("FUTURE_SENSOR = %+v (an unknown key must reach the interface verbatim)", future)
	}
}

// TestFromInverterRealtimeEnergiesMirrorTheCounters: the three counters are read
// from the measurements, so a null counter stays nil while the measurement
// itself remains published.
func TestFromInverterRealtimeEnergiesMirrorTheCounters(t *testing.T) {
	rt := solarapi.InverterRealtime{
		Measurements: map[string]solarapi.Measurement{
			"DAY_ENERGY":   {Unit: "Wh", Value: nil}, // this firmware answers null
			"YEAR_ENERGY":  {Unit: "Wh", Value: new(float64(4800000))},
			"TOTAL_ENERGY": {Unit: "Wh", Value: new(float64(12000000))},
		},
	}
	got := FromInverterRealtime(rt)

	if got.Energies.DayWh != nil {
		t.Fatalf("DayWh = %v, want nil (the device answered null)", *got.Energies.DayWh)
	}
	if got.Energies.YearWh == nil || *got.Energies.YearWh != 4800000 {
		t.Fatalf("YearWh = %v", got.Energies.YearWh)
	}
	if got.Energies.TotalWh == nil || *got.Energies.TotalWh != 12000000 {
		t.Fatalf("TotalWh = %v", got.Energies.TotalWh)
	}
	// The counter stays visible as a measurement too: the interface reads both.
	if m, ok := got.Measurements["DAY_ENERGY"]; !ok || m.Value != nil {
		t.Fatalf("DAY_ENERGY measurement = %+v", m)
	}
}

// TestFromInverterRealtimeAbsentCountersStayNil: when the collection does not
// carry a counter at all, the energy field stays nil rather than becoming 0.
func TestFromInverterRealtimeAbsentCountersStayNil(t *testing.T) {
	got := FromInverterRealtime(solarapi.InverterRealtime{
		Measurements: map[string]solarapi.Measurement{
			"PAC": {Unit: "W", Value: new(float64(1))},
		},
	})
	if got.Energies.DayWh != nil || got.Energies.YearWh != nil || got.Energies.TotalWh != nil {
		t.Fatalf("Energies = %+v, want all nil when the keys are absent", got.Energies)
	}
}

// TestFromInverterRealtimeMeasuredKeysDeterministicOrder: the documented keys
// come first in display order, the rest are appended alphabetically, and a
// second read (a fresh map iteration) must not reshuffle them.
func TestFromInverterRealtimeMeasuredKeysDeterministicOrder(t *testing.T) {
	rt := solarapi.InverterRealtime{
		Measurements: map[string]solarapi.Measurement{
			"PAC":        {Unit: "W", Value: new(float64(1))},
			"UAC":        {Unit: "V", Value: new(float64(2))},
			"DAY_ENERGY": {Unit: "Wh", Value: new(float64(3))},
			"ZZZ_CUSTOM": {Unit: "x", Value: new(float64(4))},
			"AAA_CUSTOM": {Unit: "x", Value: new(float64(5))},
		},
	}
	got := FromInverterRealtime(rt)

	want := []string{"PAC", "UAC", "DAY_ENERGY", "AAA_CUSTOM", "ZZZ_CUSTOM"}
	first := got.MeasuredKeys()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("MeasuredKeys() = %v, want %v", first, want)
	}
	for range 5 {
		if again := got.MeasuredKeys(); !reflect.DeepEqual(again, first) {
			t.Fatalf("MeasuredKeys() is not deterministic: %v, then %v", first, again)
		}
	}
}
