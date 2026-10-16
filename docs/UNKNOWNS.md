# Register of unknowns

Each unknown carries: what is established, **the provisional decision** made in
phase 1, and **what would resolve it**. An unknown never turns into an assertion:
it remains visible in the interface (`/api/v1/capabilities`) and in
`docs/SOURCES.md`.

| # | Unknown | Provisional decision | What would resolve it |
|---|---|---|---|
| 1 | Shape of the `GetPowerFlowRealtimeData` JSON | `[HYPOTHESIS]` encoded in `dto.go`, tolerant decoding, fixtures marked synthetic | a real capture by `cmd/fronius-probe` and promotion of the fixtures |
| 2 | Authentication **of the Solar API** on firmware 1.19.7-1 | `[TO-TEST]`: the client sends none; the internal paths, for their part, are authenticated after a 401 | a 401/403 response from the probe on the Solar API path |
| 3 | **Partial source** of the inverter identity | `[SOURCE-JS]` + `[VERIFIED]` simulator: `/status/version` is the named source. **Resolved by the extraction**: the software version is read on the proven path `swrevisions.GEN24` (l. 128882, 135137), which the official client displays as the current version — the application now uses it (`firmwareOf`). Proven fields: `serialNumber`, `numberOfPhases`, `devicegroup`, `hwrevisions.FAMILYID`, and `hardwareId`/`devicename`/`articleNumber` (read, not exposed). **Still open**: no **model** key is observed ⇒ `Model` stays `nil` (the device group is no longer used as a fallback), and the three extracted fields are not exposed up to the screen — wiring them costs 14 files including an SQL migration, decision deferred for lack of a consumer (ADR 0008) | a real capture of `/status/version`: confirm the version, decide on the existence of a model key |
| 4 | Presence of a Fronius Smart Meter | detected by the `Cgi` envelope of `/components/PowerMeter/readable`: non-empty `Body.Data` ⇒ present, empty ⇒ absent, unreadable ⇒ **not determined** (never "absent"). Detection depends on no named field; the extraction does, however, record the fields usable afterwards (`channels`: 23 `SMARTMETER_*` signals, `attributes.addr`, `attributes["[ENABLE]"]` — `docs/ENDPOINTS.md`) | a real capture of `/components/PowerMeter/readable` |
| 5 | Presence of a battery and state of charge | presence: same rule as #4 on `/components/BatteryManagementSystem/readable`; `battery.soc` remains **unavailable** in the application — scope decision unchanged. Nuance brought by the extraction: the official client **does read** a relative state of charge, `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` in `%`, in the `channels` of this path (l. 128986), and also `inverters[<id>].SOC` in `/status/powerflow` (l. 129053): proof of the existence of a measurement therefore exists on the internal API side, without any reading having been made on the device | a real capture, then an explicit decision (justification by a metric unavailable in the Solar API) before any reading of the state of charge |
| 6 | Permission matrix per endpoint and per role | the extraction (see `docs/ENDPOINTS.md`) establishes the **role declared by the client** — a display condition, never an authorization: `_isUserAllowed` (l. 75972-75984) grants `customer` to `customer \| technician \| support`, and `technician`/`support` to `technician \| support`; **a single** registry entry declares `user: "customer"` (`/commands/GetUTCTime`, l. 76039). It also establishes the **account sent** (`_getUserAndPass`, l. 46807-46813: request-group account, except `Login` as `passwordUser`) — **no server permission is provable from a bundle**. Decision unchanged: the **code allowlist** (`internal/fronius/internalapi/allowlist.go`) is the only executable authorization: four internal paths, `READ`, GET; the other 79 are inventoried to be avoided | a test account and HTTP captures per role (401 / 403 / 200); see `docs/ENDPOINTS.md` |
| 7 | Technician access recovery | no confirmed secret or autonomous means; **no attempt** | official manufacturer procedure, installer documentation |
| 8 | Non-standard challenge header `x-www-authenticate` | `[SOURCE-JS]` + `[VERIFIED]` simulator **and** third-party client: a single retry on 401, challenge read first from the non-standard header, `www-authenticate` as fallback | the real device, to confirm it indeed uses the non-standard header |
| 9 | Dynamic hardware detection | **implemented**: `internal/detect` queries the four paths, the detection is persisted then injected into `model.Capabilities(detection)` | the resolution of #3, #4 and #5 by hardware capture |
| 11 | Shape of `/status/activeEvents` | **Resolved by the extraction and applied**: payload = array of objects; `severity` is an **integer** (1 error, 2 warning, 3 information, l. 135728-135733) — before the fix, only strings were recognized and every hardware event was classified as "information"; the instance identity is `uuid`, otherwise `prefix`-`eventID` (l. 135693, 164699) — deduplicating on `code` alone lost a second event of the same type; the displayed text is `label` (l. 135654). The keys `code`, `state`, `level`, `message`, `text`, `description` remain **fallback candidates**, and `viewer`, `category`, `confirmable`, `activeUntil`, `sourceID`, `timestamp` are kept in `Detail` (raw JSON bounded to 2,048 bytes) without being interpreted | a real capture: confirm names and types, the semantics of `viewer` (the client sees it as an audience level, l. 135709-135717) and the fact that `label` indeed carries readable text |
| 12 | Digest challenge signature (`cnonce="NaN"`) | not reproduced: the official client emits `NaN` through a conversion bug; we emit a random `cnonce` of 16 hexadecimal characters | if the device refuses anyway, replace with the constant `"NaN"` (fallback documented in `CHANGELOG.md`) |
| 10 | Outlying values of the device | **no filtering**: a received value is recorded as-is; only *absent* values are distinguished | a real observed need (drift, spike, meter running backwards) and an explicit rule |

