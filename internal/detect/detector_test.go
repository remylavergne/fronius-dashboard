package detect

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// fakeReader est une source de détection en mémoire.
type fakeReader struct {
	version    internalapi.Version
	versionErr error

	components map[string]bool
	compErr    map[string]error

	events    []json.RawMessage
	eventsErr error

	mu           sync.Mutex
	versionCalls int
	compCalls    map[string]int
	eventCalls   int
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		components: map[string]bool{},
		compErr:    map[string]error{},
		compCalls:  map[string]int{},
	}
}

func (f *fakeReader) Version(context.Context) (internalapi.Version, error) {
	f.mu.Lock()
	f.versionCalls++
	f.mu.Unlock()
	return f.version, f.versionErr
}

func (f *fakeReader) ComponentPresent(_ context.Context, path string) (bool, error) {
	f.mu.Lock()
	f.compCalls[path]++
	err := f.compErr[path]
	f.mu.Unlock()
	if err != nil {
		return false, err
	}
	return f.components[path], nil
}

func (f *fakeReader) ActiveEvents(context.Context) ([]json.RawMessage, error) {
	f.mu.Lock()
	f.eventCalls++
	f.mu.Unlock()
	return f.events, f.eventsErr
}

func (f *fakeReader) calls() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.versionCalls, f.compCalls[internalapi.PowerMeterPath], f.eventCalls
}

// fakeSink est un magasin en mémoire.
type fakeSink struct {
	mu        sync.Mutex
	devices   []storage.DeviceRecord
	events    []model.Event
	seen      map[string]bool
	state     map[string]string
	saveErr   error
	stateErr  error
	insertErr error
}

func newFakeSink() *fakeSink {
	return &fakeSink{seen: map[string]bool{}, state: map[string]string{}}
}

func (s *fakeSink) SaveDevice(_ context.Context, d storage.DeviceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.devices = append(s.devices, d)
	return nil
}

func (s *fakeSink) MarkDeviceEventSeen(_ context.Context, key string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

func (s *fakeSink) InsertEvent(_ context.Context, e model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return s.insertErr
	}
	s.events = append(s.events, e)
	return nil
}

func (s *fakeSink) SetState(_ context.Context, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stateErr != nil {
		return s.stateErr
	}
	s.state[k] = v
	return nil
}

func (s *fakeSink) snapshot() ([]storage.DeviceRecord, []model.Event, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]model.Event(nil), s.events...)
	devices := append([]storage.DeviceRecord(nil), s.devices...)
	state := map[string]string{}
	for k, v := range s.state {
		state[k] = v
	}
	return devices, events, state
}

// fullyDetected configure une source où tout est présent.
func fullyDetected(events ...string) *fakeReader {
	f := newFakeReader()
	f.version = internalapi.Version{
		SerialNumber:   new("SIMULATED-0001"),
		Model:          new("Primo GEN24"),
		Firmware:       new("1.24.3-sim"),
		NumberOfPhases: new(3),
	}
	f.components[internalapi.PowerMeterPath] = true
	f.components[internalapi.BatteryPath] = false
	for _, raw := range events {
		f.events = append(f.events, json.RawMessage(raw))
	}
	return f
}

func newDetector(t *testing.T, reader Reader, sink Sink) *Detector {
	t.Helper()
	return &Detector{
		Source:   reader,
		Sink:     sink,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval: time.Minute,
		Now:      func() time.Time { return time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC) },
	}
}

