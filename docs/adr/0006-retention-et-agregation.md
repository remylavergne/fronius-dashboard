# ADR 0006 — Rétention et agrégation

## Statut

Accepté (phase 1)

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

- Les agrégats matérialisés heure/jour et une purge planifiée sont reportés en phase 3, avec pour
  critère de sortie une durée de réponse inférieure à 200 ms sur un an de données.
- La rétention à `0` évite toute perte de données par défaut.
- L'API expose par bucket `PowerPVMinW`, `PowerPVMaxW`, `Count` et `Missing` ; côté graphique,
  `connectNulls={false}` préserve les trous.
- L'implémentation SQLite ne dispose pas de vues matérialisées : les agrégats de la phase 3 prendront la
  forme de tables d'agrégats et d'index dédiés (voir ADR 0001).

## Alternatives écartées

- **Agrégats matérialisés dès la phase 1** : complexité de migration et de rattrapage avant d'avoir des
  données réelles.
- **Purge activée par défaut** : risque de destruction de la seule donnée disponible avant validation.
- **Interpolation des buckets vides** : inventerait une production non mesurée.
- **Stockage en série continue compressée** : illisible par `sqlite3`, contraire à l'exigence de
  diagnostic simple.

## Sources

- `internal/storage/storage_test.go` (bornes de buckets, jours à 23 h / 25 h)
- `internal/api` (`/timeseries`, `/energy/daily`, paramètre `step`)
- `docs/RUNBOOK.md` (purge, sauvegarde)
- Plan phase 1, étapes 7 et 9 ; roadmap phase 3
