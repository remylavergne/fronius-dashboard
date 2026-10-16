// Tests of the readable-cache extraction.
//
// The cache is where the device speaks its own language: names it chose, a type
// suffix it appended, and identifiers it gives no name at all. The extraction
// must keep all three as they are — naming a bare identifier or re-typing a
// 64-bit counter would be inventing what the device did not say.
package internalapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// cacheBody is trimmed from the answer observed on firmware 1.19.7-1: one node,
// two named registers of different types, one bare identifier, one attribute.
const cacheBody = `{
  "Body": {"Data": {
    "393216": {
      "attributes": {
        "createTS": "1603034125",
        "inverter": {"devicetype": "Primo", "nodetype": "inverter"}
      },
      "channels": {
        "PV_VOLTAGE_MEAN_01_F32": 612.5,
        "PV_ENERGYACTIVE_ACTIVE_SUM_01_U64": 15089627579,
        "<1090584584>": 0
      }
    }
  }},
  "Head": {"Status": {"Code": 0}}
}`

func cacheClient(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = io.WriteString(w, cacheBody)
	}))
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, nil, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, &paths
}

func TestCacheKeepsTheDeviceVocabulary(t *testing.T) {
	c, paths := cacheClient(t)

	nodes, err := c.Cache(context.Background())
	if err != nil {
		t.Fatalf("Cache: %v", err)
	}
	if len(*paths) != 1 || (*paths)[0] != CachePath {
		t.Fatalf("requests = %v, want the cache path", *paths)
	}
	if len(nodes) != 1 || nodes[0].ID != "393216" {
		t.Fatalf("nodes = %+v, want the one the device reports", nodes)
	}
	node := nodes[0]

	// The attributes keep the device's own labels and order.
	if len(node.Attributes) != 2 {
		t.Fatalf("attributes = %+v", node.Attributes)
	}
	if node.Attributes[0].Key != "createTS" || node.Attributes[0].Value != "1603034125" {
		t.Errorf("attribute 0 = %+v", node.Attributes[0])
	}
	byKey := map[string]CacheAttribute{}
	for _, a := range node.Attributes {
		byKey[a.Key] = a
	}
	if got := byKey["inverter"]; !strings.Contains(got.Value, "Primo") {
		t.Errorf("a nested attribute must keep its JSON form: %+v", got)
	}

	// The registers: the name and the type suffix are the device's, and the bare
	// identifier stays bare.
	if len(node.Channels) != 3 {
		t.Fatalf("channels = %+v", node.Channels)
	}
	byName := map[string]CacheChannel{}
	for _, ch := range node.Channels {
		byName[ch.Key] = ch
	}

	voltage := byName["PV_VOLTAGE_MEAN_01_F32"]
	if voltage.Name != "PV_VOLTAGE_MEAN_01" || voltage.Kind != "F32" || voltage.Type != "float" {
		t.Errorf("voltage = %+v", voltage)
	}
	if string(voltage.Value) != "612.5" {
		t.Errorf("voltage value = %s", voltage.Value)
	}

	// A 64-bit counter is kept verbatim: as a float64 this value would come back
	// rounded, and a rounded counter is a number the device did not report.
	energy := byName["PV_ENERGYACTIVE_ACTIVE_SUM_01_U64"]
	if energy.Kind != "U64" || energy.Type != "int" {
		t.Errorf("energy = %+v", energy)
	}
	if string(energy.Value) != "15089627579" {
		t.Errorf("energy value = %s, want the exact integer", energy.Value)
	}
	var asFloat float64
	if err := json.Unmarshal(energy.Value, &asFloat); err != nil {
		t.Fatalf("the value is not a JSON number: %v", err)
	}

	// A bare identifier is published as the device wrote it, with no invented
	// name and no invented kind.
	bare := byName["<1090584584>"]
	if bare.Name != "" || bare.Kind != "" || bare.Type != "int" {
		t.Errorf("bare identifier = %+v", bare)
	}
}

func TestCacheRefusesAShapeItCannotRead(t *testing.T) {
	// The path is allowlisted; what must be refused is a payload that is not the
	// envelope the path promises.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"Body":{"Data":[]}}`)
	}))
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, nil, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Cache(context.Background()); err == nil {
		t.Fatal("an array where an object is expected was accepted")
	}
}
