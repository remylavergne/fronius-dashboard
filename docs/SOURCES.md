# Sources, provenance and confidence levels

This document keeps the register of **where each piece of information** used by
the application comes from, and with what level of proof. Any statement about the
hardware or about a protocol must be traceable to a line of this file.

## Confidence levels

| Label | Meaning |
|---|---|
| `[SOURCE-JS]` | Read directly in `references/fronius-original.js`. Proves the presence of client-side code, **not** a permission granted by the firmware. |
| `[SOURCE-PDF]` | Read in the manufacturer's system export (`fronius-information.pdf`, 16 October 2026). |
| `[OBSERVED-CONVERSATION]` | Reported during the analysis, not replayed in this session. |
| `[HYPOTHESIS]` | Assumed shape or behaviour, encoded as such in the code. |
| `[TO-TEST]` | Easy to verify, but unverified: requires the hardware or the probe. |
| `[VERIFIED]` | Run and observed in this repository (tests, simulator, reproducible command). |

Standing rule: "found in the bundle" ≠ "allowed by the server". No `[SOURCE-JS]`
item may be presented as a firmware permission.


## Vendor artefact

`references/fronius-original.js` — 184,378 lines, ≈ 8.6 MB. This is the
JavaScript bundle of the manufacturer's Web interface, kept **unmodified** as the
primary source of analysis, in accordance with `AGENTS.md`.

This file is a proprietary software artefact (Fronius International GmbH). It is
**explicitly excluded from the scope of the MIT license** of this repository
(see `LICENSE` and the License section at the end of the document).

## Code landmarks in the bundle

Line numbers in `references/fronius-original.js`. They may change after
reformatting the file; modules are identified by their Webpack `id`, which
remains stable.

| Module | Role | Start | Retained for |
|---|---|---|---|
| `CNEH` | Storage of credentials in `localStorage` (key `AuthHash`) | 24793 | FORBIDDEN to reproduce |
| `JWhm` | Base HTTP class: `callGet$`, `callPost$`, `callGetWithoutCatch$`, `callPostWithoutCatch$`, `_getUserAndPass` | 46762 | Phase 2 |
| `LsM5` | Login DAO, store update | 48467 | Context |
| `i3oe` | Command service (`Login`, `Logout`, `SetPassword`, `ActivatePasswords`, …) | 90502 | Phase 2 / out of scope |
| `pMcQ` | Enumeration of the response **envelope type**: `Default = 0`, `Cgi = 1`, `CommandResult = 2` (l. 155872-155877) | 155866 | Envelope extraction |
| `mgOI` | Login component | 121442 | Context |
| `nuFz` | `[Login] login` action, role logic in the store | 122473 | Context |
| `xeXE` | Generation of the HTTP Digest headers | 182391 | Phase 2 (implemented) |
| `ytu1` | Re-export of the Digest module | 183396 | Phase 2 (implemented) |
| `aYRN` | Registry of "polled" endpoints (class `b`, inheriting from `JWhm`): path, interval, envelope type and declared role per entry (l. 76027-76045) | 75885 | Phase 2 internal paths |
| `yTJA` | Status service: second client, independent of `JWhm`, which reads `httpClient.get(this.url)` **without** an `Authorization` header (l. 183019-183021) | 182982 | Second route to the same paths |
| `rCKn` | Configuration DAO: one instance per `/config/*` path (l. 161462-161485), `loadData()` = `GET` (l. 161422) and `saveData()` = `POST` (l. 161428), envelope `Default` | 161333 | Extraction of `/config/*` methods |
| `lFFc` | `/commands/License` and its variants `?status=`, `?acknowledgement=`, `?serviceFile=` (l. 120669-120677) | 120656 | Extraction (outside the allowlist) |
| `966A` | `/commands/InverterRollbackStatus` (l. 21341) | 21319 | Extraction (outside the allowlist) |
| `rqlu` | Event identifier `prefix-eventID` and translation key of the states (l. 164699, 164705) | 164690 | DTO of `/status/activeEvents` |
| `jRQl` | Enumeration `T_SystemServiceId` (`GridlimitsAutotest = 21`, l. 116025) | 115999 | `serviceId` argument |

