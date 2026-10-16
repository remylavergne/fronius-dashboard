package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"
)

// aggStateKey is the state key that carries the watermark: start of the
// first non-aggregated hour.
const aggStateKey = "agg_last_hour_utc"

// RollupResult describes an aggregation pass.
type RollupResult struct {
	From      time.Time // start of the first recomputed hour (inclusive)
	To        time.Time // start of the open hour (exclusive: it is still being fed)
	Buckets   int64     // rows written or updated in agg_hour
	Watermark time.Time // watermark after the pass (zero if none)
}

// Rollup updates the aggregates of the closed hours (phase 3, ADR 0006).
//
// The aggregated reads of the API used to be computed by walking `samples` on
// every request: correct, but proportional to the retained volume (≈6.3
// million rows for one year). This pass materialises the minimal reduction
// that makes it possible to answer without walking the raw measurements.
//
// Three invariants carry the correctness:
//
//  1. The watermark (aggStateKey) is always aligned on the UTC hour. Any hour
//     strictly earlier than it has a row in agg_hour if and only if it contains
//     measurements: an aggregated hour is never partial, and an hour without a
//     measurement has no row (no empty bucket is emitted, so no interpolation
//     is invented).
//  2. An hour is served from agg_hour only if it is whole and contained in the
//     requested range (see aggSplit): an aggregate covers the full hour while a
//     raw read only sees [from, to).
//  3. Any measurement write below the watermark lowers it (see
//     lowerWatermark): a measurement older than the watermark can never
//     stay non-aggregated behind it.
//
// The pass starts again from the watermark and stops at the start of the open
// hour: an hour still being fed is never aggregated, so a read never has to
// decide whether an aggregate is up to date. Without a watermark (fresh
// database), the pass starts again from the oldest retained sample.
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
			// No measurement retained: nothing to aggregate, and above all no
			// watermark to write, without which a history imported later
			// would be read as already aggregated.
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
		return RollupResult{}, fmt.Errorf("hourly aggregation: %w", err)
	}
	buckets, err := res.RowsAffected()
	if err != nil {
		return RollupResult{}, fmt.Errorf("counting aggregates: %w", err)
	}

	// The watermark advances only after the aggregates are written: if the
	// process is interrupted between the two, the next pass recomputes the
	// same range (the write is idempotent).
	if err := s.SetState(ctx, aggStateKey, formatTS(open)); err != nil {
		return RollupResult{}, err
	}
	return RollupResult{From: from, To: open, Buckets: buckets, Watermark: open}, nil
}

// aggWatermark returns the recorded watermark. The second return is
// false if it is absent or unreadable.
func (s *Store) aggWatermark(ctx context.Context) (time.Time, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM collector_state WHERE k = ?`, aggStateKey).Scan(&v)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("reading the watermark: %w", err)
	}
	t, err := parseTS(v)
	if err != nil {
		return time.Time{}, false, nil
	}
	// The watermark is always aligned on the hour (invariant 1): an
	// unexpected value is brought down to the lower whole hour, which
	// causes a recomputation of one hour at worst — never a partial
	// aggregate, which a read would believe complete.
	return t.UTC().Truncate(time.Hour), true, nil
}

// oldestSample returns the oldest retained sample.
func (s *Store) oldestSample(ctx context.Context) (time.Time, bool, error) {
	var v sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(ts_utc) FROM samples`).Scan(&v); err != nil {
		return time.Time{}, false, fmt.Errorf("searching for the oldest sample: %w", err)
	}
	if !v.Valid {
		return time.Time{}, false, nil
	}
	t, err := parseTS(v.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("unexpected timestamp %q: %w", v.String, err)
	}
	return t, true, nil
}

