package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

type respEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Meta  json.RawMessage `json:"meta"`
	Error *apiError       `json:"error"`
}

type respMeta struct {
	GeneratedAt string   `json:"generated_at"`
	SampledAt   *string  `json:"sampled_at"`
	Stale       *bool    `json:"stale"`
	StaleAfterS *float64 `json:"stale_after_s"`
	StepS       *float64 `json:"step_s"`
}

func testConfig() config.Config {
	return config.Config{
		SolarAPIURL:   "http://127.0.0.1:8081",
		HTTPAddr:      "127.0.0.1:8080",
		DBPath:        "./data/fronius.db",
		TZ:            "Europe/Brussels",
		PollInterval:  5 * time.Second,
		PollJitter:    500 * time.Millisecond,
		HTTPTimeout:   5 * time.Second,
		RetentionDays: 0,
		LogLevel:      "error",
		LogFormat:     "text",
		StaleAfter:    30 * time.Second,
	}
}

func newTestServer(t *testing.T, tweak func(*config.Config)) (*Server, *httptest.Server, *storage.Store) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), loc)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := testConfig()
	if tweak != nil {
		tweak(&cfg)
	}
	srv := New(store, cfg, "test-version", slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return srv, httpSrv, store
}

func doRequest(t *testing.T, srv *httptest.Server, method, path string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, resp.Header, body
}

func decodeEnvelope(t *testing.T, body []byte) respEnvelope {
	t.Helper()
	var env respEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("Unmarshal(%s): %v", body, err)
	}
	return env
}

func decodeMeta(t *testing.T, env respEnvelope) respMeta {
	t.Helper()
	var m respMeta
	if err := json.Unmarshal(env.Meta, &m); err != nil {
		t.Fatalf("Unmarshal(meta): %v", err)
	}
	if m.GeneratedAt == "" {
		t.Fatal("meta.generated_at manquant")
	}
	if _, err := time.Parse(apiTimeLayout, m.GeneratedAt); err != nil {
		t.Fatalf("meta.generated_at = %q : %v", m.GeneratedAt, err)
	}
	return m
}

func insertSample(t *testing.T, store *storage.Store, ts time.Time, pv float64, eDay float64) {
	t.Helper()
	sm := model.Sample{
		TS:            ts,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		Mode:          "produce-only",
		PowerPVW:      new(pv),
		EnergyDayWh:   new(eDay),
		EnergyYearWh:  new(float64(1000)),
		EnergyTotalWh: new(float64(2000)),
		Inverters:     []model.InverterSample{{ID: "1", DT: 1, PowerW: new(pv)}},
	}
	if err := store.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample: %v", err)
	}
}

