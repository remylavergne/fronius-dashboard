# Running the service on a NAS (production)

One container, a SQLite database on a local volume, the interface embedded in
the binary. This page covers the deployment that is expected to stay up: the
NAS and the inverter on the **same network**, the image from a registry, and the
settings that matter on real hardware.

The inverter serves the local API only to clients it considers local; the
network section below is the part that most deployments get wrong.

## 1. Prerequisites

- Docker (or a NAS that runs compose projects).
- The NAS and the inverter on the **same L2 network / subnet** — the inverter's
  own. If they are not, read § 5 before starting; every other step is worthless
  until the NAS can reach the inverter.
- A fixed address for the NAS (reserved DHCP lease or static), so the published
  port and the optional source alias below do not change.

Check reachability from the NAS **before** deploying:

```bash
ip -br addr                          # the NAS's own interface and address
ip route get <inverter-ip>           # which interface and source are used
curl -s -o /dev/null -w '%{http_code}\n' --max-time 5 \
  http://<inverter-ip>/solar_api/v1/GetPowerFlowRealtimeData.fcgi
```

`200` means the device answers this source. If it answers `000` while
`ping <inverter-ip>` succeeds, go to § 5: ICMP can pass while the device refuses
the TCP source.

## 2. The compose file

Create the deployment directory (for example `/srv/fronius/`) with two files.

`compose.yml`:

```yaml
name: fronius-monitoring

services:
  froniusd:
    image: ${FRONIUS_IMAGE:?set FRONIUS_IMAGE to the registry reference}
    container_name: fronius-monitoring
    restart: unless-stopped
    ports:
      # host:container. The container always listens on 8080; the host port is
      # yours. Bind to the NAS address rather than 0.0.0.0: the API has no
      # authentication (ADR 0005).
      - "${FRONIUS_BIND:-127.0.0.1}:${FRONIUS_PORT:-8080}:8080"
    volumes:
      - fronius-data:/data
    environment:
      # Compose only passes variables named here; anything else in .env is
      # silently ignored and the image default applies.
      FRONIUS_SOLARAPI_URL: ${FRONIUS_SOLARAPI_URL:?set the inverter URL}
      FRONIUS_TZ: ${FRONIUS_TZ:-Europe/Brussels}
      FRONIUS_DEVICE_USER: ${FRONIUS_DEVICE_USER:-}
      FRONIUS_DEVICE_PASSWORD: ${FRONIUS_DEVICE_PASSWORD:-}
      FRONIUS_POLL_INTERVAL: ${FRONIUS_POLL_INTERVAL:-5s}
      FRONIUS_POLL_JITTER: ${FRONIUS_POLL_JITTER:-500ms}
      FRONIUS_HTTP_TIMEOUT: ${FRONIUS_HTTP_TIMEOUT:-20s}
      FRONIUS_STALE_AFTER: ${FRONIUS_STALE_AFTER:-90s}
      FRONIUS_DETECT_INTERVAL: ${FRONIUS_DETECT_INTERVAL:-60s}
      FRONIUS_LOG_LEVEL: ${FRONIUS_LOG_LEVEL:-info}
      FRONIUS_LOG_FORMAT: ${FRONIUS_LOG_FORMAT:-text}
      FRONIUS_RETENTION_DAYS: ${FRONIUS_RETENTION_DAYS:-0}
    stop_grace_period: 20s

volumes:
  fronius-data:
```

`.env` (next to it, never versioned):

```ini
FRONIUS_IMAGE=<registry-host>/<owner>/fronius-monitoring:<tag>
FRONIUS_SOLARAPI_URL=http://<inverter-ip>
FRONIUS_TZ=Europe/Brussels
FRONIUS_BIND=<nas-ip>
FRONIUS_PORT=8080
FRONIUS_HTTP_TIMEOUT=20s
FRONIUS_STALE_AFTER=90s

# Optional: only for the internal read paths (clock, events, self-test state).
# The Solar API works without them; with them unset those rows stay "unknown".
# FRONIUS_DEVICE_USER=<user>
# FRONIUS_DEVICE_PASSWORD=<password>
# Or the Docker-secrets route, preferred:
# FRONIUS_DEVICE_USER_FILE=/run/secrets/device_user
# FRONIUS_DEVICE_PASSWORD_FILE=/run/secrets/device_password
```

Values to adjust:

| Setting | How to find it |
|---|---|
| `<inverter-ip>` | the inverter's Wi-Fi/LAN configuration screen |
| `<nas-ip>` | `ip -br addr`, the address on the interface that reaches the inverter |
| `<registry-host>/<owner>` | the registry where the image was pushed |
| `FRONIUS_HTTP_TIMEOUT` | `20s` is a safe production value; a slow NAS-to-inverter path can exceed 5 s |
| `FRONIUS_STALE_AFTER` | keep it above `HTTP_TIMEOUT` + one interval (`90s` here) so one slow cycle is not reported as stale |

`FRONIUS_PORT` is only the **host** side: the container always listens on 8080.
If 8080 is already used on the NAS, pick a free port (for example `8085`) and
keep it consistent in `docker compose ps`, the browser URL and any reverse
proxy.

## 3. Start and verify

```bash
docker compose up -d --no-build
docker compose ps

curl -s http://127.0.0.1:<port>/api/v1/health | tr ',' '\n' | grep -E 'status|last_error|last_run_ms'
curl -s http://127.0.0.1:<port>/api/v1/live  | grep -o '"ts":"[^"]*"'
```

Expected: `status: ok`, an empty `last_error`, `last_run_ms` in the tens of
milliseconds, and a `ts` that advances every few seconds. The interface is
served from the same origin: `http://<nas-ip>:<port>/`.

