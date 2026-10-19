# Running on a NAS

The service is a single static Go binary in a small image with a SQLite
database — a good fit for a NAS that is already on all the time. This page
covers what is specific to that setting; the mechanics of the container are in
the [runbook](RUNBOOK.md#docker).

Tested on a development machine, not on a NAS: treat this as guidance, and the
`Test NAS/local installation` item in the backlog as still open.

## 1. Get the image onto the NAS

Building on a NAS natively needs Go and Node, which most do not ship. Build on a
workstation, then either push it to the Forgejo container registry (recommended:
the NAS pulls it with one command and every version stays addressable) or move
it as a tarball (the offline fallback). A NAS can also build the image itself
with nothing but Docker — § C — which is the route when the engine has no
BuildKit API (a Colima install without the `buildx` plugin, for example).

### A. Through the Forgejo container registry (recommended)

Forgejo serves an OCI registry on the forge's own host, and an image is named
`{host}/{owner}/{image}:{tag}` — the owner being a user or an organization. With
a forge at `forgejo.example.com` and the owner `testuser`:

The image must match the NAS's architecture. The **classic builder builds for the
daemon's architecture** — there is no `--platform` without BuildKit — so on
Apple silicon (whose default Colima VM is arm64) start a second, x86_64 profile
and point the targets at it:

```bash
# once: an amd64 daemon on the Mac (QEMU; buildx is not involved)
colima start --profile amd64 --arch x86_64 --cpu 4 --memory 6

# once per workstation: a personal access token with package write scope
# (with 2FA, or OAuth, the account password is refused: use the token)
echo "$FORGEJO_TOKEN" | docker --context colima-amd64 login forgejo.example.com -u testuser --password-stdin

# from the repository root; the version tag is yours to choose
DOCKER_BUILDKIT=0 make docker-push \
  DOCKER="docker --context colima-amd64" \
  REGISTRY=forgejo.example.com/testuser TAG=0.1.0
```

`DOCKER_BUILDKIT=0` forces the classic builder; drop it if the engine's BuildKit
works and only the `buildx` plugin is missing.

`make docker-push` builds, tags and pushes in one go, and every `docker` command
in the target goes through `DOCKER`, so the image is amd64. To split it, the
same variable applies to `make docker`, then `docker tag`/`docker push`.

Notes:

- Image names must be **lowercase**; the registry treats tags case-insensitively.
- The package is created by the first push and appears under the owner's
  *Packages*. To show it on a repository's tab, either name the image after the
  repository or add `LABEL org.opencontainers.image.source=<repository URL>` to
  the image.
- Cross-building needs the BuildKit API. Without it (classic builder), build on a
  daemon of the target architecture — the x86_64 Colima profile above, an Intel
  Mac, or an ARM profile for an ARM NAS.
- A registry served over plain HTTP is refused by the daemon unless it is listed
  in `insecure-registries`; HTTPS on the forge is the better answer.
- The image does not carry `docker-compose.yml` or `.env`; those stay on the NAS
  (§ 2), as does the login for a private package.

### B. As a tarball (offline fallback)

```bash
# workstation, from the repository root
make docker                                  # fronius-monitoring:local
docker save fronius-monitoring:local | gzip > fronius-monitoring.tgz

# NAS, over SSH
gunzip -c fronius-monitoring.tgz | docker load
```

The image embeds the interface: there is no asset directory to copy.

### C. Built on the NAS itself (classic builder, no registry)

The Dockerfile is self-contained: its stages pull the Node, Go and Alpine images
themselves, so the NAS needs **only Docker** — no Go, no Node, no buildx, no
registry. Check the repository out on the NAS (or copy it there), then:

```bash
docker build -t fronius-monitoring:local .
# no BuildKit API? the classic builder builds the same Dockerfile:
DOCKER_BUILDKIT=0 docker build -t fronius-monitoring:local .
```

The Dockerfile uses only features the classic builder supports (`COPY --from`,
build arguments; the `# syntax=` line is a comment to it), so both builders
produce the same image. `make docker` honours `DOCKER_BUILDKIT=0` too. The first
build downloads the whole toolchain, and without BuildKit there is **no
cross-architecture build**: this route is for a NAS whose architecture the NAS
itself has. The compose file then uses the local name directly (§ 2).

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

With the registry route, pull the image first — a private package needs the
login here too — and tell compose which image to use from `.env`:

```bash
docker login forgejo.example.com -u testuser
docker pull forgejo.example.com/testuser/fronius-monitoring:0.1.0
echo 'FRONIUS_IMAGE=forgejo.example.com/testuser/fronius-monitoring:0.1.0' >> .env
```

`FRONIUS_IMAGE` is interpolated into all three services, so the app and demo
profiles use it alike, and the `build:` sections stay available for development.
The NAS must not build: start with `--no-build`, which makes a missing or
misspelled image fail loudly instead of starting a build there.

Then, in that directory:

```bash
docker compose --profile app up -d --no-build
docker compose ps
```

### A compose for a shared external network

A NAS that already runs a shared Docker network — Homepage, a reverse proxy, the
other containers — does not need the development file: the service, its volume
and that network are enough. This is a complete, self-contained deployment
(`compose.yml` in the deployment directory, image from the registry route of § 1):

```yaml
name: fronius-monitoring

services:
  froniusd:
    image: ${FRONIUS_IMAGE:-fronius-monitoring:local}
    container_name: fronius-monitoring
    restart: unless-stopped
    networks:
      - homelab
    ports:
      # host:container. 8085 only because 8080 is already taken on this host;
      # the container always listens on 8080.
      - "8085:8080"
    volumes:
      - fronius-data:/data
    environment:
      FRONIUS_SOLARAPI_URL: ${FRONIUS_SOLARAPI_URL:-}
      FRONIUS_TZ: ${FRONIUS_TZ:-Europe/Brussels}
      FRONIUS_DEVICE_USER: ${FRONIUS_DEVICE_USER:-}
      FRONIUS_DEVICE_PASSWORD: ${FRONIUS_DEVICE_PASSWORD:-}
      FRONIUS_POLL_INTERVAL: ${FRONIUS_POLL_INTERVAL:-5s}
      FRONIUS_POLL_JITTER: ${FRONIUS_POLL_JITTER:-500ms}
      FRONIUS_HTTP_TIMEOUT: ${FRONIUS_HTTP_TIMEOUT:-5s}
      FRONIUS_STALE_AFTER: ${FRONIUS_STALE_AFTER:-30s}
      FRONIUS_DETECT_INTERVAL: ${FRONIUS_DETECT_INTERVAL:-60s}
      FRONIUS_LOG_LEVEL: ${FRONIUS_LOG_LEVEL:-info}
      FRONIUS_LOG_FORMAT: ${FRONIUS_LOG_FORMAT:-text}
      FRONIUS_RETENTION_DAYS: ${FRONIUS_RETENTION_DAYS:-0}
    stop_grace_period: 20s

volumes:
  fronius-data:

networks:
  homelab:
    external: true
```

Three points specific to this shape:

- `container_name` is what makes `http://fronius-monitoring:8080` resolvable
  from another container on the same network (Homepage's widget). The trade-off:
  only one stack per host can use that name.
- **Port**: inside the shared network the container listens on **8080**; the
  published **8085** is only for clients outside Docker (a browser, a widget's
  `href` pointing at the host). Homepage's server-side `url` uses 8080.
- **Exposure**: the published port answers on every interface, and the API has no
  authentication (ADR 0005). Bind it to the NAS's LAN address
  (`"192.168.1.50:8085:8080"`) or do not publish it at all and keep the
  container on the shared network.

### What compose must pass explicitly

Compose interpolates `.env` only for variables its own file names. A variable in
`.env` that the service's `environment:` does not list never reaches the
container, and the image default applies — which is how a
`FRONIUS_HTTP_TIMEOUT` set in `.env` can be silently ignored (observed on a NAS
deployment, where the 5 s default expired before the inverter answered). The
block above names every knob; what each one changes:

| Variable | Default | On a NAS |
|---|---|---|
| `FRONIUS_SOLARAPI_URL` | `http://127.0.0.1:8081` | inside a container, loopback is the container: set the inverter's LAN address, or the service polls itself |
| `FRONIUS_HTTP_TIMEOUT` | `5s` | a NAS-to-inverter path (especially Wi-Fi) can exceed it; the observed case needed **20s** |
| `FRONIUS_POLL_INTERVAL` / `FRONIUS_POLL_JITTER` | `5s` / `500ms` | measurement cadence and its spread |
| `FRONIUS_STALE_AFTER` | `30s` | when the interface declares the last sample stale |
| `FRONIUS_DETECT_INTERVAL` | `60s` | identity, meter, battery and event reads |
| `FRONIUS_LOG_LEVEL` / `FRONIUS_LOG_FORMAT` | `info` / `text` | container logs |
| `FRONIUS_RETENTION_DAYS` | `0` (unlimited) | bounds the database |
| `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` (or `_FILE`) | unset | internal paths only; unset keeps them `unknown` while the Solar API works |

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
