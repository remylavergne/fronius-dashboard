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