func TestDetectOnceSuccess(t *testing.T) {
	reader := fullyDetected(`{"code":"SIM-0","state":"active","severity":"warning","message":"Événement simulé 0"}`)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	if got.Identity.State != model.StatePresent || got.Identity.Source != internalapi.VersionPath {
		t.Fatalf("identité = %+v", got.Identity)
	}
	if got.Meter.State != model.StatePresent {
		t.Fatalf("compteur = %+v", got.Meter)
	}
	if got.Battery.State != model.StateAbsent {
		t.Fatalf("batterie = %+v", got.Battery)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("événements = %+v", got.DeviceEvents)
	}

	devices, events, state := sink.snapshot()
	if len(devices) != 1 {
		t.Fatalf("SaveDevice appelé %d fois, attendu 1", len(devices))
	}
	rec := devices[0]
	if rec.Model != "Primo GEN24" || rec.Firmware != "1.24.3-sim" || rec.SerialNumber != "SIMULATED-0001" {
		t.Fatalf("enregistrement = %+v", rec)
	}
	if rec.MeterState != model.StatePresent || rec.BatteryState != model.StateAbsent ||
		rec.DeviceEventsState != model.StatePresent || rec.IdentityState != model.StatePresent {
		t.Fatalf("états enregistrés = %+v", rec)
	}
	if rec.Source != internalapi.VersionPath ||
		rec.MeterSource != internalapi.PowerMeterPath ||
		rec.BatterySource != internalapi.BatteryPath {
		t.Fatalf("sources = %+v", rec)
	}
	if !rec.ReadAtUTC.Equal(d.Now()) {
		t.Fatalf("read_at_utc = %v", rec.ReadAtUTC)
	}

	if len(events) != 1 || events[0].Kind != model.EventDevice {
		t.Fatalf("événements insérés = %+v", events)
	}
	if events[0].Severity != model.SeverityWarning || events[0].Message != "Événement simulé 0" {
		t.Fatalf("événement = %+v", events[0])
	}
	if !strings.Contains(events[0].Detail, "SIM-0") {
		t.Fatalf("le détail doit conserver le JSON brut : %q", events[0].Detail)
	}
	if state[StateLastError] != "" {
		t.Fatalf("detect_last_error = %q", state[StateLastError])
	}
	if state[StateLastReadUTC] == "" {
		t.Fatal("detect_last_read_utc doit être renseigné")
	}
}

func TestDetectOnceUnauthorizedWithCredentials(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.VersionPath}
	reader.compErr[internalapi.PowerMeterPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.PowerMeterPath}
	reader.compErr[internalapi.BatteryPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.BatteryPath}
	reader.eventsErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.ActiveEventsPath}

	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	d.CredentialsConfigured = true

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	for name, r := range map[string]model.ResourceDetection{
		"identité": got.Identity, "compteur": got.Meter, "batterie": got.Battery, "événements": got.DeviceEvents,
	} {
		if r.State != model.StateUnknown {
			t.Fatalf("%s : état = %q, attendu unknown", name, r.State)
		}
		if r.Reason != ReasonUnauthorized {
			t.Fatalf("%s : motif = %q", name, r.Reason)
		}
	}

	// Avec identifiants, l'état est un vrai doute : les chemins restent interrogés
	// à chaque cycle (aucun abandon).
	before, _, eventsBefore := reader.calls()
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	after, _, eventsAfter := reader.calls()
	if after != before+1 || eventsAfter != eventsBefore+1 {
		t.Fatalf("les chemins doivent rester interrogés (version %d→%d, événements %d→%d)",
			before, after, eventsBefore, eventsAfter)
	}
	_, _, state := sink.snapshot()
	if !strings.Contains(state[StateLastError], "401") {
		t.Fatalf("detect_last_error = %q", state[StateLastError])
	}
}

// TestDetectOnceUnauthorizedWithoutCredentialsStopsPolling : un 401 reçu sans
// identifiants ne doit pas être réémis à chaque cycle.
func TestDetectOnceUnauthorizedWithoutCredentialsStopsPolling(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.VersionPath}
	reader.compErr[internalapi.PowerMeterPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.PowerMeterPath}
	reader.compErr[internalapi.BatteryPath] = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.BatteryPath}
	reader.eventsErr = &froniuserr.Error{Kind: froniuserr.KindUnauthorized, HTTPStatus: 401, Endpoint: internalapi.ActiveEventsPath}

	sink := newFakeSink()
	d := newDetector(t, reader, sink) // CredentialsConfigured = false

	first, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	for name, r := range map[string]model.ResourceDetection{
		"identité": first.Identity, "compteur": first.Meter, "batterie": first.Battery, "événements": first.DeviceEvents,
	} {
		if r.State != model.StateUnknown || r.Reason != ReasonNoCredentials {
			t.Fatalf("%s : %+v, attendu unknown avec le motif des identifiants absents", name, r)
		}
	}

	// Second cycle : plus aucune requête.
	before, beforeMeter, beforeEvents := reader.calls()
	second, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	after, afterMeter, afterEvents := reader.calls()
	if after != before || afterMeter != beforeMeter || afterEvents != beforeEvents {
		t.Fatalf("des requêtes ont été réémises (version %d→%d, compteur %d→%d, événements %d→%d)",
			before, after, beforeMeter, afterMeter, beforeEvents, afterEvents)
	}
	if second.Identity.State != model.StateUnknown || second.Identity.Reason != ReasonNoCredentials {
		t.Fatalf("seconde identité = %+v", second.Identity)
	}
}