Other landmarks: `createNode` (password derivation) around 43079,
`recoverPassword` around 137550, `createSdpFile` around 136669.

## Verifications run on the bundle

Reproducible commands from the repository root, operating **read-only** on the
artefact:

| Search | Result | Consequence |
|---|---|---|
| `grep -c fcgi` | `0` | No `.fcgi` endpoint is called by the manufacturer's interface. |
| `grep -c RealtimeData` | `0` | `GetPowerFlowRealtimeData` is absent from the bundle. |
| `grep -c PowerFlowRealtime` | `0` | idem. |
| `grep -c solar_api` | `1` | Single occurrence: `new h(t,"/config/solar_api",this)` — a **configuration** endpoint, not a measurement one. |
| `grep -c x-www-authenticate` | `7` | Non-standard challenge header, as implemented client-side. |
| `grep -c` on `www-authenticate` without the `x-` prefix | `0` | No standard variant is read by the client. |
| `grep -c AuthHash` | `15` | Credentials stored in plain text client-side (see below). |
| `grep -c 'www-authenticate'` (outside the `x-` prefix) | `0` | The official client only reads the non-standard header. |

Commands of the method/arguments/DTO extraction (see `docs/ENDPOINTS.md`):

| Search | Result | Consequence |
|---|---|---|
| `grep -n 'callGet\$(\|callGetWithoutCatch\$(\|callPost\$(\|callPostWithoutCatch\$('` | `53` occurrences | minus the 4 definitions of `JWhm` (l. 46782, 46787, 46791, 46796) and the 3 lines of `_refreshValue` (l. 75958, 75960, 75963) ⇒ **46 lines**, of which 2 internal delegations (l. 46784, 46793) ⇒ **44 call sites** |
| `grep -n '_httpClient\.\(get\|post\)(' \| awk '$1 < 46761 \|\| $1 > 46820'` (the `\|` are the shell escapes) | `7` | 6 raw calls on device paths (SDP, Service/License blob), 1 out of scope (Angular Material icons, l. 54377) |
| `grep -n '/status/version'` | `1` | a single occurrence (l. 183050, module `yTJA`): `/status/version` **is not** in the polled registry `aYRN` |
| `grep -n 'new h(t,"/config'` | `24` | one DAO and one literal per `/config/*` path (l. 161462-161485) |

**Structural conclusion: the bundle contains no Solar API path.** It therefore
provides **no schema** for `GetPowerFlowRealtimeData`: neither field names, nor
units, nor locations. The schema of `internal/fronius/solarapi/dto.go` is a
`[HYPOTHESIS]` based on the public documentation of Solar API v1 and on reported
observations. This is the reason for the `cmd/fronius-probe` probe and
`docs/PROBE-READONLY.md`.

## Digest authentication — implemented in phase 2

Everything below is `[SOURCE-JS]` (module `xeXE`, l. 182391-182440, and module
`JWhm`, l. 46761-46795), reproduced in `internal/fronius/digest` and verified by
the simulator **and** by a third-party client (`curl --digest`, § Verifications).

### Two HTTP routes to the same paths

This is the structural fact of phase 2: the same internal paths are read by two
distinct clients of the bundle.

