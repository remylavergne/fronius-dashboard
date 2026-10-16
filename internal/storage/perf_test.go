package storage

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

// TestOneYearTimeSeriesPerformance vérifie le critère de sortie de l'ADR 0006 :
// « une durée de réponse inférieure à 200 ms sur un an de données ».
//
// Le test est désactivé par défaut : il insère ≈6,3 millions de mesures, ce qui
// n'a pas sa place dans une suite de tests ordinaire. Il se lance à la demande :
//
//	FRONIUS_PERF_YEAR=1 go test ./internal/storage/ -run TestOneYearTimeSeriesPerformance -v -timeout 30m
func TestOneYearTimeSeriesPerformance(t *testing.T) {
	if os.Getenv("FRONIUS_PERF_YEAR") == "" {
		t.Skip("FRONIUS_PERF_YEAR non défini : mesure sur un an ignorée")
	}

	const (
		step  = 5 * time.Second // cadence du collecteur
		rows  = 365 * 24 * 3600 / 5
		chunk = 200_000
	)
	start := time.Date(2025, 10, 16, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)

	s := openStore(t, brussels(t))
	ctx := context.Background()

	// Remplissage direct : la boucle d'insertion du collecteur (une instruction
	// d'abaissement de borne par ligne) n'est pas ce que l'on mesure ici.
	insertStart := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO samples (`+sampleColumns+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for i := range rows {
		ts := start.Add(time.Duration(i) * step)
		// Profil déterministe : une cloche journalière et une énergie en dents
		// de scie. La valeur importe peu, seul le volume compte.
		secOfDay := float64(ts.Hour()*3600 + ts.Minute()*60 + ts.Second())
		pv := 5000 * (1 + math.Sin(secOfDay/86400*2*math.Pi)) / 2
		eDay := float64(ts.Hour()) * 1000
		if _, err := stmt.ExecContext(ctx,
			formatTS(ts), "solarapi", "device", "measured", "produce-only",
			pv, eDay, 0, 0, nil, nil, nil, nil, nil, "[]"); err != nil {
			t.Fatalf("insertion ligne %d: %v", i, err)
		}
		if i%chunk == chunk-1 {
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			tx, err = s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			stmt, err = tx.PrepareContext(ctx, `INSERT INTO samples (`+sampleColumns+`)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit final: %v", err)
	}
	count, err := s.SampleCount(ctx)
	if err != nil {
		t.Fatalf("SampleCount: %v", err)
	}
	t.Logf("remplissage : %d mesures en %s", count, time.Since(insertStart).Round(time.Millisecond))

	// Chemin phase 2 : lecture directe des mesures brutes.
	rawHourly := timeQuery(t, s, start, end, time.Hour)
	t.Logf("AVANT (mesures brutes, pas 1 h, un an) : %s pour %d buckets",
		rawHourly.duration.Round(time.Millisecond), rawHourly.buckets)
	rawDaily := dailyQuery(t, s, start, end)
	t.Logf("AVANT (énergie journalière, un an) : %s pour %d journées",
		rawDaily.duration.Round(time.Millisecond), rawDaily.buckets)

	rollupStart := time.Now()
	res, err := s.Rollup(ctx, end)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	t.Logf("agrégation d'un an : %s (%d buckets)",
		time.Since(rollupStart).Round(time.Millisecond), res.Buckets)

	// Chemin phase 3 : agrégats horaires. La seconde mesure inclut l'heure
	// ouverte, donc une heure de mesures brutes, comme une requête réelle
	// « jusqu'à maintenant ».
	aggHourly := timeQuery(t, s, start, end, time.Hour)
	t.Logf("APRÈS (agrégats, pas 1 h, un an) : %s pour %d buckets",
		aggHourly.duration.Round(time.Millisecond), aggHourly.buckets)
	aggDaily := dailyQuery(t, s, start, end)
	t.Logf("APRÈS (énergie journalière, un an) : %s pour %d journées",
		aggDaily.duration.Round(time.Millisecond), aggDaily.buckets)
	openTail := timeQuery(t, s, start, end.Add(-time.Minute), time.Hour)
	t.Logf("APRÈS avec heure ouverte : %s pour %d buckets",
		openTail.duration.Round(time.Millisecond), openTail.buckets)
	aggDayStep := timeQuery(t, s, start, end, 24*time.Hour)
	t.Logf("APRÈS (pas 1 j, un an) : %s pour %d buckets",
		aggDayStep.duration.Round(time.Millisecond), aggDayStep.buckets)

	if rawHourly.duration <= aggHourly.duration {
		t.Logf("avertissement : le chemin brut n'est pas plus lent (%s vs %s)",
			rawHourly.duration.Round(time.Millisecond), aggHourly.duration.Round(time.Millisecond))
	}
	for _, m := range []struct {
		name string
		d    time.Duration
	}{{"pas 1 h", aggHourly.duration}, {"énergie journalière", aggDaily.duration}, {"heure ouverte", openTail.duration}, {"pas 1 j", aggDayStep.duration}} {
		if m.d > 200*time.Millisecond {
			t.Errorf("%s : %s, au-delà du critère de 200 ms (ADR 0006)", m.name, m.d.Round(time.Millisecond))
		}
	}
	if aggHourly.buckets != rawHourly.buckets {
		t.Fatalf("buckets agrégés = %d, brut = %d", aggHourly.buckets, rawHourly.buckets)
	}
	if aggDaily.buckets != rawDaily.buckets {
		t.Fatalf("journées agrégées = %d, brutes = %d", aggDaily.buckets, rawDaily.buckets)
	}
}

type measured struct {
	duration time.Duration
	buckets  int
}

func timeQuery(t *testing.T, s *Store, from, to time.Time, step time.Duration) measured {
	t.Helper()
	start := time.Now()
	buckets, err := s.TimeSeries(context.Background(), TimeSeriesQuery{From: from, To: to, Step: step})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	return measured{duration: time.Since(start), buckets: len(buckets)}
}

func dailyQuery(t *testing.T, s *Store, from, to time.Time) measured {
	t.Helper()
	start := time.Now()
	days, err := s.DailyEnergy(context.Background(), from, to)
	if err != nil {
		t.Fatalf("DailyEnergy: %v", err)
	}
	return measured{duration: time.Since(start), buckets: len(days)}
}
