# ADR 0006 — Retention and aggregation

## Status

Accepted (phase 1) — **revised in phase 3** (see "Phase 3 revision" at the end of the document)

## Context

Raw measurements accumulate at the rate of one sample every 5 s, that is ≈17,280 rows per day
and ≈6.3 million rows per year. Historization must feed charts over 24 h, 7 d, 30 d or
a custom range, as well as a daily energy.

Two structuring constraints: missing data must remain visible (no invented
interpolation), and the calculation of daily energy must withstand local-time
transitions (Europe/Brussels).

## Decision

- Raw measurements are kept as they are in the `samples` table, primary key
  `(ts_utc, source)` (idempotent upsert).
- Retention is parameterized by `FRONIUS_RETENTION_DAYS`, with `0` = unlimited retention (default).
- Aggregation is done at read time in phase 1:
  - time series by buckets aligned on the epoch (`1m`, `5m`, `15m`, `1h`, `1d`, `auto` bounded to
    1500 buckets); an empty bucket is not emitted;
  - daily energy by `MAX(E_Day)` per local day (Europe/Brussels calendar, hence 23 h or 25 h
    on daylight-saving change days). Hourly maxima being invariant to offset, the calculation per UTC
    hour followed by grouping by local date remains correct during transitions.
- The purge (`PurgeBefore`) is shipped with the database, but no scheduled purge is enabled by
  default in phase 1.

## Consequences

- **Phase 3 (2026-10-16)**: hourly aggregates are materialized (`agg_hour`) and the purge is
  scheduled. Exit criterion verified: on 6,307,200 measurements (one year at 5 s), the series at
  step 1 h responds in 14 ms versus 5,168 ms via the raw path, and daily energy in 5 ms versus
  5,535 ms — beyond the 200 ms target. Details and measurements in "Phase 3 revision".
- Retention at `0` avoids any data loss by default.
- The API exposes per bucket `PowerPVMinW`, `PowerPVMaxW`, `Count` and `Missing`; on the chart side,
  `connectNulls={false}` preserves the gaps.
- The SQLite implementation has no materialized views: aggregates take the form
  of a dedicated table (`agg_hour`), a clustered primary index and a watermark (see
  "Phase 3 revision").

## Rejected alternatives

- **Aggregates materialized from phase 1**: migration and catch-up complexity before having real
  data.
- **Purge enabled by default**: risk of destroying the only available data before validation.
- **Interpolation of empty buckets**: would invent unmeasured production.
- **Storage as a compressed continuous series**: unreadable by `sqlite3`, contrary to the requirement
  of simple diagnosis.

## Phase 3 revision (2026-10-16)

### What was put in place

A single aggregate table, hourly: `agg_hour` (migration `0003_agg.sql`).

| Column | Role |
|---|---|
| `bucket_utc` | start of the UTC hour, primary key `WITHOUT ROWID` (sequential access by range) |
| `count_all`, `count_pv` | bucket count and number of non-null power values |
| `sum_pv_w`, `min_pv_w`, `max_pv_w` | sum of non-null powers, minimum, maximum |
| `max_energy_day_wh` | maximum of `E_Day` within the hour |

**Why these columns and not an average.** An average does not recompose: the average of a
union is not the average of the averages. By keeping `sum_pv_w` and `count_pv`, the average of a
step that is a multiple of one hour is recomputed exactly as a weighted average by the counts, and
the minimum as well as the maximum are associative. `missing` is deduced from `count_all - count_pv`,
so it is not stored.

**Why no daily table.** The ADR planned "hour/day" aggregates. Grouping by local date is done in Go,
from the hourly rows (8,760 rows for one year, measured at 5 ms): there is therefore nothing to gain
from maintaining a second table that would have to be fed, purged and tested in parallel. The gap is
assumed; it remains reversible without a change of read schema.

### Bounds and splitting

