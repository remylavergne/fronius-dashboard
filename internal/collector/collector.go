// Package collector performs the periodic acquisition of measurements.
//
// Invariant: the only remote operation is a GET. No retry, no
// backoff and no recovery can trigger a write on the inverter,
// since the transport itself refuses any method other than GET
// (internal/fronius/readonly.go).
package collector

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// Persisted state keys.
const (
	StateLastSuccess = "last_success_utc"
	StateLastError   = "last_error"
)

// Backoff bounds applied after a failure.
const (
	initialBackoff = time.Second
	maxBackoff     = time.Minute
)

// Source provides the raw measurement. The interface is deliberately reduced to
// the single endpoint of phase 1.
type Source interface {
	GetPowerFlowRealtimeData(ctx context.Context) (solarapi.PowerFlowEnvelope, error)
}

// Sink persists the effects of a collection cycle.
//
// InsertEvent does not appear in the minimal definition of the plan but it is
// necessary for the requested behaviour (warning-severity event on failure,
// with anti-duplication, and recovery event).
type Sink interface {
	InsertSample(context.Context, model.Sample) error
	InsertEvent(context.Context, model.Event) error
	InsertRun(context.Context, storage.RunRecord) error
	SetState(context.Context, string, string) error
}

// Collector loops over the source and persists the results.
type Collector struct {
	Source Source
	Sink   Sink
	Log    *slog.Logger
	// Interval is the nominal period on success.
	Interval time.Duration
	// Jitter is the amplitude of the random variation applied to Interval.
	Jitter time.Duration
	// Now allows injecting a clock (tests).
	Now func() time.Time
	// Rand allows injecting a reproducible random source (tests).
	Rand *rand.Rand

	mu sync.Mutex
	// lastEventDetail remembers the last error message that gave rise to
	// an event, so as not to duplicate an event on every cycle.
	lastEventDetail string
	// failing indicates that a series of failures is in progress.
	failing bool
}

// Run collects until the context is cancelled. Returns nil when the
// context is cancelled: stopping is a nominal case, not an error.
func (c *Collector) Run(ctx context.Context) error {
	c.log().Info("collector started",
		"interval", c.Interval, "jitter", c.Jitter)

	attempt := 0
	for {
		_, err := c.CollectOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}

		var delay time.Duration
		if err != nil {
			attempt++
			delay = c.backoff(attempt)
		} else {
			attempt = 0
			delay = c.successDelay()
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// CollectOnce runs a complete cycle and returns the persisted sample.
//
// On error, no sample is inserted: a failed cycle must
// never produce a measurement point, on pain of inventing data.
func (c *Collector) CollectOnce(ctx context.Context) (model.Sample, error) {
	started := c.now()

	env, err := c.Source.GetPowerFlowRealtimeData(ctx)
	duration := c.now().Sub(started)

	if err != nil {
		c.recordFailure(ctx, started, duration, err)
		return model.Sample{}, err
	}

	sm := model.FromPowerFlow(env, started)
	if err := c.Sink.InsertSample(ctx, sm); err != nil {
		// The persistence failure is not a device failure: no
		// hardware-failure event, and the error is returned as is.
		return model.Sample{}, err
	}

	if err := c.Sink.SetState(ctx, StateLastSuccess, started.UTC().Format(time.RFC3339)); err != nil {
		return sm, err
	}
	if err := c.Sink.SetState(ctx, StateLastError, ""); err != nil {
		return sm, err
	}
	if err := c.Sink.InsertRun(ctx, storage.RunRecord{
		StartedUTC: started, DurationMS: duration.Milliseconds(), OK: true, HTTPStatus: 200,
	}); err != nil {
		return sm, err
	}

	c.onSuccess(ctx, started)
	return sm, nil
}

func (c *Collector) recordFailure(ctx context.Context, started time.Time, duration time.Duration, err error) {
	kind, _ := froniuserr.KindOf(err)

	httpStatus := 0
	var typed *froniuserr.Error
	if errors.As(err, &typed) {
		httpStatus = typed.HTTPStatus
	}

	logErr := c.Sink.InsertRun(ctx, storage.RunRecord{
		StartedUTC: started,
		DurationMS: duration.Milliseconds(),
		OK:         false,
		HTTPStatus: httpStatus,
		ErrorKind:  string(kind),
		Error:      err.Error(),
	})
	if logErr != nil {
		c.log().Error("failed to record the failed cycle", "error", logErr)
	}
	if setErr := c.Sink.SetState(ctx, StateLastError, err.Error()); setErr != nil {
		c.log().Error("failed to write last_error", "error", setErr)
	}

	detail := err.Error()
	c.mu.Lock()
	duplicate := detail == c.lastEventDetail
	c.lastEventDetail = detail
	c.failing = true
	c.mu.Unlock()

	if duplicate {
		return
	}
	if insErr := c.Sink.InsertEvent(ctx, model.Event{
		TS:       started.UTC(),
		Kind:     model.EventCollectorError,
		Severity: model.SeverityWarning,
		Message:  "collection failure",
		Detail:   detail,
	}); insErr != nil {
		c.log().Error("failed to insert the failure event", "error", insErr)
	}

	c.log().Warn("collection failed", "kind", kind, "error", err)
}

// onSuccess publishes the recovery event if a series of failures has just
// ended.
func (c *Collector) onSuccess(ctx context.Context, started time.Time) {
	c.mu.Lock()
	wasFailing := c.failing
	c.failing = false
	c.lastEventDetail = ""
	c.mu.Unlock()

	if !wasFailing {
		return
	}
	if err := c.Sink.InsertEvent(ctx, model.Event{
		TS:       started.UTC(),
		Kind:     model.EventCollectorRecovered,
		Severity: model.SeverityInfo,
		Message:  "collection recovered",
	}); err != nil {
		c.log().Error("failed to insert the recovery event", "error", err)
	}
	c.log().Info("collection recovered")
}

// successDelay returns Interval ± Jitter.
func (c *Collector) successDelay() time.Duration {
	if c.Jitter <= 0 {
		return c.Interval
	}
	// Int64N(2*Jitter+1) ∈ [0, 2·Jitter]: the offset stays within bounds.
	offset := time.Duration(c.int64n(int64(2*c.Jitter) + 1))
	d := c.Interval - c.Jitter + offset
	if d < 0 {
		return 0
	}
	return d
}

// backoff returns the delay after attempt consecutive failures:
// 1 s, 2 s, 4 s, … capped at 60 s.
func (c *Collector) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 30 {
		return maxBackoff
	}
	d := initialBackoff << shift
	if d <= 0 || d > maxBackoff {
		return maxBackoff
	}
	return d
}

func (c *Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Collector) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}

func (c *Collector) int64n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if c.Rand != nil {
		return c.Rand.Int64N(n)
	}
	return rand.Int64N(n)
}
