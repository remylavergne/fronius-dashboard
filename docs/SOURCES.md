# Sources, provenance and confidence levels

This document keeps the register of **where each piece of information** used by
the application comes from, and with what level of proof. Any statement about the
hardware or about a protocol must be traceable to a line of this file.

## Confidence levels

| Label | Meaning |
|---|---|
| `[SOURCE-JS]` | Read directly in `references/fronius-original.js`. Proves the presence of client-side code, **not** a permission granted by the firmware. |
| `[SOURCE-PDF]` | Read in the manufacturer's system export (`fronius-information.pdf`, 16 October 2026). |
| `[DOC]` | Read in the manufacturer's specification **`42,0410,2012,EN 02115052025`** (`references/42,0410,2012.pdf`, 91 pages, Solar API V1); each `[DOC]` claim cites its section. |
| `[OBSERVED-CONVERSATION]` | Reported during the analysis, not replayed in this session. |
| `[HYPOTHESIS]` | Assumed shape or behaviour, encoded as such in the code. |
| `[TO-TEST]` | Easy to verify, but unverified: requires the hardware or the probe. |
| `[VERIFIED]` | Run and observed: in this repository (tests, simulator, reproducible command) or on the reference hardware through `cmd/fronius-probe` (then marked `(hardware)`). |

Standing rule: "found in the bundle" ≠ "allowed by the server". No `[SOURCE-JS]`
item may be presented as a firmware permission.

## Machine-readable catalogue

`internal/sources/catalog.go` is the **machine-readable form** of this register
and of `docs/ENDPOINTS.md`: one entry per known source, each carrying its
transport (`solar_api` / `internal_api`), state (`called` / `captured` /
`candidate` / `archived`), the field-level status of what it returns
(`displayed` / `stored` / `validated` / `ignored` / `unverified`, an `ignored` or
`unverified` field carrying its reason), the proofs (the register's own marker
and, for a captured source, the firmware that produced it) and the capability it
feeds or would unlock. It is served by `GET /api/v1/sources` (the "Data sources"
page).

`internal/sources/catalog_test.go` binds the catalogue to the registers: the
`called` set is exactly the code allowlist plus the Solar API paths the collector
reads, every
capability id exists (or is a candidate the catalogue introduces), every declared
fixture exists on disk, and the catalogue holds exactly the Solar API paths of
the candidate table below and every internal path of `docs/ENDPOINTS.md`. A path
cannot be added to one without the other.

## The recorded survey — per-firmware evidence

The catalogue states what the device can expose; it says nothing about what a
given firmware **does** answer. That observation is the **survey**:
`cmd/fronius-probe --survey` walks every catalogued path, one read-only `GET`
each, and records one outcome per path — its **class**, the HTTP status, the
bytes, whether a Digest header was needed (`digest_required`), and the **shape**
of the answer (JSON paths and types, never a value). The outcome is written to
the application's database, table `source_probe`, keyed by `(path, query)`, and
read back by `GET /api/v1/sources` (the "Data sources" page); it is the
per-firmware evidence behind the catalogue's states.

