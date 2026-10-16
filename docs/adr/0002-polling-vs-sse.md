# ADR 0002 — Polling vs SSE

## Statut

Accepté (phase 1)

## Contexte

Deux flux doivent être transportés : la collecte côté serveur (du matériel vers la base) et
l'affichage côté client (de l'API REST vers le navigateur). La source de vérité est un appareil qui ne
pousse aucune donnée : l'onduleur est interrogé périodiquement. Le client officiel interroge son
endpoint de puissance toutes les 5 000 ms, ce qui fixe l'ordre de grandeur de la fraîcheur utile.

Le frontend doit distinguer une donnée fraîche d'une donnée périmée, et ne jamais bloculer l'interface
sur un flux persistant.

## Décision

- Collecte côté serveur : polling en boucle, intervalle de 5 s plus un jitter (`FRONIUS_POLL_INTERVAL`,
  `FRONIUS_POLL_JITTER`, défaut 500 ms). En cas d'échec, backoff exponentiel 1 s → 2 s → … → 60 s.
- Affichage côté client : polling de l'API REST via TanStack Query, `refetchInterval: 5000`,
  `staleTime: 4000`, `retry: 1`, `throwOnError: false`.
- SSE n'est pas implémenté en phase 1.

## Conséquences

- La latence d'affichage est de l'ordre de l'intervalle de collecte (plus le cycle de revalidation),
  ce qui est cohérent avec la source de données elle-même.
- Le coût réseau reste faible et prévisible : une requête REST par cycle et par vue active.
- `meta.stale` est calculé côté serveur (`FRONIUS_STALE_AFTER`, défaut 30 s), accompagné de
  `meta.sampled_at` et `meta.stale_after_s`, pour que l'UI affiche un badge « données périmées » et un
  bandeau hors ligne sans heuristique côté client.
- L'absence d'état serveur supplémentaire simplifie le déploiement et les tests.

## Alternatives écartées

- **SSE** : ajoute une connexion persistante, un état serveur et des cas de reconnexion à tester, pour un
  gain de latence marginal sur un producteur de données lui-même interrogé par polling.
- **WebSocket** : sur-dimensionné ; le canal bidirectionnel est inutile puisque l'écriture matérielle est
  hors périmètre (voir ADR 0004).
- **Long polling** : complexité équivalente au polling simple, sans bénéfice mesurable ici.

## Sources

- `docs/ARCHITECTURE.md` (flux de données, invariants)
- `internal/collector` (boucle, jitter, backoff)
- `internal/api` (endpoints `live`, `health`, `meta.stale`, `meta.sampled_at`)
- `apps/web/src/lib/queries.ts` (réglage TanStack Query)
- Plan phase 1, étapes 8 et 9
