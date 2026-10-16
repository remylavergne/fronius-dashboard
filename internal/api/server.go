package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/detect"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

const (
	// autoMaxBuckets borne le nombre de points renvoyés par step=auto.
	autoMaxBuckets = 1500
	// maxRangeDays borne l'amplitude d'une requête de série temporelle.
	maxRangeDays = 366
	// defaultEventsLimit / maxEventsLimit bornent la pagination des événements.
	defaultEventsLimit = 100
	maxEventsLimit     = 500
	// defaultDailyDays est la profondeur par défaut de l'énergie journalière.
	defaultDailyDays = 30
)

// stepOptions liste les pas acceptés, du plus fin au plus grossier.
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

// Server sert l'API REST.
type Server struct {
	store   *storage.Store
	cfg     config.Config
	log     *slog.Logger
	version string
	started time.Time
	now     func() time.Time
}

// New construit le serveur d'API.
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
	}
}

// Handler retourne le routeur, éventuellement enveloppé du service statique.
func (s *Server) Handler() http.Handler {
	api := s.apiMux()
	if s.cfg.WebDir == "" {
		return api
	}
	return s.withStaticWeb(api)
}

func (s *Server) apiMux() http.Handler {
	mux := http.NewServeMux()

	// Go 1.22 sélectionne le motif le plus spécifique : « GET /x » gagne pour un
	// GET, tandis que « /x » capte les autres méthodes et produit un 405 JSON.
	register := func(path string, h http.HandlerFunc) {
		mux.HandleFunc("GET "+path, h)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", http.MethodGet)
			s.writeError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
				fmt.Sprintf("méthode %s non autorisée sur %s", r.Method, path))
		})
	}

	register("/api/v1/health", s.handleHealth)
	register("/api/v1/system", s.handleSystem)
	register("/api/v1/live", s.handleLive)
	register("/api/v1/timeseries", s.handleTimeSeries)
	register("/api/v1/energy/daily", s.handleDailyEnergy)
	register("/api/v1/events", s.handleEvents)
	register("/api/v1/capabilities", s.handleCapabilities)

	// Tout le reste : 404 JSON.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, http.StatusNotFound, CodeNotFound,
			fmt.Sprintf("chemin inconnu : %s", r.URL.Path))
	})

	return mux
}

// withStaticWeb sert les assets du frontend, avec repli SPA sur index.html.
//
// Aucun go:embed en phase 1 : l'intégration des assets est prévue en phase 5
// (ADR 0007).
func (s *Server) withStaticWeb(api http.Handler) http.Handler {
	fileServer := http.FileServer(http.Dir(s.cfg.WebDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		cleaned := filepath.Clean("/" + r.URL.Path)
		candidate := filepath.Join(s.cfg.WebDir, cleaned)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		index := filepath.Join(s.cfg.WebDir, "index.html")
		if _, err := os.Stat(index); err != nil {
			api.ServeHTTP(w, r) // sans assets : comportement API (404 JSON)
			return
		}
		http.ServeFile(w, r, index)
	})
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
		// Le message d'erreur brut peut contenir un chemin de fichier : il reste
		// dans les logs, pas dans la réponse.
		s.log.Error("health: comptage des échantillons impossible", "error", err)
		out.DB = dbHealthDTO{OK: false, Error: "lecture de la base impossible"}
		out.Status = "error"
	} else {
		out.DB = dbHealthDTO{OK: true}
		out.Collector.SamplesTotal = count
	}

	degraded := false
	if state, err := s.store.State(ctx); err != nil {
		s.log.Error("health: lecture de l'état du collecteur impossible", "error", err)
	} else {
		out.Collector.LastSuccessUTC = state["last_success_utc"]
		out.Collector.LastError = state["last_error"]
	}

	if run, ok, err := s.store.LatestRun(ctx); err != nil {
		s.log.Error("health: lecture du dernier cycle impossible", "error", err)
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
	}
	out.Device, out.Detection = s.deviceAndDetection(ctx)
	s.writeData(w, http.StatusOK, out, s.baseMeta())
}

