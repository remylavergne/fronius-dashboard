-- Migration 0007: the recorded outcome of a read-only survey.
--
-- Structural choices:
--   * one row per (path, query): a survey answers "what does this firmware
--     answer today, on this path", not "what did it answer on every past
--     day". The probe timestamp and the firmware version are kept with it, so
--     the answer stays attributable; a fresh survey replaces the row.
--   * `class` carries the outcome in one word (`ok`, `empty`, `not_json`,
--     `auth_required`, `not_found`, `error`): "the path is absent" and "we
--     were not allowed in" are different facts and must not collapse into a
--     failed request.
--   * `fields_json` holds the SHAPE of the answer — JSON paths and types,
--     never a value. A survey of an identity endpoint must not become a
--     second copy of the device's serial numbers.
--   * `digest_required` records whether the request needed a Digest header,
--     which is how the interface can say "reachable, authentication needed"
--     without holding any credential.
--   * `firmware` is the device software version observed at survey time
--     (empty when the survey could not establish it): a shape is only true
--     for a firmware.

CREATE TABLE source_probe (
  path TEXT NOT NULL,
  query TEXT NOT NULL,
  class TEXT NOT NULL,
  http_status INTEGER NOT NULL,
  digest_required INTEGER NOT NULL,
  body_bytes INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  firmware TEXT NOT NULL DEFAULT '',
  probed_at_utc TEXT NOT NULL,
  fields_json TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (path, query)
) WITHOUT ROWID;
