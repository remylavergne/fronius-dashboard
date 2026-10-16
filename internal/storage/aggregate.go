package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"
)

// aggStateKey est la clé d'état qui porte la borne d'eau : début de la
// première heure non agrégée.
const aggStateKey = "agg_last_hour_utc"

// RollupResult décrit une passe d'agrégation.
type RollupResult struct {
	From      time.Time // début de la première heure recalculée (incluse)
	To        time.Time // début de l'heure ouverte (exclue : elle est encore alimentée)
	Buckets   int64     // lignes écrites ou mises à jour dans agg_hour
	Watermark time.Time // borne d'eau après la passe (zéro si aucune)
}

// Rollup met à jour les agrégats des heures closes (phase 3, ADR 0006).
//
// Les lectures agrégées de l'API étaient calculées en parcourant `samples` à
// chaque requête : correct, mais proportionnel au volume conservé (≈6,3
// millions de lignes pour un an). Cette passe matérialise la réduction
// minimale qui permet de répondre sans parcourir les mesures brutes.
//
// Trois invariants portent la correction :
//
//  1. La borne d'eau (aggStateKey) est toujours alignée sur l'heure UTC. Toute
//     heure qui lui est strictement antérieure possède une ligne dans agg_hour
//     si et seulement si elle contient des mesures : une heure agrégée n'est
//     jamais partielle, et une heure sans mesure n'a pas de ligne (aucun bucket
//     vide n'est émis, donc aucune interpolation n'est inventée).
//  2. Une heure n'est servie depuis agg_hour que si elle est entière et
//     contenue dans la plage demandée (voir aggSplit) : un agrégat couvre
//     l'heure complète alors qu'une lecture brute ne voit que [from, to).
//  3. Toute écriture de mesure sous la borne d'eau abaisse celle-ci (voir
//     lowerWatermark) : une mesure plus ancienne que la borne ne peut jamais
//     rester non agrégée derrière elle.
//
// La passe repart de la borne d'eau et s'arrête au début de l'heure ouverte :
// une heure encore alimentée n'est jamais agrégée, donc une lecture n'a jamais
// à décider si un agrégat est à jour. Sans borne d'eau (base neuve), la passe
// repart du plus ancien échantillon conservé.
func (s *Store) Rollup(ctx context.Context, now time.Time) (RollupResult, error) {
	open := now.UTC().Truncate(time.Hour)

	watermark, hasWM, err := s.aggWatermark(ctx)
	if err != nil {
		return RollupResult{}, err
	}

	from := watermark
	if !hasWM {
		oldest, ok, err := s.oldestSample(ctx)
		if err != nil {
			return RollupResult{}, err
		}
		if !ok {
			// Aucune mesure conservée : rien à agréger, et surtout aucune
			// borne d'eau à écrire, sans quoi un historique importé plus tard
			// serait lu comme déjà agrégé.
			return RollupResult{From: open, To: open}, nil
		}
		from = oldest.Truncate(time.Hour)
	}
	if !from.Before(open) {
		return RollupResult{From: open, To: open, Watermark: watermark}, nil
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO agg_hour (
			bucket_utc, count_all, count_pv, sum_pv_w, min_pv_w, max_pv_w, max_energy_day_wh)
		SELECT strftime('%Y-%m-%dT%H:00:00.000Z',
		               CAST(strftime('%s', ts_utc) AS INTEGER) / 3600 * 3600, 'unixepoch') AS bucket,
		       COUNT(*), COUNT(power_pv_w), COALESCE(SUM(power_pv_w), 0),
		       MIN(power_pv_w), MAX(power_pv_w), MAX(energy_day_wh)
		FROM samples
		WHERE ts_utc >= ? AND ts_utc < ?
		GROUP BY bucket
		ON CONFLICT(bucket_utc) DO UPDATE SET
			count_all = excluded.count_all,
			count_pv = excluded.count_pv,
			sum_pv_w = excluded.sum_pv_w,
			min_pv_w = excluded.min_pv_w,
			max_pv_w = excluded.max_pv_w,
			max_energy_day_wh = excluded.max_energy_day_wh`,
		formatTS(from), formatTS(open))
	if err != nil {
		return RollupResult{}, fmt.Errorf("agrégation horaire: %w", err)
	}
	buckets, err := res.RowsAffected()
	if err != nil {
		return RollupResult{}, fmt.Errorf("comptage des agrégats: %w", err)
	}

	// La borne d'eau n'avance qu'après l'écriture des agrégats : si le
	// processus s'interrompt entre les deux, la passe suivante recalcule la
	// même plage (l'écriture est idempotente).
	if err := s.SetState(ctx, aggStateKey, formatTS(open)); err != nil {
		return RollupResult{}, err
	}
	return RollupResult{From: from, To: open, Buckets: buckets, Watermark: open}, nil
}

// aggWatermark retourne la borne d'eau enregistrée. Le second retour vaut
// false si elle est absente ou illisible.
func (s *Store) aggWatermark(ctx context.Context) (time.Time, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM collector_state WHERE k = ?`, aggStateKey).Scan(&v)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("lecture de la borne d'eau: %w", err)
	}
	t, err := parseTS(v)
	if err != nil {
		return time.Time{}, false, nil
	}
	// La borne d'eau est toujours alignée sur l'heure (invariant 1) : une
	// valeur inattendue est ramenée à l'heure pleine inférieure, ce qui
	// provoque un recalcul d'une heure au pire — jamais un agrégat partiel,
	// qu'une lecture croirait complet.
	return t.UTC().Truncate(time.Hour), true, nil
}

