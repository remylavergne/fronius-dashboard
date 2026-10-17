# ADR 0007 — Packaging

## Status

Accepted (phase 1)

## Context

Phase 1 must be verifiable on a development machine: collect from the simulator,
historize, serve the REST API and display the React dashboard. The frontend is a Vite project with
hot reload; the backend is a Go binary.

The final target is an installation on a NAS, but the installation unit and the deployment mode are
not yet decided.

## Decision

In phase 1, three standalone binaries and a Vite development server:

- `froniusd`: collector + REST API + optional static service of `FRONIUS_WEB_DIR`;
- `fronius-sim`: simulator;
- `fronius-probe`: read-only probe.

They are compiled with `CGO_ENABLED=0`, without `go:embed` of the React assets. Container packaging
(multi-stage Dockerfile, compose, healthcheck, NAS installation documentation) and the integration of
assets by `go:embed` are deferred to phase 5.

## Consequences

- No `go:embed` must be introduced before phase 5.
- The static service merely serves `FRONIUS_WEB_DIR` with an SPA fallback to `index.html`; if the
  variable is empty, no static content is served.
- `CGO_ENABLED=0` already proves the absence of a native dependency (verified in CI).
- Operation on a NAS depends on phase 5; no systemd unit or image is produced in phase 1.
- The frontend build cycle remains independent of the backend's.

## Rejected alternatives

- **Single binary with `go:embed` from phase 1**: freezes the frontend build cycle into the
  backend's, with no identified need.
- **Docker from phase 1**: adds a dependency on Docker for a local application already runnable
  as a binary.
- **Native systemd service in phase 1**: the installation unit is not yet known.

## Sources

- `Makefile` (`build`, `dev-sim`, `dev-api`, `dev-web`)
- `cmd/froniusd/main.go` (`FRONIUS_WEB_DIR`, SPA fallback)
- `.github/workflows/ci.yml` (`CGO_ENABLED=0 go build ./...`)
- Phase 5 roadmap (packaging, `go:embed`, multi-stage Dockerfile, compose, E2E)

## Amendment (2026-10-17) — container packaging delivered

The deferred packaging is now implemented; the phase-1 decision above stands
unchanged for the binaries.

**Delivered.** A multi-stage image (`Dockerfile`): Node builds the interface,
Go builds the four binaries (`CGO_ENABLED=0`, `-trimpath`, a stamped
`-X main.buildVersion`), and a minimal `alpine` runtime carries them with
`tzdata` (a time zone database is required for `FRONIUS_TZ`), `ca-certificates`,
a **non-root** user (uid 10001), a `/data` volume, and a `HEALTHCHECK` on
`/api/v1/health`. A `docker-compose.yml` exposes two profiles: `app` for a real
installation, `demo` for the simulator plus a day of synthetic history on
its own volume and port (the backfill precedes the listener, so its first start
takes about a minute). `make docker` builds the image with the git revision as
its version.

**Where the ADR 0005 boundary moved.** Inside a container the process must bind
`0.0.0.0`, which the loopback guard refuses unless `FRONIUS_ALLOW_REMOTE=1`; the
image therefore sets it, and the boundary becomes the **published port** — the
compose file publishes on `127.0.0.1` only. The flag and the publication are
documented together, because either alone is misleading.

**Delivered since (same day).** `go:embed` of the interface: the build copies
`apps/web/dist` into `internal/webui/assets`, `webui.FS()` embeds it, and the
server prefers `FRONIUS_WEB_DIR` when set, then the embedded copy, then
API-only; the image no longer carries an asset directory, and a path that looks
like an asset but is missing answers the API's JSON 404 rather than the SPA
index. Credentials may also come from files
(`FRONIUS_DEVICE_USER_FILE` / `FRONIUS_DEVICE_PASSWORD_FILE`), which is how
Docker secrets are used; the compose block is documented rather than enabled,
because compose validates secret files at parse time and a fresh checkout has
none — a commented block would otherwise break the demo profile. NAS guidance is
in `docs/NAS.md`; it is **untested on a NAS**.

**Still deferred.** TLS/CORS/CSP, and secret *distribution* at scale (Swarm or
an external store) beyond files.

**Consequences.** The image is the deployment unit on a NAS; the version comes
from `--build-arg VERSION` and reads `dev` without one; a **bind mount** of the
data directory needs its host ownership adjusted for uid 10001, which the
runbook states — the named volume does not.

**Sources.** `Dockerfile`, `docker-compose.yml`, `Makefile` (`docker`),
`docs/RUNBOOK.md` § Docker, `docs/adr/0005`.
