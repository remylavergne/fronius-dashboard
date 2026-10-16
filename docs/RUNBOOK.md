# Operations runbook

Everything below runs **without touching the inverter**: the application only
reads device paths, over `GET`, and the credentials only serve to answer a `401`.

## Starting

```bash
# 1. Simulator (development and off-hardware verification)
go run ./cmd/fronius-sim                     # 127.0.0.1:8081

# 2. Supervision service
FRONIUS_SOLARAPI_URL=http://127.0.0.1:8081 \
FRONIUS_DB_PATH=./data/fronius.db \
go run ./cmd/froniusd                        # API on 127.0.0.1:8080
```

Single binary with the interface served by the backend (after `make build`):

```bash
FRONIUS_WEB_DIR=apps/web/dist ./bin/froniusd
# then http://127.0.0.1:8080/ (SPA fallback to index.html for known routes)
```

Development backfill:

```bash
./bin/froniusd --backfill-days 7    # 7 days from the local simulator
```

Backfill refuses a non-local target (protection: do not hammer the inverter);
`--force-backfill` lifts that refusal knowingly.

## Checking everything is fine

```bash
curl -s localhost:8080/api/v1/health | jq
```

| Field | Reading |
|---|---|
| `status` | `ok`, `degraded` (last cycle failed), `error` (database unreachable) |
| `db.ok` | the database responds |
| `collector.last_success_utc` | last successful cycle; empty = never |
| `collector.last_error` | last error message (empty = all is well) |
| `collector.last_run_ms` | duration of the last cycle |
| `collector.samples_total` | volume retained |

```bash
curl -s localhost:8080/api/v1/live | jq '.data.power_pv_w, .meta.stale'
curl -s localhost:8080/api/v1/capabilities | jq -r '.data[]|select(.available==false)|.id+" — "+.reason'
curl -s localhost:8080/api/v1/sources | jq '.data.summary'
curl -s localhost:8080/api/v1/detail | jq -r '.data.families[]|.family+" — "+.available|tostring'
```

The `/sources` call lists what the device exposes and what this application does
with it: each source with its state (`called` / `captured` / `candidate` /
`archived`), its fields with their status, and the capability it feeds or would
unlock. It is the machine-readable form of `docs/SOURCES.md` and
`docs/ENDPOINTS.md` (see `internal/sources/catalog.go`). The `/detail` call lists
the device detail families with their availability, their reason and their own
read instant (§ Device detail).

`meta.stale = true` means: no measurement for longer than `FRONIUS_STALE_AFTER`.
The displayed values remain the last known ones — they are not wrong, they are
old.

## Diagnosis by symptom

| Symptom | Probable cause | Action |
|---|---|---|
| `status: degraded`, `last_error` contains `transport` | inverter or simulator unreachable | check the address, the network, that the simulator is running |
| `last_error` contains `HTTP 401`/`403` | the device requires authentication on the Solar API path of this firmware | set the credentials (§ "Device credentials") and restart; do not attempt improvised credentials |
| `detection.meter.state: "unknown"` and reason "credentials not configured" | `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` absent: the device requires authentication on this path | set both variables in the environment of `froniusd`, then restart the service |
| `device.identity.available: false` with reason "access denied (401)" | credentials present but refused: user, password or realm different from the expected one | check the user (role "customer" or "technician" in the manufacturer interface); compare the realm announced by `fronius-probe` |
| `last_error` contains `HTTP 404` | the path differs on this firmware, **or** the request never reached the inverter | compare `solarapi_url` from `/api/v1/system` with the device address, then check both paths by hand: `curl -s -o /dev/null -w '%{http_code}\n' http://<device>/status/version` and the same on `/solar_api/v1/GetPowerFlowRealtimeData.fcgi`. A bare `404` on both means the host is not the inverter; a `401` means authentication is now required |
| detection reason `challenged, then refused` | the device answered `401` with a challenge, then refused the **authenticated** request (often with `404` or `403`) | the path exists — a bare `404` challenges nobody — so the credentials or the role they carry are the problem: check `FRONIUS_DEVICE_USER`/`FRONIUS_DEVICE_PASSWORD`, then run `fronius-probe --endpoint /status/version`, which prints the raw status and whether a challenge was issued |
| `last_error` contains `decode` | unexpected response shape | capture with `cmd/fronius-probe`, compare with `docs/SOURCES.md` |
| `last_error` contains `device_status` | the inverter reports an application error | consult the manufacturer interface; no measurement is recorded in that case |
| `meta.stale: true` while the device responds | cadence or threshold misconfigured | compare `FRONIUS_POLL_INTERVAL` and `FRONIUS_STALE_AFTER` |
| interface: "API unreachable" | `froniusd` stopped or in error | start/restart the service |
| the curve has gaps | periods without collection (stop, failure, restart) | intended behaviour: no point is invented to fill them |
| `froniusd` refuses to start: `refusal: … is not a local address` | non-local listen without authorization | read `docs/adr/0005` then, knowingly, `FRONIUS_ALLOW_REMOTE=1` |

