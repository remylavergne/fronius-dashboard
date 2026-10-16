# Exhaustive TODO — Fronius Go + React

> Boxes to be checked by the agent. Priority: phases 0 to 6 for the MVP, phase 7 after validation. No inverter mutation in the MVP.

## 0 — Scoping and evidence

- [x] Create the monorepo Git repository and a development README
- [x] Import the MD files from the folder and reference the original sources without publishing any secrets
- [x] Verify the hardware/firmware inventory against the PDF export
- [x] Identify the metrics actually returned by the Solar API (anonymized samples) — redacted capture of 2026-10-16 promoted as `test/fixtures/solarapi/powerflow_produce_only.json`; exact keys in `docs/SOURCES.md` § Shape of the Solar API v1 response
- [x] Dynamically detect the presence of a meter and a battery (absence/unknown distinguished)
- [x] Confirm the absence of Smart Meter and battery on the real hardware (probe, to be run by the user) — capture of 2026-10-16: both `/components/*/readable` answers carry an **empty** `Body.Data` (`docs/UNKNOWNS.md` #4, #5)
- [x] Define MVP vs phase 2 (control) goals and success criteria
- [x] Choose license, branch strategy, commit conventions
- [x] Record all unknowns in a decision register

## 1 — API research and security

- [x] Document `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` and its fields actually available — `docs/SOURCES.md` § Shape of the Solar API v1 response; two `[HYPOTHESIS]` divergences settled (`Meter_Location` string, `E_Day`/`E_Year` null)
- [x] Extract other documented and useful Solar API endpoints
- [x] Extract methods/arguments/DTO of the internal commands useful in read-only mode (table per path in `docs/ENDPOINTS.md`, provenance in `docs/SOURCES.md`)
- [x] Audit the Angular guard `oi.a` and the component checks without assuming server permissions — the guard is module `euqz` (only `CanActivate` in the bundle, l. 81341) and is **not** a role gate: four middlewares + a client-side menu lookup whose refusal cancels the navigation; the client-side role tables and the single device-published `writePermission` flag are recorded in `docs/ENDPOINTS.md` § Declared roles and `docs/UNKNOWNS.md` #6 (still open: no server matrix is provable from a bundle)
- [x] Build the endpoint/method/parameters/declared role/tested role/side effect matrix
- [x] Classify each endpoint READ / MUTATION / DANGEROUS
- [x] Analyze the `ytu1` Digest module, nonce handling and error cases
- [x] Verify 401/403 handling, expired nonce and cases of missing auth
- [x] Document Technician recovery and counter without performing any trial
- [x] Check old profiles/password manager if recovery is still desired
- [x] Document `SupportAccount`, SDP, ServiceFile as not validated on the firmware side
- [x] Exclude electrical network settings, firmware and recovery from active tests

## 2 — Architecture and tooling

- [x] ADR SQLite vs PostgreSQL
- [x] ADR SSE vs polling
- [x] ADR Solar API vs Modbus vs internal API
- [x] ADR authentication and remote access
- [x] ADR storage/retention and aggregations
- [x] ADR packaging Go static/Docker Compose/NAS
- [x] Initialize the Go module and the internal/ structure
- [x] Initialize Vite React strict TypeScript and shadcn/ui
- [x] Install Tailwind, TanStack Query, React Router, Recharts, Zod if necessary
- [x] Set up lint/format/tests Go + frontend
- [x] Add CI GitHub Actions or equivalent
- [x] Add `.env.example` examples without secrets

## 3 — Acquisition backend

- [x] Implement a configurable Go HTTP client with timeout/context
- [x] Implement the read-only Solar API adapter
- [x] Define versioned raw DTOs and normalization into a domain model
- [x] Normalize units W/kW/Wh/kWh, UTC timestamps, data quality
- [x] Handle absent/null values and optional fields
- [x] Handle 404/401/5xx, invalid JSON, timeout and disconnection
- [x] Set up configurable polling with reasonable jitter
- [x] Guarantee that no automatic retry triggers a write
- [x] Add collector metrics and redacted structured logs
- [x] Implement the Fronius mock/simulator and anonymized fixtures
- [x] Add contract tests from the real redacted responses — `internal/fronius/solarapi/contract_test.go` (promoted capture) and `internal/detect/contract_test.go` (the four internal captures); a guard covers the whole `test/fixtures` tree
- [x] Prepare an optional isolated Digest client, without using it if unnecessary for the MVP
- [x] Verify that the frontend never has access to the Fronius credentials

## 4 — Persistence and Go API

- [x] Define the SQLite schema: measurements, aggregates, events, system_status
- [x] Add versioned migrations and WAL
- [x] Implement idempotent upsert and deduplication
- [x] Aggregate minutes/hours/days with explicit rules
- [x] Handle time zones and the Europe/Brussels daylight saving change
- [x] Define retention and scheduled purge
- [x] Implement tested backup/restore
- [x] Implement `/api/v1/health` and `/api/v1/system`
- [x] Implement `/api/v1/live` and `/api/v1/timeseries`
- [x] Implement `/api/v1/energy/daily` and `/api/v1/events`
- [x] Implement `/api/v1/capabilities` and non-sensitive settings
- [x] Publish the source catalogue: machine-readable register of every endpoint and field, served by /api/v1/sources
- [x] Run a read-only survey of every catalogued path and record its outcome (class, shape, firmware) in `source_probe`, merged into `/api/v1/sources`
- [x] Read and publish the device detail families (inverter, system, network, devices, emrs) served by `GET /api/v1/detail` — a snapshot per family in `device_detail` (migration `0008_detail.sql`), the inverter family with the collector, the four diagnostics with the detector, so each family carries its own read instant; the five new readings documented in `docs/ENDPOINTS.md` § Executable allowlist and `docs/SOURCES.md` § Endpoint inventory
- [x] Read and publish the cache family (`/components/cache/readable`) served by `GET /api/v1/detail` as the sixth family — read by `internal/detect` on the detection cycle (a `4 897`-byte register dump per `5 s` cycle would be waste), stored in `device_detail` and rendered by the Device detail page's **Cache block**; the allowlist grows from eight to nine internal paths (`Cgi` envelope, `Digest: true` per the registry while the device answers `200` without a header), the device's own 44 named registers and 37 bare `<id>` are kept verbatim, and **no unit is derived from a name** (`docs/UNKNOWNS.md` #14 keeps the bare ids open)
- [x] Implement SSE or polling according to the ADR
- [x] Validate range/date/granularity parameters
- [x] Paginate and bound large requests
- [ ] Generate OpenAPI and typed TS clients
- [x] Add API+SQLite integration tests

## 5 — React/shadcn UI

- [ ] Define the design system, light/dark themes and responsive navigation
- [ ] Build the shell (sidebar, header, breadcrumb, mobile navigation)
- [x] Build the instantaneous production / energy of the day / state KPI cards
- [x] Build the 24h power curve with tooltips, units and period selection
- [ ] Build daily/weekly/monthly/yearly aggregates
- [ ] Add period comparisons and production calendar
- [x] Add inverter state, firmware, network and available diagnostics — the "Device detail" page (`/detail`) shows the device detail families with their read instant, their availability and their reason; the Overview carries the **inverter state chip**; the firmware and the identity stay on the System page
- [x] Display the cache family — the "Device detail" page's **Cache block**: the node's attributes as a list and its registers in a collapsible table, the 44 the device names with its own type suffix and the 37 bare `<id>` shown as they are, **no unit derived from a name**
- [ ] Add events and filters by date/severity
- [x] Add collector health page and last refresh
- [ ] Add settings page and read-only connectivity test
- [x] Add capabilities page (measured/estimated/unavailable/unverified)
- [x] Data sources page: states, field inventory, reasons and capability join
- [x] Hide consumption, import/export and self-consumption cards without a meter
- [x] Implement loading/error/empty/offline/stale states
- [x] Implement cache and refresh via TanStack Query
- [x] Implement responsive and accessible Recharts charts
- [ ] Implement CSV export of the histories
- [x] Add FR translation with an i18n DE/EN structure
- [x] Add keyboard navigation and WCAG AA checks
- [ ] Test responsive desktop/tablet/mobile

## 6 — Quality, deployment, operations

- [x] Unit tests for parsers, conversions and aggregations
- [x] DST, time gaps, duplicates, reboot and outlier data tests
- [x] API tests on network errors, authentication and JSON corruption
- [ ] React component tests and E2E tests on the simulator
- [x] Verify the absence of secrets in the bundle, logs, localStorage, repository
- [ ] Secure CORS, cookies, CSP and authentication if remote access
- [ ] Configure localhost/LAN bind and HTTPS proxy according to the ADR
- [ ] Compile the Go binary with embedded React assets
- [ ] Create a multi-stage Dockerfile and an optional compose
- [ ] Add volumes, healthcheck, restart policy and secret management
- [ ] Test NAS/local installation, startup and updates
- [x] Test backup and restore of the database
- [x] Document installation, configuration, maintenance and troubleshooting
- [ ] Create screenshots and a user guide
- [ ] Verify resource consumption and network load of the inverter

## 7 — Later phase (out of MVP)

- [ ] Identify the exact need for control (list of business commands)
- [ ] For each command, obtain proof of authorization and firmware documentation
- [ ] Define guardrails, confirmation and audit log of the mutations
- [ ] Provide a dry-run and a simulator of the commands
- [ ] Specific security review before any write on real hardware
- [ ] Study Smart Meter or other reliable sensor integration for grid consumption
- [ ] Add battery and energy flows only if hardware detected
- [ ] Study notifications (failure, fault, abnormal production)
- [ ] Study multi-inverters and user profiles if a real need
- [ ] Reassess the Technician recovery procedure via an authorized channel

## Log

- 2026-10-16 — Step 1: repository foundation (`.gitignore`, MIT, `CHANGELOG.md`, `.env.example`,
  `Makefile`, `docs/`), fix of `references/` in `AGENTS.md`. Commit
  `chore: init monorepo`.
- 2026-10-16 — Step 2: `go.mod` (`fronius-supervisor`, go 1.24) and simulator
  `cmd/fronius-sim`. Curve `max(0, sin(π·(h−7)/12))·nominal·cloud(t)` with smoothed noise
  derived from the seed, `?at=` (simulator extension), faults `500/503/401/404/timeout/invalid_json/
  empty/status7`, `/sim/health`. Decision: `rel_*` emitted as `null` when their denominator is
  zero (no invented zero); `P_Akku` stays `null` even with `--with-meter` because no
  battery is modeled. Verified live: `P_PV > 0` at 11:39 local, `P_Grid = null`,
  `Head.Status.Code = 0`, two `?at=` calls identical byte for byte, night at 0.
- 2026-10-16 — Step 3: `internal/config`, environment only (no configuration file
  in phase 1). `CheckListenAddress` guard: `froniusd` will refuse any non-loopback listen
  without `FRONIUS_ALLOW_REMOTE=1`. The two `TODO.md` boxes mapped to this step
  ("MVP vs phase 2 goals", "register of unknowns") depend respectively on
  ADR 0004 (step 6) and `docs/UNKNOWNS.md` (step 13): they will be checked at those
  steps, not here.
- 2026-10-16 — Step 4: `internal/fronius/froniuserr` (typed errors, `KindOf`,
  `errors.Is` by nature) and `internal/fronius/readonly.go` (`ReadOnly`: structural refusal
  of POST/PUT/PATCH/DELETE/HEAD, including a nil request and nil transport). The guard will be
  instantiated in `froniusd` and `fronius-probe`; a non-GET call never reaches the
  network.
- 2026-10-16 — Step 5: `internal/fronius/solarapi` (`dto.go` = the only place where the assumed
  shape of the JSON is encoded, `client.go` = transport + error mapping). Verified
  with `httptest`: nominal 200, 404/401/403/429/500/503, empty body, truncated JSON, HTML page,
  missing `Body`/`Data`, non-numeric `P_PV`, `Head.Status.Code = 7`, exceeded delay,
  closed server, body > 1 MiB, unknown fields tolerated, path prefix preserved.
  The item "Document GetPowerFlowRealtimeData.fcgi" remains open until `docs/SOURCES.md`
  (step 6) then the reconciliation by the probe (step 11).
- 2026-10-16 — Step 6: `internal/model` (`measurement.go`, `units.go`, `capabilities.go`,
  `event.go`) with the "no invented zero" invariant; ADR 0001 to 0007;
  `docs/SOURCES.md` (provenance + checks run on the bundle: `fcgi` 0,
  `RealtimeData` 0, `PowerFlowRealtime` 0, `solar_api` 1, `x-www-authenticate` 7,
  `AuthHash` 15); `docs/ENDPOINTS.md` (classification of the 79 paths of the bundle, allowlist
  reduced to the single Solar API GET). Model decision: `Capability.Reason` carries an
  unavailability reason when the capability is unavailable, a caveat when it is
  available but unverified (`production.*`), never both.
- 2026-10-16 — Step 7: `internal/storage`. Deviation from the plan, assumed and documented:
  `modernc.org/sqlite v1.60.1` (version pinned by the plan) declares
  `go 1.26.0`, so `go.mod` carries `go 1.26.0` instead of `go 1.24` — keeping the
  pinned driver version takes precedence over the language directive, and the 1.26
  toolchain is available locally. Consequence: the Go 1.26 idioms
  (`new(expr)`, `for range n`) are now applicable and were adopted
  (`fptr`/`iptr` of `cmd/fronius-sim` and `pf`/`pi` of the tests removed).
  Tables: `samples` (WITHOUT ROWID, key `(ts_utc, source)`), `events`,
  `collector_runs`, `collector_state`, `schema_migrations`. The materialized aggregates
  (`aggregates`) remain deferred to phase 3 by ADR 0006; the
  purge exists (`PurgeBefore`) but is not scheduled. Three anomalies
  detected by the tests and fixed: splitting of the migration statements
  before removing the comments (a `;` in a comment broke the
  migration), double scaling of the bucket start, and two test
  fixtures that did not actually cross local midnight.
- 2026-10-16 — Step 8: `internal/collector`. `Sink` includes `InsertEvent`, absent from the
  plan definition but necessary for the requested behavior (anti-duplicated warning event
  on failure, recovery event). Decisions: `last_success_utc` carries the clock of the
  **collector** (instant of the successful cycle), not the device timestamp, which remains in
  `Sample.TS`; a persistence failure (`InsertSample`) writes neither a cycle nor a failure
  event, because it is not a device failure. "Redacted logs" is satisfied by
  construction in phase 1: the backend holds no Fronius credentials.
- 2026-10-16 — Step 9: `internal/api` and `cmd/froniusd`. Assumed deviation: `/api/v1/health`
  respects the `{data, meta}` envelope like the six other routes, whereas verification
  point 11 of the plan reads `.collector.last_error` at the root; the correct
  read is `.data.collector.last_error`. Decisions: `/api/v1/live` returns 200 with `data:null` and
  `error.code=no_data` on an empty database (an empty state is not a failure, and the client must
  be able to read `no_data`); errors do not carry a `data` field; a UTC window on
  `/energy/daily` may include a partially covered local day, behavior documented in
  the handler. Full chain verified live: 34,560 backfill points in
  8 s, +1 point every ~5 s, `stale` flips after 30 s, simulator stop → 5 failed cycles without
  any invented measurement, `last_error` set, `status: degraded`, simulator restart →
  automatic recovery with a `collector_recovered` event without restarting `froniusd`.
- 2026-10-16 — Step 10: fixtures and contract tests. The four fixtures are produced by
  the simulator (seed 42, instant `2026-06-21T12:00:00+02:00`) and carry `synthetic: true`,
  `confidence: "HYPOTHESIS"`: they do not replace a real capture. The item "contract
  tests from **real** redacted responses" therefore remains open until the execution of
  `fronius-probe` by the user (step 11) and the promotion of the fixtures. The contract
  test already covers the whole chain fixture → client → `model.FromPowerFlow` → `Sample`.
- 2026-10-16 — Step 11: probe `cmd/fronius-probe` (a single GET in the allowlist, refusal of
  non-local addresses, structural redaction, `--save` in 0600) and `docs/PROBE-READONLY.md`.
  Two redaction bugs detected by the tests and fixed before delivery: Go's JSON encoder
  serialized `<redacted>` as `\u003c…\u003e`, and the permissive IPv6 pattern masked
  **timestamps** (`12:00:00` passed for an address) — IPv6 detection is now a
  function requiring four groups or the `::` form. Probe verified live against the simulator
  (`--list`, full run, `--save`); its refusal was verified against `192.0.2.10`
  **without emitting a request**. The item "GET-only tests on hardware" remains open:
  this run belongs to the user.
- 2026-10-16 — Step 12: React interface in `apps/web`. Two defects detected by the
  browser verification, not by the tests, and fixed: (1) `connectNulls={false}` was
  without effect, because a bucket absent from the response was not in the data and Recharts
  linked its neighbors — the gaps are now materialized by `null` points (limit of
  5000 points, beyond which the grid is not completed); (2) the step was deduced from the
  difference between the first two buckets, which is wrong as soon as a bucket is missing — the server
  now exposes `meta.step_s` (commit `fix(api)`). Verified live: KPIs fed by the
  simulator, 24 h curve announcing "1 point(s) without measurement" and plot cut into two
  sub-paths, Capabilities page listing the 5 unavailable ones with their reason, Settings page in
  read-only, stopping `froniusd` → "API unreachable" banner **and** "Stale data" badge
  (freshness re-judged locally when the API no longer responds), recovery after
  restart, empty state on a fresh database, keyboard navigation traversing the sidebar, and
  production build served by `froniusd` (`FRONIUS_WEB_DIR`) with SPA fallback.
  Not delivered at this step, left open: connectivity test from the Settings page,
  light/dark theme selector, breadcrumb, CSV export, E2E tests, multi-device
  responsive verification.
- 2026-10-16 — Step 13: `docs/ARCHITECTURE.md`, `DEVELOPMENT.md`, `RUNBOOK.md`,
  `UNKNOWNS.md` and `.github/workflows/ci.yml`. The root README receives a "Delivered
  software" section pointing to `docs/`. No secret in the produced bundle: the only occurrences of
  `password`/`nonce` come from React DOM (input type table, CSP nonce), `src/` contains
  none, `localStorage` is never used and no secrets file is versioned
  apart from `.env.example`. Decisions recorded: no periodic purge (restart is the only
  trigger), no filtering of outlier values as long as no real case is observed.

- 2026-10-16 — Step 14 (phase 2): authenticated reading of internal paths. Delivered:
  `internal/secret` (type `secret.String`, masked for `fmt`, `slog` and JSON),
  `internal/fronius/digest` (Digest RFC 2617: challenge, header, **one** retry on 401, nonce
  cache, decimal counter, unique `nc` under concurrency), `internal/fronius/internalapi`
  (executable allowlist of four paths + tolerant extraction of the identity),
  `internal/detect` (identity, presence, events, anti-duplication, abandoning a path
  after a 401 without credentials), migration `0002_device.sql`, three-state capabilities
  (`unknown`/`absent`/`present`), enriched `/api/v1/system` and `/api/v1/capabilities`
  endpoints, System page (identity + detection), extended probe (5 endpoints, `--endpoint`,
  `--password-stdin`, never `--password`), documentation (SOURCES, ENDPOINTS, UNKNOWNS,
  ARCHITECTURE, RUNBOOK, DEVELOPMENT, PROBE-READONLY) and CI guard "no versioned
  password".

  Checks run: `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `go test ./... -race`
  (all packages ok); exact RFC 2617 vector; 50 concurrent requests producing 50 distinct
  `nc`; simulator protected by Digest **and authenticated by a third-party client**
  (`curl --digest` → 200); end to end simulator + `froniusd`: `/api/v1/system` returns
  model, firmware and serial number from the simulated hardware, `meter.state=present`,
  `battery.state=absent`, `device.identity` and `device.events` available, `battery.soc`
  unavailable with its reason; **anti-duplication verified over ~16 cycles** republishing the same
  flow (2 `device_event` rows, 2 keys, 1 `device` row); without credentials, the four states
  move to `unknown` with a reason naming the environment variables, **four 401s on the first
  cycle and none afterwards**; no trace of the password in the `froniusd` log
  (0 occurrence) nor in the body of any of the seven routes; the search for the pattern
  "`FRONIUS_DEVICE_PASSWORD` followed by a value" returns nothing, including
  for the CI guard itself (the pattern is never written in clear text in the repository:
  it would fail this same guard).

  Two deviations from the plan, assumed and documented: (1) the `device` table stores the **three-
  valued states and their cause** (`*_state`, `*_reason`) instead of booleans — a boolean cannot
  distinguish "absent" from "undetermined", and the interface would have displayed "no Smart Meter
  detected" after a mere 401; (2) `internal/secret` implements `GoString` in addition to
  `String`, without which `%#v` bypassed the masking.

  Not delivered, left open: running the probe against the real hardware and promoting the
  fixtures (belongs to the user, see `docs/PROBE-READONLY.md`), real names of the `modelName`/`firmwareVersion` keys, shape of `/status/activeEvents`, reading of the battery state of charge,
  event filters by date/severity in the interface, OpenAPI, CSV export,
  E2E tests, screenshots.

- 2026-10-16 — Step 15 (phase 3): hourly aggregates and scheduled purge. Delivered:
  migration `0003_agg.sql` (`agg_hour`: `count_all`, `count_pv`, `sum_pv_w`,
  `min_pv_w`, `max_pv_w`, `max_energy_day_wh`), switching reads at hourly or daily step
  to these aggregates as soon as the range is whole and closed,
  invalidation of the water mark on write (a history imported under the
  mark is picked up by the next pass), purge of the aggregates together with that of the
  measurements, and a new `internal/maintenance` package that schedules the aggregation
  (every 5 min, catch-up at startup) and the purge (at startup then
  every day from 03:00, `FRONIUS_RETENTION_DAYS=0` disabling it).
  The "retention and scheduled purge" item of section 4 is thus
  completely satisfied (ADR 0006 deferred it to this phase); `make
  test-perf` replays the exit criterion.

  Checks run: `go vet ./...`, `CGO_ENABLED=0 go build ./...`,
  `go test ./... -race` (all packages ok); raw/aggregate equivalence
  verified on a data set covering an empty hour, absent values,
  a header in the middle of an hour, the tail in the open hour and a
  local hour transition (23 h) — buckets, counts and minimum/maximum
  identical, averages equal to within 1e-9 relative; at the API level, payload
  **identical byte for byte** before and after aggregation on
  `/timeseries?step=1h`, `step=1d` and `/energy/daily`; anti-regression on the
  water mark (lowering on write, never an open hour aggregated, no
  mark written on an empty database, purge consistent between the two tables);
  measurement over one year (6,307,200 measurements inserted in 22.7 s): 1 h step **14 ms**
  against 5,168 ms, daily energy **5 ms** against 5,535 ms, 1 d step 7 ms,
  with 8,760 and 366 buckets identical in both paths — criterion of
  ADR 0006 (< 200 ms) largely met.

  Deviation from the plan, assumed and documented in the ADR: no daily table is
  created. Grouping by local date is done in Go from the hourly table
  (8,760 rows for a year, 5 ms), so a second table would only bring an additional
  synchronization and purge to test. Two design points not planned by the plan
  and necessary for correctness: the read serves an hour from the aggregates
  only if it is **whole and contained** in the requested range (an aggregate covers
  the full hour, the raw read only sees `[from, to)` — serving a partial hour from
  an aggregate would invent measurements), and the purge bound is aligned with the hour so that
  measurements and aggregates share the same boundary. Not delivered, left open:
  same items as step 14 (hardware probe and promotion of the fixtures,
  real identity keys, shape of `/status/activeEvents`, battery state of charge,
  event filters, OpenAPI, CSV export, E2E tests, screenshots).

- 2026-10-16 — Step 16 (capture session, run by the user): the probe was executed against
  the hardware (firmware `1.19.7-1`) and its answers became the reference. Promoted as
  fixtures and consumed by two contract tests: `test/fixtures/solarapi/powerflow_produce_only.json`
  (`internal/fronius/solarapi/contract_test.go`) and the five internal bodies
  `status_version`, `status_activeevents`, `status_powerflow`,
  `components_powermeter_readable`, `components_batterymanagementsystem_readable`
  (`internal/detect/contract_test.go`). Four registers of unknowns were settled by the
  capture: #1 (shape of `GetPowerFlowRealtimeData`, two `[HYPOTHESIS]` divergences —
  `Meter_Location` is a string, `E_Day`/`E_Year` are **null**), #4 and #5 (no Smart Meter,
  no battery: an **empty** `Body.Data` on both `/components/*/readable`), and #13 (three
  candidate sources for the day and year counters — the flow endpoint, the internal
  `/status/powerflow` and the documented accumulation collection — all `null`, only the
  total is live). Consequences: the internal `/status/powerflow` **fallback was removed**
  (clean cutover: allowlist back to four entries, `samples.fallback_source` dropped by
  migration `0006`), and the two quantities are now **derived** from our own stored total
  (option (b), `internal/api/derived.go`, commit `2489ce4`). Boxes ticked at this step:
  "Identify the metrics actually returned by the Solar API", "Confirm the absence of
  Smart Meter and battery", "Document `GetPowerFlowRealtimeData.fcgi` and its fields" and
  "Add contract tests from the real redacted responses". Left open: the route guard
  `oi.a` audit, the accumulation answer's `DeviceStatus` (recorded as a candidate, see
  `docs/UNKNOWNS.md` #13), the daily chart derivation, OpenAPI, CSV export, E2E tests.

