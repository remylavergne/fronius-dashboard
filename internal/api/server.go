package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/detect"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
	"fronius-supervisor/internal/webui"
)

const (
	// autoMaxBuckets bounds the number of points returned by step=auto.
	autoMaxBuckets = 1500
	// maxRangeDays bounds the span of a time-series request.
	maxRangeDays = 366
	// defaultEventsLimit / maxEventsLimit bound the pagination of events.
	defaultEventsLimit = 100
	maxEventsLimit     = 500
	// defaultDailyDays is the default depth of daily energy.
	defaultDailyDays = 30
)

// stepOptions lists the accepted steps, from finest to coarsest.
var stepOptions = []struct {
	name string
	d    time.Duration
}{
	{"1m", time.Minute},
	{"5m", 5 * time.Minute},
	{"15m", 15 * time.Minute},
	{"1h", time.Hour},
	{"1d", 24 * time.Hour},
}

// Server serves the REST API.
type Server struct {
	store   *storage.Store
	cfg     config.Config
	log     *slog.Logger
	version string
	started time.Time
	now     func() time.Time

	// assets is the interface source resolved once at construction: an
	// explicit FRONIUS_WEB_DIR, else the embedded copy, else nil (API only).
	assets fs.FS

	// Connectivity test cache. connMu also serialises the probes themselves:
	// the server is shared, and two simultaneous presses must not multiply
	// the authentication attempts towards the device.
	connMu       sync.Mutex
	connResult   *connectivityDTO
	connTestedAt time.Time
}

// New builds the API server.
func New(store *storage.Store, cfg config.Config, version string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		store:   store,
		cfg:     cfg,
		log:     log,
		version: version,
		started: time.Now(),
		now:     time.Now,
		assets:  resolveAssets(cfg.WebDir, webui.FS),
	}
}

// Handler returns the router, wrapped with the interface service when a
// frontend source exists (an explicit FRONIUS_WEB_DIR or the embedded copy).
func (s *Server) Handler() http.Handler {
	return staticHandler(s.apiMux(), s.assets)
}

func (s *Server) apiMux() http.Handler {
	mux := http.NewServeMux()

	// Go 1.22 selects the most specific pattern: "GET /x" wins for a
	// GET, while "/x" catches the other methods and produces a JSON 405.
	register := func(path string, h http.HandlerFunc) {
		mux.HandleFunc("GET "+path, h)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", http.MethodGet)
			s.writeError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
				fmt.Sprintf("method %s not allowed on %s", r.Method, path))
		})
	}

	register("/api/v1/health", s.handleHealth)
	register("/api/v1/system", s.handleSystem)
	register("/api/v1/connectivity", s.handleConnectivity)
	register("/api/v1/live", s.handleLive)
	register("/api/v1/timeseries", s.handleTimeSeries)
	register("/api/v1/energy/daily", s.handleDailyEnergy)
	register("/api/v1/events", s.handleEvents)
	register("/api/v1/capabilities", s.handleCapabilities)
	register("/api/v1/sources", s.handleSources)
	register("/api/v1/detail", s.handleDetail)

	// Everything else: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, http.StatusNotFound, CodeNotFound,
			fmt.Sprintf("unknown path: %s", r.URL.Path))
	})

	return mux
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := healthDTO{
		Version: s.version,
		UptimeS: int64(s.now().Sub(s.started).Seconds()),
	}

	count, err := s.store.SampleCount(ctx)
	if err != nil {
		// The raw error message may contain a file path: it stays
		// in the logs, not in the response.
		s.log.Error("health: counting samples failed", "error", err)
		out.DB = dbHealthDTO{OK: false, Error: "reading the database failed"}
		out.Status = "error"
	} else {
		out.DB = dbHealthDTO{OK: true}
		out.Collector.SamplesTotal = count
	}

	degraded := false
	if state, err := s.store.State(ctx); err != nil {
		s.log.Error("health: reading collector state failed", "error", err)
	} else {
		out.Collector.LastSuccessUTC = state["last_success_utc"]
		out.Collector.LastError = state["last_error"]
	}

	if run, ok, err := s.store.LatestRun(ctx); err != nil {
		s.log.Error("health: reading the last cycle failed", "error", err)
	} else if ok {
		out.Collector.LastRunMS = run.DurationMS
		degraded = !run.OK
	}

	switch {
	case out.Status == "error":
	default:
		if degraded {
			out.Status = "degraded"
		} else {
			out.Status = "ok"
		}
	}

	s.writeData(w, http.StatusOK, out, s.baseMeta())
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := s.cfg
	out := systemDTO{
		Version:       s.version,
		GoVersion:     runtime.Version(),
		SolarAPIURL:   cfg.SolarAPIURL,
		PollIntervalS: cfg.PollInterval.Seconds(),
		TZ:            cfg.TZ,
		DBPath:        cfg.DBPath,
		RetentionDays: cfg.RetentionDays,
		APIAddr:       cfg.HTTPAddr,
		StaleAfterS:   cfg.StaleAfter.Seconds(),

		DetectIntervalS:       cfg.DetectInterval.Seconds(),
		HTTPTimeoutS:          cfg.HTTPTimeout.Seconds(),
		CredentialsConfigured: s.credentialsConfigured(),
	}
	out.Device, out.Detection = s.deviceAndDetection(ctx)
	s.writeData(w, http.StatusOK, out, s.baseMeta())
}

