package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"fronius-supervisor/internal/model"
)

// ErrNoData signale une base vide pour la requête demandée.
var ErrNoData = errors.New("storage: aucune donnée")

// RunRecord trace une exécution du collecteur.
type RunRecord struct {
	ID         int64
	StartedUTC time.Time
	DurationMS int64
	OK         bool
	// HTTPStatus vaut 0 lorsqu'aucun code HTTP n'est disponible (panne réseau
	// avant réponse) ; la colonne correspondante reste alors NULL.
	HTTPStatus int
	ErrorKind  string
	Error      string
}

// TimeSeriesQuery décrit une demande de série temporelle agrégée.
type TimeSeriesQuery struct {
	From time.Time // inclus
	To   time.Time // exclu
	Step time.Duration
}

// Bucket est un point agrégé sur un pas de temps aligné sur l'epoch.
type Bucket struct {
	Start       time.Time
	PowerPVAvgW *float64
	PowerPVMinW *float64
	PowerPVMaxW *float64
	Count       int
	Missing     int // nombre d'échantillons du bucket sans valeur de puissance PV
}

// DailyEnergy est l'énergie d'une journée locale.
type DailyEnergy struct {
	Date     string // YYYY-MM-DD en fuseau local
	EnergyWh *float64
}

// EventsQuery décrit une lecture d'événements.
type EventsQuery struct {
	Limit    int
	From, To time.Time // valeur nulle = pas de borne
}

const sampleColumns = `ts_utc, source, ts_source, quality, mode,
	power_pv_w, energy_day_wh, energy_year_wh, energy_total_wh,
	power_grid_w, power_load_w, power_battery_w,
	rel_autonomy_pct, rel_self_consumption_pct, inverters_json`

// inverterJSON est la forme persistée des mesures d'onduleur. Les champs
// absents ne sont pas écrits, donc lus à nil : l'absence reste discernable.
type inverterJSON struct {
	ID          string   `json:"id"`
	DT          int      `json:"dt"`
	PowerW      *float64 `json:"power_w,omitempty"`
	EnergyDayWh *float64 `json:"energy_day_wh,omitempty"`
}

// InsertSample enregistre un échantillon. L'écriture est idempotente sur la
// clé (ts_utc, source) : rejouer un backfill met à jour la ligne au lieu de la
// dupliquer.
func (s *Store) InsertSample(ctx context.Context, sm model.Sample) error {
	inverters, err := json.Marshal(toInverterJSON(sm.Inverters))
	if err != nil {
		return fmt.Errorf("sérialisation des onduleurs: %w", err)
	}

	const q = `INSERT INTO samples (` + sampleColumns + `)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(ts_utc, source) DO UPDATE SET
			ts_source = excluded.ts_source,
			quality = excluded.quality,
			mode = excluded.mode,
			power_pv_w = excluded.power_pv_w,
			energy_day_wh = excluded.energy_day_wh,
			energy_year_wh = excluded.energy_year_wh,
			energy_total_wh = excluded.energy_total_wh,
			power_grid_w = excluded.power_grid_w,
			power_load_w = excluded.power_load_w,
			power_battery_w = excluded.power_battery_w,
			rel_autonomy_pct = excluded.rel_autonomy_pct,
			rel_self_consumption_pct = excluded.rel_self_consumption_pct,
			inverters_json = excluded.inverters_json`

	_, err = s.db.ExecContext(ctx, q,
		formatTS(sm.TS), sm.Source, sm.TSSource, string(sm.Quality), sm.Mode,
		sm.PowerPVW, sm.EnergyDayWh, sm.EnergyYearWh, sm.EnergyTotalWh,
		sm.PowerGridW, sm.PowerLoadW, sm.PowerBatteryW,
		sm.RelAutonomyPct, sm.RelSelfConsumptionPct, string(inverters),
	)
	if err != nil {
		return fmt.Errorf("insertion de l'échantillon %s: %w", formatTS(sm.TS), err)
	}
	return nil
}

// LatestSample retourne l'échantillon le plus récent, toutes sources confondues.
func (s *Store) LatestSample(ctx context.Context) (model.Sample, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sampleColumns+` FROM samples ORDER BY ts_utc DESC LIMIT 1`)
	sm, err := scanSample(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Sample{}, ErrNoData
	}
	return sm, err
}

