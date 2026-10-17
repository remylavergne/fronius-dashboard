# Fronius GEN24 — handover folder for an AI agent

**Goal:** replace the manufacturer's Angular consultation interface with a local **Go + React/TypeScript + shadcn/ui** application, with dashboard, charts, history and, later, explicitly authorized controls.

## At a glance

**The device's own console tells you what the inverter thinks now. This application
tells you what happened — in data you can back up, audit and export.**

| ![Overview screen: production, day and year energy, collector state, and the power curve](docs/screenshots/01-overview.png) | ![Energy screen: three rollups and the per-day table with its device-counter and derived markers](docs/screenshots/02-energy.png) |
|---|---|
| Live state, with freshness and provenance | Local history, with measured and derived values kept apart |

### Same installation, a different contract

| | Manufacturer's interface (the Angular app served *by* the inverter) | This application |
|---|---|---|
| Runs on | the inverter, as its web console | your machine, as a local service and interface |
| Needs the device | yes, to show anything | no: history, events and exports keep working while the inverter is offline |
| Access | role-gated screens — `customer` / `technician` / `support`, a **display** condition, not a server permission | no account; the device credentials never leave the backend and never reach the browser |
| Scope | the full console: configuration, commands, firmware, service | **read-only by construction** — a non-GET is refused before it leaves the process |
| History | the device's own reports | hourly aggregates, daily energy derived from the stored total, an event journal, CSV export |
| Numbers | values as the device reports them | `measured` / `derived` / `unavailable` **with a reason**, `unverified` paths marked, gaps never interpolated |
| Depends on | the manufacturer's bundle | nothing from it: the bundle is read as a source (`references/`), never loaded |

No capture of the manufacturer's interface is reproduced here: none exists in this
folder, and one is not invented. The comparison rests on the extracted facts in
[`docs/ENDPOINTS.md`](docs/ENDPOINTS.md) and [`docs/SOURCES.md`](docs/SOURCES.md),
and on the simulator captures in [`docs/screenshots/`](docs/screenshots/) — taken
against `cmd/fronius-sim`, with synthetic data.

## Reading order

1. [`docs/USER-GUIDE.md`](docs/USER-GUIDE.md) — what it does, screen by screen.
2. [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the invariants and the data flow.
3. [`docs/SOURCES.md`](docs/SOURCES.md) and [`docs/ENDPOINTS.md`](docs/ENDPOINTS.md) — provenance, and the endpoint matrix per path.
4. [`docs/UNKNOWNS.md`](docs/UNKNOWNS.md) — what remains open, and the provisional decisions.
5. [`docs/RUNBOOK.md`](docs/RUNBOOK.md) — operating it: checks, diagnosis, backup, retention.
6. [`TODO.md`](TODO.md) — the exhaustive backlog and the step log.
7. [`AGENTS.md`](AGENTS.md) — the rules a development agent must follow.

## Local sources

- `references/fronius-original.js`: the Webpack JavaScript bundle provided by the user (about 184,000 lines), kept verbatim as the primary source and **never modified**.
- `references/42,0410,2012.pdf`: the vendor's Solar API specification (`42,0410,2012,EN 02115052025`), cited throughout `docs/SOURCES.md`.
- `fronius-information.pdf`: system export of 16 October 2026; **not copied**.
- HTTP observations and user discussion: some values are reported and not reproducible from the JS alone.

**Confidence:** distinguish [SOURCE-JS], [SOURCE-PDF], [OBSERVED-CONVERSATION], [HYPOTHESIS], [TO-TEST]. Never turn a route found in the JS into a confirmed server permission.

## Delivered software (phases 1 to 3)

The folder above describes the analysis and the target. The corresponding software is
in this same repository:

| Path                | Content                                                                  |
| ------------------- | ------------------------------------------------------------------------ |
| `cmd/fronius-sim`   | Deterministic simulator of the Solar API (no hardware required)          |
| `cmd/froniusd`      | Service: collection, SQLite, REST API                                    |
| `cmd/fronius-probe` | Read-only probe to validate the real shape of the responses              |
| `internal/`         | Config, Solar API client, normalized model, persistence, collector, API  |
| `apps/web`          | React 19 + strict TypeScript + shadcn/ui + Recharts interface            |
| `docs/`             | Architecture, development, runbook, sources, unknowns, ADR               |

```bash
make dev-sim   # simulator on 127.0.0.1:8081
make dev-api   # service on 127.0.0.1:8080
make dev-web   # interface on 127.0.0.1:5173
```

Phase 1 is **strictly read-only**: no code path emits
`POST`, `PUT`, `PATCH` or `DELETE` to the inverter (structural guard in
`internal/fronius/readonly.go`). To query the real hardware, first read
`docs/PROBE-READONLY.md`.

Phase 2 adds authenticated reading of internal paths (device identity, presence of a
meter and a battery, active events): the credentials live in the environment of
`froniusd`, the `Authorization` header is only emitted in response to a 401, and an
undetermined resource stays "undetermined" — never "absent". Phase 3 materializes the
hourly aggregates and schedules the retention purge (`make test-perf` measures the
criterion of ADR 0006 over one year of data).

A task-oriented guide, with a screenshot of every screen, is in
[`docs/USER-GUIDE.md`](docs/USER-GUIDE.md).

Landmarks: `docs/ARCHITECTURE.md` (invariants), `docs/RUNBOOK.md` (operations),
`docs/SOURCES.md` (provenance of each claim), `docs/UNKNOWNS.md`
(unknowns and provisional decisions).