| Route | Module | `Authorization` header | Proof |
|---|---|---|---|
| "Polled" registry | `aYRN` (l. 75885), `let b = class extends f.a` where `f = n("JWhm")` (l. 75899, 75901) | yes: `callGet$` = GET + Digest + **one** retry on 401 (l. 46782-46786) | `/status/devices` l. 76027, `/status/network` l. 76028, `/status/emrs` l. 76029, `/status/activeEvents` l. 76030, `/status/events` l. 76031, `/components/BatteryManagementSystem/readable` l. 76032, `/components/cache/readable` l. 76033, `/components/Ohmpilot/readable` l. 76034, `/components/PowerMeter/readable` l. 76035, `/status/powerflow` l. 76036, `/commands/GetUTCTime` l. 76037, `/status/common` l. 76041, `/commands/LicenseExchange` l. 76042, `/commands/GetServiceState?serviceId=` l. 76043, `/commands/RemoteAccess` l. 76044, `/commands/StandbyState` l. 76045 — **`/status/version` is not in it** |
| Status service | `yTJA` (l. 182982), class `A` **independent of `JWhm`** | **no**: `this.httpClient.get(this.url)` (l. 183020), no header construction | `/status/common` l. 183049, `/status/version` l. 183050, `/status/registrationData` l. 183051, `/status/network` l. 183052, `/status/capabilities` l. 183053, `/status/ioServices` l. 183054, `/status/powerflowcontroller` l. 183055, `/status/deviceDB` l. 183056, `/components/DataManager/readable` l. 183057, `/status/license` l. 183058, `/status/license/features` l. 183059, `/status/activeEvents?sync=true` l. 183060, `/status/events?sync=true` l. 183061 |

Consequence retained: **the same path can respond without authentication**. The
client of this application therefore always sends the bare request first, and
authenticates only in response to a `401` — exactly what `callGet$` does.

### Shape of the produced header

`Digest username="U", realm="R", nonce="N", uri="URI", response="RESP"`,
then `, opaque="O"` if the challenge carries one, then
`, qop=QOP, nc=NC, cnonce="CNONCE"` (l. 182425-182428).

| Point | `[SOURCE-JS]` value | Retained treatment |
|---|---|---|
| Algorithm | `HA1 = MD5(user:realm:pass)`, `HA2 = MD5(METHOD:uri)`, `response = MD5(HA1:nonce[:nc:cnonce:qop]:HA2)` | reproduced identically; anchored on the RFC 2617 vector |
| `qop` | truncated at the first token (`qop="auth,auth-int"` → `auth`) | reproduced |
| `nc` | `String(++NC)` padded to 8 digits by `NC_PAD` → **decimal** zero-padded, monotonic counter | reproduced (`%08d`, incremented under lock) |
| Without `qop` | neither `qop`, nor `nc`, nor `cnonce` in the header (RFC 2069 form) | reproduced |
| `algorithm=` | **never sent** by the official client | never sent (the `algorithm=SHA-256` challenge is refused, not bypassed) |
| `cnonce` | `Number(randomBytes(8)).toString(16)` where `randomBytes` joins hexadecimal bytes with `,` (l. 182405-182411) → `Number("a3,ff,…")` equals `NaN`: the official client literally emits `cnonce="NaN"` | **not reproduced**: 8 bytes of `crypto/rand` in hexadecimal (16 characters). The server re-reads the `cnonce` from the header, it cannot verify it. Fallback documented in `CHANGELOG.md` if the device refuses anyway. |
| Quotes | `qop=` and `nc=` **without** quotes, `cnonce` **in** quotes | reproduced, verified character by character |
| Incomplete challenge | empty `realm` or `nonce` ⇒ empty header (l. 182412-182414) | translated into an explicit error, never into an empty header |

### Retry and challenge storage

- **A single** retry on HTTP 401; the `callGetWithoutCatch$` /
  `callPostWithoutCatch$` variants do not retry (l. 46787-46790).
- The challenge is read in the **`x-www-authenticate`** response header (7
  occurrences in the bundle, **0** occurrence of `www-authenticate` alone); this
  application tries the non-standard one first, then the standard one.
- The raw challenge is stored as-is and re-parsed on each header
  (`setNonceHeader`, l. 29232); `getUserAndPass` returns `username:password`
  (l. 29238-29240), split at the **first** `:` — a password may contain one.

### What is never reproduced

- `CNEH`: `localStorage.setItem("AuthHash", username + ";" + password)`,
  credentials **in plain text** in the browser (l. 29232, 15 occurrences of
  `AuthHash`).
- `createNode`: `MD5("user:Webinterface area:password")` then PBKDF2
  (100,000 iterations, 64-byte output, 4-byte salt from
  `Math.random()`). Out of scope.
- Credentials never leave the server process: neither to the frontend, nor to an
  HTTP response, nor to a log (type `secret.String`).