- 2026-10-16 — Step 17: audit of the route guard, the last open item of phase 1. The 37 guarded
  routes of the AppModule array carry `canActivate: [oi.a]` (the lazy `ji.a.solarWeb` route carries
  none) and the bundle contains **one** `CanActivate` definition (`canActivate(t, e)`, l. 81341, module
  `euqz`, l. 81184-81388): the `oi` → `euqz` binding is an `[INFERENCE]` (the import binding lies
  outside the readable window; three concurring clues recorded). The guard is **not a role gate** — it
  chains four middlewares (`_updateMiddleware` l. 81311 … `_defaultMiddleware` l. 81325) that yield
  `true`, `false` or a redirect `UrlTree` (`/update`, `/exchange`, `/dashboard`, `/startPage`) — its role
  part reads a client-side menu permission (`_mapLoggedInUser` l. 81366-81374 → `isUserPermittedForUrl`
  l. 43732 → the three-valued `Wfm5` enum at l. 70078), and a refusal **cancels the navigation** (the
  observable never emits) rather than answering 403. The per-route minima are a client table
  (`H8nL`, `technician` for `DEVICE_CONFIGURATION`, `customer` for `INVERTER`) and the role flags are
  expanded from the login string (map `D`, l. 77898-77905). The only authorization-like value the client
  reads **from the device** is `writePermission` (l. 69202), compared field by field — unknown #6 stays
  open: a payload boolean is not a matrix, and the executable authorization remains the code allowlist.
  Recorded in `docs/ENDPOINTS.md` § Declared roles (client only) and `docs/UNKNOWNS.md` #6.
  Remaining open boxes at that point: `backup/restore`, `OpenAPI`, the phase-5/6 interface
  and packaging work (the backup/restore pair is delivered at step 18).

