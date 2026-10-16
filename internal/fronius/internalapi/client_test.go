package internalapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

func newClient(t *testing.T, baseURL string, doer fronius.Doer) *Client {
	t.Helper()
	c, err := New(baseURL, doer, 2*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// countingDoer compte les requêtes effectivement transmises.
type countingDoer struct {
	count   atomic.Int64
	inner   fronius.Doer
	lastURL string
}

func (d *countingDoer) Do(r *http.Request) (*http.Response, error) {
	d.count.Add(1)
	return d.inner.Do(r)
}

func TestAllowlistIsExactlyFourReadEndpoints(t *testing.T) {
	list := Allowlist()
	if len(list) != 4 {
		t.Fatalf("liste blanche = %d entrées, attendu 4", len(list))
	}
	want := map[string]Envelope{
		VersionPath:      EnvelopeDefault,
		ActiveEventsPath: EnvelopeDefault,
		PowerMeterPath:   EnvelopeCgi,
		BatteryPath:      EnvelopeCgi,
	}
	for _, ep := range list {
		if ep.Class != ClassRead {
			t.Fatalf("%s : classe %q, seul READ est autorisé", ep.Path, ep.Class)
		}
		if !ep.Digest {
			t.Fatalf("%s : la voie du registre pollé est authentifiée", ep.Path)
		}
		if ep.Purpose == "" || ep.Evidence == "" {
			t.Fatalf("%s : purpose et evidence sont obligatoires", ep.Path)
		}
		expected, ok := want[ep.Path]
		if !ok {
			t.Fatalf("chemin inattendu dans la liste blanche : %s", ep.Path)
		}
		if ep.Envelope != expected {
			t.Fatalf("%s : enveloppe %q, attendue %q", ep.Path, ep.Envelope, expected)
		}
		delete(want, ep.Path)
	}
	if len(want) != 0 {
		t.Fatalf("chemins manquants : %v", want)
	}

	// La liste est une copie : la corrompre ne doit pas modifier la table.
	Allowlist()[0].Path = "/corrompu"
	if _, ok := Lookup(VersionPath); !ok {
		t.Fatal("la liste blanche a été corrompue par son appelant")
	}
}

// TestAllowlistRejectsEverythingElse : un chemin hors liste est refusé avant
// toute construction de requête.
func TestAllowlistRejectsEverythingElse(t *testing.T) {
	var doer countingDoer
	doer.inner = &http.Client{}
	c := newClient(t, "http://127.0.0.1:1", &doer)

	for _, path := range []string{
		"/config/modbus",
		"/status/events",
		"/status/devices",
		"/components/DataManager/readable",
		"/status/version/../events",
		"",
		"http://192.168.1.5/status/version",
	} {
		_, err := c.Get(context.Background(), path)
		if !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("%q : erreur = %v, attendu ErrNotAllowed", path, err)
		}
	}
	if got := doer.count.Load(); got != 0 {
		t.Fatalf("requêtes émises = %d, attendu 0", got)
	}
	if _, err := c.ComponentPresent(context.Background(), "/config/meter"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("ComponentPresent hors liste : %v", err)
	}
}

func TestGetSendsGetOnly(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL, fronius.ReadOnly{Inner: &http.Client{}})
	if _, err := c.Get(context.Background(), VersionPath); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if method != http.MethodGet || path != VersionPath {
		t.Fatalf("requête = %s %s", method, path)
	}
}

