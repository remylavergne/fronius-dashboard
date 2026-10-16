// Tests of GET /api/v1/sources.
//
// The endpoint publishes static catalogue data; what matters here is the
// contract the interface consumes: the envelope, the counters that agree with
// the list, and the states that must be visible (a source that is called, one
// that is only captured, one that is a candidate).
package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
)

type sourceField struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Why    string `json:"why"`
}

type sourceEntry struct {
	ID          string        `json:"id"`
	Transport   string        `json:"transport"`
	Path        string        `json:"path"`
	Query       string        `json:"query"`
	Method      string        `json:"method"`
	Provides    string        `json:"provides"`
	State       string        `json:"state"`
	Called      bool          `json:"called"`
	Fixture     string        `json:"fixture"`
	Capability  string        `json:"capability"`
	WouldUnlock string        `json:"would_unlock"`
	Fields      []sourceField `json:"fields"`
	Proofs      []struct {
		Tag      string `json:"tag"`
		Firmware string `json:"firmware"`
		Detail   string `json:"detail"`
	} `json:"proofs"`
	Docs []string `json:"docs"`
	Note string   `json:"note"`
}

type sourcesPayload struct {
	Data struct {
		Summary struct {
			Sources          int `json:"sources"`
			Called           int `json:"called"`
			Captured         int `json:"captured"`
			Candidates       int `json:"candidates"`
			Archived         int `json:"archived"`
			Fields           int `json:"fields"`
			FieldsDisplayed  int `json:"fields_displayed"`
			FieldsStored     int `json:"fields_stored"`
			FieldsValidated  int `json:"fields_validated"`
			FieldsIgnored    int `json:"fields_ignored"`
			FieldsUnverified int `json:"fields_unverified"`
		} `json:"summary"`
		Sources []sourceEntry `json:"sources"`
	} `json:"data"`
	Meta struct {
		GeneratedAt string `json:"generated_at"`
	} `json:"meta"`
}

func TestSourcesPublishesTheCatalogue(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)

	status, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/sources")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	var got sourcesPayload
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got.Meta.GeneratedAt == "" {
		t.Error("meta.generated_at is absent")
	}
	if len(got.Data.Sources) == 0 {
		t.Fatal("no source published")
	}
	if got.Data.Summary.Sources != len(got.Data.Sources) {
		t.Errorf("summary.sources = %d, %d entries", got.Data.Summary.Sources, len(got.Data.Sources))
	}

	var called, captured, candidates, archived, fields int
	sawStates := map[string]bool{}
	for _, s := range got.Data.Sources {
		if s.ID == "" || s.Path == "" || s.Method != "GET" || s.Provides == "" {
			t.Errorf("incomplete entry: %+v", s)
		}
		if len(s.Proofs) == 0 || s.Proofs[0].Tag == "" {
			t.Errorf("%s: no provenance published", s.ID)
		}
		if s.Transport != "solar_api" && s.Transport != "internal_api" {
			t.Errorf("%s: transport %q", s.ID, s.Transport)
		}
		sawStates[s.State] = true
		if s.Called {
			called++
		}
		switch s.State {
		case "captured":
			captured++
			// A captured source must rest on something an operator can check:
			// either a promoted fixture or a [VERIFIED] proof naming the capture
			// (the accumulation answer was read but never promoted — the
			// application does not call it).
			hasVerified := false
			for _, p := range s.Proofs {
				if p.Tag == "[VERIFIED]" {
					hasVerified = true
				}
			}
			if s.Fixture == "" && !hasVerified {
				t.Errorf("%s: captured with neither a fixture nor a verified proof", s.ID)
			}
		case "candidate":
			candidates++
		case "archived":
			archived++
		}
		fields += len(s.Fields)
	}

	// The published summary must agree with the published list, or a reader has
	// to recount by hand — which is exactly what this page exists to avoid.
	switch {
	case got.Data.Summary.Called != called:
		t.Errorf("summary.called = %d, counted %d", got.Data.Summary.Called, called)
	case got.Data.Summary.Captured != captured:
		t.Errorf("summary.captured = %d, counted %d", got.Data.Summary.Captured, captured)
	case got.Data.Summary.Candidates != candidates:
		t.Errorf("summary.candidates = %d, counted %d", got.Data.Summary.Candidates, candidates)
	case got.Data.Summary.Archived != archived:
		t.Errorf("summary.archived = %d, counted %d", got.Data.Summary.Archived, archived)
	case got.Data.Summary.Fields != fields:
		t.Errorf("summary.fields = %d, counted %d", got.Data.Summary.Fields, fields)
	}

	// The display distinguishes the three situations an operator cares about.
	for _, want := range []string{"called", "captured", "candidate"} {
		if !sawStates[want] {
			t.Errorf("no source is in state %q: the payload cannot show the difference", want)
		}
	}
	if got.Data.Summary.FieldsUnverified == 0 {
		t.Error("no field is published as unverified")
	}
	if got.Data.Summary.FieldsIgnored == 0 {
		t.Error("no field is published as ignored: the reasons for the gaps are missing")
	}
}

func TestSourcesPublishesTheCalledSetAndThePaths(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)
	_, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/sources")

	var got sourcesPayload
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	called := map[string]bool{}
	byID := map[string]sourceEntry{}
	for _, s := range got.Data.Sources {
		byID[s.ID] = s
		if s.Called {
			called[s.Path] = true
		}
	}

	// The four internal paths of the code allowlist, plus the one Solar API path
	// the collector reads: nothing else is called.
	for _, ep := range internalapi.Allowlist() {
		if !called[ep.Path] {
			t.Errorf("%s is allowlisted but not published as called", ep.Path)
		}
	}
	if !called[solarapi.PowerFlowPath] {
		t.Errorf("%s is not published as called", solarapi.PowerFlowPath)
	}
	if len(called) != len(internalapi.Allowlist())+1 {
		t.Errorf("%d paths published as called, want %d", len(called), len(internalapi.Allowlist())+1)
	}

	// The filtered fields explain the two carried-over findings of the session.
	flow, ok := byID["solarapi.powerflow"]
	if !ok {
		t.Fatal("the flow source is missing")
	}
	var sawNullDay, sawIgnored bool
	for _, f := range flow.Fields {
		if f.Path == "Body.Data.Site.E_Day" && f.Status == "unverified" && f.Why != "" {
			sawNullDay = true
		}
		if f.Status == "ignored" && f.Why != "" {
			sawIgnored = true
		}
	}
	if !sawNullDay {
		t.Error("the null day counter is not published with its reason")
	}
	if !sawIgnored {
		t.Error("no ignored field is published with its reason")
	}

	cumulation, ok := byID["solarapi.inverter.cumulation"]
	if !ok {
		t.Fatal("the captured accumulation source is missing")
	}
	if cumulation.Called {
		t.Error("the accumulation source is published as called")
	}
	if cumulation.WouldUnlock == "" {
		t.Error("the accumulation source does not say what it would unlock")
	}
}

func TestSourcesRefusesOtherMethods(t *testing.T) {
	_, srv, _ := newTestServer(t, nil)
	status, header, _ := doRequest(t, srv, http.MethodPost, "/api/v1/sources")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/sources = %d, want 405", status)
	}
	if allow := header.Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow = %q, want GET", allow)
	}
}
