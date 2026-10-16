# TODO exhaustif — Fronius Go + React

> Cases à cocher par l’agent. Priorité : phases 0 à 6 pour MVP, phase 7 après validation. Aucune mutation de l’onduleur dans le MVP.

## 0 — Cadrage et preuves

- [x] Créer dépôt Git monorepo et README de développement
- [ ] Importer les MD du dossier et référencer les sources originales sans publier de secrets
- [ ] Vérifier l’inventaire matériel/firmware contre l’export PDF
- [ ] Identifier métriques effectivement renvoyées par la Solar API (échantillons anonymisés)
- [ ] Confirmer absence de Smart Meter et batterie ; détecter dynamiquement leur présence future
- [x] Définir objectifs MVP vs phase 2 (contrôle) et critères de succès
- [x] Choisir licence, stratégie branches, conventions de commit
- [ ] Consigner toutes les inconnues dans un registre de décisions

## 1 — Recherche API et sécurité

- [ ] Documenter `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` et ses champs réellement disponibles
- [ ] Relever autres endpoints Solar API documentés et utiles
- [ ] Extraire méthodes/arguments/DTO des commandes internes utiles en lecture seule
- [ ] Auditer garde Angular `oi.a` et contrôles de composants sans supposer permissions serveur
- [ ] Construire matrice endpoint/méthode/paramètres/rôle déclaré/rôle testé/effet de bord
- [x] Classifier chaque endpoint READ / MUTATION / DANGEROUS
- [ ] Analyser module `ytu1` Digest, gestion nonce et cas d’erreur
- [ ] Vérifier gestion 401/403, nonce expiré et cas d’absence d’auth
- [ ] Documenter récupération Technician et compteur sans effectuer d’essai
- [ ] Vérifier anciens profils/gestionnaire de mots de passe si récupération encore souhaitée
- [ ] Documenter `SupportAccount`, SDP, ServiceFile comme non validés côté firmware
- [ ] Exclure des tests actifs les réglages réseau électrique, firmware et récupération

## 2 — Architecture et outillage

- [ ] ADR SQLite vs PostgreSQL
- [ ] ADR SSE vs polling
- [ ] ADR Solar API vs Modbus vs API interne
- [ ] ADR authentification et accès distant
- [ ] ADR stockage/rétention et agrégations
- [ ] ADR packaging Go statique/Docker Compose/NAS
- [ ] Initialiser module Go et structure internal/
- [ ] Initialiser Vite React TypeScript strict et shadcn/ui
- [ ] Installer Tailwind, TanStack Query, React Router, Recharts, Zod si nécessaire
- [ ] Mettre en place lint/format/tests Go + frontend
- [ ] Ajouter CI GitHub Actions ou équivalent
- [ ] Ajouter exemples `.env.example` sans secrets

## 3 — Backend acquisition

- [x] Implémenter client HTTP Go configurable avec timeout/context
- [x] Implémenter adaptateur Solar API en lecture seule
- [x] Définir DTO bruts versionnés et normalisation en modèle domaine
- [x] Normaliser unités W/kW/Wh/kWh, timestamps UTC, qualité des données
- [x] Gérer valeurs absentes/nulles et champs optionnels
- [x] Gérer 404/401/5xx, JSON invalide, timeout et déconnexion
- [x] Mettre en place polling configurable avec jitter raisonnable
- [x] Garantir qu’aucun retry automatique ne déclenche une écriture
- [x] Ajouter métriques collecteur et logs structurés expurgés
- [x] Implémenter mock/simulateur Fronius et fixtures anonymisées
- [ ] Ajouter tests de contrat à partir des réponses réelles expurgées
- [ ] Préparer client Digest optionnel isolé, sans l’utiliser si inutile au MVP
- [ ] Vérifier que le frontend n’a jamais accès aux identifiants Fronius

## 4 — Persistance et API Go

- [x] Définir schéma SQLite : measurements, aggregates, events, system_status
- [x] Ajouter migrations versionnées et WAL
- [x] Implémenter upsert idempotent et déduplication
- [ ] Agréger minutes/heures/jours avec règles explicites
- [x] Gérer fuseaux et changement d’heure Europe/Brussels
- [ ] Définir rétention et purge programmée
- [ ] Implémenter sauvegarde/restauration testée
- [ ] Implémenter `/api/v1/health` et `/api/v1/system`
- [ ] Implémenter `/api/v1/live` et `/api/v1/timeseries`
- [ ] Implémenter `/api/v1/energy/daily` et `/api/v1/events`
- [ ] Implémenter `/api/v1/capabilities` et paramètres non sensibles
- [ ] Implémenter SSE ou polling selon ADR
- [ ] Valider paramètres de plage/date/granularité
- [ ] Paginer et borner requêtes volumineuses
- [ ] Générer OpenAPI et clients TS typés
- [ ] Ajouter tests intégration API+SQLite

