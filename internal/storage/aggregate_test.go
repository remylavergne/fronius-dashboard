package storage

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
)

// fill insère des échantillons de from (inclus) à to (exclu) par pas de step.
// mk reçoit l'indice et l'instant ; un échantillon d'horodatage nul est ignoré,
// ce qui permet de créer un trou.
func fill(t *testing.T, s *Store, from, to time.Time, step time.Duration, mk func(i int, ts time.Time) model.Sample) {
	t.Helper()
	i := 0
	for ts := from; ts.Before(to); ts, i = ts.Add(step), i+1 {
		sm := mk(i, ts)
		if sm.TS.IsZero() {
			continue
		}
		mustInsert(t, s, sm)
	}
}

// pvSample construit un échantillon de puissance PV, sans énergie journalière.
func pvSample(ts time.Time, pv *float64) model.Sample {
	sm := sample(ts, 0)
	sm.PowerPVW = pv
	return sm
}

// seriesFixture remplit une base autour d'une transition d'heure locale, avec
// des trous et des valeurs absentes :
//
//   - 5 minutes de pas sur [2026-03-28T21:00Z, 2026-03-29T06:30Z[ ;
//   - une heure entière sans aucune mesure (04:00Z à 05:00Z) ;
//   - des valeurs de puissance absentes sur une fenêtre de 20 minutes ;
//   - une énergie journalière en dents de scie, pour éprouver la règle du
//     maximum par heure puis par date locale.
//
// Le passage à l'heure d'été de Bruxelles a lieu le 2026-03-29 à 02:00 locales
// (01:00Z) : la fenêtre couvre la nuit de 23 h.
func seriesFixture(t *testing.T, s *Store) time.Time {
	t.Helper()
	base := time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)
	const step = 5 * time.Minute

	fill(t, s, base, end, step, func(i int, ts time.Time) model.Sample {
		if ts.Hour() == 4 {
			return model.Sample{} // trou d'une heure complète
		}
		sm := sample(ts, 0)
		// Valeurs absentes sur une fenêtre de 20 minutes.
		if ts.Hour() == 3 && ts.Minute() >= 10 && ts.Minute() < 30 {
			sm.PowerPVW = nil
		} else {
			sm.PowerPVW = new(float64(i%37) * 13.5)
		}
		e := float64(ts.Hour())*100 + float64(ts.Minute())
		sm.EnergyDayWh = &e
		return sm
	})
	return end
}

func closeFloat(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	scale := math.Max(1, math.Max(math.Abs(*a), math.Abs(*b)))
	return math.Abs(*a-*b) <= 1e-9*scale
}

// assertSameBuckets compare deux séries en tolérant la réassociation des
// flottants (la somme d'agrégats horaires n'est pas sommée dans le même ordre
// que la somme brute), mais en exigeant l'égalité exacte des effectifs.
func assertSameBuckets(t *testing.T, label string, got, want []Bucket) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s : %d buckets, attendu %d\ngot : %+v\nwant: %+v", label, len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.Start.Equal(w.Start) {
			t.Fatalf("%s : bucket %d à %v, attendu %v", label, i, g.Start, w.Start)
		}
		if g.Count != w.Count {
			t.Fatalf("%s : bucket %v, effectif %d, attendu %d", label, g.Start, g.Count, w.Count)
		}
		if g.Missing != w.Missing {
			t.Fatalf("%s : bucket %v, absents %d, attendu %d", label, g.Start, g.Missing, w.Missing)
		}
		if !closeFloat(g.PowerPVAvgW, w.PowerPVAvgW) {
			t.Fatalf("%s : bucket %v, moyenne %v, attendu %v", label, g.Start, deref(g.PowerPVAvgW), deref(w.PowerPVAvgW))
		}
		if !closeFloat(g.PowerPVMinW, w.PowerPVMinW) {
			t.Fatalf("%s : bucket %v, minimum %v, attendu %v", label, g.Start, deref(g.PowerPVMinW), deref(w.PowerPVMinW))
		}
		if !closeFloat(g.PowerPVMaxW, w.PowerPVMaxW) {
			t.Fatalf("%s : bucket %v, maximum %v, attendu %v", label, g.Start, deref(g.PowerPVMaxW), deref(w.PowerPVMaxW))
		}
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// seriesRanges liste les plages éprouvant chaque branche du découpage : plage
// entièrement close, entête au milieu d'une heure, queue dans l'heure ouverte,
// plage sans heure pleine éligible, et plage couvrant une heure vide.
func seriesRanges() []struct {
	name     string
	from, to time.Time
} {
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, time.UTC) }
	return []struct {
		name     string
		from, to time.Time
	}{
		{"tout close", at(0, 0), at(5, 0)},
		{"entête partielle", at(2, 30), at(5, 0)},
		{"queue ouverte", at(2, 0), at(6, 30)},
		{"sans heure pleine", at(2, 30), at(3, 30)},
		{"avec heure vide", at(3, 0), at(6, 0)},
		{"une seule minute", at(2, 30), at(2, 31)},
	}
}