func TestVersionExtractsKnownAndCandidateKeys(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Version
	}{
		{
			name: "clés prouvées seules",
			body: `{"serialNumber":"S1","devicegroup":"GEN24PRIMO","numberOfPhases":3}`,
			want: Version{
				SerialNumber: new("S1"),
				DeviceGroup:  new("GEN24PRIMO"),
				// `devicegroup` n'est pas un modèle : c'est le groupe d'appareil,
				// un champ frère. Le modèle reste nul, jamais déduit d'un autre champ.
				Model:          nil,
				NumberOfPhases: new(3),
			},
		},
		{
			name: "version lue sur le chemin prouvé swrevisions.GEN24",
			body: `{"serialNumber":"S","swrevisions":{"GEN24":"1.19.7-1","GEN24PRIMO":"1.19.7-1"},"firmwareVersion":"0.0-inconnu"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.19.7-1")},
		},
		{
			name: "chemin prouvé insensible à la casse, objet imbriqué d'un autre nom ignoré",
			body: `{"serialNumber":"S","swrevisions":{"gen24":"1.19.7-1"},"SWRevisions":{"GEN24":"9.9.9"}}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.19.7-1")},
		},
		{
			name: "swrevisions sans GEN24 : repli sur le candidat plat",
			body: `{"serialNumber":"S","swrevisions":{"OTHER":"x"},"firmwareVersion":"1.2"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.2")},
		},
		{
			name: "swrevisions d'un autre type : ni panique ni valeur inventée",
			body: `{"serialNumber":"S","swrevisions":"1.19.7-1"}`,
			want: Version{SerialNumber: new("S")},
		},
		{
			name: "candidats du simulateur",
			body: `{"serialNumber":"SIMULATED-0001","devicegroup":"GEN24PRIMO","numberOfPhases":3,"firmwareVersion":"1.24.3-sim","modelName":"Fronius Primo (simulé)"}`,
			want: Version{
				SerialNumber:   new("SIMULATED-0001"),
				DeviceGroup:    new("GEN24PRIMO"),
				Model:          new("Fronius Primo (simulé)"),
				Firmware:       new("1.24.3-sim"),
				NumberOfPhases: new(3),
			},
		},
		{
			name: "priorité respectée : modelName avant model",
			body: `{"serialNumber":"S","model":"générique","modelName":"spécifique"}`,
			want: Version{SerialNumber: new("S"), Model: new("spécifique")},
		},
		{
			name: "priorité respectée : firmwareVersion avant firmware",
			body: `{"serialNumber":"S","firmware":"vieux","firmwareVersion":"1.24.3"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.24.3")},
		},
		{
			name: "clé vide ignorée : on passe à la suivante",
			body: `{"serialNumber":"","serial":"S2"}`,
			want: Version{SerialNumber: new("S2")},
		},
		{
			name: "casse des clés tolérée",
			body: `{"SerialNumber":"S3","Numberofphases":1}`,
			want: Version{SerialNumber: new("S3"), NumberOfPhases: new(1)},
		},
		{
			name: "nombre de phases en chaîne",
			body: `{"serialNumber":"S","numberOfPhases":"1"}`,
			want: Version{SerialNumber: new("S"), NumberOfPhases: new(1)},
		},
		{
			name: "clés inconnues tolérées, absentes restent nulles",
			body: `{"serialNumber":"S","champInconnu":{"a":1}}`,
			want: Version{SerialNumber: new("S")},
		},
		{
			name: "nombre de phases absent",
			body: `{"serialNumber":"S"}`,
			want: Version{SerialNumber: new("S")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			got, err := newClient(t, srv.URL, &http.Client{}).Version(context.Background())
			if err != nil {
				t.Fatalf("Version: %v", err)
			}
			assertVersionEqual(t, got, tc.want)
		})
	}
}

func assertVersionEqual(t *testing.T, got, want Version) {
	t.Helper()
	gotJSON, _ := json.Marshal(struct {
		A, B, C, D *string
		E          *int
	}{got.SerialNumber, got.Model, got.Firmware, got.DeviceGroup, got.NumberOfPhases})
	wantJSON, _ := json.Marshal(struct {
		A, B, C, D *string
		E          *int
	}{want.SerialNumber, want.Model, want.Firmware, want.DeviceGroup, want.NumberOfPhases})
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("Version = %s, attendu %s", gotJSON, wantJSON)
	}
}

func TestVersionDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"corps tronqué", `{"serialNumber":"S`},
		{"tableau racine", `[1,2,3]`},
		{"chaîne racine", `"texte"`},
		{"null racine", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := newClient(t, srv.URL, &http.Client{}).Version(context.Background())
			if err == nil {
				t.Fatal("une erreur de décodage était attendue")
			}
			if kind, ok := froniuserr.KindOf(err); !ok || kind != froniuserr.KindDecode {
				t.Fatalf("nature = %q (ok=%v)", kind, ok)
			}
		})
	}
}

