# Développement

## Prérequis

| Outil | Version | Remarque |
|---|---|---|
| Go | celle de `go.mod` (1.26) | imposée par `modernc.org/sqlite` |
| Node | 26 | |
| pnpm | 10 | |
| SQLite | 3.x (CLI) | uniquement pour inspecter la base à la main |

`CGO_ENABLED=0` doit rester possible : c'est la raison du choix du pilote
`modernc.org/sqlite` (pur Go). Aucune dépendance C.

## Démarrage

Trois terminaux, depuis la racine du dépôt :

```bash
make dev-sim     # simulateur Fronius sur 127.0.0.1:8081
make dev-api     # froniusd : API sur 127.0.0.1:8080 + collecteur
make dev-web     # interface Vite sur 127.0.0.1:5173
```

`make seed` remplit la base avec 7 jours d'historique du simulateur
(`froniusd --backfill-days 7`). Le simulateur doit tourner, et l'adresse doit
être locale : le remplissage d'historique refuse une cible non locale sans
`--force-backfill`.

Puis <http://127.0.0.1:5173>. Le proxy Vite relaie `/api` vers `froniusd` : le
frontend n'a jamais l'adresse de l'onduleur.

## Commandes

| Commande | Effet |
|---|---|
| `make build` | binaires statiques dans `bin/` + build du frontend |
| `make test` | `go test ./... -race` puis `vitest --run` |
| `make test-go` / `make test-web` | une seule moitié |
| `make lint` | `go vet ./...` puis `oxlint` |
| `make fmt` | `go fmt ./...` |
| `make seed` | 7 jours d'historique simulé |
| `make clean` | supprime `bin/`, `data/`, `coverage/`, `apps/web/dist` |

## Configuration

**Aucun fichier de configuration en phase 1** : uniquement des variables
d'environnement, toutes documentées dans `.env.example`. `config.Load()` refuse
au démarrage toute valeur illisible ou incohérente, et liste **toutes** les
fautes d'un coup.

Variables principales :

| Variable | Défaut | Rôle |
|---|---|---|
| `FRONIUS_SOLARAPI_URL` | `http://127.0.0.1:8081` | source des mesures |
| `FRONIUS_HTTP_ADDR` | `127.0.0.1:8080` | écoute de l'API REST |
| `FRONIUS_ALLOW_REMOTE` | `0` | lève la garde d'écoute non locale (ADR 0005) |
| `FRONIUS_DB_PATH` | `./data/fronius.db` | base SQLite |
| `FRONIUS_TZ` | `Europe/Brussels` | journées locales |
| `FRONIUS_POLL_INTERVAL` | `5s` | cadence de collecte (minimum 1 s) |
| `FRONIUS_POLL_JITTER` | `500ms` | amplitude du jitter |
| `FRONIUS_HTTP_TIMEOUT` | `5s` | délai par requête |
| `FRONIUS_RETENTION_DAYS` | `0` | `0` = conservation illimitée |
| `FRONIUS_STALE_AFTER` | `30s` | seuil de péremption affiché |
| `FRONIUS_WEB_DIR` | *(vide)* | assets statiques servis par `froniusd` |
| `FRONIUS_LOG_LEVEL` / `_FORMAT` | `info` / `text` | journalisation |

## Tests

```bash
go test ./... -race
pnpm --dir apps/web test --run
```

Organisation :

- `cmd/fronius-sim` : déterminisme (mêmes paramètres ⇒ même corps), modes de
  panne, `/sim/health` ;
- `internal/config` : défauts, surcharges, valeurs invalides, garde d'écoute ;
- `internal/fronius{,/solarapi}` : refus des méthodes non-GET ; cartographie des
  erreurs HTTP, JSON et délais ; fixtures de contrat ;
- `internal/model` : unités, qualité, capacités, invariant « pas de zéro » ;
- `internal/storage` : migrations idempotentes, upsert, buckets, changement
  d'heure (23 h / 25 h), purge ;
- `internal/collector` : jitter, backoff, anti-duplication des événements,
  annulation, absence d'insertion en cas d'échec ;
- `internal/api` : 404/405, validation des paramètres, états vides, capacités ;
- `apps/web` : formats, mapping de graphique (trous), client API (3 classes
  d'échec), fraîcheur, composants.

Les tests n'ouvrent aucune connexion vers un appareil : le simulateur et
`httptest` couvrent tout. Un test ne doit jamais appeler l'onduleur réel.

## Conventions

- Branche `main`, Conventional Commits (`feat(scope):`, `fix(scope):`,
  `docs:`, `test(scope):`, `chore:`), un commit par étape livrable.
- Identifiants de code en anglais ; commentaires, documentation et interface en
  français.
- `TODO.md` et `CHANGELOG.md` sont tenus à jour à chaque étape ; les décisions
  structurantes passent par un ADR dans `docs/adr/`.
- Une information nouvelle est documentée **avec sa source et sa version de
  firmware** (`docs/SOURCES.md`), et jamais présentée comme une permission
  firmware si elle vient du client.

## Pièges rencontrés (à ne pas réintroduire)

- **`connectNulls={false}` ne suffit pas.** Recharts ne coupe la courbe que s'il
  reçoit des points `null` : un bucket absent de la réponse rend ses voisins
  adjacents, et la courbe est tracée en travers du trou. D'où le complément de
  grille dans `chart.ts`, piloté par `meta.step_s`.
- **Le pas d'une série ne se déduit pas des données.** L'écart entre deux
  buckets renvoyés est faux dès qu'un bucket manque ; le serveur expose donc le
  pas réellement appliqué.
- **Un bucket n'est pas une mesure.** `count` et `missing` décrivent des
  **échantillons** ; `power_pv_avg_w` décrit la puissance moyenne du bucket.
- **Ne pas déduire le pas de `time.Time.AddDate`** pour les journées locales :
  une soustraction de 24 h traverse le changement d'heure. Utiliser
  `time.Date(…, loc)`.
