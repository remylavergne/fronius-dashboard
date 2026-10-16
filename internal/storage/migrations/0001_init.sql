-- Migration 0001: initial phase-1 schema.
--
-- Structural choices:
--   * `samples` is WITHOUT ROWID with primary key (ts_utc, source): it is this
--     key that makes ingestion idempotent and backfill replay duplicate-free.
--   * `ts_utc` is a UTC text in the format 2006-01-02T15:04:05.000Z: lexicographic
--     order = chronological order, and SQLite's date functions read it
--     directly.
--   * the measurement columns are NULLable: the absence of a value (no
--     Smart Meter, no battery) stays NULL, never 0.
--   * the units are the base SI units (W, Wh, %); no conversion is
--     applied at write time.

CREATE TABLE samples (
  ts_utc TEXT NOT NULL,
  source TEXT NOT NULL,
  ts_source TEXT NOT NULL,
  quality TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT '',
  power_pv_w REAL,
  energy_day_wh REAL,
  energy_year_wh REAL,
  energy_total_wh REAL,
  power_grid_w REAL,
  power_load_w REAL,
  power_battery_w REAL,
  rel_autonomy_pct REAL,
  rel_self_consumption_pct REAL,
  inverters_json TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (ts_utc, source)
) WITHOUT ROWID;

CREATE INDEX samples_source_ts ON samples(source, ts_utc);

CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  ts_utc TEXT NOT NULL,
  kind TEXT NOT NULL,
  severity TEXT NOT NULL,
  message TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);

CREATE INDEX events_ts ON events(ts_utc DESC);

CREATE TABLE collector_runs (
  id INTEGER PRIMARY KEY,
  started_utc TEXT NOT NULL,
  duration_ms INTEGER NOT NULL,
  ok INTEGER NOT NULL,
  http_status INTEGER,
  error_kind TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE collector_state (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
