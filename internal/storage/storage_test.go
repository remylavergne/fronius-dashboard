package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

func brussels(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

func openStore(t *testing.T, loc *time.Location) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fronius.db")
	s, err := Open(path, loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sample(ts time.Time, pv float64) model.Sample {
	return model.Sample{
		TS:       ts,
		TSSource: model.TSSourceDevice,
		Source:   model.SourceSolarAPI,
		Quality:  model.QualityMeasured,
		Mode:     "produce-only",
		PowerPVW: new(pv),
	}
}

func mustInsert(t *testing.T, s *Store, sm model.Sample) {
	t.Helper()
	if err := s.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample(%s): %v", sm.TS, err)
	}
}

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	loc := brussels(t)
	path := filepath.Join(t.TempDir(), "nested", "dir", "fronius.db")

	first, err := Open(path, loc)
	if err != nil {
		t.Fatalf("premier Open: %v", err)
	}
	var version, rows int
	if err := first.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("lecture de la version: %v", err)
	}
	if err := first.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("comptage des migrations: %v", err)
	}
	if version != 2 || rows != 2 {
		t.Fatalf("schema_migrations = (version=%d, lignes=%d), attendu (2, 2)", version, rows)
	}
	// Le mode WAL doit être actif (preuve que la DSN a bien été appliquée).
	var journalMode string
	if err := first.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, attendu wal", journalMode)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Deuxième ouverture : aucune migration ne doit être rejouée.
	second, err := Open(path, loc)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if err := second.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("comptage après second Open: %v", err)
	}
	if rows != 2 {
		t.Fatalf("migrations rejouées : %d lignes", rows)
	}
	// Les tables restent utilisables.
	if _, err := second.SampleCount(context.Background()); err != nil {
		t.Fatalf("SampleCount après réouverture: %v", err)
	}

}

func TestUpsertIsIdempotent(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	ts := time.Date(2026, 10, 16, 7, 17, 43, 0, time.UTC)

	first := sample(ts, 100)
	first.EnergyDayWh = new(1000.0)
	first.Inverters = []model.InverterSample{{ID: "1", DT: 1, PowerW: new(100.0)}}
	mustInsert(t, s, first)

	second := sample(ts, 250)
	second.Quality = model.QualityEstimated
	second.Mode = "produce-only"
	second.EnergyDayWh = new(2000.0)
	second.PowerGridW = new(0.0)
	second.Inverters = []model.InverterSample{{ID: "1", DT: 1, PowerW: new(250.0), EnergyDayWh: new(7.0)}}
	mustInsert(t, s, second)

	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("SampleCount = %d, attendu 1 (upsert sur (ts_utc, source))", count)
	}

	got, err := s.LatestSample(ctx)
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if got.PowerPVW == nil || *got.PowerPVW != 250 {
		t.Fatalf("PowerPVW = %v, attendu 250 (mise à jour)", got.PowerPVW)
	}
	if got.EnergyDayWh == nil || *got.EnergyDayWh != 2000 {
		t.Fatalf("EnergyDayWh = %v", got.EnergyDayWh)
	}
	// Un 0 explicite doit survivre à l'aller-retour (pas de confusion avec NULL).
	if got.PowerGridW == nil || *got.PowerGridW != 0 {
		t.Fatalf("PowerGridW = %v, attendu 0 explicite", got.PowerGridW)
	}
	if got.Quality != model.QualityEstimated {
		t.Fatalf("Quality = %q", got.Quality)
	}
	// Un champ jamais renseigné doit rester nil.
	if got.PowerLoadW != nil || got.PowerBatteryW != nil || got.RelAutonomyPct != nil {
		t.Fatalf("des champs absents sont devenus non nuls : %+v", got)
	}
	if len(got.Inverters) != 1 || got.Inverters[0].PowerW == nil || *got.Inverters[0].PowerW != 250 {
		t.Fatalf("onduleurs = %+v", got.Inverters)
	}
	if got.Inverters[0].EnergyDayWh == nil || *got.Inverters[0].EnergyDayWh != 7 {
		t.Fatalf("E_Day onduleur = %v", got.Inverters[0].EnergyDayWh)
	}
	if !got.TS.Equal(ts) {
		t.Fatalf("TS = %v, attendu %v", got.TS, ts)
	}
}