func TestUnknownPathAndMethod(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/inconnu")
	if code != http.StatusNotFound {
		t.Fatalf("chemin inconnu : status = %d", code)
	}
	env := decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeNotFound {
		t.Fatalf("corps = %s", body)
	}

	// Une méthode non autorisée sur un chemin connu.
	code, hdr, body := doRequest(t, srv, http.MethodPost, "/api/v1/health")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/health : status = %d", code)
	}
	if allow := hdr.Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q", allow)
	}
	env = decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeMethodNotAllowed {
		t.Fatalf("corps = %s", body)
	}
	// Une réponse d'erreur ne porte pas de champ data.
	if strings.Contains(string(body), `"data"`) {
		t.Fatalf("l'erreur ne doit pas porter de champ data : %s", body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestHealth(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	// Aucune donnée, aucun cycle : état nominal.
	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var health healthDTO
	env := decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "ok" || !health.DB.OK || health.Version != "test-version" {
		t.Fatalf("health = %+v", health)
	}
	if health.Collector.SamplesTotal != 0 || health.Collector.LastError != "" {
		t.Fatalf("collector = %+v", health.Collector)
	}
	decodeMeta(t, env)

	// Échantillons + cycle réussi.
	insertSample(t, store, time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), 100, 500)
	if err := store.SetState(ctx, "last_success_utc", "2026-10-16T07:00:00Z"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := store.InsertRun(ctx, storage.RunRecord{
		StartedUTC: time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC), DurationMS: 42, OK: true, HTTPStatus: 200,
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "ok" {
		t.Fatalf("status = %q", health.Status)
	}
	if health.Collector.SamplesTotal != 1 || health.Collector.LastRunMS != 42 {
		t.Fatalf("collector = %+v", health.Collector)
	}
	if health.Collector.LastSuccessUTC != "2026-10-16T07:00:00Z" {
		t.Fatalf("last_success_utc = %q", health.Collector.LastSuccessUTC)
	}

	// Un cycle en échec dégrade l'état sans faire échouer l'endpoint.
	if err := store.InsertRun(ctx, storage.RunRecord{
		StartedUTC: time.Date(2026, 10, 16, 7, 5, 0, 0, time.UTC), DurationMS: 5001, OK: false,
		HTTPStatus: 503, ErrorKind: "server", Error: "service indisponible",
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := store.SetState(ctx, "last_error", "service indisponible"); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/health")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &health); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if health.Status != "degraded" {
		t.Fatalf("status = %q, attendu degraded", health.Status)
	}
	if health.Collector.LastError == "" || health.Collector.LastRunMS != 5001 {
		t.Fatalf("collector = %+v", health.Collector)
	}
	if health.UptimeS < 0 {
		t.Fatalf("uptime_s = %d", health.UptimeS)
	}
	_ = srv
}

func TestSystemExposesNoDeviceAndNoSecret(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	_, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/system")
	env := decodeEnvelope(t, body)

	var sys systemDTO
	if err := json.Unmarshal(env.Data, &sys); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if sys.Device != nil {
		t.Fatalf("device doit rester null en phase 1 : %+v", sys.Device)
	}
	if sys.SolarAPIURL != "http://127.0.0.1:8081" || sys.TZ != "Europe/Brussels" ||
		sys.PollIntervalS != 5 || sys.StaleAfterS != 30 || sys.APIAddr != "127.0.0.1:8080" {
		t.Fatalf("système = %+v", sys)
	}
	if sys.GoVersion == "" || sys.Version != "test-version" {
		t.Fatalf("versions = %+v", sys)
	}
	decodeMeta(t, env)

	// Aucun mot de passe, jeton ou identifiant ne doit apparaître.
	lower := strings.ToLower(string(body))
	for _, forbidden := range []string{"password", "secret", "token", "authorization", "credential"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("la réponse système contient %q : %s", forbidden, body)
		}
	}
}

func TestLiveWithEmptyStoreReturnsNoData(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":null`) {
		t.Fatalf("data doit être null : %s", body)
	}
	env := decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeNoData {
		t.Fatalf("erreur = %+v", env.Error)
	}
	decodeMeta(t, env)
}

func TestLiveAndStaleFlag(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)

	deviceTS := time.Date(2026, 10, 16, 7, 17, 43, 0, time.UTC)
	insertSample(t, store, deviceTS, 1902.4, 12345.6)

	// Horloge figée 1 s après la mesure : données fraîches.
	srv.now = func() time.Time { return deviceTS.Add(time.Second) }

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var live sampleDTO
	if err := json.Unmarshal(env.Data, &live); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if live.PowerPVW == nil || *live.PowerPVW != 1902.4 {
		t.Fatalf("power_pv_w = %v", live.PowerPVW)
	}
	if live.EnergyDayWh == nil || *live.EnergyDayWh != 12345.6 {
		t.Fatalf("energy_day_wh = %v", live.EnergyDayWh)
	}
	if live.TS != "2026-10-16T07:17:43.000Z" {
		t.Fatalf("ts = %q", live.TS)
	}
	// Les grandeurs non mesurées restent explicitement nulles.
	for name, v := range map[string]*float64{
		"power_grid_w":     live.PowerGridW,
		"power_load_w":     live.PowerLoadW,
		"power_battery_w":  live.PowerBatteryW,
		"rel_autonomy_pct": live.RelAutonomyPct,
	} {
		if v != nil {
			t.Fatalf("%s = %v, attendu null", name, *v)
		}
	}
	if len(live.Inverters) != 1 || live.Inverters[0].ID != "1" {
		t.Fatalf("inverters = %+v", live.Inverters)
	}

	m := decodeMeta(t, env)
	if m.Stale == nil || *m.Stale {
		t.Fatalf("stale = %v, attendu false", m.Stale)
	}
	if m.SampledAt == nil || *m.SampledAt != "2026-10-16T07:17:43.000Z" {
		t.Fatalf("sampled_at = %v", m.SampledAt)
	}
	if m.StaleAfterS == nil || *m.StaleAfterS != 30 {
		t.Fatalf("stale_after_s = %v", m.StaleAfterS)
	}

	// Même base, horloge 60 s plus tard (seuil configuré à 30 s).
	srv.now = func() time.Time { return deviceTS.Add(time.Minute) }
	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/live")
	env = decodeEnvelope(t, body)
	m = decodeMeta(t, env)
	if m.Stale == nil || !*m.Stale {
		t.Fatalf("stale = %v, attendu true", m.Stale)
	}
}

func TestTimeSeriesValidation(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)
	base := "2026-10-14T00:00:00Z"
	end := "2026-10-16T00:00:00Z"

	cases := []struct {
		name       string
		query      string
		wantStatus int
		wantCode   string
	}{
		{"from manquant", "?to=" + end, http.StatusBadRequest, CodeInvalidRange},
		{"to manquant", "?from=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"from illisible", "?from=hier&to=" + end, http.StatusBadRequest, CodeInvalidRange},
		{"plage inversée", "?from=" + end + "&to=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"plage nulle", "?from=" + base + "&to=" + base, http.StatusBadRequest, CodeInvalidRange},
		{"plage trop large", "?from=2020-01-01T00:00:00Z&to=2026-01-01T00:00:00Z", http.StatusBadRequest, CodeInvalidRange},
		{"pas inconnu", "?from=" + base + "&to=" + end + "&step=17s", http.StatusBadRequest, CodeInvalidStep},
		{"pas vide explicite", "?from=" + base + "&to=" + end + "&step=0s", http.StatusBadRequest, CodeInvalidStep},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/timeseries"+tc.query)
			if code != tc.wantStatus {
				t.Fatalf("status = %d, corps %s", code, body)
			}
			env := decodeEnvelope(t, body)
			if env.Error == nil || env.Error.Code != tc.wantCode {
				t.Fatalf("erreur = %+v, corps %s", env.Error, body)
			}
		})
	}
}

