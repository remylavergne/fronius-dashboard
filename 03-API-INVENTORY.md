# Static inventory of the paths present in the bundle

> Regex extraction of the provided JavaScript. **Indicative inventory**: a path may be used for read, for write, or only in an unavailable feature. The paths may be incomplete when they are built dynamically; the method and arguments of each path actually queried are now extracted from the polled registry and the call sites in the bundle, with their line numbers, in `docs/ENDPOINTS.md`. Permissions, however, are never inferred.

## Endpoints `/commands/` (35)

- `/commands/ActivatePasswords`
- `/commands/ConfirmEvent`
- `/commands/ConnectedPowermeter`
- `/commands/CreateSDPFile`
- `/commands/DCInjectionTest`
- `/commands/GetAutoTestReport`
- `/commands/GetSDPFile`
- `/commands/GetServiceState`
- `/commands/GetUTCTime`
- `/commands/GetUpdateStatus`
- `/commands/GridcodeSelection`
- `/commands/InverterControl`
- `/commands/InverterRollback`
- `/commands/InverterRollbackStatus`
- `/commands/License`
- `/commands/LicenseExchange`
- `/commands/LicenseExchangeData`
- `/commands/ListGridcodes`
- `/commands/ListOhmpilots`
- `/commands/Login`
- `/commands/Logout`
- `/commands/PasswordRecovery`
- `/commands/ReInitInterface`
- `/commands/RemoteAccess`
- `/commands/ScanAvailableWlan`
- `/commands/ServiceFile`
- `/commands/SetPassword`
- `/commands/SetUTCTime`
- `/commands/SetWPSActive`
- `/commands/StandbyRequestState`
- `/commands/StandbyState`
- `/commands/StartSystemService`
- `/commands/SupportAccount`
- `/commands/VerifyVCode`
- `/commands/factoryReset`

## Endpoints `/config/` (24)

- `/config/batteries`
- `/config/common`
- `/config/emrs`
- `/config/gridcode`
- `/config/ics`
- `/config/iomapping`
- `/config/logging`
- `/config/meter`
- `/config/modbus`
- `/config/multinetwork`
- `/config/ohmpilot`
- `/config/pcc`
- `/config/policy`
- `/config/powerlimits`
- `/config/pushservice`
- `/config/setup/gridcode/general`
- `/config/setup/gridcode/gridfeatures`
- `/config/setup/gridcode/interfaceprotection`
- `/config/setup/gridcode/safety`
- `/config/setup/powerunit`
- `/config/solar_api`
- `/config/solarweb`
- `/config/timeofuse`
- `/config/wizard`

## Other paths found

- `/components/BatteryManagementSystem/readable`
- `/components/DataManager/readable`
- `/components/Ohmpilot/readable`
- `/components/PowerMeter/readable`
- `/components/cache/readable`
- `/status/activeEvents`
- `/status/capabilities`
- `/status/common`
- `/status/deviceDB`
- `/status/devices`
- `/status/emrs`
- `/status/events`
- `/status/ioServices`
- `/status/license`
- `/status/license/features`
- `/status/network`
- `/status/powerflow`
- `/status/powerflowcontroller`
- `/status/registrationData`
- `/status/version`
- `/update/start`

## Confirmed APIs / historical trials

| Path                                          | Method   | Auth                | Result         | Source                 |
| --------------------------------------------- | -------- | ------------------- | -------------- | ---------------------- |
| `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` | GET      | to be documented    | responds       | conversation           |
| `/config/common`                              | GET      | Customer Digest     | 200            | conversation           |
| `/config/modbus`                              | GET      | Customer Digest     | 200            | conversation           |
| `/config/meter`                               | GET      | Customer Digest     | 200            | conversation           |
| `/config/iomapping`                           | GET      | Customer Digest     | 200            | conversation           |
| `/config/powerlimits`                         | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/config/setup/powerunit`                     | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/config/setup/gridcode/general`              | GET      | Customer Digest     | 200 `{}`       | conversation           |
| `/commands/Login?user=<username>`             | GET      | Digest passwordUser | defined in JS  | bundle `i3oe`          |
| `/commands/PasswordRecovery?user=<username>`  | GET/POST | specific            | defined in JS  | bundle and conversation |

## To record per endpoint before implementation

**Already recorded in the bundle** (done; table per path and line numbers in
`docs/ENDPOINTS.md`, section "Method, arguments and DTO per path (bundle
extraction)"): HTTP method, URL arguments, response envelope, fields
actually read by a consumer, and role declared by the client — 54 paths
covered, including the `?sync=true`, `?status=true`, `?doScan=`,
`?serviceId=`, `?componentId=`, `?testName=` variants.

**Remaining to record, and this requires a capture on the device** (probe
`cmd/fronius-probe`, out of this step):

- **redacted** JSON examples: real shape of the payloads, in particular
  those marked `[NOT OBSERVED]` (no consumer in the official client
  reads their fields) and those with dynamic keys;
- units, timestamps and errors actually returned by the firmware
  `1.19.7-1`;
- permissions actually **verified by the server** (401/403 per role): the
  bundle only proves display conditions (`docs/UNKNOWNS.md` #6);
- reasonable sampling frequency and timeout adapted to the device;
- observed side effects and `read-only` / `mutation` /
  `dangerous` classification confirmed by usage;
- official documentation or available contract tests.
