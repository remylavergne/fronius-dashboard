# Runbook d'exploitation

Tout ce qui suit s'exécute **sans toucher à l'onduleur** : l'application ne lit
que des chemins de l'appareil, en `GET`, et les identifiants ne servent qu'à
répondre à un `401`.

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
| `last_error` contient `HTTP 401`/`403` | l'appareil exige une authentification sur le chemin Solar API de ce firmware | poser les identifiants (§ « Identifiants de l'appareil ») et relancer ; ne pas tenter d'identifiants improvisés |
| `detection.meter.state: "unknown"` et motif « identifiants non configurés » | `FRONIUS_DEVICE_USER` / `FRONIUS_DEVICE_PASSWORD` absents : l'appareil exige une authentification sur ce chemin | poser les deux variables dans l'environnement de `froniusd`, puis redémarrer le service |
| `device.identity.available: false` avec motif « accès refusé (401) » | identifiants présents mais refusés : utilisateur, mot de passe ou realm différent de celui attendu | vérifier l'utilisateur (rôle « customer » ou « technician » côté interface fabricant) ; comparer le realm annoncé par `fronius-probe` |
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

## Identifiants de l'appareil

Certains chemins de l'appareil n'existent que derrière une authentification
Digest : identité (`/status/version`), événements (`/status/activeEvents`) et
présence des composants (`/components/…`). C'est `froniusd` qui les interroge,
périodiquement, et l'en-tête `Authorization` n'est émis **qu'en réponse à un
`401`** — jamais préemptivement, jamais vers un autre hôte, jamais vers le
frontend.

Où les poser : dans l'environnement du **service**, pas dans un fichier
versionné, pas dans l'interface.

```bash
export FRONIUS_DEVICE_USER=installateur
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
FRONIUS_DETECT_INTERVAL=60s ./bin/froniusd
```

Les deux variables vont ensemble : n'en définir qu'une fait échouer le démarrage,
avec un message qui nomme les deux (et jamais la valeur). Pour un lancement
interactif, `read -rs` évite d'inscrire le mot de passe dans l'historique du
shell :

```bash
read -rs FRONIUS_DEVICE_PASSWORD && export FRONIUS_DEVICE_PASSWORD
read -r FRONIUS_DEVICE_USER && export FRONIUS_DEVICE_USER
./bin/froniusd
```

Vérifier que la lecture aboutit :

```bash
curl -s localhost:8080/api/v1/system | jq '.data.device, .data.detection'
curl -s localhost:8080/api/v1/capabilities | jq -r '.data[]|select(.id|startswith("device"))|.id+" — "+.available|tostring' 2>/dev/null
```

| Observation | Lecture |
|---|---|
| `detection.meter.state: "present"` | l'appareil a répondu, un Smart Meter est installé : `grid.import_export`, `load.consumption` et `autoconsumption.ratio` deviennent disponibles |
| `detection.meter.state: "absent"` | l'appareil a répondu, pas de Smart Meter : la consommation reste indisponible, avec son motif |
| `detection.meter.state: "unknown"` + motif « identifiants non configurés » | aucun identifiant posé : le service cesse d'interroger ce chemin jusqu'au prochain redémarrage (pas de `401` inutile toutes les minutes) |
| `device.identity.available: false` | l'identité n'a pas été lue : voir `detection.internal_api.state` et `last_error` |
| `battery.soc` indisponible même batterie détectée | normal : la détection de présence ne prouve pas qu'une mesure d'état de charge existe côté API (phase ultérieure) |

Sur `401` : le service réessaie **une** fois par chemin après le défi. Si le
`401` persiste, le message de détection est « accès refusé (401) : identifiants
absents ou invalides » — vérifier alors l'utilisateur, le mot de passe, et le
realm annoncé par l'appareil (`fronius-probe --endpoint /status/version`).
Ne jamais « corriger » cela en élargissant l'application : aucune commande de
modification de l'appareil n'existe dans ce binaire.

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
(Solar API et chemins internes : chemin, statut, octets, durée ; jamais d'en-tête).

Les identifiants de l'appareil ne peuvent pas apparaître dans les journaux : le
type `secret.String` (`internal/secret`) masque la valeur pour toute mise en
forme — `slog`, `fmt` et `encoding/json` compris — et seul le calcul de `HA1` du
Digest appelle `Reveal()`. Le démarrage du détecteur trace un booléen
(`credentials=true|false`), jamais le nom d'utilisateur ni le mot de passe.

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
- aucun en-tête `Authorization` émis préemptivement : il n'est construit qu'en
  réponse à un `401`, seulement vers l'hôte de `FRONIUS_SOLARAPI_URL`, et jamais
  vers le frontend ;
- aucune écriture de configuration sur l'appareil (phase 6, hors MVP) ;
- aucune exposition réseau hors de la boucle locale sans `FRONIUS_ALLOW_REMOTE=1`
  et lecture de l'ADR 0005 ;
- aucun calcul de consommation, d'import/export ou d'autoconsommation sans Smart
  Meter : ces capacités sont marquées indisponibles avec leur motif ;
- aucune donnée inventée pour combler un trou de collecte, et aucune absence
  inventée : une lecture qui échoue donne « non déterminé », jamais « absent ».
