-- Migration 0008: the diagnostic detail of the device.
--
-- Structural choices:
--   * one row per FAMILY (`inverter`, `system`, `network`, `devices`, `emrs`),
--     because a family is the unit that succeeds or fails together: the
--     detector reads a path, and either it answered or it did not.
--   * `available` and `reason` are stored per family, not derived at read time:
--     a family that could not be read must be distinguishable from one that was
--     never read at all (absent row), and the cause belongs with the read that
--     produced it. Reconstructing a reason later would be inventing it.
--   * `data_json` holds what the device answered, as it answered it. It is a
--     snapshot, not a measurement series: the point of this table is "what does
--     the device say about itself", which is why it is replaced on each read
--     rather than accumulated.
--   * `read_at_utc` is the instant of the read, so the interface can say how
--     recent the answer is — these values are read on a slow cycle, not live.

CREATE TABLE device_detail (
  family TEXT PRIMARY KEY,
  read_at_utc TEXT NOT NULL,
  available INTEGER NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  data_json TEXT NOT NULL DEFAULT '{}'
) WITHOUT ROWID;