// SampleCount retourne le nombre d'échantillons conservés.
func (s *Store) SampleCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples`).Scan(&n); err != nil {
		return 0, fmt.Errorf("comptage des échantillons: %w", err)
	}
	return n, nil
}

// TimeSeries retourne les buckets non vides de [From, To[.
//
// Les buckets sont alignés sur l'epoch et un bucket sans aucune ligne n'est pas
// émis : aucune interpolation n'est faite, l'absence de donnée doit rester
// visible côté interface.
func (s *Store) TimeSeries(ctx context.Context, q TimeSeriesQuery) ([]Bucket, error) {
	if q.Step <= 0 {
		return nil, fmt.Errorf("pas de temps invalide: %v", q.Step)
	}
	step := int64(q.Step.Seconds())
	if step <= 0 {
		return nil, fmt.Errorf("pas de temps trop fin pour un stockage à la seconde: %v", q.Step)
	}

	const sqlQ = `
		SELECT CAST(strftime('%s', ts_utc) AS INTEGER) / ? * ? AS bucket,
		       AVG(power_pv_w), MIN(power_pv_w), MAX(power_pv_w),
		       COUNT(*), SUM(CASE WHEN power_pv_w IS NULL THEN 1 ELSE 0 END)
		FROM samples
		WHERE ts_utc >= ? AND ts_utc < ?
		GROUP BY bucket
		ORDER BY bucket`

	rows, err := s.db.QueryContext(ctx, sqlQ, step, step, formatTS(q.From), formatTS(q.To))
	if err != nil {
		return nil, fmt.Errorf("série temporelle: %w", err)
	}
	defer rows.Close()

	var out []Bucket
	for rows.Next() {
		var (
			bucket        int64
			avg, min, max sql.NullFloat64
			count         int
			missingNull   sql.NullInt64
		)
		if err := rows.Scan(&bucket, &avg, &min, &max, &count, &missingNull); err != nil {
			return nil, fmt.Errorf("lecture d'un bucket: %w", err)
		}
		out = append(out, Bucket{
			// La requête renvoie déjà le début du bucket en secondes depuis
			// l'epoch (secondes / pas × pas).
			Start:       time.Unix(bucket, 0).UTC(),
			PowerPVAvgW: nullFloat(avg),
			PowerPVMinW: nullFloat(min),
			PowerPVMaxW: nullFloat(max),
			Count:       count,
			Missing:     int(missingNull.Int64),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parcours des buckets: %w", err)
	}
	return out, nil
}

// DailyEnergy retourne l'énergie d'une journée locale, calculée comme le
// maximum de E_Day observé dans la journée.
//
// Le maximum est pris par heure UTC puis par date locale : les maxima horaires
// sont invariants d'offset, donc le résultat reste correct lors des passages à
// l'heure d'été ou d'hiver (journées de 23 h et 25 h). Prendre le maximum par
// date évite qu'une remise à zéro du compteur à minuit ne soit interprétée
// comme une énergie négative.
func (s *Store) DailyEnergy(ctx context.Context, from, to time.Time) ([]DailyEnergy, error) {
	const sqlQ = `
		SELECT strftime('%Y-%m-%dT%H', ts_utc) AS hour_utc, MAX(energy_day_wh)
		FROM samples
		WHERE ts_utc >= ? AND ts_utc < ? AND energy_day_wh IS NOT NULL
		GROUP BY hour_utc
		ORDER BY hour_utc`

	rows, err := s.db.QueryContext(ctx, sqlQ, formatTS(from), formatTS(to))
	if err != nil {
		return nil, fmt.Errorf("énergie journalière: %w", err)
	}
	defer rows.Close()

	type acc struct {
		max  float64
		seen bool
	}
	byDate := map[string]*acc{}
	var order []string

	for rows.Next() {
		var hour string
		var v float64
		if err := rows.Scan(&hour, &v); err != nil {
			return nil, fmt.Errorf("lecture d'un maximum horaire: %w", err)
		}
		hourUTC, err := time.ParseInLocation("2006-01-02T15", hour, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("heure UTC inattendue %q: %w", hour, err)
		}
		date := hourUTC.In(s.loc).Format("2006-01-02")
		a, ok := byDate[date]
		if !ok {
			a = &acc{}
			byDate[date] = a
			order = append(order, date)
		}
		if !a.seen || v > a.max {
			a.max = v
		}
		a.seen = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parcours des maxima horaires: %w", err)
	}

	slices.Sort(order)
	out := make([]DailyEnergy, 0, len(order))
	for _, date := range order {
		a := byDate[date]
		out = append(out, DailyEnergy{Date: date, EnergyWh: new(a.max)})
	}
	return out, nil
}

// InsertEvent enregistre un événement local.
func (s *Store) InsertEvent(ctx context.Context, e model.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (ts_utc, kind, severity, message, detail) VALUES (?,?,?,?,?)`,
		formatTS(e.TS), e.Kind, string(e.Severity), e.Message, e.Detail)
	if err != nil {
		return fmt.Errorf("insertion de l'événement %s: %w", e.Kind, err)
	}
	return nil
}

