# Development

## Prerequisites

| Tool | Version | Note |
|---|---|---|
| Go | that of `go.mod` (1.26) | imposed by `modernc.org/sqlite` |
| Node | 26 | |
| pnpm | 10 | |
| SQLite | 3.x (CLI) | only to inspect the database by hand |

`CGO_ENABLED=0` must remain possible: this is the reason for the choice of the
`modernc.org/sqlite` driver (pure Go). No C dependency.

## Getting started

Three terminals, from the repository root:

```bash
make dev-sim     # Fronius simulator on 127.0.0.1:8081
make dev-api     # froniusd: API on 127.0.0.1:8080 + collector
make dev-web     # Vite interface on 127.0.0.1:5173
```

To exercise Digest authentication, start the simulator with an account and
`froniusd` with the matching credentials:

```bash
go run ./cmd/fronius-sim --api-user sim --api-password simpass --with-meter

# the simulator password is "simpass", read without leaving it in the
# shell history
export FRONIUS_DEVICE_USER=sim
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
go run ./cmd/froniusd
```

No example in this documentation writes a password after an `=`: CI refuses any
such line, and the shell history is a log like any other.

`make seed` fills the database with 7 days of simulator history
(`froniusd --backfill-days 7`). The simulator must be running, and the address must
be local: history filling refuses a non-local target without
`--force-backfill`.

Then <http://127.0.0.1:5173>. The Vite proxy relays `/api` to `froniusd`: the
frontend never has the inverter address.

## Commands

| Command | Effect |
|---|---|
| `make build` | static binaries in `bin/` + frontend build |
| `make test` | `go test ./... -race` then `vitest --run` |
| `make test-go` / `make test-web` | only one half |
| `make test-perf` | criterion of ADR 0006: one year of measurements (≈6.3 M rows) and < 200 ms per request; slow, on demand |
| `make lint` | `go vet ./...` then `oxlint` |
| `make fmt` | `go fmt ./...` |
| `make seed` | 7 days of simulated history |
| `make clean` | removes `bin/`, `data/`, `coverage/`, `apps/web/dist` |

## Configuration

**No configuration file in phase 1**: only environment variables, all documented
in `.env.example`. `config.Load()` refuses at startup any unreadable or inconsistent
value, and lists **all** faults at once.

Main variables:

| Variable | Default | Role |
|---|---|---|
| `FRONIUS_SOLARAPI_URL` | `http://127.0.0.1:8081` | source of measurements |
| `FRONIUS_HTTP_ADDR` | `127.0.0.1:8080` | REST API listen address |
| `FRONIUS_ALLOW_REMOTE` | `0` | lifts the non-local listen guard (ADR 0005) |
| `FRONIUS_DB_PATH` | `./data/fronius.db` | SQLite database |
| `FRONIUS_TZ` | `Europe/Brussels` | local days |
| `FRONIUS_POLL_INTERVAL` | `5s` | collection cadence (minimum 1 s) |
| `FRONIUS_POLL_JITTER` | `500ms` | jitter amplitude |
| `FRONIUS_HTTP_TIMEOUT` | `5s` | timeout per request |
| `FRONIUS_RETENTION_DAYS` | `0` | `0` = unlimited retention |
| `FRONIUS_STALE_AFTER` | `30s` | staleness threshold displayed |
| `FRONIUS_WEB_DIR` | *(empty)* | static assets served by `froniusd` |
| `FRONIUS_DEVICE_USER` | *(empty)* | user of the device's internal paths |
| `FRONIUS_DEVICE_PASSWORD` | *(empty)* | Digest password, of type `secret.String` (masked everywhere except the computation of `HA1`) |
| `FRONIUS_DETECT_INTERVAL` | `60s` | detection cadence (identity, meter, battery, events; minimum 5 s) |
| `FRONIUS_LOG_LEVEL` / `_FORMAT` | `info` / `text` | logging |

The two credentials go together: defining only one is refused at
startup. They are read by `froniusd` and `fronius-probe`, never by the
frontend nor by the REST API. The official client queries these same paths every
5 s; 60 s is enough here, since an identity and a presence rarely change.

The maintenance cadence (hourly aggregation every 5 min, daily purge
from 03:00) is **not** configurable: it depends on no operational need,
and exposing it would add a variable to validate, document and test for no gain.
Only the retention depth (`FRONIUS_RETENTION_DAYS`) is adjustable (ADR 0006).

## Tests

```bash
go test ./... -race
pnpm --dir apps/web test --run
```

Organization:

- `cmd/fronius-sim`: determinism (same parameters ⇒ same body), failure
  modes, `/sim/health`;
- `internal/config`: defaults, overrides, invalid values, listen guard;
- `internal/fronius{,/solarapi}`: refusal of non-GET methods; mapping of
  HTTP, JSON and timeout errors; contract fixtures;
- `internal/fronius/digest`: RFC 2617 vector, exact header shape, number
  of requests per case (fresh challenge, cached nonce, stale nonce, 401 without challenge,
  persistent 401), `nc` counter, concurrency;
- `internal/fronius/internalapi`: allowlist (refusal outside the list, zero request
  emitted), tolerant extraction of identity, `Cgi` envelope, unreadable body
  never confused with an absence;
- `internal/secret`: masking in `fmt`, `slog` and JSON;
- `internal/detect`: three detection states, event deduplication,
  abandoning a path after a 401 without credentials;
- `internal/model`: units, quality, capabilities, "no zero" invariant;
- `internal/storage`: idempotent migrations, upsert, buckets, daylight-saving
  change (23 h / 25 h), purge, and equivalence of reads before/after
  aggregation (bounds in the middle of an hour, open hour, empty hour, catch-up
  of an imported history); `make test-perf` carries the measurement over one year;
- `internal/maintenance`: retention boundary aligned on the hour, a single
  purge per local day, retention `0` without purge, errors that do not stop
  the loop, a real pass on a SQLite database;
- `internal/collector`: jitter, backoff, event deduplication,
  cancellation, no insertion on failure;
- `internal/api`: 404/405, parameter validation, empty states, capabilities
  derived from detection, identity nullability, hygiene (no route
  returns the password);
- `apps/web`: formats, chart mapping (gaps), API client (3 failure
  classes), freshness, components.

The tests open no connection to a device: the simulator and
`httptest` cover everything. A test must never call the real inverter.

## Conventions

- Branch `main`, Conventional Commits (`feat(scope):`, `fix(scope):`,
  `docs:`, `test(scope):`, `chore:`), one commit per deliverable step.
- Code identifiers in English; comments, documentation, interface and commit
  messages in English.
- `TODO.md` and `CHANGELOG.md` are kept up to date at each step; structuring
  decisions go through an ADR in `docs/adr/`.
- New information is documented **with its source and firmware version**
  (`docs/SOURCES.md`), and never presented as a firmware permission if it comes from
  the client.

## Pitfalls encountered (not to be reintroduced)

- **`connectNulls={false}` is not enough.** Recharts cuts the curve only if it
  receives `null` points: a bucket missing from the response makes its neighbors
  adjacent, and the curve is drawn across the gap. Hence the grid fill in
  `chart.ts`, driven by `meta.step_s`.
- **The step of a series cannot be deduced from the data.** The gap between two
  returned buckets is wrong as soon as a bucket is missing; the server therefore exposes the
  step actually applied.
- **A bucket is not a measurement.** `count` and `missing` describe
  **samples**; `power_pv_avg_w` describes the average power of the bucket.
- **Do not deduce the step from `time.Time.AddDate`** for local days:
  subtracting 24 h crosses the daylight-saving change. Use
  `time.Date(…, loc)`.
