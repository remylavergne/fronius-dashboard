# Architecture cible — Go + React/shadcn

## Décision proposée (à valider via ADR)
**Monorepo** : `apps/web` (Vite, React, TypeScript, shadcn/ui, Tailwind, Recharts), `apps/api` (Go), `internal/fronius` (client API), `internal/storage`, `internal/http`, `docs/adr`, `test/fixtures`.

```text
Fronius GEN24 (LAN)
  ├─ Solar API (lecture)
  └─ Endpoints locaux Digest (lecture contrôlée)
           │
      Go collector / adapters
           │
   Normalisation + qualité des données
           │
    SQLite (WAL) + agrégations
           │
  Go REST API + SSE (ou WebSocket si justifié)
           │
   React + TS + shadcn/ui + Recharts
```

## Backend Go
- Adapter `solarapi` séparé de `internalapi` : ne pas coupler l'UI aux formats Fronius.
- Client HTTP avec context timeout, retry **uniquement pour lectures idempotentes**, gestion des 401 / nonce `X-WWW-Authenticate`, auth Digest MD5 si nécessaire, pas de logs de secrets.
- Collecte initiale : puissance, énergie, états et informations système ; enregistrer valeurs brutes (optionnel, taille bornée) et séries normalisées.
- SQLite pour installation mono-onduleur ; migrations versionnées, WAL, sauvegardes et rétention/agrégation. Évaluer PostgreSQL seulement si multi-sites ou volume supérieur.
- API Go versionnée `/api/v1` : `/health`, `/system`, `/live`, `/timeseries?from=&to=&step=`, `/energy/daily`, `/events`, `/capabilities`, `/settings` ; SSE `/stream` optionnel.
- Modèle `Capability`: `available`, `requires_meter`, `requires_role`, `verified`, `read_only`, `reason` ; ne jamais afficher une métrique impossible comme zéro.
- Service de commandes séparé, **désactivé par défaut**, liste blanche et confirmation explicite ; aucune écriture gridcode / powerlimits / firmware sans revue de sécurité dédiée.
- Config via fichier/env, secrets via variables d'environnement ou magasin sécurisé, permissions fichiers restrictives. Bind loopback par défaut, proxy authentifié si accès distant.
- Observabilité : logs structurés expurgés, métriques de collecte, état du dernier échantillon, erreurs, latence, reconnexion.

## Frontend React
- React + TS strict, Vite, shadcn/ui + Tailwind ; TanStack Query pour cache/polling, React Router, Recharts pour graphiques ; Zod pour validation des réponses si utile.
- Design responsive desktop/tablette/mobile, modes clair/sombre, unités cohérentes, fuseau local Europe/Brussels configurable.
- Les identifiants Fronius restent **dans le backend** ; pas de `AuthHash`, pas de mot de passe en localStorage.
- États `loading`, `stale`, `offline`, `not available`, `error` ; distinguer valeurs mesurées, estimées, non disponibles.
- i18n FR initialement, architecture extensible EN/DE.

## Déploiement
- Go compilé, frontend statique servi par Go (embed) ou reverse proxy ; Docker Compose optionnel (compatible NAS), healthcheck, volumes persistants.
- LAN-only par défaut ; HTTPS via reverse proxy si nécessaire ; ne pas exposer l'onduleur à Internet.
- Sauvegarde de SQLite, config et documentation ; procédure de restauration testée.

## ADR à rédiger
1. SQLite vs PostgreSQL.
2. SSE vs polling pour temps réel.
3. Interrogation Solar API vs Modbus pour chaque mesure.
4. Séparation supervision / commandes.
5. Auth locale et accès distant.
6. Politique de conservation et d'agrégation des données.
7. Packaging Docker/NAS vs binaire autonome.