// deviceAndDetection builds the identity and the detection state from the
// last recorded read.
//
// Without a row in the database, `device` stays null ("unknown" is not an empty
// object) and the detections are unknown with their reason. No state
// is deduced from an absent field: the states are those that were read.
func (s *Server) deviceAndDetection(ctx context.Context) (*deviceDTO, *detectionDTO) {
	record, hasRecord, err := s.store.Device(ctx)
	if err != nil {
		s.log.Error("reading device state failed", "error", err)
		return nil, detectionFrom(storage.DeviceRecord{}, false, storage.DeviceHealth{}, false, "")
	}

	// The clock and the service state live on their own single-row reading:
	// /commands/* is read on the same cycle, but it is not part of the device
	// identity, and an absent reading must not be confused with an identity
	// that was never read.
	health, hasHealth, err := s.store.DeviceHealth(ctx)
	if err != nil {
		s.log.Error("reading device health failed", "error", err)
		health, hasHealth = storage.DeviceHealth{}, false
	}

	state, err := s.store.State(ctx)
	if err != nil {
		s.log.Error("reading collector state failed", "error", err)
		state = map[string]string{}
	}

	out := detectionFrom(record, hasRecord, health, hasHealth, state[detect.StateLastError])
	if !hasRecord {
		return nil, out
	}
	return &deviceDTO{
		Model:         nullableString(record.Model),
		DeviceName:    nullableString(record.DeviceName),
		ArticleNumber: nullableString(record.ArticleNumber),
		Firmware:      nullableString(record.Firmware),
		SerialNumber:  nullableString(record.SerialNumber),
		Source:        record.Source,
		ReadAtUTC:     formatTime(record.ReadAtUTC),
	}, out
}

// detectionFrom translates the persisted rows into a detection DTO.
func detectionFrom(record storage.DeviceRecord, hasRecord bool,
	health storage.DeviceHealth, hasHealth bool, lastError string) *detectionDTO {

	out := &detectionDTO{
		LastError: lastError,
		Clock:     clockFrom(health, hasHealth),
		Service:   serviceFrom(health, hasHealth),
	}
	if !hasRecord {
		unknown := detectionResourceDTO{State: string(model.StateUnknown), Reason: detect.ReasonNoReadYet}
		out.Meter = unknown
		out.Battery = unknown
		out.InternalAPI = unknown
		out.DeviceEvents = unknown
		return out
	}

	readAt := formatTime(record.ReadAtUTC)
	out.ReadAtUTC = &readAt
	out.Meter = detectionResourceDTO{
		State:  string(record.MeterState),
		Source: record.MeterSource,
		Reason: record.MeterReason,
	}
	out.Battery = detectionResourceDTO{
		State:  string(record.BatteryState),
		Source: record.BatterySource,
		Reason: record.BatteryReason,
	}
	out.InternalAPI = detectionResourceDTO{
		State:  string(record.IdentityState),
		Source: record.Source,
		Reason: record.IdentityReason,
	}
	out.DeviceEvents = detectionResourceDTO{
		State:  string(record.DeviceEventsState),
		Source: internalapi.ActiveEventsPath,
		Reason: record.DeviceEventsReason,
	}
	return out
}

