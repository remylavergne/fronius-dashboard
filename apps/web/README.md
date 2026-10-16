# Interface de supervision

Frontend React de `froniusd` : consultation locale d'un Fronius GEN24 (production,
énergies, capacités, état du collecteur).

## Démarrage

```bash
pnpm install
pnpm dev          # http://127.0.0.1:5173
```

Le proxy Vite relaie `/api` vers `http://127.0.0.1:8080` (`vite.config.ts`) :
`froniusd` doit tourner. Le frontend ne connaît ni l'adresse de l'onduleur ni un
identifiant — la phase 1 n'en détient aucun.

## Commandes

| Commande | Effet |
|---|---|
| `pnpm dev` | serveur de développement |
| `pnpm build` | typecheck strict (`tsc -b`) puis build de production |
| `pnpm test` / `pnpm test --run` | tests vitest (jsdom) |
| `pnpm typecheck` | `tsc -b` seul |
| `pnpm lint` | oxlint |

Build servi directement par le backend :

```bash
pnpm build
cd .. && FRONIUS_WEB_DIR=apps/web/dist ../bin/froniusd
```

## Structure

| Chemin | Rôle |
|---|---|
| `src/lib/schema.ts` | Schémas zod **miroirs** de `internal/api/dto.go` |
| `src/lib/api.ts` | `fetchJSON` : échecs classés `network` / `http` / `schema` |
| `src/lib/queries.ts` | Hooks TanStack Query (polling 5 s) et plages de temps |
| `src/lib/chart.ts` | Buckets → points de graphique, trous matérialisés en `null` |
| `src/lib/freshness.ts` | Décision de péremption affichée |
| `src/routes/` | Vue d'ensemble, Système, Capacités, Paramètres |
| `src/components/ui/` | Composants shadcn/ui générés (ne pas éditer à la main) |
| `src/i18n/` | Dictionnaire plat FR, prêt pour EN/DE |

Deux règles à respecter :

1. **Toute évolution de `internal/api/dto.go` doit être répercutée dans
   `src/lib/schema.ts`** : c'est ce qui détecte la dérive entre backend et
   interface.
2. **Ne jamais remplacer une mesure absente par `0`.** L'API renvoie `null`,
   l'affichage un tiret ; l'invariant « pas de zéro inventé » va jusqu'à l'écran.

Documentation complète : `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`.
