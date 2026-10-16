package solarapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CommonQuery selects the common inverter collection: the electrical detail
// (AC and DC voltage, current, power) and the device status.
//
// The query is a constant of the binary, like the path: the client cannot target
// an arbitrary URL. Its answer was observed on firmware 1.19.7-1 on 2026-10-16
// (cmd/fronius-probe --survey): 200, 1680 bytes, 36 fields — thirteen
// measurements as {"Unit": …, "Value": …} pairs directly under Body.Data, with
// DeviceStatus beside them, and no `CommonInverterData` wrapper (the official
// documentation shows one; the device does not).
const CommonQuery = "Scope=Device&DeviceId=1&DataCollection=CommonInverterData"

// Measurement is one value of the realtime-data family: a unit and a value.
//
// Value is a pointer because the device answers `null` for a measurement it
// does not have (an unconnected DC input, an energy counter this firmware does
// not report). A nil value is not a zero, and the model invariant of this
// project forbids the confusion.
type Measurement struct {
	Unit  string   `json:"Unit"`
	Value *float64 `json:"Value"`
}

// DeviceStatus is the inverter state carried by the realtime-data answers.
//
// It is the observed source of the state the flow endpoints never carry
// (docs/UNKNOWNS.md #13): `InverterState` is a word ("Running", "Sleeping"),
// `StatusCode` and `ErrorCode` are the device's own codes.
type DeviceStatus struct {
	InverterState string `json:"InverterState"`
	StatusCode    *int   `json:"StatusCode"`
	ErrorCode     *int   `json:"ErrorCode"`
}

// InverterRealtime is one realtime-data answer: the measurements, keyed by the
// device's own names, and the device status.
//
// The measurement map is deliberately generic: the set depends on the
// collection and on the installation, and the survey established the names by
// observation rather than by documentation. An unknown key is therefore kept
// and displayed, never dropped.
type InverterRealtime struct {
	Measurements map[string]Measurement
	Status       DeviceStatus
	Timestamp    time.Time
}

// InverterRealtime reads one collection of the realtime-data family.
//
// query is restricted to the collections this client knows: the caller cannot
// compose a query string, so the read-only surface stays the allowlist.
func (c *Client) InverterRealtime(ctx context.Context, query string) (InverterRealtime, error) {
	var out InverterRealtime

	if query != CumulationQuery && query != CommonQuery {
		return out, fmt.Errorf("%w: collection %q", errNotAllowedQuery, query)
	}

	body, err := c.get(ctx, InverterRealtimePath, query)
	if err != nil {
		return out, err
	}

	// The captured answer is flat: the measurements sit directly under
	// Body.Data, whatever the collection.
	var raw struct {
		Body *struct {
			Data map[string]json.RawMessage `json:"Data"`
		} `json:"Body"`
		Head Head `json:"Head"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return out, decodeError(InverterRealtimePath, err)
	}
	if raw.Body == nil {
		return out, decodeError(InverterRealtimePath, errors.New("Body absent from the response"))
	}

	// An empty Body.Data is not an error here: the three-phase collection
	// answers exactly that on a single-phase inverter, and "no measurement for
	// this collection" is a fact the caller can render.
	out.Measurements = make(map[string]Measurement, len(raw.Body.Data))
	for key, value := range raw.Body.Data {
		if key == "DeviceStatus" {
			var status DeviceStatus
			if err := json.Unmarshal(value, &status); err != nil {
				return out, decodeError(InverterRealtimePath, fmt.Errorf("DeviceStatus: %w", err))
			}
			out.Status = status
			continue
		}
		var m Measurement
		if err := json.Unmarshal(value, &m); err != nil {
			// A key that is not a measurement is tolerated: the collections are
			// device-defined, and dropping the whole answer over a key we do not
			// know would lose the measurements beside it.
			continue
		}
		out.Measurements[key] = m
	}

	if ts, err := time.Parse(time.RFC3339, raw.Head.Timestamp); err == nil {
		out.Timestamp = ts.UTC()
	}

	if err := statusError(InverterRealtimePath, raw.Head); err != nil {
		return out, err
	}
	return out, nil
}