The collection backoff is exponential (1 s → 2 s → … → 60 s): after a long
outage, recovery may take up to a minute after the device comes back. This is
normal, and it avoids hammering a device that is restarting.

## Device credentials

Some device paths exist only behind Digest authentication: identity
(`/status/version`), events (`/status/activeEvents`), component presence
(`/components/…`) and, since the device detail, the four diagnostic families
(`/status/common`, `/status/network`, `/status/devices`, `/status/emrs`). It is
`froniusd` that queries them, periodically, and the
`Authorization` header is emitted **only in response to a `401`** — never
preemptively, never to another host, never to the frontend.

Where to set them: in the environment of the **service**, not in a versioned
file, not in the interface.

```bash
export FRONIUS_DEVICE_USER=installer
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
FRONIUS_DETECT_INTERVAL=60s ./bin/froniusd
```

Both variables go together: defining only one makes startup fail, with a message
that names both (and never the value). For an interactive launch, `read -rs`
avoids writing the password to the shell history:

```bash
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
read -r FRONIUS_DEVICE_USER && export FRONIUS_DEVICE_USER
./bin/froniusd
```

Check that the read succeeds:

```bash
curl -s localhost:8080/api/v1/system | jq '.data.device, .data.detection'
curl -s localhost:8080/api/v1/capabilities | jq -r '.data[]|select(.id|startswith("device"))|.id+" — "+.available|tostring' 2>/dev/null
```

| Observation | Reading |
|---|---|
| `detection.meter.state: "present"` | the device responded, a Smart Meter is installed: `grid.import_export`, `load.consumption` and `autoconsumption.ratio` become available |
| `detection.meter.state: "absent"` | the device responded, no Smart Meter: consumption stays unavailable, with its reason |
| `detection.meter.state: "unknown"` + reason "credentials not configured" | no credentials set: the service stops querying this path until the next restart (no useless `401` every minute) |
| `device.identity.available: false` | the identity was not read: see `detection.internal_api.state` and `last_error` |
| `battery.soc` unavailable even with a battery detected | normal: presence detection does not prove that a state-of-charge measurement exists on the API side (later phase) |

On `401`: the service retries **once** per path after the challenge. If the
`401` persists, the detection message is "access denied (401): credentials
absent or invalid" — then check the user, the password, and the realm announced
by the device (`fronius-probe --endpoint /status/version`). Never "fix" this by
broadening the application: no device-modifying command exists in this binary.

## Device detail

The **Device detail** page (`/detail`, served by `GET /api/v1/detail`) shows what
the device says about **itself**, beyond its production: the **inverter** family
(its state and its thirteen electrical measurements) plus the **system**,
**network**, **devices** and **emrs** families. The **Overview** page also
carries an **inverter state chip**, fed by the same inverter family.

The readings come on **two cadences**, because they answer two different needs:

- the **inverter** family is read by the collector **once per collection cycle**
  (`FRONIUS_POLL_INTERVAL`), right after the measurements — this is **one extra
  `GET` per cycle**, the cost of a fresh electrical detail the flow endpoint does
  not carry;
- the **system**, **network**, **devices** and **emrs** families are read by the
  detector on its own cycle (`FRONIUS_DETECT_INTERVAL`): they change rarely.

Each family therefore carries **its own** read instant (`read_at`), and the page
shows it. The endpoint serves a **snapshot**, never a live read: opening the page
costs one read of the API's own table, never a read of the device.

A family is in one of **three** states, and the page renders all three — they are
not the same fact:

| The page shows | What it means | What to do |
|---|---|---|
| the values | recorded and **available** | nothing; the read instant tells how recent it is |
| the reason | recorded and **unavailable**, with the cause the read produced (a `401`, a timeout, a decoding error) | read the reason; if it names credentials, set `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` (§ Device credentials) and restart; if it is a decoding error, capture with `cmd/fronius-probe` and compare with `docs/SOURCES.md` |
| "Never read by the collector" | the family has **no** recorded row yet (a fresh database, or a cycle that has not run) | wait for the next cycle; it is not an error |

