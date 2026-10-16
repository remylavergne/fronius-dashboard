// Tests of the Solar API step of a detection.
//
// The Solar API is the fallback, never the authority: it fills what the internal
// API could not establish, and it must not overwrite an answer the device
// already gave through the authenticated path. These tests pin both halves of
// that rule, and the three families it publishes beside it.
package detect

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// fakeSolar is the Solar API reader a test drives.
type fakeSolar struct {
	inverters  []solarapi.InverterIdentity
	infoErr    error
	inventory  solarapi.DeviceInventory
	inventErr  error
	version    solarapi.APIVersion
	versionErr error

	classesAsked []string
	infoCalls    int
}

func (f *fakeSolar) InverterInfo(context.Context) ([]solarapi.InverterIdentity, error) {
	f.infoCalls++
	return f.inverters, f.infoErr
}

func (f *fakeSolar) ActiveDevices(_ context.Context, class string) (solarapi.DeviceInventory, error) {
	f.classesAsked = append(f.classesAsked, class)
	return f.inventory, f.inventErr
}

func (f *fakeSolar) APIVersion(context.Context) (solarapi.APIVersion, error) {
	return f.version, f.versionErr
}

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

// solarDetector builds a detector whose Solar API reader answers.
func solarDetector(t *testing.T, reader *fakeReader, solar *fakeSolar) (*Detector, *fakeSink) {
	t.Helper()
	sink := newFakeSink()
	det := newDetector(t, reader, sink)
	det.Solar = solar
	return det, sink
}

func TestSolarIdentityFillsWhatTheInternalReadCouldNot(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = unauthorized(internalapi.VersionPath)
	solar := &fakeSolar{
		inverters: []solarapi.InverterIdentity{{
			Index: "1", UniqueID: str("2909100097509000"), CustomName: str("tr-3-pn-01"),
			StatusCode: num(13), InverterState: str("Sleeping"),
		}},
		inventory: solarapi.DeviceInventory{Classes: map[string][]solarapi.DeviceEntry{}},
	}
	det, sink := solarDetector(t, reader, solar)

	detection, err := det.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	// The identity is present, and it says where it came from: the record keeps
	// the serial and the name the Solar API gave, and nothing it did not.
	records, _, _ := sink.snapshot()
	if len(records) != 1 {
		t.Fatalf("device records = %d", len(records))
	}
	record := records[0]
	if record.SerialNumber != "2909100097509000" || record.DeviceName != "tr-3-pn-01" {
		t.Errorf("identity = %+v", record)
	}
	if record.Source != solarapi.InverterInfoPath {
		t.Errorf("source = %q, want the Solar API path that established it", record.Source)
	}
	// The Solar API does not report the model or the firmware: they stay empty
	// rather than being guessed from the custom name.
	if record.Model != "" || record.Firmware != "" {
		t.Errorf("model/firmware invented: %+v", record)
	}
	if detection.Identity.State != model.StatePresent {
		t.Errorf("identity state = %q, want present (an inverter answered)", detection.Identity.State)
	}
	// The RECORD is what the store holds and what the capabilities are computed
	// from: a fallback that only touched the in-memory detection would leave the
	// interface saying "identity not determined".
	if record.IdentityState != model.StatePresent {
		t.Errorf("recorded identity state = %q, want present", record.IdentityState)
	}
	if record.IdentityReason == "" {
		t.Error("the recorded identity carries no cause: the reader cannot tell which source answered")
	}
}

func TestSolarIdentityNeverOverwritesTheInternalAnswer(t *testing.T) {
	reader := newFakeReader()
	reader.version = internalapi.Version{SerialNumber: str("33319328"), DeviceName: str("Primo GEN24")}
	solar := &fakeSolar{
		inverters: []solarapi.InverterIdentity{{
			Index: "1", UniqueID: str("a-different-serial"), CustomName: str("from the solar api"),
		}},
		inventory: solarapi.DeviceInventory{Classes: map[string][]solarapi.DeviceEntry{}},
	}
	det, sink := solarDetector(t, reader, solar)

	if _, err := det.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	records, _, _ := sink.snapshot()
	record := records[0]
	if record.Source != "/status/version" {
		t.Errorf("source = %q, want the internal path that answered first", record.Source)
	}
	if record.SerialNumber != "33319328" {
		t.Errorf("serial = %q, want the internal answer kept", record.SerialNumber)
	}
}

func TestSolarPresenceSettlesWhatTheInternalReadLeftUnknown(t *testing.T) {
	reader := newFakeReader()
	// No credentials: the internal component paths are unknown (abandoned or
	// refused), which is exactly when the Solar API must speak.
	reader.compErr[internalapi.PowerMeterPath] = unauthorized(internalapi.PowerMeterPath)
	reader.compErr[internalapi.BatteryPath] = unauthorized(internalapi.BatteryPath)

	solar := &fakeSolar{
		inventory: solarapi.DeviceInventory{Classes: map[string][]solarapi.DeviceEntry{
			"Inverter": {{Index: "1", Serial: str("2909100097509000")}},
			"Meter":    {{Index: "0", Serial: str("16420055")}},
			"Storage":  {},
		}},
	}
	det, sink := solarDetector(t, reader, solar)

	detection, err := det.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	if detection.Meter.State != model.StatePresent {
		t.Errorf("meter = %+v, want present (the inventory lists one)", detection.Meter)
	}
	if detection.Meter.Source != solarapi.ActiveDeviceInfoPath {
		t.Errorf("meter source = %q", detection.Meter.Source)
	}
	// A class the device reports with nothing behind it is an established
	// absence, not a doubt.
	if detection.Battery.State != model.StateAbsent {
		t.Errorf("battery = %+v, want absent", detection.Battery)
	}
	// The class asked for is the one the catalogue freezes.
	if len(solar.classesAsked) != 1 || solar.classesAsked[0] != solarapi.SystemDeviceClass {
		t.Errorf("classes asked = %v", solar.classesAsked)
	}
	records, _, _ := sink.snapshot()
	record := records[0]
	if record.MeterSource != solarapi.ActiveDeviceInfoPath || record.MeterState != model.StatePresent {
		t.Errorf("record meter = %+v", record)
	}
	if record.BatteryState != model.StateAbsent {
		t.Errorf("record battery = %+v", record)
	}
}

