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

// ErrNoData signals an empty database for the requested query.
var ErrNoData = errors.New("storage: no data")

// RunRecord traces a collector run.
type RunRecord struct {
	ID         int64
	StartedUTC time.Time
	DurationMS int64
	OK         bool
	// HTTPStatus is 0 when no HTTP status is available (network failure
	// before a response); the corresponding column then stays NULL.
	HTTPStatus int
	ErrorKind  string
	Error      string
}

// TimeSeriesQuery describes an aggregated time-series request.
type TimeSeriesQuery struct {
	From time.Time // inclusive
	To   time.Time // exclusive
	Step time.Duration
}

// Bucket is a point aggregated over a time step aligned on the epoch.
type Bucket struct {
	Start       time.Time
	PowerPVAvgW *float64
	PowerPVMinW *float64
	PowerPVMaxW *float64
	Count       int
	Missing     int // number of samples in the bucket without a PV power value
}

// DailyEnergy is the energy of one local day.
type DailyEnergy struct {
	Date     string // YYYY-MM-DD in local timezone
	EnergyWh *float64
}

// EventsQuery describes an event read.
type EventsQuery struct {
	Limit    int
	From, To time.Time // zero value = no bound
}

const sampleColumns = `ts_utc, source, ts_source, quality, mode,
	power_pv_w, energy_day_wh, energy_year_wh, energy_total_wh,
	power_grid_w, power_load_w, power_battery_w,
	rel_autonomy_pct, rel_self_consumption_pct, inverters_json`

// inverterJSON is the persisted shape of inverter measurements. Fields that
// are absent are not written, so read back as nil: the absence stays discernible.
type inverterJSON struct {
	ID          string   `json:"id"`
	DT          int      `json:"dt"`
	PowerW      *float64 `json:"power_w,omitempty"`
	EnergyDayWh *float64 `json:"energy_day_wh,omitempty"`
}

// InsertSample records a sample. The write is idempotent on the
// key (ts_utc, source): replaying a backfill updates the row instead of
// duplicating it.
func (s *Store) InsertSample(ctx context.Context, sm model.Sample) error {
	inverters, err := json.Marshal(toInverterJSON(sm.Inverters))
	if err != nil {
		return fmt.Errorf("serialising inverters: %w", err)
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
		return fmt.Errorf("inserting sample %s: %w", formatTS(sm.TS), err)
	}
	// A measurement older than the watermark must be picked up by the
	// next aggregation pass (import of a backfill after the fact, device
	// clock running late): without this lowering, the read would believe it
	// already aggregated and omit it. The cost is a no-op statement on a
	// table of a few rows.
	return s.lowerWatermark(ctx, sm.TS)
}

// LatestSample returns the most recent sample, all sources combined.
func (s *Store) LatestSample(ctx context.Context) (model.Sample, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sampleColumns+` FROM samples ORDER BY ts_utc DESC LIMIT 1`)
	sm, err := scanSample(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Sample{}, ErrNoData
	}
	return sm, err
}