"Unavailable with a reason" is neither "absent" nor a zero: the read **failed**,
and the cause belongs with the read that produced it. A measurement the device
reported as nothing is shown as a dash, never as `0`.

**Every family unavailable at once, all `404`?** The collector is not talking to
the inverter. The development default is the **simulator**
(`FRONIUS_SOLARAPI_URL=http://127.0.0.1:8081`), and an older `cmd/fronius-sim`
serves the flow endpoint while answering `404` to everything else —
`/status/version` included, which is also why the identity panel would be empty.
Check the source the daemon actually polls, then point it at the device and
restart:

```bash
curl -s localhost:8080/api/v1/system | jq -r .data.solarapi_url
# the inverter itself: no credential is needed for the five detail paths
FRONIUS_SOLARAPI_URL=http://192.0.2.10 ./bin/froniusd
```

These five paths answered `200` **without** a header on the reference
installation (§ Surveying every catalogued path): a `404` on all of them is a
wrong source, never missing authentication — that would be a `401`.

## Surveying every catalogued path

`cmd/fronius-probe --survey` walks the **whole catalogued allowlist** (27 paths
today), one read-only `GET` per path, and records what each one answers. It is
the tool that tells, for the firmware actually installed, which paths exist,
what they return and what sits behind authentication — without a single value
ever leaving the device. The guarantees of `docs/PROBE-READONLY.md` hold: `GET`
only, the loopback refusal without `--allow-non-loopback`, and no password on the
command line.

```bash
# against the simulator (no risk)
go run ./cmd/fronius-sim --api-user sim --api-password simpass
# terminal 2
go run ./cmd/fronius-probe --base-url http://127.0.0.1:8081 --survey

# against the inverter, after the consent of docs/PROBE-READONLY.md
export FRONIUS_DEVICE_USER=installer
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
go run ./cmd/fronius-probe \
  --base-url http://192.0.2.10 \
  --allow-non-loopback \
  --survey --delay 2s \
  --db ./data/fronius.db            # default: $FRONIUS_DB_PATH
```

Flags: `--survey` walks the whole catalogue; `--delay` pauses between two paths
(default `1s`, so an inverter is not asked to serve 27 requests in a burst);
`--db` names the database that records the survey (empty writes the report file
only); `--save <dir>` also writes a `survey_<UTC instant>.json` report. `--survey`
refuses `--endpoint`, and the allowlist is **derived from the source catalogue**
(a path cannot be probed without being catalogued).

Then read the result on the **"Data sources" page** (`GET /api/v1/sources`): each
source carries a `probe` object with its class, HTTP status, whether it needed
authentication (`digest_required`), the bytes, the `firmware` and the instant,
and the **shape** of the answer (fields and types) — never a value. The page's
survey strip counts the classes and gives `last_probed_at` and the firmware. A
source never surveyed has **no** `probe` key: the page shows no class rather than
zeros.

Reading a class:

| Class           | Meaning                                                                                        | What to do                                                                 |
| --------------- | ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `ok`            | answered with content; its shape is recorded                                                   | read the field inventory to see what this firmware actually returns        |
| `empty`         | answered `{}`, `[]` or an empty `Body.Data`: the endpoint exists, the component is not installed | none — an absent component is a fact, not a failure                       |
| `not_json`      | answered, but not with JSON (e.g. an HTML error page)                                          | compare with the Solar API capture; the path or the host may be wrong      |
| `auth_required` | `401`/`403`: the path exists, behind authentication                                            | set `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` and survey again     |
| `not_found`     | `404`: this firmware does not serve the path                                                   | the path is absent here; check the firmware or the path spelling           |
| `error`         | `5xx`, timeout or refusal, with the message                                                    | network or device problem; the run continues and records the other paths   |

A `404` or a `401` is a **result**, never an aborted run: the survey keeps
walking and records each class as its own fact.

Recording and retention: the rows land in the application's database, table
`source_probe`, keyed by `(path, query)` — a new survey **replaces** its previous
row (a survey says what the firmware answers *now*). They are carried by
`backup`/`restore` like the other tables, and when `FRONIUS_RETENTION_DAYS > 0`
they are **purged with the measurements under the same bound**. A shape is only
true for the firmware that produced it (`docs/SOURCES.md`), so re-survey after a
firmware update.

## Backup and restore

`cmd/fronius-backup` is the tool for the one operation the running application
never performs: taking the whole database away and putting it back. Three
subcommands — **`save`**, **`verify`**, **`restore`** — exposed by two `make`
targets. The database path and the timezone default to the application's
configuration (`FRONIUS_DB_PATH`, `FRONIUS_TZ`), so the tool reads the same
database as `froniusd` without repeating anything.

