package solarapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

// The three requests that describe the installation itself.
//
// All three are served by the reference firmware (survey of 2026-10-16 with the
// paths the specification gives) and, unlike the internal API, **none of them
// needs a credential**: they are the Solar API-first route of ADR 0003/0008 made
// usable.
const (
	// InverterInfoPath lists every inverter the device knows, online or not.
	InverterInfoPath = "/solar_api/v1/GetInverterInfo.cgi"
	// ActiveDeviceInfoPath lists the devices currently online, grouped by class.
	ActiveDeviceInfoPath = "/solar_api/v1/GetActiveDeviceInfo.cgi"
	// APIVersionPath answers the API version, the base URL and the compatibility
	// range of the device.
	APIVersionPath = "/solar_api/GetAPIVersion.cgi"

	// SystemDeviceClass asks GetActiveDeviceInfo for every class at once (§4.7.5).
	SystemDeviceClass = "System"
)

// deviceClasses are the classes this client may ask for: the set the
// specification lists, so a caller cannot compose a query of its own.
var deviceClasses = map[string]bool{
	"Inverter": true, "Storage": true, "Ohmpilot": true,
	"SensorCard": true, "StringControl": true, "Meter": true,
	SystemDeviceClass: true,
}

// InverterIdentity is what one inverter reports about itself.
//
// Every field is a pointer because the reference firmware omits some of them
// (the specification notes that `DT` "is not valid for Inverters on
// GEN24/Tauro/Verto"), and an absent statement is not a zero.
type InverterIdentity struct {
	Index         string
	DT            *int
	CustomName    *string
	UniqueID      *string
	PVPower       *int
	StatusCode    *int
	InverterState *string
	ErrorCode     *int
	Show          *int
}

// DeviceEntry is one device of a class in the active-device inventory.
type DeviceEntry struct {
	Index string
	DT    *int
	// Serial is the device's serial number, the identifier a support case needs.
	Serial *string
	// ChannelNames lists the channels of a SensorCard, empty for the other
	// classes (the specification documents it for that class only).
	ChannelNames []string
}

// DeviceInventory is the answer of GetActiveDeviceInfo: the classes the device
// reported, each with its devices. A class that is absent from the map was NOT
// reported: that is how "no meter is installed" reads, and a caller must not
// turn the absence into an empty list of its own.
type DeviceInventory struct {
	Classes map[string][]DeviceEntry
}

// Count returns how many devices a class reported.
func (d DeviceInventory) Count(class string) int {
	return len(d.Classes[class])
}

// APIVersion is the answer of GetAPIVersion: a bare object, not an envelope.
type APIVersion struct {
	Version            *int
	BaseURL            *string
	CompatibilityRange *string
}