func TestComponentPresence(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"Data vide : absent", `{"Body":{"Data":{}}}`, false},
		{"Data nul : absent", `{"Body":{"Data":null}}`, false},
		{"Body sans Data : absent", `{"Body":{}}`, false},
		{"un composant : présent", `{"Body":{"Data":{"0":{"Serial":"SIM-METER-0"}}}}`, true},
		{"deux composants : présent", `{"Body":{"Data":{"0":{},"1":{}}}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			got, err := newClient(t, srv.URL, &http.Client{}).ComponentPresent(context.Background(), PowerMeterPath)
			if err != nil {
				t.Fatalf("ComponentPresent: %v", err)
			}
			if got != tc.want {
				t.Fatalf("présence = %v, attendue %v", got, tc.want)
			}
		})
	}
}

// TestComponentPresentNeverConfusesErrorWithAbsence : un corps illisible est
// une erreur de décodage, jamais « absent ». Une réponse qu'on ne sait pas lire
// ne prouve aucune absence.
func TestComponentPresentNeverConfusesErrorWithAbsence(t *testing.T) {
	for _, body := range []string{`{"Body":{"Data":`, `{"Body":{"Data":`, ``, `{"Body":{"Data":[1,2]}}`, `{"autre":"forme"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		_, err := newClient(t, srv.URL, &http.Client{}).ComponentPresent(context.Background(), PowerMeterPath)
		srv.Close()

		if err == nil {
			t.Fatalf("corps %q : une erreur était attendue, pas une absence", body)
		}
		if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
			t.Fatalf("corps %q : nature = %q", body, kind)
		}
	}
}

func TestComponentPresentRejectsNonCgiPath(t *testing.T) {
	c := newClient(t, "http://127.0.0.1:1", &http.Client{})
	_, err := c.ComponentPresent(context.Background(), VersionPath)
	if err == nil {
		t.Fatal("un chemin à enveloppe Default n'est pas un composant")
	}
}

func TestActiveEventsForms(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "objet indexé, clés triées",
			body: `{"Body":{"Data":{"1":{"code":"B"},"0":{"code":"A"}}}}`,
			want: []string{`{"code":"A"}`, `{"code":"B"}`},
		},
		{
			name: "aucun événement",
			body: `{"Body":{"Data":{}}}`,
			want: []string{},
		},
		{
			name: "Data nul : aucun événement",
			body: `{"Body":{"Data":null}}`,
			want: []string{},
		},
		{
			name: "repli sur tableau racine",
			body: `[{"code":"A"},{"code":"B"}]`,
			want: []string{`{"code":"A"}`, `{"code":"B"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			got, err := newClient(t, srv.URL, &http.Client{}).ActiveEvents(context.Background())
			if err != nil {
				t.Fatalf("ActiveEvents: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("événements = %d, attendu %d", len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if compactJSON(t, got[i]) != want {
					t.Fatalf("événement %d = %s, attendu %s", i, got[i], want)
				}
			}
		})
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("Compact(%s): %v", raw, err)
	}
	return buf.String()
}

func TestActiveEventsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"Body":{"Data":`)
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL, &http.Client{}).ActiveEvents(context.Background())
	if err == nil {
		t.Fatal("une erreur de décodage était attendue")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("nature = %q", kind)
	}
}

func TestStatusCodesAreClassified(t *testing.T) {
	cases := []struct {
		status int
		want   froniuserr.Kind
	}{
		{http.StatusNotFound, froniuserr.KindNotFound},
		{http.StatusUnauthorized, froniuserr.KindUnauthorized},
		{http.StatusForbidden, froniuserr.KindUnauthorized},
		{http.StatusInternalServerError, froniuserr.KindServer},
		{http.StatusServiceUnavailable, froniuserr.KindServer},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		_, err := newClient(t, srv.URL, &http.Client{}).Get(context.Background(), VersionPath)
		srv.Close()

		if err == nil {
			t.Fatalf("status %d : erreur attendue", tc.status)
		}
		if kind, _ := froniuserr.KindOf(err); kind != tc.want {
			t.Fatalf("status %d : nature = %q, attendue %q", tc.status, kind, tc.want)
		}
	}
}

func TestOversizedBodyIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		big := strings.Repeat("a", maxBodyBytes+10)
		_, _ = io.WriteString(w, `{"serialNumber":"`+big+`"}`)
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL, &http.Client{}).Version(context.Background())
	if err == nil {
		t.Fatal("un corps hors limite doit être refusé")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("nature = %q", kind)
	}
}

func TestNewValidatesBaseURL(t *testing.T) {
	for _, url := range []string{"ftp://x/y", "127.0.0.1:8081", ""} {
		if _, err := New(url, &http.Client{}, time.Second, nil); err == nil {
			t.Fatalf("URL %q devait être refusée", url)
		}
	}
}
