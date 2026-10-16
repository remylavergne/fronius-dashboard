# Read-only probe — protocol and prior consent

`cmd/fronius-probe` is the **only** tool in this repository able to contact a
real inverter other than the simulator. This document describes exactly what it
does, what it refuses to do, and what the user must do with its result.

Founding rule: **the development agent never runs this probe**, neither on
hardware nor against a non-local address. It is shipped to be launched by the
person responsible for the installation, who decides when.

## 1. What the probe asks before leaving

Before any execution against real hardware, five questions must have an explicit
answer:

1. **Target address** — the exact URL of the device (for example
   `http://192.0.2.10`). Are the host and port those of the inverter and
   not of another piece of equipment?
2. **Moment** — is it acceptable that the inverter's log records this request at
   that instant?
3. **Network** — is access planned (installation Wi-Fi, VPN, workstation on the
   same network)?
4. **Credentials** — does the device require a user and a password for its
   internal paths? Where are they set: service environment, or
   `--password-stdin`? (Never write them to a versioned file.)
5. **Output** — where will the redacted result be kept, and who will read it
   again?

None of these answers can be guessed by the agent: they are the user's
responsibility.

## 2. Exact requests emitted

The probe can only emit the requests of its allowlist, frozen in the binary
(six entries, all `READ` and all `GET`):

