# Fronius GEN24 — handover folder for an AI agent

**Goal:** replace the manufacturer's Angular consultation interface with a local **Go + React/TypeScript + shadcn/ui** application, with dashboard, charts, history and, later, explicitly authorized controls.

## Reading order

1. `01-CONTEXT-AND-STATE.md` — hardware, observed facts, unknowns.
2. `02-REVERSE-ENGINEERING.md` — Webpack modules and mechanisms.
3. `03-API-INVENTORY.md` — endpoints found in the bundle (static inventory, not proof of accessibility).
4. `04-TARGET-ARCHITECTURE.md` — Go/React technical proposal.
5. `05-PRODUCT-UI-SPEC.md` — screens, visualizations, UX.
6. `06-SECURITY-AND-TESTS.md` — mandatory rules.
7. `TODO.md` — exhaustive and ordered backlog.
8. `AGENTS.md` — instructions directly usable by a development agent.

## Local sources

- `Pasted text(20261016-090329).txt`: Webpack JavaScript bundle provided by the user (about 184,000 lines); **not copied** into this folder.
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

Landmarks: `docs/ARCHITECTURE.md` (invariants), `docs/RUNBOOK.md` (operations),
`docs/SOURCES.md` (provenance of each claim), `docs/UNKNOWNS.md`
(unknowns and provisional decisions).