func TestInverterAbsentValuesRoundTrip(t *testing.T) {
	s := openStore(t, brussels(t))
	ts := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)

	sm := sample(ts, 10)
	sm.Inverters = []model.InverterSample{
		{ID: "1", DT: 1, PowerW: new(10.0)},     // EnergyDayWh absent
		{ID: "2", DT: 1, EnergyDayWh: new(3.0)}, // PowerW absent
	}
	mustInsert(t, s, sm)

	got, err := s.LatestSample(context.Background())
	if err != nil {
		t.Fatalf("LatestSample: %v", err)
	}
	if len(got.Inverters) != 2 {
		t.Fatalf("onduleurs = %+v", got.Inverters)
	}
	if got.Inverters[0].EnergyDayWh != nil {
		t.Fatalf("E_Day absent devenu %v", *got.Inverters[0].EnergyDayWh)
	}
	if got.Inverters[1].PowerW != nil {
		t.Fatalf("P absent devenu %v", *got.Inverters[1].PowerW)
	}
}

func TestEmptyStoreReturnsErrNoData(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	if _, err := s.LatestSample(ctx); !errors.Is(err, ErrNoData) {
		t.Fatalf("LatestSample = %v, attendu ErrNoData", err)
	}
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{
		From: time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC),
		Step: time.Hour,
	})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 0 {
		t.Fatalf("buckets = %d, attendu 0", len(buckets))
	}
	daily, err := s.DailyEnergy(ctx, time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(daily) != 0 {
		t.Fatalf("daily = %d, attendu 0", len(daily))
	}
	events, err := s.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d, attendu 0", len(events))
	}
	if _, ok, err := s.LatestRun(ctx); err != nil || ok {
		t.Fatalf("LatestRun = (%v, %v), attendu (vide, nil)", ok, err)
	}
}

func TestTimeSeriesBucketBoundsAndGap(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Origine alignée sur une minute UTC.
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	// Bucket 10:00 → 3 échantillons (100, 200, 300 W).
	for i, pv := range []float64{100, 200, 300} {
		mustInsert(t, s, sample(base.Add(time.Duration(i)*10*time.Second), pv))
	}
	// 10:01 est laissé vide volontairement (trou).
	// Bucket 10:02 → 1 échantillon sans valeur de puissance PV.
	missing := sample(base.Add(2*time.Minute+5*time.Second), 0)
	missing.PowerPVW = nil
	mustInsert(t, s, missing)
	// Exactement à la borne `to` : doit être exclu.
	mustInsert(t, s, sample(base.Add(3*time.Minute), 999))

	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{
		From: base,
		To:   base.Add(3 * time.Minute),
		Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("buckets = %d, attendu 2 (le bucket vide ne doit pas être émis)", len(buckets))
	}

	first := buckets[0]
	if !first.Start.Equal(base) {
		t.Fatalf("premier bucket = %v, attendu %v", first.Start, base)
	}
	if first.Start.Unix()%60 != 0 {
		t.Fatalf("bucket non aligné sur l'epoch : %v", first.Start)
	}
	if first.PowerPVAvgW == nil || *first.PowerPVAvgW != 200 {
		t.Fatalf("AVG = %v, attendu 200", first.PowerPVAvgW)
	}
	if first.PowerPVMinW == nil || *first.PowerPVMinW != 100 {
		t.Fatalf("MIN = %v", first.PowerPVMinW)
	}
	if first.PowerPVMaxW == nil || *first.PowerPVMaxW != 300 {
		t.Fatalf("MAX = %v", first.PowerPVMaxW)
	}
	if first.Count != 3 || first.Missing != 0 {
		t.Fatalf("Count=%d Missing=%d, attendu 3 et 0", first.Count, first.Missing)
	}

	second := buckets[1]
	if !second.Start.Equal(base.Add(time.Minute * 2)) {
		t.Fatalf("second bucket = %v, attendu %v (bucket 10:01 absent)", second.Start, base.Add(2*time.Minute))
	}
	if second.PowerPVAvgW != nil || second.PowerPVMinW != nil || second.PowerPVMaxW != nil {
		t.Fatalf("un bucket sans valeur PV doit rester nil : %+v", second)
	}
	if second.Count != 1 || second.Missing != 1 {
		t.Fatalf("Count=%d Missing=%d, attendu 1 et 1", second.Count, second.Missing)
	}
}

func TestTimeSeriesRejectsInvalidStep(t *testing.T) {
	s := openStore(t, brussels(t))
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	for _, step := range []time.Duration{0, -time.Minute, 500 * time.Millisecond} {
		if _, err := s.TimeSeries(context.Background(), TimeSeriesQuery{From: base, To: base.Add(time.Hour), Step: step}); err == nil {
			t.Fatalf("step=%v devait être refusé", step)
		}
	}
}