// TotalEnergyAt returns the total energy recorded at or before t: the most recent
// sample that carries one. The second return is that sample's timestamp — the
// baseline a derived energy is measured from — and the third says whether one was
// found at all.
func (s *Store) TotalEnergyAt(ctx context.Context, t time.Time) (float64, time.Time, bool, error) {
	var (
		ts    string
		value float64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT ts_utc, energy_total_wh FROM samples
		 WHERE ts_utc <= ? AND energy_total_wh IS NOT NULL
		 ORDER BY ts_utc DESC LIMIT 1`, formatTS(t)).Scan(&ts, &value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf("reading the total energy at or before %s: %w", formatTS(t), err)
	}
	parsed, err := parseTS(ts)
	if err != nil {
		return 0, time.Time{}, false, err
	}
	return value, parsed, true, nil
}

// SampleCount returns the number of retained samples.
func (s *Store) SampleCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting samples: %w", err)
	}
	return n, nil
}

// TimeSeries returns the non-empty buckets of [From, To[.
//
// The buckets are aligned on the epoch and a bucket with no row at all is not
// emitted: no interpolation is done, the absence of data must stay
// visible on the interface side.
//
// Steps that are multiples of one hour are served by the hourly aggregates
// (aggregate.go) when they exist; the other steps walk the raw measurements.
// The result is the same in both cases: the average is recomposed from the sum
// and the count, the minimum and the maximum are associative.
func (s *Store) TimeSeries(ctx context.Context, q TimeSeriesQuery) ([]Bucket, error) {
	if q.Step <= 0 {
		return nil, fmt.Errorf("invalid time step: %v", q.Step)
	}
	step := int64(q.Step.Seconds())
	if step <= 0 {
		return nil, fmt.Errorf("time step too fine for second-level storage: %v", q.Step)
	}

	acc := map[int64]*bucketAcc{}
	fromAggregates := false
	if q.Step >= time.Hour && q.Step%time.Hour == 0 {
		closed, hasWM, err := s.aggWatermark(ctx)
		if err != nil {
			return nil, err
		}
		if aggFrom, aggTo, ok := aggSplit(q.From, q.To, closed, hasWM); ok {
			if err := s.aggBucketsInto(ctx, acc, aggFrom, aggTo, step); err != nil {
				return nil, err
			}
			// Partial header and open hour: raw measurements.
			if err := s.rawBucketsInto(ctx, acc, q.From, aggFrom, step); err != nil {
				return nil, err
			}
			if err := s.rawBucketsInto(ctx, acc, aggTo, q.To, step); err != nil {
				return nil, err
			}
			fromAggregates = true
		}
	}
	if !fromAggregates {
		if err := s.rawBucketsInto(ctx, acc, q.From, q.To, step); err != nil {
			return nil, err
		}
	}
	return bucketsFromAcc(acc), nil
}

// DailyEnergy returns the energy of one local day, computed as the
// maximum of E_Day observed during the day.
//
// The maximum is taken per UTC hour then per local date: hourly maxima
// are offset-invariant, so the result stays correct across the transitions to
// summer or winter time (23 h and 25 h days). Taking the maximum per date
// avoids a meter reset at midnight being interpreted as a
// negative energy.
//
// Whole closed hours are read from the hourly aggregates
// (aggregate.go): the maximum of a union is the maximum of the maximums. The
// partial header, the open hour — or the whole range as long as no watermark
// exists — are read from the raw measurements.
func (s *Store) DailyEnergy(ctx context.Context, from, to time.Time) ([]DailyEnergy, error) {
	hours := map[string]float64{} // UTC hour "2006-01-02T15" -> maximum

	closed, hasWM, err := s.aggWatermark(ctx)
	if err != nil {
		return nil, err
	}
	if aggFrom, aggTo, ok := aggSplit(from, to, closed, hasWM); ok {
		if err := s.aggHourlyEnergyInto(ctx, hours, aggFrom, aggTo); err != nil {
			return nil, err
		}
		if err := s.rawHourlyEnergyInto(ctx, hours, from, aggFrom); err != nil {
			return nil, err
		}
		if err := s.rawHourlyEnergyInto(ctx, hours, aggTo, to); err != nil {
			return nil, err
		}
	} else if err := s.rawHourlyEnergyInto(ctx, hours, from, to); err != nil {
		return nil, err
	}

	hourKeys := make([]string, 0, len(hours))
	for h := range hours {
		hourKeys = append(hourKeys, h)
	}
	slices.Sort(hourKeys)

	type acc struct {
		max  float64
		seen bool
	}
	byDate := map[string]*acc{}
	var order []string

	for _, hour := range hourKeys {
		v := hours[hour]
		hourUTC, err := time.ParseInLocation("2006-01-02T15", hour, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("unexpected UTC hour %q: %w", hour, err)
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

	slices.Sort(order)
	out := make([]DailyEnergy, 0, len(order))
	for _, date := range order {
		a := byDate[date]
		out = append(out, DailyEnergy{Date: date, EnergyWh: new(a.max)})
	}
	return out, nil
}

// InsertEvent records a local event.
func (s *Store) InsertEvent(ctx context.Context, e model.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (ts_utc, kind, severity, message, detail) VALUES (?,?,?,?,?)`,
		formatTS(e.TS), e.Kind, string(e.Severity), e.Message, e.Detail)
	if err != nil {
		return fmt.Errorf("inserting event %s: %w", e.Kind, err)
	}
	return nil
}

// Events returns the events from most recent to oldest.
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
		return nil, fmt.Errorf("reading events: %w", err)
	}
	defer rows.Close()

	var out []model.Event
	for rows.Next() {
		var (
			ts, kind, severity, message, detail string
		)
		if err := rows.Scan(&ts, &kind, &severity, &message, &detail); err != nil {
			return nil, fmt.Errorf("reading an event: %w", err)
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
		return nil, fmt.Errorf("iterating events: %w", err)
	}
	return out, nil
}