| Path                                           | Digest    | What it provides                                                        |
| ---------------------------------------------- | --------- | ----------------------------------------------------------------------- |
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi`  | no        | production measurements (site and inverters)                            |
| `/status/version`                              | after 401 | device identity (serial number, number of phases, device group)         |
| `/status/activeEvents`                         | after 401 | active events (shape established: **root array** of objects)            |
| `/components/PowerMeter/readable`              | after 401 | presence of a Fronius Smart Meter                                       |
| `/components/BatteryManagementSystem/readable` | after 401 | presence of a battery                                                   |
| `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` | no | documented Solar API accumulation data (`E_Day`, `E_Year`, `E_Total`), on the "Solar API first" route (ADR 0003/0008); **captured** 2026-10-16 (`200`, 883 bytes, no challenge): `DAY_ENERGY`/`YEAR_ENERGY` are **null** while `TOTAL_ENERGY` is live, so it is **not** a source for the day and year counters (`docs/UNKNOWNS.md` #13) — it is the documented source of the inverter state (`DeviceStatus`, a **candidate** metric, not implemented) |

**Result of the first real run** (2026-10-16, firmware 1.19.7-1): the five paths
of the allowlist at that time answered `200` on the **bare** request — no
`Authorization` header was ever
constructed and no `401` was ever issued, so `digest_required` is `false` for all
five and the "after 401" column was never triggered. The two component paths
replied with `Content-Type: text/javascript` (not `application/json`). Details
and residual points: `docs/UNKNOWNS.md`, `docs/SOURCES.md`.

Since then, the allowlist briefly held a sixth entry, `/status/powerflow`
(fallback for the day and year energies the Solar API returns as `null`, unknown
#13), added later and **captured** on hardware (2026-10-16, `200`, no challenge);
the body is promoted as `internalapi/status_powerflow.json`. The captured body is
**decisive**: `site.E_Day`/`E_Year` are **null there too**, so the fallback filled
nothing on this installation, and the entry has since been **removed** — the probe
lists **six** entries again, and `cmd/fronius-sim` no longer serves
`/status/powerflow` (the captured fixture does).

The sixth entry that **stays** is the documented Solar API accumulation endpoint:
`GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData`
(class `READ`, `digest=false`). It carries a **frozen query string** — the
`endpoint.query` field, a constant of the binary (`solarapi.CumulationQuery`) — so
the guarantee below holds unchanged: the probe cannot target an arbitrary URL, and
the query is shown in `--list`; `--endpoint` still selects by **path** (without
the query). It was **captured on the device** on 2026-10-16 (`200`, 883 bytes, no
challenge): `DAY_ENERGY` and `YEAR_ENERGY` come back **null** while `TOTAL_ENERGY`
is live, so **no local source** exposes the day and year counters
(`docs/UNKNOWNS.md` #13) — the "Solar API first" route (ADR 0003/0008) is settled
and the internal fallback was removed. The answer carries the inverter state the
flow endpoints do not (`Body.Data.DeviceStatus`), a **candidate** for a future
metric, not implemented. `cmd/fronius-sim` serves the endpoint (documented shape,
values from its own curve), so a probe rehearsal against the simulator answers
**six** paths with `200`; the rehearsal was run and passed (`200`, 296 bytes, query
echoed in `Head.RequestArguments`). The probe's file name for this path is
`solar_api_v1_getinverterrealtimedata.json` (derived from the path, query
excluded); the redacted capture is kept at
`/tmp/probe-real/probe_solar_api_v1_getinverterrealtimedata_20261016T170038Z.json`
(§ 5).

### What the capture must confirm, path by path

The bundle extraction (`docs/ENDPOINTS.md`) replaced the unknowns with
**client-code** facts; what remains is to obtain the **device** facts. Each
capture of the allowlist closes a precise unknown. The first run took place on
2026-10-16 (firmware 1.19.7-1) and closed the five unknowns below; later runs the
same day captured `/status/powerflow` (since removed) and the accumulation
endpoint. The table is kept as the record of what each capture had to settle:

| Path                                           | Unknown to close                     | What the extraction already asserts                                                                                                                                                                                             | What the capture settles                                                                                 |
| ---------------------------------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi`  | #1 (JSON shape), #2 (authentication) | **absent from the bundle**: no schema is proven, `dto.go` is a hypothesis                                                                                                                                                       | schema, units, timestamps; and whether a Smart Meter or a battery exists (`P_Grid`, `P_Akku`, `rel_*`)   |
| `/status/version`                              | #3 (identity)                        | `serialNumber`, `numberOfPhases`, `devicegroup` proven; `swrevisions.GEN24` is the software version read by the official client (l. 128882); `hardwareId`, `devicename`, `articleNumber` are read; **no model key** is observed | the real version, the existence of a model key, and the expected shape of `articleNumber`                |
| `/status/activeEvents`                         | #11 (event shape)                    | payload = array; 11 fields read; `severity` is an **integer** (1 error, 2 warning, 3 information, l. 135728); identity = `uuid`, or `prefix`-`eventID`; text = `label`                                                          | the real names and types, the semantics of `viewer`, and whether `label` carries readable text or a code |
| `/components/PowerMeter/readable`              | #4 (meter presence)                  | `Body.Data` indexed by component identifier; `channels` (23 `SMARTMETER_*` signals); `attributes.addr` and `attributes["[ENABLE]"]`                                                                                             | presence, real signal names and units on `1.19.7-1`                                                      |
| `/components/BatteryManagementSystem/readable` | #5 (battery and state of charge)     | `channels` (10 signals), including `BAT_VALUE_STATE_OF_CHARGE_RELATIVE_U16` in `%`                                                                                                                                              | presence, and whether the state of charge really exists on this firmware                                 |
| `/status/powerflow`                            | #13 (null day/year energies)         | envelope `Default`; fields read `site.E_Day`/`E_Year`/`E_Total`, `site.P_PV`/`P_Grid`/`P_Load`/`P_Akku`, `inverters.<id>.SOC`/`BatMode`, `Smartloads.Ohmpilots.<id>.P_AC_Total` (l. 129034, 129043-129063) — **captured** 2026-10-16 (fixture `internalapi/status_powerflow.json`) | the real values and units of the day and year energies — **settled**: `site.E_Day`/`E_Year` are **null here too**, so this internal path does **not** return them where the Solar API nulls them; the fallback built on it was **removed** (#13) |
| `/solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` | #13 (null day/year energies) | the **documented** Solar API v1 shape, `Body.Data.CumulationInverterData` with `E_Day`, `E_Year`, `E_Total` — documented, **not extracted** (the bundle has no Solar API path, `docs/SOURCES.md`); the probe expects it as-is | whether this firmware exposes the day/year counters **at all** — **settled (captured 2026-10-16)**: `DAY_ENERGY`/`YEAR_ENERGY` are **null** while `TOTAL_ENERGY` is live, so the counters are not reported by any local source; the same answer carries `DeviceStatus` (`InverterState`, `StatusCode`), a **candidate** metric, not implemented. (The first attempt used the `.fcgi` suffix and answered `404` with the webserver's XHTML page, which is what a wrong path looks like here.) |

