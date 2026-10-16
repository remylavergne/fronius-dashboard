# ADR 0001 — SQLite vs PostgreSQL

## Status

Accepted (phase 1)

## Context

The application retains one normalized sample per collection cycle. The reference interval is
5,000 ms, aligned with the cadence of the official client (`/status/powerflow`); one sample every 5 s
represents ≈17,280 rows per day. The installation is single-machine: one Go collector, one local
database, a frontend served separately or by the same binary. No constraint of shared service, of
high availability or of concurrent multi-writer access is identified.

The technical baseline imposes a static binary without CGO (`CGO_ENABLED=0`), and operation must stay
simple: diagnose with `sqlite3`, back up by copying the file, restore without a dedicated tool.

## Decision

Use SQLite through the pure Go driver `modernc.org/sqlite` (the only external Go dependency of phase 1).
Opening configuration:

- a single write connection: `db.SetMaxOpenConns(1)`;
- WAL mode (`journal_mode(WAL)`);
- `busy_timeout` at 5,000 ms;
- `synchronous=NORMAL`;
- foreign keys enabled (`foreign_keys(1)`).

The schema and the API of the persistence package are described in `internal/storage` (step 7); the
migrations are applied by `internal/storage/migrations/0001_init.sql`.

## Consequences

- No concurrent multi-writer access: a single writer at a time, which is enough for a volume of
  one sample every 5 s; to be re-evaluated if write latency becomes measurable.
- No replication, no automatic failover, no network read/write separation.
- Aggregates are to be materialized in phase 3 (dedicated indexes and hour/day aggregate tables) rather
  than relying on a query planner or on materialized views that do not exist in SQLite.
- Backup and restore by consistent copy of the file (WAL mode); procedure documented in
  `docs/RUNBOOK.md`.
- The database file lives under `./data/` by default (`FRONIUS_DB_PATH`) and is excluded from version
  control.

## Rejected alternatives

- **PostgreSQL**: a server to administer, extra backup and supervision; disproportionate for a
  single-machine home installation.
- **DuckDB**: less mature Go ecosystem and a concurrent write model unsuited to a collector that
  inserts continuously.
- **Flat-file or CSV storage**: no range queries, no transaction, no index; forces reimplementing what
  SQLite provides.

## Sources

- `docs/DEVELOPMENT.md` (tooling and environment variables)
- `docs/RUNBOOK.md` (backup, restore, purge)
- `internal/storage/schema.go`, `internal/storage/migrations/0001_init.sql`
- `TODO.md` / phase 1 plan, step 7 (SQLite schema, WAL, idempotent upsert)
