-- Migration 0001 : schéma initial de la phase 1.
--
-- Choix structurants :
--   * `samples` est WITHOUT ROWID avec la clé primaire (ts_utc, source) : c'est
--     cette clé qui rend l'ingestion idempotente et le rejeu du backfill sans
--     doublon.
--   * `ts_utc` est un texte UTC au format 2006-01-02T15:04:05.000Z : ordre
--     lexicographique = ordre chronologique, et les fonctions de date de SQLite
--     le lisent directement.
--   * les colonnes de mesure sont NULLables : l'absence d'une valeur (pas de
--     Smart Meter, pas de batterie) reste NULL, jamais 0.
--   * les unités sont les unités SI de base (W, Wh, %) ; aucune conversion
--     n'est appliquée à l'écriture.

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
