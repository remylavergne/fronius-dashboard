# Target architecture — Go + React/shadcn

## Proposed decision (to validate via ADR)
**Monorepo**: `apps/web` (Vite, React, TypeScript, shadcn/ui, Tailwind, Recharts), `apps/api` (Go), `internal/fronius` (API client), `internal/storage`, `internal/http`, `docs/adr`, `test/fixtures`.

```text
Fronius GEN24 (LAN)
  ├─ Solar API (read)
  └─ Local Digest endpoints (controlled read)
           │
      Go collector / adapters
           │
   Normalization + data quality
           │
    SQLite (WAL) + aggregations
           │
  Go REST API + SSE (or WebSocket if justified)
           │
   React + TS + shadcn/ui + Recharts
```

## Go backend
- `solarapi` adapter separate from `internalapi`: do not couple the UI to the Fronius formats.
- HTTP client with context timeout, retry **only for idempotent reads**, handling of 401 / nonce `X-WWW-Authenticate`, Digest MD5 auth if necessary, no logging of secrets.
- Initial collection: power, energy, states and system information; record raw values (optional, bounded size) and normalized series.
- SQLite for single-inverter installation; versioned migrations, WAL, backups and retention/aggregation. Evaluate PostgreSQL only for multi-site or larger volume.
- Versioned Go API `/api/v1`: `/health`, `/system`, `/live`, `/timeseries?from=&to=&step=`, `/energy/daily`, `/events`, `/capabilities`, `/settings`; optional SSE `/stream`.
- `Capability` model: `available`, `requires_meter`, `requires_role`, `verified`, `read_only`, `reason`; never display an impossible metric as zero.
- Separate command service, **disabled by default**, allowlist and explicit confirmation; no gridcode / powerlimits / firmware write without a dedicated security review.
- Config via file/env, secrets via environment variables or secure store, restrictive file permissions. Loopback bind by default, authenticated proxy if remote access.
- Observability: redacted structured logs, collection metrics, last sample state, errors, latency, reconnection.

## React frontend
- React + strict TS, Vite, shadcn/ui + Tailwind; TanStack Query for cache/polling, React Router, Recharts for charts; Zod for response validation if useful.
- Responsive desktop/tablet/mobile design, light/dark modes, consistent units, configurable local Europe/Brussels time zone.
- The Fronius credentials stay **in the backend**; no `AuthHash`, no password in localStorage.
- `loading`, `stale`, `offline`, `not available`, `error` states; distinguish measured, estimated, unavailable values.
- i18n FR initially, architecture extensible EN/DE.

## Deployment
- Compiled Go, static frontend served by Go (embed) or reverse proxy; optional Docker Compose (NAS-compatible), healthcheck, persistent volumes.
- LAN-only by default; HTTPS via reverse proxy if necessary; do not expose the inverter to the Internet.
- Backup of SQLite, config and documentation; tested restore procedure.

## ADRs to write
1. SQLite vs PostgreSQL.
2. SSE vs polling for real time.
3. Solar API vs Modbus querying for each measurement.
4. Separation supervision / commands.
5. Local auth and remote access.
6. Data retention and aggregation policy.
7. Docker/NAS packaging vs standalone binary.