// oldestSample retourne le plus ancien échantillon conservé.
func (s *Store) oldestSample(ctx context.Context) (time.Time, bool, error) {
	var v sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(ts_utc) FROM samples`).Scan(&v); err != nil {
		return time.Time{}, false, fmt.Errorf("recherche du plus ancien échantillon: %w", err)
	}
	if !v.Valid {
		return time.Time{}, false, nil
	}
	t, err := parseTS(v.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("horodatage inattendu %q: %w", v.String, err)
	}
	return t, true, nil
}

// lowerWatermark abaisse la borne d'eau si un échantillon plus ancien qu'elle
// vient d'être écrit. Une mesure sous la borne doit toujours être couverte par
// la passe suivante, sinon elle resterait éternellement absente des agrégats
// alors que la lecture les croit complets.
func (s *Store) lowerWatermark(ctx context.Context, ts time.Time) error {
	floor := formatTS(ts.UTC().Truncate(time.Hour))
	if _, err := s.db.ExecContext(ctx,
		`UPDATE collector_state SET v = ? WHERE k = ? AND v > ?`, floor, aggStateKey, floor); err != nil {
		return fmt.Errorf("abaissement de la borne d'eau: %w", err)
	}
	return nil
}

// aggSplit détermine la plage horaire servie par les agrégats.
//
// Une heure ne peut venir d'un agrégat que si elle est entière et contenue
// dans [from, to) : elle doit donc commencer à une heure pleine (d'où la borne
// haute ceilHour(from)) et se terminer avant min(to, borne d'eau), elle-même
// tronquée à l'heure. Le reste de la plage — l'entête partielle et l'heure
// ouverte — est lu dans les mesures brutes.
//
// Le troisième retour vaut false lorsque aucune heure pleine n'est éligible :
// la lecture se fait alors entièrement sur les mesures brutes.
func aggSplit(from, to, closed time.Time, enabled bool) (aggFrom, aggTo time.Time, ok bool) {
	if !enabled {
		return time.Time{}, time.Time{}, false
	}
	hi := to
	if closed.Before(hi) {
		hi = closed
	}
	start := ceilHour(from)
	end := hi.UTC().Truncate(time.Hour)
	if !start.Before(end) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// ceilHour retourne le début de la première heure pleine à partir de t.
func ceilHour(t time.Time) time.Time {
	h := t.UTC().Truncate(time.Hour)
	if h.Before(t) {
		h = h.Add(time.Hour)
	}
	return h
}

// bucketAcc accumule les réductions d'un bucket.
//
// La moyenne n'est pas accumulée : elle se recompose depuis sumPV et countPV,
// ce qui rend le résultat identique que les lignes viennent des mesures brutes
// ou des agrégats (moyenne des moyennes pondérée par les effectifs).
type bucketAcc struct {
	countAll int
	countPV  int
	sumPV    float64
	minPV    *float64
	maxPV    *float64
}

// add fusionne une réduction partielle dans l'accumulateur.
func (a *bucketAcc) add(countAll, countPV int, sum float64, min, max *float64) {
	a.countAll += countAll
	a.countPV += countPV
	a.sumPV += sum
	if min != nil && (a.minPV == nil || *min < *a.minPV) {
		a.minPV = new(*min)
	}
	if max != nil && (a.maxPV == nil || *max > *a.maxPV) {
		a.maxPV = new(*max)
	}
}

// bucket retourne le point agrégé correspondant.
func (a *bucketAcc) bucket(start time.Time) Bucket {
	b := Bucket{Start: start, Count: a.countAll, Missing: a.countAll - a.countPV}
	if a.countPV > 0 {
		b.PowerPVAvgW = new(a.sumPV / float64(a.countPV))
	}
	b.PowerPVMinW = a.minPV
	b.PowerPVMaxW = a.maxPV
	return b
}

// rawBucketsInto accumule les buckets calculés sur les mesures brutes.
func (s *Store) rawBucketsInto(ctx context.Context, acc map[int64]*bucketAcc, from, to time.Time, step int64) error {
	if !from.Before(to) {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%s', ts_utc) AS INTEGER) / ? * ? AS bucket,
		       COUNT(*), COUNT(power_pv_w), SUM(power_pv_w),
		       MIN(power_pv_w), MAX(power_pv_w)
		FROM samples
		WHERE ts_utc >= ? AND ts_utc < ?
		GROUP BY bucket
		ORDER BY bucket`, step, step, formatTS(from), formatTS(to))
	if err != nil {
		return fmt.Errorf("série temporelle: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key           int64
			countAll      int
			countPV       int
			sum, min, max sql.NullFloat64
		)
		if err := rows.Scan(&key, &countAll, &countPV, &sum, &min, &max); err != nil {
			return fmt.Errorf("lecture d'un bucket: %w", err)
		}
		bucketAccFor(acc, key).add(countAll, countPV, sum.Float64,
			nullPtr(min), nullPtr(max))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("parcours des buckets: %w", err)
	}
	return nil
}

