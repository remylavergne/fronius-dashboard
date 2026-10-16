# ADR 0003 — Solar API vs internal API

## Status

Accepted (phase 1)

## Context

The application must retrieve the production measurements of a Fronius Primo GEN24 3.6 Plus. Two
families of interfaces are identified:

- the **Solar API v1**, a documented read interface (`GET /solar_api/v1/...`);
- the **internal API** used by the manufacturer's Angular interface (`/status/*`, `/config/*`),
  protected by a Digest MD5 with a non-standard challenge header `x-www-authenticate` and a single
  retry on 401 (module `xeXE` / `JWhm` of the bundle, see `docs/SOURCES.md`).

A project requirement forbids any dependency on the manufacturer's interface. Access to the Solar API
without authentication on firmware 1.19.7-1 is a [HYPOTHESIS] to be confirmed by the probe
(`fronius-probe`) — status **[TO-TEST]**.

Acquisition must be strictly read-only (see ADR 0004).

## Decision

The read-only Solar API v1 is the single source of phase 1:
`GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`, without a parameter.

The internal API adapter (Digest sessions, `/status/*`, `/config/*`) is deferred to phase 2 and does
not exist in the phase 1 code. The JSON schema of `GetPowerFlowRealtimeData` remains a
[HYPOTHESIS] until actually captured by the probe.

## Consequences

- In phase 1, `GET /api/v1/system` returned `device: null`: no inverter identity coming from the
  hardware.
- In phase 1, the capability `device.identity` was unavailable, with the reason "Internal API adapter
  not implemented (phase 2)" — the generic `ReasonUnavailable` fallback of that time. Since phase 2
  the adapter exists and the reason depends on the last read (`docs/UNKNOWNS.md` #3, `internal/detect`).
- If the probe reveals that the Solar API requires authentication, phase 2 (Digest adapter) is raised
  in priority; credentials then remain confined to the backend environment, never
  in the frontend (see ADR 0005).
- Phase 1 remains fully functional on the `fronius-sim` simulator without hardware.

## Rejected alternatives

- **Modbus TCP**: requires enabling a feature on the inverter, hence a configuration change, forbidden
  in phase 1.
- **Internal API from phase 1**: imposes secrets and opens the way to write endpoints;
  out of scope.
- **Reading the Angular bundle**: forbidden — no dependency on the manufacturer's interface.

## Sources

- `docs/SOURCES.md` (modules `xeXE`, `JWhm`, provenance, confidence level)
- `docs/UNKNOWNS.md` (Solar API schema, Solar API authentication, inverter identity)
- `03-API-INVENTORY.md` (endpoint inventory; the Solar API is absent from the bundle)
- `internal/fronius/solarapi/dto.go` (assumed shape, [HYPOTHESIS])
- `cmd/fronius-probe`, `docs/PROBE-READONLY.md` (capture protocol)
- Phase 1 plan, steps 5, 11
