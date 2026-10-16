# ADR 0006 — Rétention et agrégation

## Statut

Accepté (phase 1) — **révisé en phase 3** (voir « Révision phase 3 » en fin de document)

## Contexte

Les mesures brutes s'accumulent au rythme d'un échantillon toutes les 5 s, soit ≈17 280 lignes par jour
et ≈6,3 millions de lignes par an. L'historisation doit alimenter des graphiques sur 24 h, 7 j, 30 j ou
une plage personnalisée, ainsi qu'une énergie journalière.

Deux contraintes structurantes : l'absence de donnée doit rester visible (jamais d'interpolation
inventée), et le calcul de l'énergie journalière doit résister aux transitions d'heure locale
(Europe/Brussels).

## Décision

- Les mesures brutes sont conservées telles quelles dans la table `samples`, clé primaire
  `(ts_utc, source)` (upsert idempotent).
- La rétention est paramétrée par `FRONIUS_RETENTION_DAYS`, avec `0` = conservation illimitée (défaut).
- L'agrégation est faite à la lecture en phase 1 :
  - séries temporelles par buckets alignés sur l'epoch (`1m`, `5m`, `15m`, `1h`, `1d`, `auto` borné à
    1500 buckets) ; un bucket vide n'est pas émis ;
  - énergie journalière par `MAX(E_Day)` par jour local (calendrier Europe/Brussels, donc 23 h ou 25 h
    les jours de changement d'heure). Les maxima horaires étant invariants d'offset, le calcul par heure
    UTC puis regroupement par date locale reste correct lors des transitions.
- La purge (`PurgeBefore`) est fournie avec la base, mais aucune purge programmée n'est activée par
  défaut en phase 1.

## Conséquences

- **Phase 3 (2026-10-16)** : les agrégats horaires sont matérialisés (`agg_hour`) et la purge est
  planifiée. Critère de sortie vérifié : sur 6 307 200 mesures (un an à 5 s), la série au pas 1 h
  répond en 14 ms contre 5 168 ms par le chemin brut, et l'énergie journalière en 5 ms contre
  5 535 ms — au-delà des 200 ms visés. Détail et mesures dans « Révision phase 3 ».
- La rétention à `0` évite toute perte de données par défaut.
- L'API expose par bucket `PowerPVMinW`, `PowerPVMaxW`, `Count` et `Missing` ; côté graphique,
  `connectNulls={false}` préserve les trous.
- L'implémentation SQLite ne dispose pas de vues matérialisées : les agrégats prennent la forme
  d'une table dédiée (`agg_hour`), d'un index primaire clusterisé et d'une borne d'eau (voir
  « Révision phase 3 »).

## Alternatives écartées

- **Agrégats matérialisés dès la phase 1** : complexité de migration et de rattrapage avant d'avoir des
  données réelles.
- **Purge activée par défaut** : risque de destruction de la seule donnée disponible avant validation.
- **Interpolation des buckets vides** : inventerait une production non mesurée.
- **Stockage en série continue compressée** : illisible par `sqlite3`, contraire à l'exigence de
  diagnostic simple.

## Révision phase 3 (2026-10-16)

### Ce qui a été mis en place

Une seule table d'agrégats, horaire : `agg_hour` (migration `0003_agg.sql`).

| Colonne | Rôle |
|---|---|
| `bucket_utc` | début d'heure UTC, clé primaire `WITHOUT ROWID` (accès séquentiel par plage) |
| `count_all`, `count_pv` | effectif du bucket et nombre de valeurs de puissance non nulles |
| `sum_pv_w`, `min_pv_w`, `max_pv_w` | somme des puissances non nulles, minimum, maximum |
| `max_energy_day_wh` | maximum de `E_Day` dans l'heure |

**Pourquoi ces colonnes et pas une moyenne.** Une moyenne ne se recompose pas : la moyenne d'une
union n'est pas la moyenne des moyennes. En conservant `sum_pv_w` et `count_pv`, la moyenne d'un
pas multiple d'une heure se recalcule exactement comme une moyenne pondérée par les effectifs, et
le minimum comme le maximum sont associatifs. `missing` se déduit de `count_all - count_pv`, il
n'est donc pas stocké.

**Pourquoi pas de table journalière.** L'ADR prévoyait des agrégats « heure/jour ». Le regroupement
par date locale se fait en Go, à partir des lignes horaires (8 760 lignes pour un an, mesuré à
5 ms) : il n'y a donc rien à gagner à entretenir une seconde table qu'il faudrait alimenter,
purger et tester en parallèle. L'écart est assumé ; il reste réversible sans changement de schéma
de lecture.

### Bornes et découpage

- **Borne d'eau** : `collector_state.agg_last_hour_utc`, toujours alignée sur l'heure UTC. Toute
  heure qui lui est antérieure a une ligne dans `agg_hour` si et seulement si elle contient des
  mesures. Une valeur illisible est traitée comme absente (recalcul complet plutôt que lecture
  fausse) et ramenée à l'heure pleine inférieure.
- **L'heure ouverte n'est jamais agrégée** : une heure encore alimentée ne peut pas être
  matérialisée sans risquer qu'une lecture la croie complète. Une lecture de longue plage parcourt
  donc toujours, au plus, l'heure en cours dans `samples` (≈720 lignes, 1 ms de plus).
- **Une heure n'est servie depuis les agrégats que si elle est entière et contenue dans
  `[from, to)`.** C'est la règle la plus facile à casser : un agrégat couvre l'heure complète,
  alors qu'une lecture brute ne voit que la tranche demandée. Servir une heure partielle depuis un
  agrégat complet inventerait des mesures (moyenne, minimum, maximum et effectif portant sur plus
  de lignes que demandé). D'où les trois segments d'une lecture : entête partielle sur `samples`,
  heures pleines closes sur `agg_hour`, queue ouverte sur `samples`.
- **Pas multi-heure** : seuls les pas multiples d'une heure (1 h, 1 j) empruntent les agrégats. Les
  pas 1 m, 5 m et 15 m parcourent les mesures brutes, inchangés depuis la phase 1.

### Invalidation à l'écriture

Une mesure écrite sous la borne d'eau l'abaisse (`lowerWatermark`), donc la passe suivante la
reprend. Sans cela, un historique importé après coup (backfill lancé après une première passe) ou
une horloge d'appareil en retard resterait éternellement absent des agrégats, alors que la lecture
les croirait complets. La comparaison `MIN(ts_utc) < borne` aurait été trop grossière : elle ne
distingue pas « déjà agrégé » de « importé après coup » et aurait recalculé tout l'historique à
chaque passe.

### Purge

- La planification passe dans `internal/maintenance` : une purge au démarrage (comportement
  conservé de la phase 1) puis une fois par journée locale à partir de 03:00, sans nouvelle
  variable d'environnement. La rétention reste `FRONIUS_RETENTION_DAYS`, `0` = conservation
  illimitée, auquel cas aucune purge n'est planifiée.
- La borne de purge est **minuit local arrondi à l'heure pleine supérieure**. L'arrondi est
  structurel : sans lui, l'heure à cheval sur la borne perdrait son agrégat tout en conservant une
  partie de ses mesures, et ces mesures deviendraient invisibles des lectures agrégées. Pour un
  fuseau décalé d'un nombre entier d'heures, dont Europe/Brussels, l'arrondi est neutre.
- La purge supprime les agrégats de la même plage que les mesures et remonte la borne d'eau à la
  borne de purge : les agrégats ne sont jamais une seconde source de vérité, ils restent
  exactement dérivables des mesures conservées.

### Mesures (Apple M-series, macOS 27, `make test-perf`)

| Requête | Avant (mesures brutes) | Après (agrégats) |
|---|---|---|
| `/timeseries` pas 1 h, un an (8 760 buckets) | 5 168 ms | **14 ms** |
| `/timeseries` pas 1 h, un an, heure ouverte incluse | — | 15 ms |
| `/timeseries` pas 1 j, un an (365 buckets) | — | 7 ms |
| `/energy/daily`, un an (366 journées) | 5 535 ms | **5 ms** |
| Agrégation du premier an complet (une fois) | — | 8 736 ms |

Les 6 307 200 mesures sont insérées en 22,7 s par le générateur de test. Le nombre de buckets et
les journées sont identiques dans les deux chemins : l'égalité est vérifiée par comparaison
littérale des réductions (`TestTimeSeriesIdenticalWithAndWithoutAggregates`,
`TestDailyEnergyIdenticalWithAndWithoutAggregates`) et, au niveau de l'API, par comparaison octet à
octet de la charge utile avant et après agrégation.

## Sources

- `internal/storage/aggregate.go` (agrégats, borne d'eau, découpage), `internal/storage/store.go`
- `internal/storage/aggregate_test.go` (équivalence brut/agrégat, rattrapage, purge)
- `internal/storage/perf_test.go` (mesures ci-dessus, `make test-perf`)
- `internal/maintenance` (planification de la purge et de l'agrégation)
- `internal/storage/storage_test.go` (bornes de buckets, jours à 23 h / 25 h)
- `internal/api` (`/timeseries`, `/energy/daily`, paramètre `step`)
- `docs/RUNBOOK.md` (purge, sauvegarde)
- Plan phase 1, étapes 7 et 9 ; plan phase 3
