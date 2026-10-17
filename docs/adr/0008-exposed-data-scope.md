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
7. **Technician-level information comes from an account, never from circumvention**
   (2026-10-17). The manufacturer's UI declares `technician`/`support` minimums on
   client **routes** (five of the ~37 entries of the route table, l. 43534, 43538,
   43605, 43609, 43619; one entry declares `"profi"`, l. ~43623) and on **signal
   visibility** (`_getRequiredUsersTechnician`, l. 128962-963, applied to the
   `channels` mappers), and on **no endpoint**: the only role attached to a read
   path is `customer`, on one registry entry (`/commands/GetUTCTime`,
   l. 76037-76040). A client-side role is a display condition — `_isUserAllowed`
   drops a stream, `canActivate` cancels a navigation, a mapper hides a signal —
   so the bundle cannot say which paths the **device** reserves for a technician,
   and no reading of it can. The application therefore:
   - reads technician-gated information **only** with a technician account the
     operator supplies, through the credentials already in the backend
     (`FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD`) and the same GET-only
     path; whatever that account may read is read like any other path, and still
     needs a metric and a consumer (decisions 2 and 3) before it enters the
     allowlist;
   - settles the **empirical** matrix by measurement, not inference: the same
     catalogue probed once per account (`cmd/fronius-probe --survey`, recorded in
     `source_probe`), where `401`/`403` under one account and `200` under the
     other **is** the gating evidence — the sanctioned answer to
     `docs/UNKNOWNS.md` #6;
   - **never** obtains access by extracting credentials, patching or dissecting
     the firmware, bypassing or replaying Digest, or exploiting the device. This
     is a boundary, not a preference: the technician surface is grid protection,
     firmware and service operations — the `DANGEROUS` class of
     `docs/ENDPOINTS.md` — where a mistake is an unsafe installation, and the
     project's invariant is that no non-GET can leave the process (ADR 0004,
     `internal/fronius/readonly.go`);
   - keeps technician-gated **configuration** in the same read scope only if a
     metric names it (decision 2). The `MUTATION`/`DANGEROUS` surface stays out of
     the application entirely, whatever the account — an account changes what may
     be *read*, never what may be *done*.

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
- Technician-level information costs **an account, not a bypass** (decision 7). The
  two-account survey is a measurement anyone holding the accounts can reproduce,
  and it is the only honest answer to "which paths does the device reserve?" — the
  role strings in the bundle stay what they are, display conditions, and this
  document says so wherever it lists them.

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
- **Obtaining technician access by reversing the firmware or the API**
  (credential extraction, Digest bypass, patching): circumventing an access
  control on grid-connected equipment, on the very surface that carries grid
  protection and firmware updates — illegal in several jurisdictions, unsafe in
  all, and outside the GET-only invariant the code enforces (decision 7).
- **Inferring a role matrix from the bundle's display conditions**: it would turn
  a hidden button into a claimed permission — precisely the error `AGENTS.md`
  forbids and `docs/UNKNOWNS.md` #6 keeps open.

## Sources

- `docs/ENDPOINTS.md` (extraction per path, [SOURCE-JS], bundle lines)
- `docs/SOURCES.md` (module-by-module provenance, confidence level)
- `docs/UNKNOWNS.md` (#3 identity, #4/#5 presence, #6 roles, #11 events)
- `AGENTS.md` (requirements), `docs/adr/0003`, `docs/adr/0004`
