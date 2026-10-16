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
without authentication on firmware 1.19.7-1 was a [HYPOTHESIS] in phase 1; the surveys of 2026-10-16
**confirmed it**: every catalogued Solar API path answered `200` to the bare request
(`digest_required: false`), no credential needed (see "The Solar API-first route, implemented" below).

Acquisition must be strictly read-only (see ADR 0004).

## Decision

The read-only Solar API v1 is the single source of phase 1:
`GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`, without a parameter.

The internal API adapter (Digest sessions, `/status/*`, `/config/*`) is deferred to phase 2 and does
not exist in the phase 1 code. The JSON schema of `GetPowerFlowRealtimeData` remains a
[HYPOTHESIS] until actually captured by the probe.

## The Solar API-first route, implemented (phase 2)

The Solar API is no longer only the phase 1 flow endpoint. Three more requests are read, on the
detector's slow cycle, and **used**; all three answered `ok` on firmware `1.19.7-1` (survey of
2026-10-16) and, unlike the internal API, **none of them needs a credential**:

- `GET /solar_api/v1/GetInverterInfo.cgi` — the identity of every inverter the device knows
  (`UniqueID`, `CustomName`, …);
- `GET /solar_api/v1/GetActiveDeviceInfo.cgi` — the active-device inventory, grouped by class
  (frozen query `DeviceClass=System`, §4.7);
- `GET /solar_api/GetAPIVersion.cgi` — the API version, the base URL and the compatibility range
  (the one answer that is **not** enveloped).

**One direction only.** The internal API remains the precise source, and the Solar API **fills what
it left undetermined; it never overwrites** an answer the device already gave through the
authenticated path. Concretely: the identity falls back to `GetInverterInfo.cgi` only when the
record holds no serial number, and the Solar API does not report the model or the firmware, which
therefore stay empty rather than guessed; the meter and battery presence falls back to the
active-device inventory only when the internal read left the state `unknown` — a class reported with
at least one device is `present`, a class reported empty or not mentioned is `absent`.

This makes **identity and presence work without any credential**. The device events
(`/status/activeEvents`) and the two `/commands/*` read paths (`GetUTCTime`,
`GetServiceState`) still require one: they have no Solar API counterpart. The three answers are also
published as the device-detail families `inverter_info`, `active_devices` and `api_version`.

## Consequences

- In phase 1, `GET /api/v1/system` returned `device: null`: no inverter identity coming from the
  hardware.
- In phase 1, the capability `device.identity` was unavailable, with the reason "Internal API adapter
  not implemented (phase 2)" — the generic `ReasonUnavailable` fallback of that time. Since phase 2
  the adapter exists and the reason depends on the last read (`docs/UNKNOWNS.md` #3, `internal/detect`).
- Since the Solar API-first route is implemented, `device.identity` is available **without any
  credential** (reason naming `GetInverterInfo.cgi`), and the meter and battery presence is settled
  by `GetActiveDeviceInfo.cgi` when the authenticated path answered nothing. The model and the
  firmware are still not invented: they stay empty on this route (`docs/UNKNOWNS.md` #3).
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
- `internal/fronius/solarapi/dto.go` (assumed shape, [HYPOTHESIS]) and
  `internal/fronius/solarapi/info.go` (the three identity/inventory readers, the Solar API-first route)
- `internal/detect/solar.go` (the two fallbacks and their precedence)
- `docs/RUNBOOK.md` (what an operator gains without credentials)
- `cmd/fronius-probe`, `docs/PROBE-READONLY.md` (capture protocol)
- Phase 1 plan, steps 5, 11; second survey of 2026-10-16 (firmware `1.19.7-1`)
