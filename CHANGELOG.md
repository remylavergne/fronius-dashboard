# Changelog

Toutes les modifications notables de ce projet sont documentées dans ce fichier.

Le format suit [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) et le
projet adhère au [versionnage sémantique](https://semver.org/lang/fr/).

## [Unreleased]

### Added

- 2026-10-16 — **Phase 2 : lecture authentifiée des chemins internes.**
  `internal/secret` : type `secret.String` qui masque sa valeur pour `fmt`,
  `slog` et `encoding/json`, `Reveal()` étant la seule sortie en clair.
  `internal/fronius/digest` : transport Digest RFC 2617 conforme au client
  officiel (défi lu d'abord dans `x-www-authenticate`, **un** seul retry sur
  401, `nc` décimal zéro-complété incrémenté sous verrou, `algorithm=` jamais
  émis, `cnonce` aléatoire au lieu du `"NaN"` du fabricant), en-tête jamais
  envoyé préemptivement ni vers un hôte étranger (`ErrForeignHost`).
  `internal/fronius/internalapi` : liste blanche exécutable de quatre chemins
  (`/status/version`, `/status/activeEvents`, `/components/PowerMeter/readable`,
  `/components/BatteryManagementSystem/readable`) et extraction de l'identité par
  listes de clés candidates ordonnées. `internal/detect` : boucle de détection
  (identité, présence, événements actifs), anti-duplication des événements dans
  `device_events_seen`, abandon d'un chemin après un 401 reçu sans identifiants
  configurés. Nouvelles variables `FRONIUS_DEVICE_USER`,
  `FRONIUS_DEVICE_PASSWORD`, `FRONIUS_DETECT_INTERVAL`, refusées au démarrage si
  la paire est incomplète.

### Changed

- 2026-10-16 — `internal/model` : les capacités prennent une détection à trois
  états (`unknown`, `absent`, `present`) au lieu de trois booléens de
  configuration. `device.identity` et `device.events` rejoignent la liste ;
  `battery.soc` reste indisponible **même batterie détectée** (détecter un
  composant ne prouve pas qu'une mesure d'état de charge existe côté API).
  `internal/api` : `/api/v1/system` expose `device` (identité nullable :
  une valeur vide devient `null`, jamais `""`) et `detection` (état et motif des
  quatre ressources, plus `last_error`) ; `/api/v1/capabilities` dérive de la
  détection persistée. Migration `0002_device.sql` : table `device` à ligne
  unique (états à trois valeurs **et** leur cause), table `device_events_seen`,
  et purge des marques d'anti-duplication avec celle des événements.
  `cmd/fronius-probe` : liste blanche portée à cinq endpoints, `--endpoint`
  (tout chemin hors liste est refusé), `--password-stdin`, et noms de fichiers
  dérivés du chemin complet (les deux composants finissaient tous deux par
  `readable` et se seraient écrasés).
- 2026-10-16 — Interface : page Système refondue (`DevicePanel`) affichant
  l'identité avec sa source et son âge, puis les quatre détections avec leur état
  et leur motif ; les trois états sont traduits distinctement (« présent »,
  « absent », « non déterminé »), et un champ absent s'affiche en tiret — jamais
  `0`, jamais « inconnu ».

### Security

- 2026-10-16 — Garde CI : `git grep` refuse toute ligne `FRONIUS_DEVICE_PASSWORD`
  non vide dans le dépôt ; aucun exemple de documentation n'écrit de mot de passe
  après un `=` (saisie par `read -rs` ou `--password-stdin`). Aucun identifiant
  n'est journalisé (vérifié sur le journal de `froniusd` : 0 occurrence) ni
  restitué par l'une des sept routes de l'API (test dédié).

- 2026-10-16 — Socle du dépôt : `.gitignore`, licence MIT, `CHANGELOG.md`,
  `.env.example`, `Makefile`, arborescence `docs/` et correction du chemin
  `references/` dans `AGENTS.md`.

- 2026-10-16 — Simulateur Fronius (`cmd/fronius-sim`) : module Go
  `fronius-supervisor`, `GET /solar_api/v1/GetPowerFlowRealtimeData.fcgi`
  déterministe (seed, courbe solaire, nébulosité lissée), extension `?at=`,
  injection de pannes (`--fault` / `X-Sim-Fault`), `GET /sim/health`, logs
  `log/slog`. Tests unitaires et HTTP déterministes.

- 2026-10-16 — Configuration (`internal/config`) : lecture exclusive par
  variables d'environnement `FRONIUS_*`, validation exhaustive (durées, entier,
  booléen, fuseau, URL, adresse d'écoute, niveau et format de log) avec
  accumulant toutes les erreurs, et garde `CheckListenAddress` qui refuse
  une écoute non locale sans `FRONIUS_ALLOW_REMOTE=1`.

- 2026-10-16 — Erreurs typées (`internal/fronius/froniuserr`) : natures
  `not_found`, `unauthorized`, `server`, `decode`, `timeout`, `transport`,
  `device_status`, `readonly_violation` ; `errors.Is`/`errors.As` et `KindOf`.
  Garde lecture seule (`internal/fronius/readonly.go`) : `ReadOnly` refuse
  toute méthode autre que GET avant d'atteindre le transport.

- 2026-10-16 — Client Solar API (`internal/fronius/solarapi`) : DTO explicites de
  `GetPowerFlowRealtimeData.fcgi` (forme [HYPOTHÈSE], décodage tolérant), borne
  de 1 MiB sur le corps, cartographie complète des erreurs (404, 401/403,
  429/5xx, JSON invalide ou tronqué, `Body.Data` absent, `Head.Status.Code != 0`,
  délai dépassé, serveur injoignable).

- 2026-10-16 — Modèle normalisé (`internal/model`) : `Sample` (unités SI de base
  W/Wh/%, horodatage UTC, `Quality` measured/estimated/missing), conversion
  `FromPowerFlow`, capacités avec prérequis, preuves et motif d'indisponibilité,
  événements locaux typés. Invariant testé : un champ absent reste `nil`, un `0`
  explicite de l'appareil reste `0`.

- 2026-10-16 — Documentation de référence : `docs/SOURCES.md` (registre de
  provenance, vérifications exécutées sur le bundle, schéma Solar API supposé),
  `docs/ENDPOINTS.md` (classification READ/MUTATION/DANGEROUS des 79 chemins
  inventoriés + liste blanche phase 1) et les ADR 0001 à 0007.

- 2026-10-16 — Persistance SQLite (`internal/storage`) : dépendance unique
  `modernc.org/sqlite v1.60.1` (pur Go, `CGO_ENABLED=0` vérifié), migrations
  embarquées et versionnées (`schema_migrations`), WAL + `busy_timeout` 5000 ms
  + `synchronous=NORMAL`, upsert idempotent sur `(ts_utc, source)`, buckets
  alignés sur l'epoch, énergie journalière par `MAX(E_Day)` en fuseau local avec
  gestion des journées de 23 h et 25 h, événements, cycles de collecte, état du
  collecteur et purge de rétention.

- 2026-10-16 — Collecteur (`internal/collector`) : boucle de polling avec jitter
  (`Interval ± Jitter`) en cas de succès et backoff exponentiel 1 s → 60 s en cas
  d'échec, horloge et source aléatoire injectables, traçage de chaque cycle
  (`collector_runs`), état du collecteur (`last_success_utc`, `last_error`),
  événements `collector_error` (anti-duplication par message) et
  `collector_recovered`. Aucun échantillon n'est inséré sur un cycle en échec.

- 2026-10-16 — API REST (`internal/api`) : enveloppe `{data, meta}`, erreurs
  `{error:{code,message}}`, routes `health`, `system`, `live`, `timeseries`,
  `energy/daily`, `events`, `capabilities` ; 404/405 JSON ; `meta.sampled_at` /
  `stale` / `stale_after_s` sur les endpoints de mesure ; validation de `from`,
  `to`, `step` (dont `auto` borné à 1500 points), plage ≤ 366 j, `limit` ≤ 500 ;
  service statique optionnel avec repli SPA sur `index.html`.

- 2026-10-16 — Binaire `froniusd` (`cmd/froniusd`) : garde d'écoute non locale,
  rétention appliquée au démarrage si `FRONTIUS_RETENTION_DAYS` est fixée,
  remplissage d'historique (`--backfill-days`, refusé hors boucle locale sans
  `--force-backfill`), collecteur en goroutine, API REST, arrêt propre sur
  SIGINT/SIGTERM. Variante de client `GetPowerFlowRealtimeDataAt` (extension
  simulateur `?at=`) réservée au remplissage d'historique.

