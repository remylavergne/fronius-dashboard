# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) and the
project adheres to [semantic versioning](https://semver.org/lang/fr/).

## [Unreleased]

### Added

- 2026-10-17 — **The shell is finished with light/dark themes.** A three-mode
  toggle in the header (`system` → `light` → `dark`, `system` by default) applies
  `.dark` on the root, sets `color-scheme` so native controls follow, and updates
  `theme-color` (`#ffffff` / `#171717`). The choice persists in `localStorage`,
  other tabs follow through a `storage` listener, and the system mode reacts live
  to the OS. A tiny inline script applies the stored/system theme **before** the
  bundle, so there is no flash of the wrong theme. Verified in a real browser:
  system + OS-dark flips the class and background live, an explicit light
  overrides the OS, and both explicit modes survive a reload. Contrast was
  computed, not guessed (foreground/background 18.96:1, muted 6.91:1, destructive
  badge 4.63:1); no token needed changing.
- 2026-10-17 — **An OpenAPI contract, generated TypeScript types, and a real
  favicon.** `api/openapi.json` (OpenAPI 3.1) documents every registered route
  and DTO. `internal/api/openapi_test.go` fails when a DTO field or a registered
  path is missing from it, when a documented path answers 404, or when a schema
  property is neither typed nor explicitly exempted — proven by deleting a field
  and renaming a path, both of which fail with a precise message. The frontend
  types are generated from the contract (`openapi-typescript`, `make api-types`,
  the file committed so a checkout typechecks without codegen), and the zod
  schemas are tied to those types through a compile-time contract registry: a
  property present on one side only is a `tsc` error. Eight nullability
  corrections went into the spec from the Go struct tags — a pointer with
  `omitempty` is absent, never `null`; a pointer without it is nullable. The Vite
  scaffold bolt favicon is replaced by a sun mark on the primary tile, with a
  180-px Apple touch icon and `theme-color` (legibility checked by pixels: 7.8%
  amber on a 76.6% dark tile at 16 px).
- 2026-10-17 — **A stored timestamp that does not parse now costs its row, not
  the read.** The reader accepts the canonical layout *and* RFC 3339 without
  milliseconds or with an offset (normalised to UTC), so an older or hand-written
  row still reads instead of failing the whole journal. Anything truly
  unparseable is skipped, counted, and logged once per query —
  `skipping rows with an unreadable timestamp table=events skipped=1 first=…` —
  so `/api/v1/events` and its CSV answer `200` with the other rows instead of
  `500`. The same rule covers `agg_hour` buckets and the SQL-side bucket loops;
  single-row records (device, run, health) degrade to the zero value. Found while
  exercising the CSV export, where one hand-inserted row blinded every read.
- 2026-10-17 — **The interface embedded in the binary, credentials from files,
  and NAS guidance.** `//go:embed` of `internal/webui/assets`: the build copies
  `apps/web/dist` there, `webui.FS()` reports it only when it holds an
  `index.html`, and the server prefers `FRONIUS_WEB_DIR` → embedded → API-only.
  The image no longer ships an asset directory, and a missing path that looks
  like an asset answers the API's JSON 404 instead of the SPA index.
  `FRONIUS_DEVICE_USER_FILE` / `FRONIUS_DEVICE_PASSWORD_FILE` read a credential
  from a file (trailing CR/LF stripped, `_FILE` wins, unreadable is a startup
  error naming the variable and path, never the content) — the mechanism Docker
  secrets use; the compose block is documented, not enabled. `docs/NAS.md` covers
  image transport, local-storage-only databases (never CIFS/NFS), retention
  (~4 MB/day), autostart, updates and the port boundary — and says it is
  untested on a NAS.
- 2026-10-17 — **Container packaging.** A multi-stage `Dockerfile` (Node builds
  the interface, Go builds the four binaries with `CGO_ENABLED=0`, a minimal
  `alpine` runtime carries them as a **non-root** user with `tzdata`,
  `ca-certificates`, a `/data` volume and a `HEALTHCHECK` on `/api/v1/health`),
  plus a `docker-compose.yml` with two profiles: `app` for a real installation
  (fails fast when `FRONIUS_SOLARAPI_URL` is unset) and `demo` for the simulator
  with a day of synthetic history on its own volume and port. The published
  port is `127.0.0.1` only — the container must bind `0.0.0.0` and therefore sets
  `FRONIUS_ALLOW_REMOTE=1`, which moves the boundary of ADR 0005 to the
  publication and is documented as such. `make docker` builds the image with the
  git revision stamped in (`-X main.buildVersion`). ADR 0007 amended: packaging
  delivered.
- 2026-10-17 — **Screenshots and a user guide.** `docs/USER-GUIDE.md` walks the
  ten screens — Overview, Energy, Events, Device detail, System, Capabilities,
  Settings with a **completed connectivity test**, Data sources, and the mobile
  pair (page and navigation drawer) — each with alt text and what to look at.
  `docs/screenshots/` holds the PNGs, captured from the rendered application
  against `cmd/fronius-sim` with a two-day backfill (1440×900 desktop, 390×844
  mobile, default theme) and verified for content before each shot. The guide
  states the synthetic provenance and repeats the honesty rules: unverified
  paths, absent versus zero, no write access, and what the connectivity test does
  not prove. The capture procedure is in `docs/DEVELOPMENT.md`.
- 2026-10-17 — **The application shell.** A real structure replaces the static
  header and the flat wrapped link list: a **sidebar** on desktop (brand, then
  the three groups — Monitoring, Device, Application — fed by a single
  `NAV_ITEMS` table so order and labels cannot drift, the active link marked with
  `aria-current="page"`), a **sticky header** carrying the **breadcrumb** and the
  mobile trigger, a **skip link** to `#main`, and a **mobile drawer** rendered
  only while open (`role="dialog"`, `aria-modal`, closed by Escape, the overlay or
  a chosen link; focus moved to the close button and returned to the trigger; body
  scroll locked and restored, including on unmount). One navigation landmark per
  viewport, no second `<h1>` on any page, no new dependency.
- 2026-10-17 — **CSV export of the histories.** `?format=csv` on
  `/api/v1/timeseries`, `/api/v1/energy/daily` and `/api/v1/events` — the same
  parameters and the same data as the JSON routes, streamed through
  `encoding/csv`, with `Content-Disposition` and `Cache-Control: no-store`.
  Columns are the JSON field names in order; **a missing value is an empty cell**
  (never `0`, never `null`); and text cells are neutralised against spreadsheet
  formula injection — a leading `=`, `+`, `-`, `@`, TAB or CR gets a `'`, while
  numeric cells are untouched so a negative number keeps its sign (event messages
  and derived reasons come from the device). Bounds are the JSON routes' own
  (`maxRangeDays`, the step, the events' `1..500` limit), so an invalid range
  still answers the JSON `400` before a single CSV byte. The Events, Energy and
  Overview pages gain a plain download link (`<a href download>`, no fetch)
  carrying the current range/limit/step; the Events page states that its
  client-side severity and kind filters are not part of the file.
- 2026-10-17 — **The Settings page, and a read-only connectivity test.** The
  page now shows the whole effective configuration — including the detection
  interval, the HTTP timeout and whether device credentials are configured
  (**never** a value, never a masked one) — and carries a **Test the
  connection** button. `GET /api/v1/connectivity` sends exactly **two `GET`s** to
  the device and nothing else: the unauthenticated Solar API powerflow request
  and `/status/version` with the configured credentials (the second probe is
  `skipped`, never attempted, without credentials). Each probe reports its state
  (`ok`, `skipped`, `unauthorized`, `absent`, `timeout`, `unreachable`),
  its HTTP status when one was received, its duration, and a classified `reason`
  — never a body. The verdict is `ok` when every probe is `ok` or `skipped`,
  `failed` when the Solar API probe fails, `partial` otherwise. The answer is
  cached **10 s** (`cached: true`, original `tested_at`) so a wrong password
  pressed repeatedly cannot hammer the device's authentication counter, and every
  JSON response now carries `Cache-Control: no-store` so a re-test is never
  served from the browser cache. The page states what the test proves —
  reachability and credential acceptance for those paths — and what it does not:
  no write access, and a role is a display rule, not a firmware permission.
- 2026-10-16 — **The device's own clock and the state of its grid-limits
  self-test, read end to end.** The two remaining read-only internal paths
  (`/commands/GetUTCTime`, `/commands/GetServiceState?serviceId=21`) are read by
  `internal/detect` on its detection cycle and published in `GET /api/v1/system`,
  and the System screen's resource panel shows both: the **drift** between the
  device clock and the host clock — warned beyond **±60 s**, an order of magnitude
  below the ten-minute tolerance of the daily derivation, whose local day
  boundaries depend on the installation's clock — and the state of the
  grid-limits self-test, **read but never triggered**. `device_health` (migration
  `0009`) keeps the last reading, the drift stored `NULL` — never `0` — when the
  clock was not read; the internal allowlist grows from nine to eleven paths with
  the metric each one serves; `cmd/fronius-sim` serves both, `-clock-offset`
  shifting the served clock on purpose. Both paths are `[SOURCE-JS]`-extracted and
  **`unverified` on hardware** — never probed (`docs/UNKNOWNS.md` #16).
- 2026-10-16 — **A refusal is no longer dressed as a transport failure.** The
  client wrapped an error the Digest transport had already classified, and
  `froniuserr.KindOf` reads the outermost error: an HTTP 401 from the device was
  therefore reported as a network failure, which made the detector's
  `KindUnauthorized` branch unreachable and hid the *"credentials not
  configured"* reason behind `fronius: transport | …`. An error that already
  carries a decided nature now keeps it.

- 2026-10-16 — **The Events page, the Energy page and the derived daily
  energy.** Two read surfaces that were served but consumed by nothing — the
  audit that lists the hooks found `/api/v1/events` with **no hook at all** and
  `useEnergyDaily` used in **zero files** — now have a page each, and the daily
  energy finally has a source. **`/events`** (`useEvents`) is the journal of the
  collector and of the inverter: failures, recoveries, retention purges and the
  device's own events, one row per event (time · severity badge · kind · message)
  with the raw payload of each event in a collapsed disclosure. Its range preset
  (24 h / 7 days / 30 days / all) and its limit selector (the API accepts
  `1..500`, `100` by default) go to the endpoint; the **severity and kind filters
  are client-side**, because the endpoint has neither parameter — so the page
  says how many of the fetched events it shows, and its filter options carry the
  labels the table shows (the raw word stays the option's value). **`/energy`**
  (`useEnergyDaily`, now used) states one row per local day with the value, a
  marker saying whether it is the **device counter** or a value **derived from
  total**, and the day's reason when there is neither; a bar chart of the daily
  energies materialises a missing day as a **gap** (never interpolated, never a
  zero), and
  three rollups (this week, this month, this year) sum the days that **could be
  measured**, each stating how many **contributed** and how many were
  **excluded** — a **lower bound**, the day in progress included. **Backend:** the
  daily payload now carries, per day, `energy_wh` (the device's own `E_Day`,
  always null on the reference firmware), `derived_wh` (**measured from our own
  stored total**, the same rules as the day KPI: a baseline within **10 minutes**
  of the local midnight, an end reading within 10 minutes of the day's end, no
  counter that went backwards), `derived_reason` (the cause when it could not be
  established) and `partial` (the day in progress); the range now lists **every
  local day** as soon as anything was recorded in it and stays `"data": []` when
  the range holds nothing at all — the empty state stated once, not once per day.
  New storage query `Store.DailyEnergyFromTotal` (`internal/storage/daily_derived.go`),
  built on the baseline lookup `Store.TotalEnergyAt`. **Fixes:** the three Solar
  API families on `/detail` (`inverter_info`, `active_devices`, `api_version`)
  render under proper headings instead of their raw ids, and a day's
  `derived_reason` is shown **only beside a dash** — beside a value it read as a
  defect. **Verified running** against the simulator with
  `froniusd --backfill-days 3`: `/energy` fetched **289** days (the year, for the
  rollups), the table showing the last 30 (`30 / 289 day(s) of the window
  fetched`), rows reading `2026-10-14 · 24.60 kWh · device counter`, the day in
  progress labelled, valueless days carrying `no sample carries a total at or
  before the day's start`, and the rollups `4 days contributed · 12 days excluded
  · lower bound`; `/events` rendered the two events of the run (`collector_started`
  info, `device_event` warning) with its filters and its detail disclosure. Web
  suite **196 tests** (was 156), typecheck and lint clean; Go `-race`, `vet`,
  `gofmt` green.

- 2026-10-16 — **The Solar API-first route, implemented: identity and presence
  without credentials.** Three Solar API requests need no credential and now
  answer what the Digest-protected internal API could not: `internal/fronius/solarapi`
  gains three readers — `InverterInfo()` (`/solar_api/v1/GetInverterInfo.cgi`),
  `ActiveDevices(ctx, class)` (`/solar_api/v1/GetActiveDeviceInfo.cgi`, frozen
  query `DeviceClass=System`) and `APIVersion()` (`/solar_api/GetAPIVersion.cgi`,
  the one answer that is **not** enveloped). `internal/detect` gains a `Solar
  SolarReader` field (optional: a detector without it reads the internal paths
  only) and a `readSolar` step on its own slow cycle. The three requests are
  **used, never merely recorded**: they publish three new device-detail families
  — `inverter_info`, `active_devices`, `api_version` — and fill what the internal
  API left undetermined. The precedence is one-directional: identity falls back
  to `GetInverterInfo.cgi` **only** when the record holds no serial number
  (filling `SerialNumber` from `UniqueID`, `DeviceName` from `CustomName`,
  `record.Source = GetInverterInfo.cgi`, identity state `present`), never
  overwriting an identity the authenticated path established, and **never
  inventing** the model or the firmware (the Solar API does not report them);
  presence falls back to the active-device inventory **only** when the internal
  meter/battery state is `unknown` (a class reported with at least one device is
  `present`, a class reported empty or not mentioned is `absent`, `Source =
  GetActiveDeviceInfo.cgi`). The collector paces its two Solar API reads: the
  common inverter detail is read **every other collection cycle**
  (`detailEveryNCycles = 2`, §2.6 — at most two realtime requests in parallel and
  four seconds between two consecutive calls; at the default `5 s` interval,
  alternating keeps them `5 s` apart). `cmd/fronius-sim` serves the three paths
  in the observed shapes and answers a class without a device **empty**, which
  the fallback reads as an established absence. Verified running against the
  simulator **without any credential**: identity `available: true` (serial
  `SIMULATED-0001`, `source = /solar_api/v1/GetInverterInfo.cgi`, `model` stays
  `null`), meter and battery `absent` with `source = /solar_api/v1/GetActiveDeviceInfo.cgi`,
  and `/api/v1/detail` carrying the three families. One bug found and fixed
  during that verification: the fallback updated the in-memory detection but not
  the **record**, so the capability still said "identity not determined" — a test
  now asserts the recorded state. Documentation only in this entry's scope: no
  code beyond the readers, the detector step and the simulator.

- 2026-10-16 — **Two Solar API endpoints the specification has and the catalogue
  lacked.** Reading the manufacturer's specification
  **`42,0410,2012,EN 02115052025`** (`references/42,0410,2012.pdf`, 91 pages,
  Solar API V1) added `/solar_api/v1/GetLoggerLEDInfo.cgi` (§4.5.2) and
  `/solar_api/v1/GetOhmPilotRealtimeData.cgi` (§4.10.2, capital `P` in
  `GetOhmPilot`) to the source catalogue (`internal/sources/catalog.go`, state
  `captured`, proof `[DOC]` citing the document number and the section) and to
  the observed inventory of `docs/SOURCES.md`. The second survey of 2026-10-16
  answered both `empty` (291 and 276 bytes) — consistent with §4.5.1
  ("available but returns an error") and with no Ohmpilot being installed. Both
  are reachable through `cmd/fronius-probe --survey` / `--endpoint`; neither is
  called by the application.

- 2026-10-16 — **Readable cache.** The application reads the internal
  `/components/cache/readable` and publishes what the device puts in its own
  register cache. The internal allowlist grows from **eight to nine** paths: the
  entry is `READ`, envelope `Cgi` (its `Body.Data` is keyed by node id),
  `Digest: true` per the polled registry while the reference device answered it
  `200` without a header. It is read by the **detector** on its slow cycle —
  `4 897` bytes and `99` values per read, so a register dump on every `5 s`
  collection cycle would be waste (the survey of 2026-10-16 is the evidence).
  Observed shape (firmware `1.19.7-1`): one node (`393216`) with **ten
  attributes** (`[ENABLE]`, `[VISIBLE]`, `createTS`, `note`,
  `inverter.devicetype`, `inverter.devicetree`, `inverter.nodetype`,
  `inverter.pmc.0`/`1`/`2`) and **81 registers** — **44 named by the device**
  with its own type suffix (`_F32` ×27, `_U64` ×11, `_U16` ×6, e.g.
  `PV_VOLTAGE_MEAN_01_F32`, `ACBRIDGE_ENERGYACTIVE_PRODUCED_SUM_01_U64`) and
  **37 bare `<id>` identifiers** the device names nowhere. The extraction keeps
  the device's vocabulary: the key verbatim, the name and the type suffix split
  out as `name`/`kind`, the value as the device wrote it (`json.RawMessage`, so a
  64-bit counter is not rounded through a float64), the bare ids kept as `<id>`
  with no invented name, and channels ordered named first. **No unit is derived
  from a name**: the device states a name and a type suffix, not a unit, and a
  guessed unit would be a fabricated measurement. Published by `GET /api/v1/detail`
  as the sixth family `cache` (`internal/model/detail.go`: `CacheDetail` /
  `CacheNodeDetail` / `CacheAttribute` / `CacheChannel`), stored in
  `device_detail` like the others, and rendered by the Device detail page's
  **Cache block** (attributes list + a collapsible registers table). The source
  catalogue moves `internalapi.cache` from `candidate` to **`called`**, keeping
  the note that no unit is asserted, and **no fixture is promoted**.
  `internal/fronius/internalapi/cache.go` + `cache_test.go` carry the extraction
  (the 64-bit counter kept exact, the bare identifier kept bare). The 37 bare ids
  are a **new open unknown** (`docs/UNKNOWNS.md` #14): nothing observed explains
  them. Also added: `make dev-device DEVICE=http://<inverter>` runs `froniusd`
  against the inverter instead of the simulator — a whole-page `404` on the
  Device detail page is the signature of a daemon pointed at the development
  simulator (`docs/RUNBOOK.md` § Device detail).

- 2026-10-16 — **Device detail.** The application reads five more paths and
  publishes what the device says about **itself**, beyond its production. Solar
  API: `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CommonInverterData`
  (constant `solarapi.CommonQuery`), read by `internal/collector` **every
  collection cycle** — its `DeviceStatus` and its thirteen measurements, one
  extra `GET` per cycle. Internal API: `/status/common`, `/status/network`,
  `/status/devices`, `/status/emrs` — the allowlist grows from **four to eight
  paths** (all `READ`, envelope `Default`: their answers are **bare** JSON, no
  Body/Head; `Digest: true` per the polled registry while the device answered
  them `200` without a header). The four diagnostics are read by `internal/detect`
  on the detection cycle (`FRONIUS_DETECT_INTERVAL`), so each family carries its
  **own** read instant. Storage: migration `0008_detail.sql`, table
  `device_detail(family PRIMARY KEY, read_at_utc, available, reason, data_json)` —
  one row per family (`inverter`, `system`, `network`, `devices`, `emrs`),
  replaced on each read: a **snapshot**, not a series. Carried by
  `Store.Restore` (added to `backupTables`), **not** purged with the retention
  (it is state, like `device`, not history). New route `GET /api/v1/detail`
  (`{read_at, families: [{family, available, reason, read_at, data}]}`), which
  keeps the **three** states distinct: recorded and available · recorded and
  unavailable **with its reason** · never read (**absent from the list**). The
  inverter measurements come as an **ordered** list with a `label` per key (a
  fallback; the interface has its own dictionary), a `unit` and a nullable
  `value` (`null` = the device reported nothing, never a zero). Models:
  `internal/model/detail.go`, `internal/fronius/internalapi/status.go`,
  `internal/fronius/solarapi/realtime.go`. The source catalogue follows: the
  common collection and the four diagnostics move from `candidate` to **`called`**
  with `Called: true`, their `would_unlock` gone; `/status/common`'s
  `updateRunning` becomes `displayed` (it is on the detail page).
  `cmd/fronius-sim` serves the four diagnostic families in the observed shapes
  (bare JSON; `network` carries one `eth0` on **documentation-range** `192.0.2.x`).
  Frontend: a **"Device detail"** page (`/detail`) and an **inverter state chip**
  on Overview. Tests: `internal/storage/detail_test.go`,
  `internal/api/detail_test.go`, `apps/web/src/routes/Detail.test.tsx` (new) and
  the extended `internal/collector/collector_test.go`,
  `internal/detect/detector_test.go`, `internal/fronius/internalapi/client_test.go`,
  `cmd/fronius-probe/probe_test.go`, `cmd/fronius-sim/internal_api_test.go` and
  `apps/web/src/routes/Overview.test.tsx`.
  Documentation updated (`docs/ARCHITECTURE.md`, `docs/ENDPOINTS.md`,
  `docs/SOURCES.md`, `docs/PROBE-READONLY.md`, `docs/RUNBOOK.md`,
  `docs/UNKNOWNS.md`, `TODO.md`).

- 2026-10-16 — **Read-only survey of the whole catalogue.** `cmd/fronius-probe
  --survey` walks every catalogued path, **one `GET` per path**, sequentially
  with `--delay` between paths (default `1s`), and records the outcome of each:
  its **class** (`ok`, `empty`, `not_json`, `auth_required`, `not_found`,
  `error`), the HTTP status, `digest_required`, the bytes, the error, the
  firmware and the **shape** of the answer — JSON paths and types, arrays by
  length with element paths suffixed `[]`, bounded to 500 entries and 8 levels
  with an explicit `(truncated)` marker — and **never a value**, so a survey of
  an identity endpoint cannot become a second copy of the serial numbers. New
  flags `--delay` and `--db` (default `$FRONIUS_DB_PATH`; empty writes the report
  only); `--survey` refuses to combine with `--endpoint`. For each path a
  **bare** GET comes first — whether a path is reachable without a header is
  itself a fact — and only a `401`/`403` **with** credentials triggers one more
  read through the Digest transport (never preemptive). A `404` or a `401` is a
  **result** and never aborts the survey. The probe's allowlist is now
  **derived from `internal/sources`**: the default run is the catalogued sources
  whose state is `called` or `captured` (six entries), `--survey` walks the whole
  catalogue, `--endpoint <path>` accepts any catalogued path — a path cannot be
  probed without being catalogued, and the catalogue test keeps the two
  registers bound. Output: one line per path (class, status, bytes, fields,
  path), a summary per class, the firmware discovered on the identity path, and,
  with `--save`, a `survey_<UTC instant>.json` report written `0600`. Storage:
  migration `0007_probe.sql`, table `source_probe` keyed by `(path, query)` (the
  query is part of the identity of a path, since two DataCollections of one
  endpoint answer different things; a survey replaces its previous row) with the
  columns `class`, `http_status`, `digest_required`, `body_bytes`, `error`,
  `firmware`, `probed_at_utc`, `fields_json`, plus `SaveSourceProbes` /
  `SourceProbes`; the table is carried by `Store.Restore` (added to
  `backupTables`) and purged by `PurgeBefore` under the same bound as the
  measurements. `GET /api/v1/sources` gains a `survey` block (`probed`, one
  counter per class, `last_probed_at`, `firmware`) and a `probe` object on each
  source that has a recorded row (`class`, `http_status`, `digest_required`,
  `bytes`, `error`, `firmware`, `probed_at`, `fields`); a source never surveyed
  has **no** `probe` key, so the interface can say "never surveyed" rather than
  show zeros. Tests: `cmd/fronius-probe/survey_test.go` (the classes stay
  distinct; the inventory carries no value; arrays walked through their first
  element; the bound and its marker; both observed firmware shapes; a full survey
  against a stub device writing the report *and* the database; the authenticated
  second read) and `internal/api/sources_test.go` (the merge, the counters, the
  path+query key, the never-surveyed case). Documentation updated
  (`docs/PROBE-READONLY.md`, `docs/RUNBOOK.md`, `docs/SOURCES.md`,
  `docs/ARCHITECTURE.md`).

- 2026-10-16 — **Source catalogue.** `internal/sources/catalog.go` is the
  machine-readable register of every source the device exposes and of what this
  application does with it: `type Transport` (`solar_api` | `internal_api`),
  `type State` (`called` | `captured` | `candidate` | `archived`), `type
  FieldStatus` (`displayed` | `stored` | `validated` | `ignored` |
  `unverified`) and the types `Field`, `Proof` and `Source`, with `Catalog()`,
  `Called()`, `ByID()`, `IDs()` and `Summarize()`. One entry per source carries
  its path, query, method, proofs (register marker + firmware), the capability it
  feeds — or `WouldUnlock` for a candidate that names a capability that **does
  not exist yet** — and its field inventory, an `ignored` or `unverified` field
  carrying its reason. New route `GET /api/v1/sources`
  (`internal/api/sources.go`) returns `{data: {summary, sources}, meta:
  {generated_at}}`, registered like the seven other routes.
  `internal/sources/catalog_test.go` binds the catalogue to the registers:
  unique sorted ids, `GET` only, every ignored/unverified field carries its
  reason, every capability id exists or is a candidate the catalogue introduces,
  the `called` set equals the code allowlist plus the one Solar API path, every
  declared fixture exists on disk, the Solar API paths of the candidate table of
  `docs/SOURCES.md` and the internal paths of `docs/ENDPOINTS.md` are held
  exactly (both directions), the summary counters agree with the list, and a
  caller cannot mutate the package's data; `internal/api/sources_test.go` covers
  the payload, the counters, the three states, the called set and `405` on
  `POST`. The React side gains the "Data sources" page
  (`apps/web/src/routes/Sources.tsx`): a summary strip, filters, one block per
  source with its proofs, capabilities and docs, an expandable field inventory
  and a legend. Documentation updated (`docs/SOURCES.md`, `docs/ENDPOINTS.md`,
  `docs/ARCHITECTURE.md`, `docs/RUNBOOK.md`).

- 2026-10-16 — **Database backup and restore.** `internal/storage/backup.go`
  (`Store.Backup`, `Store.Restore`, `VerifyBackup`, `Inspect`, `BackupInfo`):
  the snapshot is SQLite's `VACUUM INTO` taken **through the application's own
  connection**, so it is consistent while the collector writes, compacted and
  standalone (no WAL sidecar to carry along); the file is built under
  `<dest>.partial-*` and renamed only once complete, and an **existing
  destination is refused** (`ErrBackupExists`) rather than overwritten.
  `Restore` replaces the whole persisted state (`samples`, `events`,
  `collector_runs`, `collector_state`, `device`, `device_events_seen`,
  `agg_hour`) **in one transaction**, after checking the backup's
  `PRAGMA integrity_check`, that its recorded migration set is **exactly** the
  one applied here (otherwise refused) and that the row counts match before
  committing — a restore that did not carry everything over rolls back rather
  than reporting success; the aggregation watermark lives in `collector_state`,
  so the aggregates are restored with it. New command `cmd/fronius-backup`
  (`save -out <file> [-db <path>] [-tz <zone>]`, `verify -file <file>`,
  `restore -from <file> -yes [-db <path>] [-tz <zone>]`), plus `make backup` and
  `make restore`; `restore` refuses to run without `-yes` and first keeps the
  current database aside as `<db>.pre-restore-<UTC instant>.db`.
  Verified against a database written by a **running** `froniusd` (simulator on
  8081, WAL): `save` while the collector ran carried **9 samples**
  (2026-10-16T17:46:41Z → 17:46:50Z) and the event, and `verify` reported the
  same; after the database reached **23 samples**, `restore -yes` (daemon
  stopped) brought it back to the **9 of the snapshot** and kept
  `<db>.pre-restore-20261016T174711Z.db` holding the **24** samples it replaced;
  `froniusd` restarted on the **restored** database collected again (9 → 17
  samples) and `/api/v1/health` answered `status: ok`, `samples: 17`. Tests:
  `internal/storage/backup_test.go` (round trip read back by a second store,
  every table carried, destination never overwritten, a failed backup leaves
  nothing behind, refusals) and `cmd/fronius-backup/main_test.go` (the three
  subcommands through `main1`, refusal without `-yes`, save over an existing
  file, verify on junk, usage errors); `go test ./... -race`, `gofmt` and
  `go vet` clean.

- 2026-10-16 — **Derived day and year energies (option b).** No local endpoint
  publishes the day or year counter on firmware `1.19.7-1` (`docs/UNKNOWNS.md`
  #13: `GetPowerFlowRealtimeData.fcgi`, `/status/powerflow` and the documented
  accumulation collection all return them `null` while the total is live). The
  two KPIs are now **derived from our own stored total**: `GET /api/v1/live`
  gains an `energy_derived` object (`from: "energy_total_wh"`, `day_wh`,
  `day_baseline_utc`, `day_reason`, `year_wh`, `year_baseline_utc`,
  `year_reason`), **present on that endpoint only**. The value is the difference
  between the current sample's total and the most recent stored sample **at or
  before the local boundary** — midnight for the day, 1 January for the year
  (store timezone `Europe/Brussels`, via `Store.TotalEnergyAt`). A value is
  **refused**, carrying its reason, when no sample carries a total at or before
  the boundary, when the closest sample before it is **older than 10 minutes**, when
  the counter **went backwards** since the boundary, or when the current sample
  carries no total. Measured fields are untouched: `energy_day_wh` /
  `energy_year_wh` stay `null` when the device does not report them, a computed
  value is never presented as a measurement, and the interface marks it
  **"estimated from the total"** (`kpi.energyDerived`, `resolveEnergy` in
  `apps/web/src/lib/energy.ts`, KPI wiring in `apps/web/src/routes/Overview.tsx`).
  Code: `internal/api/derived.go` (rule, tolerance, refusals). **Consequence
  documented:** on a fresh installation the day value is refused until a first
  local midnight has a baseline, the year value until a baseline exists before
  1 January — the reason string says so, no zero is invented. The daily **chart**
  (`/api/v1/energy/daily`) is **not** covered: it reads `agg_hour.max_energy_day_wh`,
  `null` on this firmware, so it stays empty — a separate, open decision.
  **Superseded** by the newest `### Added` entry: the daily chart is now derived
  from the stored total (`Store.DailyEnergyFromTotal`), so it is no longer empty.

- 2026-10-16 — **Seventh probe endpoint: the documented Solar API accumulation
  data.** `cmd/fronius-probe` carries a new allowlist entry,
  `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData`
  (class `READ`, `digest=false`), the **last candidate source** for the day and
  year energies that **both** flow endpoints return as `null` on firmware
  `1.19.7-1` (`docs/UNKNOWNS.md` #13), on the "Solar API first" route
  (ADR 0003/0008) — the internal `/status/powerflow` fallback fills nothing on
  this installation (the fallback was **since removed**; see `### Removed`
  below). The probe gains support for a **frozen query string** per
  entry (`endpoint.query`, a constant of the binary): the probe still cannot
  target an arbitrary URL and the query shows up in `--list`, while `--endpoint`
  still selects by path, without the query. The schema constants
  `solarapi.InverterRealtimePath` and `solarapi.CumulationQuery` name the path and
  the query (module `internal/fronius/solarapi`). The shape the probe expects is
  the **documented** Solar API v1 answer (`Body.Data.CumulationInverterData` with
  `E_Day`, `E_Year`, `E_Total`); `cmd/fronius-sim` serves that endpoint too
  (documented shape, values from its own curve), so a probe rehearsal against the
  simulator answers **seven** paths with `200` (`296` bytes, query echoed in
  `Head.RequestArguments`). **Since captured on the device** (2026-10-16, firmware
  `1.19.7-1`, file `solar_api_v1_getinverterrealtimedata.json`): the answer is
  `200`, `883` bytes, and its `DAY_ENERGY` / `YEAR_ENERGY` are **null** while
  `TOTAL_ENERGY` is live — the third and last candidate source settled, so **no
  local endpoint exposes the day and year counters** (`docs/UNKNOWNS.md` #13,
  option (b) implements the derivation). The application's client does
  **not** call the endpoint. Bonus, recorded but not implemented: the same answer
  carries `Body.Data.DeviceStatus` (`InverterState` `"Sleeping"`, `StatusCode`
  13) — the documented source of the inverter state the flow endpoints lack.

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
  API numbers; it does **not** reproduce the `null` case). **Since removed**: the
  captures of the same day proved no local source exposes the counters; see
  `### Removed` below.

- 2026-10-16 — **`/status/powerflow` captured and its fixture promoted.** A
  `cmd/fronius-probe --endpoint /status/powerflow` run against the reference
  GEN24 captured the path (`200`, `digest_required: false`, firmware `1.19.7-1`)
  and promoted `test/fixtures/internalapi/status_powerflow.json` (`synthetic:
  false`, `confidence: "VERIFIED (hardware)"`, `captured_at`
  `2026-10-16T14:47:07Z`) — the shape is no longer a `[HYPOTHESIS]`. The captured
  body settles it: `inverters` is an **array** of `{CID, DT, E_Total, ID, P}`
  (the bundle could not tell, its client reading that member through
  `Object.keys`/`Object.values`), `site.MLoc` is the internal twin of the Solar
  API's `Meter_Location` (integer `255`, versus the string `"unknown"`), and
  `common`/`version` are not read by the application. **Decisive consequence:**
  `site.E_Day` and `site.E_Year` are **null here too**, exactly as in the Solar
  API answer, so the fallback **fills nothing on this installation** — it records
  no `fallback_source`, logs no warning, and costs one extra GET per cycle. The
  cause of the null stays unknown, and the fate of the entry is an **open
  decision** (remove it, keep it dormant, or re-observe after midnight), with two
  candidate paths: the documented Solar API accumulation endpoint
  (`GetInverterRealtimeData` with `DataCollection=CumulationInverterData`, needing
  one probe capture) or deriving the daily energy from our own stored `E_Total`
  deltas (a **derived** metric, not a measurement). Tests cover both cases:
  `TestFallbackFillsNothingWhenTheDeviceNullsItToo` (the observed reality, driven
  by the promoted capture) and `TestFallbackCompletesTheNulledEnergies` (the fill
  logic, with a body explicitly labelled hypothetical). **Since removed**: the
  accumulation capture the same day confirmed no local source exposes the
  counters, so the entry was dropped; see `### Removed` below.

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

- 2026-10-17 — **The device clock read is verified on hardware; the service state
  is not.** The consented probe on the reference installation (`1.19.7-1`) read
  `/commands/GetUTCTime`: `200`, 51 bytes,
  `{"resultData":{"epoch":1792272194},"success":true}` — the extracted DTO
  exactly, in seconds, and the instant matched the probe's own clock to the
  second; captured as `test/fixtures/internalapi/commands_utc_time.json`.
  `/commands/GetServiceState?serviceId=21` was **challenged, then refused with
  `403`**: the path exists and is Digest-protected, but that account has no
  permission for the grid-limits self-test, so its response shape stays
  `unverified` and the application reports the state as unknown with this reason.
  This is the project's first **server-side** permission evidence
  (`docs/UNKNOWNS.md` #6): the device enforces the role, and the account used does
  not carry the one that unlocks the self-test.
- 2026-10-16 — **The probe's `--endpoint` selector disambiguates the
  realtime-data collections.** Three collections share
  `/solar_api/v1/GetInverterRealtimeData.cgi` and differ only by their query;
  selecting the **path** alone silently probed the first of them — which is what
  happened on the device run of 2026-10-16, where the **accumulation** collection
  was captured twice. `--endpoint` now accepts a **catalogue identifier**
  (`solarapi.inverter.common`) or a **path with its query**, in addition to a
  bare path, and an **error names the three ways to select** when none matches.
  The saved file name carries the query for an entry that has one
  (`probe_…_commoninverterdata_<instant>.json`), so two collections of one path
  can no longer overwrite each other. The probe's default set, still derived from
  the catalogue, is now **eleven** entries (ten `called` plus the one `captured`
  accumulation collection). Recorded in `docs/PROBE-READONLY.md`.

- 2026-10-16 — **The daily chart cannot come from the device: the first real
  survey settles it.** `cmd/fronius-probe --survey` was run on
  `http://192.0.2.10`, firmware `1.19.7-1`, with no credentials (27
  catalogued paths, one `GET` each); it answered **11 `ok`, 4 `empty`,
  2 `auth_required`, 10 `not_found`** (report
  `probe-survey/survey_20261016T191253Z.json`, rows in `data/fronius.db`). Two
  established assumptions change. `GetArchiveData.cgi` is a **`404`** on this
  firmware, so the daily chart (`/api/v1/energy/daily`) **cannot** be filled
  from device-side history — the aggregate/derived route is the only one
  (`docs/UNKNOWNS.md` #13); the endpoint leaves the candidate table as an open
  source for the chart. `GetInverterInfo.fcgi` and `GetLoggerInfo.fcgi` are also
  `404`, closing the "identity from the Solar API" route: the identity stays on
  the Digest-protected internal `/status/version` (`docs/UNKNOWNS.md` #3).
  Conversely, `CommonInverterData` and `CumulationInverterData` are **served**,
  so the electrical detail and the inverter `DeviceStatus`
  (`InverterState`/`StatusCode`/`ErrorCode`) exist on this device — candidates
  for the dashboard's missing state and detail, **wired later the same day** (see
  the Device detail entry above). `/solar_api/GetAPIVersion`
  is `404` and stays an **open candidate**, no reason asserted. **Corrected the
  same day (see `### Fixed`):** the `GetInverterInfo.fcgi`,
  `GetLoggerInfo.fcgi` and `GetAPIVersion` `404`s were a wrong `.fcgi` suffix,
  not a firmware fact — with the specification's `.cgi` they answer, so the
  "identity from the Solar API" route is **open**, not closed. Recorded in
  `docs/SOURCES.md` (§ Hardware captures and the candidate table),
  `docs/UNKNOWNS.md`, `docs/PROBE-READONLY.md` and `TODO.md` (step 21).

- 2026-10-16 — **Route guard audited: it is a navigation condition, not a role
  gate.** The 37 guarded routes of the AppModule array (`canActivate: [oi.a]`,
  `const Yb` l. 155510-155701; the lazy `ji.a.solarWeb` route carries none) are
  protected by the bundle's only `CanActivate` definition (`canActivate(t, e)`,
  l. 81341, module `euqz` l. 81184-81388; the `oi` → `euqz` binding is an
  `[INFERENCE]`, the import binding lying outside the readable window, and three
  concurring clues are recorded). It chains four middlewares
  (`_updateMiddleware` l. 81311 … `_defaultMiddleware` l. 81325) yielding `true`,
  `false` or a redirect `UrlTree` (`/update`, `/exchange`, `/dashboard`,
  `/startPage`), and its role branch is a **client-side** menu permission
  (`_mapLoggedInUser` l. 81366-81374 → `isUserPermittedForUrl` l. 43732 → the
  three-valued `Wfm5` enum, l. 70078): a refusal **cancels the navigation** (the
  observable never emits) instead of answering `403`. Per-route minima are a
  client table (`H8nL`: `technician` for `DEVICE_CONFIGURATION`, `customer` for
  `INVERTER`) and the role flags expand from the login string (map `D`,
  l. 77898-77905). The only authorization-like value the client reads **from the
  device** is `writePermission` (l. 69202), compared field by field: a payload
  boolean is not a permission matrix, so `docs/UNKNOWNS.md` #6 stays open and the
  executable authorization remains the code allowlist. Recorded in
  `docs/ENDPOINTS.md` § Declared roles (client only) and `docs/UNKNOWNS.md` #6.

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

### Removed

- 2026-10-16 — **Internal `/status/powerflow` fallback removed, and the
  accumulation capture recorded (clean cutover, no shim).** The fallback's premise
  is refuted by the captures of the same day: `GetPowerFlowRealtimeData.fcgi`
  (site `E_Day`/`E_Year` null), `/status/powerflow` (`site.E_Day`/`E_Year` null
  too) and the documented accumulation collection
  `GET /solar_api/v1/GetInverterRealtimeData.cgi?Scope=Device&DeviceId=1&DataCollection=CumulationInverterData`
  (2026-10-16T17:00:38Z, `200`, `883` bytes, no challenge — `DAY_ENERGY` and
  `YEAR_ENERGY` null while `TOTAL_ENERGY` is live at `15089627.579722222` Wh) all
  agree: **no local source exposes the day and year counters**. Removed: the
  allowlist entry `/status/powerflow` (the internal allowlist is back to **four**
  entries, the Solar API path unchanged), `internalapi.PowerFlow`/`PowerFlowSite`,
  `Client.PowerFlow`, `powerFlowHasContent`,
  `collector.PowerFlowSource`/`Collector.Fallback`/`completeFromInternal`,
  `model.Sample.FallbackSource` and the `samples.fallback_source` column dropped
  by migration `0006_drop_fallback_source.sql`, the `/api/v1/live` field
  `fallback_source`, the probe entry for `/status/powerflow` (the probe lists
  **six** entries again; the accumulation entry stays), the simulator's
  `/status/powerflow` route, and `internal/collector/fallback_test.go` (the tests
  pinned to the removed symbols are updated). **Kept on purpose:**
  `test/fixtures/internalapi/status_powerflow.json` — the recorded capture of that
  shape; its `_provenance` states the endpoint is **no longer called**. Verified: a
  fresh database migrates to version 6, and a **v5 database holding the column and
  4 rows upgraded to v6** (column dropped, rows intact, new samples inserted);
  `go test ./...` green. The accumulation answer also carries a **bonus** the flow
  endpoints do not — `Body.Data.DeviceStatus` (`InverterState` `"Sleeping"`,
  `StatusCode` 13, `ErrorCode` 1175), the documented source of the inverter state
  and a **candidate** for a future metric, **not implemented**; it shows the
  inverter was Sleeping at 17:00 UTC, which explains the `0.0` powers while the
  total stayed live. **Open:** the cause of the null counters (not established),
  and the product decision — derive the day and year energy from our own stored
  `E_Total` deltas (option b, a **derived** metric), or leave the two KPIs absent
  with a capability reason (`docs/UNKNOWNS.md` #13).

### Fixed

- 2026-10-17 — **The CI's "no versioned device password" step was red, on
  documentation.** Three examples carried the credential variable with a value —
  two placeholders and a simulator password — and the workflow greps for exactly
  that pattern. The rule is right; the examples now use the `_FILE` form (README,
  runbook) or `--password-stdin` (the probe example in Sources), and the
  development notes say why a placeholder fails too.
- 2026-10-17 — **The probe no longer puts a documentation placeholder on the
  wire.** The service-state entry declares `Query: "serviceId=<id>"`, and
  `--endpoint /commands/GetServiceState` used to send that literal — a wasted
  request and, on hardware, a pointless failed authentication. It is now refused
  **before any request**, naming the placeholder and the new `--query` flag
  (`--query serviceId=21`), which applies only to a single `--endpoint` whose
  catalogue entry declares a query. A survey skips such entries with one notice
  and never sends them. The path stays the allowlisted one, so the executable
  allowlist is unchanged. Seven tests assert the refusals send nothing and the
  override produces exactly `/commands/GetServiceState?serviceId=21`.
- 2026-10-16 — **Wrong Solar API suffixes in the catalogue — the catalogue's
  error, not the firmware's.** Eight Solar API paths had been probed with a
  `.fcgi` suffix and their `404`s were read as "this firmware does not serve
  them". The manufacturer's specification **`42,0410,2012,EN 02115052025`**
  (`references/42,0410,2012.pdf`, 91 pages, Solar API V1) proves that **only
  `GetPowerFlowRealtimeData` is `.fcgi`** and that the twelve other requests are
  `.cgi`. Corrected in `internal/sources/catalog.go` and in the registers
  (`docs/SOURCES.md`, `docs/ENDPOINTS.md`): `GetInverterInfo.cgi`,
  `GetActiveDeviceInfo.cgi`, `GetMeterRealtimeData.cgi`,
  `GetStorageRealtimeData.cgi`, `GetStringRealtimeData.cgi`,
  `GetSensorRealtimeData.cgi`, `GetLoggerInfo.cgi` and
  `/solar_api/GetAPIVersion.cgi` (the `.cgi` belongs to the path, §2.4.1). The
  phantom `GetDeviceInfo`, which appears **nowhere** in the specification, was
  **removed**. A **second survey** the same day (firmware `1.19.7-1`, corrected
  paths, `--delay 4s` as §2.6 prescribes) answered each of the eight — three
  `ok` (identity, device inventory, API version), five `empty` — so the ten
  `not_found`s of the first run became **one**, `GetArchiveData.cgi`, which
  §5.1.1 says GEN24/Tauro/Verto is **NEVER** supported for. The "identity from
  the Solar API is closed" claim is **retired**: `GetInverterInfo.cgi` provides
  identity without credentials (`docs/UNKNOWNS.md` #3). **Also:** the probe's
  `--survey` now defaults to `--delay 4s`, the pacing §2.6 prescribes (at most
  two realtime requests in parallel, 4 s between two consecutive calls); a lower
  value is a deliberate choice.


- 2026-10-16 — **The realtime-data answer is flat, and the simulator was not.**
  The capture of `GetInverterRealtimeData.cgi?…DataCollection=CumulationInverterData`
  (firmware `1.19.7-1`, 883 bytes, promoted as
  `test/fixtures/solarapi/inverter_realtime_accumulation.json`) proves the answer
  carries **no** `CumulationInverterData` wrapper — contrary to the official
  documentation — and that every measurement is a `{"Unit": …, "Value": …}` pair,
  not a bare number: `DAY_ENERGY`, `YEAR_ENERGY`, `TOTAL_ENERGY`, `PAC` sit
  directly under `Body.Data`, with `DeviceStatus` (`InverterState` `"Sleeping"`,
  `StatusCode` 13, `ErrorCode` 1175) beside them. The survey of the same day
  observed the same for `CommonInverterData` — thirteen measurements
  (`IAC`, `IDC`, `IDC_2`, `IDC_3`, `PAC`, `SAC`, `UAC`, `UDC`, `UDC_2`, `UDC_3`,
  the two energy counters and `DAY_ENERGY`/`YEAR_ENERGY` null) — and an **empty**
  `Body.Data` for the three-phase collection, the Primo being single-phase.
  Corrected: `cmd/fronius-sim` now answers **per collection** in the observed
  shape (it used to answer one body for all three, with the guessed keys), the
  catalogue's field paths and types for the three collections follow the
  observations instead of the documentation, and
  `internal/fronius/solarapi`'s contract test pins the promoted fixture so the
  flat shape, the null counters and `DeviceStatus` cannot drift again. Found by
  the survey mode on its first device run: a rehearsal would otherwise have
  taught keys and a nesting the device does not have.

- 2026-10-16 — **Wrong endpoint suffix for the accumulation capture**
  (`internal/fronius/solarapi`). The documented Solar API v1 realtime-data family is
  served as `.cgi` (`GetInverterRealtimeData.cgi`), while `.fcgi` carries the flow
  and inventory endpoints; the probe entry had inherited the `.fcgi` spelling from
  the candidate list and the first hardware run answered `404` with the webserver's
  XHTML error page (`Server: webserver`, `Content-Type: text/html`, `iso-8859-1`)
  instead of JSON. The path is corrected to `.cgi` — same frozen query, same
  purpose — and `docs/SOURCES.md`/`docs/PROBE-READONLY.md` record both the
  correction and the shape of the 404 body, which is how a wrong path presents
  itself on this device.

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
  `docs/UNKNOWNS.md` #2/#13) then said "observed on hardware, body not saved"
  instead of "not captured"; the same-day capture recorded in Added above replaces
  that wording with the promoted fixture.

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
  the `/status/powerflow` option was an open decision then — it is **since
  removed**, see `### Removed` above). The extra keys the capture
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