// deviceAndDetection construit l'identité et l'état des détections à partir de
// la dernière lecture enregistrée.
//
// Sans ligne en base, `device` reste null (« non déterminé » n'est pas un objet
// vide) et les quatre détections valent inconnu avec leur motif. Aucun état
// n'est déduit d'un champ absent : les états sont ceux qui ont été lus.
func (s *Server) deviceAndDetection(ctx context.Context) (*deviceDTO, *detectionDTO) {
	record, hasRecord, err := s.store.Device(ctx)
	if err != nil {
		s.log.Error("lecture de l'état de l'appareil impossible", "error", err)
		return nil, detectionFrom(storage.DeviceRecord{}, false, "")
	}

	state, err := s.store.State(ctx)
	if err != nil {
		s.log.Error("lecture de l'état du collecteur impossible", "error", err)
		state = map[string]string{}
	}

	out := detectionFrom(record, hasRecord, state[detect.StateLastError])
	if !hasRecord {
		return nil, out
	}
	return &deviceDTO{
		Model:        nullableString(record.Model),
		Firmware:     nullableString(record.Firmware),
		SerialNumber: nullableString(record.SerialNumber),
		Source:       record.Source,
		ReadAtUTC:    formatTime(record.ReadAtUTC),
	}, out
}

// detectionFrom traduit un enregistrement en DTO de détection.
func detectionFrom(record storage.DeviceRecord, hasRecord bool, lastError string) *detectionDTO {
	out := &detectionDTO{LastError: lastError}
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

// detectionForCapabilities restaure la détection telle qu'elle a été persistée :
// les capacités et l'endpoint /system décrivent le même état, sans le recalculer.
func (s *Server) detectionForCapabilities(ctx context.Context) model.Detection {
	record, hasRecord, err := s.store.Device(ctx)
	if err != nil {
		s.log.Error("lecture de l'état de l'appareil impossible", "error", err)
		return unknownDetection(detect.ReasonNoReadYet)
	}
	return detectionModel(record, hasRecord)
}

// detectionModel traduit l'enregistrement persisté en détection du modèle.
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

// nullableString retourne nil pour une chaîne vide : « non trouvé dans la
// réponse » doit arriver en null côté client, jamais en "".
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
		// Base vide : état légitime d'une installation neuve. Le corps porte
		// data=null et error.code=no_data, avec un statut 200 pour que le client
		// puisse distinguer « pas encore de donnée » d'une panne d'API.
		s.writeEnvelope(w, http.StatusOK, envelope{
			Data:  nil,
			Meta:  m,
			Error: &apiError{Code: CodeNoData, Message: "aucun échantillon enregistré"},
		})
		return
	}
	if err != nil {
		s.internalError(w, "lecture du dernier échantillon", err)
		return
	}

	s.applyFreshness(m, sm.TS)
	s.writeData(w, http.StatusOK, sampleToDTO(sm), m)
}

func (s *Server) handleTimeSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	from, okFrom, err := timeParam(q, "from")
	if err != nil || !okFrom {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			"paramètre from requis et au format RFC3339")
		return
	}
	to, okTo, err := timeParam(q, "to")
	if err != nil || !okTo {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			"paramètre to requis et au format RFC3339")
		return
	}
	if !to.After(from) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to doit être strictement postérieur à from")
		return
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			fmt.Sprintf("plage maximale : %d jours", maxRangeDays))
		return
	}

	step, err := parseStep(q.Get("step"), to.Sub(from))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidStep, err.Error())
		return
	}

	buckets, err := s.store.TimeSeries(ctx, storage.TimeSeriesQuery{From: from, To: to, Step: step})
	if err != nil {
		s.internalError(w, "lecture de la série temporelle", err)
		return
	}

	m := s.baseMeta()
	m.StepS = new(step.Seconds())
	s.freshnessMeta(ctx, m)
	s.writeData(w, http.StatusOK, bucketsToDTO(buckets), m)
}

