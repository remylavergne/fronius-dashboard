package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"fronius-supervisor/internal/model"
)

// DeviceHealth is the last reading of the device's own clock and of its
// grid-limits self-test state (the two /commands/* read paths).
//
// A zero ClockDeviceUTC means "the clock was not read": it is stored as NULL
// and read back as the zero time, never as a fabricated timestamp. The drift is
// stored as NULL alongside it, so an unread clock never comes back as a 0 ms
// drift that would pass for a measurement; a valid device time with a genuine
// drift of 0 ms is stored as 0 and stays distinguishable. Decision 4 of ADR
// 0008, "unmeasured data is not computed".
type DeviceHealth struct {
	ReadAtUTC      time.Time
	ClockState     model.DetectionState
	ClockReason    string
	ClockDeviceUTC time.Time
	ClockDriftMS   int64
	ServiceState   model.DetectionState
	ServiceReason  string
	ServiceValue   string
}

// SaveDeviceHealth records the last health reading. The write is idempotent:
// the single row (id = 1) is replaced, never duplicated.
func (s *Store) SaveDeviceHealth(ctx context.Context, h DeviceHealth) error {
	// "Unread" is keyed on the absence of the device time: when it is zero,
	// neither the device time nor the drift is written, so the row cannot
	// carry a 0 that would pass for a measured drift.
	var deviceUTC, drift any
	if !h.ClockDeviceUTC.IsZero() {
		deviceUTC = formatTS(h.ClockDeviceUTC)
		drift = h.ClockDriftMS
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device_health (
			id, read_at_utc,
			clock_state, clock_reason, clock_device_utc, clock_drift_ms,
			service_state, service_reason, service_value)
		 VALUES (1,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
			read_at_utc = excluded.read_at_utc,
			clock_state = excluded.clock_state,
			clock_reason = excluded.clock_reason,
			clock_device_utc = excluded.clock_device_utc,
			clock_drift_ms = excluded.clock_drift_ms,
			service_state = excluded.service_state,
			service_reason = excluded.service_reason,
			service_value = excluded.service_value`,
		formatTS(h.ReadAtUTC),
		string(h.ClockState), h.ClockReason, deviceUTC, drift,
		string(h.ServiceState), h.ServiceReason, h.ServiceValue)
	if err != nil {
		return fmt.Errorf("writing device health: %w", err)
	}
	return nil
}

// DeviceHealth returns the last health reading. The second return is false if
// nothing was ever read: the caller must not invent a reading from a zero
// value.
func (s *Store) DeviceHealth(ctx context.Context) (DeviceHealth, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT read_at_utc,
		        clock_state, clock_reason, clock_device_utc, clock_drift_ms,
		        service_state, service_reason, service_value
		 FROM device_health WHERE id = 1`)

	var (
		readAt      string
		clockState  string
		clockReason string
		deviceUTC   sql.NullString
		driftMS     sql.NullInt64
		svcState    string
		svcReason   string
		svcValue    string
	)
	if err := row.Scan(&readAt,
		&clockState, &clockReason, &deviceUTC, &driftMS,
		&svcState, &svcReason, &svcValue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeviceHealth{}, false, nil
		}
		return DeviceHealth{}, false, fmt.Errorf("reading device health: %w", err)
	}

	parsed, err := parseTS(readAt)
	if err != nil {
		return DeviceHealth{}, false, err
	}

	h := DeviceHealth{
		ReadAtUTC:     parsed,
		ClockState:    model.DetectionState(clockState),
		ClockReason:   clockReason,
		ServiceState:  model.DetectionState(svcState),
		ServiceReason: svcReason,
		ServiceValue:  svcValue,
	}
	// NULL device time and NULL drift both come back as the zero value: an
	// unread clock is not a clock that read the epoch.
	if deviceUTC.Valid {
		h.ClockDeviceUTC, err = parseTS(deviceUTC.String)
		if err != nil {
			return DeviceHealth{}, false, err
		}
	}
	if driftMS.Valid {
		h.ClockDriftMS = driftMS.Int64
	}
	return h, true, nil
}