- **Watermark**: `collector_state.agg_last_hour_utc`, always aligned on the UTC hour. Any
  hour earlier than it has a row in `agg_hour` if and only if it contains
  measurements. An unreadable value is treated as absent (full recomputation rather than a false
  read) and rounded down to the lower whole hour.
- **The open hour is never aggregated**: an hour still being fed cannot be
  materialized without the risk that a read believes it complete. A read of a long range therefore
  always walks, at most, the current hour in `samples` (≈720 rows, 1 ms more).
- **An hour is served from the aggregates only if it is whole and contained in
  `[from, to)`.** This is the easiest rule to break: an aggregate covers the whole hour,
  whereas a raw read sees only the requested slice. Serving a partial hour from a
  complete aggregate would invent measurements (average, minimum, maximum and count covering more
  rows than requested). Hence the three segments of a read: a partial head on `samples`,
  closed whole hours on `agg_hour`, an open tail on `samples`.
- **Multi-hour steps only**: only steps that are multiples of one hour (1 h, 1 d) use the aggregates.
  The 1 m, 5 m and 15 m steps walk the raw measurements, unchanged since phase 1.

### Invalidation on write

A measurement written below the watermark lowers it (`lowerWatermark`), so the next pass picks it
up. Without this, a history imported afterwards (backfill launched after a first pass) or a device
clock running behind would remain forever absent from the aggregates, while the read would believe
them complete. The comparison `MIN(ts_utc) < watermark` would have been too coarse: it does not
distinguish "already aggregated" from "imported afterwards" and would have recomputed the whole
history on every pass.

### Purge

- Scheduling moves into `internal/maintenance`: a purge at startup (behavior
  retained from phase 1) then once per local day from 03:00, with no new
  environment variable. Retention remains `FRONIUS_RETENTION_DAYS`, `0` = unlimited
  retention, in which case no purge is scheduled.
- The purge boundary is **local midnight rounded up to the whole hour**. The rounding is
  structural: without it, the hour straddling the boundary would lose its aggregate while keeping
  part of its measurements, and those measurements would become invisible to aggregated reads. For a
  time zone offset by a whole number of hours, including Europe/Brussels, the rounding is neutral.
- The purge deletes the aggregates of the same range as the measurements and raises the watermark to
  the purge boundary: the aggregates are never a second source of truth, they remain
  exactly derivable from the retained measurements.

### Measurements (Apple M-series, macOS 27, `make test-perf`)

| Request | Before (raw measurements) | After (aggregates) |
|---|---|---|
| `/timeseries` step 1 h, one year (8,760 buckets) | 5,168 ms | **14 ms** |
| `/timeseries` step 1 h, one year, open hour included | — | 15 ms |
| `/timeseries` step 1 d, one year (365 buckets) | — | 7 ms |
| `/energy/daily`, one year (366 days) | 5,535 ms | **5 ms** |
| Aggregation of the first complete year (once) | — | 8,736 ms |

The 6,307,200 measurements are inserted in 22.7 s by the test generator. The number of buckets and
the days are identical in both paths: equality is verified by literal comparison of the reductions
(`TestTimeSeriesIdenticalWithAndWithoutAggregates`,
`TestDailyEnergyIdenticalWithAndWithoutAggregates`) and, at the API level, by byte-for-byte
comparison of the payload before and after aggregation.

## Sources

- `internal/storage/aggregate.go` (aggregates, watermark, splitting), `internal/storage/store.go`
- `internal/storage/aggregate_test.go` (raw/aggregate equivalence, catch-up, purge)
- `internal/storage/perf_test.go` (measurements above, `make test-perf`)
- `internal/maintenance` (purge and aggregation scheduling)
- `internal/storage/storage_test.go` (bucket bounds, 23 h / 25 h days)
- `internal/api` (`/timeseries`, `/energy/daily`, parameter `step`)
- `docs/RUNBOOK.md` (purge, backup)
- Phase 1 plan, steps 7 and 9; phase 3 plan