// InsertRun records a collector run.
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
		return fmt.Errorf("inserting collector cycle: %w", err)
	}
	return nil
}

// LatestRun returns the last collector cycle. The second return is false
// if no cycle has been recorded yet.
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
		return RunRecord{}, false, fmt.Errorf("reading the last cycle: %w", err)
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

// SetState writes a collector state value.
func (s *Store) SetState(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO collector_state (k, v) VALUES (?,?)
		 ON CONFLICT(k) DO UPDATE SET v = excluded.v`, k, v)
	if err != nil {
		return fmt.Errorf("writing state %q: %w", k, err)
	}
	return nil
}

// State returns the collector state (never nil).
func (s *Store) State(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k, v FROM collector_state`)
	if err != nil {
		return nil, fmt.Errorf("reading collector state: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("reading a state entry: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating state: %w", err)
	}
	return out, nil
}

// DeviceRecord is the identity and detection state of the device.
//
// The four detection states are kept as they are (and not flattened into
// booleans): "unknown" is not "absent".
//
// An empty string means "not found in the response": the conversion to
// null is done at the API boundary (internal/api/dto.go), never here.
type DeviceRecord struct {
	ReadAtUTC     time.Time
	Source        string
	Model         string
	DeviceName    string
	ArticleNumber string
	Firmware      string
	SerialNumber  string
	// Each state carries its cause: "unknown" without a reason is not
	// usable, and reconstructing the cause at read time would be inventing it.
	IdentityState      model.DetectionState
	IdentityReason     string
	MeterState         model.DetectionState
	MeterSource        string
	MeterReason        string
	BatteryState       model.DetectionState
	BatterySource      string
	BatteryReason      string
	DeviceEventsState  model.DetectionState
	DeviceEventsReason string
}

// SaveDevice records the device state. The write is idempotent: the
// single row (id = 1) is replaced, never duplicated.
func (s *Store) SaveDevice(ctx context.Context, d DeviceRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device (
			id, read_at_utc, source, model, device_name, article_number, firmware, serial_number,
			identity_state, identity_reason,
			meter_state, meter_source, meter_reason,
			battery_state, battery_source, battery_reason,
			device_events_state, device_events_reason)
		 VALUES (1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
			read_at_utc = excluded.read_at_utc,
			source = excluded.source,
			model = excluded.model,
			device_name = excluded.device_name,
			article_number = excluded.article_number,
			firmware = excluded.firmware,
			serial_number = excluded.serial_number,
			identity_state = excluded.identity_state,
			identity_reason = excluded.identity_reason,
			meter_state = excluded.meter_state,
			meter_source = excluded.meter_source,
			meter_reason = excluded.meter_reason,
			battery_state = excluded.battery_state,
			battery_source = excluded.battery_source,
			battery_reason = excluded.battery_reason,
			device_events_state = excluded.device_events_state,
			device_events_reason = excluded.device_events_reason`,
		formatTS(d.ReadAtUTC), d.Source, d.Model, d.DeviceName, d.ArticleNumber, d.Firmware, d.SerialNumber,
		string(d.IdentityState), d.IdentityReason,
		string(d.MeterState), d.MeterSource, d.MeterReason,
		string(d.BatteryState), d.BatterySource, d.BatteryReason,
		string(d.DeviceEventsState), d.DeviceEventsReason)
	if err != nil {
		return fmt.Errorf("writing device state: %w", err)
	}
	return nil
}

// Device returns the recorded state. The second return is false if no
// identity read has succeeded yet: "unknown" is not an empty
// record.
func (s *Store) Device(ctx context.Context) (DeviceRecord, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT read_at_utc, source, model, device_name, article_number, firmware, serial_number,
		        identity_state, identity_reason,
		        meter_state, meter_source, meter_reason,
		        battery_state, battery_source, battery_reason,
		        device_events_state, device_events_reason
		 FROM device WHERE id = 1`)

	var (
		readAt, source, modelName, deviceName, articleNumber, firmware, serial string
		identityState, meterState, batteryState, eventsState                   string
		identityReason, meterReason, batteryReason                             string
		eventsReason, meterSource, batterySource                               string
	)
	if err := row.Scan(&readAt, &source, &modelName, &deviceName, &articleNumber, &firmware, &serial,
		&identityState, &identityReason,
		&meterState, &meterSource, &meterReason,
		&batteryState, &batterySource, &batteryReason,
		&eventsState, &eventsReason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeviceRecord{}, false, nil
		}
		return DeviceRecord{}, false, fmt.Errorf("reading device state: %w", err)
	}
	parsed, err := parseTS(readAt)
	if err != nil {
		return DeviceRecord{}, false, err
	}
	return DeviceRecord{
		ReadAtUTC:          parsed,
		Source:             source,
		Model:              modelName,
		DeviceName:         deviceName,
		ArticleNumber:      articleNumber,
		Firmware:           firmware,
		SerialNumber:       serial,
		IdentityState:      model.DetectionState(identityState),
		IdentityReason:     identityReason,
		MeterState:         model.DetectionState(meterState),
		MeterSource:        meterSource,
		MeterReason:        meterReason,
		BatteryState:       model.DetectionState(batteryState),
		BatterySource:      batterySource,
		BatteryReason:      batteryReason,
		DeviceEventsState:  model.DetectionState(eventsState),
		DeviceEventsReason: eventsReason,
	}, true, nil
}