Now that these five captures are taken (2026-10-16, firmware 1.19.7-1; promotion
in § 8), the freeze is broader: a later run captured `/status/powerflow` (the
fallback for the day and year energies the Solar API returns as `null`, #13) and
it is promoted in § 8 — the capture is decisive, the internal path returns those
energies as **null too**, so the fallback filled nothing on this installation and
the entry was **removed**. The same day, the documented Solar API accumulation
endpoint (see above) was **captured** as well: it nulls the same counters while
its total is live, so **no local source** exposes the day and year energies. The
rule for adding remains a metric that the Solar API does not provide, and a metric
is not chosen from an inventory (ADR 0008).

Shape of a request to an internal path:

```
GET /status/version HTTP/1.1
Accept: application/json
Host: <address provided with --base-url>
```

then, **only if** the device answered `401`:

```
GET /status/version HTTP/1.1
Accept: application/json
Authorization: Digest username="<user>", realm="…", nonce="…", uri="/status/version",
              response="…", qop=auth, nc=00000001, cnonce="…"
Host: <address provided with --base-url>
```

- **Method**: `GET` only. The method of each entry is a code constant.
- **Parameters**: no caller-supplied parameter. The probe adds neither body nor
  form field, and no query string of its own: an entry MAY carry a **frozen query
  string** (`endpoint.query`), a constant of the binary — the Solar API
  accumulation entry carries
  `Scope=Device&DeviceId=1&DataCollection=CumulationInverterData` — which `--list`
  shows; `--endpoint` selects by path, without the query.
- **Headers**: `Accept: application/json`, then `Authorization` **only in
  response to a 401**. The header is never sent preemptively: the first send to
  an internal path is always a bare request, which the simulator and the test
  `TestProbeSendsNoCredentialsPreemptively` verify.
- **Repetition**: a single resend per path, after a 401. The received challenge
  is remembered: subsequent paths reuse the same nonce and emit only one
  request. No loop, no interval.
- **Scope**: the credentials can only leave towards the host of `--base-url`;
  the transport refuses any other host (`digest.ErrForeignHost`).

The contract is verified by construction, not by discipline:

- `fronius.ReadOnly` is interposed in front of the transports and rejects any
  request whose method is not `GET`, before any connection is opened
  (`internal/fronius/readonly.go`);
- `selectEndpoints` refuses any path absent from the allowlist
  (`errNotWhitelisted`): the probe cannot target an arbitrary URL;
- the check `grep -rn 'MethodPost\|MethodPut\|MethodDelete\|MethodPatch' internal cmd`
  finds nothing outside the error message of this guard and its tests.

To display the allowlist without emitting any request:

```bash
go run ./cmd/fronius-probe --list
```

## 3. Device credentials

Credentials **never** go through a command-line argument: a password in an
argument appears in the shell history and in the process table.

| Source                    | Usage                                                                              |
| ------------------------- | ---------------------------------------------------------------------------------- |
| `FRONIUS_DEVICE_USER`     | user, read from the environment                                                    |
| `FRONIUS_DEVICE_PASSWORD` | password, read from the environment (set with `read -rs`, never on a command line) |
| `--password-stdin`        | password read from the first line of standard input, for interactive use           |

Both go together: providing only one is an error, refused before any request.
With no credentials at all, the probe works as in phase 1: it emits bare
requests and reports the `401` received.

```bash
# interactive: the password appears neither in the history nor in ps
read -r FRONIUS_DEVICE_USER && export FRONIUS_DEVICE_USER
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081 --endpoint /status/version

# or, password on standard input
export FRONIUS_DEVICE_USER=installer
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081 --password-stdin
```

`--endpoint <path|all>` restricts the probe to one allowlist entry (selected by
**path**; a frozen query belongs to the entry, not to the selector); any other
path is refused.

## 4. Local-address guard

By default, the probe **refuses** any address that is not the loopback
(`127.0.0.1`, `::1`, `localhost`):

```bash
go run ./cmd/fronius-probe --base-url http://192.0.2.10
# fronius-probe: refusal: http://192.0.2.10 is not a local address;
# re-run with --allow-non-loopback after reading docs/PROBE-READONLY.md
# and obtaining the user's explicit consent
```

The refusal happens **before** any name resolution and any connection: no
packet leaves towards the device. Lifting the guard is a deliberate act:
`--allow-non-loopback`.

## 5. Command to run

Towards the local simulator (no risk, no hardware):

```bash
# terminal 1
go run ./cmd/fronius-sim --api-user sim --api-password simpass --with-meter

# terminal 2: without credentials, the four internal paths answer 401
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081

# terminal 2: with the simulator account, the six paths answer 200.
# The simulator password is "simpass"; it is entered on standard input
# so it does not appear in the shell history.
export FRONIUS_DEVICE_USER=sim
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081 --save /tmp/probe-out
```

Towards the real inverter, after the consent of point 1:

```bash
export FRONIUS_DEVICE_USER=installer
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
go run ./cmd/fronius-probe \
  --base-url http://192.0.2.10 \
  --allow-non-loopback \
  --timeout 5s \
  --save /tmp/probe-reel
```

The accumulation endpoint **was captured** with the command below (2026-10-16,
into a fresh directory so the earlier captures are not overwritten); it is kept
for reference:

```bash
./bin/fronius-probe \
  --base-url http://192.0.2.10 \
  --allow-non-loopback \
  --endpoint /solar_api/v1/GetInverterRealtimeData.cgi \
  --save /tmp/probe-real
```

The password is then entered on standard input, never as an argument. At most
one resend per path, and only after a `401`. If the device answers with an
error, the probe reports it and stops.

## 6. Expected output

```
Request
  GET http://192.0.2.10/status/version
  classification: READ
  allowlist: Device identity: serial number (established key), number of phases, device group

Response
  status   : 200 OK
  duration : 12ms
  bytes    : 164
  headers (sensitive values masked)
    Authorization: <redacted>
    Content-Type: application/json
    Date: Fri, 16 Oct 2026 09:58:15 GMT

Redacted body
{ … }
```

The received challenge header (`x-www-authenticate`, then `www-authenticate` as
a fallback) is displayed masked: it carries a nonce, hence a session identifier.

Interpretation:

| Observation                                   | Consequence                                                                                                                                                                                                                       |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `200 OK` and `Body.Data.Site` populated       | The assumed shape of `dto.go` is confirmed: promote the fixture (§ 8).                                                                                                                                                            |
| `401` or `403` with `x-www-authenticate`      | The device requires authentication on this path: set `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` (§ 3) and re-run. A persistent `401` **after** re-authentication signals a wrong user or password, or an unexpected realm. |
| `404`                                         | The path differs on this firmware: fix `internal/fronius/solarapi/dto.go` and this document.                                                                                                                                      |
| Fields or units different from those expected | Fix `dto.go`, the fixtures and `docs/SOURCES.md`. The SQLite columns do not change (base SI units).                                                                                                                               |
| Absence of `P_Grid`, `P_Load`, `rel_*`        | Expected behaviour without a Smart Meter; confirms the invariant "no invented zero".                                                                                                                                              |

## 7. Redaction rules

Redaction applies **before** any display and **before** any write to disk. It is
implemented in `cmd/fronius-probe/redact.go` and tested in `probe_test.go`.

| Target         | Rule                                                                                                                                                                                                                                                                                                           |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| JSON fields    | Any key containing `password`, `passwd`, `secret`, `nonce`, `token`, `authorization`, `credential`, `serial`, `mac`, `ssid`, `hostname`, `ipaddress` or `sessionid` has its value replaced by `<redacted>`.                                                                                                    |
| Textual values | IPv4 address, IPv6 address, MAC address, host name or fully qualified name → `<redacted>`.                                                                                                                                                                                                                     |
| Non-JSON body  | The assignments `password=…`, `nonce=…`, `serial=…`, etc. are redacted, then the address patterns are applied to the text.                                                                                                                                                                                     |
| HTTP headers   | `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `WWW-Authenticate` / `x-www-authenticate`, `X-CSRF-Token`, `X-Auth-Token`: value fully masked. Other headers are masked if their value looks like an address.                                                                                  |
| Written file   | Mode `0600`, name derived from the **path** and timestamped (`probe_components_powermeter_readable_<UTC>.json`). The name cannot come from the last segment alone: `/components/PowerMeter/readable` and `/components/BatteryManagementSystem/readable` both end in `readable` and would overwrite each other. |

Two non-regression rules are tested explicitly:

- **measurements are never redacted**: a power, an energy, a timestamp or a mode
  stay intact (otherwise the fixture would be unusable);
- **a time is not an IPv6 address**: `12:00:00` and
  `2026-06-21T12:00:00+02:00` are not masked (`looksLikeIPv6` requires at least
  four groups, or the compressed form `::`).

The JSON is rewritten with stable indentation, which may **reorder the keys** of
an object. The result remains valid JSON; that is the price of reliable
structural redaction.

## 8. Promotion of a redacted fixture

**First promotion: 2026-10-16, firmware 1.19.7-1** — the six captures were
promoted (`powerflow_produce_only.json` and the five internal fixtures); see
`docs/SOURCES.md`. A later run the same day captured the **accumulation
endpoint** (`883` bytes) but **no fixture is versioned for it**: the application
does not call the endpoint, and its facts live in `docs/SOURCES.md` and
`docs/UNKNOWNS.md` (#13).

1. Check the written file: open `probe_*.json` and confirm that no serial
   number, host name, address or identifier remains.
2. Mask the device serial number if it appears in a form not covered by the
   redaction, then replace the root field `_provenance` with:

   ```json
   "_provenance": {
     "synthetic": false,
     "source": "fronius-probe",
     "confidence": "VERIFIED (hardware)",
     "firmware": "<observed version>",
     "captured_at": "<UTC timestamp of the capture>",
     "http_status": 200,
     "digest_required": true
   }
   ```

   `digest_required` is `true` if the device answered `401` before the challenge,
   `false` if it answered directly: both paths exist in the official client (see
   `docs/SOURCES.md`), and it is the observed response that settles it for _this_
   installation.

3. Rename after the captured **path** (`status_version.json`,
   `components_powermeter_readable.json`, `status_activeevents.json`,
   `components_batterymanagementsystem_readable.json`, `status_powerflow.json`,
   `solar_api_v1_getinverterrealtimedata.json`, or
   `powerflow_produce_only.json`, `powerflow_with_meter.json`, …) and replace
   the corresponding synthetic file.
4. Re-run `go test ./...`: the contract test
   (`internal/fronius/solarapi/contract_test.go`) will fail on the expected
   values. **That is the point**: update the expected values _after_
   verification, never to make the test pass.
5. Update `docs/SOURCES.md` (capture status, firmware, date) and record the
   change in `CHANGELOG.md`.
6. Re-evaluate the capabilities in `internal/model/capabilities.go`: the
   mentions "Schema not validated on firmware 1.19.7-1" can be removed only for
   what was actually observed.

A promoted fixture becomes the contractual reference of the project. Any later
capture must be compared to it before being adopted.

## 9. No-write rule

**No `POST`, `PUT`, `PATCH` or `DELETE` command must be emitted towards the
inverter until a dedicated security review has authorized a named endpoint.**

This rule covers:

- the application (`froniusd`): all its clients are built behind
  `fronius.ReadOnly`;
- the probe (`fronius-probe`): allowlist of `GET` only;
- the simulator (`fronius-sim`): it never contacts the inverter;
- tests and CI: no test performs an outgoing request to an inverter.

The `MUTATION` and `DANGEROUS` endpoints are inventoried and classified in
`docs/ENDPOINTS.md`, with the sole goal of **knowing what must not be called**.
The inventory is not an authorization.

Phase 2 may widen the allowlist to internal API reads (`/status/*`, `/config/*`
over GET). Each addition will have to be:

1. justified by a metric that the Solar API does not provide;
2. documented in `docs/ENDPOINTS.md` (declared role, tested role, side effect);
3. accompanied by a test proving that the method is indeed `GET`;
4. explicitly authorized by the user before the first execution on hardware.
