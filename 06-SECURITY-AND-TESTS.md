# Security, limits and validation plan

## Prohibited by default
- No POST/PUT/PATCH/DELETE to the GEN24 without explicit approval and endpoint review.
- No attempt at a recovery signature, password brute force, nor manipulation of `gridcode`, `powerlimits`, relays, backup modes, firmware update.
- No storage of a password in localStorage, logs, committed fixtures or public exports.
- Do not reuse the setup unlock keys as Technician credentials.

## Tests
- Unit tests: Solar API parser, units, energy vs power, time series, errors, aggregate computations, summer-time transition.
- HTTP tests: 200, 401 Digest challenge, refreshed nonce, 403, 404, 429, 5xx, timeout, connection cut, invalid JSON, null values.
- Security tests: no secrets in the logs, restricted CORS, interface authentication if accessible beyond localhost, request limits, parameter validation.
- UI tests: states without a Smart Meter, missing data, offline, long period, mobile, screen reader.
- Integration tests: Fronius simulator/mock from anonymized fixtures; no hardware access required in CI.
- Manual tests on hardware: **GET only**, reasonable frequency, validation of results, capture of redacted evidence.

## Confidence rules
- `[SOURCE-JS]`: proven in the provided bundle; may differ on other firmwares.
- `[SOURCE-PDF]`: timestamped system export.
- `[OBSERVED-CONVERSATION]`: result reported from an earlier test; keep the evidence if necessary.
- `[HYPOTHESIS]`: unvalidated proposal.
- `[TO-TEST]`: never presented as functional.

## Data policy
- Retain power W, energy Wh/kWh, UTC timestamps, source, quality, acquisition state.
- Do not store a password in the history; mask IP and serial in exports intended for the public.
- Document data retention and deletion.
