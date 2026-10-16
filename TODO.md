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
- [ ] Implement tested backup/restore
- [x] Implement `/api/v1/health` and `/api/v1/system`
- [x] Implement `/api/v1/live` and `/api/v1/timeseries`
- [x] Implement `/api/v1/energy/daily` and `/api/v1/events`
- [x] Implement `/api/v1/capabilities` and non-sensitive settings
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
- [ ] Add inverter state, firmware, network and available diagnostics
- [ ] Add events and filters by date/severity
- [x] Add collector health page and last refresh
- [ ] Add settings page and read-only connectivity test
- [x] Add capabilities page (measured/estimated/unavailable/unverified)
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
- [ ] Test backup and restore of the database
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
  Remaining open boxes: `backup/restore` (tested), `OpenAPI`, the phase-5/6 interface and packaging work.
