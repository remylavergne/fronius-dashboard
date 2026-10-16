# Endpoint classification — READ / MUTATION / DANGEROUS

**Static** inventory from `03-API-INVENTORY.md` (literal paths present in
`references/fronius-original.js`) completed by reading the modules
`i3oe`, `JWhm`, `mgOI`, `nuFz`.

## Method and limits

- The classification describes **the intent of the path and its use in the bundle**.
  It is **never** a permission granted by the firmware: no role
  matrix is proven at this stage (see `docs/UNKNOWNS.md`).
- `READ`: no expected persistent effect on the device.
- `MUTATION`: modifies a state, a session, a file or a preference.
- `DANGEROUS`: may cut production, modify the electrical grid
  protection, the firmware or the accesses.
- Anything that is not `READ` is **out of scope** and is never called by
  the application, the probe or the tests.

The **field-level view** of these paths — which fields are displayed, stored,
validated, ignored or unverified, each with its reason, plus the capability a
source feeds or would unlock — lives in the machine-readable catalogue
`internal/sources/catalog.go`, served by `GET /api/v1/sources`.
`internal/sources/catalog_test.go` keeps that catalogue in step with this
document: every internal path it holds must appear here.

## Executable allowlist

Eleven entries, and no others: this is the table frozen in
`internal/fronius/internalapi/allowlist.go` (nine internal paths) and in the
Solar API client (two paths). `cmd/fronius-probe` accepts nothing beyond its own
list, **derived from the source catalogue** (`--list`, `--endpoint`).

