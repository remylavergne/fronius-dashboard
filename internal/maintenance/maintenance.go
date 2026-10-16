// Package maintenance runs the background database jobs: hourly
// aggregation and retention purge (ADR 0006).
//
// These jobs do not depend on any fresh measurement, hence their scheduling here
// rather than in the collector: the aggregation starts again from the recorded
// watermark and therefore catches up on a service interruption on its own.
//
// Both jobs are logged but cannot stop the loop: a failure of one cycle
// (database busy, disk full) must not deprive the supervision of its maintenance
// until the next restart.
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// DefaultTick is the cadence of the background jobs. It also bounds the
// precision of the purge time: a retention shifted by a few minutes
// has no consequence.
const DefaultTick = 5 * time.Minute

// purgeHour is the local hour from which the daily purge takes place.
const purgeHour = 3

// Store is the subset of the store used by the maintenance.
// *storage.Store satisfies it without an adapter.
type Store interface {
	Rollup(ctx context.Context, now time.Time) (storage.RollupResult, error)
	PurgeBefore(ctx context.Context, t time.Time) (int64, error)
	InsertEvent(ctx context.Context, e model.Event) error
}

// Runner schedules the hourly aggregation and the retention purge.
type Runner struct {
	Store Store
	Loc   *time.Location
	// RetentionDays is 0 for unlimited retention: no purge, neither
	// at startup nor afterwards, is then executed.
	RetentionDays int
	// Tick bounds the cadence of the jobs (DefaultTick if zero).
	Tick time.Duration
	Log  *slog.Logger
	// Now is injectable (tests); time.Now by default.
	Now func() time.Time

	// lastPurgeDate remembers the last purged local day. The state is
	// deliberately in memory: a restart triggers a purge at
	// startup, which is the expected behaviour (and sufficient).
	lastPurgeDate string
}

// Run aggregates the closed hours every Tick and purges once per local day,
// until the context is cancelled. Returns nil on stop.
func (r *Runner) Run(ctx context.Context) error {
	log := r.log()
	log.Info("maintenance started", "tick", r.tick(), "retention_days", r.RetentionDays)

	r.RollupOnce(ctx)
	if r.RetentionDays > 0 {
		// A purge at startup: this is the phase-1 behaviour, and
		// it catches up on the downtime without waiting for the next day.
		r.PurgeOnce(ctx)
	} else {
		log.Info("unlimited retention: scheduled purge disabled")
	}

	tick := time.NewTicker(r.tick())
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("maintenance stopped")
			return nil
		case <-tick.C:
			r.RollupOnce(ctx)
			r.maybePurge(ctx)
		}
	}
}

// RollupOnce aggregates the closed hours up to now.
func (r *Runner) RollupOnce(ctx context.Context) storage.RollupResult {
	res, err := r.Store.Rollup(ctx, r.now())
	if err != nil {
		r.log().Error("hourly aggregation failed", "error", err)
		return storage.RollupResult{}
	}
	switch {
	case res.To.Sub(res.From) > time.Hour:
		// Catch-up after an interruption: a routine pass covers
		// only one hour, it deserves the debug level only.
		r.log().Info("hourly aggregation caught up",
			"from", res.From, "to", res.To, "buckets", res.Buckets)
	case res.Buckets > 0:
		r.log().Debug("hourly aggregation", "buckets", res.Buckets)
	}
	return res
}

// maybePurge triggers the purge once per local day, from
// purgeHour.
func (r *Runner) maybePurge(ctx context.Context) {
	if r.RetentionDays <= 0 {
		return
	}
	now := r.now().In(r.loc())
	if now.Hour() < purgeHour || now.Format("2006-01-02") == r.lastPurgeDate {
		return
	}
	r.PurgeOnce(ctx)
}

// PurgeOnce applies the retention and logs the result.
func (r *Runner) PurgeOnce(ctx context.Context) {
	now := r.now()
	// The day is marked as soon as the attempt is made: a retention that fails
	// is reported, then retried the next day rather than on every tick.
	r.lastPurgeDate = now.In(r.loc()).Format("2006-01-02")

	limit := retentionLimit(now, r.loc(), r.RetentionDays)
	deleted, err := r.Store.PurgeBefore(ctx, limit)
	if err != nil {
		r.log().Error("retention purge failed", "error", err)
		return
	}
	r.log().Info("retention purge completed",
		"before", limit, "samples_deleted", deleted, "retention_days", r.RetentionDays)

	if err := r.Store.InsertEvent(ctx, model.Event{
		TS:       now.UTC(),
		Kind:     model.EventPurge,
		Severity: model.SeverityInfo,
		Message:  "retention purge executed",
		Detail: fmt.Sprintf("%d samples deleted before %s",
			deleted, limit.Format(time.RFC3339)),
	}); err != nil {
		r.log().Warn("purge event not recorded", "error", err)
	}
}

// retentionLimit returns the purge bound: local midnight of the day `days`
// days ago, rounded up to the NEXT whole hour.
//
// The rounding is structural: the aggregates and the measurements must share a
// boundary aligned on the hour. A finer boundary would leave the hour
// straddling with its aggregate deleted but part of its measurements kept —
// those measurements would become invisible to the aggregated reads. For a
// timezone offset by a whole number of hours, including Europe/Brussels, the
// rounding is neutral.
func retentionLimit(now time.Time, loc *time.Location, days int) time.Time {
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day()-days, 0, 0, 0, 0, loc)
	if floor := midnight.Truncate(time.Hour); floor.Before(midnight) {
		return floor.Add(time.Hour)
	}
	return midnight
}

func (r *Runner) log() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

func (r *Runner) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

func (r *Runner) loc() *time.Location {
	if r.Loc == nil {
		return time.Local
	}
	return r.Loc
}

func (r *Runner) tick() time.Duration {
	if r.Tick <= 0 {
		return DefaultTick
	}
	return r.Tick
}
