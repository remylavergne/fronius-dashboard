# ADR 0008 — Exposed data scope: domain model, not an API mirror

## Status

Accepted (phase 2)

## Context

The bundle extraction (`docs/ENDPOINTS.md`, section "Method, arguments and DTO
per path (bundle extraction)") has replaced the unknowns with facts: 54 `READ` paths are
documented with their method, their arguments, their envelope and the fields that
the official client **actually reads** — 43 signals per inverter, 23 per Smart
Meter, 10 per battery, 11 event fields, whole configurations, plus
10 paths whose payload is never consumed.

The question that follows is mechanical: "since the device exposes all of this,
should everything be exposed to the interface?" It runs up against four already
stated constraints:

- `AGENTS.md`: never compute consumption, import/export or self-consumption
  without a validated measurement; add internal authentication only if **a metric
  justifies it**; mark any untested endpoint `unverified`; document any new
  information with its source and firmware version;
- ADR 0003: the Solar API first, the internal API in phase 2;
- ADR 0004: no command, no `MUTATION` or `DANGEROUS` endpoint;
- the definition of "done": errors and empty states handled, offline
  compatibility.

An API mirror (for example `GET /api/v1/raw/<Fronius path>`) would satisfy the
question in appearance, and would cost: the public contract would become that of a
third-party firmware, each screen would have to know an unvalidated shape, and the
slightest device change would become a breaking interface change.

## Decision

1. **The application's public contract is the domain model**, plus
   `/api/v1/capabilities` to state what is available, absent or unknown. The
   frontend knows **no** Fronius path and receives no raw device payload.
2. **The Solar API first.** An addition to the internal allowlist requires a
   metric that the Solar API does not provide, written in `docs/ENDPOINTS.md`.
   The route is now implemented, not a principle only (ADR 0003): the Solar API
   identity (`GetInverterInfo.cgi`), the active-device inventory
   (`GetActiveDeviceInfo.cgi`) and the API version (`GetAPIVersion.cgi`) are read
   without any credential and **fill** what the authenticated internal API left
   undetermined — identity and presence work with no credentials, while the
   device events and the two `/commands/*` read paths need one. **Both
   `/commands/*` reads are implemented** (2026-10-16), each with the consumer
   this decision demands: the device's **own clock** — its drift against the
   host clock is published and warned beyond ±60 s, because every local day
   boundary of the daily energy is computed from the installation's clock while
   the inverter's `E_Day` resets on *its* midnight — and the **state of its
   grid-limits self-test**, shown on the System screen, the application never
   triggering the test.
3. **An addition without a consumer is not an addition.** Any new entry must
   name its use — a model field, an endpoint, a screen. What is
   extracted but unused stays documented in `docs/ENDPOINTS.md`, not in the
   code.
4. **Unmeasured data is not computed.** A capability whose measurement
   is missing stays unavailable with its reason; a missing field stays `nil`, never
   `0` nor a replacement value borrowed from another field.
5. **An untested endpoint is announced as such** (`unverified`), including
   when its method and its fields are proven by the bundle: the missing proof
   is the device's response, not the reading of the official client.
6. **No secret to the frontend**, under any circumstance.

## Consequences

- "Expose all Solar API data" is **not** a goal: it is
  an inventory, not a need. The inventory lives in `docs/ENDPOINTS.md`.
- The cost of an addition is now measurable and uneven, and can be read in the tree:
  adding a field to an already extracted and already consumed DTO cost **one** production
  file plus its test (the event fields, `internal/detect/detector.go`);
  exposing a new identity field all the way to the screen costs **14** (of which one
  SQL migration), since `device` is frozen by `0002_device.sql` — decision
  deferred, cost recorded in `docs/UNKNOWNS.md` #3.
- The two `/commands/*` readings, each exposed to the screen, cost **35 touched
  files** (measured on the 2026-10-17 delivery): the allowlist and the client,
  the simulator and its `-clock-offset` flag, one SQL migration and its store
  methods, the detector's reader, the model types, the API's detection block, the
  source catalogue, the System panel, and their tests — plus five registers and
  this ADR. The cost of a *reading* is therefore of the same order as that of an
  identity field, which is why it stays reserved for a metric the Solar API does
  not provide.
- A screen does not break when the device changes: only the extraction layer
  moves, and it is covered by unit tests.
- The 10 paths whose payload is never read by the official client
  (`/status/registrationData`, `/status/powerflowcontroller`, `/commands/StandbyState`,
  `/commands/GetSDPFile`, `/commands/ConnectedPowermeter?componentId=`, the two
  `?sync=true` variants, `/config/logging`, `/config/solarweb`, `/config/gridcode`)
  remain out of scope until a metric names them.
- The offline state remains tenable: the frontend depends only on the persisted model.

## Rejected alternatives

- **1:1 mirror of the Solar API**: the public contract would become a third-party contract
  unvalidated on `1.19.7-1`; a break at every device change; impossible to
  mark `unverified` field by field.
- **Indexing all `channels` signals** (43 inverter, 23 meter, 10 battery):
  a volume of storage and schema without a measured need, and unvalidated units;
  contradicts the "no invented value" invariant.
- **Exposing non-`READ` paths** (34 call sites, including `POST`s): violates
  ADR 0004.
- **Reproducing the manufacturer's interface**: forbidden by `AGENTS.md` (no
  dependency on the manufacturer's interface).

## Sources

- `docs/ENDPOINTS.md` (extraction per path, [SOURCE-JS], bundle lines)
- `docs/SOURCES.md` (module-by-module provenance, confidence level)
- `docs/UNKNOWNS.md` (#3 identity, #4/#5 presence, #6 roles, #11 events)
- `AGENTS.md` (requirements), `docs/adr/0003`, `docs/adr/0004`