// dailyFixture remplit chaque journée locale de [from, to[ avec un échantillon
// par heure locale réelle, dont E_Day est l'énergie accumulée depuis minuit
// local. Le parcours se fait en ajoutant des heures absolues à partir de minuit
// local : les journées de changement d'heure comptent donc 23 ou 25 échantillons.
//
// La fonction retourne le nombre d'échantillons insérés par date locale.
func dailyFixture(t *testing.T, s *Store, loc *time.Location, from, to time.Time) map[string]int {
	t.Helper()
	perDate := map[string]int{}

	for dayStart := from; dayStart.Before(to); {
		dayEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day()+1, 0, 0, 0, 0, loc)
		date := dayStart.Format("2006-01-02")
		for ts := dayStart; ts.Before(dayEnd); ts = ts.Add(time.Hour) {
			elapsedHours := ts.Sub(dayStart).Hours()
			sm := sample(ts, 1000)
			sm.EnergyDayWh = new(elapsedHours * 1000)
			mustInsert(t, s, sm)
			perDate[date]++
		}
		dayStart = dayEnd
	}
	return perDate
}

func TestDailyEnergyAcrossDSTTransitions(t *testing.T) {
	loc := brussels(t)

	cases := []struct {
		name      string
		from, to  time.Time
		wantHours map[string]int
	}{
		{
			name: "passage à l'heure d'été (23 h)",
			from: time.Date(2026, 3, 28, 0, 0, 0, 0, loc),
			to:   time.Date(2026, 3, 31, 0, 0, 0, 0, loc),
			wantHours: map[string]int{
				"2026-03-28": 24,
				"2026-03-29": 23, // 02:00 → 03:00 locales n'existent pas
				"2026-03-30": 24,
			},
		},
		{
			name: "passage à l'heure d'hiver (25 h)",
			from: time.Date(2026, 10, 24, 0, 0, 0, 0, loc),
			to:   time.Date(2026, 10, 27, 0, 0, 0, 0, loc),
			wantHours: map[string]int{
				"2026-10-24": 24,
				"2026-10-25": 25, // 03:00 → 02:00 locales : une heure en double
				"2026-10-26": 24,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openStore(t, loc)
			ctx := context.Background()
			perDate := dailyFixture(t, s, loc, tc.from, tc.to)

			// Le fixture doit réellement exercer le changement d'heure.
			for date, want := range tc.wantHours {
				if perDate[date] != want {
					t.Fatalf("fixture : %s = %d échantillons, attendu %d", date, perDate[date], want)
				}
			}

			daily, err := s.DailyEnergy(ctx, tc.from, tc.to)
			if err != nil {
				t.Fatalf("DailyEnergy: %v", err)
			}
			if len(daily) != len(tc.wantHours) {
				t.Fatalf("jours = %d (%v), attendu %d", len(daily), daily, len(tc.wantHours))
			}

			seen := map[string]int{}
			for _, d := range daily {
				seen[d.Date]++
				if d.EnergyWh == nil {
					t.Fatalf("%s : energy_wh est nil", d.Date)
				}
				// Le maximum du jour est atteint à l'avant-dernier échantillon
				// horaire de la journée locale, soit (heures − 1) × 1000 Wh.
				wantWh := float64(tc.wantHours[d.Date]-1) * 1000
				if *d.EnergyWh != wantWh {
					t.Fatalf("%s : energy_wh = %v, attendu %v", d.Date, *d.EnergyWh, wantWh)
				}
			}
			for date, n := range seen {
				if n != 1 {
					t.Fatalf("jour %s émis %d fois", date, n)
				}
			}
			for date := range tc.wantHours {
				if seen[date] == 0 {
					t.Fatalf("jour %s manquant", date)
				}
			}
			// Ordre croissant, sans trou.
			for i := 1; i < len(daily); i++ {
				if daily[i-1].Date >= daily[i].Date {
					t.Fatalf("ordre des jours non croissant : %v", daily)
				}
			}
		})
	}
}

