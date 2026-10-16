# ADR 0001 — SQLite vs PostgreSQL

## Statut

Accepté (phase 1)

## Contexte

L'application conserve un échantillon normalisé par cycle de collecte. L'intervalle de référence est de
5 000 ms, aligné sur la cadence du client officiel (`/status/powerflow`) ; un échantillon toutes les 5 s
représente ≈17 280 lignes par jour. L'installation est mono-machine : un collecteur Go, une base locale,
un frontend servi séparément ou par le même binaire. Aucune contrainte de service partagé, de
haute disponibilité ou d'accès concurrent multi-écrivain n'est identifiée.

Le socle technique impose un binaire statique sans CGO (`CGO_ENABLED=0`), et l'exploitation doit rester
simple : diagnostiquer avec `sqlite3`, sauvegarder par copie de fichier, restaurer sans outil dédié.

## Décision

Utiliser SQLite via le pilote pur Go `modernc.org/sqlite` (seule dépendance externe Go de la phase 1).
Configuration d'ouverture :

- une seule connexion en écriture : `db.SetMaxOpenConns(1)` ;
- mode WAL (`journal_mode(WAL)`) ;
- `busy_timeout` à 5 000 ms ;
- `synchronous=NORMAL` ;
- clés étrangères activées (`foreign_keys(1)`).

Le schéma et l'API du paquet de persistance sont décrits dans `internal/storage` (étape 7) ; les
migrations sont appliquées par `internal/storage/migrations/0001_init.sql`.

## Conséquences

- Aucun accès concurrent multi-écrivain : un seul écrivain à la fois, ce qui suffit pour un volume
  d'un échantillon toutes les 5 s ; à réévaluer si la latence d'écriture devient mesurable.
- Aucune réplication, aucun basculement automatique, aucune séparation lecture/écriture réseau.
- Les agrégats sont à matérialiser en phase 3 (index dédiés et tables d'agrégats heure/jour) plutôt que
  de compter sur un planificateur de requêtes ou des vues matérialisées inexistantes dans SQLite.
- Sauvegarde et restauration par copie cohérente du fichier (mode WAL) ; procédure documentée dans
  `docs/RUNBOOK.md`.
- Le fichier de base vit sous `./data/` par défaut (`FRONIUS_DB_PATH`) et est exclu du versionnement.

## Alternatives écartées

- **PostgreSQL** : serveur à administrer, sauvegarde et supervision supplémentaire ; disproportionné pour
  une installation domestique mono-machine.
- **DuckDB** : écosystème Go moins mûr et modèle d'écriture concurrente non adapté à un collecteur qui
  insère en continu.
- **Stockage de fichiers plats ou CSV** : pas de requêtes de plage, pas de transaction, pas d'index ;
  impose de réimplémenter ce que fournit SQLite.

## Sources

- `docs/DEVELOPMENT.md` (outillage et variables d'environnement)
- `docs/RUNBOOK.md` (sauvegarde, restauration, purge)
- `internal/storage/schema.go`, `internal/storage/migrations/0001_init.sql`
- `TODO.md` / plan phase 1, étape 7 (schéma SQLite, WAL, upsert idempotent)
