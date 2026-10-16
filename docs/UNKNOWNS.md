# Register of unknowns

Each unknown carries: what is established, **the provisional decision** made in
phase 1, and **what would resolve it**. An unknown never turns into an assertion:
it remains visible in the interface (`/api/v1/capabilities`) and in
`docs/SOURCES.md`.

| # | Unknown | Provisional decision | What would resolve it |
|---|---|---|---|
| 1 | Shape of the `GetPowerFlowRealtimeData` JSON | **Resolved by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: the shape is confirmed and `test/fixtures/solarapi/powerflow_produce_only.json` is promoted from the real capture. Two divergences from the retired `[HYPOTHESIS]` were settled: `Meter_Location` is a **string** (`"unknown"`), not `Meter_Location_Current`, an **integer** (field renamed and retyped); and `E_Day`/`E_Year` are **null** on this firmware, on `Site` and on the inverter (residual unknown #13). `BackupMode`, `BatteryStandby`, `Smartloads`, `Version` are present and tolerated. `dto.go`, the simulator, the tests and the fixtures are aligned | a later capture (another point in the day or after midnight) or, for the residual null energies, the resolution tracked under #13 (no local source delivered the counters; the `/status/powerflow` fallback was removed) |
| 2 | Authentication **of the Solar API** on firmware 1.19.7-1 | **Resolved by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: **no authentication required** on this installation. All five paths allowlisted at that time (the Solar API path and the four internal paths) answered `200 OK` with no `Authorization` header ever constructed (`digest_required: false` for each); no `401` was issued, so the Digest path was never exercised. `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` are therefore not needed here; the `internal/fronius/digest` machinery stays implemented and tested but unused by this device. A sixth path, `/status/powerflow`, briefly joined the allowlist later (fallback, #13), was **captured** (2026-10-16, `200`, no challenge) and has since been **removed**; the captures nonetheless established that this firmware answers every path without a challenge | a capture on another firmware or installation that does return a `401`/`403` |
| 3 | **Partial source** of the inverter identity | **Resolved by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: `/status/version` confirms the key the application reads — `swrevisions.GEN24` = `1.19.7-1` (used by `firmwareOf`) — and `numberOfPhases` = **1** (the installation is **single-phase**). **No model key exists** in the payload ⇒ `Model` stays empty (the device group `devicegroup` = `"P6RW"` is not a model name and is no longer used as a fallback). `devicename` = `"Primo GEN24 3.6 Plus"` and `articleNumber` = `"4,210,141,002"` are captured and **available but not surfaced** by the application — wiring them costs 14 files including a dedicated SQL migration, decision deferred for lack of a consumer (ADR 0008). The first survey (2026-10-16) answered `404` on `/solar_api/v1/GetInverterInfo.fcgi` and `/solar_api/v1/GetLoggerInfo.fcgi` ⇒ the "identity from the Solar API" route is **closed**; the identity stays on `/status/version` (`docs/SOURCES.md`) | only a decision remains: surface `devicename`/`articleNumber` to the screen, or not (no capture is needed, the payload is known) |
| 4 | Presence of a Fronius Smart Meter | **Resolved by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: `/components/PowerMeter/readable` answered `200` with `Body.Data` an **empty object** ⇒ **no Smart Meter installed**. The detection rule is confirmed (`Cgi` envelope: non-empty `Body.Data` ⇒ present, empty ⇒ absent, unreadable ⇒ **not determined**, never "absent"); the fields usable afterwards remain those of `docs/ENDPOINTS.md` (`channels`: 23 `SMARTMETER_*` signals, `attributes.addr`, `attributes["[ENABLE]"]`) | none on this installation; the presence rule stays available for another installation |
| 5 | Presence of a battery and state of charge | **Resolved for presence by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: `/components/BatteryManagementSystem/readable` answered `200` with `Body.Data` an **empty object** ⇒ **no battery installed**. `battery.soc` stays **unavailable** in the application — scope decision unchanged. The official client does read a relative state of charge, `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` in `%`, in the `channels` of this path (l. 128986), and `inverters[<id>].SOC` in `/status/powerflow` (l. 129053): the measurement exists on the internal API side, but its use by this application remains a separate decision | for `battery.soc`: an explicit decision (a metric the Solar API does not provide) and a capture on an installation fitted with a battery |
| 6 | Permission matrix per endpoint and per role | the extraction (see `docs/ENDPOINTS.md`) establishes the **role declared by the client** — a display condition, never an authorization: `_isUserAllowed` (l. 75972-75984) grants `customer` to `customer \| technician \| support`, and `technician`/`support` to `technician \| support`; **a single** registry entry declares `user: "customer"` (`/commands/GetUTCTime`, l. 76039). It also establishes the **account sent** (`_getUserAndPass`, l. 46807-46813: request-group account, except `Login` as `passwordUser`) — **no server permission is provable from a bundle**. **Route guard audited** (2026-10-16): the guard behind `canActivate: [oi.a]` on 37 routes is the bundle's only `CanActivate` (`canActivate(t, e)`, l. 81341, module `euqz` l. 81184-81388 — the `oi` → `euqz` binding is an `[INFERENCE]`, three concurring clues in `docs/ENDPOINTS.md`) and it is **not a role gate**: four middlewares (`_updateMiddleware` l. 81311, `_defaultMiddleware` l. 81325) yield `true`, `false` or a redirect `UrlTree` (`/update`, `/exchange`, `/dashboard`, `/startPage`), while the role branch runs through `_mapLoggedInUser` (l. 81366-81374) → `isUserPermittedForUrl` (l. 43732) → the three-valued `Wfm5` enum (l. 70078, `Permitted = 0` / `NotPermitted = 1` / `ProfiRequired = 2`), a refusal **cancelling the navigation** (the observable never emits) instead of answering 403. The per-route minima are a client table (`H8nL`, l. 43533-43535 `technician`, l. 43542 `customer`) and the flags are expanded from the login string (map `D`, l. 77898-77905). The only authorization-like value the client reads **from the device** is `writePermission` (l. 69202), compared field by field: its value per firmware and per account is unknown, and a payload boolean is not a matrix. Decision unchanged: the **code allowlist** (`internal/fronius/internalapi/allowlist.go`) is the only executable authorization: four internal paths, `READ`, GET; the other 79 are inventoried to be avoided | a test account and HTTP captures per role (401 / 403 / 200); see `docs/ENDPOINTS.md` |
| 7 | Technician access recovery | no confirmed secret or autonomous means; **no attempt** | official manufacturer procedure, installer documentation |
| 8 | Non-standard challenge header `x-www-authenticate` | `[SOURCE-JS]` + `[VERIFIED]` simulator **and** third-party client: a single retry on 401, challenge read first from the non-standard header, `www-authenticate` as fallback | the real device, to confirm it indeed uses the non-standard header |
| 9 | Dynamic hardware detection | **implemented**: `internal/detect` queries the four paths, the detection is persisted then injected into `model.Capabilities(detection)` | **done** — #3 (identity), #4 (no meter) and #5 (no battery) are resolved by the 2026-10-16 hardware capture (see above) |
| 11 | Shape of `/status/activeEvents` | **Resolved by extraction and confirmed by capture** `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: the body is a **root array** of objects (not a `Cgi` envelope); `severity` is an **integer** (3 = information), so the integer mapping in `internal/detect` is correct; the instance identity is `uuid` (e.g. `"56b077da-0d11-45be-8b24-7b09d53ad558"`) ⇒ the dedup key `device:<uuid>` is right; `label` is **not prose** but the vendor's **state-code name** (`LicensedFeatureDisabled`), which the vendor interface translates through its own dictionary (`STATECODETRANSLATIONS.StateCodes.<prefix>-<eventID>`); `subID`, `type`, `category` are additional keys the client reads, kept in `Detail`; `viewer` is an audience level (integer) | `viewer` semantics beyond an audience level (the client sees it as an audience level, l. 135709-135717) |
| 12 | Digest challenge signature (`cnonce="NaN"`) | not reproduced: the official client emits `NaN` through a conversion bug; we emit a random `cnonce` of 16 hexadecimal characters | if the device refuses anyway, replace with the constant `"NaN"` (fallback documented in `CHANGELOG.md`) |
| 10 | Outlying values of the device | **no filtering**: a received value is recorded as-is; only *absent* values are distinguished | a real observed need (drift, spike, meter running backwards) and an explicit rule |
| 13 | `E_Day` / `E_Year` **null** in `GetPowerFlowRealtimeData` (and in every local collection) | `[VERIFIED]` (hardware), firmware 1.19.7-1, 2026-10-16: `E_Day` and `E_Year` come back **null** on both `Site` and `Inverters."1"`, while `E_Total` is a number and `P_PV`/`P` are live. The model handles it correctly (`nil`, never `0`), and no local endpoint reports these two counters on the reference installation. **Three endpoints were captured the same day, all null on those two counters:** `GetPowerFlowRealtimeData.fcgi` (site `E_Day`/`E_Year` null), `/status/powerflow` (`site.E_Day`/`E_Year` null; body promoted as `test/fixtures/internalapi/status_powerflow.json`, `200`, `digest_required: false`), and the accumulation collection `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` (`DAY_ENERGY`/`YEAR_ENERGY` null, `883` bytes). Only the **total** is live on each (`E_Total` / `TOTAL_ENERGY` = `15089627.579722222` Wh) ⇒ **no local source exposes the day and year counters**. **The internal fallback was removed** (clean cutover, no shim): the allowlist entry `/status/powerflow` (the internal allowlist is back to **four** entries), `internalapi.PowerFlow`/`PowerFlowSite`, `Client.PowerFlow`, `powerFlowHasContent`, `collector.PowerFlowSource`/`Collector.Fallback`/`completeFromInternal`, `model.Sample.FallbackSource` and the `samples.fallback_source` column (dropped by migration `0006_drop_fallback_source.sql`), the `/api/v1/live` field `fallback_source`, the probe entry (the probe lists **six** entries again, the accumulation entry stays), and the simulator's `/status/powerflow` route; `internal/collector/fallback_test.go` is deleted and the tests pinned to the removed symbols are updated. **Kept on purpose:** `test/fixtures/internalapi/status_powerflow.json`, the recorded capture of that shape (array `inverters`, null counters) — its `_provenance` states the endpoint is **no longer called**. **Bonus finding (candidate, not implemented):** the accumulation answer carries `Body.Data.DeviceStatus` (`InverterState` `"Sleeping"`, `StatusCode` 13, `ErrorCode` 1175), the documented source of the **inverter state** the flow endpoints do not carry; it shows the inverter was **Sleeping** at 17:00 UTC, which explains the `0.0` powers while `TOTAL_ENERGY` stayed live. **Option (b) is implemented.** The day and year energies are **derived** from our own stored total (`internal/api/derived.go`, `Store.TotalEnergyAt` in `internal/storage`): the value is the difference between the current sample's `energy_total_wh` and the most recent stored sample **at or before the local boundary** — midnight for the day, 1 January for the year (store timezone `Europe/Brussels`). `GET /api/v1/live` carries the object `energy_derived` (`from`, `day_wh`, `day_baseline_utc`, `day_reason`, `year_wh`, `year_baseline_utc`, `year_reason`), present on that endpoint only. A value is **refused**, carrying its `*_reason` instead, when no sample carries a total at or before the boundary, when the closest sample before it is **older than 10 minutes**, when the counter **went backwards** since the boundary, or when the current sample carries no total. Measured fields are untouched: `energy_day_wh`/`energy_year_wh` stay null when the device does not report them, a computed value is never presented as a measurement, and the interface marks it **"estimated from the total"** (`kpi.energyDerived`, `apps/web/src/lib/energy.ts`). **Fresh-install consequence:** the day value is refused until a first local midnight has a baseline, the year value until a baseline exists before 1 January — the reason string says so, no zero is invented. The daily **chart** (`/api/v1/energy/daily`) is **not** covered: it reads `agg_hour.max_energy_day_wh`, null on this firmware, so it stays empty — a separate open decision. The first survey (2026-10-16) answered `404` on `/solar_api/v1/GetArchiveData.cgi` ⇒ device-side history is unavailable on this firmware, so the chart **cannot** be filled from the device; the aggregate/derived route is the only one (`docs/SOURCES.md`, `CHANGELOG.md`). **What stays open:** the **cause** of the null counters (not established) and the chart question above | the cause of the null: a later capture (another point in the day or after midnight). The chart would need an aggregate column carrying the total |

## 1. Shape of the Solar API JSON

**Established.** The manufacturer's bundle contains **no** Solar API path:
`fcgi` 0 occurrences, `RealtimeData` 0, `PowerFlowRealtime` 0, `solar_api` 1
(only `/config/solar_api`, a configuration endpoint). The JS therefore provides
no schema. Reproducible verifications in `docs/SOURCES.md`.

**Resolved by capture.** `[VERIFIED]` (hardware), firmware 1.19.7-1,
2026-10-16: `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` answered `200`,
`Content-Type: application/json`, and the shape is confirmed against the
retired `[HYPOTHESIS]`. Two divergences were settled: `Meter_Location` is a
**string** (`"unknown"`), not `Meter_Location_Current`, an **integer** — the DTO
field is renamed and retyped, with the simulator, the tests and the fixtures
aligned — and `E_Day`/`E_Year` are **null** on this firmware, on `Site` and on
the inverter, while `E_Total` is a number (see #13). `BackupMode`,
`BatteryStandby`, `Smartloads`, `Version` are present and tolerated (decoding
ignores unknown fields). The fixture `powerflow_produce_only.json` is promoted
from the real capture.

**Decision.** The shape is fixed in a single file
(`internal/fronius/solarapi/dto.go`), decoding still ignores unknown fields, and
the promoted fixture carries `synthetic: false, confidence: "VERIFIED
(hardware)"`. The caveat "Schema not validated on firmware 1.19.7-1" no longer
applies for this firmware: the schema is now validated. The SQLite columns do
not change: they are already in base SI units (W, Wh, %).

**Fallback if a later capture differs.** Fix `dto.go`, the fixtures and
`docs/SOURCES.md` (citing the firmware version), as was done here for
`Meter_Location`.

**First survey of the reference installation (2026-10-16).** The first real
survey walked the 27 catalogued paths, one read-only `GET` each, and answered
**11 `ok`, 4 `empty`, 2 `auth_required`, 10 `not_found`** on firmware `1.19.7-1`
(report `probe-survey/survey_20261016T191253Z.json`, not versioned; rows in
`data/fronius.db`, table `source_probe`; full outcome in `docs/SOURCES.md`,
§ Hardware captures). It is the per-firmware evidence behind the closures in
this table: `GetArchiveData.cgi` (the daily chart, #13),
`GetInverterInfo.fcgi`/`GetLoggerInfo.fcgi` (the identity, #3) and six other
Solar API candidates are `404` on this firmware, while `CommonInverterData` and
`CumulationInverterData` are served. One candidate stays open with **no reason
asserted**: `/solar_api/GetAPIVersion` also answered `404`; the documented path
may need a suffix (`.cgi`), but the answer does not say.

## 2. Solar API authentication

**Established (bundle).** In the official client the Solar API path does not
exist; the **internal** paths, for their part, are reached through two routes
(with and without a header), which is established and implemented.

**Resolved by capture.** `[VERIFIED]` (hardware), firmware 1.19.7-1,
2026-10-16: **no authentication is required** on this installation. All five
paths allowlisted at that time — the Solar API path and the four internal paths —
answered
`200 OK` with no `Authorization` header ever constructed (`digest_required:
false` for each); no `401` was issued, so the Digest path was never exercised.
`FRONIUS_DEVICE_USER`/`FRONIUS_DEVICE_PASSWORD` are not needed here. A sixth
path, `/status/powerflow` (the day/year energy fallback, #13), was added to the
allowlist later, **captured** (2026-10-16, `200`, without any challenge,
`inverters` an array, `digest_required: false`) and has since been **removed**
(see #13). The captures also proved that this firmware answers every path without
a challenge, so the same is expected of it.

**Decision.** The Digest transport stays composed in front of both clients and
always sends the **bare** request first. It remains implemented and tested, but
is unused by this device; a firmware or installation that does return a `401`
would re-enable it without change.

## 3. Inverter identity

**Established (bundle and PDF).** The PDF export provides model, firmware
version and serial number `[SOURCE-PDF]`. On the HTTP side, `/status/version` is
the source named by the official client (header-less route, module `yTJA`,
l. 183050), and the extraction records there the fields **read**:
`serialNumber`, `numberOfPhases`, `devicegroup`, `hardwareId`, `devicename`,
`articleNumber`, `swrevisions.GEN24`, `hwrevisions.FAMILYID`.

**Resolved by capture.** `[VERIFIED]` (hardware), firmware 1.19.7-1,
2026-10-16: `/status/version` answered `200`, `application/json`. Confirmed:
`swrevisions.GEN24` = `1.19.7-1` is the key read by the application (`firmwareOf`,
l. 128882, 135137), and the value matches the system export;
`numberOfPhases` = **1**, so the installation is **single-phase**. **No model key
exists** in the payload ⇒ `Model` stays empty (the device group
`devicegroup` = `"P6RW"` is not a model name and is no longer used as a
fallback). `devicename` = `"Primo GEN24 3.6 Plus"` and
`articleNumber` = `"4,210,141,002"` are captured and **surfaced** by the
application as their own rows (see the decision below).

**Decision.** The identity is read by `internal/detect` and persisted in the
`device` table. Unproven keys are extracted through an ordered list of candidates,
and an absent key gives `null`, never `""` nor "unknown". The two captured fields
are persisted in `device.device_name` / `device.article_number` (migration
`0005_device_name.sql`) and exposed by `/api/v1/system`; the panel shows them as
**Device name** and **Article number**, separate from the model row. `hardwareId`
stays unused: no consumer names it (ADR 0008). The name is never copied into
`model`: a name is not a model, and `model` stays `null` because no model key
exists in that payload. Without a successful read, `GET /api/v1/system` returns
`device: null` and an `unknown` detection with its reason: the PDF is never
copied, it describes an instant, not a measurement.

**What remains open.** Nothing to capture: the payload is known and the two fields
are in use. Only the model name stays undetermined, and only a future firmware
that reports a model key — or an explicit decision to display `devicename` *as*
the model — would change that. **Closed by the first survey (2026-10-16):**
`/solar_api/v1/GetInverterInfo.fcgi` and `/solar_api/v1/GetLoggerInfo.fcgi`
answered `404` on firmware 1.19.7-1 (`docs/SOURCES.md`), so the identity cannot
come from the Solar API on this firmware; it stays on the internal
`/status/version`.

## 4 and 5. Smart Meter and battery

**Resolved by capture.** `[VERIFIED]` (hardware), firmware 1.19.7-1,
2026-10-16: both `/components/PowerMeter/readable` and
`/components/BatteryManagementSystem/readable` answered `200` — but with
`Content-Type: text/javascript`, **not** `application/json` — and `Body.Data` was
an **empty object** in both ⇒ **no Smart Meter and no battery installed**. The
PDF declared `Battery Operation` and `Full Backup` **active**: those were declared
software features, not a hardware inventory (the Ethernet is `disconnected`).

**Decision.** Presence is **detected** through the `Cgi` envelope: non-empty
`Body.Data` ⇒ present, empty or absent ⇒ absent, unreadable body ⇒ decoding error
therefore **not determined**. The three states are persisted as-is
(`meter_state`, `battery_state`): a 401 cannot become "no Smart Meter detected".
Any computation of consumption, import/export and self-consumption remains
forbidden without a detected meter, and `battery.soc` remains unavailable even
with a battery detected. No corresponding card is hard-coded in the interface:
they derive from `/api/v1/capabilities`.

**What remains open (state of charge).** The official client does read
`BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` (`%`) in the `channels` of the battery
path (l. 128986) and `inverters[<id>].SOC` in `/status/powerflow` (l. 129053), so
the measurement exists on the internal API side; its use by this application
remains a separate decision, not taken here, which requires a justification (a
metric the Solar API does not provide) and a capture on an installation fitted
with a battery.

## 6. Permission matrix

**Established.** The bundle declares roles on the **client** side
(`RoleEverybody`, `RoleCustomer`, `RoleTechnician`, `RoleSupport`,
`SetupAccessRole01/02`) that condition the display. No server role is proven.
`docs/ENDPOINTS.md` classifies the 79 paths as READ / MUTATION / DANGEROUS **by
intention and usage in the JS**.

**What the method/arguments/DTO extraction adds.** For each path read by the
official client, `docs/ENDPOINTS.md` now gives the method, the arguments, the
envelope and the role **declared** by the client. Three facts emerge:
`_isUserAllowed` (l. 75972-75984) implements only a display hierarchy
(`customer` ⊂ `technician` ⊂ `support`), **a single** registry entry declares a
role (`user: "customer"` on `/commands/GetUTCTime`, l. 76040), and the account
actually sent is that of the request group (`fallback`, l. 46809) except for
`Login` (`passwordUser`, l. 46811). None of this is an authorization: a client
bundle cannot prove what the firmware accepts.

**Decision.** The code allowlist is the only executable authorization: four
internal paths plus the Solar API path, all `READ`, all in GET. Everything else is
inventoried to be avoided, never to be authorized. An "unknown" cell is not an
implicit permission — and a method now **observed** (for example the official
client's `POST` on `/commands/ConnectedPowermeter`, l. 91025) does not change
this list.

## 7. Technician recovery

**Established.** `[SOURCE-JS]`: `GET /commands/PasswordRecovery?user=<user>`
obtains a challenge and `remainingSignatureChecks`; the browser **does not
compute** the signature, it is entered by the user then verified by the firmware
(possible failure `ERROR_VERIFY_SIGNATURE`). No password, no signature secret, no
autonomous means has been found.

**Decision.** No attempt, not even "just to see": the number of attempts is
limited and failure can lock the access. The application implements no recovery.

**Legitimate routes.** Official manufacturer documentation, documentation left by
the installer, password manager, earlier browser profiles.

## 9. Dynamic hardware detection

**Decision.** `model.Capabilities(detection)` takes a three-state detection per
resource (`unknown`, `absent`, `present`); the API builds it from the last
persisted reading (`internal/detect`, `device` table). No configuration flag
allows forcing these values: it would create an "available" capability without a
measurement behind it, or an "absence" without a reading.

**Traffic rule.** A read that receives a 401 while no credential is configured
makes **that path** give up until restart, with the reason that names the two
environment variables. An unknown state is not an absence, but there is no point
re-emitting a 401 every minute.

## 10. Outlying values

**Decision.** No filtering is applied in phase 1: if the device returns a value,
it is recorded. The model distinguishes only **present** and **absent** (`nil`),
never "plausible" and "outlying". Inventing a rejection rule without observation
would mean deciding on the user's behalf what a valid measurement is.

**What would resolve it.** A real observed case (meter running backwards,
impossible spike, negative value where it cannot be) and a written rule:
thresholds, marking in `Quality`, retention of the raw value. In the meantime,
responsibility is left to the sensor and the interface displays what is received.

## Unknowns out of MVP scope

Documented for the record, with no decision needed in phase 1: Angular guard
`oi.a` and component checks, format of the `sdp.cry` file, exact role of
`SupportAccount`, profiles and password manager, need for an application
authentication for remote access (to be decided by an ADR before any permanent
remote access).

The DTOs of the read-only internal commands are **no longer** an unknown out of
scope: they are extracted in `docs/ENDPOINTS.md`. What remains `[NOT OBSERVED]`
after extraction — a payload never consumed by the official client — is named in
this table: `/status/registrationData` (JSON blob), `/status/powerflowcontroller`,
`/commands/StandbyState`, `/commands/GetSDPFile`,
`/commands/ConnectedPowermeter?componentId=`, the two `?sync=true` variants and
`/config/logging`, `/config/solarweb`, `/config/gridcode`.
