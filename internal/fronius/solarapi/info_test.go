// Tests of the three requests that describe the installation.
//
// The shapes are the ones the survey of 2026-10-16 observed on firmware
// 1.19.7-1 with the paths the specification gives (`42,0410,2012`), and the
// classes are the ones it lists: what matters here is that an absent statement
// stays absent — a class the device did not report must not read as an empty
// list of its own — and that no caller can compose a query.
package solarapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/solarapi"
)

// infoServer answers the three paths and records the queries it received.
func infoServer(t *testing.T, bodies map[string]string) (*solarapi.Client, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c, err := solarapi.New(srv.URL, nil, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &queries
}

// The observed answer of GetInverterInfo on GEN24: one inverter, `DT` present
// but documented as "not valid for Inverters on GEN24/Tauro/Verto", a serial in
// UniqueID and the state as both a word and a code.
const inverterInfoBody = `{
  "Body": {"Data": {"1": {
    "CustomName": "tr-3-pn-01", "DT": 1, "ErrorCode": 0, "InverterState": "Sleeping",
    "PVPower": 500, "Show": 1, "StatusCode": 13, "UniqueID": "2909100097509000"
  }}},
  "Head": {"Status": {"Code": 0, "Reason": "", "UserMessage": ""}, "Timestamp": "2026-10-16T20:32:55+00:00"}
}`

func TestInverterInfoKeepsTheObservedIdentity(t *testing.T) {
	c, queries := infoServer(t, map[string]string{solarapi.InverterInfoPath: inverterInfoBody})

	inverters, err := c.InverterInfo(context.Background())
	if err != nil {
		t.Fatalf("InverterInfo: %v", err)
	}
	if len(*queries) != 1 || !strings.HasPrefix((*queries)[0], solarapi.InverterInfoPath) {
		t.Fatalf("requests = %v", *queries)
	}
	if len(inverters) != 1 {
		t.Fatalf("inverters = %+v", inverters)
	}
	inv := inverters[0]
	if inv.Index != "1" {
		t.Errorf("index = %q", inv.Index)
	}
	for name, got := range map[string]*string{"CustomName": inv.CustomName, "UniqueID": inv.UniqueID, "InverterState": inv.InverterState} {
		if got == nil {
			t.Errorf("%s is nil: the answer carries it", name)
		}
	}
	if inv.UniqueID == nil || *inv.UniqueID != "2909100097509000" {
		t.Errorf("UniqueID = %v, want the serial the device reports", inv.UniqueID)
	}
	if inv.StatusCode == nil || *inv.StatusCode != 13 {
		t.Errorf("StatusCode = %v, want 13 (Sleeping)", inv.StatusCode)
	}
	if inv.PVPower == nil || *inv.PVPower != 500 {
		t.Errorf("PVPower = %v", inv.PVPower)
	}
}

func TestInverterInfoReportsWhatItCannotRead(t *testing.T) {
	c, _ := infoServer(t, map[string]string{
		solarapi.InverterInfoPath: `{"Body":{"Data":{"1":"not an object","2":{"UniqueID":"x"}}}}`,
	})

	inverters, err := c.InverterInfo(context.Background())
	if err != nil {
		t.Fatalf("InverterInfo: %v", err)
	}
	// An unreadable entry stays an entry: dropping it would read as "no
	// inverter", which is a different statement.
	if len(inverters) != 2 {
		t.Fatalf("inverters = %+v, want both entries", inverters)
	}
	if inverters[0].Index != "1" || inverters[0].UniqueID != nil {
		t.Errorf("unreadable entry = %+v", inverters[0])
	}
	if inverters[1].UniqueID == nil || *inverters[1].UniqueID != "x" {
		t.Errorf("second entry = %+v", inverters[1])
	}
}

func TestInverterInfoRefusesADeviceStatusError(t *testing.T) {
	c, _ := infoServer(t, map[string]string{
		solarapi.InverterInfoPath: `{"Body":{"Data":{}},"Head":{"Status":{"Code":11,"Reason":"not supported"}}}`,
	})

	if _, err := c.InverterInfo(context.Background()); err == nil {
		t.Fatal("a non-zero Status.Code was accepted as an answer")
	}
}

const activeDevicesBody = `{
  "Body": {"Data": {
    "Inverter": {"1": {"DT": 1, "Serial": "2909100097509000"}},
    "Meter": {"0": {"DT": -1, "Serial": "16420055"}},
    "StringControl": {}
  }},
  "Head": {"RequestArguments": {"DeviceClass": "System"}, "Status": {"Code": 0}}
}`

func TestActiveDevicesKeepsTheClassesAndTheGaps(t *testing.T) {
	c, queries := infoServer(t, map[string]string{solarapi.ActiveDeviceInfoPath: activeDevicesBody})

	inventory, err := c.ActiveDevices(context.Background(), solarapi.SystemDeviceClass)
	if err != nil {
		t.Fatalf("ActiveDevices: %v", err)
	}
	if len(*queries) != 1 || !strings.HasSuffix((*queries)[0], "DeviceClass=System") {
		t.Fatalf("requests = %v, want the frozen DeviceClass query", *queries)
	}

	if n := inventory.Count("Meter"); n != 1 {
		t.Errorf("meters = %d, want the one the device reported", n)
	}
	if entry := inventory.Classes["Meter"][0]; entry.Serial == nil || *entry.Serial != "16420055" {
		t.Errorf("meter entry = %+v", entry)
	}
	// A class the device did not report is NOT an empty class: it is the
	// absence that says "no battery installed".
	if _, ok := inventory.Classes["Storage"]; ok {
		t.Error("Storage is present although the answer does not mention it")
	}
	if n := inventory.Count("Storage"); n != 0 {
		t.Errorf("Storage count = %d", n)
	}
	// A reported class with no device is an empty list, which is a statement.
	if entries, ok := inventory.Classes["StringControl"]; !ok || len(entries) != 0 {
		t.Errorf("StringControl = %v (present=%v)", entries, ok)
	}
}

func TestActiveDevicesRefusesAClassOutsideTheSpecification(t *testing.T) {
	c, queries := infoServer(t, map[string]string{solarapi.ActiveDeviceInfoPath: activeDevicesBody})

	if _, err := c.ActiveDevices(context.Background(), "Battery"); err == nil {
		t.Fatal("a class the specification does not list was accepted")
	}
	if len(*queries) != 0 {
		t.Fatalf("a request was emitted for a refused class: %v", *queries)
	}
}

// GetAPIVersion is the one request whose answer is not enveloped.
const apiVersionBody = `{"APIVersion": 1, "BaseURL": "/solar_api/v1/", "CompatibilityRange": "1.5-9"}`

func TestAPIVersionReadsTheBareAnswer(t *testing.T) {
	c, _ := infoServer(t, map[string]string{solarapi.APIVersionPath: apiVersionBody})

	got, err := c.APIVersion(context.Background())
	if err != nil {
		t.Fatalf("APIVersion: %v", err)
	}
	if got.Version == nil || *got.Version != 1 {
		t.Errorf("version = %v", got.Version)
	}
	if got.BaseURL == nil || *got.BaseURL != "/solar_api/v1/" {
		t.Errorf("base url = %v, want the path the device serves", got.BaseURL)
	}
	if got.CompatibilityRange == nil || *got.CompatibilityRange != "1.5-9" {
		t.Errorf("compatibility range = %v", got.CompatibilityRange)
	}
}
