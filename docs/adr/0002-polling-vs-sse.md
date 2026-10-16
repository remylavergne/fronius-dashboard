# ADR 0002 — Polling vs SSE

## Status

Accepted (phase 1)

## Context

Two streams must be carried: server-side collection (from the hardware to the database) and
client-side display (from the REST API to the browser). The source of truth is a device that pushes
no data: the inverter is polled periodically. The official client polls its power endpoint every
5,000 ms, which sets the order of magnitude of the useful freshness.

The frontend must distinguish fresh data from stale data, and never block the interface on a
persistent stream.

## Decision

- Server-side collection: polling in a loop, interval of 5 s plus a jitter (`FRONIUS_POLL_INTERVAL`,
  `FRONIUS_POLL_JITTER`, default 500 ms). On failure, exponential backoff 1 s → 2 s → … → 60 s.
- Client-side display: polling of the REST API through TanStack Query, `refetchInterval: 5000`,
  `staleTime: 4000`, `retry: 1`, `throwOnError: false`.
- SSE is not implemented in phase 1.

## Consequences

- Display latency is on the order of the collection interval (plus the revalidation cycle), which is
  consistent with the data source itself.
- The network cost stays low and predictable: one REST request per cycle and per active view.
- `meta.stale` is computed server-side (`FRONIUS_STALE_AFTER`, default 30 s), accompanied by
  `meta.sampled_at` and `meta.stale_after_s`, so that the UI can display a "stale data" badge and an
  offline banner without a client-side heuristic.
- The absence of additional server state simplifies deployment and testing.

## Rejected alternatives

- **SSE**: adds a persistent connection, server state and reconnection cases to test, for a marginal
  latency gain on a data producer that is itself polled.
- **WebSocket**: oversized; the bidirectional channel is useless since hardware writing is
  out of scope (see ADR 0004).
- **Long polling**: complexity equivalent to plain polling, with no measurable benefit here.

## Sources

- `docs/ARCHITECTURE.md` (data flow, invariants)
- `internal/collector` (loop, jitter, backoff)
- `internal/api` (endpoints `live`, `health`, `meta.stale`, `meta.sampled_at`)
- `apps/web/src/lib/queries.ts` (TanStack Query settings)
- Phase 1 plan, steps 8 and 9
