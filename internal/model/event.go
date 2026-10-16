package model

import "time"

// Severity classifies a local collector event.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Event kinds: some produced locally by the collector, the others republished
// from the device.
const (
	// EventCollectorError: collection failure (device unreachable, invalid
	// response, application error reported by the device).
	EventCollectorError = "collector_error"
	// EventCollectorRecovered: recovery after a series of failures.
	EventCollectorRecovered = "collector_recovered"
	// EventStartup: collector startup.
	EventStartup = "collector_started"
	// EventPurge: retention purge executed.
	EventPurge = "retention_purge"
	// EventDevice: event reported by the device itself (/status/activeEvents).
	// Its deduplication relies on device_events_seen (internal/storage): the
	// stream is republished in full on every poll.
	EventDevice = "device_event"
	// EventClockDrift: the device's own clock drifted beyond the tolerance of
	// the daily derivation (/commands/GetUTCTime). Its severity is a warning:
	// the local day boundary of the daily energy is computed from that clock.
	EventClockDrift = "clock_drift"
	// EventClockDriftRecovered: the drift came back within tolerance.
	EventClockDriftRecovered = "clock_drift_recovered"
)

// Event is a local event, kept in the database and exposed by
// /api/v1/events. Hardware events (EventDevice) come from
// /status/activeEvents; the others are produced locally by the collector.
type Event struct {
	TS       time.Time
	Kind     string
	Severity Severity
	Message  string
	Detail   string
}
