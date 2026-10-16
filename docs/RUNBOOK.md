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
```

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
(`/status/version`), events (`/status/activeEvents`) and component presence
(`/components/…`). It is `froniusd` that queries them, periodically, and the
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

## Backup and restore

The database is a SQLite file in WAL mode: it must be backed up **with** the
SQLite method, not by a plain hot copy of the `.db-wal` files.

```bash
# Consistent hot backup
sqlite3 data/fronius.db ".backup '/path/backup/fronius-$(date -u +%Y%m%dT%H%M%SZ).db'"

# Restore: stop froniusd first
systemctl stop froniusd 2>/dev/null || pkill -f bin/froniusd
mv data/fronius.db data/fronius.db.before-restore
cp /path/backup/fronius-20261016T120000Z.db data/fronius.db
rm -f data/fronius.db-wal data/fronius.db-shm
FRONIUS_DB_PATH=./data/fronius.db ./bin/froniusd
```

Verification after restore:

```bash
sqlite3 data/fronius.db 'select count(*), min(ts_utc), max(ts_utc) from samples;'
curl -s localhost:8080/api/v1/live | jq '.data.power_pv_w, .meta.stale'
```

Migrations are versioned (`schema_migrations`) and applied automatically at
startup: restoring a database older than the binary is safe, in the direction of
the migration.

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
