// Package maintenance exécute les travaux de fond de la base : agrégation
// horaire et purge de rétention (ADR 0006).
//
// Ces travaux ne dépendent d'aucune mesure fraîche, d'où leur planification ici
// plutôt que dans le collecteur : l'agrégation repart de la borne d'eau
// enregistrée et rattrape donc à elle seule une interruption du service.
//
// Les deux travaux sont journalisés mais ne peuvent pas arrêter la boucle : une
// panne d'un cycle (base occupée, disque plein) ne doit pas priver la
// supervision de sa maintenance jusqu'au prochain redémarrage.
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// DefaultTick est la cadence des travaux de fond. Elle borne aussi la
// précision de l'heure de purge : une rétention décalée de quelques minutes
// est sans conséquence.
const DefaultTick = 5 * time.Minute

// purgeHour est l'heure locale à partir de laquelle la purge quotidienne a lieu.
const purgeHour = 3

// Store est le sous-ensemble du magasin utilisé par la maintenance.
// *storage.Store le satisfait sans adaptateur.
type Store interface {
	Rollup(ctx context.Context, now time.Time) (storage.RollupResult, error)
	PurgeBefore(ctx context.Context, t time.Time) (int64, error)
	InsertEvent(ctx context.Context, e model.Event) error
}

// Runner planifie l'agrégation horaire et la purge de rétention.
type Runner struct {
	Store Store
	Loc   *time.Location
	// RetentionDays vaut 0 pour une conservation illimitée : aucune purge, ni
	// au démarrage ni ensuite, n'est alors exécutée.
	RetentionDays int
	// Tick borne la cadence des travaux (DefaultTick si nul).
	Tick time.Duration
	Log  *slog.Logger
	// Now est injectable (tests) ; time.Now par défaut.
	Now func() time.Time

	// lastPurgeDate mémorise la dernière journée locale purgée. L'état est
	// volontairement en mémoire : un redémarrage provoque une purge au
	// démarrage, ce qui est le comportement attendu (et suffisant).
	lastPurgeDate string
}

// Run agrège les heures closes toutes les Tick et purge une fois par jour
// local, jusqu'à l'annulation du contexte. Retourne nil à l'arrêt.
func (r *Runner) Run(ctx context.Context) error {
	log := r.log()
	log.Info("maintenance démarrée", "tick", r.tick(), "retention_days", r.RetentionDays)

	r.RollupOnce(ctx)
	if r.RetentionDays > 0 {
		// Une purge au démarrage : c'est le comportement de la phase 1, et
		// cela rattrape la durée d'arrêt sans attendre le lendemain.
		r.PurgeOnce(ctx)
	} else {
		log.Info("rétention illimitée : purge planifiée désactivée")
	}

	tick := time.NewTicker(r.tick())
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("maintenance arrêtée")
			return nil
		case <-tick.C:
			r.RollupOnce(ctx)
			r.maybePurge(ctx)
		}
	}
}

// RollupOnce agrège les heures closes jusqu'à maintenant.
func (r *Runner) RollupOnce(ctx context.Context) storage.RollupResult {
	res, err := r.Store.Rollup(ctx, r.now())
	if err != nil {
		r.log().Error("agrégation horaire impossible", "error", err)
		return storage.RollupResult{}
	}
	switch {
	case res.To.Sub(res.From) > time.Hour:
		// Rattrapage après une interruption : une passe de routine ne couvre
		// qu'une heure, elle mérite le niveau debug seulement.
		r.log().Info("agrégation horaire rattrapée",
			"from", res.From, "to", res.To, "buckets", res.Buckets)
	case res.Buckets > 0:
		r.log().Debug("agrégation horaire", "buckets", res.Buckets)
	}
	return res
}

// maybePurge déclenche la purge une fois par journée locale, à partir de
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

// PurgeOnce applique la rétention et journalise le résultat.
func (r *Runner) PurgeOnce(ctx context.Context) {
	now := r.now()
	// La journée est marquée dès la tentative : une rétention qui échoue est
	// signalée, puis retentée le lendemain plutôt qu'à chaque tick.
	r.lastPurgeDate = now.In(r.loc()).Format("2006-01-02")

	limit := retentionLimit(now, r.loc(), r.RetentionDays)
	deleted, err := r.Store.PurgeBefore(ctx, limit)
	if err != nil {
		r.log().Error("purge de rétention impossible", "error", err)
		return
	}
	r.log().Info("purge de rétention terminée",
		"before", limit, "samples_deleted", deleted, "retention_days", r.RetentionDays)

	if err := r.Store.InsertEvent(ctx, model.Event{
		TS:       now.UTC(),
		Kind:     model.EventPurge,
		Severity: model.SeverityInfo,
		Message:  "purge de rétention exécutée",
		Detail: fmt.Sprintf("%d échantillons supprimés avant %s",
			deleted, limit.Format(time.RFC3339)),
	}); err != nil {
		r.log().Warn("événement de purge non enregistré", "error", err)
	}
}

// retentionLimit retourne la borne de purge : minuit local du jour il y a
// `days` jours, arrondi à l'heure pleine SUPÉRIEURE.
//
// L'arrondi est structurel : les agrégats et les mesures doivent partager une
// frontière alignée sur l'heure. Une frontière plus fine laisserait l'heure à
// cheval avec son agrégat supprimé mais une partie de ses mesures conservée —
// ces mesures deviendraient invisibles des lectures agrégées. Pour un fuseau
// décalé d'un nombre entier d'heures, dont Europe/Brussels, l'arrondi est
// neutre.
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
