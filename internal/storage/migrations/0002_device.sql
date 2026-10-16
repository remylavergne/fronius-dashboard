-- Migration 0002: device identity, presence detection and
-- anti-duplication of hardware events.
--
-- Structural choices:
--   * `device` is a single row (CHECK id = 1): a device's identity
--     REPLACES the previous one, it does not accumulate. An identity history
--     would make no sense for single-inverter supervision.
--   * detection states are stored as text with three values
--     (unknown/absent/present) and not as booleans: "unknown" is not
--     "absent". A boolean would erase this distinction, and the interface
--     would display "no Smart Meter detected" after a simple 401 — a
--     fabricated absence, exactly what the model invariant forbids.
--   * each state carries its CAUSE (`*_reason`), written at the same time as it:
--     "unknown" without a reason is not usable, and reconstructing the
--     cause at read time would be inventing it.
--   * the identity text columns default to the empty string, which means
--     "not found in the response". It is converted to null at the API
--     boundary: never to 0, never to "unknown".
--   * `device_events_seen` is the anti-duplication memory: an event key
--     already seen does not produce a second row in `events`. The index on
--     seen_at_utc lets the purge remove the marks at the same time as the
--     events they deduplicate.

CREATE TABLE device (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  read_at_utc TEXT NOT NULL,
  source TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  firmware TEXT NOT NULL DEFAULT '',
  serial_number TEXT NOT NULL DEFAULT '',
  identity_state TEXT NOT NULL DEFAULT 'unknown' CHECK (identity_state IN ('unknown', 'absent', 'present')),
  identity_reason TEXT NOT NULL DEFAULT '',
  meter_state TEXT NOT NULL DEFAULT 'unknown' CHECK (meter_state IN ('unknown', 'absent', 'present')),
  meter_source TEXT NOT NULL DEFAULT '',
  meter_reason TEXT NOT NULL DEFAULT '',
  battery_state TEXT NOT NULL DEFAULT 'unknown' CHECK (battery_state IN ('unknown', 'absent', 'present')),
  battery_source TEXT NOT NULL DEFAULT '',
  battery_reason TEXT NOT NULL DEFAULT '',
  device_events_state TEXT NOT NULL DEFAULT 'unknown' CHECK (device_events_state IN ('unknown', 'absent', 'present')),
  device_events_reason TEXT NOT NULL DEFAULT ''
);

CREATE TABLE device_events_seen (
  key TEXT PRIMARY KEY,
  seen_at_utc TEXT NOT NULL
);

CREATE INDEX device_events_seen_ts ON device_events_seen(seen_at_utc);