// handleDailyEnergy expose l'énergie par journée locale.
//
// Le calcul regroupe par heure UTC puis par date locale (internal/storage) :
// une journée locale n'apparaît donc que si la fenêtre demandée contient au
// moins un échantillon de cette journée, et la valeur renvoyée est alors le
// maximum observé, qui peut être partiel si la fenêtre ne couvre qu'une partie
// de la journée. Les journées de 23 h et 25 h sont correctement comptées.
func (s *Server) handleDailyEnergy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	loc := s.store.Location()

	today := s.now().In(loc)
	from := time.Date(today.Year(), today.Month(), today.Day()-(defaultDailyDays-1), 0, 0, 0, 0, loc)
	to := time.Date(today.Year(), today.Month(), today.Day()+1, 0, 0, 0, 0, loc)

	if v, ok, err := timeParam(q, "from"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "paramètre from au format RFC3339 attendu")
		return
	} else if ok {
		from = v
	}
	if v, ok, err := timeParam(q, "to"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "paramètre to au format RFC3339 attendu")
		return
	} else if ok {
		to = v
	}
	if !to.After(from) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to doit être strictement postérieur à from")
		return
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
			fmt.Sprintf("plage maximale : %d jours", maxRangeDays))
		return
	}

	daily, err := s.store.DailyEnergy(ctx, from, to)
	if err != nil {
		s.internalError(w, "lecture de l'énergie journalière", err)
		return
	}

	m := s.baseMeta()
	s.freshnessMeta(ctx, m)
	s.writeData(w, http.StatusOK, dailyToDTO(daily), m)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := defaultEventsLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxEventsLimit {
			s.writeError(w, http.StatusBadRequest, CodeInvalidRange,
				fmt.Sprintf("limit doit être un entier entre 1 et %d", maxEventsLimit))
			return
		}
		limit = n
	}

	query := storage.EventsQuery{Limit: limit}
	if v, ok, err := timeParam(q, "from"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "paramètre from au format RFC3339 attendu")
		return
	} else if ok {
		query.From = v
	}
	if v, ok, err := timeParam(q, "to"); err != nil {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "paramètre to au format RFC3339 attendu")
		return
	} else if ok {
		query.To = v
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.To.After(query.From) {
		s.writeError(w, http.StatusBadRequest, CodeInvalidRange, "to doit être strictement postérieur à from")
		return
	}

	events, err := s.store.Events(ctx, query)
	if err != nil {
		s.internalError(w, "lecture des événements", err)
		return
	}
	s.writeData(w, http.StatusOK, eventsToDTO(events), s.baseMeta())
}

// handleCapabilities expose ce que la supervision peut restituer.
//
// La décision repose sur la dernière détection persistée (internal/detect) :
// « non déterminé » n'est pas « absent », et une capacité indisponible porte un
// motif et aucune valeur numérique.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	caps := model.Capabilities(s.detectionForCapabilities(r.Context()))
	s.writeData(w, http.StatusOK, capabilitiesToDTO(caps), s.baseMeta())
}

// ---------------------------------------------------------------------------
// Enveloppe, méta, erreurs
// ---------------------------------------------------------------------------

func (s *Server) baseMeta() *meta {
	return &meta{GeneratedAt: formatTime(s.now())}
}

// applyFreshness renseigne sampled_at / stale / stale_after_s pour un instant
// de mesure connu.
func (s *Server) applyFreshness(m *meta, sampledAt time.Time) {
	ts := formatTime(sampledAt)
	m.SampledAt = &ts
	stale := s.now().Sub(sampledAt) > s.cfg.StaleAfter
	m.Stale = &stale
	after := s.cfg.StaleAfter.Seconds()
	m.StaleAfterS = &after
}

// freshnessMeta fait de même à partir du dernier échantillon en base, sans
// échouer si la base est vide (les champs restent alors absents).
func (s *Server) freshnessMeta(ctx context.Context, m *meta) {
	sm, err := s.store.LatestSample(ctx)
	if errors.Is(err, storage.ErrNoData) {
		return
	}
	if err != nil {
		s.log.Error("lecture du dernier échantillon impossible", "error", err)
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
	s.log.Error("erreur interne", "operation", what, "error", err)
	s.writeError(w, http.StatusInternalServerError, CodeInternal, "erreur interne")
}

func (s *Server) writeEnvelope(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		// L'en-tête est déjà envoyé : il ne reste que la trace dans les logs.
		s.log.Error("écriture de la réponse impossible", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// timeParam lit un horodatage RFC3339. Le second retour indique sa présence.
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

// parseStep résout le paramètre step, en choisissant le pas pour « auto ».
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
	return 0, fmt.Errorf("step %q inconnu : valeurs admises %s", raw, strings.Join(names, ", "))
}
