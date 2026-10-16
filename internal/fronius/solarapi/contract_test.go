// Test de contrat : les fixtures de test/fixtures/solarapi sont la forme
// [HYPOTHÈSE] de la Solar API v1 encodée dans dto.go. Toute divergence entre la
// fixture et le modèle normalisé doit être visible ici.
//
// Le test est dans le paquet externe solarapi_test : il consomme le modèle
// (qui importe solarapi) sans créer de cycle d'import.
package solarapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/model"
)

// Instants figés : les fixtures sont générées par cmd/fronius-sim --seed 42.
var (
	fixtureInstant = time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC) // 12:00 locales (+02:00)
	fixtureNow     = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "..", "test", "fixtures", "solarapi", name)
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("lecture de %s: %v", name, err)
	}
	return body
}

func fixtureServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fixtureClient(t *testing.T, srv *httptest.Server) *solarapi.Client {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := solarapi.New(srv.URL, fronius.ReadOnly{Inner: &http.Client{}}, 2*time.Second, logger)
	if err != nil {
		t.Fatalf("solarapi.New: %v", err)
	}
	return c
}

// productionInverters est l'onduleur unique rapporté par le simulateur.
func productionInverters() []model.InverterSample {
	return []model.InverterSample{{
		ID:          "1",
		DT:          1,
		PowerW:      new(float64(3251)),
		EnergyDayWh: new(9064.1),
	}}
}

func baseSample() model.Sample {
	return model.Sample{
		TS:            fixtureInstant,
		TSSource:      model.TSSourceDevice,
		Source:        model.SourceSolarAPI,
		Quality:       model.QualityMeasured,
		Mode:          "produce-only",
		PowerPVW:      new(3251.0),
		EnergyDayWh:   new(9064.1),
		EnergyYearWh:  new(4215488.4),
		EnergyTotalWh: new(14236126.1),
		Inverters:     productionInverters(),
	}
}

func TestFixtureContract(t *testing.T) {
	produceOnly := baseSample()

	withMeter := baseSample()
	withMeter.PowerGridW = new(2485.1)
	withMeter.PowerLoadW = new(765.9)
	withMeter.RelAutonomyPct = new(100.0)
	withMeter.RelSelfConsumptionPct = new(23.6)

	cases := []struct {
		name     string
		file     string
		want     model.Sample
		validate func(t *testing.T, got model.Sample)
	}{
		{name: "produce-only (sans compteur ni batterie)", file: "powerflow_produce_only.json", want: produceOnly},
		{name: "avec Smart Meter simulé", file: "powerflow_with_meter.json", want: withMeter,
			validate: func(t *testing.T, got model.Sample) {
				t.Helper()
				// P_Akku reste null : le simulateur ne modélise aucune batterie.
				if got.PowerBatteryW != nil {
					t.Fatalf("power_battery_w = %v, attendu nil", *got.PowerBatteryW)
				}
				// Cohérence : P_Grid ≈ P_PV − P_Load.
				if got.PowerGridW == nil || got.PowerPVW == nil || got.PowerLoadW == nil {
					t.Fatal("P_Grid, P_PV et P_Load doivent être renseignés")
				}
				if diff := *got.PowerPVW - *got.PowerLoadW - *got.PowerGridW; diff > 0.05 || diff < -0.05 {
					t.Fatalf("P_Grid = %v, attendu P_PV − P_Load = %v", *got.PowerGridW, *got.PowerPVW-*got.PowerLoadW)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fixtureServer(t, readFixture(t, tc.file))
			env, err := fixtureClient(t, srv).GetPowerFlowRealtimeData(t.Context())
			if err != nil {
				t.Fatalf("GetPowerFlowRealtimeData: %v", err)
			}
			got := model.FromPowerFlow(env, fixtureNow)

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("échantillon normalisé inattendu\n got: %+v\nwant: %+v", got, tc.want)
			}
			if got.TS.Location() != time.UTC {
				t.Fatalf("TS doit être en UTC, reçu %v", got.TS.Location())
			}
			if tc.validate != nil {
				tc.validate(t, got)
			}
		})
	}
}

// TestFixturesTolerateProvenanceField : le champ racine _provenance n'est pas
// consommé par le client et ne doit pas casser le décodage.
func TestFixturesTolerateProvenanceField(t *testing.T) {
	files := []string{
		"powerflow_produce_only.json",
		"powerflow_with_meter.json",
		"powerflow_status7.json",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body := readFixture(t, name)
			if !strings.Contains(string(body), "_provenance") {
				t.Fatalf("%s doit porter un champ _provenance", name)
			}
			var env solarapi.PowerFlowEnvelope
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("Unmarshal(%s): %v", name, err)
			}
			if env.Body.Data.Site.Mode == "" && env.Body.Data.Site.P_PV == nil {
				t.Fatalf("%s : aucun contenu décodé", name)
			}
		})
	}
}

func TestStatus7FixtureIsReportedAsDeviceStatus(t *testing.T) {
	srv := fixtureServer(t, readFixture(t, "powerflow_status7.json"))
	env, err := fixtureClient(t, srv).GetPowerFlowRealtimeData(t.Context())
	if err == nil {
		t.Fatal("Head.Status.Code = 7 devait produire une erreur")
	}
	kind, ok := froniuserr.KindOf(err)
	if !ok || kind != froniuserr.KindStatus {
		t.Fatalf("KindOf = (%q, %v), attendu %q", kind, ok, froniuserr.KindStatus)
	}
	var typed *froniuserr.Error
	if !errors.As(err, &typed) {
		t.Fatal("errors.As n'a pas retrouvé *froniuserr.Error")
	}
	if typed.Code != 7 || typed.Reason != "Simulated device error" {
		t.Fatalf("erreur = %+v", typed)
	}
	// Les données restent exploitables : l'appelant choisit de les ignorer.
	if env.Body.Data.Site.P_PV == nil {
		t.Fatal("l'enveloppe doit rester accessible malgré l'erreur applicative")
	}
}

func TestInvalidFixtureIsADecodeError(t *testing.T) {
	body := readFixture(t, "powerflow_invalid.json")
	if len(body) == 0 {
		t.Fatal("powerflow_invalid.json est vide")
	}

	// Décodage direct.
	var env solarapi.PowerFlowEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		t.Fatal("un corps tronqué devait échouer au décodage")
	}

	// Et via le client, avec l'erreur typée attendue.
	srv := fixtureServer(t, body)
	if _, err := fixtureClient(t, srv).GetPowerFlowRealtimeData(t.Context()); err == nil {
		t.Fatal("une erreur de décodage était attendue")
	} else if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("KindOf = %q, attendu %q", kind, froniuserr.KindDecode)
	}
}

// TestFixturesContainNoSensitiveData : ni numéro de série, ni adresse IP, ni
// identifiant ne doit se trouver dans une fixture versionnée.
func TestFixturesContainNoSensitiveData(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "test", "fixtures", "solarapi"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("aucune fixture trouvée")
	}

	forbidden := []string{
		"33319328", // numéro de série observé sur l'installation
		"192.168.",
		"255.255.",
		"password",
		"Authorization",
		"AuthHash",
		"nonce",
		"realm",
	}
	for _, entry := range entries {
		body := readFixture(t, entry.Name())
		for _, needle := range forbidden {
			if strings.Contains(string(body), needle) {
				t.Fatalf("%s contient une donnée sensible (%q)", entry.Name(), needle)
			}
		}
	}
}