## 1. Shape of the Solar API JSON

**Established.** The manufacturer's bundle contains **no** Solar API path:
`fcgi` 0 occurrences, `RealtimeData` 0, `PowerFlowRealtime` 0, `solar_api` 1
(only `/config/solar_api`, a configuration endpoint). The JS therefore provides
no schema. Reproducible verifications in `docs/SOURCES.md`.

**Decision.** The shape is encoded as a hypothesis in a single file
(`internal/fronius/solarapi/dto.go`), decoding ignores unknown fields, and the
fixtures carry `synthetic: true, confidence: "HYPOTHESIS"`. The `production.*`
capabilities are available but marked "unverified" with the caveat "Schema not
validated on firmware 1.19.7-1".

**Fallback if the capture differs.** Fix `dto.go`, the fixtures and
`docs/SOURCES.md` (citing the firmware version). The SQLite columns do not
change: they are already in base SI units (W, Wh, %).

## 2. Solar API authentication

**Established.** `[OBSERVED-CONVERSATION]`: the endpoint "responds". Nothing
proves that an authentication applies to the Solar API path: in the official
client, this path does not even exist. The **internal** paths, for their part, are
reached through two routes (with and without a header), which is established and
implemented.

**Decision.** The Digest transport is composed in front of both clients, but it
always sends the **bare** request first: if the Solar API path requires nothing,
no header is added and the behaviour remains that of phase 1.

## 3. Inverter identity

**Established.** The PDF export provides model, firmware version and serial
number `[SOURCE-PDF]`. On the HTTP side, `/status/version` is the source named by
the official client (header-less route, module `yTJA`, l. 183050), and the
extraction records there the fields **read**: `serialNumber`, `numberOfPhases`,
`devicegroup`, `hardwareId`, `devicename`, `articleNumber`, `swrevisions.GEN24`,
`hwrevisions.FAMILYID`.

**Resolved for the software version.** `swrevisions.GEN24` (l. 128882, 135137) is
the key that the official client displays as the current version: it now comes
before the flat candidates `firmwareVersion`/`firmware`/`version`, and the test
`TestVersionExtractsKnownAndCandidateKeys` covers both paths. Since no **model**
key is observed, `Model` stays `nil`: the device group (`devicegroup`) is no
longer used as a fallback, because it is a sibling field of another nature
(l. 183035).

**Decision.** The identity is read by `internal/detect` and persisted in the
`device` table. Unproven keys are extracted through an ordered list of candidates,
and an absent key gives `null`, never `""` nor "unknown". The three extracted
fields without a consumer (`hardwareId`, `devicename`, `articleNumber`) are not
persisted: exposing them costs 14 files including an SQL migration (`0004`),
measured cost and decision deferred as long as no need names them (ADR 0008).
Without a successful read, `GET /api/v1/system` returns `device: null` and an
`unknown` detection with its reason: the PDF is never copied, it describes an
instant, not a measurement.

**What would resolve it.** A real capture of `/status/version` by
`cmd/fronius-probe --endpoint /status/version` (§ `docs/PROBE-READONLY.md`):
confirm the version, decide on the existence of a model key.

## 4 and 5. Smart Meter and battery

**Established.** Neither the PDF nor the bundle proves the presence or absence of
a Fronius Smart Meter or a battery. The PDF declares `Battery Operation` and
`Full Backup` **active**: these are declared software features, not a hardware
inventory. The Ethernet is `disconnected`.

**Decision.** Presence is **detected** through the `Cgi` envelope: non-empty
`Body.Data` ⇒ present, empty or absent ⇒ absent, unreadable body ⇒ decoding error
therefore **not determined**. The three states are persisted as-is
(`meter_state`, `battery_state`): a 401 cannot become "no Smart Meter detected".
Any computation of consumption, import/export and self-consumption remains
forbidden without a detected meter, and `battery.soc` remains unavailable even
with a battery detected. No corresponding card is hard-coded in the interface:
they derive from `/api/v1/capabilities`.

**What would resolve it.** A real capture of the two `readable` paths — and, for
the state of charge, an explicit decision: the official client does read
`BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` (`%`) in the `channels` of the battery
path (l. 128986) and `inverters[<id>].SOC` in `/status/powerflow` (l. 129053), so
the measurement exists on the internal API side; its use by this application
remains a separate decision, not taken here, which requires a justification (a
metric the Solar API does not provide) and a capture to confirm the signal name on
firmware **1.19.7-1**.

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
