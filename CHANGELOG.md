# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) and the
project adheres to [semantic versioning](https://semver.org/lang/fr/).

## [Unreleased]

### Added

- 2026-10-16 — **Device name and article number surfaced.** `/status/version`
  carries `devicename` (`"Primo GEN24 3.6 Plus"`) and `articleNumber`
  (`"4,210,141,002"`), both captured on hardware; the identity panel now shows
  them as their own rows beside model, firmware and serial number. Persisted in
  the `device` table (`device_name`, `article_number`, migration
  `0005_device_name.sql`) and exposed by `/api/v1/system` as `device_name` and
  `article_number` — an absent value stays `null`, never an empty string. The
  model row is untouched: no model key exists in that payload, so `model` stays
  `null` and the panel keeps showing the placeholder instead of passing a device
  name off as a model (ADR 0008).

- 2026-10-16 — **Internal `/status/powerflow` fallback for the day and year
  energies.** The Solar API returns `E_Day` and `E_Year` as `null` on firmware
  `1.19.7-1` (`docs/UNKNOWNS.md` #13, cause still unknown); the decided fallback
  (option B) reads the internal `/status/powerflow` path. `internalapi.PowerFlow`
  (DTO) and `Client.PowerFlow` serve it; the path joins the executable allowlist
  (class `READ`, envelope `Default`), bringing it to **five internal paths plus
  the Solar API endpoint** — six paths callable by the application and by
  `cmd/fronius-probe --endpoint /status/powerflow`. The collector fills **only**
  the missing day and year energies: **a measured Solar API value is never
  overwritten**, and the fallback is called only when one of the two is missing.
  A fallback failure is merely logged as a warning — it is **not** a collection
  failure — and never invents a zero. The provenance is recorded in the new
  `samples.fallback_source` column (migration `0004_fallback_source.sql`, empty
  string by default) and exposed by `/api/v1/live` as `fallback_source`.
  `cmd/fronius-sim` now also serves `/status/powerflow` (mirroring its own Solar
  API numbers; it does **not** reproduce the `null` case, which the synthetic
  fixture `test/fixtures/internalapi/status_powerflow.json` encodes). That shape
  is still `[HYPOTHESIS]` until a hardware capture promotes it
  (`docs/PROBE-READONLY.md`).

- 2026-10-16 — **First hardware capture and fixture promotion.** Two
  `cmd/fronius-probe` runs against a real GEN24 (`http://192.0.2.10`, one
  without credentials, one with) captured the five allowlisted paths on firmware
  `1.19.7-1`. **No authentication is required on this installation**: all five
  paths answered `200 OK` with no `Authorization` header ever constructed
  (`digest_required: false` for each), so the Digest transport remains implemented
  and tested but unused by this device; `FRONIUS_DEVICE_USER` /
  `FRONIUS_DEVICE_PASSWORD` are not needed here. The real
  `GetPowerFlowRealtimeData.fcgi` payload promotes
  `test/fixtures/solarapi/powerflow_produce_only.json` (`synthetic: false`,
  `confidence: "VERIFIED (hardware)"`); the four redacted internal captures
  promote `test/fixtures/internalapi/status_version.json`,
  `status_activeevents.json`, `components_powermeter_readable.json` and
  `components_batterymanagementsystem_readable.json`. The capture confirms the
  firmware key `swrevisions.GEN24` = `1.19.7-1`, a **single-phase** installation
  (`numberOfPhases` = 1), **no model key**, and **no Smart Meter and no battery**
  (`Body.Data` empty on both component paths, returned as
  `Content-Type: text/javascript`). `docs/SOURCES.md` records the provenance and
  `docs/UNKNOWNS.md` is updated: #1, #2, #3, #4, #5 and #11 are resolved, #13 is
  added. No path was added to the allowlist.
- 2026-10-16 — **Documentation: extraction of method / arguments / DTO of the internal
  read-only paths.** `docs/ENDPOINTS.md` gains a table per path
  (54 paths, 56 rows): method, URL arguments, envelope (`Default` /
  `Cgi` / `CommandResult`), fields **actually read** and role **declared**, each
  row backed by bundle line references. The group cells of the
  matrix are replaced by a cross-reference and three false references are
  corrected: `/status/version` is **not** in the polled registry `aYRN` (only
  the `yTJA` path, l. 183050), `/components/PowerMeter/readable` is at l. 76035
  (and not 76031), and the `rCKn` module — hence the `/config/*` literals — is
  readable (l. 161462-161485). Reproducible inventory: 46 call lines
  swept outside `JWhm` (of which 2 internal delegations), 7 raw HTTP calls,
  10 `[NOT OBSERVED]`, 7 "schema" payloads. Provenance module by module
  in `docs/SOURCES.md`; `docs/UNKNOWNS.md` #3, #4, #5, #6 and #11 are updated
  with what the extraction establishes (including a state-of-charge signal read by the
  official client) without ever presenting a client role as a server
  permission; `03-API-INVENTORY.md` leaves open only the readings requiring a
  capture on the device. Associated documentation: ADR 0008 (scope of the exposed
  data: domain model, no API mirror; rule for adding to the
  allowlist) and the list of the five expected captures, path by path, in
  `docs/PROBE-READONLY.md`. The executable allowlist remains unchanged; the
  code fixes that the extraction made necessary are in
  `### Fixed`.
- 2026-10-16 — **Phase 3: hourly aggregates and scheduled purge.** Migration
  `0003_agg.sql`: table `agg_hour` (`count_all`, `count_pv`, `sum_pv_w`,
  `min_pv_w`, `max_pv_w`, `max_energy_day_wh`) which materializes the exact
  reduction of the measurements — the average is recomposed from the sum and the count, the
  average of a union not being the average of the averages. New package
  `internal/maintenance`: aggregation of the closed hours every 5 minutes with
  catch-up at startup, and retention purge at startup then once per
  local day from 03:00 (the `purge()` of `cmd/froniusd` is moved there). New
  `make test-perf` target that replays the criterion of ADR 0006
  over one year of measurements.

- 2026-10-16 — **Phase 2: authenticated reading of the internal paths.**
  `internal/secret`: type `secret.String` that masks its value for `fmt`,
  `slog` and `encoding/json`, `Reveal()` being the only clear-text output.
  `internal/fronius/digest`: Digest RFC 2617 transport conforming to the official
  client (challenge read first in `x-www-authenticate`, **one** single retry on
  401, zero-padded decimal `nc` incremented under lock, `algorithm=` never
  emitted, random `cnonce` instead of the manufacturer's `"NaN"`), header never
  sent preemptively nor to a foreign host (`ErrForeignHost`).
  `internal/fronius/internalapi`: executable allowlist of four paths
  (`/status/version`, `/status/activeEvents`, `/components/PowerMeter/readable`,
  `/components/BatteryManagementSystem/readable`) and extraction of the identity by
  ordered candidate key lists. `internal/detect`: detection loop
  (identity, presence, active events), anti-duplication of events in
  `device_events_seen`, abandoning a path after a 401 received without configured
  credentials. New variables `FRONIUS_DEVICE_USER`,
  `FRONIUS_DEVICE_PASSWORD`, `FRONIUS_DETECT_INTERVAL`, refused at startup if
  the pair is incomplete.

### Changed

- 2026-10-16 — **Repository language switched to English.** Documentation, code
  comments, user-facing strings (API messages, capability and detection reasons,
  event messages, log messages), test messages, CLI help and the interface are
  now in English; `docs/DEVELOPMENT.md` and `AGENTS.md` state the convention
  (identifiers, comments, documentation, interface and commits in English).
  Files whose names were French have been renamed and every reference updated:
  `01-CONTEXT-AND-STATE.md`, `03-API-INVENTORY.md`, `04-TARGET-ARCHITECTURE.md`,
  `05-PRODUCT-UI-SPEC.md`, `06-SECURITY-AND-TESTS.md`, `07-SOURCES-INDEX.md`,
  `docs/adr/0003-solar-api-vs-internal-api.md`,
  `docs/adr/0004-supervision-vs-commands.md`,
  `docs/adr/0005-authentication-and-remote-access.md`,
  `docs/adr/0006-retention-and-aggregation.md`,
  `docs/adr/0008-exposed-data-scope.md`, `apps/web/src/i18n/en.ts`. Two
  behaviour-neutral consistency fixes came with it: SPA routes become
  `/capabilities` and `/settings`, and display formatting uses `en-GB` instead of
  `fr-BE` (decimal point, comma thousands separator, 24-hour clock) — values are
  unchanged, only their rendering is. Machine contracts are untouched: paths, JSON
  keys and enum values, environment variables, flags, SQL identifiers and
  migration numbers. Verified: `go test -count=1 ./... -race`, `go vet`,
  `oxlint`, `pnpm -C apps/web build` and `test --run` (61 tests), plus a runtime
  pass (simulator + `froniusd` + browser: logs, API payloads, capability reasons
  and the four screens are English).
- 2026-10-16 — `internal/storage`: reads at hourly or daily step
  are served by `agg_hour` as soon as they cover whole and closed hours, the rest
  (partial header and open hour) remaining read from the raw
  measurements; the result is identical, only the duration changes (14 ms against
  5,168 ms for one year at 1 h step, 5 ms against 5,535 ms for the daily
  energy). A measurement written under the water mark
  (`collector_state.agg_last_hour_utc`) lowers it, without which a history
  imported afterwards or a lagging device clock would have been omitted from the
  aggregates without anything signaling it. `PurgeBefore` now deletes the
  aggregates of the same range as the measurements and raises the water mark: the
  aggregates are never a second source of truth. The purge bound is
  rounded up to the whole hour, so that measurements and aggregates share the
  same boundary. The purge therefore no longer takes place only at
  restart.

- 2026-10-16 — `internal/model`: the capabilities take a three-state
  detection (`unknown`, `absent`, `present`) instead of three configuration
  booleans. `device.identity` and `device.events` join the list;
  `battery.soc` remains unavailable **even with battery detected** (detecting a
  component does not prove that a state-of-charge measurement exists on the API
  side). `internal/api`: `/api/v1/system` exposes `device` (nullable identity:
  an empty value becomes `null`, never `""`) and `detection` (state and reason of the
  four resources, plus `last_error`); `/api/v1/capabilities` derives from the
  persisted detection. Migration `0002_device.sql`: `device` table with a
  single row (three-valued states **and** their cause), `device_events_seen` table,
  and purge of the anti-duplication marks together with that of the events.
  `cmd/fronius-probe`: allowlist extended to five endpoints, `--endpoint`
  (any path outside the list is refused), `--password-stdin`, and file names
  derived from the full path (the two components both ended in
  `readable` and would have overwritten each other).
- 2026-10-16 — Interface: System page redesigned (`DevicePanel`) displaying
  the identity with its source and its age, then the four detections with their state
  and their reason; the three states are translated distinctly ("present",
  "absent", "undetermined"), and an absent field is displayed as a dash — never
  `0`, never "unknown".

### Fixed

- 2026-10-16 — **`/status/powerflow`: `inverters` is an array on the device, not an
  index-keyed object** (`internal/fronius/internalapi`). The bundle could not
  settle the question — the vendor client reads that member through
  `Object.keys`/`Object.values`, which accept both an array and a map — and the DTO
  had modelled a map, so the fallback failed on hardware with
  `json: cannot unmarshal array into ... PowerFlow.inverters` and the day and year
  energies stayed empty. Only `site` is modelled now, since only those two energies
  are consumed; `inverters` is kept as raw JSON, so no shape is presumed for a
  member the application does not read. The fixture and the simulator both use the
  array form observed on the device, and the client test accepts both.
  The registers (`docs/ENDPOINTS.md`, `docs/PROBE-READONLY.md`, `docs/SOURCES.md`,
  `docs/UNKNOWNS.md` #2/#13) now say "observed on hardware, body not saved"
  instead of "not captured".

- 2026-10-16 — **A refusal received after a Digest challenge is no longer reported
  as an absent endpoint** (`internal/fronius/digest`, `internal/detect`). When the
  device answers `401` with a challenge and then refuses the *authenticated*
  request with `404` (or any other 4xx), the path demonstrably exists — a bare
  `404` challenges nobody — yet the final status alone produced the detection
  reason "endpoint absent on this firmware (404)", which sent the operator looking
  at the firmware instead of at the credentials. The transport now reports the
  sequence (`froniuserr.ErrRefusedAfterChallenge`, kind `unauthorized`, with the
  final status) and the detector exposes it as its own reason,
  "challenged, then refused: check the credentials and the role they carry
  (HTTP 404 on /status/version)". Both clients also log the **full URL** at debug
  level, so a wrong `FRONIUS_SOLARAPI_URL` is visible at a glance instead of
  surfacing as a `404` on every path.

- 2026-10-16 — **`GetPowerFlowRealtimeData` DTO** (`internal/fronius/solarapi/dto.go`).
  The hardware capture contradicts the retired `[HYPOTHESIS]` on the meter-location
  field: it is **`Meter_Location`**, a **string** (`"unknown"`), not
  `Meter_Location_Current`, an **integer**. The field is **renamed and retyped**;
  the simulator, the tests and the fixtures are aligned. Corollary observed on this
  firmware: `E_Day` and `E_Year` come back **null** on both `Site` and the
  inverter, while `E_Total` is a number — the "energy of the day" and "energy of
  the year" KPIs are therefore **absent** on the reference installation, never
  zero (the model keeps `*float64` ⇒ `nil`, never `0`; residual unknown #13, and
  the `/status/powerflow` option is an open decision). The extra keys the capture
  reveals (`BackupMode`, `BatteryStandby`, `Smartloads`, `Version`) are tolerated
  and ignored (no `DisallowUnknownFields`).
- 2026-10-16 — **Severity and identity of the device events**
  (`internal/detect`). The extraction of the bundle establishes that `severity` is an
  **integer** (1 error, 2 warning, 3 information — `getEventLevel`,
  l. 135728-135733); only strings were recognized, so **every**
  hardware event was classified as "information". The integer severity is
  now interpreted, the text labels remaining accepted by tolerance and
  `level` remaining a fallback key. The anti-duplication key follows the
  instance identity of the official client — `uuid`, then `prefix`-`eventID`, then `code`,
  `state`, then the JSON fingerprint: deduplicating on `code` alone, which designates
  an event *type*, lost the second of two active events of the same type. The
  displayed text goes through `label` (what the official client
  displays when the translation is missing) before the historical candidates.
  Assumed consequence: an event already seen under a `code` key is notified one
  more time after an update, the first time it carries a `uuid`. Tests:
  `TestSeverityMapping` (integers, strings, out-of-scale values),
  `TestEventKeyPriorities`, `TestSameCodeDistinctInstancesAreBothRecorded`.
- 2026-10-16 — **Device software version**
  (`internal/fronius/internalapi`). The version is read on the proven path
  `swrevisions.GEN24` (l. 128882, 135137 — the key that the official client displays
  as the current version), before the flat candidates
  `firmwareVersion`/`firmware`/`version`: `/api/v1/system` therefore sets
  `firmware` where it remained `null` on the hardware. Corollary: the device
  group (`devicegroup`) is **no longer** used as a model fallback — it is
  a sibling field of a different nature (l. 183035) — so `model` stays `null` as long
  as no model key is observed, instead of displaying "GEN24PRIMO"
  as the model.

### Security

- 2026-10-16 — CI guard: `git grep` refuses any non-empty `FRONIUS_DEVICE_PASSWORD`
  line in the repository; no documentation example writes a password
  after an `=` (entered by `read -rs` or `--password-stdin`). No credential
  is logged (verified on the `froniusd` log: 0 occurrence) nor
  returned by any of the seven API routes (dedicated test).

- 2026-10-16 — Repository foundation: `.gitignore`, MIT license, `CHANGELOG.md`,
  `.env.example`, `Makefile`, `docs/` tree and fix of the `references/` path
  in `AGENTS.md`.

- 2026-10-16 — Fronius simulator (`cmd/fronius-sim`): Go module
  `fronius-supervisor`, deterministic `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`
  (seed, solar curve, smoothed cloudiness), `?at=` extension,
  fault injection (`--fault` / `X-Sim-Fault`), `GET /sim/health`, `log/slog` logs.
  Deterministic unit and HTTP tests.

- 2026-10-16 — Configuration (`internal/config`): exclusive reading via
  `FRONIUS_*` environment variables, exhaustive validation (durations, integer,
  boolean, time zone, URL, listen address, log level and format) accumulating
  all the errors, and `CheckListenAddress` guard that refuses
  a non-local listen without `FRONIUS_ALLOW_REMOTE=1`.

- 2026-10-16 — Typed errors (`internal/fronius/froniuserr`): kinds
  `not_found`, `unauthorized`, `server`, `decode`, `timeout`, `transport`,
  `device_status`, `readonly_violation`; `errors.Is`/`errors.As` and `KindOf`.
  Read-only guard (`internal/fronius/readonly.go`): `ReadOnly` refuses
  any method other than GET before reaching the transport.

- 2026-10-16 — Solar API client (`internal/fronius/solarapi`): explicit DTOs of
  `GetPowerFlowRealtimeData.fcgi` ([HYPOTHESIS] shape, tolerant decoding), 1 MiB
  bound on the body, complete error mapping (404, 401/403,
  429/5xx, invalid or truncated JSON, missing `Body.Data`, `Head.Status.Code != 0`,
  exceeded delay, unreachable server).

- 2026-10-16 — Normalized model (`internal/model`): `Sample` (base SI units
  W/Wh/%, UTC timestamp, `Quality` measured/estimated/missing), `FromPowerFlow`
  conversion, capabilities with prerequisites, evidence and unavailability reason,
  typed local events. Tested invariant: an absent field stays `nil`, an explicit
  `0` from the device stays `0`.

- 2026-10-16 — Reference documentation: `docs/SOURCES.md` (provenance
  register, checks run on the bundle, assumed Solar API schema),
  `docs/ENDPOINTS.md` (READ/MUTATION/DANGEROUS classification of the 79 inventoried
  paths + phase 1 allowlist) and ADR 0001 to 0007.

- 2026-10-16 — SQLite persistence (`internal/storage`): single dependency
  `modernc.org/sqlite v1.60.1` (pure Go, `CGO_ENABLED=0` verified), embedded and
  versioned migrations (`schema_migrations`), WAL + `busy_timeout` 5000 ms
  + `synchronous=NORMAL`, idempotent upsert on `(ts_utc, source)`, buckets aligned
  on the epoch, daily energy by `MAX(E_Day)` in local time zone with
  handling of the 23 h and 25 h days, events, collection cycles, state of the
  collector and retention purge.

- 2026-10-16 — Collector (`internal/collector`): polling loop with jitter
  (`Interval ± Jitter`) on success and exponential backoff 1 s → 60 s on
  failure, injectable clock and random source, tracing of each cycle
  (`collector_runs`), collector state (`last_success_utc`, `last_error`),
  `collector_error` events (anti-duplication by message) and
  `collector_recovered`. No sample is inserted on a failed cycle.

- 2026-10-16 — REST API (`internal/api`): `{data, meta}` envelope, errors
  `{error:{code,message}}`, routes `health`, `system`, `live`, `timeseries`,
  `energy/daily`, `events`, `capabilities`; JSON 404/405; `meta.sampled_at` /
  `stale` / `stale_after_s` on the measurement endpoints; validation of `from`,
  `to`, `step` (including `auto` bounded to 1500 points), range ≤ 366 d, `limit` ≤ 500;
  optional static service with SPA fallback on `index.html`.

- 2026-10-16 — `froniusd` binary (`cmd/froniusd`): non-local listen guard,
  retention applied at startup if `FRONTIUS_RETENTION_DAYS` is set,
  history backfill (`--backfill-days`, refused outside the local loop without
  `--force-backfill`), collector in a goroutine, REST API, clean shutdown on
  SIGINT/SIGTERM. Client variant `GetPowerFlowRealtimeDataAt` (simulator
  extension `?at=`) reserved for history backfill.

- 2026-10-16 — Fixtures and contract tests (`test/fixtures/solarapi`,
  `internal/fronius/solarapi/contract_test.go`): four fixtures generated by
  the simulator (seed 42, frozen instant) with a `_provenance` root field,
  table-driven comparison of the expected normalized model, verification that the
  `_provenance` field is not consumed, that `Head.Status.Code = 7` surfaces as
  `device_status`, that the truncated fixture produces `KindDecode`, and that
  no fixture contains sensitive data.

- 2026-10-16 — Read-only probe (`cmd/fronius-probe`): allowlist of a single
  GET, refusal of non-local addresses without `--allow-non-loopback` (before any
  connection), structural redaction of the body and sensitive headers before
  display and writing, `--save` in `0600` mode, `--list`. Tests covering
  redaction, the guard refusal and the absence of a non-GET method.

- 2026-10-16 — `docs/PROBE-READONLY.md` (prior-agreement protocol, exact
  request, expected output, redaction rules, promotion of a fixture,
  prohibition of any non-GET method); matrix
  endpoint/method/parameters/declared role/tested role/side effect in
  `docs/ENDPOINTS.md`; candidate Solar API endpoints `[TO-TEST]` for
  the device identity and the meter/battery detection in
  `docs/SOURCES.md`.

- 2026-10-16 — React interface (`apps/web`): Vite 8 + React 19 + TypeScript 5.9
  in strict mode (`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`,
  `verbatimModuleSyntax`), shadcn/ui (Radix base, `radix-nova` style), Tailwind 4,
  TanStack Query, React Router 7, Recharts 3.10, zod 4. Four routes (overview
  , system, capabilities, settings), zod schemas mirroring the Go DTOs,
  `fetchJSON` classifying failures as `network`/`http`/`schema`, flat FR i18n
  ready for EN/DE, loading/error/empty/offline/stale states, 24 h/7 d/30 d curve
  or custom range with genuinely visible gaps, capabilities table,
  keyboard navigation and accessible textual summary of the chart. 56 vitest tests.

- 2026-10-16 — Operations documentation: `docs/ARCHITECTURE.md` (flows,
  packages, six invariants, fragile points), `docs/DEVELOPMENT.md`
  (prerequisites, commands, variables, test strategy, pitfalls encountered),
  `docs/RUNBOOK.md` (startup, diagnosis by symptom, SQLite backup and
  restore, retention, maintenance) and `docs/UNKNOWNS.md` (register
  of the ten unknowns with provisional decision and lift condition).

- 2026-10-16 — Continuous integration: `.github/workflows/ci.yml` with two jobs,
  `go` (`go vet`, `go test ./... -race`, `CGO_ENABLED=0 go build ./...`) and
  `web` (`pnpm install --frozen-lockfile`, `lint`, `test --run`, `build`).

### Changed
- 2026-10-16 — `go.mod` moves to `go 1.26.0`: `modernc.org/sqlite v1.60.1`
  itself declares `go 1.26.0`, the `go 1.24` directive planned is therefore
  not achievable with this pinned driver. The Go 1.26.0 toolchain is used
  (automatic detection).
