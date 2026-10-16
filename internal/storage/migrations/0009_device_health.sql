-- Migration 0009: the last reading of the device's own clock and of its
-- grid-limits self-test state (the two /commands/* read paths).
--
-- Structural choices:
--   * `device_health` is a single row (CHECK id = 1), the same discipline as
--     `device`: a reading REPLACES the previous one, it does not accumulate.
--     The point of this table is "what did the device last answer about
--     itself", not a series, so there is nothing to keep history for.
--   * `clock_device_utc` and `clock_drift_ms` are NULLABLE: a failed clock read
--     stores neither the device time nor the drift, so "unread" is stored as
--     NULL and never as a zero timestamp or a 0 ms drift. Decision 4 of ADR
--     0008, "unmeasured data is not computed": a fabricated timestamp would
--     silently pass for a reading. A genuine zero drift is stored as 0 and a
--     genuine device time as its text; the NULL is what distinguishes unread.
--   * the two readings are separated (clock_* / service_*), each with its own
--     state and its own cause (`*_reason`), written together: "unknown" without
--     a reason is not usable, and reconstructing the cause at read time would
--     be inventing it.
--   * `service_value` defaults to the empty string: it holds the state string
--     the device reported, and stays empty when nothing was read.

CREATE TABLE device_health (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  read_at_utc TEXT NOT NULL,
  clock_state TEXT NOT NULL DEFAULT 'unknown' CHECK (clock_state IN ('unknown', 'absent', 'present')),
  clock_reason TEXT NOT NULL DEFAULT '',
  clock_device_utc TEXT,
  clock_drift_ms INTEGER,
  service_state TEXT NOT NULL DEFAULT 'unknown' CHECK (service_state IN ('unknown', 'absent', 'present')),
  service_reason TEXT NOT NULL DEFAULT '',
  service_value TEXT NOT NULL DEFAULT ''
);