func TestTimeSeriesReturnsBucketsAndEmptyRange(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)

	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	for i := range 6 {
		insertSample(t, store, base.Add(time.Duration(i)*10*time.Minute), float64(100*(i+1)), float64(i))
	}

	from := base.Format(time.RFC3339)
	to := base.Add(time.Hour).Format(time.RFC3339)
	code, _, body := doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/timeseries?from="+from+"&to="+to+"&step=1h")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env := decodeEnvelope(t, body)
	var buckets []bucketDTO
	if err := json.Unmarshal(env.Data, &buckets); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("buckets = %d, attendu 1", len(buckets))
	}
	b := buckets[0]
	if b.Count != 6 || b.Missing != 0 {
		t.Fatalf("bucket = %+v", b)
	}
	if b.PowerPVAvgW == nil || *b.PowerPVAvgW != 350 {
		t.Fatalf("power_pv_avg_w = %v, attendu 350", b.PowerPVAvgW)
	}
	if b.PowerPVMinW == nil || *b.PowerPVMinW != 100 || b.PowerPVMaxW == nil || *b.PowerPVMaxW != 600 {
		t.Fatalf("min/max = %v / %v", b.PowerPVMinW, b.PowerPVMaxW)
	}
	if b.Start != "2026-10-14T10:00:00.000Z" {
		t.Fatalf("start = %q", b.Start)
	}
	decodeMeta(t, env)

	// Plage sans donnée : tableau vide, statut 200, jamais null.
	emptyFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	emptyTo := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/timeseries?from="+emptyFrom+"&to="+emptyTo+"&step=1h")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":[]`) {
		t.Fatalf("une plage vide doit produire un tableau vide : %s", body)
	}
}

// TestTimeSeriesExposesResolvedStep : le client ne peut pas déduire le pas de
// façon fiable (un bucket manquant fausse l'écart entre deux points), le
// serveur doit donc le lui donner.
func TestTimeSeriesExposesResolvedStep(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	insertSample(t, store, base, 100, 1)

	cases := []struct {
		step string
		want float64
	}{
		{"1h", 3600},
		{"5m", 300},
		{"auto", 60}, // 1 minute sur une fenêtre d'une heure
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			code, _, body := doRequest(t, httpSrv, http.MethodGet,
				"/api/v1/timeseries?from="+base.Format(time.RFC3339)+"&to="+base.Add(time.Hour).Format(time.RFC3339)+"&step="+tc.step)
			if code != http.StatusOK {
				t.Fatalf("status = %d (%s)", code, body)
			}
			env := decodeEnvelope(t, body)
			m := decodeMeta(t, env)
			if m.StepS == nil {
				t.Fatal("meta.step_s manquant")
			}
			if *m.StepS != tc.want {
				t.Fatalf("step_s = %v, attendu %v", *m.StepS, tc.want)
			}
		})
	}
}

func TestParseStepAutoBounded(t *testing.T) {
	if got, err := parseStep("17s", time.Hour); err == nil || got != 0 {
		t.Fatalf("parseStep(17s) = (%v, %v)", got, err)
	}

	cases := []struct {
		span time.Duration
		want time.Duration
	}{
		{time.Hour, time.Minute},
		{24 * time.Hour, time.Minute},
		{2 * 24 * time.Hour, 5 * time.Minute},
		{7 * 24 * time.Hour, 15 * time.Minute},
		{30 * 24 * time.Hour, time.Hour},
		{366 * 24 * time.Hour, 24 * time.Hour},
	}
	for _, tc := range cases {
		got, err := parseStep("auto", tc.span)
		if err != nil {
			t.Fatalf("parseStep(auto, %v): %v", tc.span, err)
		}
		if got != tc.want {
			t.Fatalf("span %v : pas = %v, attendu %v", tc.span, got, tc.want)
		}
		if buckets := tc.span / got; buckets > autoMaxBuckets {
			t.Fatalf("span %v : %d buckets dépasse la borne %d", tc.span, buckets, autoMaxBuckets)
		}
	}

	// Chaque pas nommé est accepté et résolu à sa durée.
	for _, opt := range stepOptions {
		got, err := parseStep(opt.name, time.Hour)
		if err != nil || got != opt.d {
			t.Fatalf("parseStep(%q) = (%v, %v)", opt.name, got, err)
		}
	}
}

func TestDailyEnergyEndpoint(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)

	// 14 et 15 octobre locaux, avec une remise à zéro du compteur entre les deux.
	insertSample(t, store, time.Date(2026, 10, 13, 23, 0, 0, 0, time.UTC), 500, 20000) // 14/10 01:00 local
	insertSample(t, store, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), 900, 20000) // 14/10 14:00 local
	insertSample(t, store, time.Date(2026, 10, 14, 22, 30, 0, 0, time.UTC), 0, 0)      // 15/10 00:30 local
	insertSample(t, store, time.Date(2026, 10, 14, 23, 30, 0, 0, time.UTC), 10, 1000)  // 15/10 01:30 local

	srv.now = func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/energy/daily")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env := decodeEnvelope(t, body)
	var daily []dailyEnergyDTO
	if err := json.Unmarshal(env.Data, &daily); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(daily) != 2 {
		t.Fatalf("jours = %+v, attendu 2", daily)
	}
	if daily[0].Date != "2026-10-14" || daily[1].Date != "2026-10-15" {
		t.Fatalf("dates = %+v", daily)
	}
	if daily[0].EnergyWh == nil || *daily[0].EnergyWh != 20000 {
		t.Fatalf("14/10 = %v", daily[0].EnergyWh)
	}
	if daily[1].EnergyWh == nil || *daily[1].EnergyWh != 1000 {
		t.Fatalf("15/10 = %v", daily[1].EnergyWh)
	}
	decodeMeta(t, env)

	// Bornes explicites : la fenêtre porte sur ts_utc, tandis que le
	// regroupement se fait par date locale. Une journée locale dont seule la
	// fin (heure locale) tombe dans la fenêtre apparaît donc avec un maximum
	// partiel — c'est le comportement documenté du endpoint.
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/energy/daily?from=2026-10-14T00:00:00Z&to=2026-10-15T00:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, body)
	}
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &daily); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(daily) != 2 || daily[0].Date != "2026-10-14" || daily[1].Date != "2026-10-15" {
		t.Fatalf("daily = %+v", daily)
	}
	if daily[0].EnergyWh == nil || *daily[0].EnergyWh != 20000 {
		t.Fatalf("14/10 = %v", daily[0].EnergyWh)
	}
	if daily[1].EnergyWh == nil || *daily[1].EnergyWh != 1000 {
		t.Fatalf("15/10 = %v", daily[1].EnergyWh)
	}

	// Fenêtre ne couvrant que le 13 octobre UTC : aucune journée locale.
	code, _, body = doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/energy/daily?from=2026-10-12T00:00:00Z&to=2026-10-13T12:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(body), `"data":[]`) {
		t.Fatalf("une plage sans donnée doit produire [] : %s", body)
	}

	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/energy/daily?from=pas-une-date")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d", code)
	}
	env = decodeEnvelope(t, body)
	if env.Error == nil || env.Error.Code != CodeInvalidRange {
		t.Fatalf("erreur = %+v", env.Error)
	}
}

func TestEventsEndpoint(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	for i := range 3 {
		if err := store.InsertEvent(ctx, model.Event{
			TS:       time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
			Kind:     model.EventCollectorError,
			Severity: model.SeverityWarning,
			Message:  "échec de la collecte",
			Detail:   "fronius: server | HTTP 503",
		}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/events")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var events []eventDTO
	if err := json.Unmarshal(env.Data, &events); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("événements = %d", len(events))
	}
	if events[0].TS != "2026-10-16T08:02:00.000Z" || events[0].Severity != "warning" {
		t.Fatalf("premier événement = %+v", events[0])
	}

	// Limite appliquée.
	_, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/events?limit=2")
	env = decodeEnvelope(t, body)
	if err := json.Unmarshal(env.Data, &events); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("limite ignorée : %d", len(events))
	}

	for _, bad := range []string{"0", "-1", "501", "beaucoup"} {
		code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/events?limit="+bad)
		if code != http.StatusBadRequest {
			t.Fatalf("limit=%s : status = %d", bad, code)
		}
		env := decodeEnvelope(t, body)
		if env.Error == nil || env.Error.Code != CodeInvalidRange {
			t.Fatalf("limit=%s : erreur = %+v", bad, env.Error)
		}
	}
}

func TestCapabilitiesEndpoint(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/api/v1/capabilities")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	env := decodeEnvelope(t, body)
	var caps []capabilityDTO
	if err := json.Unmarshal(env.Data, &caps); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var unavailable int
	for _, c := range caps {
		if c.Available {
			continue
		}
		unavailable++
		if c.Reason == "" {
			t.Fatalf("%s : motif manquant", c.ID)
		}
		if c.Quality != string(model.QualityMissing) {
			t.Fatalf("%s : qualité %q", c.ID, c.Quality)
		}
	}
	if unavailable < 4 {
		t.Fatalf("capacités indisponibles = %d, attendu au moins 4", unavailable)
	}
	if len(caps) != 9 {
		t.Fatalf("capacités = %d, attendu 9", len(caps))
	}

	// Les tableaux vides sont sérialisés en [] et non null.
	if strings.Contains(string(body), `"requires":null`) || strings.Contains(string(body), `"evidence":null`) {
		t.Fatalf("un tableau doit être sérialisé en [] : %s", body)
	}
	// Aucune valeur numérique nulle inventée dans la réponse complète.
	for _, needle := range []string{`":0,`, `":0}`, `":0.0,`, `":0.0}`} {
		if strings.Contains(string(body), needle) {
			t.Fatalf("valeur numérique nulle détectée (%s) : %s", needle, body)
		}
	}
	decodeMeta(t, env)
}

func TestStaticWebSPAFallback(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>app</html>"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "app.js"), []byte("console.log(1)"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, httpSrv, _ := newTestServer(t, func(c *config.Config) { c.WebDir = webDir })

	// Racine → index.html.
	code, _, body := doRequest(t, httpSrv, http.MethodGet, "/")
	if code != http.StatusOK || !strings.Contains(string(body), "app") {
		t.Fatalf("GET / = %d %s", code, body)
	}
	// Fichier réel.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/app.js")
	if code != http.StatusOK || !strings.Contains(string(body), "console.log") {
		t.Fatalf("GET /app.js = %d %s", code, body)
	}
	// Route SPA inconnue → index.html.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/capacites")
	if code != http.StatusOK || !strings.Contains(string(body), "app") {
		t.Fatalf("GET /capacites = %d %s", code, body)
	}
	// L'API garde la main.
	code, _, body = doRequest(t, httpSrv, http.MethodGet, "/api/v1/inconnu")
	if code != http.StatusNotFound || !strings.Contains(string(body), CodeNotFound) {
		t.Fatalf("GET /api/v1/inconnu = %d %s", code, body)
	}
}

func TestNoStaticWebWhenUnset(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)
	code, _, _ := doRequest(t, httpSrv, http.MethodGet, "/")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, attendu 404 sans FRONIUS_WEB_DIR", code)
	}
}