// aggBucketsInto accumule les buckets calculés sur les agrégats horaires.
func (s *Store) aggBucketsInto(ctx context.Context, acc map[int64]*bucketAcc, from, to time.Time, step int64) error {
	if !from.Before(to) {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT CAST(strftime('%s', bucket_utc) AS INTEGER) / ? * ? AS bucket,
		       SUM(count_all), SUM(count_pv), SUM(sum_pv_w),
		       MIN(min_pv_w), MAX(max_pv_w)
		FROM agg_hour
		WHERE bucket_utc >= ? AND bucket_utc < ?
		GROUP BY bucket
		ORDER BY bucket`, step, step, formatTS(from), formatTS(to))
	if err != nil {
		return fmt.Errorf("série temporelle agrégée: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			key           int64
			countAll      int
			countPV       int
			sum, min, max sql.NullFloat64
		)
		if err := rows.Scan(&key, &countAll, &countPV, &sum, &min, &max); err != nil {
			return fmt.Errorf("lecture d'un bucket agrégé: %w", err)
		}
		bucketAccFor(acc, key).add(countAll, countPV, sum.Float64,
			nullPtr(min), nullPtr(max))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("parcours des buckets agrégés: %w", err)
	}
	return nil
}

// rawHourlyEnergyInto accumule le maximum d'énergie journalière par heure UTC
// à partir des mesures brutes.
func (s *Store) rawHourlyEnergyInto(ctx context.Context, acc map[string]float64, from, to time.Time) error {
	if !from.Before(to) {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT strftime('%Y-%m-%dT%H', ts_utc) AS hour_utc, MAX(energy_day_wh)
		FROM samples
		WHERE ts_utc >= ? AND ts_utc < ? AND energy_day_wh IS NOT NULL
		GROUP BY hour_utc
		ORDER BY hour_utc`, formatTS(from), formatTS(to))
	if err != nil {
		return fmt.Errorf("énergie journalière: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hour string
		var v sql.NullFloat64
		if err := rows.Scan(&hour, &v); err != nil {
			return fmt.Errorf("lecture d'un maximum horaire: %w", err)
		}
		if !v.Valid {
			continue
		}
		if cur, ok := acc[hour]; !ok || v.Float64 > cur {
			acc[hour] = v.Float64
		}
	}
	return rows.Err()
}

// aggHourlyEnergyInto accumule le maximum d'énergie journalière par heure UTC
// à partir des agrégats horaires.
func (s *Store) aggHourlyEnergyInto(ctx context.Context, acc map[string]float64, from, to time.Time) error {
	if !from.Before(to) {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT substr(bucket_utc, 1, 13) AS hour_utc, max_energy_day_wh
		FROM agg_hour
		WHERE bucket_utc >= ? AND bucket_utc < ? AND max_energy_day_wh IS NOT NULL
		ORDER BY bucket_utc`, formatTS(from), formatTS(to))
	if err != nil {
		return fmt.Errorf("énergie journalière agrégée: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hour string
		var v sql.NullFloat64
		if err := rows.Scan(&hour, &v); err != nil {
			return fmt.Errorf("lecture d'un maximum horaire agrégé: %w", err)
		}
		if !v.Valid {
			continue
		}
		if cur, ok := acc[hour]; !ok || v.Float64 > cur {
			acc[hour] = v.Float64
		}
	}
	return rows.Err()
}

// bucketAccFor retourne l'accumulateur d'un bucket, en le créant au besoin.
func bucketAccFor(acc map[int64]*bucketAcc, key int64) *bucketAcc {
	a, ok := acc[key]
	if !ok {
		a = &bucketAcc{}
		acc[key] = a
	}
	return a
}

// bucketsFromAcc matérialise les accumulateurs en points triés par instant.
func bucketsFromAcc(acc map[int64]*bucketAcc) []Bucket {
	keys := make([]int64, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	out := make([]Bucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, acc[k].bucket(time.Unix(k, 0).UTC()))
	}
	return out
}

// nullPtr convertit une valeur nullable en pointeur, nil si absente.
func nullPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return new(v.Float64)
}
