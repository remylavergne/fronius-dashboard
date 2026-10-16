-- Migration 0003: hourly aggregates (phase 3, ADR 0006).
--
-- `agg_hour` materialises exactly what the two aggregated reads of the
-- API need, without loss:
--   * /api/v1/timeseries (step that is a multiple of one hour): average, minimum, maximum
--     of the PV power, number of samples and number of missing values.
--     The average is recomposed from `sum_pv_w` and `count_pv` (average of
--     averages weighted by the number of non-null values), the minimum and the
--     maximum are associative.
--   * /api/v1/energy/daily: maximum of `energy_day_wh` per UTC hour, then grouped
--     by local date in Go (the maximum of a union is the maximum of the
--     maximums).
--
-- `bucket_utc` is the start of the UTC hour, in the same text format as `ts_utc`:
-- the lexicographic order stays chronological and SQLite's date functions
-- read it directly. It is always aligned on the hour (see
-- the invariant documented in aggregate.go).
--
-- `min_pv_w` and `max_pv_w` stay NULLable: an hour without a power
-- measurement stays NULL, never 0.

CREATE TABLE agg_hour (
  bucket_utc TEXT PRIMARY KEY,
  count_all INTEGER NOT NULL,
  count_pv INTEGER NOT NULL,
  sum_pv_w REAL NOT NULL,
  min_pv_w REAL,
  max_pv_w REAL,
  max_energy_day_wh REAL
) WITHOUT ROWID;