// clockFrom translates the recorded clock reading into its DTO.
//
// The source is the path itself: the reading is the answer of
// /commands/GetUTCTime, and no other endpoint can produce it. When no read has
// ever been recorded, the state is unknown, with the reason that says so, and
// neither the device time nor the drift is emitted.
func clockFrom(health storage.DeviceHealth, hasHealth bool) clockResourceDTO {
	if !hasHealth {
		return clockResourceDTO{detectionResourceDTO: detectionResourceDTO{
			State:  string(model.StateUnknown),
			Source: internalapi.UTCTimePath,
			Reason: detect.ReasonNoHealthRead,
		}}
	}

	out := clockResourceDTO{detectionResourceDTO: detectionResourceDTO{
		State:  string(health.ClockState),
		Source: internalapi.UTCTimePath,
		Reason: health.ClockReason,
	}}
	// The device time is the flag of a successful read: the storage layer keeps
	// it NULL when there was none, so a zero here means "not read", never
	// "read a long time ago" — and the drift is left out with it.
	if health.ClockDeviceUTC.IsZero() {
		return out
	}

	deviceUTC := formatTime(health.ClockDeviceUTC)
	driftS := (time.Duration(health.ClockDriftMS) * time.Millisecond).Seconds()
	out.DeviceUTC = &deviceUTC
	out.DriftS = &driftS
	return out
}

// serviceFrom translates the recorded service state into its DTO.
//
// `value` is omitted, never "", when nothing was read: the state string is the
// device's answer, and an empty answer is not an answer.
func serviceFrom(health storage.DeviceHealth, hasHealth bool) serviceResourceDTO {
	if !hasHealth {
		return serviceResourceDTO{detectionResourceDTO: detectionResourceDTO{
			State:  string(model.StateUnknown),
			Source: internalapi.ServiceStatePath,
			Reason: detect.ReasonNoHealthRead,
		}}
	}

	return serviceResourceDTO{
		detectionResourceDTO: detectionResourceDTO{
			State:  string(health.ServiceState),
			Source: internalapi.ServiceStatePath,
			Reason: health.ServiceReason,
		},
		Value: nullableString(health.ServiceValue),
	}
}

// detectionForCapabilities restores the detection as it was persisted:
// the capabilities and the /system endpoint describe the same state, without recomputing it.
func (s *Server) detectionForCapabilities(ctx context.Context) model.Detection {
	record, hasRecord, err := s.store.Device(ctx)
	if err != nil {
		s.log.Error("reading device state failed", "error", err)
		return unknownDetection(detect.ReasonNoReadYet)
	}
	return detectionModel(record, hasRecord)
}

// detectionModel translates the persisted record into the model detection.
func detectionModel(record storage.DeviceRecord, hasRecord bool) model.Detection {
	if !hasRecord {
		return unknownDetection(detect.ReasonNoReadYet)
	}
	return model.Detection{
		Identity: model.ResourceDetection{
			State: record.IdentityState, Source: record.Source, Reason: record.IdentityReason,
		},
		Meter: model.ResourceDetection{
			State: record.MeterState, Source: record.MeterSource, Reason: record.MeterReason,
		},
		Battery: model.ResourceDetection{
			State: record.BatteryState, Source: record.BatterySource, Reason: record.BatteryReason,
		},
		DeviceEvents: model.ResourceDetection{
			State: record.DeviceEventsState, Source: internalapi.ActiveEventsPath, Reason: record.DeviceEventsReason,
		},
	}
}

func unknownDetection(reason string) model.Detection {
	r := model.ResourceDetection{State: model.StateUnknown, Reason: reason}
	return model.Detection{Identity: r, Meter: r, Battery: r, DeviceEvents: r}
}

// nullableString returns nil for an empty string: "not found in the
// response" must arrive as null on the client side, never as "".
func nullableString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return new(s)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m := s.baseMeta()

	sm, err := s.store.LatestSample(ctx)
	if errors.Is(err, storage.ErrNoData) {
		// Empty database: a legitimate state of a fresh installation. The body
		// carries data=null and error.code=no_data, with a 200 status so the client
		// can distinguish "no data yet" from an API failure.
		s.writeEnvelope(w, http.StatusOK, envelope{
			Data:  nil,
			Meta:  m,
			Error: &apiError{Code: CodeNoData, Message: "no sample recorded"},
		})
		return
	}
	if err != nil {
		s.internalError(w, "reading the latest sample", err)
		return
	}

	s.applyFreshness(m, sm.TS)

	// The day and year energies are derived from our own stored total, because the
	// measured fields are null on firmware that does not publish those counters
	// (docs/UNKNOWNS.md #13). The measured values keep their place: the client
	// prefers them when they exist.
	dto := sampleToDTO(sm)
	dto.EnergyDerived = s.derivedEnergyFromTotal(ctx, sm)
	s.writeData(w, http.StatusOK, dto, m)
}