What cannot be asserted: that the firmware actually grants a given permission to
a given role. The role names (`customer`, `technician`, `support`, `profi`,
`utility`) are client-side `[SOURCE-JS]`, and the allowlist of
`internal/fronius/internalapi/allowlist.go` remains the **only** executable
authorization of this application.

## Verifications run on Digest `[VERIFIED]`

| Check | Result |
|---|---|
| RFC 2617 vector (`Mufasa` / `testrealm@host.com` / `Circle Of Life`, `nc=00000001`, `cnonce=0a4f113b`) | `response="6629fae49393a05397450978507c4ef1"` — `go test ./internal/fronius/digest/ -run TestRFC2617Vector` |
| Header shape | field order, `qop=`/`nc=` without quotes, `cnonce` in quotes, **no** `algorithm=` — test on the full string |
| Number of requests per case | fresh challenge: 2 (bare then authenticated); cached nonce: 1; stale nonce: 3 (cache refused, bare request, fresh challenge); 401 without a challenge: 1 (no retry); persistent 401: 2 (no third attempt) |
| Third-party client | `curl --digest -u sim:simpass` gets `200` on the simulator in `--emit-standard-challenge` mode — an independent implementer recomputes the Digest and the server accepts it |
| Counter uniqueness | 50 concurrent requests (`go test -race`) produce 50 distinct and increasing `nc` |


## Fields of internal paths

Column "proven key?": **yes** means the name is read in the bundle at the cited
line; **no** means the name is a **candidate** of the simulator, added to
exercise the client's reading tolerance, and to be confirmed by a capture
(`cmd/fronius-probe`).