| Path | Method | Classification | Envelope | Evidence |
|---|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET | READ | — | `[OBSERVED-CONVERSATION]` "responds"; **absent from the bundle**; GET method imposed by `internal/fronius/readonly.go`; **captured** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false` |
| `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CommonInverterData` | GET | READ | — (flat `Body.Data`) | **absent from the bundle**; query is a binary constant (`solarapi.CommonQuery`); read **every collector cycle** by `internal/collector`; **captured/surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false`, `1680` bytes, `36` fields — the thirteen measurements and `DeviceStatus` sit **flat** under `Body.Data` (the documented wrapper object does **not** appear) |
| `/status/version` | GET | READ | `Default` | `[SOURCE-JS]` l. 183050 (path **without** header, module `yTJA`); **absent from the `aYRN` registry** (see the note below); `[VERIFIED]` on the simulator; **captured** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false` |
| `/status/activeEvents` | GET | READ | `Default` | `[SOURCE-JS]` l. 76030 (registry, interval 10 000 ms) + l. 183060 (variant `?sync=true`, without header); `[VERIFIED]` on the simulator; **captured** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false`. Fields read: see the extraction table |
| `/components/PowerMeter/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76035 + l. 75958 (`aYRN` registry, only path); `[VERIFIED]` on the simulator; **captured** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false`, `Content-Type: text/javascript` |
| `/components/BatteryManagementSystem/readable` | GET | READ | `Cgi` (`Body.Data`) | `[SOURCE-JS]` l. 76032 + l. 75958 (`aYRN` registry, only path); `[VERIFIED]` on the simulator; **captured** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false`, `Content-Type: text/javascript` |
| `/status/common` | GET | READ | `Default` (bare body) | `[SOURCE-JS]` l. 76041 (`aYRN` registry, interval 5 000 ms, `Default` envelope); read by `internal/detect`; **surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `349` bytes, `10` fields (`systemName`, `serviceState`, `updateRunning`, `supportEnabled`, `networkState.connectedViaAP`, `networkState.interfaces[]`, `passwordState.customer`, `passwordState.technician`) |
| `/status/network` | GET | READ | `Default` (bare body) | `[SOURCE-JS]` l. 76028 (`aYRN` registry, `Default` envelope); read by `internal/detect`; **surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `768` bytes, `38` fields — per-interface `ip.{ip,mask,gateway,dns[],configured}` and `network.{interface_mode,status,ssid,wps_state}`, plus `isFDMPConnected` |
| `/status/devices` | GET | READ | `Default` (bare body) | `[SOURCE-JS]` l. 76027 (`aYRN` registry, `Default` envelope); read by `internal/detect`; **surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `65` bytes — a bare JSON array whose entries carry `type`, `status`, `statusMessage` |
| `/status/emrs` | GET | READ | `Default` (bare body) | `[SOURCE-JS]` l. 76029 (`aYRN` registry, `Default` envelope); read by `internal/detect`; **surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `214` bytes, `8` fields — four pins (`pin1`…`pin4`), each with `state` and its reason |
| `/components/cache/readable` | GET | READ | `Cgi` (`Body.Data`, keyed by node id) | `[SOURCE-JS]` l. 76033 + l. 75958 (`aYRN` registry, `Cgi` envelope); read by `internal/detect`; **surveyed** `[VERIFIED]` (hardware) 2026-10-16, firmware 1.19.7-1: `200` without `Authorization`, `digest_required: false`, `4 897` bytes, `99` values — one node (`393216`), ten attributes, 81 registers (44 named by the device with its own type suffix, 37 bare `<id>`). Published as the `cache` family of `GET /api/v1/detail` and rendered by the Device detail page's **Cache block**; **no unit is derived from a name** |

Note: in the official client, **two paths** exist — with an `Authorization`
header (module `aYRN`, polled registry) and without (module `yTJA`, status
service) — but they do not overlap: `/components/PowerMeter/readable` and
`/components/BatteryManagementSystem/readable` only appear in `aYRN`,
while `/status/version` only appears in `yTJA` (verified by searching the
literal in the bundle). The `READ` classification is about the effect, not
the authentication: none of the ten has an observed persistent effect.
On the reference device, however, **neither route challenges**: on firmware
1.19.7-1 (captured 2026-10-16) all five paths of the allowlist at that time
answered `200` without an
`Authorization` header, so the with/without-header distinction is a bundle fact
only and is not observable on this installation. A sixth entry,
`/status/powerflow`, briefly joined the allowlist as a **fallback** for the day
and year energies (unknown #13) and was captured (2026-10-16, `200`, no challenge):
the device answers with `inverters` as an array, and the day and year energies are
the only members the application reads. Decisively, those two energies are **null
here too**, and the documented accumulation collection is **null on the same
counters** while its total stays live — so **no local source exposes them** and the
entry is **no longer called**: it was removed and `/status/powerflow` is now
documented here as **extracted but not called** (unknown #13).

The application then gained **six readings**. The four internal **diagnostic
families** — `/status/common`, `/status/network`, `/status/devices`,
`/status/emrs` — took the internal allowlist from four to **eight paths**; they
are read by `internal/detect` on the detection cycle (`FRONIUS_DETECT_INTERVAL`).
The internal **cache** (`/components/cache/readable`) took it from eight to
**nine paths**: read by the same detector on the same cycle (a `4 897`-byte
register dump per `5 s` collection cycle would be waste), published as the sixth
family of `GET /api/v1/detail` and rendered by the Device detail page's Cache
block. The Solar API **common** inverter collection is read by
`internal/collector` on the collection cycle. All six come from the first survey
of the reference installation (2026-10-16, firmware `1.19.7-1`), which is their
evidence; each addition is a documented decision (see the rows above). On the
cache the device names 44 of its 81 registers and leaves 37 bare, and **no unit
is derived from a name**: the device states a name and a type suffix, not a unit.

## Paths explicitly set aside

| Path | Reason |
|---|---|
| `/config/meter`, `/config/batteries` | Plausible and **now established** read: `GET` by `loadData()` (l. 161422), literals at l. 161471 and 161462, write by `saveData()` in `POST` (l. 161428). The earlier justification — "literal and verb outside the readable area of the bundle (module `rCKn`)" — was false and is withdrawn. These two paths remain **outside** the application by scope choice, not for lack of contract: the presence of the meter and of the battery is deduced from `Body.Data` of the `Cgi` paths, without reading these configurations. |
| `/status/events`, `/components/DataManager/readable` | Inventoried and readable (method, arguments and fields are now extracted in the table below), but outside the phase 2 need: not calling what you do not need reduces the surface. `/status/devices` and `/status/common` left this table when the survey of 2026-10-16 observed them and the application began reading them (see the allowlist above). |

## `/commands/` (35 paths)

| Path | Classification | Reason |
|---|---|---|
| `/commands/ActivatePasswords` | MUTATION | Activates passwords |
| `/commands/ConfirmEvent` | MUTATION | Acknowledges an event |
| `/commands/ConnectedPowermeter` | READ | Queries the connected meter |
| `/commands/CreateSDPFile` | MUTATION | Creates a service file |
| `/commands/DCInjectionTest` | DANGEROUS | DC injection test on the installation |
| `/commands/GetAutoTestReport` | READ | Reads a report |
| `/commands/GetSDPFile` | READ | Reads a service file |
| `/commands/GetServiceState` | READ | Reads the service state |
| `/commands/GetUTCTime` | READ | Reads the device time |
| `/commands/GetUpdateStatus` | READ | Reads the update state |
| `/commands/GridcodeSelection` | DANGEROUS | Selects the grid code / the protection |
| `/commands/InverterControl` | DANGEROUS | Controls the inverter |
| `/commands/InverterRollback` | DANGEROUS | Rolls back the inverter firmware |
| `/commands/InverterRollbackStatus` | READ | Reads the rollback state |
| `/commands/License` | UNDETERMINED | Semantics not established ⇒ treated as a write |
| `/commands/LicenseExchange` | MUTATION | License exchange |
| `/commands/LicenseExchangeData` | UNDETERMINED | Same |
| `/commands/ListGridcodes` | READ | Lists the grid codes |
| `/commands/ListOhmpilots` | READ | Lists the Ohmpilots |
| `/commands/Login` | MUTATION | Opens a session (`GET /commands/Login?user=<username>`) |
| `/commands/Logout` | MUTATION | Closes the session |
| `/commands/PasswordRecovery` | DANGEROUS | Password recovery procedure |
| `/commands/ReInitInterface` | DANGEROUS | Reinitializes the interface |
| `/commands/RemoteAccess` | DANGEROUS | Enables remote access |
| `/commands/ScanAvailableWlan` | MUTATION | Triggers a radio scan |
| `/commands/ServiceFile` | UNDETERMINED | Same |
| `/commands/SetPassword` | MUTATION | Sets a password |
| `/commands/SetUTCTime` | MUTATION | Sets the clock |
| `/commands/SetWPSActive` | MUTATION | Enables WPS |
| `/commands/StandbyRequestState` | MUTATION | Requests a standby state |
| `/commands/StandbyState` | READ | Reads the standby state |
| `/commands/StartSystemService` | DANGEROUS | Starts a system service |
| `/commands/SupportAccount` | DANGEROUS | Enables / terminates a support account (`resetPassword: true`) |
| `/commands/VerifyVCode` | MUTATION | Verifies a code |
| `/commands/factoryReset` | DANGEROUS | Factory reset |

## `/config/` (24 paths)

All these paths are **configuration resources**. Reading in GET was
observed with a "Customer" Digest for four of them
(`/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping`:
HTTP 200) `[OBSERVED-CONVERSATION]`; any write is a `MUTATION` at
minimum, and `DANGEROUS` as soon as it touches the electrical grid, the power
limits or the control interfaces.

| Path | Read classification | Write classification |
|---|---|---|
| `/config/batteries` | READ | MUTATION |
| `/config/common` | READ `[OBSERVED-CONVERSATION]` 200 | MUTATION |
| `/config/emrs` | READ | MUTATION |
| `/config/gridcode` | READ | **DANGEROUS** |
| `/config/ics` | READ | MUTATION |
| `/config/iomapping` | READ `[OBSERVED-CONVERSATION]` 200 | MUTATION |
| `/config/logging` | READ | MUTATION |
| `/config/meter` | READ `[OBSERVED-CONVERSATION]` 200 | MUTATION |
| `/config/modbus` | READ `[OBSERVED-CONVERSATION]` 200 | **DANGEROUS** (exposes a control interface) |
| `/config/multinetwork` | READ | MUTATION |
| `/config/ohmpilot` | READ | MUTATION |
| `/config/pcc` | READ | **DANGEROUS** (grid connection point) |
| `/config/policy` | READ | MUTATION |
| `/config/powerlimits` | READ `[OBSERVED-CONVERSATION]` 200 `{}` | **DANGEROUS** (power limits) |
| `/config/pushservice` | READ | MUTATION |
| `/config/setup/gridcode/general` | READ `[OBSERVED-CONVERSATION]` 200 `{}` | **DANGEROUS** |
| `/config/setup/gridcode/gridfeatures` | READ | **DANGEROUS** |
| `/config/setup/gridcode/interfaceprotection` | READ | **DANGEROUS** |
| `/config/setup/gridcode/safety` | READ | **DANGEROUS** |
| `/config/setup/powerunit` | READ `[OBSERVED-CONVERSATION]` 200 `{}` | **DANGEROUS** |
| `/config/solar_api` | READ | MUTATION |
| `/config/solarweb` | READ | MUTATION |
| `/config/timeofuse` | READ | MUTATION |
| `/config/wizard` | READ | MUTATION |

## Other paths (20)

| Path | Classification | Reason |
|---|---|---|
| `/components/BatteryManagementSystem/readable` | READ | **called**: presence of a battery deduced from `Body.Data` |
| `/components/DataManager/readable` | READ | State read |
| `/components/Ohmpilot/readable` | READ | State read |
| `/components/PowerMeter/readable` | READ | **called**: presence of a Smart Meter deduced from `Body.Data` |
| `/components/cache/readable` | READ | **called**: the internal register set of the device, keyed by node id (device detail, `cache` family) |
| `/status/activeEvents` | READ | **called**: active events of the device |
| `/status/capabilities` | READ | Capability read |
| `/status/common` | READ | **called**: device-wide state of the device detail (`systemName`, `serviceState`, `updateRunning`, `supportEnabled`, declared account kinds, interfaces) |
| `/status/deviceDB` | READ | Device database read |
| `/status/devices` | READ | **called**: inventory of the devices the device knows (device detail) |
| `/status/emrs` | READ | **called**: EMRS pin states (device detail) |
| `/status/events` | READ | Event read |
| `/status/ioServices` | READ | State read |
| `/status/license` | READ | License read |
| `/status/license/features` | READ | Active features read |
| `/status/network` | READ | **called**: network interfaces, addressing and radio state (device detail) |
| `/status/powerflow` | READ | Queried every 5 000 ms by the official client `[SOURCE-JS]`; **extracted but not called** by the application — it briefly served as the **fallback** for the day and year energies the Solar API returns as `null` (unknown #13), and that fallback was removed once the captures proved no local source exposes those counters |
| `/status/powerflowcontroller` | READ | State read |
| `/status/registrationData` | READ | State read |
| `/status/version` | READ | **called**: identity (`serialNumber`, `numberOfPhases`, `devicegroup`) |
| `/update/start` | **DANGEROUS** | Triggers a firmware update |

## Method, arguments and DTO per path (bundle extraction)

`[SOURCE-JS]` extraction done path by path in
`references/fronius-original.js` (184 378 lines): it replaces the group
cells of the matrix below for the `READ` paths. Announced counts, each
reproducible by a bundle command:

- **54 paths** covered, i.e. **56 lines** (the `?sync=true` variants are
  listed separately): 17 under `/status/`, 5 under `/components/`, 10 under
  `/commands/`, 24 under `/config/`;
- **46 call lines** to the four client envelopes, outside the four
  `JWhm` definitions (l. 46782, 46787, 46791, 46796) and the three
  `_refreshValue` lines (l. 75958, 75960, 75963) — of which **two are internal
  delegations** to `JWhm` (l. 46784, 46793) ⇒ **44 real call sites**;
- **7 raw HTTP calls** outside `JWhm`: six target device paths
  (SDP/Service/License downloads), the seventh (l. 54377) is the Angular
  Material icon loader, **out of scope**;
- **10 `[NOT OBSERVED]`** (no payload consumer found) and **7
  `schema`** (consumer present, but no named field read: the payload
  is passed as-is to a form builder or to a mapper).

Rules applied:

- **Method** carried by the envelope called: `callGet$` /
  `callGetWithoutCatch$` ⇒ `GET` + Digest (l. 46782-46790); `callPost$` /
  `callPostWithoutCatch$` ⇒ `POST` + Digest (l. 46791-46799); `_get$()` of the module
  `yTJA` (l. 183019-183021) and the file downloads ⇒ `GET` **without**
  header, with for the latter a single manual retry on 401 and
  `responseType: "blob"` (l. 90876-90886).
- **Envelope**: `Default` = raw body; `Cgi` = `Body.Data` (l. 75958);
  `CommandResult` = `resultData` (l. 75960); same rules in `yTJA`
  (l. 183010-183016).
- **Arguments**: the query variants (`?sync=true`, `?status=true`,
  `?doScan=`, `?componentId=`, `?serviceId=`) are **arguments of the same
  path**, not new paths. No observed path was missing from the
  classification: nothing had to be added there.
- **DTO**: only the fields **actually read** by a consumer are recorded.
  `[NOT OBSERVED]` means "no payload consumer found", never
  "empty payload"; no shape is presumed.
- **Declared role**: client display condition, **never** a server
  permission (see `docs/UNKNOWNS.md` #6). "none" = the client adds no role
  option and takes the request group's account (l. 46807-46813).

### `/status/*`: polled registry `aYRN` and status service `yTJA`

The entries of the polled registry `aYRN` (l. 76027-76045) are read by `callGet$`
(l. 75963) ⇒ `GET` + Digest; the 2nd argument (`5e3`, `1e4`, `15e3`) is
the refresh interval in ms. The entries of the status service `yTJA` are
read by `httpClient.get(this.url)` (l. 183020) ⇒ `GET` **without** header.

| Path | Method | Arguments | Envelope | DTO (fields read) | Declared role | Evidence (lines) |
|---|---|---|---|---|---|---|
| `/status/devices` | `GET` | none | `Default` | array of objects: `type`, `id`, `model`, `serial`, `name`, `additionalLabel`, `status` | none | l. 76027 (registry, 5 000 ms); l. 75963 (envelope); consumers l. 129075, 129089-129097, 129112, 129143-129146, 131216, 131225 |
| `/status/network` | `GET` | none | `Default` | `eth0.network.status`, `eth0.ip.ip`, `ip.dns`, `ip.mask`, `ip.gateway`, `wlan0.network.interface_mode`, `wlan0.ip.ip`, `uap0.network.status`, `uap0.network.interface_mode` | none | l. 76028 (registry, 5 000 ms) **and** l. 183052 (path `yTJA`, without header); consumers l. 127383, 128678-128683, 143035, 143061-143066 |
| `/status/emrs` | `GET` | none | `Default` | dynamic-key object (`Object.entries`); keys prefixed with `_` are discarded | none | l. 76029 (registry, 5 000 ms); consumer l. 134656 |
| `/status/activeEvents` | `GET` | none | `Default` (**root array**, confirmed `[VERIFIED]` (hardware), captured 2026-10-16) | array of objects: `severity` (**integer**), `viewer`, `uuid`, `timestamp` (s), `activeUntil` (s), `category`, `confirmable`, `label` (vendor state-code name), `sourceID`, `prefix`, `eventID` | none | l. 76030 (registry, 10 000 ms); consumers l. 135689-135701, 135654, 164699, 71151, 154444; captured 2026-10-16, firmware 1.19.7-1 |
| `/status/events` | `GET` | none | `Default` | **the same fields** as `/status/activeEvents` (same mapper) | none | l. 76031 (registry, 10 000 ms); consumer l. 135651 |
| `/status/activeEvents?sync=true` | `GET` | `sync=true` | `Default` | `[NOT OBSERVED]` — emission used as a simple trigger | none (bare request, without header) | l. 183060 (path `yTJA`); only call site l. 135633 |
| `/status/events?sync=true` | `GET` | `sync=true` | `Default` | `[NOT OBSERVED]` — same | none (bare request, without header) | l. 183061 (path `yTJA`); only call site l. 135633 |
| `/status/powerflow` | `GET` | none | `Default` | `site.P_PV`, `site.P_Grid`, `site.P_Load`, `site.P_Akku`, `site.E_Day`, `site.E_Year`, `site.E_Total`, `inverters[<id>].SOC`, `inverters[<id>].BatMode`, `Smartloads.Ohmpilots[<id>].P_AC_Total` | none | l. 76036 (registry, 5 000 ms); consumers l. 129034, 129043-129063, 136261; **extracted but not called**: it briefly served as the application fallback for the day and year energies (unknown #13), removed after the captures proved no local source exposes them — **captured** 2026-10-16 (firmware 1.19.7-1): the shape is settled (`inverters` an **array** of `{CID, DT, E_Total, ID, P}`, `site.MLoc`), but `site.E_Day`/`site.E_Year` are **null here too** |
| `/status/common` | `GET` | none | `Default` | `licenseExchangeData`, `serviceState`, `updateRunning`, `networkState.connectedViaAP` | none | l. 76041 (registry, 5 000 ms) **and** l. 183049 (path `yTJA`, without header); consumers l. 25824, 45241, 127386, 135196, 143145 |
| `/status/version` | `GET` | none | `Default` | `serialNumber`, `hardwareId`, `devicename`, `articleNumber`, `swrevisions.GEN24`, `hwrevisions.FAMILYID`, `numberOfPhases`, `devicegroup[0]` | none (path without header) | l. 183050 (path `yTJA` — **this path is not in the `aYRN` registry**); consumers l. 45243, 126568, 127380, 128813-128814, 128842, 128881-128882, 135137, 143048-143049, 183034-183035; captured 2026-10-16 (firmware 1.19.7-1: `swrevisions.GEN24` = `1.19.7-1`, `numberOfPhases` = 1, no model key). The application consumes `serialNumber`, `swrevisions.GEN24`, `numberOfPhases`, `devicename` and `articleNumber`; `hardwareId` and `hwrevisions` remain read-only for the vendor client |
| `/status/registrationData` | `GET` | none | `Default` | `[NOT OBSERVED]` — payload handled as a JSON blob (`JSON.stringify` to a `<textarea>`), no named field read | none (path without header) | l. 183051; consumer l. 143036, display l. 143144 |
| `/status/capabilities` | `GET` | none | `Default` | `gpios` | none (path without header) | l. 183053 (accessor `ioCapabilities`); consumers l. 126565, 126582 |
| `/status/ioServices` | `GET` | none | `Default` | `services[].name`, `services[].label` | none (path without header) | l. 183054; consumers l. 126566, 126576-126578 |
| `/status/powerflowcontroller` | `GET` | none | `Default` | `[NOT OBSERVED]` — accessor `powerflowcontroller`, no payload consumer | none (path without header) | l. 183055 |
| `/status/deviceDB` | `GET` | none | `Default` | `Inverters[<id>].NominalPower` | none (path without header) | l. 183056; consumer l. 129072 |
| `/status/license` | `GET` | none | `Default` | `serialNumber`, `powerValue`, `sequenceNumber` | none (path without header) | l. 183058; consumer l. 128785 |
| `/status/license/features` | `GET` | none | `Default` | dynamic-key object (`Object.keys` / `t[key]`), no named key | none (path without header) | l. 183059; consumer l. 128793, 128797 |

### Components `Cgi`

Envelope `Cgi` ⇒ the useful payload is `Body.Data` (l. 75958). In
`Body.Data`, the top-level keys are the **component identifiers**;
the client iterates over them (`Object.keys`, l. 129130) and reads `[id].channels`.

| Path | Method | Arguments | Envelope | DTO (fields read) | Declared role | Evidence (lines) |
|---|---|---|---|---|---|---|
| `/components/cache/readable` | `GET` | none | `Cgi` (`Body.Data`) | `[id].channels` (43 signals, see below), `[id].attributes["inverter.devicetype"]` | none | l. 76033 (registry, 5 000 ms); l. 75958 (envelope); consumers l. 129070, 129091, 129123, 129130, 128972, 128976 |
| `/components/BatteryManagementSystem/readable` | `GET` | none | `Cgi` (`Body.Data`) | `[id].channels` (10 signals), keys = component identifiers | none | l. 76032 (registry, 5 000 ms); l. 75958; consumers l. 129098, 129123, 129130, 128986; captured 2026-10-16: `Body.Data` empty (no battery), `Content-Type: text/javascript` |
| `/components/Ohmpilot/readable` | `GET` | none | `Cgi` (`Body.Data`) | `[id].channels` (4 signals) | none | l. 76034 (registry, 5 000 ms); l. 75958; consumers l. 129105, 129002 |
| `/components/PowerMeter/readable` | `GET` | none | `Cgi` (`Body.Data`) | `[id].channels` (23 signals), `[id].attributes.addr`, `[id].attributes["[ENABLE]"]` | none | l. 76035 (registry, 5 000 ms); l. 75958; consumers l. 129114, 129011, 129015, 130974-130983; captured 2026-10-16: `Body.Data` empty (no meter), `Content-Type: text/javascript` |
| `/components/DataManager/readable` | `GET` | none | `Cgi` (`Body.Data`) | `[id].attributes.deviceTreeInfo` (JSON to decode), then `modbus.extras.defaults` in the decoded tree | none (path without header) | l. 183057; l. 183011 (envelope `Cgi`); consumers l. 132248-132249, 132251-132257 |

#### Signals read in `channels`

A missing signal ⇒ `undefined`, never `0`: the mappers build each
entry from a `t.<SIGNAL>` access (l. 128972, 128976, 128986, 129002,
129011, 129015). The units declared by the client are those of the bundle.

| Component | Signals read | Evidence |
|---|---|---|
| inverter (`/components/cache/readable`) | `ACBRIDGE_POWERACTIVE_SUM_MEAN_F32`, `FEEDINPOINT_FREQUENCY_MEAN_F32`, `PV_POWERACTIVE_MEAN_01/02/03_F32`, `PV_VOLTAGE_MEAN_01/02/03_F32`, `PV_CURRENT_MEAN_01/02/03_F32`, `BAT_POWERACTIVE_MEAN_F32`, `BAT_VOLTAGE_OUTER_MEAN_01_F32`, `BAT_CURRENT_MEAN_F32`, `FEEDINPOINT_VOLTAGE_MEAN_01/02/03/12/23/31_F32`, `ACBRIDGE_CURRENT_ACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERAPPARENT_MEAN_01/02/03_F32`, `ACBRIDGE_POWERAPPARENT_SUM_MEAN_F32`, `ACBRIDGE_POWERREACTIVE_MEAN_01/02/03_F32`, `ACBRIDGE_POWERREACTIVE_SUM_MEAN_F32`, `FEEDINPOINT_ANGLE_MEAN_12/13/23_F32`, `ISO_RESISTANCE_MEAN_F32`, `MODULE_TEMPERATURE_MEAN_01/03/04_F32`, `DEVICE_TEMPERATURE_AMBIENTEMEAN_F32`, `DCLINK_VOLTAGE_MEAN_F32` | l. 128972 (23 signals), l. 128976 (20 signals) |
| battery (`/components/BatteryManagementSystem/readable`) | `LEGACY_CURRENT_DC_CHARGE_TARGET_F64`, `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16`, `BAT_TEMPERATURE_CELL_F64`, `BAT_TEMPERATURE_CELL_MIN_F64`, `BAT_TEMPERATURE_CELL_MAX_F64`, `DCLINK_VOLTAGE_MEAN_F32`, `POWERMANAGER_VOLTAGE_LINKMIN_F32`, `BAT_VOLTAGE_DC_CELL_MAX_F64`, `BAT_ENERGYACTIVE_ESTIMATION_MAX_CAPACITY_F64`, `BAT_ENERGYACTIVE_MAX_CAPACITY_F64` | l. 128986 (10 signals; the two energies are converted `Ws → Wh`, l. 128989) |
| Ohmpilot (`/components/Ohmpilot/readable`) | `SMARTMETER_ENERGYACTIVE_CONSUMED_SUM_F64`, `OHMPILOT_POWERACTIVE_DESIRED_F64`, `ACBRIDGE_POWERACTIVE_SUM_MEAN_F32`, `LEGACY_TEMPERATURE_MEAN_00_F64` | l. 129002 (4 signals) |
| Smart Meter (`/components/PowerMeter/readable`) | `SMARTMETER_POWERACTIVE_MEAN_SUM_F64`, `SMARTMETER_FREQUENCY_MEAN_F64`, `SMARTMETER_POWERACTIVE_01/02/03_F64`, `SMARTMETER_VOLTAGE_MEAN_01/02/03_F64`, `SMARTMETER_CURRENT_MEAN_01/02/03_F64`, `SMARTMETER_POWERAPPARENT_MEAN_SUM_F64`, `SMARTMETER_POWERAPPARENT_01/02/03_F64`, `SMARTMETER_POWERREACTIVE_MEAN_SUM_F64`, `SMARTMETER_POWERREACTIVE_01/02/03_F64`, `SMARTMETER_FACTOR_POWER_SUM_F64`, `SMARTMETER_FACTOR_POWER_01/02/03_F64` | l. 129011 (11 signals), l. 129015 (12 signals) |

### `/commands/*` reads: call sites outside the registry

| Path | Method | Arguments | Envelope | DTO (fields read) | Declared role | Evidence (lines) |
|---|---|---|---|---|---|---|
| `/commands/GetUTCTime` | `GET` | none | `CommandResult` | `epoch` (seconds) | `user: "customer"` (registry, l. 76039) | l. 76037-76040 (registry, 15 000 ms); l. 90579 (direct site); consumers l. 128622, 128767 |
| `/commands/GetServiceState?serviceId=<id>` | `GET` | `serviceId` (integer, `T_SystemServiceId`; `GridlimitsAutotest` = 21, l. 116025) | `CommandResult` | `resultData.state` (fallback `BLOCKED` if `success` is false) | none | l. 76043 (registry, 5 000 ms); l. 90935, 90939 (direct site); consumer l. 136831 |
| `/commands/StandbyState` | `GET` | none | `CommandResult` | `[NOT OBSERVED]` — payload stored as-is (accessor `standbyState$`, l. 135484), no field read | none | l. 76045 (registry, 5 000 ms); consumer l. 135524 |
| `/commands/InverterRollbackStatus` | `GET` | none | `CommandResult` | `partitionNumber`, `available` (shapes declared by the client's fallback object) | none | l. 21334-21336 (site, module `966A`); consumers l. 135220, 135259, 135325, 135332 |
| `/commands/GetUpdateStatus` | `GET` | none | `CommandResult` | `status`, `lastActiveStatus`, `rebootPending`, `nsteps`, `cur_step`, `cur_percent` | none | l. 90610 (site); consumers l. 135220, 135249, 135315, 135435, 135446 |
| `/commands/ListGridcodes` | `GET` | none | `CommandResult` | `primary[<id>].group` (string or array), `primary[<id>].title` | none | l. 90730 (site); consumers l. 128640-128649 |
| `/commands/ListOhmpilots` | `GET` | none | `CommandResult` (fallback `[]`) | array of objects: `serial`, `masterId`, `label`, `ip`, `hardware`, `software` | none | l. 90921 (site); consumers l. 131187-131202, 131304-131311 |
| `/commands/GetAutoTestReport?testName=<name>` | `GET` | `testName` (string; `GridlimitsAutotest` → `Italiantest`, l. 90968) | `CommandResult` (if `success`) | `Common.TESTTIMESTAMP`, then all keys dynamically, each with its `_<key>_meta` metadata (`unit`, `displayType`, `valueType`, `displayIndex`) | none | l. 90964, 90976 (site); consumers l. 136812, 136913-136956 |
| `/commands/ConnectedPowermeter?componentId=<id>` | **`POST`** | `componentId` (in the URL); body = payload of the calling DAO | `CommandResult` | `[NOT OBSERVED]` — the payload is never read (`take(1)` alone) | none | l. 91025 (site, POST); call l. 130764; `componentId` comes from `[id].attributes` of the polling (l. 130982) |
| `/commands/GetSDPFile` | `GET` (blob) | none | `Default` (binary body; the client builds `{fileName, data}`) | `[NOT OBSERVED]` — `{fileName, data}` is never unpacked (accessor `cryData$`, l. 136664) | none | l. 90876-90896 (including two raw `_httpClient.get` l. 90876, 90882); consumer l. 136726-136727 |

Three of these ten paths are also read by the **polled registry** (with a Digest
header), with their interval: `/commands/GetUTCTime` (15 000 ms, l. 76037),
`/commands/GetServiceState?serviceId=` (5 000 ms, l. 76043) and
`/commands/StandbyState` (5 000 ms, l. 76045); the seven others exist only
as direct calls. `ConnectedPowermeter` is the only path classified `READ`
for which the official client emits a **`POST`**.

### `/config/*`: DAO `rCKn` (`loadData` / `saveData`)

The module `rCKn` builds one DAO per path (`new h(t, "<path>", this)`,
l. 161462-161485); each exposes `loadData()` (`GET` + Digest, l. 161422) and
`saveData()` (`POST` + Digest, l. 161428), both with the logged-in account
(`type: fallback`, l. 46809-46810) — **no named role**. The envelope is
`Default`: the body is consumed at the root (the cache receives the response as-is
l. 161382, and `execute$` reads `writeSuccess` at the root l. 161405-161406).
The fields below are those **read** by an application consumer.

| Path | Method | Arguments | Envelope | DTO (fields read) | Declared role | Evidence (lines) |
|---|---|---|---|---|---|---|
| `/config/common` | `GET` / `POST` | none | `Default` | `systemName`, `timezone`, `wattPeakValues[].id`, `wattPeakValues[].wattPeak` | logged-in account (`fallback`) | l. 161463 (declaration), 161422, 161428; consumers l. 127249, 128765-128766, 129031, 129426-129427 |
| `/config/emrs` | `GET` / `POST` | none | `Default` | `rules.pin1..pin4`, `rules._pinN_meta`, `_rules_meta`, `priorities` | logged-in account | l. 161464; consumers l. 134420-134430, 134569 |
| `/config/iomapping` | `GET` / `POST` | none | `Default` | `features[].label`, `features[].ios[].service`, `features[].ios[].PinId`, `gpios` (`features[].enable` is only **written**) | logged-in account | l. 161467; consumers l. 126572-126582, 132901-132902 |
| `/config/meter` | `GET` / `POST` | none | `Default` | `primary.available`, `primary.isLoadMeter`, rest passed to the form builder | logged-in account | l. 161471; consumers l. 130410, 130663, 131596, 132233, 134666 |
| `/config/batteries` | `GET` / `POST` | none | `Default` | `HYB_BM_PACMIN`, `_HYB_BM_PACMIN_meta.validators`, `_BAT_TYPE_meta.options`, `BAT_ENABLED` | logged-in account | l. 161462; consumers l. 129879-129881, 129945-129946, 131596 |
| `/config/modbus` | `GET` / `POST` | none | `Default` | `master.rtuif[].if`, `slave.rtuif[].if` | logged-in account | l. 161472; consumers l. 132444-132445, 132735-132741 |
| `/config/pcc` | `GET` / `POST` | none | `Default` | `dno`, `as4777` | logged-in account | l. 161475; consumers l. 132897, 134148 |
| `/config/policy` | `GET` / `POST` | none | `Default` | `newPolicyVersion.dmweb`, `policyTerms[].name`, `policyTerms[].label`, `policyTerms[].date` | logged-in account | l. 161476; consumers l. 127763-127772 |
| `/config/multinetwork` | `GET` / `POST` | none | `Default` | `eth0.ipConfig.hostname`, `eth0.ipConfig.dhcpEnabled`, `wlan0.ipConfig.dhcpEnabled`, `inetConnection.selInetIface` | logged-in account | l. 161473; consumers l. 127250-127251, 127255-127256 |
| `/config/ohmpilot` | `GET` / `POST` | none | `Default` | `selected` | logged-in account | l. 161474; consumers l. 131234, 135825, 135829 |
| `/config/pushservice` | `GET` / `POST` | none | `Default` | `Hosts[].Enable`, `Hosts[].Format.key`, `Hosts[].Format.value.Protocol`, `Hosts[].Format.value.Interval`, `Hosts[].Name`, `Hosts[].Host.Addr`, `Hosts[].Host.Remote`, `Hosts[].Auth.Use`, `Hosts[].Proxy.Use`, `_Hosts_Element_meta._Format_Element_meta` | logged-in account | l. 161477; consumers l. 134894, 134958-134962, 134964, 134968, 134973, 134986 |
| `/config/wizard` | `GET` / `POST` | none | `Default` | `active`, `stateNetwork`, `stateService`, `stateSolarweb` | logged-in account | l. 161478; consumers l. 81353, 135155, 143035, 143047-143051 |
| `/config/timeofuse` | `GET` / `POST` | none | `Default` | `timeofuse`, `_timeofuse_meta.entryBaseType._ScheduleType_meta.options` | logged-in account | l. 161479; consumers l. 137879, 137890 |
| `/config/setup/powerunit` | `GET` / `POST` | none | `Default` | `mppt`, `mppt.PV_MODE_COMBINE_U16`, `mppt1.PV_MODE_MPP_01_U16`, `mppt2.PV_MODE_MPP_02_U16`, `mppt3.PV_MODE_MPP_03_U16` | logged-in account | l. 161485; consumers l. 129313, 129322, 129430, 135527 |
| `/config/powerlimits` | `GET` / `POST` | none | `Default` | `schema` — passed to `_powerLimitsMapper.mapToDestination`; only `exportLimits.activePower.mode` and its meta are then modified on the form side | logged-in account | l. 161465; consumers l. 130417, 130435, 130447-130454 |
| `/config/ics` | `GET` / `POST` | none | `Default` | `schema` — passed to `Ge.a.build(t, this._form)` | logged-in account | l. 161466; consumers l. 133806, 133810 |
| `/config/setup/gridcode/general` | `GET` / `POST` | none | `Default` | `schema` — passed to the form builder, then to `mapper.getMappedDataSets(…, "CHANNELS")` | logged-in account | l. 161481; consumers l. 133274, 128855-128859 |
| `/config/setup/gridcode/safety` | `GET` / `POST` | none | `Default` | `schema` — same | logged-in account | l. 161482; consumer l. 133306 |
| `/config/setup/gridcode/interfaceprotection` | `GET` / `POST` | none | `Default` | `schema` — same | logged-in account | l. 161483; consumer l. 133338 |
| `/config/setup/gridcode/gridfeatures` | `GET` / `POST` | none | `Default` | `schema` — same | logged-in account | l. 161484; consumers l. 133370, 128855-128865 |
| `/config/solar_api` | `GET` / `POST` | none | `Default` | `schema` — passed to the builder `addSourceStream(...).build()` | logged-in account | l. 161470; consumer l. 155339 |
| `/config/logging` | `GET` / `POST` | none | `Default` | `[NOT OBSERVED]` — accessor `config.logging` declared, no consumer | logged-in account | l. 161468 |
| `/config/solarweb` | `GET` / `POST` | none | `Default` | `[NOT OBSERVED]` — accessor `config.solarweb` declared, no consumer | logged-in account | l. 161469 |
| `/config/gridcode` | `GET` / `POST` | none | `Default` | `[NOT OBSERVED]` — accessor `config.allGridSettings` declared, no consumer | logged-in account | l. 161480 |

### Swept call sites not retained (non-`READ` paths)

Of the 44 call sites, 34 target paths that this document classifies
`MUTATION`, `DANGEROUS` or `UNDETERMINED`: they are inventoried here to be
**avoided**, with the verb actually emitted by the official client.

| Path | Observed verb(s) | Classification | Evidence (lines) |
|---|---|---|---|
| `/commands/SetPassword` | `POST` (`?user=`) | MUTATION | l. 90512 |
| `/commands/ActivatePasswords` | `GET` | MUTATION | l. 90529 |
| `/commands/Login` | `GET` (`?user=`, `passwordUser`) | MUTATION | l. 90546 |
| `/commands/Logout` | `GET` | MUTATION | l. 90564 |
| `/commands/ReInitInterface` | `GET` (`?iface=`) | DANGEROUS | l. 90596 |
| `/commands/ScanAvailableWlan` | `GET` (`?doScan=`) | MUTATION | l. 90624 |
| `/commands/StandbyRequestState` | `GET` / `POST` | MUTATION | l. 90638, 90641 |
| `/commands/GridcodeSelection` | `GET` / `POST` | DANGEROUS | l. 90752, 90760 |
| `/commands/SetUTCTime` | `POST` | MUTATION | l. 90784 |
| `/commands/factoryReset` | `POST` | DANGEROUS | l. 90800 |
| `/commands/ConfirmEvent` | `POST` | MUTATION | l. 90814 |
| `/commands/InverterControl` | `POST` | DANGEROUS | l. 90828 |
| `/commands/DCInjectionTest` | `POST` | DANGEROUS | l. 90842 |
| `/commands/CreateSDPFile` | `GET`, `GET ?status=true` | MUTATION | l. 90857, 90860 |
| `/commands/SetWPSActive` | `GET` (`?set=`) | MUTATION | l. 90906 |
| `/commands/StartSystemService?serviceId=` | `GET` | DANGEROUS | l. 90949 |
| `/commands/RemoteAccess` | `GET` / `POST` | DANGEROUS | l. 90987, 90992 |
| `/commands/InverterRollback` | `POST` | DANGEROUS | l. 91008 |
| `/commands/PasswordRecovery` | `GET` / `POST` (`?user=`) | DANGEROUS | l. 91039, 91045 |
| `/commands/VerifyVCode` | `GET` (`?vCode=`) | MUTATION | l. 91066 |
| `/commands/LicenseExchange` | `POST` (`?exchangeMode=`), `GET`, `GET ?quit=true` | MUTATION | l. 91078, 91081, 91084 |
| `/commands/LicenseExchangeData` | `POST` / `GET` | UNDETERMINED | l. 91093, 91096 |
| `/commands/ServiceFile` | `GET ?status=`, `POST` (XHR), `GET ?serviceFile=`, `GET ?acknowledgement=` (blob) | UNDETERMINED | l. 91160, 91152, 91157, 91166, 91172 |
| `/commands/SupportAccount` | `POST`, `POST {resetPassword: true}` | DANGEROUS | l. 91193, 91196 |
| `/commands/License` | `GET ?status=`, `GET ?acknowledgement=`, `GET ?serviceFile=` (blob) | UNDETERMINED (treated as a write) | l. 120739, 120745, 120751; URL l. 120675-120677 |
| `/update/start` | `POST` (XHR) | DANGEROUS | l. 90677, 90712 |

Off-device: the `_httpClient.get` of l. 54377 loads the SVG icons of Angular
Material — this is not a Fronius path.

### Declared roles (client only)

- The polled registry attaches a role option to **one** entry only:
  `/commands/GetUTCTime` with `{loginDao: …, user: "customer"}` (l. 76037-76040).
  This option feeds `_minimumRequiredUserOption` (l. 75909) and the
  `_isUserAllowed` condition (l. 75972-75984): `customer` passes for
  `customer | technician | support`, `technician` and `support` pass for
  `technician | support`. This is a **display condition**, not a
  server authorization.
- The choice of the account sent is distinct from the role:
  `_getUserAndPass` (l. 46807-46813) returns either the request group's account
  (`fallback`, l. 46809), or an explicit `user:password` pair
  (`passwordUser`, l. 46811). The only direct sites observed in `fallback` are
  the `/config/*` DAO (l. 161422, 161428) and the commands `VerifyVCode`,
  `PasswordRecovery`, `SetPassword`, `ActivatePasswords`; `Login` is the only
  site in `passwordUser`.
- The signal mappers also declare display roles:
  `_getRequiredUsersCustomer` (l. 128959-128960) =
  `technician | support | utility | unauthorized | user | customer`, and
  `_getRequiredUsersTechnician` (l. 128962-128963) =
  `technician | support | utility` (l. 128971, 128975, 128985, 129001, 129010,
  129014).
- **Route guard**: the 37 route entries of the AppModule array (`const Yb`,
  l. 155510-155701, `RouterModule.forRoot(Yb)` l. 155779) carry
  `canActivate: [oi.a]`; the lazy `ji.a.solarWeb` route (l. 155691) carries
  **none**. `[INFERENCE]` that `oi.a` is module `euqz` (l. 81184-81388): the
  import binding of `oi` lies outside the readable window, and the mapping rests
  on (i) `canActivate(t, e) {` (l. 81341) being the **only** such definition in
  the bundle, (ii) `euqz` exporting exactly one binding, `a` = class `M`
  (l. 81248, 81251), (iii) `oi.a` appearing in the AppModule provider list
  (l. 138531).
  **It is not a role gate.** `canActivate` (l. 81341-81347) chains four
  middlewares — `_updateMiddleware` (l. 81311), `_exchangeMiddleware`,
  `_dashboardMiddleware`, `_defaultMiddleware` (l. 81325) — and yields `true`,
  `false` (`_generateResult`, l. 81283-81286) or a **redirect `UrlTree`** to
  `/update`, `/exchange`, `/dashboard` or `/startPage`. Its role part is
  `_mapLoggedInUser` (l. 81366-81374) → `_mainMenuDAO.isUserPermittedForUrl`
  (l. 43732-43741) → the three-valued enum `Wfm5` (`Permitted = 0`,
  `NotPermitted = 1`, `ProfiRequired = 2`, l. 70078-70090): on `ProfiRequired`
  or on an unauthorized path that is not a login-free one, the observable
  completes **without emitting**, so the navigation is cancelled rather than
  refused. The route table states a client-side minimum per menu entry
  (`H8nL`, l. 43533-43674: `DEVICE_CONFIGURATION` → `technician`, l. 43533-43535;
  `INVERTER` → `customer`, l. 43542) and the same strings enable or disable the
  menu entries (l. 43803); the login string is expanded into role flags by
  the map `D` (l. 77898-77905, `customer` → `RoleEverybody` + `RoleCustomer`).
- The **only authorization-like value that comes back from the device** and is
  read by the client is `writePermission`, compared field by field
  (`isEditAllowed(this._user, t && t.writePermission)`, l. 69202): the flag's
  value per firmware and per account is unknown, and no boolean appearing in a
  payload constitutes a matrix.
- **None** of these roles is a server permission: the bundle contains
  no authorization matrix, only display conditions. Unknown
  #6 of `docs/UNKNOWNS.md` remains open.

## Matrix: endpoint / method / parameters / declared role / tested role / side effect

"Declared role" comes from the **client**: the cumulative roles (`RoleEverybody`,
`RoleCustomer`, `RoleTechnician`, `RoleSupport`, `SetupAccessRole01/02`) are
declared in the bundle and condition the display, not the server
authorization. No server role is proven at this stage: filling it in requires
the internal API (phase 2) and a test account, which is out of scope here.
"Tested role" therefore equals "none" everywhere unless stated otherwise: nothing was
replayed on the hardware.

| Path | Method | Parameters | Declared role | Tested role | Side effect |
|---|---|---|---|---|---|
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | `GET` (imposed by the guard) | none | none (no known authentication) `[TO-TEST]` | **simulator**: 200 without header; **hardware**: not executed | none: read-only |
| `/status/version` | `GET` (imposed by the guard) | none | Digest "Customer" `[SOURCE-JS]` — also read without header by the module `yTJA` | **simulator**: 401 without header then 200 after Digest; `curl --digest` accepted (third-party client) | none: identity read |
| `/status/activeEvents` | `GET` (imposed by the guard) | none | same | same | none: event read |
| `/components/PowerMeter/readable` | `GET` (imposed by the guard) | none | same | same | none: state read |
| `/components/BatteryManagementSystem/readable` | `GET` (imposed by the guard) | none | same | same | none: state read |
| `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CommonInverterData` | `GET` (imposed by the guard) | `Scope`, `DeviceId`, `DataCollection` (the frozen `solarapi.CommonQuery`) | none (no known authentication) | **simulator**: 200 without header; **hardware**: surveyed 2026-10-16, `200`, `digest_required: false` | none: read; the thirteen measurements and `DeviceStatus` (state and codes), read every collector cycle |
| `/status/common` | `GET` (imposed by the guard) | none | `Default` `[SOURCE-JS]`; the Digest flag `true` describes the official client, not this device | **simulator**: 200 (or 401 without credentials), then 200 after Digest; **hardware**: surveyed 2026-10-16, `200`, `digest_required: false` | none: device-wide state (device detail) |
| `/status/network` | `GET` (imposed by the guard) | none | `Default` `[SOURCE-JS]`; the Digest flag `true` describes the official client, not this device | **simulator**: 401 without credentials, then 200 after Digest; **hardware**: surveyed 2026-10-16, `200`, `digest_required: false` | none: network interfaces (device detail) |
| `/status/devices` | `GET` (imposed by the guard) | none | same | same | none: device inventory (device detail) |
| `/status/emrs` | `GET` (imposed by the guard) | none | same | same | none: EMRS pin states (device detail) |
| `/components/cache/readable` | `GET` (imposed by the guard) | none | `Cgi` (`Body.Data`) `[SOURCE-JS]`; the Digest flag `true` describes the official client, not this device | **simulator**: 401 without credentials, then 200 after Digest; **hardware**: surveyed 2026-10-16, `200`, `digest_required: false`, `4 897` bytes, `99` values | none: on the detection cycle, the device's own register set (device detail, **Cache block**); no unit is derived from a name |
| `/status/powerflow` (**removed from the allowlist — extracted but not called**) | `GET` (imposed by the guard) | none | `Default` `[SOURCE-JS]`; the Digest flag `true` describes the official client, not this device | **simulator**: no longer serves the path; **hardware**: captured 2026-10-16 (firmware 1.19.7-1, `200`, `digest_required: false`, `inverters` an array; fixture `internalapi/status_powerflow.json`) | none: read; it briefly served as the **fallback** for the day and year energies the Solar API returns as `null`, but the captured body has them **null too** and the accumulation collection nulls them as well, so it fills nothing on this installation (unknown #13) |
| **`READ` paths now extracted**: `/status/*` (17 lines), `/components/*/readable` (5), `/commands/*` reads (10), `/config/*` reads (24) | `GET`, except `POST` for `/commands/ConnectedPowermeter` and for the `/config/*` write | per path: see "Method, arguments and DTO per path" | Angular display roles `[SOURCE-JS]`, unproven at this stage; `user: "customer"` declared on `/commands/GetUTCTime` alone | **none** | none expected (read); `[OBSERVED-CONVERSATION]`: `GET` 200 on the four `/config/*` paths already tried |
| `/config/*` writes (24 paths) | `POST` observed (l. 161428) | per path: see the extraction table | logged-in account (`fallback`, l. 161428) — server role unknown | **none** | configuration change; `DANGEROUS` for `gridcode`, `pcc`, `powerlimits`, `modbus`, `setup/*` |
| `/commands/*` mutations (17 paths: `ActivatePasswords`, `ConfirmEvent`, `CreateSDPFile`, `LicenseExchange`, `Login`, `Logout`, `ScanAvailableWlan`, `SetPassword`, `SetUTCTime`, `SetWPSActive`, `StandbyRequestState`, `VerifyVCode`, …) | `GET`/`POST` depending on the command `[SOURCE-JS]` | variable | variable depending on the command | **none** | state, session, file or preference modification |
| `/commands/*` dangerous (10 paths: `DCInjectionTest`, `GridcodeSelection`, `InverterControl`, `InverterRollback`, `PasswordRecovery`, `ReInitInterface`, `RemoteAccess`, `StartSystemService`, `SupportAccount`, `factoryReset`) | `GET`/`POST` observed (see the extraction table) | variable | unknown | **none** | production shutdown, firmware, electrical grid protection, access or erasure |
| `/commands/License`, `LicenseExchangeData`, `ServiceFile` | `GET`/`POST` observed (`?status=`, `?acknowledgement=`, `?serviceFile=`) | variable | unknown | **none** | undetermined ⇒ treated as a write |
| `/update/start` | `POST` observed (XHR, l. 90677) | body = file | unknown | **none** | device firmware update |

What this matrix establishes: **eleven paths are called** — the **nine** internal
paths of the allowlist, the Solar API flow endpoint and the Solar API common
inverter collection — all in `GET`, without observed side effect, and without any
`MUTATION` or `DANGEROUS` entry in the executable allowlist. `/status/powerflow`
appears in the matrix only as a retired fallback: it is **extracted but not
called**. The 79 other paths listed in
this document remain inventoried to be explicitly avoided. The "unknown"
cells are not implicit authorizations: they are unknowns
(see `docs/UNKNOWNS.md`).

This extraction **adds nothing to the allowlist**: it replaces
unknowns with facts observed in the official client, and it shows that the
official client does emit `POST`s — on around thirty paths outside the
allowlist and even on a path classified `READ`
(`/commands/ConnectedPowermeter?componentId=`, l. 91025). `/status/powerflow`
briefly joined the allowlist later as a distinct decision (fallback, #13) and was
**removed** once the captures proved no local source exposes the day and year
energies; it is now **extracted but not called**. The rule "no
non-`GET` command" below applies to **the application**, not to the
manufacturer's client: it is the one that remains the only executable
authorization.

## Applicable rule

No `POST`, `PUT`, `PATCH` or `DELETE` command is emitted to the inverter.
This rule is structural (`internal/fronius/readonly.go`), not declarative:
a non-GET request is rejected before reaching the transport, and the check
`grep -rn 'MethodPost\|MethodPut\|MethodDelete\|MethodPatch' internal cmd apps/web/src`
must find nothing outside the guard's error message and its tests.

`READ` endpoints marked `[TO-TEST]` will only be called after an explicit
review and a confirmation by the user. Adding an entry to
`internal/fronius/internalapi/allowlist.go` is such a decision: an
unlisted path is not callable, whatever the calling code.