// TestTimeSeriesIdenticalWithAndWithoutAggregates est le test d'équivalence de
// la phase 3 : la même requête doit rendre le même résultat qu'elle soit
// servie par les agrégats horaires ou par les mesures brutes.
func TestTimeSeriesIdenticalWithAndWithoutAggregates(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	end := seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	steps := []time.Duration{time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour}
	ranges := seriesRanges()

	// Référence : aucune borne d'eau n'existe encore, tout est lu au brut.
	before := map[string][]Bucket{}
	for _, st := range steps {
		for _, rg := range ranges {
			b, err := s.TimeSeries(ctx, TimeSeriesQuery{From: rg.from, To: rg.to, Step: st})
			if err != nil {
				t.Fatalf("TimeSeries brut (%s, %s): %v", st, rg.name, err)
			}
			before[st.String()+"/"+rg.name] = b
		}
	}

	res, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.To.Sub(res.From) <= 0 {
		t.Fatalf("Rollup n'a rien agrégé : %+v", res)
	}
	var aggRows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agg_hour`).Scan(&aggRows); err != nil {
		t.Fatalf("comptage des agrégats: %v", err)
	}
	if aggRows == 0 {
		t.Fatal("aucune ligne d'agrégat : le test comparerait le brut à lui-même")
	}

	closed, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("borne d'eau absente après agrégation (ok=%v, err=%v)", ok, err)
	}
	if !closed.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("borne d'eau = %v, attendu %v", closed, now.Truncate(time.Hour))
	}
	// Garde anti-test-dégénéré : au moins une des plages doit réellement être
	// servie par les agrégats.
	usedAggregates := false
	for _, rg := range ranges {
		if _, _, ok := aggSplit(rg.from, rg.to, closed, true); ok {
			usedAggregates = true
		}
	}
	if !usedAggregates {
		t.Fatal("aucune plage n'emprunte le chemin agrégé")
	}

	for _, st := range steps {
		for _, rg := range ranges {
			got, err := s.TimeSeries(ctx, TimeSeriesQuery{From: rg.from, To: rg.to, Step: st})
			if err != nil {
				t.Fatalf("TimeSeries après agrégation (%s, %s): %v", st, rg.name, err)
			}
			assertSameBuckets(t, st.String()+"/"+rg.name, got, before[st.String()+"/"+rg.name])
		}
	}

	// La plage « queue ouverte » couvre le trou d'une heure et la dernière
	// mesure : l'heure vide ne doit produire aucun bucket (aucune
	// interpolation), et le dernier bucket doit précéder la fin des mesures.
	open := before["1h0m0s/queue ouverte"]
	if len(open) != 4 {
		t.Fatalf("%d buckets sur la plage ouverte, attendu 4 : %+v", len(open), open)
	}
	if last := open[len(open)-1].Start; !last.Before(end) {
		t.Fatalf("dernier bucket %v au-delà de la dernière mesure %v", last, end)
	}
}

// TestDailyEnergyIdenticalWithAndWithoutAggregates vérifie l'équivalence du
// calcul d'énergie journalière, y compris sur une plage coupée au milieu d'une
// journée locale et sur la journée de 23 h.
func TestDailyEnergyIdenticalWithAndWithoutAggregates(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	loc := brussels(t)
	ranges := []struct {
		name     string
		from, to time.Time
	}{
		{"journées entières", time.Date(2026, 3, 28, 0, 0, 0, 0, loc), time.Date(2026, 3, 30, 0, 0, 0, 0, loc)},
		{"coupée en plein jour", time.Date(2026, 3, 28, 12, 30, 0, 0, loc), time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
		{"heure ouverte incluse", time.Date(2026, 3, 29, 0, 0, 0, 0, loc), now.Add(30 * time.Minute)},
		{"avant toute mesure", time.Date(2026, 3, 20, 0, 0, 0, 0, loc), time.Date(2026, 3, 21, 0, 0, 0, 0, loc)},
	}

	before := map[string][]DailyEnergy{}
	for _, rg := range ranges {
		d, err := s.DailyEnergy(ctx, rg.from, rg.to)
		if err != nil {
			t.Fatalf("DailyEnergy brut (%s): %v", rg.name, err)
		}
		before[rg.name] = d
	}
	if len(before["journées entières"]) == 0 {
		t.Fatal("aucune journée agrégée : test sans objet")
	}

	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	// Garde anti-test-dégénéré : la plage principale doit réellement être
	// servie par les agrégats.
	if _, _, ok := aggSplit(ranges[0].from, ranges[0].to, now.Truncate(time.Hour), true); !ok {
		t.Fatal("la plage de référence n'emprunte pas le chemin agrégé")
	}
	for _, rg := range ranges {
		got, err := s.DailyEnergy(ctx, rg.from, rg.to)
		if err != nil {
			t.Fatalf("DailyEnergy agrégé (%s): %v", rg.name, err)
		}
		want := before[rg.name]
		if len(got) != len(want) {
			t.Fatalf("%s : %d journées, attendu %d (%v vs %v)", rg.name, len(got), len(want), got, want)
		}
		for i := range want {
			if got[i].Date != want[i].Date {
				t.Fatalf("%s : journée %d = %q, attendu %q", rg.name, i, got[i].Date, want[i].Date)
			}
			if !closeFloat(got[i].EnergyWh, want[i].EnergyWh) {
				t.Fatalf("%s : %s = %v, attendu %v", rg.name, got[i].Date,
					deref(got[i].EnergyWh), deref(want[i].EnergyWh))
			}
		}
	}
}

// TestRollupIsIdempotent vérifie qu'une seconde passe sur la même plage ne
// change rien, et qu'une reprise après une autre passe non plus.
func TestRollupIsIdempotent(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	seriesFixture(t, s)
	now := time.Date(2026, 3, 29, 6, 30, 0, 0, time.UTC)

	first, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("premier Rollup: %v", err)
	}
	dump := func(before string) string {
		q := `SELECT bucket_utc, count_all, count_pv, sum_pv_w, min_pv_w, max_pv_w, max_energy_day_wh
			FROM agg_hour`
		var args []any
		if before != "" {
			q += ` WHERE bucket_utc < ?`
			args = append(args, before)
		}
		q += ` ORDER BY bucket_utc`
		rows, err := s.db.Query(q, args...)
		if err != nil {
			t.Fatalf("lecture des agrégats: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var (
				bucket            string
				countAll, countPV int
				sum               float64
				min, max, eDay    *float64
			)
			if err := rows.Scan(&bucket, &countAll, &countPV, &sum, &min, &max, &eDay); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, fmt.Sprintf("%s|%d|%d|%v|%v|%v|%v",
				bucket, countAll, countPV, sum, deref(min), deref(max), deref(eDay)))
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("parcours des agrégats: %v", err)
		}
		return strings.Join(out, "\n")
	}
	before := dump("")
	if len(before) == 0 {
		t.Fatal("aucun agrégat écrit")
	}
	closedAt := formatTS(now.Truncate(time.Hour))

	// Même instant : la passe ne couvre plus rien et ne doit rien réécrire.
	second, err := s.Rollup(ctx, now)
	if err != nil {
		t.Fatalf("second Rollup: %v", err)
	}
	if second.Buckets != 0 {
		t.Fatalf("second Rollup a écrit %d buckets, attendu 0", second.Buckets)
	}
	if got := dump(""); got != before {
		t.Fatalf("agrégats modifiés par une passe sans travail:\n%s\nvs\n%s", got, before)
	}

	// Plus tard sans nouveau relevé : seule l'heure encore ouverte à la
	// première passe (06:00Z, qui contient déjà six relevés) devient close.
	// Les heures déjà agrégées ne sont pas réécrites.
	third, err := s.Rollup(ctx, now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("troisième Rollup: %v", err)
	}
	if third.Buckets != 1 {
		t.Fatalf("troisième Rollup a écrit %d buckets, attendu 1 (fermeture de l'heure ouverte)", third.Buckets)
	}
	if want := now.Add(3 * time.Hour).Truncate(time.Hour); !third.Watermark.Equal(want) {
		t.Fatalf("borne d'eau = %v, attendu %v", third.Watermark, want)
	}
	if got := dump(closedAt); got != before {
		t.Fatalf("heures closes réécrites sans nouveaux relevés:\n%s\nvs\n%s", got, before)
	}
	if first.Buckets == 0 {
		t.Fatal("la première passe n'a rien écrit")
	}
}

// TestRollupCatchesUpDataInsertedBelowWatermark reproduit l'import d'un
// historique après une première passe : les mesures plus anciennes que la borne
// d'eau doivent être agrégées, et donc visibles des lectures agrégées.
func TestRollupCatchesUpDataInsertedBelowWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)

	// Première journée : agrégée immédiatement.
	fill(t, s, base, base.Add(2*time.Hour), 10*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(1000+i)))
	})
	now := base.Add(4 * time.Hour)
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("premier Rollup: %v", err)
	}
	wm, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("borne d'eau absente (ok=%v, err=%v)", ok, err)
	}
	if !wm.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("borne d'eau = %v, attendu %v", wm, now.Truncate(time.Hour))
	}

	// Import tardif de la veille (backfill) : les mesures sont plus anciennes
	// que la borne d'eau.
	old := base.Add(-3 * time.Hour)
	fill(t, s, old, old.Add(2*time.Hour), 10*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(500+i)))
	})
	after, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("borne d'eau disparue (ok=%v, err=%v)", ok, err)
	}
	if !after.Before(wm) {
		t.Fatalf("borne d'eau non abaissée : %v (avant %v)", after, wm)
	}
	if !after.Equal(old.Truncate(time.Hour)) {
		t.Fatalf("borne d'eau = %v, attendu %v", after, old.Truncate(time.Hour))
	}

	// Avant la passe suivante, la lecture agrégée doit malgré tout rester
	// correcte : la plage n'est pas close au-delà de la nouvelle borne.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: old, To: now, Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	raw := 0
	for _, b := range buckets {
		raw += b.Count
	}
	if want := 4 * 6; raw != want { // 4 heures × 6 relevés de 10 minutes
		t.Fatalf("échantillons couverts = %d, attendu %d", raw, want)
	}

	// Après la passe, les agrégats couvrent de nouveau tout l'historique.
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("Rollup de rattrapage: %v", err)
	}
	after, _, err = s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("borne d'eau: %v", err)
	}
	if !after.Equal(wm) {
		t.Fatalf("borne d'eau = %v, attendu %v après rattrapage", after, wm)
	}
	var aggCount, rawCount int
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(count_all), 0) FROM agg_hour`).Scan(&aggCount); err != nil {
		t.Fatalf("comptage des agrégats: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM samples`).Scan(&rawCount); err != nil {
		t.Fatalf("comptage des mesures: %v", err)
	}
	if aggCount != rawCount {
		t.Fatalf("agrégats couvrent %d mesures, %d conservées", aggCount, rawCount)
	}
}

