# Running on a NAS

The service is a single static Go binary in a small image with a SQLite
database — a good fit for a NAS that is already on all the time. This page
covers what is specific to that setting; the mechanics of the container are in
the [runbook](RUNBOOK.md#docker).

Tested on a development machine, not on a NAS: treat this as guidance, and the
`Test NAS/local installation` item in the backlog as still open.

## 1. Get the image onto the NAS

Building on a NAS needs Go and Node, which most do not ship. Build on a
workstation and move the image:

```bash
# workstation, from the repository root
make docker                                  # fronius-monitoring:local
docker save fronius-monitoring:local | gzip > fronius-monitoring.tgz

# NAS, over SSH
gunzip -c fronius-monitoring.tgz | docker load
```

The image embeds the interface: there is no asset directory to copy.

## 2. The compose file

Copy `docker-compose.yml` and `.env.example` (renamed `.env`) to a directory on
the NAS, for example `/srv/fronius/`, and set in `.env`:

```bash
FRONIUS_SOLARAPI_URL=http://192.168.1.50   # the inverter's LAN address
FRONIUS_TZ=Europe/Brussels                  # the installation's zone
```

`127.0.0.1` would be the container itself. If the inverter's hostname resolves
through the NAS's DNS, a name works too; `host.docker.internal` reaches the NAS
host on Docker Desktop, not on Linux.

Then, in that directory:

```bash
docker compose --profile app up -d
docker compose ps
```

## 3. Where the data lives — and where it must not

Keep the database on a **local volume** (`fronius-data` in the compose file) or
on a local disk of the NAS. Do **not** put it on a CIFS/NFS share:

- SQLite relies on POSIX locking and on `fsync` semantics that network
  filesystems do not implement faithfully; a WAL database over CIFS/NFS can
  corrupt silently.
- The service writes continuously (a sample every `FRONIUS_POLL_INTERVAL`), so a
  network hiccup is a write failure, not a delay.

If you prefer a bind mount, create the directory on local storage and give it to
uid `10001`:

```bash
sudo mkdir -p /srv/fronius/data
sudo chown -R 10001:10001 /srv/fronius/data
```

**Backups** belong on the NAS share — that is what it is for:

```bash
# write to a bind-mounted directory, or copy the file out afterwards
docker compose exec -T froniusd /app/fronius-backup save -out /data/backup.db
docker compose cp froniusd:/data/backup.db /srv/backups/fronius-$(date +%F).db
docker compose exec -T froniusd /app/fronius-backup verify -file /data/backup.db
```

Schedule that, and keep the retention in mind (§ 5).

## 4. Credentials

Credentials can live in `.env`: compose interpolates them into
`FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD`, and the service reads them from
its environment. That is the simplest setup, and it is what the compose file
enables by default. The trade-off is visibility — an environment variable shows
in `docker inspect` and in `docker compose config`, so anyone who can use the
Docker daemon on the NAS can read it (the same set of people who could `exec`
into the container anyway). Keep the file at `chmod 600` and out of git, and do
not paste the output of `docker compose config` anywhere.

If you share the NAS, or simply want the password out of the environment, use the
`_FILE` form instead: the service reads the value from a file and it never enters
the process environment. Both are supported; nothing else changes.

Both credential variables have a `_FILE` form, which reads the value from a file
and strips the trailing newline Docker writes:

Create the files, readable only by you on the host:

```bash
mkdir -p secrets && chmod 700 secrets
printf '%s' 'technician' > secrets/device_user
printf '%s' 'the-password' > secrets/device_password
chmod 600 secrets/*
```

**With compose `secrets:`** (uncomment the blocks described in
`docker-compose.yml`) this is all you need: the engine reads the files as root
and delivers them inside the container as `root:root 0444`, and the service runs
as uid `10001` — no ownership to arrange. Point the variables at the mount:

```yaml
environment:
  FRONIUS_DEVICE_USER_FILE: /run/secrets/device_user
  FRONIUS_DEVICE_PASSWORD_FILE: /run/secrets/device_password
secrets:
  device_user:
    file: ./secrets/device_user
  device_password:
    file: ./secrets/device_password
```

**With a plain bind mount instead** (`-v ./secrets:/run/secrets:ro`), the
container's uid `10001` must be able to *traverse the directory and read the
files*. A `0600` file owned by you does not work — the service refuses at startup
with `permission denied`. Either:

```bash
chmod 755 secrets && chmod 644 secrets/*        # readable by anyone on the host
# or, if the host user may lose access:
chown -R 10001:10001 secrets && chmod 700 secrets && chmod 600 secrets/*
```

(`_FILE` wins over an inline value when both are set. The files are read at
startup; the values never appear in `/api/v1/system`, in the logs or in the
interface.)

## 5. Growth, retention and CPU

At the default 5 s interval the database grows by roughly **4 MB a day** —
about **1.4 GB a year** — plus the hourly aggregates. Set a retention if the NAS
volume is small:

```bash
FRONIUS_RETENTION_DAYS=730    # two years
```

The purge runs on its own schedule, records a `retention_purge` event, and keeps
the aggregates. CPU is negligible: one sample every five seconds, and a
detection cycle every minute.

## 6. Autostart, updates and access

- **Autostart**: `restart: unless-stopped` plus the NAS's own "start containers
  at boot" (Container Manager on Synology, Container Station on QNAP, the Apps
  service on TrueNAS SCALE). Verify with `docker compose ps` after a reboot.
- **Updates**: load a newly built image
  (`docker compose --profile app up -d --image=fronius-monitoring:local`, or
  `docker load` then `docker compose up -d`). The schema migrates on start; the
  database volume is untouched.
- **Access**: the compose file publishes `127.0.0.1:8080`, which is the NAS
  itself, not your LAN. The API has no authentication of its own
  ([ADR 0005](adr/0005-authentication-and-remote-access.md)), so do not widen
  that publication — put the reverse proxy and TLS **you** choose in front of it.
- **Health**: `docker compose ps` shows the container's healthcheck, which calls
  `/api/v1/health`; `docker compose logs -f froniusd` is the log.

## What not to do

- No credentials in the **compose file** or the **image**. `.env` is acceptable
  (§ 4); secrets are preferable when the NAS is shared.
- No database on a network filesystem.
- No `0.0.0.0` publication without a proxy in front.
- No `--force-backfill` against the real inverter: the backfill is for the
  simulator, and the flag exists only for the demo profile.
