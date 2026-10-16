# ADR 0004 — Supervision / commands separation

## Status

Accepted (phase 1)

## Context

The application queries a real inverter. An erroneous read produces false yet correctable data; an
erroneous write can modify hardware settings or the electrical grid, at a cost that is hard to reverse.
No firmware write permission has been established: a string found in the JS bundle is not an
authorization granted by the server.

Phase 1 aims at supervision (collection, historization, display) and has no need to write.

## Decision

Phase 1 is strictly read-only, guaranteed by construction and not by convention:

- all HTTP clients are built behind `internal/fronius/readonly.go`: the `ReadOnly` type
  refuses any method other than `GET` before reaching the transport, by returning an error of type
  `KindReadOnly`;
- no code path uses POST/PUT/PATCH/DELETE (verified by grep, step 12 of the plan);
- the `fronius-probe` probe emits only `GET` requests to a single allowlisted endpoint and requires
  `--allow-non-loopback` for any non-local address.

Control of the inverter is out of MVP (phase 6, not planned). It will require a dedicated security
review, an allowlist of named commands, a dry-run mode and an audit log.

## Consequences

- Retries bear only on `GET` requests: no retry can modify the hardware state.
- Any new feature must be classified READ / MUTATION / DANGEROUS in an ADR before being
  implemented.
- No POST/PUT/PATCH/DELETE command can be issued to the inverter until a dedicated security
  review has authorized a named endpoint.
- The attack surface is limited to a single HTTP verb on the hardware side.

## Rejected alternatives

- **Read and write from phase 1**: hardware risk without proof of firmware authorization.
- **"Dry-run by default" with writing possible behind a flag**: the flag becomes a bypass,
  and a dry-run does not prove the absence of a hardware side effect.
- **Commands limited to reading immediate state**: no gain over the already authorized `GET`s.

## Sources

- `internal/fronius/readonly.go`, `internal/fronius/froniuserr/errors.go`
- `docs/PROBE-READONLY.md`, `cmd/fronius-probe`
- `docs/ARCHITECTURE.md` (read-only invariant)
- `06-SECURITY-AND-TESTS.md`
- Phase 1 plan, steps 4 and 12