- 2026-10-16 — Fixtures et tests de contrat (`test/fixtures/solarapi`,
  `internal/fronius/solarapi/contract_test.go`) : quatre fixtures générées par
  le simulateur (seed 42, instant figé) avec champ racine `_provenance`,
  comparaison table-driven du modèle normalisé attendu, vérification que le
  champ `_provenance` n'est pas consommé, que `Head.Status.Code = 7` remonte en
  `device_status`, que la fixture tronquée produit `KindDecode`, et qu'aucune
  fixture ne contient de donnée sensible.

- 2026-10-16 — Sonde lecture seule (`cmd/fronius-probe`) : liste blanche d'un seul
  GET, refus des adresses non locales sans `--allow-non-loopback` (avant toute
  connexion), expurgation structurelle du corps et des en-têtes sensibles avant
  affichage et écriture, `--save` en mode `0600`, `--list`. Tests couvrant
  l'expurgation, le refus de la garde et l'absence de méthode non-GET.

- 2026-10-16 — `docs/PROBE-READONLY.md` (protocole d'accord préalable, requête
  exacte, sortie attendue, règles d'expurgation, promotion d'une fixture,
  interdiction de toute méthode non-GET) ; matrice
  endpoint/méthode/paramètres/rôle déclaré/rôle testé/effet de bord dans
  `docs/ENDPOINTS.md` ; endpoints Solar API candidats `[À-TESTER]` pour
  l'identité de l'appareil et la détection compteur/batterie dans
  `docs/SOURCES.md`.

