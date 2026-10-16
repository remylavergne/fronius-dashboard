package internalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// This file reads the diagnostic paths the survey of 2026-10-16 observed on
// firmware 1.19.7-1 (docs/SOURCES.md § First survey of the reference
// installation). Each answered `200` with a **bare** JSON body — no Body/Head
// envelope, unlike the /components/*/readable paths — which is why the
// extractors below read the decoded body directly.
//
// The fields modelled are exactly the ones the survey reported, key by key. A
// field the device does not answer stays nil: an absent statement is not a
// false one, and the interface renders it as such.

// SystemInterface is one network interface as /status/common describes it.
type SystemInterface struct {
	Name *string
	Type *string
}

// SystemInfo is the device-wide state of /status/common.
//
// `AccountsCustomer` and `AccountsTechnician` are the account kinds the device
// declares for its own interface. They are a statement of the device, not a
// permission of this application (docs/UNKNOWNS.md #6): the catalogue records
// them, the application keeps authorising itself with its own allowlist.
type SystemInfo struct {
	SystemName         *string
	ServiceState       *string
	UpdateRunning      *bool
	SupportEnabled     *bool
	ConnectedViaAP     *bool
	AccountsCustomer   *bool
	AccountsTechnician *bool
	Interfaces         []SystemInterface
}

// NetworkInterface is one interface of /status/network.
type NetworkInterface struct {
	Name          string
	IP            *string
	Mask          *string
	Gateway       *string
	DNS           []string
	Configured    *string
	InterfaceMode *string
	Status        *string
	SSID          *string
	WPSState      *string
}

// NetworkInfo is /status/network: one entry per interface, plus the FDMP flag.
type NetworkInfo struct {
	FDMPConnected *bool
	Interfaces    []NetworkInterface
}

// DeviceEntry is one element of the /status/devices array.
type DeviceEntry struct {
	Type          *string
	Status        *int
	StatusMessage *string
}

// EmrsPin is one pin of /status/emrs.
type EmrsPin struct {
	Name   string
	State  *bool
	Reason *string
}

// System reads /status/common.
func (c *Client) System(ctx context.Context) (SystemInfo, error) {
	var out SystemInfo
	fields, err := c.statusObject(ctx, CommonPath)
	if err != nil {
		return out, err
	}

	out.SystemName = stringField(fields, "systemName")
	out.ServiceState = stringField(fields, "serviceState")
	out.UpdateRunning = boolField(fields, "updateRunning")
	out.SupportEnabled = boolField(fields, "supportEnabled")

	if network, ok := nestedObject(fields, "networkState"); ok {
		out.ConnectedViaAP = boolField(network, "connectedViaAP")
		out.Interfaces = systemInterfaces(network["interfaces"])
	}
	if passwords, ok := nestedObject(fields, "passwordState"); ok {
		out.AccountsCustomer = boolField(passwords, "customer")
		out.AccountsTechnician = boolField(passwords, "technician")
	}
	return out, nil
}

// Network reads /status/network.
func (c *Client) Network(ctx context.Context) (NetworkInfo, error) {
	var out NetworkInfo
	fields, err := c.statusObject(ctx, NetworkPath)
	if err != nil {
		return out, err
	}

	out.FDMPConnected = boolField(fields, "isFDMPConnected")

	// The interface names are the device's own ("eth0", "eth1", …): they are
	// read, never assumed, and sorted so the display order is stable.
	names := make([]string, 0, len(fields))
	for name := range fields {
		if name == "isFDMPConnected" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry, ok := nestedObject(fields, name)
		if !ok {
			continue
		}
		iface := NetworkInterface{Name: name}
		if ip, ok := nestedObject(entry, "ip"); ok {
			iface.IP = stringField(ip, "ip")
			iface.Mask = stringField(ip, "mask")
			iface.Gateway = stringField(ip, "gateway")
			iface.Configured = stringField(ip, "configured")
			iface.DNS = stringList(ip["dns"])
		}
		if network, ok := nestedObject(entry, "network"); ok {
			iface.InterfaceMode = stringField(network, "interface_mode")
			iface.Status = stringField(network, "status")
			iface.SSID = stringField(network, "ssid")
			iface.WPSState = stringField(network, "wps_state")
		}
		out.Interfaces = append(out.Interfaces, iface)
	}
	return out, nil
}

// Devices reads /status/devices, whose body is a bare array.
func (c *Client) Devices(ctx context.Context) ([]DeviceEntry, error) {
	raw, err := c.statusArray(ctx, DevicesPath)
	if err != nil {
		return nil, err
	}

	out := make([]DeviceEntry, 0, len(raw))
	for _, entry := range raw {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil {
			// An entry we cannot read is reported as such rather than dropped:
			// the list is an inventory, and a hole in it would be a lie.
			out = append(out, DeviceEntry{Type: new("unreadable entry")})
			continue
		}
		out = append(out, DeviceEntry{
			Type:          stringField(fields, "type"),
			Status:        intField(fields, "status"),
			StatusMessage: stringField(fields, "statusMessage"),
		})
	}
	return out, nil
}

// Emrs reads /status/emrs: a set of pins, each with a state and its reason.
func (c *Client) Emrs(ctx context.Context) ([]EmrsPin, error) {
	fields, err := c.statusObject(ctx, EmrsPath)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]EmrsPin, 0, len(names))
	for _, name := range names {
		pin := EmrsPin{Name: name}
		if entry, ok := nestedObject(fields, name); ok {
			pin.State = boolField(entry, "state")
			pin.Reason = stringField(entry, "reason")
		}
		out = append(out, pin)
	}
	return out, nil
}

// statusObject reads an allowed path whose payload is a bare JSON object.
func (c *Client) statusObject(ctx context.Context, path string) (map[string]json.RawMessage, error) {
	ep, ok := Lookup(path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return nil, err
	}
	return decodeObject(ep.Path, body)
}

// statusArray reads an allowed path whose payload is a bare JSON array.
func (c *Client) statusArray(ctx context.Context, path string) ([]json.RawMessage, error) {
	ep, ok := Lookup(path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, jsonDecodeError(ep.Path, err)
	}
	return out, nil
}

// systemInterfaces reads the interface list of /status/common.
func systemInterfaces(raw json.RawMessage) []SystemInterface {
	if len(raw) == 0 {
		return nil
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]SystemInterface, 0, len(list))
	for _, entry := range list {
		out = append(out, SystemInterface{
			Name: stringField(entry, "name"),
			Type: stringField(entry, "type"),
		})
	}
	return out
}

// nestedObject extracts a sub-object.
func nestedObject(fields map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool) {
	raw, ok := lookupFold(fields, key)
	if !ok {
		return nil, false
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

// boolField reads a boolean, tolerating a string form ("true").
func boolField(fields map[string]json.RawMessage, keys ...string) *bool {
	var raw json.RawMessage
	var ok bool
	for _, key := range keys {
		if raw, ok = lookupFold(fields, key); ok {
			break
		}
	}
	if !ok {
		return nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return &b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "true":
			return new(true)
		case "false":
			return new(false)
		}
	}
	return nil
}

// stringList reads an array of strings.
func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
