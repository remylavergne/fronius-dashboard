# ADR 0005 — Authentication and remote access

## Status

Accepted (phase 1)

## Context

The REST API of phase 1 embeds no authentication mechanism. Exposing an API without
authentication on a local network amounts to publishing the household's production and consumption
curves. The project aims to be local by default.

The official client also stores credentials in cleartext in `localStorage` (key `AuthHash`,
module `CNEH`) — a practice that is never reproduced on the application side.

## Decision

In phase 1, the REST API has no authentication and therefore listens only on the loopback interface
(default `127.0.0.1:8080`). `froniusd` refuses at startup any non-local address (`127.0.0.1`, `::1`,
`localhost`) as long as `FRONIUS_ALLOW_REMOTE=1` is not set explicitly, with an error message
pointing to this ADR:

```
refusal: {addr} is not a local address; set FRONIUS_ALLOW_REMOTE=1 after reading docs/adr/0005-authentication-and-remote-access.md
```

## Consequences

- Remote access is possible only after reading this ADR and making a conscious choice.
- No Fronius authentication data transits to the frontend.
- Any credentials of the internal API (phase 2) will stay in the backend environment, never
  in the React bundle or in the repository.
- The need for application-level authentication (token or password, possibly proxied over HTTPS)
  will have to be decided by a later ADR before any permanent remote access.
- The loopback guard is tested (`internal/config`, `froniusd` tests).

## Rejected alternatives

- **Listening on `0.0.0.0` with a documentary warning**: the warning does not prevent exposure.
- **Simple password authentication from phase 1**: adds a secret to manage before the exposure
  surface exists.
- **Reusing the inverter's Digest authentication**: couples UI access to a hardware account and
  does not protect the API itself.

## Sources

- `internal/config/config.go` (loopback guard, `FRONIUS_ALLOW_REMOTE`)
- `cmd/froniusd/main.go`
- `docs/SOURCES.md` (module `CNEH`, cleartext credentials in `localStorage`)
- `06-SECURITY-AND-TESTS.md`
- Phase 1 plan, steps 3 and 9
