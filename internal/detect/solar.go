package detect

import (
	"context"
	"fmt"
	"sort"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// The Solar API step of a detection.
//
// The internal API is the precise source — it states identity and presence with
// a cause — but it is Digest-protected: without credentials it answers nothing
// at all. Three Solar API requests need no credential and answer the same
// questions, coarsely. They are read on the same slow cycle as the rest of the
// detection (ADR 0003/0008: the Solar API first), and they are used in one
// direction only: they FILL what the internal API could not establish. They
// never overwrite an answer the device already gave through the internal API.
type SolarReader interface {
	InverterInfo(ctx context.Context) ([]solarapi.InverterIdentity, error)
	ActiveDevices(ctx context.Context, class string) (solarapi.DeviceInventory, error)
	APIVersion(ctx context.Context) (solarapi.APIVersion, error)
}

// solarReason prefixes every cause the Solar API provides, so that a reader can
// tell which source answered without reading the code.
const solarReason = "read through the Solar API (no credential): "

// readSolar reads the three Solar API requests, records them as detail families
// and applies the fallbacks the internal API left open.
func (d *Detector) readSolar(ctx context.Context, now time.Time,
	detection *model.Detection, record *storage.DeviceRecord) []storage.Detail {

	details := make([]storage.Detail, 0, 3)

	// 1. Identity: GetInverterInfo lists every inverter, online or seen within
	//    the last 24 hours, with its serial and its custom name.
	inverters, err := d.Solar.InverterInfo(ctx)
	if err != nil {
		details = append(details, unavailable(model.FamilyInverterInfo, now, err))
	} else {
		details = append(details, marshalDetail(model.FamilyInverterInfo, now, identityEntries(inverters)))
		d.applyIdentityFallback(detection, record, inverters)
	}

	// 2. Presence: the active-device inventory groups the devices currently
	//    online by class.
	inventory, err := d.Solar.ActiveDevices(ctx, solarapi.SystemDeviceClass)
	if err != nil {
		details = append(details, unavailable(model.FamilyActiveDevices, now, err))
	} else {
		details = append(details, marshalDetail(model.FamilyActiveDevices, now, inventoryEntries(inventory)))
		d.applyPresenceFallback(detection, record, inventory)
	}

	// 3. The API version and the base URL: recorded for the record, not used to
	//    reconfigure anything (the client's paths are constants of the binary).
	if version, err := d.Solar.APIVersion(ctx); err != nil {
		details = append(details, unavailable(model.FamilyAPIVersion, now, err))
	} else {
		details = append(details, marshalDetail(model.FamilyAPIVersion, now, apiVersionEntries(version)))
	}

	return details
}

// applyIdentityFallback fills the identity the internal API could not establish.
//
// It only acts when the record holds no serial number: a device read through the
// authenticated path is never replaced by a coarser answer. The Solar API does
// not report the model or the firmware — those stay empty rather than guessed.
func (d *Detector) applyIdentityFallback(detection *model.Detection, record *storage.DeviceRecord, inverters []solarapi.InverterIdentity) {
	if record.SerialNumber != "" || len(inverters) == 0 {
		return
	}
	first := inverters[0]
	if first.UniqueID == nil || *first.UniqueID == "" {
		return
	}

	record.SerialNumber = *first.UniqueID
	record.Source = solarapi.InverterInfoPath
	if first.CustomName != nil {
		record.DeviceName = *first.CustomName
	}
	identity := model.ResourceDetection{
		State:  model.StatePresent,
		Source: solarapi.InverterInfoPath,
		Reason: solarReason + "the internal identity path did not answer; the model and the firmware remain unknown",
	}
	detection.Identity = identity
	// The record is what the store holds and what the capabilities are computed
	// from: a state updated only in memory would leave the interface saying
	// "identity not determined" while the identity is right there.
	record.IdentityState = identity.State
	record.IdentityReason = identity.Reason
}

// applyPresenceFallback settles a component the internal API left undetermined.
//
// The rule is the one GetActiveDeviceInfo's answer states: a class reported with
// at least one device is present, a class reported empty — or not reported at
// all — is absent. A class the answer does not mention cannot be distinguished
// from a class the device does not support; both mean "nothing to read here",
// and the reason says which source said so.
func (d *Detector) applyPresenceFallback(detection *model.Detection, record *storage.DeviceRecord, inventory solarapi.DeviceInventory) {
	if detection.Meter.State == model.StateUnknown {
		detection.Meter = solarPresence(inventory, "Meter")
		record.MeterState, record.MeterSource, record.MeterReason =
			detection.Meter.State, detection.Meter.Source, detection.Meter.Reason
	}
	if detection.Battery.State == model.StateUnknown {
		detection.Battery = solarPresence(inventory, "Storage")
		record.BatteryState, record.BatterySource, record.BatteryReason =
			detection.Battery.State, detection.Battery.Source, detection.Battery.Reason
	}
}

// solarPresence turns one class of the inventory into a detection.
func solarPresence(inventory solarapi.DeviceInventory, class string) model.ResourceDetection {
	out := model.ResourceDetection{Source: solarapi.ActiveDeviceInfoPath}
	if n := inventory.Count(class); n > 0 {
		out.State = model.StatePresent
		out.Reason = fmt.Sprintf("%s%d device(s) of class %s are online", solarReason, n, class)
		return out
	}
	out.State = model.StateAbsent
	out.Reason = fmt.Sprintf("%sthe active-device inventory reports no %s", solarReason, class)
	return out
}

// The three families are published in the flat `entries` shape the interface
// already renders for the EMRS pins: a path and a value, named the way the
// device names them.

func identityEntries(inverters []solarapi.InverterIdentity) model.EntriesDetail {
	entries := make([]model.EntryDetail, 0, 4*len(inverters))
	for _, inv := range inverters {
		addString := func(field string, value *string) {
			if value != nil && *value != "" {
				entries = append(entries, model.EntryDetail{Path: inv.Index + "." + field, Value: *value})
			}
		}
		addInt := func(field string, value *int) {
			if value != nil {
				entries = append(entries, model.EntryDetail{Path: inv.Index + "." + field, Value: *value})
			}
		}
		addString("UniqueID", inv.UniqueID)
		addString("CustomName", inv.CustomName)
		addString("InverterState", inv.InverterState)
		addInt("StatusCode", inv.StatusCode)
		addInt("ErrorCode", inv.ErrorCode)
		addInt("PVPower", inv.PVPower)
	}
	return model.EntriesDetail{Entries: entries}
}

func inventoryEntries(inventory solarapi.DeviceInventory) model.EntriesDetail {
	classes := make([]string, 0, len(inventory.Classes))
	for class := range inventory.Classes {
		classes = append(classes, class)
	}
	sort.Strings(classes)

	entries := make([]model.EntryDetail, 0, len(classes))
	for _, class := range classes {
		devices := inventory.Classes[class]
		if len(devices) == 0 {
			// A class the device reports with nothing behind it IS the
			// statement "supported, nothing online": publish it rather than
			// let it look unread.
			entries = append(entries, model.EntryDetail{Path: class, Value: "no device online"})
			continue
		}
		for _, device := range devices {
			path := class + "." + device.Index
			if device.Serial != nil && *device.Serial != "" {
				entries = append(entries, model.EntryDetail{Path: path + ".Serial", Value: *device.Serial})
			} else if device.DT != nil {
				entries = append(entries, model.EntryDetail{Path: path + ".DT", Value: *device.DT})
			} else {
				entries = append(entries, model.EntryDetail{Path: path, Value: "reported"})
			}
		}
	}
	return model.EntriesDetail{Entries: entries}
}

func apiVersionEntries(version solarapi.APIVersion) model.EntriesDetail {
	entries := make([]model.EntryDetail, 0, 3)
	if version.Version != nil {
		entries = append(entries, model.EntryDetail{Path: "APIVersion", Value: *version.Version})
	}
	if version.BaseURL != nil {
		entries = append(entries, model.EntryDetail{Path: "BaseURL", Value: *version.BaseURL})
	}
	if version.CompatibilityRange != nil {
		entries = append(entries, model.EntryDetail{Path: "CompatibilityRange", Value: *version.CompatibilityRange})
	}
	return model.EntriesDetail{Entries: entries}
}