```bash
# Save: a consistent snapshot, may run while froniusd is running
make backup                                            # data/fronius-<UTC instant>.db
FRONIUS_BACKUP=/path/backup/fronius.db make backup     # choose the file

# Verify: trust nothing before checking it
go run ./cmd/fronius-backup verify -file /path/backup/fronius.db

# Restore: stop froniusd FIRST
FRONIUS_BACKUP=/path/backup/fronius.db make restore
```

`save` writes the snapshot with SQLite's own `VACUUM INTO` **through the
application's connection**: it is therefore consistent even while the collector
writes, compacted, and standalone (no `-wal`/`-shm` sidecar to carry along). The
file is built under a temporary name (`<dest>.partial-*`) and renamed only once
complete, and an **existing destination is refused** rather than overwritten — a
backup quietly replaced would destroy the very copy being protected. Each of
`save` and `verify` prints one line: size, migration count, samples (with the
period covered), events, aggregated hours and the instant the file was written.

`verify` re-opens a file read-only and runs SQLite's `integrity_check` on it —
the cheapest way to tell a usable backup from a truncated one before relying on
it. `save` reads its own result back for the same reason: `save` alone proves the
copy was **written**, `verify` proves it is **sound**.

`restore` replaces the entire persisted state — every sample, event, collection
run and cycle, the detection state, the device event keys and the hourly
aggregates — **in one transaction**, after checking the backup's integrity, that
its recorded migration set is **exactly** the one applied here (a file of another
schema is refused), and that the row counts match before committing. A restore
that did not carry everything over rolls back rather than reporting success.

**A restore requires the collector stopped** (`make restore`, or
`restore -from <file> -yes`): it rewrites the state `froniusd` would be writing
at the same time. `restore` refuses to run without `-yes`, and before touching
anything it keeps the current database aside as
`<db>.pre-restore-<UTC instant>.db` (taken with the same consistent snapshot), so
a restore done for the wrong reason stays reversible.

After a restore, restart the service and check it is collecting again:

```bash
curl -s localhost:8080/api/v1/health | jq '.data.status, .data.collector.samples_total'
```

The aggregates travel with the snapshot: the aggregation watermark lives in
`collector_state`, so restoring a backup restores the aggregates and their
watermark together — nothing is recomputed and no hour is left half-aggregated.

Migrations are versioned (`schema_migrations`) and applied automatically at
startup. `restore` requires the backup to carry **exactly** the migrations
currently applied: a file from another schema (or an older binary) is refused
rather than silently upgraded.

## Retention and purge

`FRONIUS_RETENTION_DAYS=0` (default) keeps everything and **disables all
purge** (none is scheduled). With a value `N > 0`, `froniusd` runs a purge at
startup — catching up for the downtime — then **one purge per local day from
03:00**. Each purge deletes what is older than local midnight `N` days back:
measurements, events, collection cycles, anti-duplication marks and hourly
aggregates, then a `retention_purge` event is recorded.

The purge bound is rounded up to the next full hour, and the same bound applies
to measurements and aggregates: without this alignment, the hour straddling the
boundary would lose its aggregate while keeping part of its measurements, which
would become invisible to aggregate reads (ADR 0006). For Europe/Brussels, the
rounding is neutral.

Before enabling retention, take a backup (§ above) and check the volume:

```bash
sqlite3 data/fronius.db 'select count(*) from samples;'
du -h data/fronius.db
```

Order of magnitude: one sample every 5 s ≈ 17,280 rows per day, i.e. ≈ 6.3
million per year. Hourly aggregates add 24 rows per day (≈ 8,760 per year), an
order of magnitude below.

## Hourly aggregates

Reads at hourly or daily steps (`/api/v1/timeseries` with `step=1h|1d`,
`/api/v1/energy/daily`) do not scan the measurements: they read `agg_hour`, fed
by `internal/maintenance`, which aggregates **closed** hours every 5 minutes and
catches up at startup with what was written since the last pass. The current
hour is still read from the raw measurements: an hour still being filled is
never materialised, otherwise a read would believe it complete.

Diagnosis:

```bash
sqlite3 data/fronius.db 'select count(*), min(bucket_utc), max(bucket_utc) from agg_hour;'
sqlite3 data/fronius.db "select v from collector_state where k='agg_last_hour_utc';"
```