// InverterInfo reads the identity of every inverter the device knows.
func (c *Client) InverterInfo(ctx context.Context) ([]InverterIdentity, error) {
	body, err := c.get(ctx, InverterInfoPath, "")
	if err != nil {
		return nil, err
	}

	var raw struct {
		Body *struct {
			Data map[string]json.RawMessage `json:"Data"`
		} `json:"Body"`
		Head Head `json:"Head"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, decodeError(InverterInfoPath, err)
	}
	if raw.Body == nil {
		return nil, decodeError(InverterInfoPath, errors.New("Body absent from the response"))
	}
	if err := statusError(InverterInfoPath, raw.Head); err != nil {
		return nil, err
	}

	indexes := sortedKeys(raw.Body.Data)
	out := make([]InverterIdentity, 0, len(indexes))
	for _, index := range indexes {
		var fields struct {
			DT            *int    `json:"DT"`
			CustomName    *string `json:"CustomName"`
			UniqueID      *string `json:"UniqueID"`
			PVPower       *int    `json:"PVPower"`
			StatusCode    *int    `json:"StatusCode"`
			InverterState *string `json:"InverterState"`
			ErrorCode     *int    `json:"ErrorCode"`
			Show          *int    `json:"Show"`
		}
		if err := json.Unmarshal(raw.Body.Data[index], &fields); err != nil {
			// An inverter we cannot read is reported as an unknown one rather
			// than dropped: a missing inverter would read as "not installed".
			out = append(out, InverterIdentity{Index: index})
			continue
		}
		out = append(out, InverterIdentity{
			Index:         index,
			DT:            fields.DT,
			CustomName:    fields.CustomName,
			UniqueID:      fields.UniqueID,
			PVPower:       fields.PVPower,
			StatusCode:    fields.StatusCode,
			InverterState: fields.InverterState,
			ErrorCode:     fields.ErrorCode,
			Show:          fields.Show,
		})
	}
	return out, nil
}

// ActiveDevices reads the devices currently online, grouped by class.
//
// class must be one of the classes the specification lists: the query is a
// constant of this package, and a caller cannot target an arbitrary one.
func (c *Client) ActiveDevices(ctx context.Context, class string) (DeviceInventory, error) {
	if !deviceClasses[class] {
		return DeviceInventory{}, fmt.Errorf("%w: device class %q", errNotAllowedQuery, class)
	}

	body, err := c.get(ctx, ActiveDeviceInfoPath, "DeviceClass="+class)
	if err != nil {
		return DeviceInventory{}, err
	}

	var raw struct {
		Body *struct {
			Data map[string]map[string]json.RawMessage `json:"Data"`
		} `json:"Body"`
		Head Head `json:"Head"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return DeviceInventory{}, decodeError(ActiveDeviceInfoPath, err)
	}
	if raw.Body == nil {
		return DeviceInventory{}, decodeError(ActiveDeviceInfoPath, errors.New("Body absent from the response"))
	}
	if err := statusError(ActiveDeviceInfoPath, raw.Head); err != nil {
		return DeviceInventory{}, err
	}

	out := DeviceInventory{Classes: make(map[string][]DeviceEntry, len(raw.Body.Data))}
	for name, devices := range raw.Body.Data {
		indexes := sortedKeys(devices)
		entries := make([]DeviceEntry, 0, len(indexes))
		for _, index := range indexes {
			var fields struct {
				DT           *int     `json:"DT"`
				Serial       *string  `json:"Serial"`
				ChannelNames []string `json:"ChannelNames"`
			}
			if err := json.Unmarshal(devices[index], &fields); err != nil {
				entries = append(entries, DeviceEntry{Index: index})
				continue
			}
			entries = append(entries, DeviceEntry{
				Index: index, DT: fields.DT, Serial: fields.Serial, ChannelNames: fields.ChannelNames,
			})
		}
		out.Classes[name] = entries
	}
	return out, nil
}

// APIVersion reads the version the device exposes, with its base URL.
//
// The answer is a bare object: this request is the one that is not enveloped
// (specification §2.4.1).
func (c *Client) APIVersion(ctx context.Context) (APIVersion, error) {
	var out APIVersion
	body, err := c.get(ctx, APIVersionPath, "")
	if err != nil {
		return out, err
	}
	var raw struct {
		APIVersion         *int    `json:"APIVersion"`
		BaseURL            *string `json:"BaseURL"`
		CompatibilityRange *string `json:"CompatibilityRange"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return out, decodeError(APIVersionPath, err)
	}
	out.Version, out.BaseURL, out.CompatibilityRange = raw.APIVersion, raw.BaseURL, raw.CompatibilityRange
	return out, nil
}

// get performs a bounded GET on one allowed path, with an optional frozen query.
//
// The three readers above share it: one place for the timeout, the size bound,
// the error classification and the debug line.
func (c *Client) get(ctx context.Context, path, query string) ([]byte, error) {
	endpoint := *c.base
	endpoint.Path = path
	endpoint.RawQuery = query

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: path, Err: err}
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, path, err)
	}
	c.log.Debug("solar api",
		"solar_api_path", path, "solar_api_query", query,
		"status", resp.StatusCode, "bytes", len(body),
		"duration_ms", time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return nil, fronius.ClassifyStatus(path, resp.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return nil, &froniuserr.Error{
			Kind: froniuserr.KindDecode, Endpoint: path,
			Err: fmt.Errorf("body of %d bytes: exceeds the limit of %d MiB", len(body), maxBodyBytes>>20),
		}
	}
	return body, nil
}

// statusError turns a non-zero Head.Status.Code into a typed error.
func statusError(path string, head Head) error {
	if head.Status.Code == 0 {
		return nil
	}
	return &froniuserr.Error{
		Kind:        froniuserr.KindStatus,
		Endpoint:    path,
		Code:        head.Status.Code,
		Reason:      head.Status.Reason,
		UserMessage: head.Status.UserMessage,
	}
}

func decodeError(path string, err error) error {
	return &froniuserr.Error{Kind: froniuserr.KindDecode, Endpoint: path, Err: err}
}

// sortedKeys returns the keys of a JSON object in a stable order: an index is a
// protocol string ("1", "55"), and a display must not shuffle between two reads.
func sortedKeys[T any](in map[string]T) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
