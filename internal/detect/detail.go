package detect

import (
	"context"
	"encoding/json"
	"time"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// readDetail reads the diagnostic families and records them.
//
// A family that fails is recorded as unavailable with its cause, and the other
// three are still read: the paths are independent, and a refused path must not
// hide the rest. Nothing is recorded as an empty value — an unavailable family
// carries its reason, and a family never read is simply absent.
func (d *Detector) readDetail(ctx context.Context, now time.Time) []storage.Detail {
	details := make([]storage.Detail, 0, 5)

	if v, err := d.Source.System(ctx); err != nil {
		details = append(details, unavailable(model.FamilySystem, now, err))
	} else {
		details = append(details, marshalDetail(model.FamilySystem, now, model.SystemDetail{
			SystemName:     v.SystemName,
			ServiceState:   v.ServiceState,
			UpdateRunning:  v.UpdateRunning,
			SupportEnabled: v.SupportEnabled,
			ConnectedViaAP: v.ConnectedViaAP,
			Accounts:       model.SystemAccount{Customer: v.AccountsCustomer, Technician: v.AccountsTechnician},
			Interfaces:     systemInterfaces(v.Interfaces),
		}))
	}

	if v, err := d.Source.Network(ctx); err != nil {
		details = append(details, unavailable(model.FamilyNetwork, now, err))
	} else {
		details = append(details, marshalDetail(model.FamilyNetwork, now, model.NetworkDetail{
			FDMPConnected: v.FDMPConnected,
			Interfaces:    networkInterfaces(v.Interfaces),
		}))
	}

	if v, err := d.Source.Devices(ctx); err != nil {
		details = append(details, unavailable(model.FamilyDevices, now, err))
	} else {
		entries := make([]model.DeviceDetailEntry, 0, len(v))
		for _, e := range v {
			entries = append(entries, model.DeviceDetailEntry{
				Type: e.Type, Status: e.Status, StatusMessage: e.StatusMessage,
			})
		}
		details = append(details, marshalDetail(model.FamilyDevices, now, model.DevicesDetail{Devices: entries}))
	}

	if v, err := d.Source.Cache(ctx); err != nil {
		details = append(details, unavailable(model.FamilyCache, now, err))
	} else {
		nodes := make([]model.CacheNodeDetail, 0, len(v))
		for _, node := range v {
			attributes := make([]model.CacheAttribute, 0, len(node.Attributes))
			for _, a := range node.Attributes {
				attributes = append(attributes, model.CacheAttribute{Key: a.Key, Value: a.Value})
			}
			channels := make([]model.CacheChannel, 0, len(node.Channels))
			for _, ch := range node.Channels {
				channels = append(channels, model.CacheChannel{
					Key: ch.Key, Name: ch.Name, Kind: ch.Kind, Type: ch.Type, Value: ch.Value,
				})
			}
			nodes = append(nodes, model.CacheNodeDetail{ID: node.ID, Attributes: attributes, Channels: channels})
		}
		details = append(details, marshalDetail(model.FamilyCache, now, model.CacheDetail{Nodes: nodes}))
	}

	if v, err := d.Source.Emrs(ctx); err != nil {
		details = append(details, unavailable(model.FamilyEmrs, now, err))
	} else {
		// The pins are published as a flat path/value list: their shape is
		// device-defined, and inventing a schema for keys nobody observed would
		// be exactly the guess this project refuses.
		entries := make([]model.EntryDetail, 0, 2*len(v))
		for _, pin := range v {
			if pin.State != nil {
				entries = append(entries, model.EntryDetail{Path: pin.Name + ".state", Value: *pin.State})
			}
			if pin.Reason != nil {
				entries = append(entries, model.EntryDetail{Path: pin.Name + ".reason", Value: *pin.Reason})
			}
		}
		details = append(details, marshalDetail(model.FamilyEmrs, now, model.EntriesDetail{Entries: entries}))
	}

	return details
}

// unavailable describes a family the device did not answer.
func unavailable(family string, now time.Time, err error) storage.Detail {
	return storage.Detail{
		Family:    family,
		ReadAtUTC: now,
		Available: false,
		Reason:    truncate(err.Error(), maxReasonLength),
		Data:      json.RawMessage("{}"),
	}
}

// marshalDetail records an answered family. A serialisation failure is a
// programming error, not a device one: it leaves the family unavailable with a
// named cause rather than storing a half payload.
func marshalDetail(family string, now time.Time, payload any) storage.Detail {
	body, err := json.Marshal(payload)
	if err != nil {
		return storage.Detail{
			Family: family, ReadAtUTC: now, Available: false,
			Reason: "the snapshot could not be serialised", Data: json.RawMessage("{}"),
		}
	}
	return storage.Detail{Family: family, ReadAtUTC: now, Available: true, Data: body}
}

// systemInterfaces maps the /status/common interface list.
func systemInterfaces(in []internalapi.SystemInterface) []model.SystemInterface {
	out := make([]model.SystemInterface, 0, len(in))
	for _, i := range in {
		out = append(out, model.SystemInterface{Name: i.Name, Type: i.Type})
	}
	return out
}

// networkInterfaces maps the /status/network interface list.
func networkInterfaces(in []internalapi.NetworkInterface) []model.NetworkInterface {
	out := make([]model.NetworkInterface, 0, len(in))
	for _, i := range in {
		out = append(out, model.NetworkInterface{
			Name:          i.Name,
			IP:            i.IP,
			Mask:          i.Mask,
			Gateway:       i.Gateway,
			DNS:           i.DNS,
			Configured:    i.Configured,
			InterfaceMode: i.InterfaceMode,
			Status:        i.Status,
			SSID:          i.SSID,
			WPSState:      i.WPSState,
		})
	}
	return out
}