func (s *Server) handleTimeSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	from, okFrom, err := timeParam(q, "from")
	if err != nil || !okFrom {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			"from parameter required and in RFC3339 format")
		return
	}
	to, okTo, err := timeParam(q, "to")
	if err != nil || !okTo {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			"to parameter required and in RFC3339 format")
		return
	}
	if !to.After(from) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to must be strictly after from")
		return
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			fmt.Sprintf("maximum range: %d days", maxRangeDays))
		return
	}

	step, err := parseStep(q.Get("step"), to.Sub(from))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidStep, err.Error())
		return
	}

	// The representation is decided after every range and step check, so a bad
	// query stays a JSON 400 even when the client asks for CSV.
	format, ok := exportFormat(q)
	if !ok {
		s.writeError(w, http.StatusBadRequest, CodeInvalidFormat, invalidFormatMessage)
		return
	}

	buckets, err := s.store.TimeSeries(ctx, storage.TimeSeriesQuery{From: from, To: to, Step: step})
	if err != nil {
		s.internalError(w, "reading the time series", err)
		return
	}

	if format == "csv" {
		s.writeCSV(w, exportFilename("timeseries", from, to), timeSeriesCSVHeader,
			timeSeriesCSVRows(bucketsToDTO(buckets)))
		return
	}

	m := s.baseMeta()
	m.StepS = new(step.Seconds())
	s.freshnessMeta(ctx, m)
	s.writeData(w, http.StatusOK, bucketsToDTO(buckets), m)
}

// handleDailyEnergy exposes the energy per local day.
//
// The computation groups by UTC hour then by local date (internal/storage):
// a local day therefore appears only if the requested window contains at
// least one sample of that day, and the returned value is then the
// observed maximum, which may be partial if the window covers only part
// of the day. The 23 h and 25 h days are counted correctly.
func (s *Server) handleDailyEnergy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	loc := s.store.Location()

	today := s.now().In(loc)
	from := time.Date(today.Year(), today.Month(), today.Day()-(defaultDailyDays-1), 0, 0, 0, 0, loc)
	to := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, loc)

	if v, ok, err := timeParam(q, "from"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "from parameter expected in RFC3339 format")
		return
	} else if ok {
		from = v
	}
	if v, ok, err := timeParam(q, "to"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to parameter expected in RFC3339 format")
		return
	} else if ok {
		to = v
	}
	if !to.After(from) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to must be strictly after from")
		return
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			fmt.Sprintf("maximum range: %d days", maxRangeDays))
		return
	}

	format, ok := exportFormat(q)
	if !ok {
		s.writeError(w, http.StatusBadRequest, CodeInvalidFormat, invalidFormatMessage)
		return
	}

	measured, err := s.store.DailyEnergy(ctx, from, to)
	if err != nil {
		s.internalError(w, "reading daily energy", err)
		return
	}
	// The total is recorded on every sample, so the day's energy can be measured
	// from it when the device reports no E_Day — which is always the case on the
	// reference firmware (docs/UNKNOWNS.md #13).
	derived, err := s.store.DailyEnergyFromTotal(ctx, from, to, s.now(), derivedBaselineTolerance)
	if err != nil {
		s.internalError(w, "deriving daily energy", err)
		return
	}

	// A range that holds nothing at all keeps answering `[]`: the empty state is
	// stated once, not once per day. As soon as anything was recorded, every day
	// of the range is published — with its value, or with the reason it has none.
	if !anyDerivedValue(derived) && len(measured) == 0 {
		derived = nil
	}

	if format == "csv" {
		s.writeCSV(w, exportFilename("energy-daily", from, to), dailyEnergyCSVHeader,
			dailyEnergyCSVRows(dailyToDTO(measured, derived)))
		return
	}

	m := s.baseMeta()
	s.freshnessMeta(ctx, m)
	s.writeData(w, http.StatusOK, dailyToDTO(measured, derived), m)
}