// TestRollupNeverAggregatesOpenHour : une heure encore alimentée n'est jamais
// agrégée, sinon une lecture la croirait complète alors que de nouvelles
// mesures arrivent encore.
func TestRollupNeverAggregatesOpenHour(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)

	fill(t, s, base, base.Add(30*time.Minute), time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	res, err := s.Rollup(ctx, base.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.Buckets != 0 {
		t.Fatalf("Rollup a agrégé l'heure ouverte (%d buckets)", res.Buckets)
	}
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("borne d'eau écrite sans agrégat (ok=%v, err=%v)", ok, err)
	}

	// La lecture reste correcte : elle passe entièrement par le brut.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: base, To: base.Add(time.Hour), Step: time.Minute})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 30 {
		t.Fatalf("%d buckets, attendu 30", len(buckets))
	}
}

// TestRollupEmptyStoreWritesNoWatermark : sans mesure, aucune borne d'eau ne
// doit être écrite, sinon un historique importé plus tard serait lu comme déjà
// agrégé.
func TestRollupEmptyStoreWritesNoWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()

	res, err := s.Rollup(ctx, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if res.Buckets != 0 {
		t.Fatalf("buckets = %d, attendu 0", res.Buckets)
	}
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("borne d'eau écrite sur base vide (ok=%v, err=%v)", ok, err)
	}

	// Un historique importé ensuite doit être agrégé normalement.
	base := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	fill(t, s, base, base.Add(3*time.Hour), 15*time.Minute, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	if _, err := s.Rollup(ctx, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Rollup après import: %v", err)
	}
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: base, To: base.Add(3 * time.Hour), Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("%d buckets, attendu 3 : %+v", len(buckets), buckets)
	}
}