// TestDailyEnergyCounterReset : la remise à zéro de E_Day à minuit local ne doit
// pas produire d'énergie négative ni de doublon de jour.
//
// En été, minuit local correspond à 22:00 UTC la veille : les instants ci-dessous
// sont choisis de part et d'autre de cette frontière.
func TestDailyEnergyCounterReset(t *testing.T) {
	loc := brussels(t)
	s := openStore(t, loc)
	ctx := context.Background()

	inserts := []struct {
		ts   time.Time
		eDay float64
		note string
	}{
		{time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC), 20000, "10-15 01:00 local"},
		{time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC), 20000, "10-15 14:00 local"},
		{time.Date(2026, 10, 15, 22, 30, 0, 0, time.UTC), 0, "10-16 00:30 local (compteur remis à zéro)"},
		{time.Date(2026, 10, 15, 23, 30, 0, 0, time.UTC), 1000, "10-16 01:30 local"},
	}
	for _, ins := range inserts {
		sm := sample(ins.ts, 500)
		sm.EnergyDayWh = new(ins.eDay)
		mustInsert(t, s, sm)
	}

	daily, err := s.DailyEnergy(ctx,
		time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	if len(daily) != 2 {
		t.Fatalf("jours = %+v, attendu 2 (15 et 16 octobre locaux)", daily)
	}
	if daily[0].Date != "2026-10-15" || daily[1].Date != "2026-10-16" {
		t.Fatalf("dates = %v", daily)
	}
	if daily[0].EnergyWh == nil || *daily[0].EnergyWh != 20000 {
		t.Fatalf("10-15 : %v, attendu 20000", daily[0].EnergyWh)
	}
	if daily[1].EnergyWh == nil || *daily[1].EnergyWh != 1000 {
		t.Fatalf("10-16 : %v, attendu 1000 (maximum après remise à zéro)", daily[1].EnergyWh)
	}
}

func TestEventsAreOrderedAndLimited(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

	for i := range 5 {
		err := s.InsertEvent(ctx, model.Event{
			TS:       base.Add(time.Duration(i) * time.Minute),
			Kind:     model.EventCollectorError,
			Severity: model.SeverityWarning,
			Message:  "échec",
			Detail:   string(rune('a' + i)),
		})
		if err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	got, err := s.Events(ctx, EventsQuery{Limit: 3})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limite ignorée : %d événements", len(got))
	}
	if !got[0].TS.Equal(base.Add(4 * time.Minute)) {
		t.Fatalf("le plus récent doit venir en premier : %v", got[0].TS)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].TS.Before(got[i].TS) {
			t.Fatalf("ordre décroissant attendu : %v", got)
		}
	}

	// Bornes de plage.
	inRange, err := s.Events(ctx, EventsQuery{
		Limit: 10,
		From:  base.Add(time.Minute),
		To:    base.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Events bornés: %v", err)
	}
	if len(inRange) != 2 {
		t.Fatalf("événements dans la plage = %d, attendu 2", len(inRange))
	}
}

func TestRunRecords(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

	// Aucun cycle enregistré.
	if _, ok, err := s.LatestRun(ctx); err != nil || ok {
		t.Fatalf("LatestRun initial = (%v, %v)", ok, err)
	}

	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base, DurationMS: 12, OK: true, HTTPStatus: 200,
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base.Add(time.Minute), DurationMS: 5003, OK: false,
		HTTPStatus: 503, ErrorKind: "server", Error: "service indisponible",
	}); err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	got, ok, err := s.LatestRun(ctx)
	if err != nil || !ok {
		t.Fatalf("LatestRun = (%v, %v)", ok, err)
	}
	if got.OK || got.HTTPStatus != 503 || got.ErrorKind != "server" || got.Error != "service indisponible" {
		t.Fatalf("dernier cycle = %+v", got)
	}
	if got.DurationMS != 5003 {
		t.Fatalf("DurationMS = %d", got.DurationMS)
	}
	if !got.StartedUTC.Equal(base.Add(time.Minute)) {
		t.Fatalf("StartedUTC = %v", got.StartedUTC)
	}

	// Un statut HTTP absent reste 0 (colonne NULL) sans casser la lecture.
	if err := s.InsertRun(ctx, RunRecord{
		StartedUTC: base.Add(2 * time.Minute), DurationMS: 1, OK: false,
		ErrorKind: "transport", Error: "connexion refusée",
	}); err != nil {
		t.Fatalf("InsertRun sans statut: %v", err)
	}
	got, _, err = s.LatestRun(ctx)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if got.HTTPStatus != 0 || got.ErrorKind != "transport" {
		t.Fatalf("cycle sans statut = %+v", got)
	}
}

