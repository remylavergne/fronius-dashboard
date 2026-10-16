// Package collector assure l'acquisition périodique des mesures.
//
// Invariant : la seule opération distante est un GET. Aucun retry, aucun
// backoff et aucune reprise ne peut déclencher une écriture sur l'onduleur,
// puisque le transport lui-même refuse toute méthode autre que GET
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

// Clés d'état persistées.
const (
	StateLastSuccess = "last_success_utc"
	StateLastError   = "last_error"
)

// Bornes du backoff appliqué après un échec.
const (
	initialBackoff = time.Second
	maxBackoff     = time.Minute
)

// Source fournit la mesure brute. L'interface est volontairement réduite à
// l'unique endpoint de la phase 1.
type Source interface {
	GetPowerFlowRealtimeData(ctx context.Context) (solarapi.PowerFlowEnvelope, error)
}

// Sink persiste les effets d'un cycle de collecte.
//
// InsertEvent ne figure pas dans la définition minimale du plan mais il est
// nécessaire au comportement demandé (événement de sévérité warning à l'échec,
// avec anti-duplication, et événement de reprise).
type Sink interface {
	InsertSample(context.Context, model.Sample) error
	InsertEvent(context.Context, model.Event) error
	InsertRun(context.Context, storage.RunRecord) error
	SetState(context.Context, string, string) error
}

// Collector boucle sur la source et persiste les résultats.
type Collector struct {
	Source Source
	Sink   Sink
	Log    *slog.Logger
	// Interval est la période nominale en cas de succès.
	Interval time.Duration
	// Jitter est l'amplitude de la variation aléatoire appliquée à Interval.
	Jitter time.Duration
	// Now permet d'injecter une horloge (tests).
	Now func() time.Time
	// Rand permet d'injecter une source aléatoire reproductible (tests).
	Rand *rand.Rand

	mu sync.Mutex
	// lastEventDetail mémorise le dernier message d'erreur ayant donné lieu à
	// un événement, pour ne pas dupliquer un événement à chaque cycle.
	lastEventDetail string
	// failing indique qu'une série d'échecs est en cours.
	failing bool
}

// Run collecte jusqu'à l'annulation du contexte. Retourne nil lorsque le
// contexte est annulé : l'arrêt est un cas nominal, pas une erreur.
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

// CollectOnce exécute un cycle complet et retourne l'échantillon persisté.
//
// En cas d'erreur, aucun échantillon n'est inséré : un cycle raté ne doit
// jamais produire de point de mesure, sous peine d'inventer une donnée.
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
		// L'échec de persistance n'est pas un échec de l'appareil : pas
		// d'événement de panne matérielle, et l'erreur remonte telle quelle.
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
		c.log().Error("enregistrement du cycle échoué impossible", "error", logErr)
	}
	if setErr := c.Sink.SetState(ctx, StateLastError, err.Error()); setErr != nil {
		c.log().Error("écriture de last_error impossible", "error", setErr)
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
		Message:  "échec de la collecte",
		Detail:   detail,
	}); insErr != nil {
		c.log().Error("insertion de l'événement d'échec impossible", "error", insErr)
	}

	c.log().Warn("collecte échouée", "kind", kind, "error", err)
}

// onSuccess publie l'événement de reprise si une série d'échecs vient de
// s'achever.
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
		Message:  "collecte rétablie",
	}); err != nil {
		c.log().Error("insertion de l'événement de reprise impossible", "error", err)
	}
	c.log().Info("collecte rétablie")
}

// successDelay retourne Interval ± Jitter.
func (c *Collector) successDelay() time.Duration {
	if c.Jitter <= 0 {
		return c.Interval
	}
	// Int64N(2*Jitter+1) ∈ [0, 2·Jitter] : le décalage reste dans les bornes.
	offset := time.Duration(c.int64n(int64(2*c.Jitter) + 1))
	d := c.Interval - c.Jitter + offset
	if d < 0 {
		return 0
	}
	return d
}

// backoff retourne le délai après attempt échecs consécutifs :
// 1 s, 2 s, 4 s, … plafonné à 60 s.
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
