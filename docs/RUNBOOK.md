# Runbook d'exploitation

Tout ce qui suit s'exécute **sans toucher à l'onduleur** : la phase 1 ne lit que
la Solar API, en `GET`.

## Démarrer

```bash
# 1. Simulateur (développement et vérification hors matériel)
go run ./cmd/fronius-sim                     # 127.0.0.1:8081

# 2. Service de supervision
FRONIUS_SOLARAPI_URL=http://127.0.0.1:8081 \
FRONIUS_DB_PATH=./data/fronius.db \
go run ./cmd/froniusd                        # API sur 127.0.0.1:8080
```

Binaire unique avec l'interface servie par le backend (après `make build`) :

```bash
FRONIUS_WEB_DIR=apps/web/dist ./bin/froniusd
# puis http://127.0.0.1:8080/ (repli SPA sur index.html pour les routes connues)
```

Historique de développement :

```bash
./bin/froniusd --backfill-days 7    # 7 jours depuis le simulateur local
```

Le remplissage refuse une cible non locale (protection : ne pas marteler
l'onduleur) ; `--force-backfill` lève ce refus en connaissance de cause.

## Vérifier que tout va bien

```bash
curl -s localhost:8080/api/v1/health | jq
```

| Champ | Lecture |
|---|---|
| `status` | `ok`, `degraded` (dernier cycle en échec), `error` (base injoignable) |
| `db.ok` | la base répond |
| `collector.last_success_utc` | dernier cycle réussi ; vide = jamais |
| `collector.last_error` | dernier message d'erreur (vide = tout va bien) |
| `collector.last_run_ms` | durée du dernier cycle |
| `collector.samples_total` | volume conservé |

```bash
curl -s localhost:8080/api/v1/live | jq '.data.power_pv_w, .meta.stale'
curl -s localhost:8080/api/v1/capabilities | jq -r '.data[]|select(.available==false)|.id+" — "+.reason'
```

`meta.stale = true` signifie : aucune mesure depuis plus que
`FRONIUS_STALE_AFTER`. Les valeurs affichées restent les dernières connues —
elles ne sont pas fausses, elles sont anciennes.

## Diagnostic par symptôme

| Symptôme | Cause probable | Action |
|---|---|---|
| `status: degraded`, `last_error` contient `transport` | onduleur ou simulateur injoignable | vérifier l'adresse, le réseau, que le simulateur tourne |
| `last_error` contient `HTTP 401`/`403` | la Solar API exige une authentification sur ce firmware | la phase 2 (adaptateur Digest) devient prioritaire ; ne pas tenter d'identifiants improvisés |
| `last_error` contient `HTTP 404` | le chemin diffère sur ce firmware | corriger `internal/fronius/solarapi/dto.go` après capture par la sonde |
| `last_error` contient `decode` | réponse de forme inattendue | capturer avec `cmd/fronius-probe`, comparer à `docs/SOURCES.md` |
| `last_error` contient `device_status` | l'onduleur signale une erreur applicative | consulter l'interface du fabricant ; aucune mesure n'est enregistrée dans ce cas |
| `meta.stale: true` alors que l'appareil répond | cadence ou seuil mal réglés | comparer `FRONIUS_POLL_INTERVAL` et `FRONIUS_STALE_AFTER` |
| interface : « API injoignable » | `froniusd` arrêté ou en erreur | démarrer/relancer le service |
| la courbe a des trous | périodes sans collecte (arrêt, panne, redémarrage) | comportement voulu : aucun point n'est inventé pour combler |
| `froniusd` refuse de démarrer : `refus: … n'est pas une adresse locale` | écoute non locale sans autorisation | lire `docs/adr/0005` puis, en connaissance de cause, `FRONIUS_ALLOW_REMOTE=1` |

Le backoff de collecte est exponentiel (1 s → 2 s → … → 60 s) : après une longue
panne, la reprise peut prendre jusqu'à une minute après le retour de l'appareil.
C'est normal, et cela évite de marteler un appareil qui redémarre.

## Sauvegarder et restaurer

La base est un fichier SQLite en mode WAL : il faut sauvegarder **avec** la
méthode SQLite, pas par simple copie à chaud des fichiers `.db-wal`.

```bash
# Sauvegarde cohérente à chaud
sqlite3 data/fronius.db ".backup '/chemin/sauvegarde/fronius-$(date -u +%Y%m%dT%H%M%SZ).db'"

# Restauration : arrêter froniusd d'abord
systemctl stop froniusd 2>/dev/null || pkill -f bin/froniusd
mv data/fronius.db data/fronius.db.avant-restauration
cp /chemin/sauvegarde/fronius-20261016T120000Z.db data/fronius.db
rm -f data/fronius.db-wal data/fronius.db-shm
FRONIUS_DB_PATH=./data/fronius.db ./bin/froniusd
```

Vérification après restauration :

```bash
sqlite3 data/fronius.db 'select count(*), min(ts_utc), max(ts_utc) from samples;'
curl -s localhost:8080/api/v1/live | jq '.data.power_pv_w, .meta.stale'
```

Les migrations sont versionnées (`schema_migrations`) et appliquées
automatiquement au démarrage : restaurer une base plus ancienne que le binaire
est sûr, dans le sens de la migration.

## Rétention et purge

`FRONIUS_RETENTION_DAYS=0` (défaut) conserve tout. Avec une valeur `N > 0`,
`froniusd` exécute **une** purge au démarrage : tout ce qui est antérieur au jour
local `N` jours en arrière est supprimé des mesures, des événements et des
cycles de collecte, puis un événement `retention_purge` est enregistré.

Aucune purge périodique n'est active en phase 1 (ADR 0006) : le redémarrage est
le seul déclencheur, ce qui rend l'opération prévisible. Avant d'activer une
rétention, sauvegarder (§ ci-dessus) et vérifier le volume :

```bash
sqlite3 data/fronius.db 'select count(*) from samples;'
du -h data/fronius.db
```

Ordre de grandeur : un échantillon toutes les 5 s ≈ 17 280 lignes par jour,
soit ≈ 6,3 millions par an.

## Journalisation

`log/slog` sur stderr. `FRONIUS_LOG_FORMAT=json` pour l'ingestion par un
collecteur de journaux, `FRONIUS_LOG_LEVEL=debug` pour le détail des requêtes
Solar API.

Aucun secret ne peut apparaître dans les journaux en phase 1 : le backend ne
détient aucun identifiant. La règle vaut pour la phase 2 : les identifiants de
l'API interne resteront dans l'environnement du backend et ne devront jamais
être journalisés.

## Arrêt propre

`SIGINT` / `SIGTERM` : arrêt du serveur HTTP, attente de la fin du cycle de
collecte en cours, fermeture de la base, sortie avec le code 0. Un cycle
interrompu ne laisse aucun échantillon partiel (l'insertion est unique).

## Maintenance de la base

```bash
sqlite3 data/fronius.db 'pragma integrity_check;'
sqlite3 data/fronius.db 'vacuum;'          # après une purge importante
sqlite3 data/fronius.db 'select ok, count(*) from collector_runs group by ok;'
sqlite3 data/fronius.db 'select ts_utc, kind, severity, message from events order by id desc limit 10;'
```

## Ce que ce service ne fait jamais

- aucune méthode autre que `GET` vers l'onduleur : le refus est structurel
  (`internal/fronius/readonly.go`), pas déclaratif ;
- aucune écriture de configuration sur l'appareil (phase 6, hors MVP) ;
- aucune exposition réseau hors de la boucle locale sans `FRONIUS_ALLOW_REMOTE=1`
  et lecture de l'ADR 0005 ;
- aucun calcul de consommation, d'import/export ou d'autoconsommation sans Smart
  Meter : ces capacités sont marquées indisponibles avec leur motif ;
- aucune donnée inventée pour combler un trou de collecte.