// Events retourne les événements du plus récent au plus ancien.
func (s *Store) Events(ctx context.Context, q EventsQuery) ([]model.Event, error) {
	sqlQ := `SELECT ts_utc, kind, severity, message, detail FROM events WHERE 1=1`
	args := []any{}
	if !q.From.IsZero() {
		sqlQ += ` AND ts_utc >= ?`
		args = append(args, formatTS(q.From))
	}
	if !q.To.IsZero() {
		sqlQ += ` AND ts_utc < ?`
		args = append(args, formatTS(q.To))
	}
	sqlQ += ` ORDER BY ts_utc DESC, id DESC`
	if q.Limit > 0 {
		sqlQ += ` LIMIT ?`
		args = append(args, q.Limit)
	}

	rows, err := s.db.QueryContext(ctx, sqlQ, args...)
	if err != nil {
		return nil, fmt.Errorf("lecture des événements: %w", err)
	}
	defer rows.Close()

	var out []model.Event
	for rows.Next() {
		var (
			ts, kind, severity, message, detail string
		)
		if err := rows.Scan(&ts, &kind, &severity, &message, &detail); err != nil {
			return nil, fmt.Errorf("lecture d'un événement: %w", err)
		}
		parsed, err := parseTS(ts)
		if err != nil {
			return nil, err
		}
		out = append(out, model.Event{
			TS:       parsed,
			Kind:     kind,
			Severity: model.Severity(severity),
			Message:  message,
			Detail:   detail,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parcours des événements: %w", err)
	}
	return out, nil
}

// InsertRun enregistre une exécution du collecteur.
func (s *Store) InsertRun(ctx context.Context, r RunRecord) error {
	var httpStatus any
	if r.HTTPStatus != 0 {
		httpStatus = r.HTTPStatus
	}
	ok := 0
	if r.OK {
		ok = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collector_runs (started_utc, duration_ms, ok, http_status, error_kind, error)
		 VALUES (?,?,?,?,?,?)`,
		formatTS(r.StartedUTC), r.DurationMS, ok, httpStatus, r.ErrorKind, r.Error)
	if err != nil {
		return fmt.Errorf("insertion du cycle de collecte: %w", err)
	}
	return nil
}

// LatestRun retourne le dernier cycle de collecte. Le second retour vaut false
// si aucun cycle n'a encore été enregistré.
func (s *Store) LatestRun(ctx context.Context) (RunRecord, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT started_utc, duration_ms, ok, http_status, error_kind, error
		 FROM collector_runs ORDER BY id DESC LIMIT 1`)

	var (
		started                 string
		duration                int64
		ok                      int
		httpStatus              sql.NullInt64
		errorKind, errorMessage string
	)
	if err := row.Scan(&started, &duration, &ok, &httpStatus, &errorKind, &errorMessage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunRecord{}, false, nil
		}
		return RunRecord{}, false, fmt.Errorf("lecture du dernier cycle: %w", err)
	}
	parsed, err := parseTS(started)
	if err != nil {
		return RunRecord{}, false, err
	}
	return RunRecord{
		StartedUTC: parsed,
		DurationMS: duration,
		OK:         ok != 0,
		HTTPStatus: int(httpStatus.Int64),
		ErrorKind:  errorKind,
		Error:      errorMessage,
	}, true, nil
}

// SetState écrit une valeur d'état du collecteur.
func (s *Store) SetState(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collector_state (k, v) VALUES (?,?)
		 ON CONFLICT(k) DO UPDATE SET v = excluded.v`, k, v)
	if err != nil {
		return fmt.Errorf("écriture de l'état %q: %w", k, err)
	}
	return nil
}

// State retourne l'état du collecteur (jamais nil).
func (s *Store) State(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k, v FROM collector_state`)
	if err != nil {
		return nil, fmt.Errorf("lecture de l'état du collecteur: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("lecture d'une entrée d'état: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("parcours de l'état: %w", err)
	}
	return out, nil
}

// PurgeBefore supprime les données antérieures à t.
//
// La purge porte sur les mesures, les événements et les cycles de collecte.
// Elle retourne le nombre d'échantillons supprimés, qui est la mesure de
// référence de la rétention.
func (s *Store) PurgeBefore(ctx context.Context, t time.Time) (int64, error) {
	limit := formatTS(t)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("purge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM samples WHERE ts_utc < ?`, limit)
	if err != nil {
		return 0, fmt.Errorf("purge des mesures: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("comptage des mesures purgées: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE ts_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purge des événements: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM collector_runs WHERE started_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purge des cycles de collecte: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("validation de la purge: %w", err)
	}
	return deleted, nil
}

// scanSample lit une ligne de `samples`.
func scanSample(row interface{ Scan(...any) error }) (model.Sample, error) {
	var (
		ts, source, tsSource, quality, mode, invertersJSON string
		pv, eDay, eYear, eTotal                            sql.NullFloat64
		pGrid, pLoad, pBat, relAuto, relSelf               sql.NullFloat64
	)
	if err := row.Scan(&ts, &source, &tsSource, &quality, &mode,
		&pv, &eDay, &eYear, &eTotal, &pGrid, &pLoad, &pBat,
		&relAuto, &relSelf, &invertersJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Sample{}, err
		}
		return model.Sample{}, fmt.Errorf("lecture d'un échantillon: %w", err)
	}

	parsed, err := parseTS(ts)
	if err != nil {
		return model.Sample{}, err
	}

	sm := model.Sample{
		TS:                    parsed,
		TSSource:              tsSource,
		Source:                source,
		Quality:               model.Quality(quality),
		Mode:                  mode,
		PowerPVW:              nullFloat(pv),
		EnergyDayWh:           nullFloat(eDay),
		EnergyYearWh:          nullFloat(eYear),
		EnergyTotalWh:         nullFloat(eTotal),
		PowerGridW:            nullFloat(pGrid),
		PowerLoadW:            nullFloat(pLoad),
		PowerBatteryW:         nullFloat(pBat),
		RelAutonomyPct:        nullFloat(relAuto),
		RelSelfConsumptionPct: nullFloat(relSelf),
	}

	if invertersJSON != "" && invertersJSON != "[]" {
		var stored []inverterJSON
		if err := json.Unmarshal([]byte(invertersJSON), &stored); err != nil {
			return model.Sample{}, fmt.Errorf("désérialisation des onduleurs: %w", err)
		}
		sm.Inverters = make([]model.InverterSample, 0, len(stored))
		for _, inv := range stored {
			sm.Inverters = append(sm.Inverters, model.InverterSample{
				ID:          inv.ID,
				DT:          inv.DT,
				PowerW:      inv.PowerW,
				EnergyDayWh: inv.EnergyDayWh,
			})
		}
	}
	return sm, nil
}

func toInverterJSON(in []model.InverterSample) []inverterJSON {
	out := make([]inverterJSON, 0, len(in))
	for _, s := range in {
		out = append(out, inverterJSON{
			ID:          s.ID,
			DT:          s.DT,
			PowerW:      s.PowerW,
			EnergyDayWh: s.EnergyDayWh,
		})
	}
	return out
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return new(v.Float64)
}
