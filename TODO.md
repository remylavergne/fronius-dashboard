# TODO exhaustif — Fronius Go + React

> Cases à cocher par l’agent. Priorité : phases 0 à 6 pour MVP, phase 7 après validation. Aucune mutation de l’onduleur dans le MVP.

## 0 — Cadrage et preuves

- [x] Créer dépôt Git monorepo et README de développement
- [ ] Importer les MD du dossier et référencer les sources originales sans publier de secrets
- [ ] Vérifier l’inventaire matériel/firmware contre l’export PDF
- [ ] Identifier métriques effectivement renvoyées par la Solar API (échantillons anonymisés)
- [ ] Confirmer absence de Smart Meter et batterie ; détecter dynamiquement leur présence future
- [ ] Définir objectifs MVP vs phase 2 (contrôle) et critères de succès
- [x] Choisir licence, stratégie branches, conventions de commit
- [ ] Consigner toutes les inconnues dans un registre de décisions

## 1 — Recherche API et sécurité

- [ ] Documenter `/solar_api/v1/GetPowerFlowRealtimeData.fcgi` et ses champs réellement disponibles
- [ ] Relever autres endpoints Solar API documentés et utiles
- [ ] Extraire méthodes/arguments/DTO des commandes internes utiles en lecture seule
- [ ] Auditer garde Angular `oi.a` et contrôles de composants sans supposer permissions serveur
- [ ] Construire matrice endpoint/méthode/paramètres/rôle déclaré/rôle testé/effet de bord
- [ ] Classifier chaque endpoint READ / MUTATION / DANGEROUS
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

- [ ] Implémenter client HTTP Go configurable avec timeout/context
- [ ] Implémenter adaptateur Solar API en lecture seule
- [ ] Définir DTO bruts versionnés et normalisation en modèle domaine
- [ ] Normaliser unités W/kW/Wh/kWh, timestamps UTC, qualité des données
- [ ] Gérer valeurs absentes/nulles et champs optionnels
- [ ] Gérer 404/401/5xx, JSON invalide, timeout et déconnexion
- [ ] Mettre en place polling configurable avec jitter raisonnable
- [ ] Garantir qu’aucun retry automatique ne déclenche une écriture
- [ ] Ajouter métriques collecteur et logs structurés expurgés
- [x] Implémenter mock/simulateur Fronius et fixtures anonymisées
- [ ] Ajouter tests de contrat à partir des réponses réelles expurgées
- [ ] Préparer client Digest optionnel isolé, sans l’utiliser si inutile au MVP
- [ ] Vérifier que le frontend n’a jamais accès aux identifiants Fronius

## 4 — Persistance et API Go

- [ ] Définir schéma SQLite : measurements, aggregates, events, system_status
- [ ] Ajouter migrations versionnées et WAL
- [ ] Implémenter upsert idempotent et déduplication
- [ ] Agréger minutes/heures/jours avec règles explicites
- [ ] Gérer fuseaux et changement d’heure Europe/Brussels
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
