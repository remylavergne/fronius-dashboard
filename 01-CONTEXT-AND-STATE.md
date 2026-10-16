# Context and verified state

## Hardware and network

- [SOURCE-PDF] Model: **Fronius Primo GEN24 3.6 Plus**; rated power **3680 W**; serial number **33319328**.
- [SOURCE-PDF] Firmware `GEN24` / `GEN24PRIMO` / `DEVICEGROUP` **1.19.7-1**; WebUI **1.14.2-1**.
- [SOURCE-PDF] Wi-Fi connected, IP **192.0.2.10**, netmask **255.255.254.0**, gateway/DNS **192.0.2.1**; Ethernet disconnected at the time of the export.
- [SOURCE-PDF] Licenses/active features: `Battery Operation`, `Full Backup`; **an active license does not prove that a battery is physically installed**.
- [OBSERVED-CONVERSATION] No Fronius Smart Meter detected and no battery installed. AP address reported `192.168.250.181`.

## Access and goals

- [OBSERVED-CONVERSATION] Working `customer` account; forgotten `technician` password; fresh current browser, `AuthHash` contains only Customer.
- [OBSERVED-CONVERSATION] Solar API active; `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi` responds.
- [OBSERVED-CONVERSATION] Customer read HTTP 200 on `/config/common`, `/config/modbus`, `/config/meter`, `/config/iomapping`; `/config/powerlimits`, `/config/setup/powerunit`, `/config/setup/gridcode/general` returned `{}`.
- [OBSERVED-CONVERSATION] Permissions declared in the `/config/modbus` response: read/write `RoleCustomer` and `RoleTechnician` for `_master`, `_slave`. In `/config/meter`, write `_primary` and `_secondary` reserved to `RoleTechnician`; in `/config/iomapping`, `_features` Customer/Technician read and write. **Permission metadata ≠ validation of a POST operation.**
- [OBSERVED-CONVERSATION] PasswordRecovery offers a six-digit challenge and a counter of about five signature attempts; do not consume these attempts.

## Goals

1. Modern supervision interface **autonomous with respect to the Fronius Angular frontend** (not necessarily independent of the Fronius firmware/API).
2. Go backend for acquisition, normalization, historization, API and possible authorized commands.
3. React + TypeScript + shadcn/ui frontend + responsive charts.
4. Technical mapping of the APIs and roles; understand the possibilities of legitimate Technician recovery, without brute-force.

## Limits / unknowns

- No complete official specification of the internal endpoints.
- Possibly old firmware version: check the changes before any upgrade, without triggering it automatically.
- No reliable household consumption, grid import/export or self-consumption without dedicated measurement.
- No proof that the backend authorizes the writes accessible in the Customer screens.
- No Technician recovery signature key in the identified elements.