The first detection cycle takes up to `FRONIUS_DETECT_INTERVAL` (60 s); until
then identity, meter, battery and event rows read `unknown`.

## 4. Where the data lives

The database is the only state, and it lives in the `fronius-data` volume
(`/data/fronius.db`). Keep it on **local storage** of the NAS, never on a
CIFS/NFS share: SQLite needs POSIX locking and faithful `fsync`, and a network
hiccup becomes a write failure.

Backup (the file is consistent while the service runs):

```bash
docker compose exec -T froniusd /app/fronius-backup save -out /data/backup.db
docker compose cp froniusd:/data/backup.db ./fronius-$(date +%F).db
docker compose exec -T froniusd /app/fronius-backup verify -file /data/backup.db
```

`FRONIUS_RETENTION_DAYS=0` keeps everything; set a positive value to bound the
database (the purge runs once a day, at local 03:00).

## 5. When the NAS is not on the inverter's subnet

The inverter answers clients inside **its own subnet**. A NAS on another subnet
may see `ping` succeed while TCP requests hang or time out — the router forwards
the SYN, the inverter replies directly, and a stateful firewall then drops the
NAS's following packets as out-of-state. The interface shows
`fronius: timeout | … context deadline exceeded`, and the UIs stay empty.

Fix it by giving the NAS an address in the inverter's subnet and sourcing the
collector's traffic from it. Adjust every value to your network — the inverter's
netmask (e.g. `255.255.254.0` is `/23`) defines the subnet, and the chosen
address must be free and outside the DHCP pool.

```bash
# <interface> is the one shown by `ip route get <inverter-ip>` (ovs_eth0 on
# Synology, eth0 elsewhere). Pick an unused address in the inverter's subnet.
sudo ip addr replace <nas-address-in-inverter-subnet>/<prefix> dev <interface>

# Stay on the direct path: a router's ICMP redirect would send these packets
# into the asymmetric path that breaks TCP.
sudo sysctl -w net.ipv4.conf.all.accept_redirects=0
sudo sysctl -w net.ipv4.conf.<interface>.accept_redirects=0

# The container's traffic is NATed to the host's primary address by Docker;
# rewrite it to the alias so the device sees a local client.
sudo iptables -t nat -I POSTROUTING 1 -d <inverter-ip> -j SNAT --to-source <nas-address-in-inverter-subnet>
```

Verify from the NAS, then from the container:

```bash
ip route get <inverter-ip>            # must use <interface> and the alias as source
curl --interface <nas-address-in-inverter-subnet> -s -o /dev/null \
  -w '%{http_code}\n' --max-time 5 http://<inverter-ip>/solar_api/v1/GetPowerFlowRealtimeData.fcgi
docker exec fronius-monitoring wget -T 8 -qO- \
  http://<inverter-ip>/solar_api/v1/GetPowerFlowRealtimeData.fcgi | head -c 60
```

Both must answer. Make it survive reboots with a boot-up task (root):

```sh
for i in $(seq 1 60); do ip link show <interface> >/dev/null 2>&1 && break; sleep 2; done
ip addr replace <nas-address-in-inverter-subnet>/<prefix> dev <interface>
sysctl -w net.ipv4.conf.all.accept_redirects=0
sysctl -w net.ipv4.conf.<interface>.accept_redirects=0

for i in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 2; done
if ! iptables -t nat -C POSTROUTING -d <inverter-ip> -j SNAT --to-source <nas-address-in-inverter-subnet> 2>/dev/null \
   && ! iptables -t nat -C DEFAULT_POSTROUTING -d <inverter-ip> -j SNAT --to-source <nas-address-in-inverter-subnet> 2>/dev/null; then
  iptables -t nat -I POSTROUTING 1 -d <inverter-ip> -j SNAT --to-source <nas-address-in-inverter-subnet>
fi
```

`DEFAULT_POSTROUTING` is Synology's own chain; the check covers both so a
duplicate rule is never added.

## 6. Pitfalls

| Symptom / trap | Cause | Fix |
|---|---|---|
| every capability "not determined", `dial tcp 127.0.0.1:8081` | `FRONIUS_SOLARAPI_URL` unset or still the example; inside a container loopback is the container | set the inverter's LAN address |
| the address in `.env` has no effect | compose only passes variables its `environment:` block names | add the variable (or use the compose file above) |
| `ping` works, HTTP times out | NAS on another subnet, or asymmetric routing (see § 5) | source alias + SNAT, or move the NAS to the inverter's subnet |
| intermittent 20 s timeouts with a healthy device elsewhere | the inverter serves only its own subnet; a foreign source is starved | § 5 |
| `context deadline exceeded` | the client's `FRONIUS_HTTP_TIMEOUT` expired; the device did not answer in time | raise the timeout (20 s production), and fix the path |
| interface works, data "stale" | no sample within `FRONIUS_STALE_AFTER` | check `/api/v1/health` `last_error`; the threshold must exceed timeout + interval |
| energy of the day empty | no sample within 10 min of local midnight (collector down at the time) | it appears after the next complete local day |
| energy of the year empty | no sample at or before 1 January in the database | first possible on 2027-01-01 |
| `since monitoring` restarts | the total counter went backwards (device reset, other inverter in the same DB) | expected; it measures from the current counter run |
| two collectors on one inverter | e.g. the NAS plus a laptop running `make dev-device` | run one; the device's HTTP server is small |
| database corruption or write errors | the volume is on a CIFS/NFS share | keep it on local storage |
| data exposed beyond the LAN | the API has no authentication | bind the published port to the NAS address; Tailscale/LAN only |

## 7. Updating

```bash
docker compose pull
docker compose up -d --no-build
```

The volume is untouched; the version is visible in `GET /api/v1/health` and on
the System screen.
