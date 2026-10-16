// Tests of GET /api/v1/detail.
//
// The endpoint publishes the diagnostic families. Three states must stay
// distinguishable to the reader: recorded and available, recorded and
// unavailable with its reason, and never read (absent from the list).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"fronius-supervisor/internal/storage"
)

type detailPayload struct {
	Data struct {
		ReadAt   *string `json:"read_at"`
		Families []struct {
			Family    string          `json:"family"`
			Available bool            `json:"available"`
			Reason    string          `json:"reason"`
			ReadAt    string          `json:"read_at"`
			Data      json.RawMessage `json:"data"`
		} `json:"families"`
	} `json:"data"`
	Meta struct {
		GeneratedAt string `json:"generated_at"`
	} `json:"meta"`
}

func TestDetailPublishesWhatWasRead(t *testing.T) {
	_, srv, store := newTestServer(t, nil)

	// Nothing read: an empty list, and no snapshot instant to announce.
	_, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/detail")
	var empty detailPayload
	if err := json.Unmarshal(body, &empty); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(empty.Data.Families) != 0 {
		t.Errorf("families = %d before any read, want none", len(empty.Data.Families))
	}
	if empty.Data.ReadAt != nil {
		t.Errorf("read_at = %v before any read, want absent", *empty.Data.ReadAt)
	}

	at := time.Date(2026, 10, 16, 19, 30, 0, 0, time.UTC)
	inverter := json.RawMessage(`{
	  "state": {"inverter_state": "Sleeping", "status_code": 13, "error_code": 1175},
	  "measurements": {
	    "PAC": {"unit": "W", "value": 0},
	    "UAC": {"unit": "V", "value": 231.4},
	    "DAY_ENERGY": {"unit": "Wh", "value": null},
	    "MYSTERY": {"unit": "X", "value": 42}
	  },
	  "energies": {"day_wh": null, "year_wh": null, "total_wh": 15089627.579722222}
	}`)
	if err := store.SaveDetails(context.Background(), []storage.Detail{
		{Family: "inverter", ReadAtUTC: at, Available: true, Data: inverter},
		{Family: "network", ReadAtUTC: at, Available: false, Reason: "the device answered 500"},
		{Family: "emrs", ReadAtUTC: at, Available: true,
			Data: json.RawMessage(`{"entries":[{"path":"pin1.state","value":true}]}`)},
	}); err != nil {
		t.Fatalf("SaveDetails: %v", err)
	}

	_, _, body = doRequest(t, srv, http.MethodGet, "/api/v1/detail")
	var got detailPayload
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got.Data.ReadAt == nil || *got.Data.ReadAt != formatTime(at) {
		t.Errorf("read_at = %v, want %s", got.Data.ReadAt, formatTime(at))
	}
	if len(got.Data.Families) != 3 {
		t.Fatalf("families = %d, want the 3 recorded", len(got.Data.Families))
	}

	byFamily := map[string]json.RawMessage{}
	available := map[string]bool{}
	reasons := map[string]string{}
	for _, f := range got.Data.Families {
		byFamily[f.Family] = f.Data
		available[f.Family] = f.Available
		reasons[f.Family] = f.Reason
		if f.ReadAt == "" {
			t.Errorf("%s: no read instant", f.Family)
		}
	}
	// A family never read is absent, not empty.
	if _, ok := byFamily["system"]; ok {
		t.Error("a family never read was published")
	}
	if available["network"] || reasons["network"] == "" {
		t.Errorf("network: available=%v reason=%q", available["network"], reasons["network"])
	}

	// The inverter family is published with an ORDERED measurement list, each
	// entry labelled; a key nobody catalogued is kept with its own name.
	var inv struct {
		State struct {
			InverterState string `json:"inverter_state"`
			StatusCode    *int   `json:"status_code"`
			ErrorCode     *int   `json:"error_code"`
		} `json:"state"`
		Measurements []struct {
			Key   string   `json:"key"`
			Label string   `json:"label"`
			Unit  string   `json:"unit"`
			Value *float64 `json:"value"`
		} `json:"measurements"`
		Energies struct {
			DayWh   *float64 `json:"day_wh"`
			TotalWh *float64 `json:"total_wh"`
		} `json:"energies"`
	}
	if err := json.Unmarshal(byFamily["inverter"], &inv); err != nil {
		t.Fatalf("inverter: %v", err)
	}
	if inv.State.InverterState != "Sleeping" || inv.State.StatusCode == nil || *inv.State.StatusCode != 13 {
		t.Errorf("state = %+v", inv.State)
	}
	if len(inv.Measurements) != 4 {
		t.Fatalf("measurements = %d, want the 4 stored", len(inv.Measurements))
	}
	// Display order: the observed keys first, in their order, then the rest.
	if inv.Measurements[0].Key != "PAC" || inv.Measurements[1].Key != "UAC" {
		t.Errorf("order = %s, %s; want PAC then UAC", inv.Measurements[0].Key, inv.Measurements[1].Key)
	}
	if last := inv.Measurements[3]; last.Key != "MYSTERY" || last.Label != "MYSTERY" {
		t.Errorf("an uncatalogued key was dropped or labelled: %+v", last)
	}
	if inv.Measurements[0].Label != "AC power" || inv.Measurements[0].Unit != "W" {
		t.Errorf("PAC: %+v", inv.Measurements[0])
	}
	var day *float64
	for _, m := range inv.Measurements {
		if m.Key == "DAY_ENERGY" {
			day = m.Value
		}
	}
	if day != nil {
		t.Errorf("DAY_ENERGY = %v, want null (the device reports no value)", *day)
	}
	if inv.Energies.DayWh != nil || inv.Energies.TotalWh == nil {
		t.Errorf("energies = %+v", inv.Energies)
	}
}

func TestDetailRefusesOtherMethods(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)
	status, header, _ := doRequest(t, srv, http.MethodPost, "/api/v1/detail")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/detail = %d, want 405", status)
	}
	if allow := header.Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow = %q, want GET", allow)
	}
}