## 5 — UI React/shadcn

- [ ] Définir design system, thèmes clair/sombre et navigation responsive
- [ ] Construire shell (sidebar, header, breadcrumb, navigation mobile)
- [ ] Construire cartes KPI production instantanée / énergie du jour / état
- [ ] Construire courbe puissance 24h avec tooltips, unités et sélection de période
- [ ] Construire agrégats journaliers/hebdomadaires/mensuels/annuels
- [ ] Ajouter comparaisons de périodes et calendrier de production
- [ ] Ajouter état onduleur, firmware, réseau et diagnostics disponibles
- [ ] Ajouter événements et filtres par date/sévérité
- [ ] Ajouter page santé du collecteur et dernier rafraîchissement
- [ ] Ajouter page paramètres et test de connectivité lecture seule
- [ ] Ajouter page capacités (mesuré/estimé/indisponible/non vérifié)
- [ ] Masquer les cartes consommation, import/export et autoconsommation sans compteur
- [ ] Implémenter états loading/error/empty/offline/stale
- [ ] Implémenter cache et rafraîchissement via TanStack Query
- [ ] Implémenter graphiques Recharts responsives et accessibles
- [ ] Implémenter export CSV des historiques
- [ ] Ajouter traduction FR avec structure i18n DE/EN
- [ ] Ajouter navigation clavier et vérifications WCAG AA
- [ ] Tester responsive desktop/tablette/mobile

## 6 — Qualité, déploiement, exploitation

- [ ] Tests unitaires parseurs, conversions et agrégations
- [ ] Tests DST, trous temporels, doublons, reboot et données aberrantes
- [ ] Tests API sur erreurs réseau, authentification et corruption JSON
- [ ] Tests composants React et tests E2E sur simulateur
- [ ] Vérifier absence de secrets dans bundle, logs, localStorage, dépôt
- [ ] Sécuriser CORS, cookies, CSP et authentification si accès distant
- [ ] Configurer bind localhost/LAN et HTTPS proxy selon ADR
- [ ] Compiler binaire Go avec assets React intégrés
- [ ] Créer Dockerfile multi-stage et compose optionnel
- [ ] Ajouter volumes, healthcheck, restart policy et gestion de secrets
- [ ] Tester installation NAS/local, démarrage et mises à jour
- [ ] Tester backup et restauration de la base
- [ ] Documenter installation, configuration, maintenance et dépannage
- [ ] Créer captures d’écran et guide utilisateur
- [ ] Vérifier consommation ressources et charge réseau de l’onduleur

## 7 — Phase ultérieure (hors MVP)

- [ ] Identifier besoin exact de contrôle (liste de commandes métier)
- [ ] Pour chaque commande, obtenir preuve d’autorisation et documentation firmware
- [ ] Définir garde-fous, confirmation et journal d’audit des mutations
- [ ] Prévoir dry-run et simulateur des commandes
- [ ] Revue de sécurité spécifique avant toute écriture sur matériel réel
- [ ] Étudier intégration Smart Meter ou autre capteur fiable pour consommation réseau
- [ ] Ajouter batterie et flux énergétiques seulement si matériel détecté
- [ ] Étudier notifications (panne, défaut, production anormale)
- [ ] Étudier multi-onduleurs et profils utilisateurs si besoin réel
- [ ] Réévaluer la procédure de récupération Technician via voie autorisée

## Journal

- 2026-10-16 — Étape 1 : socle du dépôt (`.gitignore`, MIT, `CHANGELOG.md`, `.env.example`,
  `Makefile`, `docs/`), correction `references/` dans `AGENTS.md`. Commit
  `chore: init monorepo`.
- 2026-10-16 — Étape 2 : `go.mod` (`fronius-supervisor`, go 1.24) et simulateur
  `cmd/fronius-sim`. Courbe `max(0, sin(π·(h−7)/12))·nominal·cloud(t)` avec bruit lissé
  dérivé du seed, `?at=` (extension simulateur), pannes `500/503/401/404/timeout/invalid_json/
  empty/status7`, `/sim/health`. Décision : `rel_*` émis à `null` quand leur dénominateur est
  nul (jamais de zéro inventé) ; `P_Akku` reste `null` même avec `--with-meter` car aucune
  batterie n'est modélisée. Vérifié en direct : `P_PV > 0` à 11:39 locale, `P_Grid = null`,
  `Head.Status.Code = 0`, deux appels `?at=` identiques octet pour octet, nuit à 0.