- 2026-10-16 — Step 18 (phase 4/6): database backup and restore. Delivered:
  `internal/storage/backup.go` — `Store.Backup` (a snapshot taken with SQLite's
  `VACUUM INTO` **through the application's own connection**, so it is consistent
  while the collector writes, compacted and standalone with no WAL sidecar to
  carry along; built under `<dest>.partial-*` and renamed only once complete; an
  existing destination is refused with `ErrBackupExists` rather than
  overwritten), `Store.Restore` (the whole persisted state — `samples`, `events`,
  `collector_runs`, `collector_state`, `device`, `device_events_seen`,
  `agg_hour` — replaced **in one transaction**, after the attached backup passes
  `PRAGMA integrity_check`, carries **exactly** the migration set applied here and
  shows matching row counts before the commit; a restore that did not carry
  everything over rolls back rather than reporting success; the aggregation
  watermark lives in `collector_state`, so the aggregates travel with it),
  `VerifyBackup`, `Inspect` (`mode=ro`) and `BackupInfo` (size, migration count,
  samples, events, aggregated hours, first/last sample, written instant). New
  command `cmd/fronius-backup` (`save -out <file> [-db <path>] [-tz <zone>]`,
  `verify -file <file>`, `restore -from <file> -yes [-db <path>] [-tz <zone>]`),
  the database path and timezone defaulting to `FRONIUS_DB_PATH` / `FRONIUS_TZ`;
  `restore` refuses without `-yes` and first keeps the current database aside as
  `<db>.pre-restore-<UTC instant>.db`. `Makefile` targets `make backup` and
  `make restore` (`FRONIUS_BACKUP=path` to choose the file). Documentation:
  `docs/RUNBOOK.md` (operator section: the two commands, what a snapshot holds,
  "stop the collector first", the automatic safety copy, `verify` before trusting
  a file, and that a snapshot may be taken while the application runs),
  `docs/ARCHITECTURE.md` (`cmd/fronius-backup` in the package table and a new
  invariant 10) and `docs/DEVELOPMENT.md` (the two make targets and the three
  subcommands).

  Checks run: `go test ./... -race` green, `gofmt` and `go vet ./...` clean.
  Tests: `internal/storage/backup_test.go` (round trip read back by a second
  store; every table carried, including the aggregate watermark and the device
  event keys; the destination is never overwritten; a failed backup leaves
  nothing behind; a restore replaces and does not merge; refusals of a file that
  is not a database, of a database with another schema and of an absent file) and
  `cmd/fronius-backup/main_test.go` (the three subcommands through `main1`;
  refusal without `-yes`; save over an existing file; verify on junk; usage
  errors). Verified live against a database written by a **running** `froniusd`
  (simulator on 8081, WAL): `save` during collection carried **9 samples**
  (2026-10-16T17:46:41Z → 17:46:50Z) and the event, and `verify` reported the
  same; after the database reached **23 samples**, stopping the daemon then
  `restore -yes` brought it back to the **9 of the snapshot** and kept
  `live.db.pre-restore-20261016T174711Z.db` holding the **24** samples that were
  replaced; `froniusd` restarted on the **restored** database collected again
  (9 → 17 samples) and `/api/v1/health` answered `status: ok`, `samples: 17`.
  Boxes checked at this step: "Implement tested backup/restore" (phase 4) and
  "Test backup and restore of the database" (phase 6). No deviation from the
  plan; not delivered: no scheduler — backups stay manual, though a `save` may run
  while the service runs.