| Path | Field | Proven key? | Proof |
|---|---|---|---|
| `/status/version` | `serialNumber` | yes | l. 45243, l. 127380, l. 143048 (`t.serialNumber`) |
| `/status/version` | `numberOfPhases` | yes | l. 183034 |
| `/status/version` | `devicegroup` | yes | l. 183035 (`"P" === t.devicegroup[0] ? 1 : 3`) |
| `/status/version` | `hwrevisions.FAMILYID` | yes | l. 143049 |
| `/status/version` | `hardwareId`, `devicename`, `articleNumber` | yes | l. 126568, 128813, 128814, 128842 — **read** by the official client, **not used** by the application at this stage: exposing them up to the screen costs 14 files including an SQL migration (`docs/UNKNOWNS.md` #3), decision deferred |
| `/status/version` | `swrevisions.GEN24` | yes | l. 128882, 135137 — **read by the application**: this is the software version displayed by the official client; nested key, read before the flat candidates (`firmwareOf`, `internal/fronius/internalapi/client.go`) |
| `/status/version` | `firmwareVersion` | **no** | fallback candidate, used only if `swrevisions.GEN24` is absent |
| `/status/version` | `modelName`, `model` | **no** | candidates: **no model key** is observed in the bundle. `devicegroup` is not a model fallback — it is the device group, a sibling field (l. 183035) |
| `/components/PowerMeter/readable` | `channels` (23 `SMARTMETER_*` signals), `attributes.addr`, `attributes["[ENABLE]"]` | yes | l. 129011, 129015, 130976-130983; envelope `Cgi` (l. 75958, 183011); **detection** is always based on a non-empty `Body.Data` |
| `/components/BatteryManagementSystem/readable` | `channels` (10 signals) | yes | l. 128986; envelope `Cgi` (l. 75958) |
| `/status/activeEvents` | `severity` (integer: 1 error, 2 warning, 3 information) | yes | l. 135690 + l. 135728-135733 (`getEventLevel`); **used by the application** (`severityValue`, `internal/detect/detector.go`) — before the fix, the integer severity was not recognized and every hardware event was classified as "information" |
| `/status/activeEvents` | `uuid`, `prefix`-`eventID` (instance identity) | yes | l. 135693, 164699, 135742 (comparison), 135760 (`ConfirmEvent` receives `{uuid}`); **used by the application** as an anti-duplication key |
| `/status/activeEvents` | `label` (text displayed as a fallback by the client) | yes | l. 135654; **used by the application** as the event text |
| `/status/activeEvents` | `timestamp`, `activeUntil` (device clock seconds) | yes | l. 135694-135695 — not used: `TS` remains the collector clock, the sort and retention axis |
| `/status/activeEvents` | `viewer`, `category`, `confirmable`, `sourceID` | yes | l. 135691, 135696, 135697, 135701 — read by the client, **not used** by the application: `Detail` keeps the raw JSON |
| `/status/activeEvents` | `code`, `state`, `level`, `message`, `text`, `description` | **no** | application candidates: they serve deduplication and severity, with systematic fallback |

The client extracts the identity through **ordered candidate key lists**
(`internal/fronius/internalapi/client.go`): adding an observed key at the head of
the corresponding list is enough, no structure needs to change.

## Extraction: method / arguments / DTO per path

`docs/ENDPOINTS.md` (section `## Method, arguments and DTO per path (bundle
extraction)`) carries the per-path table. Provenance of this extraction, all in
`[SOURCE-JS]` confidence, **module by module**, in the bundle kept unmodified:

| Module | Lines | What is drawn from it |
|---|---|---|
| `aYRN` | 75885-76048 | polled registry: path, interval, envelope type (l. 76027-76045); `_refreshValue` proves that **all** entries are read with `GET` (l. 75955-75964); `_isUserAllowed` (l. 75972-75984) and `{user: "customer"}` (l. 76037-76040) |
| `JWhm` | 46762-46822 | four call methods: `callGet$`/`callGetWithoutCatch$` (GET) and `callPost$`/`callPostWithoutCatch$` (POST), plus `_getUserAndPass` (l. 46782-46813) |
| `pMcQ` | 155866-155878 | enumeration of the envelope type: `Default`, `Cgi`, `CommandResult` |
| `yTJA` | 182982-183066 | second client without a header: literals of the `/status/*` paths and `/components/DataManager/readable`, envelope per path (l. 183010-183021) |
| `i3oe` | 90502-91250 | call sites of `/commands/*`, URL arguments and request bodies |
| `rCKn` | 161333-161540 | `/config/*` DAO: one literal per path, `loadData` (GET, l. 161422), `saveData` (POST, l. 161428) |
| `lFFc`, `966A` | 120656-120780, 21319-21345 | `/commands/License` and variants, `/commands/InverterRollbackStatus` |
| `rqlu`, `jRQl` | 164690-164709, 115999-116025 | event identifier (`prefix`-`eventID`), enumeration `T_SystemServiceId` |

The terminal fields (`channels` signals, `Hosts[]` keys, etc.) are extracted in
the consumer components, whose line numbers appear in the same table. Reference
version unchanged: `WebUI 1.14.2-1` and `GEN24/DEVICEGROUP/GEN24PRIMO 1.19.7-1`
(§ Reference hardware).

What the extraction **does not establish**: any server permission. The roles
extracted are client display conditions (see `docs/UNKNOWNS.md` #6).

## Endpoint inventory

`03-API-INVENTORY.md` lists the paths **present in the bundle**: 35 under
`/commands/`, 24 under `/config/`, 20 others (`/components/*`, `/status/*`,
`/update/start`). This inventory is **static**: it proves neither accessibility,
nor the method, nor the permissions.

| Path | Method | Auth | Proof status |
|---|---|---|---|
| `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | none in phase 1 | `[OBSERVED-CONVERSATION]` "responds"; **absent from the bundle**; schema `[HYPOTHESIS]` |
| `/status/powerflow` | GET | internal | `[SOURCE-JS]` — polled every 5,000 ms by the official client; basis for `FRONIUS_POLL_INTERVAL` |
| `/status/version` | GET | Digest "after 401" | **called** (phase 2): `[SOURCE-JS]` l. 183050 (`yTJA` route, without a header — **this path is not in the `aYRN` registry**); `[VERIFIED]` on the simulator. Provides `serialNumber`, `numberOfPhases`, `devicegroup` |
| `/status/activeEvents` | GET | Digest "after 401" | **called** (phase 2): `[SOURCE-JS]` l. 76030 (10,000 ms interval); `[VERIFIED]` on the simulator. Fields read listed in `docs/ENDPOINTS.md` |
| `/components/PowerMeter/readable` | GET | Digest "after 401" | **called** (phase 2): `[SOURCE-JS]` l. 76035 (envelope `Cgi`); `[VERIFIED]` on the simulator. Presence deduced from `Body.Data` |
| `/components/BatteryManagementSystem/readable` | GET | Digest "after 401" | **called** (phase 2): `[SOURCE-JS]` l. 76032; `[VERIFIED]` on the simulator. Presence deduced from `Body.Data` |
| `/config/meter`, `/config/batteries` | `GET` (l. 161422) | Digest "after 401" | **off-list** by scope choice: literal and verb **observed** (l. 161471 and 161462; module `rCKn`), the opposite claim having been corrected |
| `/config/*` (24 paths), `/status/*`, `/components/*` | GET | Digest "Customer" | `[OBSERVED-CONVERSATION]` for a few (`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping`: 200; `/config/powerlimits`, `/config/setup/*`: 200 `{}`); phase 2 |
| `/commands/*` | GET/POST | Digest depending on the command | `[SOURCE-JS]`; several are mutations — **out of scope**, never called |

What is still missing and what would resolve it:

- Real schema of the Solar API → run of `fronius-probe` (§ `docs/PROBE-READONLY.md`).
- Absence or presence of authentication on firmware **1.19.7-1** → probe
  `[TO-TEST]`: the simulator exercises both cases, it does not prove the firmware.
- Real names of the **model** and the **firmware** in `/status/version` → probe
  `[TO-TEST]`: the extraction establishes the fields actually read by the client
  (`serialNumber`, `numberOfPhases`, `devicegroup`, `hardwareId`, `devicename`,
  `articleNumber`, `swrevisions`, `hwrevisions`), but **neither `modelName` nor
  `firmwareVersion`** appears there: they remain simulator candidates.
- Shape of `/status/activeEvents` → probe `[TO-TEST]`: the raw detail is kept
  bounded, so no information is lost in the meantime.
- Presence of a Smart Meter or a battery on the installation: the simulator
  exercises the four combinations, the hardware will decide. See `docs/UNKNOWNS.md`.

## Assumed shape of the Solar API v1 response

Encoded **only** in `internal/fronius/solarapi/dto.go` (`[HYPOTHESIS]`):

- envelope `{"Body":{"Data":{…}},"Head":{…}}`;
- `Body.Data.Site`: `Mode`, `P_PV`, `P_Grid`, `P_Load`, `P_Akku`, `E_Day`,
  `E_Year`, `E_Total`, `rel_Autonomy`, `rel_SelfConsumption`,
  `Meter_Location_Current`;
- `Body.Data.Inverters`: object indexed by number ("1", "2", …) with `DT`,
  `P`, `E_Day`, `E_Year`, `E_Total`;
- `Head`: `RequestArguments`, `Status` (`Code`, `Reason`, `UserMessage`),
  `Timestamp` (RFC3339).

Normalization rules applied by `internal/model`:

- units kept as-is: powers in **W**, energies in **Wh**, ratios in **%**
  (conversion to kW/kWh only at display time, `units.go`);
- unusable `Timestamp` ⇒ collector timestamp, `Quality = estimated`;
- absent field ⇒ `nil`, **never** `0`; an explicit `0` from the device stays `0`;
- `P_Grid`, `P_Load`, `P_Akku`, `rel_*` stay `null` without a Smart Meter or
  battery: no consumption and no import/export is computed without a measurement.

Landmark provided for the future reconciliation: the official Fronius Solar API
v1 documentation, to be compared against firmware **1.19.7-1**. If the probe
reveals other names or other units, the fix applies to `dto.go`, the fixtures and
this file — the SQLite columns stay in base units.

## Other Solar API endpoints — unverified candidates

These names come from the **public Fronius Solar API v1 documentation**. None has
been re-read in `references/fronius-original.js` (which contains no Solar API
path) nor observed on firmware **1.19.7-1**: they are therefore all `[TO-TEST]`.
They are listed to frame phase 2, not to be called.

| Candidate endpoint | What it would provide | Status |
|---|---|---|
| `/solar_api/GetAPIVersion` | API version exposed by the device | `[TO-TEST]` |
| `/solar_api/v1/GetInverterRealtimeData.fcgi` | Per-inverter measurements (`CommonInverterData`, `3PInverterData`, `CumulationInverterData`): voltage, current, frequency, temperature, state | `[TO-TEST]` (redundant with PowerFlow for power) |
| `/solar_api/v1/GetInverterInfo.fcgi` | **Identity**: model, serial number, firmware per inverter | `[TO-TEST]` — direct candidate for the `device.identity` capability |
| `/solar_api/v1/GetLoggerInfo.fcgi` | Identity of the datalogger / the interface | `[TO-TEST]` |
| `/solar_api/v1/GetMeterRealtimeData.fcgi` | Measurements of the Fronius Smart Meter, if present | `[TO-TEST]` — would allow **detecting** a meter and unlocking `grid.import_export`, `load.consumption`, `autoconsumption.ratio` |
| `/solar_api/v1/GetStorageRealtimeData.fcgi` | State of charge and power of the battery, if present | `[TO-TEST]` — would unlock `battery.soc` |
| `/solar_api/v1/GetActiveDeviceInfo.fcgi` | List of the system's active devices | `[TO-TEST]` |
| `/solar_api/v1/GetDeviceInfo.fcgi` | Device inventory | `[TO-TEST]` |

Consequence for phase 1: detecting the presence of a Smart Meter or a battery is
**feasible without any write**, through simple GETs on these endpoints, but it is
not implemented for lack of proof of existence on the firmware. Until it is, the
corresponding capabilities remain unavailable with an explicit reason
(`internal/model/capabilities.go`).

## Fixtures

`test/fixtures/solarapi/*.json` carry a root field `_provenance` **not consumed**
by the client:

| File | `synthetic` | `confidence` | Origin |
|---|---|---|---|
| `powerflow_produce_only.json` | `true` | `HYPOTHESIS` | `cmd/fronius-sim`, seed 42, fixed instant |
| `powerflow_with_meter.json` | `true` | `HYPOTHESIS` | idem, with `--with-meter` |
| `powerflow_status7.json` | `true` | `HYPOTHESIS` | idem, `Head.Status.Code = 7` |
| `powerflow_invalid.json` | — | — | truncated body, deliberately unreadable |

The phase 2 probe writes one file per explored path, named after the path:
`/status/version` → `probe_status_version_<UTC>.json`, etc. The name cannot come
from the last segment alone ("readable" for both components).

Promotion of a fixture: a real capture produced by `fronius-probe --save` must
set `synthetic` to `false`, `confidence` to `VERIFIED (hardware)`, carry
`firmware`, `captured_at`, `http_status` and `digest_required`, contain no serial
number, and be announced in `CHANGELOG.md`. See `docs/PROBE-READONLY.md`.

## Reference hardware

`[SOURCE-PDF]` system export of 16 October 2026: WebUI `1.14.2-1`,
`GEN24/DEVICEGROUP/GEN24PRIMO 1.19.7-1`, serial number `33319328`, 3680 W,
Wi-Fi `192.0.2.10` / `255.255.254.0` / gateway `192.0.2.1`,
Ethernet `disconnected`, active features `Battery Operation` and
`Full Backup`.

Explicit limit: this export **contains no component inventory**. Neither the PDF
nor the bundle proves the presence or absence of a Fronius Smart Meter or a
battery. The PDF's "active" features describe declared software capabilities, not
the installed hardware.

## License

The application is distributed under the MIT license (Copyright © 2026 Annika & Remy).

`references/fronius-original.js` is a manufacturer artefact, kept for
documentation purposes as the primary source required by `AGENTS.md`. It **is
not** covered by the MIT license and cannot be redistributed as part of the
produced software. The fallback route, if the artefact must not be versioned, is
to add it to `.gitignore` and keep only its provenance here.