// TestInsertSampleLowersWatermarkOnlyBelow : l'abaissement ne doit se produire
// que pour une mesure réellement plus ancienne que la borne d'eau.
func TestInsertSampleLowersWatermarkOnlyBelow(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	// Sans borne d'eau, une insertion n'en crée pas.
	mustInsert(t, s, pvSample(now.Add(-5*time.Hour), new(1.0)))
	if _, ok, err := s.aggWatermark(ctx); err != nil || ok {
		t.Fatalf("borne d'eau créée par une insertion (ok=%v, err=%v)", ok, err)
	}

	if err := s.SetState(ctx, aggStateKey, formatTS(now)); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	// Mesure postérieure : la borne d'eau ne bouge pas.
	mustInsert(t, s, pvSample(now.Add(30*time.Minute), new(2.0)))
	wm, _, err := s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("aggWatermark: %v", err)
	}
	if !wm.Equal(now) {
		t.Fatalf("borne d'eau = %v, attendu %v", wm, now)
	}
	// Mesure antérieure : la borne d'eau descend à l'heure pleine inférieure.
	mustInsert(t, s, pvSample(now.Add(-90*time.Minute), new(3.0)))
	wm, _, err = s.aggWatermark(ctx)
	if err != nil {
		t.Fatalf("aggWatermark: %v", err)
	}
	if want := now.Add(-2 * time.Hour); !wm.Equal(want) {
		t.Fatalf("borne d'eau = %v, attendu %v", wm, want)
	}
}