func TestDetectOnceNotFound(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = &froniuserr.Error{Kind: froniuserr.KindNotFound, HTTPStatus: 404, Endpoint: internalapi.VersionPath}
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.Identity.State != model.StateUnknown || got.Identity.Reason != ReasonEndpointAbsent {
		t.Fatalf("identité = %+v", got.Identity)
	}
	if got.Identity.Reason == ReasonNoCredentials {
		t.Fatal("un 404 n'est pas un problème d'identifiants")
	}
}

func TestDetectOnceTransportError(t *testing.T) {
	reader := newFakeReader()
	reader.versionErr = errors.New("connexion refusée")
	reader.eventsErr = errors.New("délai dépassé")
	reader.components[internalapi.PowerMeterPath] = true
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.Identity.State != model.StateUnknown || !strings.Contains(got.Identity.Reason, "connexion refusée") {
		t.Fatalf("identité = %+v", got.Identity)
	}
	if got.DeviceEvents.State != model.StateUnknown {
		t.Fatalf("événements = %+v", got.DeviceEvents)
	}
	if got.Meter.State != model.StatePresent {
		t.Fatalf("le compteur doit rester indépendant de la panne : %+v", got.Meter)
	}

	devices, _, state := sink.snapshot()
	if len(devices) != 1 {
		t.Fatalf("SaveDevice appelé %d fois", len(devices))
	}
	if state[StateLastError] == "" {
		t.Fatal("detect_last_error doit être renseigné")
	}
}

// TestEventDeduplication : le flux est republié en entier à chaque cycle ; un
// événement déjà vu ne produit aucune écriture.
func TestEventDeduplication(t *testing.T) {
	const first = `{"code":"SIM-0","state":"active","severity":"warning","message":"Événement simulé 0"}`
	reader := fullyDetected(first)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}

	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("événements insérés = %d, attendu 1 (anti-duplication)", len(events))
	}

	// Un événement neuf au cycle suivant est inséré.
	reader.mu.Lock()
	reader.events = append(reader.events, json.RawMessage(`{"code":"SIM-1","state":"active","message":"Événement simulé 1"}`))
	reader.mu.Unlock()

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	_, events, _ = sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("événements insérés = %d, attendu 2", len(events))
	}
	if events[1].Severity != model.SeverityInfo {
		t.Fatalf("sévérité de l'événement sans indication = %q", events[1].Severity)
	}
}

func TestEventWithoutTextOrCode(t *testing.T) {
	// Ni code, ni state, ni message : la clé est l'empreinte du JSON, et le
	// message est neutre.
	reader := fullyDetected(`{"state":{"nested":true}}`)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("événements = %+v", got.DeviceEvents)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("événements = %d", len(events))
	}
	if events[0].Message != deviceEventMessage {
		t.Fatalf("message = %q", events[0].Message)
	}

	sink.mu.Lock()
	keys := len(sink.seen)
	sink.mu.Unlock()
	if keys != 1 {
		t.Fatalf("clés = %d", keys)
	}
}

func TestNoEventsIsPresentNotUnknown(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	got, err := d.DetectOnce(context.Background())
	if err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	if got.DeviceEvents.State != model.StatePresent {
		t.Fatalf("aucun événement actif ne doit pas être un doute : %+v", got.DeviceEvents)
	}
	devices, events, _ := sink.snapshot()
	if len(events) != 0 {
		t.Fatalf("événements = %d, attendu 0", len(events))
	}
	if devices[0].DeviceEventsState != model.StatePresent {
		t.Fatal("la lecture aboutie doit être enregistrée « présent »")
	}
}