// MarkDeviceEventSeen remembers the key of a hardware event.
//
// The second return is true if the key is new (the event must be
// recorded), false if it was already known (it has already been seen: no
// write). This is the anti-duplication mechanism for device events,
// whose stream is republished in full on every poll.
func (s *Store) MarkDeviceEventSeen(ctx context.Context, key string, seenAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO device_events_seen (key, seen_at_utc) VALUES (?,?)
		 ON CONFLICT(key) DO NOTHING`,
		key, formatTS(seenAt))
	if err != nil {
		return false, fmt.Errorf("marking event %q: %w", key, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("counting marked rows: %w", err)
	}
	return affected > 0, nil
}

// DeviceEventsSeenCount returns the number of hardware event keys
// remembered.
func (s *Store) DeviceEventsSeenCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_events_seen`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting seen hardware events: %w", err)
	}
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// PurgeBefore deletes the data older than t.
//
// The purge covers the measurements, the events and the collector cycles.
// It returns the number of deleted samples, which is the reference measure
// of the retention.
func (s *Store) PurgeBefore(ctx context.Context, t time.Time) (int64, error) {
	limit := formatTS(t)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("purge: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM samples WHERE ts_utc < ?`, limit)
	if err != nil {
		return 0, fmt.Errorf("purging measurements: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("counting purged measurements: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE ts_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purging events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM collector_runs WHERE started_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purging collector cycles: %w", err)
	}
	// The anti-duplication marks follow the events they
	// deduplicate: a mark that outlived its event would
	// permanently prevent seeing it again, for no gain.
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_events_seen WHERE seen_at_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purging hardware event marks: %w", err)
	}
	// The survey outcomes follow the same bound too: a survey describes what a
	// firmware answered, and one older than the retained history describes a
	// past state of the installation. Re-running the survey rebuilds it.
	if _, err := tx.ExecContext(ctx, `DELETE FROM source_probe WHERE probed_at_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purging the survey: %w", err)
	}
	// The aggregates follow exactly the same bound as the measurements: a
	// aggregate row that overlaps the purged part describes measurements that
	// no longer exist, and the read would believe it complete. The purge bound
	// being aligned on the hour (see internal/maintenance), the two tables
	// stay consistent to the nearest hour.
	if _, err := tx.ExecContext(ctx, `DELETE FROM agg_hour WHERE bucket_utc < ?`, limit); err != nil {
		return 0, fmt.Errorf("purging aggregates: %w", err)
	}
	// The watermark cannot stay earlier than the purge bound:
	// the hours it covers have neither measurements nor aggregates.
	if _, err := tx.ExecContext(ctx,
		`UPDATE collector_state SET v = ? WHERE k = ? AND v < ?`,
		limit, aggStateKey, limit); err != nil {
		return 0, fmt.Errorf("watermark after purge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing purge: %w", err)
	}
	return deleted, nil
}

// scanSample reads a row of `samples`.
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
		return model.Sample{}, fmt.Errorf("reading a sample: %w", err)
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
			return model.Sample{}, fmt.Errorf("deserialising inverters: %w", err)
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