- 2026-10-16 — Étape 3 : `internal/config`, environnement uniquement (aucun fichier de
  configuration en phase 1). Garde `CheckListenAddress` : `froniusd` refusera toute écoute
  non loopback sans `FRONIUS_ALLOW_REMOTE=1`. Les deux cases `TODO.md` mappées à cette étape
  (« objectifs MVP vs phase 2 », « registre des inconnues ») dépendent respectivement de
  l'ADR 0004 (étape 6) et de `docs/UNKNOWNS.md` (étape 13) : elles seront cochées à ces
  étapes, pas ici.
- 2026-10-16 — Étape 4 : `internal/fronius/froniuserr` (erreurs typées, `KindOf`,
  `errors.Is` par nature) et `internal/fronius/readonly.go` (`ReadOnly` : refus structurel
  de POST/PUT/PATCH/DELETE/HEAD, y compris requête nulle et transport nul). La garde sera
  instanciée dans `froniusd` et `fronius-probe` ; un appel non-GET n'atteint jamais le
  réseau.
- 2026-10-16 — Étape 5 : `internal/fronius/solarapi` (`dto.go` = seul lieu où la forme
  supposée du JSON est encodée, `client.go` = transport + cartographie d'erreurs). Vérifié
  par `httptest` : 200 nominal, 404/401/403/429/500/503, corps vide, JSON tronqué, page HTML,
  `Body`/`Data` absent, `P_PV` non numérique, `Head.Status.Code = 7`, délai dépassé,
  serveur fermé, corps > 1 MiB, champs inconnus tolérés, préfixe de chemin conservé.
  L'item « Documenter GetPowerFlowRealtimeData.fcgi » reste ouvert jusqu'à `docs/SOURCES.md`
  (étape 6) puis la réconciliation par la sonde (étape 11).
- 2026-10-16 — Étape 6 : `internal/model` (`measurement.go`, `units.go`, `capabilities.go`,
  `event.go`) avec l'invariant « jamais de zéro inventé » ; ADR 0001 à 0007 ;
  `docs/SOURCES.md` (provenance + vérifications exécutées sur le bundle : `fcgi` 0,
  `RealtimeData` 0, `PowerFlowRealtime` 0, `solar_api` 1, `x-www-authenticate` 7,
  `AuthHash` 15) ; `docs/ENDPOINTS.md` (classification des 79 chemins du bundle, liste
  blanche réduite au seul GET Solar API). Décision de modèle : `Capability.Reason` porte un
  motif d'indisponibilité quand la capacité est indisponible, une réserve quand elle est
  disponible mais non vérifiée (`production.*`), jamais les deux.
- 2026-10-16 — Étape 7 : `internal/storage`. Écart au plan assumé et documenté :
  `modernc.org/sqlite v1.60.1` (version épinglée par le plan) déclare
  `go 1.26.0`, donc `go.mod` porte `go 1.26.0` au lieu de `go 1.24` — garder la
  version épinglée du pilote prime sur la directive de langage, et le toolchain
  1.26 est disponible localement. Conséquence : les idiomes Go 1.26
  (`new(expr)`, `for range n`) sont désormais applicables et ont été adoptés
  (`fptr`/`iptr` de `cmd/fronius-sim` et `pf`/`pi` des tests supprimés).
  Tables : `samples` (WITHOUT ROWID, clé `(ts_utc, source)`), `events`,
  `collector_runs`, `collector_state`, `schema_migrations`. Les agrégats
  matérialisés (`aggregates`) restent reportés en phase 3 par l'ADR 0006 ; la
  purge existe (`PurgeBefore`) mais n'est pas programmée. Trois anomalies
  détectées par les tests et corrigées : découpage des instructions de migration
  avant retrait des commentaires (un `;` dans un commentaire cassait la
  migration), double mise à l'échelle du début de bucket, et deux fixtures de
  test qui ne franchissaient pas réellement minuit local.
- 2026-10-16 — Étape 8 : `internal/collector`. `Sink` inclut `InsertEvent`, absent de la
  définition du plan mais nécessaire au comportement demandé (événement warning anti-dupliqué
  à l'échec, événement de reprise). Décisions : `last_success_utc` porte l'horloge du
  **collecteur** (instant du cycle réussi), pas l'horodatage de l'appareil, qui reste dans
  `Sample.TS` ; un échec de persistance (`InsertSample`) n'écrit ni cycle ni événement de
  panne, car ce n'est pas une panne de l'appareil. « Logs expurgés » est satisfait par
  construction en phase 1 : le backend ne détient aucun identifiant Fronius.