func TestEventKeyPriorities(t *testing.T) {
	key, err := eventKey(json.RawMessage(`{"code":"C1","state":"S1"}`))
	if err != nil || key != "device:C1" {
		t.Fatalf("clé = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"state":"S1","message":"m"}`))
	if err != nil || key != "device:S1" {
		t.Fatalf("clé = %q (err=%v)", key, err)
	}
	key, err = eventKey(json.RawMessage(`{"message":"m","severity":"warning"}`))
	if err != nil || !strings.HasPrefix(key, "device:") || len(key) != len("device:")+16 {
		t.Fatalf("clé = %q (err=%v)", key, err)
	}
	if _, err := eventKey(json.RawMessage(`{pas du json`)); err == nil {
		t.Fatal("un JSON illisible doit produire une erreur")
	}
}

func TestSeverityMapping(t *testing.T) {
	cases := map[string]model.Severity{
		`{"severity":"warning"}`: model.SeverityWarning,
		`{"severity":"WARN"}`:    model.SeverityWarning,
		`{"severity":"error"}`:   model.SeverityError,
		`{"severity":"fault"}`:   model.SeverityError,
		`{"severity":"alert"}`:   model.SeverityError,
		`{"level":"warn"}`:       model.SeverityWarning,
		`{"severity":"info"}`:    model.SeverityInfo,
		`{"severity":"notice"}`:  model.SeverityInfo,
		`{}`:                     model.SeverityInfo,
		`{"severity":3}`:         model.SeverityInfo,
	}
	for body, want := range cases {
		var fields map[string]any
		if err := json.Unmarshal([]byte(body), &fields); err != nil {
			t.Fatalf("Unmarshal(%s): %v", body, err)
		}
		if got := severityOf(fields); got != want {
			t.Fatalf("severityOf(%s) = %q, attendu %q", body, got, want)
		}
	}
}

func TestDetailIsBounded(t *testing.T) {
	long := `{"code":"SIM-LONG","padding":"` + strings.Repeat("é", 4000) + `"}`
	reader := fullyDetected(long)
	sink := newFakeSink()
	d := newDetector(t, reader, sink)

	if _, err := d.DetectOnce(context.Background()); err != nil {
		t.Fatalf("DetectOnce: %v", err)
	}
	_, events, _ := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("événements = %d", len(events))
	}
	if len(events[0].Detail) > maxDetailBytes {
		t.Fatalf("détail = %d octets", len(events[0].Detail))
	}
	if strings.ContainsRune(events[0].Detail, '\uFFFD') {
		t.Fatal("la troncature a cassé un caractère UTF-8")
	}
}

func TestPersistenceErrorsAreReportedNotFatal(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	sink.saveErr = errors.New("disque plein")
	d := newDetector(t, reader, sink)

	_, err := d.DetectOnce(context.Background())
	if err == nil {
		t.Fatal("une erreur de persistance doit être remontée")
	}
	if !strings.Contains(err.Error(), "disque plein") {
		t.Fatalf("erreur = %v", err)
	}
}

// TestRunStopsOnContextCancellation : l'arrêt est un cas nominal.
func TestRunStopsOnContextCancellation(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	d := newDetector(t, reader, sink)
	d.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	// Laisse tourner au moins un cycle.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run a retourné %v, attendu nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run ne s'est pas arrêté")
	}

	devices, _, _ := sink.snapshot()
	if len(devices) == 0 {
		t.Fatal("au moins un cycle devait s'exécuter")
	}
}

// TestRunSurvivesPersistenceErrors : une erreur d'un cycle n'arrête pas la boucle.
func TestRunSurvivesPersistenceErrors(t *testing.T) {
	reader := fullyDetected()
	sink := newFakeSink()
	sink.saveErr = errors.New("disque plein")
	d := newDetector(t, reader, sink)
	d.Interval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run a retourné %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run ne s'est pas arrêté")
	}

	count, _, _ := reader.calls()
	if count < 2 {
		t.Fatalf("cycles exécutés = %d, attendu au moins 2 (la boucle a dû s'arrêter)", count)
	}
}