func TestSetStateAndState(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	state, err := s.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state == nil || len(state) != 0 {
		t.Fatalf("State initial = %v, attendu map vide non nil", state)
	}

	if err := s.SetState(ctx, "last_success_utc", "2026-10-16T08:00:00.000Z"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, "last_error", "premier"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.SetState(ctx, "last_error", "second"); err != nil {
		t.Fatalf("SetState (mise à jour): %v", err)
	}

	state, err = s.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if len(state) != 2 {
		t.Fatalf("State = %v, attendu 2 entrées", state)
	}
	if state["last_error"] != "second" {
		t.Fatalf("last_error = %q", state["last_error"])
	}
	if state["last_success_utc"] != "2026-10-16T08:00:00.000Z" {
		t.Fatalf("last_success_utc = %q", state["last_success_utc"])
	}
}

func TestPurgeBefore(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

	for i := range 3 {
		mustInsert(t, s, sample(base.Add(time.Duration(i)*time.Hour), float64(i)))
		if err := s.InsertEvent(ctx, model.Event{
			TS: base.Add(time.Duration(i) * time.Hour), Kind: "test", Severity: model.SeverityInfo, Message: "m",
		}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
		if err := s.InsertRun(ctx, RunRecord{StartedUTC: base.Add(time.Duration(i) * time.Hour), OK: true}); err != nil {
			t.Fatalf("InsertRun: %v", err)
		}
	}

	deleted, err := s.PurgeBefore(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("échantillons purgés = %d, attendu 1", deleted)
	}

	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("échantillons restants = %d, attendu 2", count)
	}
	events, err := s.Events(ctx, EventsQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("événements restants = %d, attendu 2", len(events))
	}
	latest, _, err := s.LatestRun(ctx)
	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if !latest.StartedUTC.Equal(base.Add(2 * time.Hour)) {
		t.Fatalf("cycles non purgés : %v", latest.StartedUTC)
	}

	// Purge d'une plage vide : aucune erreur, aucun effet.
	deleted, err = s.PurgeBefore(ctx, base)
	if err != nil {
		t.Fatalf("PurgeBefore vide: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("échantillons purgés = %d, attendu 0", deleted)
	}
}

func TestTimestampFormattingIsStable(t *testing.T) {
	// Deux instants séparés de moins d'une milliseconde partagent la même clé :
	// l'upsert les fusionne, la précision stockée étant la milliseconde.
	ts := time.Date(2026, 10, 16, 7, 17, 43, 123456789, time.UTC)
	if got, want := formatTS(ts), "2026-10-16T07:17:43.123Z"; got != want {
		t.Fatalf("formatTS = %q, attendu %q", got, want)
	}
	back, err := parseTS("2026-10-16T07:17:43.123Z")
	if err != nil {
		t.Fatalf("parseTS: %v", err)
	}
	if !back.Equal(ts.Truncate(time.Millisecond)) {
		t.Fatalf("aller-retour = %v", back)
	}
	if back.Location() != time.UTC {
		t.Fatalf("l'horodatage relu doit être en UTC, reçu %v", back.Location())
	}
	// L'ordre lexicographique suit l'ordre chronologique.
	if formatTS(ts.Add(time.Second)) <= formatTS(ts) {
		t.Fatal("l'ordre lexicographique ne suit pas l'ordre chronologique")
	}
	if _, err := parseTS("2026-10-16 07:17:43"); err == nil {
		t.Fatal("un format non conforme devait être refusé")
	}
}

func TestSplitStatements(t *testing.T) {
	// Le point-virgule à l'intérieur du commentaire est le cas qui a réellement
	// cassé l'application de la migration : il doit être retiré avec le reste
	// du commentaire.
	body := "-- commentaire\n-- unités (W, Wh, %) ; aucune conversion\nCREATE TABLE a (x INTEGER);\n\n-- autre\nCREATE INDEX b ON a(x);\n"
	got := splitStatements(body)
	if len(got) != 2 {
		t.Fatalf("instructions = %d (%q), attendu 2", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE TABLE a") {
		t.Fatalf("première instruction inattendue : %q", got[0])
	}
	if !strings.HasPrefix(got[1], "CREATE INDEX b") {
		t.Fatalf("seconde instruction inattendue : %q", got[1])
	}
	for _, stmt := range got {
		if strings.Contains(stmt, "aucune") {
			t.Fatalf("un fragment de commentaire a survécu : %q", stmt)
		}
	}
}

// ---------------------------------------------------------------------------
// Migration 0002 : identité, détection, anti-duplication
// ---------------------------------------------------------------------------

func deviceRecord(ts time.Time, modelName string) DeviceRecord {
	return DeviceRecord{
		ReadAtUTC:         ts,
		Source:            "/status/version",
		Model:             modelName,
		Firmware:          "1.24.3-sim",
		SerialNumber:      "SIMULATED-0001",
		IdentityState:     model.StatePresent,
		MeterState:        model.StatePresent,
		MeterSource:       "/components/PowerMeter/readable",
		BatteryState:      model.StateAbsent,
		BatterySource:     "/components/BatteryManagementSystem/readable",
		DeviceEventsState: model.StatePresent,
	}
}

func TestDeviceIsASingleReplacedRow(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Base neuve : aucune ligne, donc « non déterminé » — pas un zéro déguisé.
	if got, ok, err := s.Device(ctx); err != nil {
		t.Fatalf("Device: %v", err)
	} else if ok {
		t.Fatalf("une base neuve ne doit contenir aucune ligne : %+v", got)
	}

	first := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	if err := s.SaveDevice(ctx, deviceRecord(first, "Primo GEN24")); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	// Seconde écriture : la ligne est remplacée, pas dupliquée.
	second := first.Add(time.Minute)
	rec := deviceRecord(second, "Primo GEN24 3.6")
	rec.BatteryState = model.StatePresent
	rec.DeviceEventsState = model.StateUnknown
	if err := s.SaveDevice(ctx, rec); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	got, ok, err := s.Device(ctx)
	if err != nil {
		t.Fatalf("Device: %v", err)
	}
	if !ok {
		t.Fatal("une ligne est attendue après deux écritures")
	}
	if !got.ReadAtUTC.Equal(second) || got.Model != "Primo GEN24 3.6" {
		t.Fatalf("enregistrement = %+v", got)
	}
	if got.MeterState != model.StatePresent || got.BatteryState != model.StatePresent {
		t.Fatalf("états = %+v", got)
	}
	// « non déterminé » doit survivre à l'aller-retour : il ne devient jamais
	// « absent ».
	if got.DeviceEventsState != model.StateUnknown || got.IdentityState != model.StatePresent {
		t.Fatalf("états = %+v", got)
	}
	if got.SerialNumber != "SIMULATED-0001" || got.Firmware != "1.24.3-sim" {
		t.Fatalf("identité = %+v", got)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM device`).Scan(&count); err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if count != 1 {
		t.Fatalf("lignes en base = %d, attendu 1", count)
	}

	// La contrainte CHECK (id = 1) interdit une seconde ligne.
	if _, err := s.db.Exec(`INSERT INTO device (id, read_at_utc, source) VALUES (2, '2026-01-01T00:00:00.000Z', 'x')`); err == nil {
		t.Fatal("une seconde ligne d'appareil devait être refusée")
	}
}

func TestMarkDeviceEventSeen(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	ts := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)

	fresh, err := s.MarkDeviceEventSeen(ctx, "device:ABC", ts)
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if !fresh {
		t.Fatal("une clé neuve doit être signalée comme telle")
	}

	again, err := s.MarkDeviceEventSeen(ctx, "device:ABC", ts.Add(time.Minute))
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if again {
		t.Fatal("une clé déjà vue ne doit pas être signalée comme neuve")
	}

	other, err := s.MarkDeviceEventSeen(ctx, "device:DEF", ts)
	if err != nil {
		t.Fatalf("MarkDeviceEventSeen: %v", err)
	}
	if !other {
		t.Fatal("une autre clé doit être neuve")
	}

	count, err := s.DeviceEventsSeenCount(ctx)
	if err != nil {
		t.Fatalf("DeviceEventsSeenCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("clés mémorisées = %d, attendu 2", count)
	}
}

func TestPurgeRemovesSeenEventMarks(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	old := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	marks := map[string]time.Time{"device:ancien": old, "device:récent": recent}
	for key, ts := range marks {
		if _, err := s.MarkDeviceEventSeen(ctx, key, ts); err != nil {
			t.Fatalf("MarkDeviceEventSeen: %v", err)
		}
	}

	// Borne exclusive : la marque exactement à la borne survit.
	limit := recent
	if _, err := s.PurgeBefore(ctx, limit); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}

	count, err := s.DeviceEventsSeenCount(ctx)
	if err != nil {
		t.Fatalf("DeviceEventsSeenCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("clés conservées = %d, attendu 1 (borne haute exclusive)", count)
	}
}