The second query gives the watermark: the start of the first non-aggregated
hour. It must be aligned to the hour and advance on its own (at most one hour
behind after an hour closes). If it moves back, it means a history was imported
below the watermark — normal behaviour after a `--backfill-days`, the next pass
picks it up.

A database whose `agg_hour` table is empty stays correct: every read falls back
to the raw measurements, more slowly. So it is never a failure, only a catch-up
to wait for (or to trigger by restarting). `make test-perf` measures the gain on
one year of data (ADR 0006).

## Day and year energies (derived)

The reference firmware publishes **no** day and no year counter on any local
endpoint (`docs/UNKNOWNS.md` #13): the measured fields `energy_day_wh` /
`energy_year_wh` stay `null`. To fill the two KPIs, `/api/v1/live` carries an
`energy_derived` object **computed from our own stored total**:

- `day_wh` / `year_wh` — the difference between the current sample's total energy
  and the most recent stored sample **at or before the local boundary** (midnight
  for the day, 1 January for the year, store timezone `Europe/Brussels`);
- `day_baseline_utc` / `year_baseline_utc` — the timestamp the value was measured
  from;
- `day_reason` / `year_reason` — why a value is absent, when it is.

**These are estimates, never measurements.** The interface shows the measured
counter whenever the device reports it, and marks the derived one **"estimated
from the total"**; a card with neither shows a dash, never a `0`.

Reading a `*_reason` (empty means the value is present):

| Reason (text) | Meaning | What to do |
|---|---|---|
| `no sample carries a total energy at or before <boundary>` | no baseline exists yet — normal on a **fresh install** before the first local midnight (day) or before 1 January (year) | wait for the boundary; nothing is broken |
| `no sample within 10m0s of the boundary: the closest one is … earlier` | the **10-minute tolerance** is exceeded — a collector outage or a silent device | check `collector.last_success_utc`; the day value resumes after the next boundary, or restart once collection is healthy |
| `the total energy counter went backwards since the boundary` | the device counter reset | investigate the inverter; the value stays refused rather than wrong |
| `the current sample carries no total energy` | this cycle did not read `E_Total` | check the last collection cycle |
| `the baseline could not be read` | database read error | check `/api/v1/health` (`db.ok`) |

The **daily chart** (`/api/v1/energy/daily`) is **not** derived and stays empty on
this firmware: it reads the hourly aggregate `max_energy_day_wh`, which the device
does not feed. That is a separate, open decision — not a fault of the derived
KPIs.

## Logging

`log/slog` on stderr. `FRONIUS_LOG_FORMAT=json` for ingestion by a log
collector, `FRONIUS_LOG_LEVEL=debug` for request detail (Solar API and internal
paths: **full URL**, path, status, bytes, duration; never a header). The URL is
what tells a wrong `FRONIUS_SOLARAPI_URL` from an endpoint that changed on the
device: a `404` on every path usually means the request went to the wrong host.

Device credentials cannot appear in the logs: the `secret.String` type
(`internal/secret`) masks the value for any formatting — `slog`, `fmt` and
`encoding/json` included — and only the Digest `HA1` computation calls
`Reveal()`. Detector startup logs a boolean (`credentials=true|false`), never the
username or the password.

## Clean shutdown

`SIGINT` / `SIGTERM`: HTTP server shutdown, wait for the current collection
cycle to finish, database close, exit with code 0. An interrupted cycle leaves
no partial sample (the insert is atomic).

## Database maintenance

```bash
sqlite3 data/fronius.db 'pragma integrity_check;'
sqlite3 data/fronius.db 'vacuum;'          # after a large purge
sqlite3 data/fronius.db 'select ok, count(*) from collector_runs group by ok;'
sqlite3 data/fronius.db 'select ts_utc, kind, severity, message from events order by id desc limit 10;'
sqlite3 data/fronius.db 'select count(*) from agg_hour;'   # hourly aggregates
```

## What this service never does

- no method other than `GET` towards the inverter: the refusal is structural
  (`internal/fronius/readonly.go`), not declarative;
- no `Authorization` header emitted preemptively: it is built only in response
  to a `401`, only towards the host of `FRONIUS_SOLARAPI_URL`, and never towards
  the frontend;
- no configuration write on the device (phase 6, out of MVP);
- no network exposure outside the loopback without `FRONIUS_ALLOW_REMOTE=1` and
  reading ADR 0005;
- no consumption, import/export or self-consumption computation without a Smart
  Meter: these capabilities are marked unavailable with their reason;
- no data invented to fill a collection gap, and no invented absence: a failed
  read gives "unknown", never "absent".