- 2026-10-16 — Interface React (`apps/web`) : Vite 8 + React 19 + TypeScript 5.9
  en mode strict (`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`,
  `verbatimModuleSyntax`), shadcn/ui (base Radix, style `radix-nova`), Tailwind 4,
  TanStack Query, React Router 7, Recharts 3.10, zod 4. Quatre routes (vue
  d'ensemble, système, capacités, paramètres), schémas zod miroirs des DTO Go,
  `fetchJSON` classant les échecs en `network`/`http`/`schema`, i18n FR plate
  prête pour EN/DE, états loading/error/empty/offline/stale, courbe 24 h/7 j/30 j
  ou plage personnalisée avec trous réellement visibles, tableau des capacités,
  navigation clavier et résumé textuel accessible du graphique. 56 tests vitest.

- 2026-10-16 — Documentation d'exploitation : `docs/ARCHITECTURE.md` (flux,
  paquets, six invariants, points de fragilité), `docs/DEVELOPMENT.md`
  (prérequis, commandes, variables, stratégie de tests, pièges rencontrés),
  `docs/RUNBOOK.md` (démarrage, diagnostic par symptôme, sauvegarde et
  restauration SQLite, rétention, maintenance) et `docs/UNKNOWNS.md` (registre
  des dix inconnues avec décision provisoire et condition de levée).

- 2026-10-16 — Intégration continue : `.github/workflows/ci.yml` avec deux tâches,
  `go` (`go vet`, `go test ./... -race`, `CGO_ENABLED=0 go build ./...`) et
  `web` (`pnpm install --frozen-lockfile`, `lint`, `test --run`, `build`).

### Changed
- 2026-10-16 — `go.mod` passe à `go 1.26.0` : `modernc.org/sqlite v1.60.1`
  déclare lui-même `go 1.26.0`, la directive `go 1.24` prévue au plan n'est donc
  pas réalisable avec ce pilote épinglé. Le toolchain Go 1.26.0 est utilisé
  (détection automatique).
