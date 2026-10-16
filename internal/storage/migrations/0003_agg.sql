-- Migration 0003 : agrégats horaires (phase 3, ADR 0006).
--
-- `agg_hour` matérialise exactement ce dont les deux lectures agrégées de
-- l'API ont besoin, sans perte :
--   * /api/v1/timeseries (pas multiple d'une heure) : moyenne, minimum, maximum
--     de la puissance PV, nombre d'échantillons et nombre de valeurs absentes.
--     La moyenne se recompose depuis `sum_pv_w` et `count_pv` (moyenne des
--     moyennes pondérée par le nombre de valeurs non nulles), le minimum et le
--     maximum sont associatifs.
--   * /api/v1/energy/daily : maximum de `energy_day_wh` par heure UTC, regroupé
--     ensuite par date locale en Go (le maximum d'une union est le maximum des
--     maximums).
--
-- `bucket_utc` est le début d'heure UTC, au même format texte que `ts_utc` :
-- l'ordre lexicographique reste l'ordre chronologique et les fonctions de date
-- de SQLite le lisent directement. Il est toujours aligné sur l'heure (voir
-- l'invariant documenté dans aggregate.go).
--
-- `min_pv_w` et `max_pv_w` restent NULLables : une heure sans mesure de
-- puissance reste NULL, jamais 0.

CREATE TABLE agg_hour (
  bucket_utc TEXT PRIMARY KEY,
  count_all INTEGER NOT NULL,
  count_pv INTEGER NOT NULL,
  sum_pv_w REAL NOT NULL,
  min_pv_w REAL,
  max_pv_w REAL,
  max_energy_day_wh REAL
) WITHOUT ROWID;