// lowerWatermark lowers the watermark if a sample older than it has just
// been written. A measurement below the watermark must always be covered by
// the next pass, otherwise it would remain forever absent from the aggregates
// while the read believes them complete.
func (s *Store) lowerWatermark(ctx context.Context, ts time.Time) error {
	floor := formatTS(ts.UTC().Truncate(time.Hour))
	if _, err := s.db.ExecContext(ctx,
		`UPDATE collector_state SET v = ? WHERE k = ? AND v > ?`, floor, aggStateKey, floor); err != nil {
		return fmt.Errorf("lowering the watermark: %w", err)
	}
	return nil
}

// aggSplit determines the hourly range served by the aggregates.
//
// An hour can come from an aggregate only if it is whole and contained in
// [from, to): it must therefore start on a whole hour (hence the upper bound
// ceilHour(from)) and end before min(to, watermark), itself truncated to the
// hour. The rest of the range — the partial header and the open hour — is read
// from the raw measurements.
//
// The third return is false when no whole hour is eligible: the read is then
// done entirely on the raw measurements.
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

// ceilHour returns the start of the first whole hour from t.
func ceilHour(t time.Time) time.Time {
	h := t.UTC().Truncate(time.Hour)
	if h.Before(t) {
		h = h.Add(time.Hour)
	}
	return h
}

// bucketAcc accumulates the reductions of one bucket.
//
// The average is not accumulated: it is recomposed from sumPV and countPV,
// which makes the result identical whether the rows come from the raw
// measurements or from the aggregates (average of averages weighted by the
// counts).
type bucketAcc struct {
	countAll int
	countPV  int
	sumPV    float64
	minPV    *float64
	maxPV    *float64
}

// add merges a partial reduction into the accumulator.
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

// bucket returns the corresponding aggregated point.
func (a *bucketAcc) bucket(start time.Time) Bucket {
	b := Bucket{Start: start, Count: a.countAll, Missing: a.countAll - a.countPV}
	if a.countPV > 0 {
		b.PowerPVAvgW = new(a.sumPV / float64(a.countPV))
	}
	b.PowerPVMinW = a.minPV
	b.PowerPVMaxW = a.maxPV
	return b
}

// rawBucketsInto accumulates the buckets computed on the raw measurements.
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
		return fmt.Errorf("time series: %w", err)
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
			return fmt.Errorf("reading a bucket: %w", err)
		}
		bucketAccFor(acc, key).add(countAll, countPV, sum.Float64,
			nullPtr(min), nullPtr(max))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating buckets: %w", err)
	}
	return nil
}

// aggBucketsInto accumulates the buckets computed on the hourly aggregates.
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
		return fmt.Errorf("aggregated time series: %w", err)
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
			return fmt.Errorf("reading an aggregated bucket: %w", err)
		}
		bucketAccFor(acc, key).add(countAll, countPV, sum.Float64,
			nullPtr(min), nullPtr(max))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating aggregated buckets: %w", err)
	}
	return nil
}

// rawHourlyEnergyInto accumulates the maximum daily energy per UTC hour
// from the raw measurements.
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
		return fmt.Errorf("daily energy: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hour string
		var v sql.NullFloat64
		if err := rows.Scan(&hour, &v); err != nil {
			return fmt.Errorf("reading an hourly maximum: %w", err)
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

// aggHourlyEnergyInto accumulates the maximum daily energy per UTC hour
// from the hourly aggregates.
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
		return fmt.Errorf("aggregated daily energy: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hour string
		var v sql.NullFloat64
		if err := rows.Scan(&hour, &v); err != nil {
			return fmt.Errorf("reading an aggregated hourly maximum: %w", err)
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

// bucketAccFor returns the accumulator of a bucket, creating it if needed.
func bucketAccFor(acc map[int64]*bucketAcc, key int64) *bucketAcc {
	a, ok := acc[key]
	if !ok {
		a = &bucketAcc{}
		acc[key] = a
	}
	return a
}

// bucketsFromAcc materialises the accumulators into points sorted by instant.
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

// nullPtr converts a nullable value into a pointer, nil if absent.
func nullPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return new(v.Float64)
}