// anyDerivedValue reports whether at least one day of the range could be
// measured from the total.
func anyDerivedValue(days []storage.DailyDerivedEnergy) bool {
	for _, d := range days {
		if d.Wh != nil {
			return true
		}
	}
	return false
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := defaultEventsLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxEventsLimit {
			s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
				fmt.Sprintf("limit must be an integer between 1 and %d", maxEventsLimit))
			return
		}
		limit = n
	}

	query := storage.EventsQuery{Limit: limit}
	if v, ok, err := timeParam(q, "from"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "from parameter expected in RFC3339 format")
		return
	} else if ok {
		query.From = v
	}
	if v, ok, err := timeParam(q, "to"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to parameter expected in RFC3339 format")
		return
	} else if ok {
		query.To = v
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.To.After(query.From) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to must be strictly after from")
		return
	}

	format, ok := exportFormat(q)
	if !ok {
		s.writeError(w, http.StatusBadRequest, CodeInvalidFormat, invalidFormatMessage)
		return
	}

	events, err := s.store.Events(ctx, query)
	if err != nil {
		s.internalError(w, "reading events", err)
		return
	}

	if format == "csv" {
		// An absent bound stays absent: exportFilename renders it `all` rather
		// than inventing an instant.
		s.writeCSV(w, exportFilename("events", query.From, query.To), eventsCSVHeader,
			eventsCSVRows(eventsToDTO(events)))
		return
	}
	s.writeData(w, http.StatusOK, eventsToDTO(events), s.baseMeta())
}

// handleCapabilities exposes what the supervision can return.
//
// The decision relies on the last persisted detection (internal/detect):
// "unknown" is not "absent", and an unavailable capability carries a
// reason and no numeric value.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	caps := model.Capabilities(s.detectionForCapabilities(r.Context()))
	s.writeData(w, http.StatusOK, capabilitiesToDTO(caps), s.baseMeta())
}

// ---------------------------------------------------------------------------
// Envelope, meta, errors
// ---------------------------------------------------------------------------

func (s *Server) baseMeta() *meta {
	return &meta{GeneratedAt: formatTime(s.now())}
}

// applyFreshness fills sampled_at / stale / stale_after_s for a known
// measurement instant.
func (s *Server) applyFreshness(m *meta, sampledAt time.Time) {
	ts := formatTime(sampledAt)
	m.SampledAt = &ts
	stale := s.now().Sub(sampledAt) > s.cfg.StaleAfter
	m.Stale = &stale
	after := s.cfg.StaleAfter.Seconds()
	m.StaleAfterS = &after
}

// freshnessMeta does the same from the latest sample in the database, without
// failing if the database is empty (the fields then stay absent).
func (s *Server) freshnessMeta(ctx context.Context, m *meta) {
	sm, err := s.store.LatestSample(ctx)
	if errors.Is(err, storage.ErrNoData) {
		return
	}
	if err != nil {
		s.log.Error("reading the latest sample failed", "error", err)
		return
	}
	s.applyFreshness(m, sm.TS)
}

func (s *Server) writeData(w http.ResponseWriter, status int, data any, m *meta) {
	s.writeEnvelope(w, status, envelope{Data: data, Meta: m})
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeEnvelope(w, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}

func (s *Server) internalError(w http.ResponseWriter, what string, err error) {
	s.log.Error("internal error", "operation", what, "error", err)
	s.writeError(w, http.StatusInternalServerError, CodeInternal, "internal error")
}

func (s *Server) writeEnvelope(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Every JSON response goes through here; the static assets do not. Without
	// this header a browser may serve a cached body and a diagnostic re-test
	// would show a stale verdict. The 10 s cache of /connectivity is the
	// server's own and says `cached: true`; this header keeps the browser from
	// hiding even that.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		// The header is already sent: all that is left is the trace in the logs.
		s.log.Error("writing the response failed", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// timeParam reads an RFC3339 timestamp. The second return indicates its presence.
func timeParam(q url.Values, key string) (time.Time, bool, error) {
	raw := q.Get(key)
	if raw == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, true, err
	}
	return t.UTC(), true, nil
}

// parseStep resolves the step parameter, choosing the step for "auto".
func parseStep(raw string, span time.Duration) (time.Duration, error) {
	if raw == "" || raw == "auto" {
		for _, opt := range stepOptions {
			if span/opt.d <= autoMaxBuckets {
				return opt.d, nil
			}
		}
		return stepOptions[len(stepOptions)-1].d, nil
	}
	for _, opt := range stepOptions {
		if raw == opt.name {
			return opt.d, nil
		}
	}
	names := make([]string, 0, len(stepOptions)+1)
	names = append(names, "auto")
	for _, opt := range stepOptions {
		names = append(names, opt.name)
	}
	return 0, fmt.Errorf("unknown step %q: accepted values %s", raw, strings.Join(names, ", "))
}