- 2026-10-16 — Step 19 (phase 4/5): source catalogue. Delivered:
  `internal/sources/catalog.go` — the machine-readable register of every known
  source, with `type Transport` (`solar_api` | `internal_api`), `type State`
  (`called` | `captured` | `candidate` | `archived`), `type FieldStatus`
  (`displayed` | `stored` | `validated` | `ignored` | `unverified`), the types
  `Field`, `Proof` and `Source`, and `Catalog()`, `Called()`, `ByID()`, `IDs()`,
  `Summarize()` (counters by state and by field status). Semantics recorded: a
  source's `Capability` is the capability it feeds (today if `Called`, otherwise
  the one it would feed); `WouldUnlock` names a capability that **does not exist
  yet**; a field's `Capability` must exist or be a candidate the catalogue
  introduces. It holds the Solar API families (the called
  `GetPowerFlowRealtimeData.fcgi`, the accumulation collection captured through
  the probe, and `CommonInverterData`, `3PInverterData`, `GetInverterInfo`,
  `GetLoggerInfo`, `GetMeterRealtimeData`, `GetStorageRealtimeData`,
  `GetActiveDeviceInfo`, `GetDeviceInfo`, `GetStringRealtimeData`,
  `GetSensorRealtimeData`, `GetArchiveData`, `/solar_api/GetAPIVersion` as
  candidates — the phantom `GetDeviceInfo` was **removed** and the suffixes
  **corrected** at step 24) and the internal API (the four allowlisted paths called; the five
  promoted captures; `/status/powerflow` archived after the clean cutover;
  `/status/common`, `/status/devices`, `/status/network`, `/status/emrs`,
  `/components/cache/readable`, `/components/Ohmpilot/readable`,
  `/commands/GetUTCTime`, `/commands/GetServiceState` as candidates from the
  bundle extraction). Field-level facts named: `Body.Data.Site.E_Day`/`E_Year`
  unverified (null on this firmware, the energy is derived);
  `Body.Data.Site.P_Grid`/`P_Load`/`rel_*`/`P_Akku` unverified (no meter, no
  battery); `Body.Data.DeviceStatus.InverterState`/`StatusCode`/`ErrorCode`
  unverified with `would_unlock: inverter.state` (captured, never read);
  `/status/version` exposes `apiversions`, `hwrevisions` (three board
  revisions), `commonName`, `hardwareId`, `minAppVersions` and eleven
  `swrevisions` components where only `GEN24` is read; `/status/activeEvents` is
  a **bare root JSON array** (not an envelope) whose `uuid`, `prefix`+`eventID`,
  `severity` and `label` are used and whose other keys are kept verbatim in the
  raw detail. New route `GET /api/v1/sources` (`internal/api/sources.go`)
  returns `{data: {summary, sources}, meta: {generated_at}}`, registered in
  `internal/api/server.go` like the seven other routes. Frontend:
  `apps/web/src/routes/Sources.tsx`, the "Data sources" page (summary strip,
  filters, one block per source with proofs/capabilities/docs, expandable field
  inventory, legend).

  Tests: `internal/sources/catalog_test.go` binds the catalogue to the registers
  (unique sorted ids, `GET` only, every ignored/unverified field carries its
  reason, every capability id exists or is a candidate the catalogue introduces,
  the `called` set equals the code allowlist plus the one Solar API path, every
  declared fixture exists on disk, every Solar API path of `docs/SOURCES.md`'s
  candidate table is held in both directions, every internal path appears in
  `docs/ENDPOINTS.md`, the summary counters agree with the list, and a caller
  cannot mutate the package's data); `internal/api/sources_test.go` covers the
  payload, the counters, the three states, the called set and `405` on `POST`.
  Documentation: `docs/SOURCES.md` and `docs/ENDPOINTS.md` point at the catalogue
  as the machine-readable form of the provenance registers, `docs/ARCHITECTURE.md`
  (package row, frontend route, invariant 11), `docs/RUNBOOK.md` (one `curl`).
  Boxes checked at this step: "Publish the source catalogue" (phase 4) and
  "Data sources page" (phase 5). No deviation from the plan; the catalogue is
  data — it touches neither the network nor the database, and it adds nothing to
  the allowlist or the simulator.
- 2026-10-16 — Step 20 (phase 4/5): read-only survey of the whole catalogue.
  Delivered: `cmd/fronius-probe --survey` walks the whole catalogued allowlist
  (27 paths), **one `GET` per path**, sequentially with `--delay` between paths
  (default `1s`), and records the outcome of each — class (`ok`, `empty`,
  `not_json`, `auth_required`, `not_found`, `error`), HTTP status,
  `digest_required`, bytes, error, firmware and the **shape** of the answer
  (JSON paths and types, arrays by length with element paths suffixed `[]`,
  bounded to 500 entries and 8 levels with an explicit `(truncated)` marker) —
  **never a value**. New flags `--delay` and `--db` (default
  `$FRONIUS_DB_PATH`; empty writes the report only); `--survey` refuses to
  combine with `--endpoint`. Each path is read **bare** first — reachable
  without a header is a fact — and a `401`/`403` **with** credentials triggers
  one second, authenticated read; a `404` or a `401` is a **result** and never
  aborts the run. The probe's allowlist is now **derived from
  `internal/sources`** (default: the `called`/`captured` sources, six entries;
  `--survey`: the whole catalogue; `--endpoint`: any catalogued path), so a path
  cannot be probed without being catalogued and the registers cannot drift.
  Output: one line per path, a summary per class, the firmware discovered on the
  identity path, and with `--save` a `survey_<UTC instant>.json` report written
  `0600`. Storage: migration `0007_probe.sql`, table `source_probe` keyed by
  `(path, query)` (the query is part of the identity of a path; a survey replaces
  its previous row) with `SaveSourceProbes` / `SourceProbes`, carried by
  `Store.Restore` (added to `backupTables`) and purged by `PurgeBefore` under the
  same bound as the measurements. `GET /api/v1/sources` merges the catalogue with
  the last survey: a `survey` block (`probed`, one counter per class,
  `last_probed_at`, `firmware`) and a `probe` object per surveyed source (class,
  `http_status`, `digest_required`, `bytes`, `error`, `firmware`, `probed_at`,
  `fields`) — a source never surveyed has **no** `probe` key, so the interface
  says "never surveyed" rather than showing zeros. Tests:
  `cmd/fronius-probe/survey_test.go` (the classes stay distinct; the inventory
  carries no value; arrays walked through their first element; the bound and its
  marker; both observed firmware shapes; a full survey against a stub device
  writing the report *and* the database; the authenticated second read) and
  `internal/api/sources_test.go` (the merge, the counters, the path+query key,
  the never-surveyed case). Documentation: `docs/PROBE-READONLY.md` (survey mode,
  a new § 3), `docs/RUNBOOK.md` (operator procedure and class reading table),
  `docs/SOURCES.md` (the survey as per-firmware evidence), `docs/ARCHITECTURE.md`
  (`source_probe` in the storage row, the probe row, the survey in invariant 11),
  `CHANGELOG.md`. Box checked at this step: "Run a read-only survey of every
  catalogued path and record its outcome" (phase 4). No deviation from the plan;
  the survey is a manual diagnostic — it is never started by the service or by
  CI, and it adds nothing to the application's own executable allowlist.

- 2026-10-16 — Step 21 (phase 4): the first real survey of the reference
  installation. Run by the user against `http://192.0.2.10`, firmware
  `1.19.7-1`, with **no credentials**:
  `./bin/fronius-probe --survey --base-url http://192.0.2.10
  --allow-non-loopback --db data/fronius.db --save ./probe-survey` — 27
  catalogued paths, one `GET` each, 1 s apart. Outcome: **11 `ok`, 4 `empty`,
  2 `auth_required`, 10 `not_found`**; report
  `probe-survey/survey_20261016T191253Z.json` (in the working tree, untracked),
  rows in `data/fronius.db` (table `source_probe`). What answered: the
  `GetPowerFlowRealtimeData` and the `CommonInverterData`/`CumulationInverterData`
  collections, and the internal `/status/version`, `/status/activeEvents`,
  `/status/powerflow`, `/status/common`, `/status/devices`, `/status/network`,
  `/status/emrs`, `/components/cache/readable`. What did not: `3PInverterData`
  (empty, as the single-phase Primo predicted), the meter/battery/Ohmpilot
  readables (empty — consistent with the hardware), `/commands/GetUTCTime` and
  `/commands/GetServiceState` (`401` without credentials), and ten Solar API
  paths as `404`. **Settled:** `GetArchiveData.cgi` is `404` ⇒ the daily chart
  cannot be filled from device-side history (the aggregate/derived route is the
  only one, `docs/UNKNOWNS.md` #13); `GetInverterInfo.fcgi`/`GetLoggerInfo.fcgi`
  are `404` ⇒ the identity stays on the Digest-protected internal API, the
  "identity from the Solar API" idea is closed (#3); `CommonInverterData` and
  `CumulationInverterData` are served, so the electrical detail and the inverter
  `DeviceStatus` are available on this device (candidates, not wired);
  `/solar_api/GetAPIVersion` is `404` and stays an **open candidate** with no
  reason asserted. **Corrected at step 24:** those Solar API `404`s were the
  probe's own wrong `.fcgi` suffix, not a firmware fact — the identity **is**
  available from the Solar API (`GetInverterInfo.cgi`, `ok`, no credentials) and
  `GetAPIVersion.cgi` answers `ok`; only `GetArchiveData.cgi` remains `404`
  (§5.1.1, GEN24/Tauro/Verto **NEVER**). Documentation: `docs/SOURCES.md` (the run under Hardware
  captures and the candidate-table statuses), `docs/UNKNOWNS.md` (#3, #13, and
  the survey as per-firmware evidence), `docs/PROBE-READONLY.md` (§ 3 result
  line), `CHANGELOG.md` (the chart cannot come from the device). **No box is
  ticked by this step** — the survey tool itself was delivered and checked at
  step 20 — and none the run closes is re-opened.

- 2026-10-16 — Step 22 (phase 4/5): the device detail. The survey of step 21
  observed five more paths, and this step reads and publishes them. **Solar API**:
  `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CommonInverterData`
  (constant `solarapi.CommonQuery`), read by `internal/collector` **every
  collection cycle** — its `DeviceStatus` and its thirteen measurements; the
  query is a constant of the binary, so the read-only surface stays the same.
  **Internal API**: `/status/common`, `/status/network`, `/status/devices`,
  `/status/emrs`, taking the allowlist from **four to eight paths** (all `READ`,
  envelope `Default` — bare JSON, no Body/Head; `Digest: true` per the polled
  registry while the device answered them `200` without a header), read by
  `internal/detect` on the detection cycle (`FRONIUS_DETECT_INTERVAL`). **Two
  cadences, one table**: migration `0008_detail.sql`, table
  `device_detail(family PRIMARY KEY, read_at_utc, available, reason, data_json)` —
  one row per family (`inverter`, `system`, `network`, `devices`, `emrs`),
  replaced on each read, a **snapshot** and not a series; each family carries its
  **own** instant. Carried by `Store.Restore` (added to `backupTables`), **not**
  purged with the retention (state, like `device`, not history). New route
  `GET /api/v1/detail` (`{read_at, families: […]}`) keeps **three** states
  distinct: available · unavailable **with its reason** · never read (**absent**);
  the inverter measurements are an **ordered** list with a `label`, a `unit` and
  a nullable `value` (`null` = the device reported nothing, never a zero). The
  model invariant holds: `DAY_ENERGY`/`YEAR_ENERGY` are `null` on the reference
  firmware and published as null; `IDC_3`/`UDC_3` are null (inputs not
  connected). Models: `internal/model/detail.go`,
  `internal/fronius/internalapi/status.go`,
  `internal/fronius/solarapi/realtime.go`. The catalogue follows: the common
  collection and the four diagnostics move from `candidate` to **`called`**
  (`Called: true`), their `would_unlock` gone, `/status/common`'s `updateRunning`
  now `displayed`. `cmd/fronius-sim` serves the four families in the observed
  shapes (bare JSON; `network` with one `eth0` on documentation-range
  `192.0.2.x`). `cmd/fronius-probe`: `--endpoint` accepts a **catalogue
  identifier** or a **path with its query** (three collections share the
  realtime-data path; selecting the path alone silently probed the first, which
  is what the device run of step 21 did — the accumulation was captured twice),
  and the saved file name carries the query. Frontend: the **"Device detail"**
  page (`/detail`) and the **inverter state chip** on Overview. Tests:
  `internal/storage/detail_test.go`, `internal/api/detail_test.go`,
  `apps/web/src/routes/Detail.test.tsx` (new) and the extended collector,
  detector, probe, simulator and Overview tests. Documentation:
  `docs/ARCHITECTURE.md` (the detail snapshot, the two cadences, the package and
  frontend rows), `docs/ENDPOINTS.md` (allowlist at eight internal paths, the
  common collection called, the matrix), `docs/SOURCES.md` (endpoint inventory),
  `docs/PROBE-READONLY.md` (default set of eleven, the selector, the file name),
  `docs/RUNBOOK.md` (§ Device detail), `docs/UNKNOWNS.md` (#6: the account
  booleans are the device's **declaration**, never a permission; #13: the
  inverter state is now read), `CHANGELOG.md`. Boxes ticked at this step: "Read
  and publish the device detail families" (phase 4) and "Add inverter state,
  firmware, network and available diagnostics" (phase 5). No deviation from the
  plan; the reads are `GET` only and each addition to the allowlist is justified
  by the survey (ADR 0008).

- 2026-10-16 — Step 23 (phase 4/5): the readable cache. The survey of step 21
  observed `/components/cache/readable` answering `200` with `4 897` bytes and
  `99` values, and this step reads and publishes it. **Allowlist**: the internal
  list grows from **eight to nine** paths — `READ`, envelope `Cgi` (its
  `Body.Data` is keyed by node id), `Digest: true` per the polled registry while
  the device answered it `200` without a header — justified by the survey like
  the other additions (ADR 0008). **Cadence**: read by `internal/detect` on the
  detection cycle (`FRONIUS_DETECT_INTERVAL`), not by the collector: at `4 897`
  bytes a register dump on every `5 s` collection cycle would be waste. **Shape**
  (firmware `1.19.7-1`): one node (`393216`) with ten attributes (`[ENABLE]`,
  `[VISIBLE]`, `createTS`, `note`, `inverter.devicetype`,
  `inverter.devicetree`, `inverter.nodetype`, `inverter.pmc.0`/`1`/`2`) and 81
  registers — 44 named by the device with its own type suffix (`_F32` ×27,
  `_U64` ×11, `_U16` ×6, e.g. `PV_VOLTAGE_MEAN_01_F32`,
  `ACBRIDGE_ENERGYACTIVE_PRODUCED_SUM_01_U64`) and 37 bare `<id>` identifiers the
  device names nowhere. `internal/fronius/internalapi/cache.go` (+
  `cache_test.go`) keeps the device's vocabulary: the key verbatim, name and
  type suffix split out as `name`/`kind`, the value as the device wrote it
  (`json.RawMessage`, so a 64-bit counter is not rounded through a float64), the
  bare ids kept as `<id>` with no invented name, channels ordered named first;
  **no unit is derived from a name**. Models: `internal/model/detail.go` — the
  sixth family `cache` (`CacheDetail`/`CacheNodeDetail`/`CacheAttribute`/
  `CacheChannel`), stored in `device_detail` like the others and published by
  `GET /api/v1/detail`. Catalogue: `internalapi.cache` moves from `candidate` to
  **`called`**, with the note that no unit is asserted; **no fixture is
  promoted**. `cmd/fronius-sim` serves the cache shape too. Frontend: the Device
  detail page's **Cache block** (attributes list + collapsible registers table).
  Also in this change, outside the cache: `make dev-device
  DEVICE=http://<inverter>` runs `froniusd` against the inverter instead of the
  simulator — a whole-page `404` on the Device detail page is the signature of a
  daemon pointed at the development simulator. Documentation:
  `docs/ARCHITECTURE.md` (the cache family, its cadence, the package rows),
  `docs/ENDPOINTS.md` (allowlist at nine internal paths, the matrix),
  `docs/SOURCES.md` (the observed shape under Hardware captures and the endpoint
  inventory), `docs/UNKNOWNS.md` (#14: the bare ids), `docs/RUNBOOK.md` (the
  Cache block, `make dev-device`), `docs/DEVELOPMENT.md` (the `make dev-device`
  row), `CHANGELOG.md`. Boxes ticked at this step: "Read and publish the cache
  family" (phase 4) and "Display the cache family" (phase 5). No deviation from
  the plan; the read is `GET` only.

- 2026-10-16 — Step 24 (phase 1): the Solar API specification, the URL
  correction and the second survey. The manufacturer's official specification
  **`42,0410,2012,EN 02115052025`** (`references/42,0410,2012.pdf`, 91 pages,
  Solar API V1) became the **primary source** of the Solar API URL list and was
  added to `docs/SOURCES.md` with its own section (what it settles: the URL list,
  the null day/year counters, the 5-minute total, the pacing, the disable
  behaviour, the archive NEVER, the `StatusCode` table). It proves that **only
  `GetPowerFlowRealtimeData` is `.fcgi`** and the twelve other requests are
  `.cgi`: the eight paths the catalogue had probed with `.fcgi` (their `404`s
  read as "this firmware does not serve them") are **corrected** in
  `internal/sources/catalog.go`, and the phantom `GetDeviceInfo` (absent from the
  specification) is **removed**; every catalogue entry now carries a `[DOC]`
  proof citing the document number and the section. Two endpoints the
  specification has and the catalogue lacked were **added**
  (`/solar_api/v1/GetLoggerLEDInfo.cgi`, `/solar_api/v1/GetOhmPilotRealtimeData.cgi`).
  The **second survey** of the reference installation (2026-10-16, firmware
  `1.19.7-1`, `--delay 4s` per §2.6, report
  `probe-survey/survey_20261016T203255Z.json`) is now the observed reality:
  **14 `ok`, 11 `empty`, 2 `auth_required`, 1 `not_found`** — the three identity
  and inventory paths answer `ok`, the seven component candidates answer `empty`,
  and the **only** `not_found` is `GetArchiveData.cgi` (§5.1.1: GEN24/Tauro/Verto
  is **NEVER** supported for it). Documentation updated only, no code:
  `docs/SOURCES.md` (the spec section, the observed inventory, the retired
  claims), `docs/UNKNOWNS.md` (#3 second source, #13 documented nulls and archive
  NEVER), `docs/ENDPOINTS.md` (the Solar API URL list, the corrected/added/removed
  paths), `docs/PROBE-READONLY.md` (`--delay 4s`, the 28 paths, the second-survey
  result), `docs/RUNBOOK.md` (the disabled-API cause of a `404`, the 5-minute
  `TOTAL_ENERGY`), `docs/ARCHITECTURE.md` (the Solar API read budget, two reads
  per cycle), `CHANGELOG.md`. Boxes already ticked: "Identify the metrics actually
  returned by the Solar API" and "Document `/solar_api/v1/GetPowerFlowRealtimeData.fcgi`"
  (phase 0/1); "Extract other documented and useful Solar API endpoints" (phase 1)
  is now complete with the specification as the source.

  **Open opportunities (recorded, NOT implemented):** the identity could come
  from `/solar_api/v1/GetInverterInfo.cgi` **without credentials** (537 bytes,
  12 fields) — the Solar API-first route of ADR 0003/0008, a **second source**
  beside `/status/version` (`docs/UNKNOWNS.md` #3); and
  `/solar_api/v1/GetActiveDeviceInfo.cgi` (494 bytes, 7 fields, the live device
  inventory with serials) could source presence detection. Both are `[DOC]`-proven
  and observed; neither is wired.
