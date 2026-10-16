# Monitoring interface

React frontend for `froniusd`: local consultation of a Fronius GEN24 (production,
energies, capabilities, collector status).

## Getting started

```bash
pnpm install
pnpm dev          # http://127.0.0.1:5173
```

The Vite proxy relays `/api` to `http://127.0.0.1:8080` (`vite.config.ts`):
`froniusd` must be running. The frontend knows neither the inverter address nor
any credential — phase 1 holds none.

## Commands

| Command | Effect |
|---|---|
| `pnpm dev` | development server |
| `pnpm build` | strict typecheck (`tsc -b`) then production build |
| `pnpm test` / `pnpm test --run` | vitest tests (jsdom) |
| `pnpm typecheck` | `tsc -b` alone |
| `pnpm lint` | oxlint |

Build served directly by the backend:

```bash
pnpm build
cd .. && FRONIUS_WEB_DIR=apps/web/dist ../bin/froniusd
```

## Structure

| Path | Role |
|---|---|
| `src/lib/schema.ts` | zod schemas **mirroring** `internal/api/dto.go` |
| `src/lib/api.ts` | `fetchJSON`: failures classified as `network` / `http` / `schema` |
| `src/lib/queries.ts` | TanStack Query hooks (5 s polling) and time ranges |
| `src/lib/chart.ts` | Buckets → chart points, gaps materialised as `null` |
| `src/lib/freshness.ts` | Staleness decision displayed |
| `src/routes/` | Overview, System, Capabilities, Settings |
| `src/components/ui/` | Generated shadcn/ui components (do not edit by hand) |
| `src/i18n/` | Flat EN dictionary, ready for FR/DE |

Two rules to follow:

1. **Any change to `internal/api/dto.go` must be reflected in
   `src/lib/schema.ts`**: this is what detects drift between the backend and the
   interface.
2. **Never replace an absent measurement with `0`.** The API returns `null`, the
   display a dash; the "no invented zero" invariant goes all the way to the
   screen.

Full documentation: `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`.