// TestPurgeRemovesAggregatesAndRaisesWatermark vérifie que la rétention laisse
// les deux tables cohérentes : aucune ligne d'agrégat ne survit à ses mesures.
func TestPurgeRemovesAggregatesAndRaisesWatermark(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)

	fill(t, s, base, base.AddDate(0, 0, 3), time.Hour, func(i int, ts time.Time) model.Sample {
		return pvSample(ts, new(float64(i)))
	})
	now := base.AddDate(0, 0, 3).Add(30 * time.Minute)
	if _, err := s.Rollup(ctx, now); err != nil {
		t.Fatalf("Rollup: %v", err)
	}

	limit := base.AddDate(0, 0, 1)
	if _, err := s.PurgeBefore(ctx, limit); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	var before int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM agg_hour WHERE bucket_utc < ?`, formatTS(limit)).Scan(&before); err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if before != 0 {
		t.Fatalf("%d agrégats antérieurs à la borne de purge", before)
	}
	wm, ok, err := s.aggWatermark(ctx)
	if err != nil || !ok {
		t.Fatalf("borne d'eau absente après purge (ok=%v, err=%v)", ok, err)
	}
	if wm.Before(limit) {
		t.Fatalf("borne d'eau %v antérieure à la borne de purge %v", wm, limit)
	}

	// Les journées conservées restent lisibles par le chemin agrégé.
	buckets, err := s.TimeSeries(ctx, TimeSeriesQuery{From: limit, To: now, Step: time.Hour})
	if err != nil {
		t.Fatalf("TimeSeries: %v", err)
	}
	if len(buckets) != 48 {
		t.Fatalf("%d buckets, attendu 48", len(buckets))
	}
}

// TestAggSplitBoundaries fixe la règle de découpage : seules les heures
// entières contenues dans [from, to) et closes sont servies par les agrégats.
func TestAggSplitBoundaries(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 3, 29, h, m, 0, 0, time.UTC) }
	closed := at(5, 0)

	cases := []struct {
		name             string
		from, to         time.Time
		enabled          bool
		wantFrom, wantTo time.Time
		wantOK           bool
	}{
		{"plage close alignée", at(2, 0), at(5, 0), true, at(2, 0), at(5, 0), true},
		{"entête au milieu d'une heure", at(2, 30), at(5, 0), true, at(3, 0), at(5, 0), true},
		{"queue ouverte", at(2, 0), at(6, 30), true, at(2, 0), at(5, 0), true},
		{"sans heure pleine éligible", at(2, 30), at(3, 30), true, time.Time{}, time.Time{}, false},
		{"postérieure à la borne d'eau", at(6, 0), at(7, 0), true, time.Time{}, time.Time{}, false},
		{"antérieure à la borne d'eau", at(1, 0), at(1, 30), true, time.Time{}, time.Time{}, false},
		{"agrégats désactivés", at(1, 0), at(5, 0), false, time.Time{}, time.Time{}, false},
		{"plage vide", at(3, 0), at(3, 0), true, time.Time{}, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, to, ok := aggSplit(tc.from, tc.to, closed, tc.enabled)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, attendu %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Fatalf("plage = [%v, %v), attendu [%v, %v)", from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}

// TestCeilHour couvre l'arrondi supérieur utilisé par le découpage.
func TestCeilHour(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 3, 29, h, m, s, 0, time.UTC) }
	cases := []struct{ in, want time.Time }{
		{at(3, 0, 0), at(3, 0, 0)},
		{at(3, 0, 1), at(4, 0, 0)},
		{at(3, 59, 59), at(4, 0, 0)},
	}
	for _, tc := range cases {
		if got := ceilHour(tc.in); !got.Equal(tc.want) {
			t.Fatalf("ceilHour(%v) = %v, attendu %v", tc.in, got, tc.want)
		}
	}
}
