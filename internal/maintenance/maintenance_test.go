package maintenance

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// fakeStore enregistre les appels de maintenance et permet de les faire échouer.
type fakeStore struct {
	mu        sync.Mutex
	rollups   []time.Time
	purges    []time.Time
	events    []model.Event
	rollupErr error
	purgeErr  error
}

func (f *fakeStore) Rollup(_ context.Context, now time.Time) (storage.RollupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollups = append(f.rollups, now)
	if f.rollupErr != nil {
		return storage.RollupResult{}, f.rollupErr
	}
	to := now.UTC().Truncate(time.Hour)
	return storage.RollupResult{From: to.Add(-time.Hour), To: to, Buckets: 1, Watermark: to}, nil
}

func (f *fakeStore) PurgeBefore(_ context.Context, t time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purges = append(f.purges, t)
	if f.purgeErr != nil {
		return 0, f.purgeErr
	}
	return 42, nil
}

func (f *fakeStore) InsertEvent(_ context.Context, e model.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeStore) counts() (rollups, purges int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rollups), len(f.purges)
}

func (f *fakeStore) purgeInstants() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.purges...)
}

// clock est une horloge pilotée par le test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func brussels(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return loc
}

// waitFor attend qu'une condition devienne vraie, avec une échéance.
func waitFor(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition jamais satisfaite : %s", msg)
}

// TestRetentionLimitAlignsToHour : la borne de purge est minuit local arrondi à
// l'heure pleine supérieure, sans quoi les agrégats et les mesures ne
// partageraient pas la même frontière.
func TestRetentionLimitAlignsToHour(t *testing.T) {
	loc := brussels(t)
	now := time.Date(2026, 10, 16, 14, 37, 0, 0, loc)

	got := retentionLimit(now, loc, 7)
	want := time.Date(2026, 10, 9, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("borne = %v, attendu %v", got, want)
	}
	if got.UTC().Truncate(time.Hour).Before(got) {
		t.Fatalf("borne %v non alignée sur l'heure UTC", got.UTC())
	}

	// Fuseau décalé d'une demi-heure : minuit local n'est pas aligné sur
	// l'heure UTC, l'arrondi doit remonter d'une heure.
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	got = retentionLimit(time.Date(2026, 10, 16, 14, 0, 0, 0, kolkata), kolkata, 1)
	if got.UTC().Truncate(time.Hour).Before(got.UTC()) {
		t.Fatalf("borne %v non alignée sur l'heure UTC", got.UTC())
	}
	if want := time.Date(2026, 10, 15, 0, 0, 0, 0, kolkata); got.Before(want) {
		t.Fatalf("borne %v antérieure à minuit local %v", got, want)
	}
	if d := got.Sub(time.Date(2026, 10, 15, 0, 0, 0, 0, kolkata)); d >= time.Hour {
		t.Fatalf("borne %v trop éloignée de minuit local (%v)", got, d)
	}

	// Rétention nulle : minuit local du jour même.
	got = retentionLimit(now, loc, 0)
	if want := time.Date(2026, 10, 16, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("borne = %v, attendu %v", got, want)
	}
}

// TestRunPurgesOncePerLocalDay vérifie qu'une purge a lieu au démarrage, puis
// une seule fois par journée locale, à partir de 3 h.
func TestRunPurgesOncePerLocalDay(t *testing.T) {
	loc := brussels(t)
	store := &fakeStore{}
	c := &clock{t: time.Date(2026, 10, 16, 1, 30, 0, 0, loc)}
	r := &Runner{
		Store:         store,
		Loc:           loc,
		RetentionDays: 30,
		Tick:          5 * time.Millisecond,
		Now:           c.now,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Purge au démarrage (comportement conservé de la phase 1).
	waitFor(t, "purge au démarrage", func() bool { _, p := store.counts(); return p == 1 })

	// Même journée, avant 3 h puis après : aucune purge supplémentaire.
	time.Sleep(20 * time.Millisecond)
	c.set(time.Date(2026, 10, 16, 3, 5, 0, 0, loc))
	time.Sleep(20 * time.Millisecond)
	if _, p := store.counts(); p != 1 {
		t.Fatalf("%d purges le jour du démarrage, attendu 1", p)
	}

	// Journée suivante après 3 h : une purge.
	c.set(time.Date(2026, 10, 17, 3, 0, 0, 0, loc))
	waitFor(t, "purge du lendemain", func() bool { _, p := store.counts(); return p == 2 })

	// La borne appliquée est bien celle de la rétention configurée.
	if got, want := store.purgeInstants()[1], retentionLimit(c.now(), loc, 30); !got.Equal(want) {
		t.Fatalf("borne purgée = %v, attendu %v", got, want)
	}
	// L'événement de purge est enregistré.
	store.mu.Lock()
	events := len(store.events)
	kind := ""
	if events > 0 {
		kind = string(store.events[0].Kind)
	}
	store.mu.Unlock()
	if events == 0 || kind != string(model.EventPurge) {
		t.Fatalf("événements de purge = %d (premier %q)", events, kind)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, attendu nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run n'a pas rendu la main après annulation")
	}
}

// TestRunWithoutRetentionNeverPurges : sans rétention configurée, aucune purge
// n'a lieu, mais l'agrégation continue.
func TestRunWithoutRetentionNeverPurges(t *testing.T) {
	store := &fakeStore{}
	r := &Runner{Store: store, Loc: time.UTC, RetentionDays: 0, Tick: 5 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	waitFor(t, "agrégations répétées", func() bool {
		rollups, _ := store.counts()
		return rollups >= 2
	})
	if _, p := store.counts(); p != 0 {
		t.Fatalf("%d purges malgré une rétention illimitée", p)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, attendu nil", err)
	}
}

// TestRunSurvivesErrors : une panne d'un travail n'arrête pas la boucle.
func TestRunSurvivesErrors(t *testing.T) {
	store := &fakeStore{rollupErr: errors.New("base occupée"), purgeErr: errors.New("disque plein")}
	r := &Runner{
		Store:         store,
		Loc:           time.UTC,
		RetentionDays: 7,
		Tick:          5 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	waitFor(t, "agrégations malgré l'erreur", func() bool {
		rollups, _ := store.counts()
		return rollups >= 3
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, attendu nil", err)
	}
}

// TestRollupOnceWithRealStore vérifie le câblage réel : la passe écrit des
// agrégats dans la base.
func TestRollupOnceWithRealStore(t *testing.T) {
	loc := brussels(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "fronius.db"), loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	for i := range 24 {
		ts := base.Add(time.Duration(i) * 15 * time.Minute)
		sm := model.Sample{
			TS: ts, TSSource: model.TSSourceDevice, Source: model.SourceSolarAPI,
			Quality: model.QualityMeasured, PowerPVW: new(float64(i) * 10),
		}
		if err := store.InsertSample(ctx, sm); err != nil {
			t.Fatalf("InsertSample: %v", err)
		}
	}

	r := &Runner{Store: store, Loc: loc, Now: func() time.Time { return base.Add(7 * time.Hour) }}
	res := r.RollupOnce(ctx)
	if res.Buckets == 0 {
		t.Fatal("aucun agrégat écrit par la passe réelle")
	}
	var rows int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM agg_hour`).Scan(&rows); err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if rows == 0 {
		t.Fatal("agg_hour vide après la passe")
	}
}