Classes (`ok`, `empty`, `not_json`, `auth_required`, `not_found`, `error`) keep
distinct facts distinct: `not_found` means this firmware does not serve the path
— a Solar API `404` may also mean a wrong suffix or, per §3 of the specification,
an API disabled in the WebUI (the answer then carries "Solar API disabled by
customer config"), which the internal paths distinguish because they answer;
`auth_required` means the path exists behind authentication, `empty` means it
exists and the component is not installed. A `404` or a `401` is a **result**,
not a failed run.

Two provenance rules:

- **A class is an observation of one firmware.** Each row carries the `firmware`
  version observed on the identity path and the instant (`probed_at_utc`). A
  `not_found` is true for the firmware that answered `404`, not for the model in
  general, and **a shape is only true for the firmware it was observed on**;
  re-survey after a firmware update (`docs/RUNBOOK.md`).
- **A shape is not a value.** The recorded inventory is paths and types only — a
  survey of an identity endpoint never becomes a second copy of the device's
  serial numbers. This is the same rule that governs a capture, minus the body.

The **first survey of the reference installation** was run on **2026-10-16**
(firmware `1.19.7-1`); its outcome and report path are recorded in § Hardware
captures. A **second survey** the same day, with the corrected URL list of the
specification and the `--delay 4s` of §2.6, is now the observed reality
(**14 `ok`, 11 `empty`, 2 `auth_required`, 1 `not_found`**); it is recorded in
§ Hardware captures and in the observed inventory of § Other Solar API endpoints.

## Vendor artefact

`references/fronius-original.js` — 184,378 lines, ≈ 8.6 MB. This is the
JavaScript bundle of the manufacturer's Web interface, kept **unmodified** as the
primary source of analysis, in accordance with `AGENTS.md`.

This file is a proprietary software artefact (Fronius International GmbH). It is
**explicitly excluded from the scope of the MIT license** of this repository
(see `LICENSE` and the License section at the end of the document).

`references/42,0410,2012.pdf` — the manufacturer's official specification,
**document number `42,0410,2012,EN 02115052025`**, 91 pages, "Fronius Solar API
V1". It is the **primary source for the Solar API URL list and for what each
request returns on GEN24/Tauro/Verto**, which `references/fronius-original.js`
cannot provide (the bundle contains no Solar API path). Served by
`GET /api/v1/sources` where the catalogue marks each Solar API entry `[DOC]`
with the section it comes from.

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
units, nor locations. The schema of `internal/fronius/solarapi/dto.go` therefore
rested on the public documentation of Solar API v1 and on reported observations
(`[HYPOTHESIS]`), which is the reason for the `cmd/fronius-probe` probe and
`docs/PROBE-READONLY.md`; the hardware capture of 2026-10-16 turned that
hypothesis into a `[VERIFIED]` shape (§ Shape of the Solar API v1 response).

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
| `/status/version` | `serialNumber` | yes | l. 45243, l. 127380, l. 143048 (`t.serialNumber`); captured 2026-10-16 (redacted by the probe) |
| `/status/version` | `numberOfPhases` | yes | l. 183034; **captured = `1`** (single-phase installation) |
| `/status/version` | `devicegroup` | yes | l. 183035 (`"P" === t.devicegroup[0] ? 1 : 3`); **captured = `"P6RW"`** |
| `/status/version` | `hwrevisions.FAMILYID` | yes | l. 143049; captured (`hwrevisions`, 3 entries — masked by hand) |
| `/status/version` | `devicename`, `articleNumber` | yes | l. 128813, 128842 — **read** by the official client and **used** by the application: persisted in `device.device_name` / `device.article_number` (migration `0005_device_name.sql`) and exposed by `/api/v1/system` as `device_name` / `article_number`. **Captured** on hardware: `devicename` `"Primo GEN24 3.6 Plus"`, `articleNumber` `"4,210,141,002"` |
| `/status/version` | `hardwareId` | yes | l. 126568 — **read** by the official client, **not used** by the application: no consumer names it (`docs/adr/0008-exposed-data-scope.md`) |
| `/status/version` | `swrevisions.GEN24` | yes | l. 128882, 135137 — **read by the application**: the software version the official client displays; nested key, read before the flat candidates (`firmwareOf`, `internal/fronius/internalapi/client.go`). **Captured `[VERIFIED]` = `1.19.7-1`**, matching the system export (§ Reference hardware) |
| `/status/version` | `firmwareVersion` | **no** | fallback candidate, used only if `swrevisions.GEN24` is absent; **not present** in the capture |
| `/status/version` | `modelName`, `model` | **no** | candidates: **no model key** is observed in the bundle **nor in the capture**. `devicegroup` is not a model fallback — it is the device group, a sibling field (l. 183035), and the code no longer substitutes it |
| `/components/PowerMeter/readable` | `channels` (23 `SMARTMETER_*` signals), `attributes.addr`, `attributes["[ENABLE]"]` | yes | l. 129011, 129015, 130976-130983; envelope `Cgi` (l. 75958, 183011); **detection** is always based on a non-empty `Body.Data`. **Captured 2026-10-16**: `Body.Data` empty ⇒ **absent**, `Content-Type: text/javascript` |
| `/components/BatteryManagementSystem/readable` | `channels` (10 signals) | yes | l. 128986; envelope `Cgi` (l. 75958). **Captured 2026-10-16**: `Body.Data` empty ⇒ **absent**, `Content-Type: text/javascript` |
| `/status/powerflow` | `site.E_Day`, `site.E_Year`, `site.E_Total`, `site.P_PV`, `site.P_Grid`, `site.P_Load`, `site.P_Akku`, `inverters[<id>].SOC`, `inverters[<id>].BatMode`, `Smartloads.Ohmpilots[<id>].P_AC_Total` | yes | l. 129034, 129043-129063 — read by the official client; **the application read only `E_Day`/`E_Year`**, briefly as a fallback (`docs/UNKNOWNS.md` #13) that has since been **removed**. **Captured `[VERIFIED]` (hardware) 2026-10-16**: the fixture `internalapi/status_powerflow.json` is promoted (`synthetic: false`, firmware `1.19.7-1`, `captured_at` `2026-10-16T14:47:07Z`); `site.E_Day` and `site.E_Year` are **null here too**, so no local source restores them on this installation (`docs/UNKNOWNS.md` #13) |
| `/status/activeEvents` | body **root array** (not a `Cgi` envelope) | yes | **captured 2026-10-16**: the response body is a root JSON array, one element per active event |
| `/status/activeEvents` | `severity` (integer: 1 error, 2 warning, 3 information) | yes | l. 135690 + l. 135728-135733 (`getEventLevel`); **used by the application** (`severityValue`, `internal/detect/detector.go`) — before the fix, the integer severity was not recognized and every hardware event was classified as "information". **Captured 2026-10-16: integer `3`** (`docs/UNKNOWNS.md` #11 confirmed) |
| `/status/activeEvents` | `uuid`, `prefix`-`eventID` (instance identity) | yes | l. 135693, 164699, 135742 (comparison), 135760 (`ConfirmEvent` receives `{uuid}`); **used by the application** as an anti-duplication key. Captured keys: `uuid` `"56b077da-0d11-45be-8b24-7b09d53ad558"`, `prefix` `"GEN24"`, `eventID` `1260` |
| `/status/activeEvents` | `label` (text displayed as a fallback by the client) | yes | l. 135654; **used by the application** as the event text. **Captured 2026-10-16**: `label` is **not prose** but the vendor's state-code name (`"LicensedFeatureDisabled"`), translated by the vendor dictionary (`STATECODETRANSLATIONS.StateCodes.<prefix>-<eventID>`, l. 164699, 164705; module `rqlu`) |
| `/status/activeEvents` | `timestamp`, `activeUntil` (device clock seconds) | yes | l. 135694-135695 — not used: `TS` remains the collector clock, the sort and retention axis |
| `/status/activeEvents` | `viewer`, `category`, `confirmable`, `sourceID` | yes | l. 135691, 135696, 135697, 135701 — read by the client, **not used** by the application: `Detail` keeps the raw JSON. **Captured**: `viewer` is an audience level (integer `1`); `subID`, `type`, `category` are additional keys the vendor client reads, **kept in `Detail`** |
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
| `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | none (captured) | `[VERIFIED]` (hardware, 2026-10-16): `200 OK`, no `Authorization`, schema captured (§ Shape of the Solar API v1 response); **absent from the bundle** |
| `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` | GET | none (captured) | `[VERIFIED]` (hardware, 2026-10-16, `200`, `883` bytes, no challenge): `DAY_ENERGY`/`YEAR_ENERGY` are **null** while `TOTAL_ENERGY` is live ⇒ **not** a source for the day and year counters (`docs/UNKNOWNS.md` #13); it is also the documented source of the inverter state (`Body.Data.DeviceStatus`) — **read on the common collection** and published by the device detail page, while this accumulation collection stays probe-only; **absent from the bundle**; called by `cmd/fronius-probe`, **not** by the application |
| `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CommonInverterData` | GET | none (captured) | **called**: `[VERIFIED]` (hardware, survey 2026-10-16, `200`, `1680` bytes, `digest_required: false`) — read by `internal/collector` **every collection cycle** (constant `solarapi.CommonQuery`); the thirteen measurements and `DeviceStatus` sit **flat** under `Body.Data`; the measurements and the state are published on the device detail page (`GET /api/v1/detail`). **No fixture is promoted** for this answer: it is read live, and its shape is pinned by the survey record, not by a versioned file |
| `/status/powerflow` | GET | internal | `[SOURCE-JS]` — polled every 5,000 ms by the official client (basis for `FRONIUS_POLL_INTERVAL`); **extracted but no longer called** by the application: it briefly served as the **fallback** for the day and year energies the Solar API returns as `null` (`docs/UNKNOWNS.md` #13), a fallback **removed** once the captures proved no local source exposes them. The simulator no longer serves it; it was **captured** `[VERIFIED]` (hardware, 2026-10-16, firmware `1.19.7-1`): `200`, `digest_required: false`, promoted as `internalapi/status_powerflow.json` — `inverters` is an **array** of `{CID, DT, E_Total, ID, P}` (the bundle could not tell, its client reading that member through `Object.keys`/`Object.values`), `site.MLoc` is the internal twin of the Solar API's `Meter_Location` (integer `255`, versus the string `"unknown"`), and `common`/`version` are **not read** by the application. **Decisive:** `site.E_Day` and `site.E_Year` are **null here too**, and the accumulation collection nulls the same counters, so **no local source** delivers them on this installation (`docs/UNKNOWNS.md` #13) |
| `/status/version` | GET | none (captured) | **called** (phase 2): `[SOURCE-JS]` l. 183050 (`yTJA` route, without a header — **this path is not in the `aYRN` registry**); `[VERIFIED]` on the simulator and **on hardware** (2026-10-16, `200 OK`, `digest_required: false`). Provides `serialNumber`, `numberOfPhases`, `devicegroup`, `swrevisions` |
| `/status/activeEvents` | GET | none (captured) | **called** (phase 2): `[SOURCE-JS]` l. 76030 (10,000 ms interval); `[VERIFIED]` on the simulator and **on hardware** (2026-10-16, `200 OK`, `digest_required: false`, root array). Fields read listed in `docs/ENDPOINTS.md` |
| `/components/PowerMeter/readable` | GET | none (captured) | **called** (phase 2): `[SOURCE-JS]` l. 76035 (envelope `Cgi`); `[VERIFIED]` on the simulator and **on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `Content-Type: text/javascript`, `Body.Data` empty ⇒ **no meter**). Presence deduced from `Body.Data` |
| `/components/BatteryManagementSystem/readable` | GET | none (captured) | **called** (phase 2): `[SOURCE-JS]` l. 76032; `[VERIFIED]` on the simulator and **on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `Content-Type: text/javascript`, `Body.Data` empty ⇒ **no battery**). Presence deduced from `Body.Data` |
| `/status/common` | GET | internal | **called** (device detail): `[SOURCE-JS]` l. 76041 (`aYRN` registry, envelope `Default`); `[VERIFIED]` on the simulator and **surveyed on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `349` bytes, `10` fields). Read by `internal/detect` on the detection cycle; the detection-cycle families are published by `GET /api/v1/detail` |
| `/status/network` | GET | internal | **called** (device detail): `[SOURCE-JS]` l. 76028 (`aYRN` registry, envelope `Default`); **surveyed on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `768` bytes, `38` fields). One entry per interface |
| `/status/devices` | GET | internal | **called** (device detail): `[SOURCE-JS]` l. 76027 (`aYRN` registry, envelope `Default`); **surveyed on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `65` bytes) — a bare JSON array (`type`, `status`, `statusMessage`) |
| `/status/emrs` | GET | internal | **called** (device detail): `[SOURCE-JS]` l. 76029 (`aYRN` registry, envelope `Default`); **surveyed on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `214` bytes, `8` fields) — four pins (`pin1`…`pin4`) |
| `/components/cache/readable` | GET | internal | **called** (device detail): `[SOURCE-JS]` l. 76033 (`aYRN` registry, envelope `Cgi`); **surveyed on hardware** (2026-10-16, `200 OK`, `digest_required: false`, `4 897` bytes, `99` values) — one node, ten attributes, 81 registers (44 named by the device with its own type suffix, 37 bare `<id>`). Read by `internal/detect` on the detection cycle, published as the `cache` family of `GET /api/v1/detail`; **no unit is derived from a name** and **no fixture is promoted** (§ Hardware captures) |
| `/config/meter`, `/config/batteries` | `GET` (l. 161422) | Digest "after 401" | **off-list** by scope choice: literal and verb **observed** (l. 161471 and 161462; module `rCKn`), the opposite claim having been corrected |
| `/config/*` (24 paths) | GET | Digest "Customer" | `[OBSERVED-CONVERSATION]` for a few (`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping`: 200; `/config/powerlimits`, `/config/setup/*`: 200 `{}`); phase 2. The captured `/status/*` and `/components/*` paths need **no** Digest (§ Hardware captures) |
| `/commands/GetUTCTime` | GET | Digest (called) | **called**: `[SOURCE-JS]` l. 76037-76040 (polled registry `aYRN`, interval 15,000 ms, envelope `CommandResult`, the **only** registry entry declaring `user: "customer"`) + l. 90579 (direct site); the field read is `resultData.epoch`, **in seconds** (consumers l. 128622, 128767). Read by `internal/detect` on its detection cycle: the drift against the host clock is published in `GET /api/v1/system` and shown on the System screen, warned beyond **±60 s**. **captured** `[VERIFIED]` (hardware) 2026-10-17, firmware 1.19.7-1: `200`, 51 bytes, `{"resultData":{"epoch":1792272194},"success":true}` — `epoch` in seconds, matching the probe's own clock to the second; `cmd/fronius-sim` serves it too (`-clock-offset` shifts it on purpose) |
| `/commands/GetServiceState?serviceId=21` | GET | Digest (called) | **called**: `[SOURCE-JS]` l. 76043 (polled registry `aYRN`, interval 5,000 ms, argument `serviceId` — the registry's only argument) + l. 90935/90939 (direct sites); the field read is `resultData.state` (consumer l. 136831), `serviceId` = `T_SystemServiceId.GridlimitsAutotest` = **21** (l. 116025); when the envelope's `success` is not true the manufacturer's own client renders `BLOCKED`. Read by `internal/detect` and shown on the System screen — **only the state**: the application never triggers the self-test. **on hardware 2026-10-17** the path exists and is Digest-protected: the probe was challenged, then refused `403` after presenting its account — the account lacks the role the self-test requires; the shape remains **`unverified`** |
| `/commands/*` | GET/POST | Digest depending on the command | `[SOURCE-JS]`; most are mutations — **out of scope**, never called. **The read paths are the exception**: the two the application calls are listed above (`/commands/GetUTCTime`, `/commands/GetServiceState?serviceId=21`), both `READ`; the clock is **verified** on hardware (2026-10-17), the service state is **permission-gated** (challenged, then `403`) |

What the hardware capture settled (2026-10-16, firmware `1.19.7-1`, § Hardware
captures):

- Real schema of the Solar API → **captured**: `200 OK`, no `Authorization`, exact
  key list (§ Shape of the Solar API v1 response).
- Authentication on firmware **1.19.7-1** → **none required**: all five paths
  allowlisted at that time answered `200` with no `Authorization` header and no `401`, so the Digest
  "after 401" machinery is never exercised by this device (`docs/UNKNOWNS.md` #2
  resolved). A sixth path, `/status/powerflow`, was added later and is
  **captured** (2026-10-16, `200`, no challenge); it has since been **removed**
  from the allowlist, as has its probe entry.
- Day and year energies → **no local source exposes them**, and the
  specification **documents why**: `/status/powerflow` and the accumulation
  collection (`GetInverterRealtimeData` with
  `DataCollection=CumulationInverterData`) both return the day and year counters
  as **null**, while their total stays live. §4.1.5 states it as a rule: on
  GEN24/Tauro/Verto `DAY_ENERGY` and `YEAR_ENERGY` "will always report null",
  and `TOTAL_ENERGY` is "supported since 1.14 (updated every **5 min**)" — so a
  day or year energy derived from the total cannot be fresher than five minutes
  (`docs/UNKNOWNS.md` #13). The internal fallback built on `/status/powerflow`
  was therefore **removed**.
- Real names of the **model** and the **firmware** in `/status/version` → **captured**:
  `swrevisions.GEN24` = `1.19.7-1` (the key read by the application), `numberOfPhases`
  = `1`, `devicegroup` = `P6RW`; **no model key exists**, so `Model` stays empty and
  `devicegroup` is no longer used as a substitute.
- Shape of `/status/activeEvents` → **captured**: the body is a **root array** (not a
  `Cgi` envelope), `severity` is an **integer**, `label` is a vendor state-code name
  (`docs/UNKNOWNS.md` #11 confirmed).
- Presence of a Smart Meter or a battery on the installation → **captured: neither is
  installed** (`Body.Data` empty on both `/components/*/readable`; `docs/UNKNOWNS.md`
  #4 and #5 resolved).

What remains open: the real signal names of a component that would actually be
present. The other Solar API endpoints are no longer open: the specification
fixed their URLs and the **second survey** (§ Other Solar API endpoints) answered
each of them on firmware `1.19.7-1` — `ok`, `empty`, or the archive's documented
`404`.

## Shape of the Solar API v1 response — captured `[VERIFIED]`

Captured on hardware on **2026-10-16**, firmware `1.19.7-1` (§ Hardware captures):
`GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` returned `200 OK`,
`Content-Type: application/json`, 1074 bytes. The shape is **no longer a
`[HYPOTHESIS]`**. Envelope `{"Body":{"Data":{…}},"Head":{…}}`; exact keys of the
capture:

- `Body.Data.Site`: `BackupMode` (bool), `BatteryStandby` (bool), `E_Day`
  (**null**), `E_Total` (float), `E_Year` (**null**), `Meter_Location` (string,
  `"unknown"`), `Mode` (`"produce-only"`), `P_Akku` (null), `P_Grid` (null),
  `P_Load` (null), `P_PV` (float), `rel_Autonomy` (null),
  `rel_SelfConsumption` (null);
- `Body.Data.Inverters."1"`: `DT` (1), `P` (float), `E_Day` (**null**),
  `E_Year` (**null**), `E_Total` (float);
- `Body.Data.Smartloads.Ohmpilots`: `{}`; `Body.Data.Version`: `"12"`;
- `Head.RequestArguments`: `{}`; `Head.Status`:
  `{Code: 0, Reason: "", UserMessage: ""}`; `Head.Timestamp`:
  `"2026-10-16T15:17:23+00:00"`.

Divergences from the retired `[HYPOTHESIS]` of
`internal/fronius/solarapi/dto.go`:

| Hypothesis | Captured reality | Treatment |
|---|---|---|
| `Meter_Location_Current` (int) | `Meter_Location` (string) | DTO field renamed and retyped; simulator, tests and fixtures aligned |
| `E_Day`, `E_Year` present as numbers | **null** on this firmware (site **and** inverter) | kept as `*float64` → absent, never `0`; the cause is an open point (`docs/UNKNOWNS.md` #13) |
| no `BackupMode` / `BatteryStandby` / `Smartloads` / `Version` | all four present | tolerated and ignored (no `DisallowUnknownFields`) |

Normalization rules applied by `internal/model` (unchanged):

- units kept as-is: powers in **W**, energies in **Wh**, ratios in **%**
  (conversion to kW/kWh only at display time, `units.go`);
- unusable `Timestamp` ⇒ collector timestamp, `Quality = estimated`;
- absent field ⇒ `nil`, **never** `0`; an explicit `0` from the device stays `0`;
- `P_Grid`, `P_Load`, `P_Akku`, `rel_*` stay `null` without a Smart Meter or
  battery: no consumption and no import/export is computed without a measurement
  (confirmed on hardware: no meter, no battery, § Hardware captures).

The vendor bundle contains **no Solar API path** (§ Verifications run on the
bundle): the manufacturer's interface proves nothing about this response, so the
schema above rests on the hardware capture, reconciled with the official Fronius
Solar API v1 documentation against firmware **1.19.7-1**. The SQLite columns stay
in base units.

## Derived quantities — day, year and daily energies

`[VERIFIED]` (hardware), firmware `1.19.7-1`, 2026-10-16: no local endpoint
publishes the day or year counter (`docs/UNKNOWNS.md` #13). The two quantities
are therefore **computed**, not measured, from our own stored total:

- **Source (measured):** the `energy_total_wh` of our collected samples — the
  live total the device does return (`E_Total` / `TOTAL_ENERGY`).
- **Derivation (a computation):** the difference between the current sample's
  total and the most recent stored sample **at or before the local boundary** —
  midnight for the day, 1 January for the year (store timezone
  `Europe/Brussels`). A baseline more than **10 minutes** before the boundary is
  refused: a collector outage must not turn older production into today's. The
  other refusals are a boundary with no sample at or before it (a fresh
  installation before its first local midnight, or before 1 January), a counter
  that went backwards since the boundary, and a current sample without a total;
  each carries its reason (`day_reason` / `year_reason`).
- **Confidence and provenance:** the derived value is `[VERIFIED]` as a run
  observation (tests and a runtime pass, `docs/UNKNOWNS.md` #13), but it rests on
  a computation over our own data — **never** labelled as a measurement. The
  measured fields `energy_day_wh` / `energy_year_wh` stay `null` on this firmware,
  and the interface marks the derived value **"estimated from the total"**
  (`kpi.energyDerived`).

Code: `internal/api/derived.go` (the rule, the 10-minute tolerance, the
refusals), `Store.TotalEnergyAt` in `internal/storage` (the baseline lookup),
`apps/web/src/lib/energy.ts` (`resolveEnergy`: measured wins, otherwise derived
and flagged), KPI wiring in `apps/web/src/routes/Overview.tsx`.

**The daily energy follows the same rule.** `/api/v1/energy/daily` carries, per
local day, the device's own counter `energy_wh` (`E_Day`, **always null** on this
firmware) and a `derived_wh` measured from the **same stored total**, with the
same provenance rule as the day KPI — a baseline within **10 minutes** of the
local midnight, an end reading within the same 10 minutes of the day's end, no
counter that went backwards — beside `derived_reason` (the cause when it could
not be established) and `partial` (the day in progress). Because the device
reports no `E_Day` here, the **derived value is the only source of a daily energy
on the reference installation**: the page marks it `derived` and shows the cause
beside a dash, never a zero. Code: `Store.DailyEnergyFromTotal`
(`internal/storage/daily_derived.go`), the endpoint in
`internal/api/server.go`, the page in `apps/web/src/routes/Energy.tsx`. The
daily chart is therefore no longer empty on this firmware — the aggregate column
`agg_hour.max_energy_day_wh` is still read for the device counter, but the
derived value comes from the stored total.

## The Solar API specification — what it settles

`references/42,0410,2012.pdf` (document number **`42,0410,2012,EN 02115052025`**,
91 pages) is the **primary source** for the Solar API surface. It settles, with
the section that says so:

| What | Section | Content |
|---|---|---|
| The URL list | §2.4.1, §4.1.3, §4.2.2, §4.3.2, §4.4.2, §4.5.2, §4.6.2, §4.7.2, §4.8.2, §4.9.4, §4.10.2, §4.11.3, §5 | **Only `GetPowerFlowRealtimeData` is `.fcgi`** (§4.11.3); the twelve other requests are `.cgi`. `GetAPIVersion` is the exception with no version segment: the `.cgi` belongs to the path (`/solar_api/GetAPIVersion.cgi`, §2.4.1). |
| Availability per request | §4.1.1, §4.2.1, §4.3.1, §4.4.1, §4.5.1, §4.6.1, §4.7.1, §4.8.1, §4.9.1, §4.10.1 | On GEN24/Tauro/Verto the realtime-data requests and the identity and inventory requests are available. Several are "available but returns an error" on this family (`GetLoggerInfo`, `GetSensorRealtimeData`, `GetLoggerLEDInfo`), and `GetStringRealtimeData` answers nothing because "String Control does not exist for GEN24/Tauro/Verto". |
| The null day and year counters | §4.1.5 | On GEN24/Tauro/Verto, `DAY_ENERGY` and `YEAR_ENERGY` "will always report null"; `TOTAL_ENERGY` is "supported since 1.14 (updated every 5 min)", so a day or year energy derived from the total cannot be fresher than **five minutes**. |
| The pacing | §2.6 | At most **two realtime requests in parallel**, and **4 s between two consecutive calls** — the reason `cmd/fronius-probe --survey` defaults to `--delay 4s`. |
| The disable behaviour | §3 | The Solar API can be **disabled** in the device's WebUI (Communication → Solar API); a disabled API answers **404** with "Solar API disabled by customer config". A page of Solar API `404`s therefore has three causes — a wrong suffix, a disabled API, a simulator — and the internal paths distinguish them (they answer). |
| The inverter state | §4.6.5 | The `StatusCode` table (0 Startup, 7 Running, 8 Standby, 10 Error, 11 Idle, 12 Ready, 13 Sleeping, 255 Unknown) plus `InverterState` as a string; the reference installation's `13 = Sleeping` matches it. |
| The arguments | §4.7.3, §4.8, §4.9, §4.10 | `GetActiveDeviceInfo` takes a `DeviceClass` (Inverter, Storage, Ohmpilot, SensorCard, StringControl, Meter, System); `GetMeterRealtimeData`, `GetStorageRealtimeData` and `GetOhmPilotRealtimeData` take `Scope`/`DeviceId`. |
| The archive | §5, §5.1.1 | GEN24/Tauro/Verto is **NEVER** supported for `GetArchiveData`: the device "does not provide access to history data". |
| Device types and events | §6.2, §6.3 | The inverter device-type list and the event table — available material, **not used yet**. |

The catalogue cites these sections per entry (`internal/sources/catalog.go`), and
each such entry carries a `[DOC]` proof naming the document number and the
section.

## Other Solar API endpoints — observed inventory (second survey)

These paths come from the specification (`42,0410,2012,EN 02115052025`), not from
`references/fronius-original.js`, which contains no Solar API path. The
specification settles the URL list — **only `GetPowerFlowRealtimeData` is
`.fcgi`**, the twelve others are `.cgi`. An earlier draft of this table probed
`.fcgi` on eight of them and read the resulting `404` as "this firmware does not
serve it": that was a **catalogue error, not a firmware fact** — the suffix was
missing from the probe. Those paths were corrected, the two the specification has
and the catalogue lacked were added (`GetLoggerLEDInfo.cgi`,
`GetOhmPilotRealtimeData.cgi`, capital `P`), and the phantom `GetDeviceInfo` —
which appears **nowhere** in the specification — was **removed**.

The status below is what the **second survey of the reference installation** (2026-10-16,
firmware `1.19.7-1`, corrected paths, `--delay 4s` as §2.6 prescribes) actually
answered; the availability column is what the specification states.

| Endpoint | What it provides | Availability in the specification | Second survey (2026-10-16, firmware 1.19.7-1) |
|---|---|---|---|
| `/solar_api/GetAPIVersion.cgi` | API version exposed by the device | §2.4.1: the path is `/solar_api/GetAPIVersion.cgi` (the `.cgi` belongs to the path, no version segment) | **`ok`**, 92 bytes, 3 fields (`APIVersion`, `BaseURL`, `CompatibilityRange`) — **`called`** now: read by `internal/detect` on the detection cycle and published as the `api_version` family (it is the one answer that is **not** enveloped); the earlier `404` was the probe's missing suffix, not an absent endpoint. Provenance: the specification §2.4.1 + the second survey of 2026-10-16 (firmware `1.19.7-1`) |
| `/solar_api/v1/GetInverterRealtimeData.cgi` | Per-inverter measurements (`CommonInverterData`, `3PInverterData`, `CumulationInverterData`) | §4.1.3 `.cgi`; §4.1.2 `CommonInverterData` and `CumulationInverterData` supported on GEN24, `3PInverterData` "Only on 3 phase inverters"; §4.1.5 the day and year counters always null, the total updated every 5 min | **`ok`**: `CommonInverterData` 1682 bytes / 36 fields (read by the collector every other cycle, `detailEveryNCycles = 2`), `CumulationInverterData` 883 / 18 (probe-only); `3PInverterData` **`empty`** (436 — the Primo is single-phase) |
| `/solar_api/v1/GetInverterInfo.cgi` | Identity: model, serial number, firmware per inverter | §4.6.2 `.cgi`; §4.6.1 ALWAYS on GEN24/Tauro/Verto; §4.6.5 the `StatusCode` table | **`ok`**, 537 bytes, 12 fields — **`called`** now: read by `internal/detect` on the detection cycle and **used** as the **identity fallback** (`docs/UNKNOWNS.md` #3). With no credential it fills `SerialNumber` from `UniqueID` and `DeviceName` from `CustomName` when `/status/version` established nothing, `Source = GetInverterInfo.cgi`; it never overwrites an authenticated identity and never invents the model or the firmware (published as the `inverter_info` family). Provenance: the specification §4.6 + the second survey of 2026-10-16 (firmware `1.19.7-1`) |
| `/solar_api/v1/GetLoggerInfo.cgi` | Identity of the datalogger / the interface | §4.4.2 `.cgi`; §4.4.1 ALWAYS but "available but returns an error" on GEN24/Tauro/Verto | **`empty`**, 288 bytes — consistent with the specification's own note |
| `/solar_api/v1/GetMeterRealtimeData.cgi` | Measurements of the Fronius Smart Meter, if present | §4.8.2 `.cgi`; §4.8.1 GEN24/Tauro/Verto "planned in 1.13"; §4.8 takes `Scope`/`DeviceId` | **`empty`**, 276 bytes — and no meter is installed (`Body.Data` empty on `/components/PowerMeter/readable`, § Hardware captures) |
| `/solar_api/v1/GetStorageRealtimeData.cgi` | State of charge and power of the battery, if present | §4.9.4 `.cgi`; §4.9.1 GEN24/Tauro/Verto since 1.13; §4.9 takes `Scope`/`DeviceId` | **`empty`**, 276 bytes — and no battery is installed (`docs/UNKNOWNS.md` #5) |
| `/solar_api/v1/GetActiveDeviceInfo.cgi` | Inventory of the active devices of the system (with serials) | §4.7.2 `.cgi`; §4.7.1 ALWAYS; §4.7.3 takes a `DeviceClass` | **`ok`**, 494 bytes, 7 fields — **`called`** now: read by `internal/detect` on the detection cycle with the frozen query `DeviceClass=System` and **used** as the **presence fallback** (`docs/UNKNOWNS.md` #4/#5). When the internal meter/battery readable path left the state `unknown`, a class reported with at least one device is `present`, a class reported empty or not mentioned is `absent`, `Source = GetActiveDeviceInfo.cgi` (published as the `active_devices` family). Provenance: the specification §4.7 + the second survey of 2026-10-16 (firmware `1.19.7-1`) |
| `/solar_api/v1/GetStringRealtimeData.cgi` | Per-string DC measurements (voltage, current, power) | §4.3.2 `.cgi`; §4.3.1 ALWAYS but "String Control does not exist for GEN24/Tauro/Verto" | **`empty`**, 296 bytes |
| `/solar_api/v1/GetSensorRealtimeData.cgi` | External sensor channels (irradiance, temperature) when a sensor card is fitted | §4.2.2 `.cgi`; §4.2.1 ALWAYS but "available but returns an error" | **`empty`**, 296 bytes — no sensor card |
| `/solar_api/v1/GetLoggerLEDInfo.cgi` | LED state and colour of the serving device | §4.5.2 `.cgi`; §4.5.1 ALWAYS but "available but returns an error" | **`empty`**, 291 bytes — added to the catalogue when the specification was read (the earlier list lacked it) |
| `/solar_api/v1/GetOhmPilotRealtimeData.cgi` | Ohmpilot state, energy, temperature and power | §4.10.2 `.cgi`, capital `P` in `GetOhmPilot`; §4.10.1 since 1.13; §4.10 takes `Scope`/`DeviceId` | **`empty`**, 276 bytes — no Ohmpilot is installed |
| `/solar_api/v1/GetArchiveData.cgi` | Historical time series held by the device itself | §5 `GetArchiveData.cgi?<query>`; §5.1.1 GEN24/Tauro/Verto **NEVER** — "does not provide access to history data" | **`not_found`** (404) — the one path the specification itself says this device never supports; the daily chart cannot be filled from device-side history (`docs/UNKNOWNS.md` #13) |

Outcome of the second survey (2026-10-16, firmware `1.19.7-1`, report
`probe-survey/survey_20261016T203255Z.json`): **14 `ok`, 11 `empty`,
2 `auth_required`, 1 `not_found`**. The first run, with the wrong `.fcgi` suffixes,
gave 11 `ok` / 4 `empty` / 2 `auth_required` / 10 `not_found`. **No catalogue
entry is left saying "this firmware does not serve it"**: eight of the ten earlier
`404`s now answer (three `ok`, five `empty`) once the suffix is correct, and the
archive is `404` by the specification's own rule, not by a firmware shortfall.

Consequence for phase 1: detecting the presence of a Smart Meter or a battery is
**feasible without any write**, through simple GETs. The internal
`/components/*/readable` paths are the precise source (they need credentials and
state the cause); since the Solar API-first route is implemented, they are
**corroborated** by the `GetActiveDeviceInfo.cgi` inventory, which needs no
credential and settles the presence when the authenticated read left it
`unknown` (`docs/UNKNOWNS.md` #4/#5). The Solar API
`empty` answers above confirm the same hardware fact (no meter, no battery, no
Ohmpilot / sensor card / StringControl on GEN24) rather than a firmware
limitation. The 2026-10-16 capture settled the question for the reference
installation: neither a meter nor a battery is present (§ Hardware captures).

## Fixtures

`test/fixtures/solarapi/*.json` carry a root field `_provenance` **not consumed**
by the client:

| File | `synthetic` | `confidence` | Origin |
|---|---|---|---|
| `solarapi/powerflow_produce_only.json` | `false` | `VERIFIED (hardware)` | **promoted 2026-10-16**: real capture (`fronius-probe`, firmware `1.19.7-1`) |
| `solarapi/powerflow_with_meter.json` | `true` | `HYPOTHESIS` | `cmd/fronius-sim`, `--with-meter` — the installation has **no meter**, nothing can promote it |
| `solarapi/powerflow_status7.json` | `true` | `HYPOTHESIS` | `cmd/fronius-sim`, `Head.Status.Code = 7` — no status-7 event on the installation |
| `solarapi/powerflow_invalid.json` | — | — | truncated body, deliberately unreadable |

Five internal captures were promoted on **2026-10-16** (firmware `1.19.7-1`,
redacted by `fronius-probe --save`):

| File | `_provenance` | Path |
|---|---|---|
| `internalapi/status_version.json` | yes | `/status/version` |
| `internalapi/status_activeevents.json` | **no** — the body is a root array, which cannot carry a sibling key, so **its provenance is recorded here** | `/status/activeEvents` |
| `internalapi/components_powermeter_readable.json` | yes | `/components/PowerMeter/readable` (empty `Body.Data`) |
| `internalapi/components_batterymanagementsystem_readable.json` | yes | `/components/BatteryManagementSystem/readable` (empty `Body.Data`) |
| `internalapi/status_powerflow.json` | yes | `/status/powerflow` — the recorded capture of a path that is **no longer called** (the fallback was removed) |

The **accumulation capture** is versioned as `solarapi/inverter_realtime_accumulation.json` (2026-10-16, `883` bytes, query `DataCollection=CumulationInverterData`): the application does not call the **accumulation** endpoint (the probe does; the application reads the **common** one), and the fixture is what pins its shape —
`internal/fronius/solarapi`'s contract test fails if the flat `Unit`/`Value` answer, the null `DAY_ENERGY`/`YEAR_ENERGY` or the `DeviceStatus` object ever drifts.

The internal fixture `internalapi/status_powerflow.json` is **promoted**
(`synthetic: false`, `confidence: VERIFIED (hardware)`, firmware `1.19.7-1`,
`captured_at` `2026-10-16T14:47:07Z`, `http_status: 200`,
`digest_required: false`): it replaces the hand-written fixture built from the
bundle extraction (`docs/ENDPOINTS.md`, l. 129034 and l. 129043-129063). The
captured body settles the shape — `inverters` is an **array** of
`{CID, DT, E_Total, ID, P}`, `site.MLoc` is the internal twin of the Solar API's
`Meter_Location` (integer `255`, versus the string `"unknown"`), and
`common`/`version` are not read by the application. **Decisive:** `site.E_Day`
and `site.E_Year` are **null here too**, and the accumulation collection nulls the
same counters, so **no local source** delivers them on this installation and the
path is **no longer called** — the fallback built on it was removed
(`docs/UNKNOWNS.md` #13).

Unit-level hardware identifiers (`hardwareId`, `commonName`, `hwrevisions`) were
**masked by hand** before versioning; the shape and the key names are untouched,
and `TestFixturesContainNoSensitiveData` now walks the whole `test/fixtures` tree
with those identifiers in its forbidden list. The `serialNumber` was redacted by
the probe itself.

The phase 2 probe writes one file per explored path, named after the path:
`/status/version` → `probe_status_version_<UTC>.json`, etc. The name cannot come
from the last segment alone ("readable" for both components), and a frozen query
is appended to it — two collections of one path answer different things:
`/solar_api/v1/GetInverterRealtimeData.cgi?…DataCollection=CommonInverterData` →
`probe_solar_api_v1_getinverterrealtimedata_scope_device_deviceid_1_datacollection_commoninverterdata_<UTC>.json`.

Promotion of a fixture: a real capture produced by `fronius-probe --save` must
set `synthetic` to `false`, `confidence` to `VERIFIED (hardware)`, carry
`firmware`, `captured_at`, `http_status` and `digest_required`, contain no serial
number, and be announced in `CHANGELOG.md`. See `docs/PROBE-READONLY.md`.

## Hardware captures

Two `cmd/fronius-probe` runs against the reference device
`http://192.0.2.10`, firmware `1.19.7-1`, on **2026-10-16**: one without
credentials, one with. Reports: `/tmp/probe-noauth/report.txt`,
`/tmp/probe-auth/report.txt` (not versioned; credentials come from
`FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` or `--password-stdin`, never
from the command line):

```
cmd/fronius-probe --base-url http://192.0.2.10 --allow-non-loopback
FRONIUS_DEVICE_USER=… FRONIUS_DEVICE_PASSWORD=… \
  cmd/fronius-probe --base-url http://192.0.2.10 --allow-non-loopback
```

Both runs returned `200 OK` on every allowlisted path, with **no** `Authorization`
header ever constructed and **no** `401` (`grep -c Authorization` = 0 in both
reports, so the Digest path was never exercised). `digest_required` is therefore
`false` for the whole allowlist on this firmware. The two runs differed only by
timestamps and live power values.

| Path | Status | `digest_required` | `Content-Type` | Bytes |
|---|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | `200` | `false` | `application/json` | 1074 |
| `/status/version` | `200` | `false` | `application/json` | 935 |
| `/status/activeEvents` | `200` | `false` | `application/json` | 232 |
| `/components/PowerMeter/readable` | `200` | `false` | `text/javascript` | 370 |
| `/components/BatteryManagementSystem/readable` | `200` | `false` | `text/javascript` | 383 |
| `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` | `200` | `false` | — | 883 |

A **third run** the same day —
`./bin/fronius-probe --base-url http://192.0.2.10 --allow-non-loopback
--endpoint /solar_api/v1/GetInverterRealtimeData.cgi --save /tmp/probe-real`,
with the password entered on standard input — captured the **accumulation
collection** (2026-10-16T17:00:38Z, `200`, `883` bytes, no challenge,
`Content-Type` not recorded). The redacted body is kept at
`/tmp/probe-real/probe_solar_api_v1_getinverterrealtimedata_20261016T170038Z.json`;
**no fixture is versioned for it** — the application does not call this
**accumulation** endpoint, so the facts live in this document. The body settles
it: `Body.Data.DAY_ENERGY` and
`Body.Data.YEAR_ENERGY` are **null** while `Body.Data.TOTAL_ENERGY` is live
(`15089627.579722222` Wh) and `PAC` is `0.0`. The same body carries what the flow
endpoints do not — `Body.Data.DeviceStatus` (`{"ErrorCode":1175,
"InverterState":"Sleeping","StatusCode":13}`): the endpoint is the **documented
source of the inverter state**, and it shows the inverter was **Sleeping** at 17:00
UTC, which explains the `0.0` powers while `TOTAL_ENERGY` stayed live. That state
is now **read** — on the **common** collection, published as the inverter family
of the device detail page — while this **accumulation** collection stays
probe-only.

Promoted into `test/fixtures` on **2026-10-16** (§ Fixtures). Consequences
recorded in `docs/UNKNOWNS.md`: #2 resolved (no authentication required), #4 and
#5 resolved (no Smart Meter, no battery), #11 confirmed (integer `severity`), #13
settled on the counters (`E_Day`/`E_Year` null on the Solar API, on the internal
`/status/powerflow` **and** on the accumulation collection, so **no local source**
exposes the day, year or daily counter and the internal fallback was **removed**,
see #13; the daily energy is now **derived** from the stored total).

### First survey of the reference installation (2026-10-16)

The **first real survey** was run by the user on **2026-10-16** against
`http://192.0.2.10`, firmware `1.19.7-1`, with **no credentials**:

```
./bin/fronius-probe --survey --base-url http://192.0.2.10 \
  --allow-non-loopback --db data/fronius.db --save ./probe-survey
```

27 catalogued paths, **one `GET` each, 1 s apart**, no credential given and no
`Authorization` header ever constructed. Outcome: **11 `ok`, 4 `empty`,
2 `auth_required`, 10 `not_found`**. Report:
`probe-survey/survey_20261016T191253Z.json` (in the working tree, **not
versioned**); the rows are recorded in `data/fronius.db` (table `source_probe`)
and read back by `GET /api/v1/sources`.

| Class | Paths (bytes, fields) |
|---|---|
| `ok` (11) | `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` (1044, 23) · `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` (883, 18) · the same with `DataCollection=CommonInverterData` (1680, 36) · `/status/version` (935, 29) · `/status/activeEvents` (443, 13) · `/status/powerflow` (372, 21) · `/status/common` (349, 10) · `/status/devices` (65, 4) · `/status/network` (768, 38) · `/status/emrs` (214, 8) · `/components/cache/readable` (4897, 99) |
| `empty` (4) | `/solar_api/v1/GetInverterRealtimeData.cgi?…DataCollection=3PInverterData` (436 — the Primo is single-phase, as the catalogue predicted) · `/components/PowerMeter/readable` (370) · `/components/BatteryManagementSystem/readable` (383) · `/components/Ohmpilot/readable` (368) |
| `auth_required` (2) | `/commands/GetUTCTime`, `/commands/GetServiceState?serviceId=<id>` — `401` without credentials |
| `not_found` (10) | `/solar_api/v1/GetInverterInfo.cgi`, `/solar_api/v1/GetLoggerInfo.fcgi`, `/solar_api/v1/GetMeterRealtimeData.fcgi`, `/solar_api/v1/GetStorageRealtimeData.fcgi`, `/solar_api/v1/GetActiveDeviceInfo.fcgi`, `/solar_api/v1/GetDeviceInfo.fcgi`, `/solar_api/v1/GetStringRealtimeData.fcgi`, `/solar_api/v1/GetSensorRealtimeData.fcgi`, `/solar_api/v1/GetArchiveData.cgi`, `/solar_api/GetAPIVersion` — `404`. **Corrected the same day (second survey, below):** eight of these were the probe's own wrong `.fcgi` suffix, not a firmware fact — the specification gives `.cgi` for the twelve requests other than `GetPowerFlowRealtimeData`, and the corrected paths answer. `/solar_api/GetDeviceInfo.fcgi` was a **phantom** (the name `GetDeviceInfo` appears nowhere in the specification) and was **removed** from the catalogue. `/solar_api/GetArchiveData.cgi` is `404` by the specification's own rule: §5.1.1 says GEN24/Tauro/Verto is **NEVER** supported for it |

Field-level facts observed in the report:

- **`CommonInverterData`** (36 fields) carries the Solar API `Unit`/`Value`
  measurement pairs **plus** `ErrorCode`, `InverterState`, `StatusCode`: the
  electrical detail and the device status — now **read by the application**
  every other collector cycle and published on the device detail page.
- **`/status/version`** (29) carries `apiversions` (commandsApi, configApi,
  setupAppApi, statusApi, updateApi), eleven `swrevisions` components (of which
  only `GEN24` is read), three `hwrevisions` keys, `minAppVersions`
  (Android/iOS/Win), `commonName`, `hardwareId`, `devicegroup`, `numberOfPhases`,
  `articleNumber`, `devicename`, `serialNumber` — exactly the family the
  catalogue marks as ignored or unverified.
- **`/status/common`** (10) carries `networkState.connectedViaAP`,
  `networkState.interfaces` (array, `[]` with `name`, `type`),
  `passwordState.customer` (bool), `passwordState.technician` (bool),
  `serviceState`, `supportEnabled`, `systemName`, `updateRunning`.
- **`/status/network`** (38) carries per-interface `configured`, `dns`,
  `gateway`, `ip`, `mask`, `interface_mode`, `ssid`, `status`, `wps_state`,
  plus `isFDMPConnected`.
- **`/status/devices`** (4) carries an array whose elements have `status`,
  `statusMessage`, `type`.
- **`/components/cache/readable`** (99) is the internal signal cache, **now read
  by the application** (the `cache` family of the device detail, rendered by the
  Cache block). One node (`393216`) with **ten attributes** — `[ENABLE]`,
  `[VISIBLE]`, `createTS`, `note`, `inverter.devicetype`,
  `inverter.devicetree`, `inverter.nodetype`, `inverter.pmc.0` / `1` / `2` —
  and **81 registers**: **44 named by the device** with its own type suffix
  (`_F32` ×27, `_U64` ×11, `_U16` ×6 — e.g. `PV_VOLTAGE_MEAN_01_F32`,
  `DCLINK_VOLTAGE_MEAN_F32`, `ISO_RESISTANCE_MEAN_F32`,
  `MODULE_TEMPERATURE_MEAN_01_F32`, `FANCONTROL_PERCENT_01_F32`,
  `ACBRIDGE_ENERGYACTIVE_PRODUCED_SUM_01_U64`) and **37 bare `<id>`
  identifiers** (e.g. `<1090584584>`), which the device leaves unnamed. The
  extraction keeps the device's vocabulary: the key verbatim, the name and the
  type suffix split out as `name`/`kind`, the value as the device wrote it (a
  64-bit counter is not rounded through a float64), and the bare ids keep their
  `<id>` form with no invented name. **No unit is derived from a name**: the
  device states a name and a type suffix, not a unit, and a guessed unit would
  be a fabricated measurement. **No fixture is promoted** for this answer: it is
  read live by `internal/detect` on the detection cycle, and its shape is pinned
  by the survey record, not by a versioned file.

What the run settles (**corrected the same day — see the second survey below**;
points 2 and 5 are **retired**):

1. **`GetArchiveData.cgi` is `404`** on firmware 1.19.7-1: the daily chart
   **cannot** be filled from device-side history. The aggregate/derived route is
   the only one (`docs/UNKNOWNS.md` #13, `CHANGELOG.md`). The specification makes
   this a rule, not an incident: §5.1.1 says GEN24/Tauro/Verto is **NEVER**
   supported for it.
2. ~~**`GetInverterInfo.fcgi` and `GetLoggerInfo.fcgi` are `404`**: the identity
   must stay on the Digest-protected internal API; the "identity from the Solar
   API" idea is closed.~~ **Retired.** The first run probed the wrong suffix.
   With the specification's `.cgi`, `GetInverterInfo.cgi` answers `ok` (537
   bytes, 12 fields) and `GetLoggerInfo.cgi` answers `empty` (288 bytes): the
   identity **is** available from the Solar API, without credentials — as a
   **second source** beside `/status/version`, not a replacement
   (`docs/UNKNOWNS.md` #3). It is now **implemented**: `internal/detect` reads
   `GetInverterInfo.cgi` and uses it as the identity fallback (Step 25,
   `docs/adr/0003`, `docs/ARCHITECTURE.md`).
3. **`CommonInverterData` and `CumulationInverterData` are served**: the
   electrical detail and the inverter `DeviceStatus`
   (`InverterState`/`StatusCode`/`ErrorCode`) are available on this device. The
   **common** collection is now **read by the application** — its measurements
   and state are the inverter family of the device detail page — while the
   accumulation collection stays **probe-only**.
4. **`3PInverterData` is empty** and the meter, battery and Ohmpilot readables
   are empty: consistent with the installed hardware (single-phase Primo, no
   meter, no battery, no Ohmpilot). The specification agrees: §4.1.2 marks
   `3PInverterData` "Only on 3 phase inverters".
5. ~~**`/solar_api/GetAPIVersion` is `404`**: the documented path may need a
   suffix (`.cgi`); the answer does not say.~~ **Retired.** §2.4.1 gives
   `/solar_api/GetAPIVersion.cgi` (the `.cgi` belongs to the path); re-probed, it
   answers `ok` (92 bytes, 3 fields: `APIVersion`, `BaseURL`,
   `CompatibilityRange`).
6. **The Solar API needs no credentials** on this firmware: the 11 `ok` and 4
   `empty` paths — Solar API and internal alike — answered to the **bare**
   request (`digest_required: false`), and the only `401`s were the two
   `/commands/*` paths.

### Second survey of the reference installation (2026-10-16)

The second survey corrected the URL list and the pacing, and is now the observed
reality:

```
./bin/fronius-probe --survey --base-url http://192.0.2.10 \
  --allow-non-loopback --delay 4s --db data/fronius.db --save ./probe-survey
```

28 catalogued paths, **one `GET` each, 4 s apart** (§2.6: at most two realtime
requests in parallel, four seconds between two consecutive calls). Report:
`probe-survey/survey_20261016T203255Z.json` (in the working tree, **not
versioned**); rows in `data/fronius.db` (table `source_probe`), read back by
`GET /api/v1/sources`. Outcome: **14 `ok`, 11 `empty`, 2 `auth_required`,
1 `not_found`** (against 11 `ok` / 4 `empty` / 2 `auth_required` / 10 `not_found`
for the first run).

| Class | Paths (bytes, fields) |
|---|---|
| `ok` (14) | `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` (1044, 23) · `/solar_api/v1/GetInverterRealtimeData.cgi?…DataCollection=CumulationInverterData` (883, 18) · the same with `DataCollection=CommonInverterData` (1682, 36) · `/solar_api/v1/GetInverterInfo.cgi` (537, 12) · `/solar_api/v1/GetActiveDeviceInfo.cgi` (494, 7) · `/solar_api/GetAPIVersion.cgi` (92, 3) · `/status/version` · `/status/activeEvents` · `/status/powerflow` · `/status/common` · `/status/devices` · `/status/network` · `/status/emrs` · `/components/cache/readable` |
| `empty` (11) | `/solar_api/v1/GetLoggerInfo.cgi` (288) · `/solar_api/v1/GetMeterRealtimeData.cgi` (276) · `/solar_api/v1/GetStorageRealtimeData.cgi` (276) · `/solar_api/v1/GetStringRealtimeData.cgi` (296) · `/solar_api/v1/GetSensorRealtimeData.cgi` (296) · `/solar_api/v1/GetLoggerLEDInfo.cgi` (291) · `/solar_api/v1/GetOhmPilotRealtimeData.cgi` (276) · `/solar_api/v1/GetInverterRealtimeData.cgi?…DataCollection=3PInverterData` (436 — the Primo is single-phase) · `/components/PowerMeter/readable` · `/components/BatteryManagementSystem/readable` · `/components/Ohmpilot/readable` |
| `auth_required` (2) | `/commands/GetUTCTime`, `/commands/GetServiceState?serviceId=<id>` — `401` without credentials |
| `not_found` (1) | `/solar_api/v1/GetArchiveData.cgi` (404) — the **only** one, and the specification says why: §5.1.1, GEN24/Tauro/Verto is **NEVER** supported for it |

The three Solar API paths that were `not_found` and now answer `ok` — identity
(`GetInverterInfo.cgi`), live device inventory (`GetActiveDeviceInfo.cgi`) and API
version (`GetAPIVersion.cgi`) — all answered to the **bare** request: no
credentials are needed (`docs/UNKNOWNS.md` #3), and they are now **called** by
`internal/detect` (the Solar API-first route, Step 25): the first two fill the
identity and the presence the internal API left undetermined, the third is
published as the `api_version` family. The five that now answer `empty`
are each consistent with the specification's availability note and with the
hardware (no meter, no battery, no sensor card, no Ohmpilot, no StringControl on
GEN24). The internal paths are unchanged: all answer `ok` except the two component
readables (`empty`) and the two commands (`auth_required`).

## Reference hardware

`[SOURCE-PDF]` system export of 16 October 2026: WebUI `1.14.2-1`,
`GEN24/DEVICEGROUP/GEN24PRIMO 1.19.7-1`, serial number `33319328`, 3680 W,
Wi-Fi `192.0.2.10` / `255.255.254.0` / gateway `192.0.2.1`,
Ethernet `disconnected`, active features `Battery Operation` and
`Full Backup`.

Explicit limit: this export **contains no component inventory**. Neither the PDF
nor the bundle proves the presence or absence of a Fronius Smart Meter or a
battery. The PDF's "active" features describe declared software capabilities, not
the installed hardware. The **hardware capture** of 2026-10-16 does prove their
absence (§ Hardware captures).

## License

The application is distributed under the MIT license (Copyright © 2026 Annika & Remy).

`references/fronius-original.js` is a manufacturer artefact, kept for
documentation purposes as the primary source required by `AGENTS.md`. It **is
not** covered by the MIT license and cannot be redistributed as part of the
produced software. The fallback route, if the artefact must not be versioned, is
to add it to `.gitignore` and keep only its provenance here.