func TestSolarPresenceLeavesAnEstablishedAnswerAlone(t *testing.T) {
	reader := newFakeReader()
	reader.components[internalapi.PowerMeterPath] = true
	reader.components[internalapi.BatteryPath] = false

	solar := &fakeSolar{
		inventory: solarapi.DeviceInventory{Classes: map[string][]solarapi.DeviceEntry{}},
	}
	det, sink := solarDetector(t, reader, solar)

	detection, err := det.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	// The internal API answered (present, absent): the Solar API's empty
	// inventory must not overwrite it.
	if detection.Meter.State != model.StatePresent || detection.Meter.Source != "/components/PowerMeter/readable" {
		t.Errorf("meter = %+v", detection.Meter)
	}
	if detection.Battery.State != model.StateAbsent || detection.Battery.Source != "/components/BatteryManagementSystem/readable" {
		t.Errorf("battery = %+v", detection.Battery)
	}
	_ = sink
}

func TestSolarFamiliesAreRecordedEvenWhenTheyFail(t *testing.T) {
	reader := newFakeReader()
	solar := &fakeSolar{
		infoErr:    errors.New("the device answered 500"),
		inventErr:  errors.New("connection refused"),
		versionErr: errors.New("timeout"),
	}
	det, sink := solarDetector(t, reader, solar)

	if _, err := det.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	byFamily := map[string]storage.Detail{}
	for _, detail := range sink.savedDetails() {
		byFamily[detail.Family] = detail
	}
	for _, family := range []string{model.FamilyInverterInfo, model.FamilyActiveDevices, model.FamilyAPIVersion} {
		detail, ok := byFamily[family]
		if !ok {
			t.Errorf("%s was not recorded at all", family)
			continue
		}
		if detail.Available || detail.Reason == "" {
			t.Errorf("%s: a failed read must record its cause: %+v", family, detail)
		}
	}
	// A failed Solar API read also leaves the internal answers untouched.
	if byFamily[model.FamilySystem].Available != true {
		t.Errorf("the internal families suffered from a Solar API failure: %+v", byFamily[model.FamilySystem])
	}
}

func TestSolarFamiliesCarryTheObservedValues(t *testing.T) {
	reader := newFakeReader()
	solar := &fakeSolar{
		inverters: []solarapi.InverterIdentity{{
			Index: "1", UniqueID: str("2909100097509000"), InverterState: str("Sleeping"),
			StatusCode: num(13), PVPower: num(500),
		}},
		inventory: solarapi.DeviceInventory{Classes: map[string][]solarapi.DeviceEntry{
			"Inverter": {{Index: "1", Serial: str("2909100097509000")}},
			"Storage":  {},
		}},
		version: solarapi.APIVersion{Version: num(1), BaseURL: str("/solar_api/v1/"), CompatibilityRange: str("1.5-9")},
	}
	det, sink := solarDetector(t, reader, solar)

	if _, err := det.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	entries := func(family string) []model.EntryDetail {
		for _, detail := range sink.savedDetails() {
			if detail.Family != family {
				continue
			}
			var payload model.EntriesDetail
			if err := json.Unmarshal(detail.Data, &payload); err != nil {
				t.Fatalf("%s: %v", family, err)
			}
			return payload.Entries
		}
		return nil
	}

	identity := map[string]any{}
	for _, e := range entries(model.FamilyInverterInfo) {
		identity[e.Path] = e.Value
	}
	if identity["1.UniqueID"] != "2909100097509000" || identity["1.InverterState"] != "Sleeping" {
		t.Errorf("identity entries = %v", identity)
	}
	if _, ok := identity["1.StatusCode"]; !ok {
		t.Errorf("the state code is missing: %v", identity)
	}

	devices := map[string]any{}
	for _, e := range entries(model.FamilyActiveDevices) {
		devices[e.Path] = e.Value
	}
	if devices["Inverter.1.Serial"] != "2909100097509000" {
		t.Errorf("inventory entries = %v", devices)
	}
	if devices["Storage"] != "no device online" {
		t.Errorf("a class reported empty must be published: %v", devices)
	}

	version := map[string]any{}
	for _, e := range entries(model.FamilyAPIVersion) {
		version[e.Path] = e.Value
	}
	if version["BaseURL"] != "/solar_api/v1/" || version["CompatibilityRange"] != "1.5-9" {
		t.Errorf("version entries = %v", version)
	}
}

func TestDetectorWithoutSolarReaderKeepsWorking(t *testing.T) {
	reader := newFakeReader()
	sink := newFakeSink()
	det := newDetector(t, reader, sink) // det.Solar stays nil

	if _, err := det.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	for _, detail := range sink.savedDetails() {
		if detail.Family == model.FamilyInverterInfo || detail.Family == model.FamilyActiveDevices {
			t.Errorf("%s was recorded without a Solar API reader", detail.Family)
		}
	}
}

// unauthorized is what the internal client returns when no credentials are
// configured: the path is then abandoned and its state stays unknown, which is
// exactly the gap the Solar API fills.
func unauthorized(endpoint string) error {
	return &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: endpoint}
}
