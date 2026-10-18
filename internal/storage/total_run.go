package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"fronius-supervisor/internal/model"
)

// Keys of the current total-energy run in collector_state.
//
// A run starts at the first stored total and restarts whenever the counter goes
// backwards: a device reset, a firmware reinitialisation, or another inverter
// writing to the same database. "Energy since monitoring" is measured from the
// run's start, because energy from a previous run cannot be recovered from the
// counter.
const (
	stateTotalRunStartUTC = "total_run_start_utc"
	stateTotalRunStartWh  = "total_run_start_wh"
)

// SinceBaseline returns the start of the current total-energy run: the first
// stored sample after the last observed decrease of the counter, or the first
// stored total when the counter never went backwards.
//
// The baseline is cached in collector_state, so the window scan that finds the
// last decrease runs once on an existing database; NoteTotalRun keeps it current
// afterwards.
func (s *Store) SinceBaseline(ctx context.Context) (float64, time.Time, bool, error) {
	wh, ts, ok, err := s.cachedTotalRunStart(ctx)
	if err != nil || ok {
		return wh, ts, ok, err
	}
	return s.computeTotalRunStart(ctx)
}

// NoteTotalRun maintains the run baseline after the collector inserted a
// sample. A decrease against the previous sample starts a new run.
//
// Only the newest sample can move the baseline: a backfill inserts older
// samples, and rewriting the run start from those would invent a history the
// collector never observed.
func (s *Store) NoteTotalRun(ctx context.Context, sm model.Sample) error {
	if sm.EnergyTotalWh == nil {
		return nil
	}

	var newest string
	err := s.db.QueryRowContext(ctx,
		`SELECT ts_utc FROM samples ORDER BY ts_utc DESC LIMIT 1`).Scan(&newest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the newest sample: %w", err)
	}
	if newest != formatTS(sm.TS) {
		return nil
	}

	base, _, hasBase, err := s.cachedTotalRunStart(ctx)
	if err != nil {
		return err
	}
	if !hasBase {
		// First observation, or an existing database upgrading: the scan sees
		// the sample just inserted and anchors the run there if it is a reset.
		_, _, _, err := s.computeTotalRunStart(ctx)
		return err
	}

	var prev float64
	err = s.db.QueryRowContext(ctx,
		`SELECT energy_total_wh FROM samples
		 WHERE ts_utc < ? AND energy_total_wh IS NOT NULL
		 ORDER BY ts_utc DESC LIMIT 1`, formatTS(sm.TS)).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("reading the previous total energy: %w", err)
	}

	belowBaseline := *sm.EnergyTotalWh < base
	belowPrevious := err == nil && *sm.EnergyTotalWh < prev
	if !belowBaseline && !belowPrevious {
		return nil
	}
	return s.setTotalRunStart(ctx, sm)
}

func (s *Store) cachedTotalRunStart(ctx context.Context) (float64, time.Time, bool, error) {
	state, err := s.State(ctx)
	if err != nil {
		return 0, time.Time{}, false, err
	}
	tsRaw, hasTS := state[stateTotalRunStartUTC]
	whRaw, hasWh := state[stateTotalRunStartWh]
	if !hasTS || !hasWh {
		return 0, time.Time{}, false, nil
	}
	wh, err := strconv.ParseFloat(whRaw, 64)
	if err != nil {
		return 0, time.Time{}, false, nil
	}
	ts, err := parseTS(tsRaw)
	if err != nil {
		return 0, time.Time{}, false, nil
	}
	return wh, ts, true, nil
}

// computeTotalRunStart finds the last decrease in the stored series and anchors
// the run on the first sample at or after it.
func (s *Store) computeTotalRunStart(ctx context.Context) (float64, time.Time, bool, error) {
	var resetTS string
	err := s.db.QueryRowContext(ctx, `
		SELECT ts_utc FROM (
			SELECT ts_utc, energy_total_wh,
			       lag(energy_total_wh) OVER (ORDER BY ts_utc, source) AS prev
			FROM samples WHERE energy_total_wh IS NOT NULL
		) WHERE prev IS NOT NULL AND energy_total_wh < prev
		ORDER BY ts_utc DESC LIMIT 1`).Scan(&resetTS)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, fmt.Errorf("finding the last total energy reset: %w", err)
	}

	q := `SELECT ts_utc, energy_total_wh FROM samples WHERE energy_total_wh IS NOT NULL`
	args := []any{}
	if resetTS != "" {
		q += ` AND ts_utc >= ?`
		args = append(args, resetTS)
	}
	q += ` ORDER BY ts_utc ASC LIMIT 1`

	var (
		tsRaw string
		wh    float64
	)
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&tsRaw, &wh); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, time.Time{}, false, nil
		}
		return 0, time.Time{}, false, fmt.Errorf("reading the first total of the run: %w", err)
	}
	ts, err := parseTS(tsRaw)
	if err != nil {
		return 0, time.Time{}, false, nil
	}
	if err := s.SetState(ctx, stateTotalRunStartUTC, tsRaw); err != nil {
		return 0, time.Time{}, false, err
	}
	if err := s.SetState(ctx, stateTotalRunStartWh, strconv.FormatFloat(wh, 'f', -1, 64)); err != nil {
		return 0, time.Time{}, false, err
	}
	return wh, ts, true, nil
}

func (s *Store) setTotalRunStart(ctx context.Context, sm model.Sample) error {
	if err := s.SetState(ctx, stateTotalRunStartUTC, formatTS(sm.TS)); err != nil {
		return err
	}
	return s.SetState(ctx, stateTotalRunStartWh,
		strconv.FormatFloat(*sm.EnergyTotalWh, 'f', -1, 64))
}
