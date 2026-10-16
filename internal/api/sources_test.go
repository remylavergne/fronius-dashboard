// Tests of GET /api/v1/sources.
//
// The endpoint publishes static catalogue data; what matters here is the
// contract the interface consumes: the envelope, the counters that agree with
// the list, and the states that must be visible (a source that is called, one
// that is only captured, one that is a candidate).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/storage"
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
	// The Solar API side: every path published as called must be one the client
	// can actually reach — a constant of internal/fronius/solarapi, not a string
	// that happens to look right.
	reachable := map[string]bool{
		solarapi.PowerFlowPath:        true,
		solarapi.InverterRealtimePath: true,
		solarapi.InverterInfoPath:     true,
		solarapi.ActiveDeviceInfoPath: true,
		solarapi.APIVersionPath:       true,
	}
	solarCalled := map[string]bool{}
	for _, entry := range got.Data.Sources {
		if entry.Called && entry.Transport == "solar_api" {
			solarCalled[entry.Path] = true
		}
	}
	if len(solarCalled) != len(reachable) {
		t.Errorf("%d Solar API paths published as called, want %d: %v", len(solarCalled), len(reachable), solarCalled)
	}
	for path := range solarCalled {
		if !reachable[path] {
			t.Errorf("%s is published as called but is not a path the client knows", path)
		}
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

func TestSourcesMergesTheRecordedSurvey(t *testing.T) {
	_, srv, store := newTestServer(t, nil)
	ctx := context.Background()

	// Nothing surveyed yet: the counters stay at zero and no source carries a
	// probe — an interface must not read zeros as a result.
	_, _, body := doRequest(t, srv, http.MethodGet, "/api/v1/sources")
	var before struct {
		Data struct {
			Survey struct {
				Probed       int     `json:"probed"`
				LastProbedAt *string `json:"last_probed_at"`
				Firmware     string  `json:"firmware"`
			} `json:"survey"`
			Sources []struct {
				ID    string          `json:"id"`
				Probe json.RawMessage `json:"probe"`
			} `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if before.Data.Survey.Probed != 0 || before.Data.Survey.LastProbedAt != nil {
		t.Errorf("a database without a survey announces %+v", before.Data.Survey)
	}
	for _, s := range before.Data.Sources {
		if len(s.Probe) > 0 && string(s.Probe) != "null" {
			t.Errorf("%s carries a probe without a survey", s.ID)
		}
	}

	// A survey recorded on three paths of three different outcomes.
	at := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	if err := store.SaveSourceProbes(ctx, []storage.SourceProbe{
		{
			Path: "/solar_api/v1/GetInverterRealtimeData.cgi", Query: solarapi.CumulationQuery,
			Class: storage.ProbeOK, HTTPStatus: 200, Bytes: 883, Firmware: "1.19.7-1", ProbedAt: at,
			Fields: []storage.ProbeField{{Path: "Body.Data.DeviceStatus.InverterState", Type: "string"}},
		},
		{Path: "/status/emrs", Class: storage.ProbeNotFound, HTTPStatus: 404, ProbedAt: at},
		{Path: "/status/common", Class: storage.ProbeAuthRequired, HTTPStatus: 401, DigestRequired: true, ProbedAt: at},
	}); err != nil {
		t.Fatalf("SaveSourceProbes: %v", err)
	}

	_, _, body = doRequest(t, srv, http.MethodGet, "/api/v1/sources")
	var after struct {
		Data struct {
			Survey struct {
				Probed       int     `json:"probed"`
				OK           int     `json:"ok"`
				AuthRequired int     `json:"auth_required"`
				NotFound     int     `json:"not_found"`
				LastProbedAt *string `json:"last_probed_at"`
				Firmware     string  `json:"firmware"`
			} `json:"survey"`
			Sources []struct {
				ID    string `json:"id"`
				Probe *struct {
					Class          string `json:"class"`
					HTTPStatus     int    `json:"http_status"`
					DigestRequired bool   `json:"digest_required"`
					Firmware       string `json:"firmware"`
					ProbedAt       string `json:"probed_at"`
					Fields         []struct {
						Path string `json:"path"`
						Type string `json:"type"`
					} `json:"fields"`
				} `json:"probe"`
			} `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &after); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	switch {
	case after.Data.Survey.Probed != 3:
		t.Errorf("probed = %d, want 3", after.Data.Survey.Probed)
	case after.Data.Survey.OK != 1 || after.Data.Survey.NotFound != 1 || after.Data.Survey.AuthRequired != 1:
		t.Errorf("classes = %+v, want one of each", after.Data.Survey)
	case after.Data.Survey.Firmware != "1.19.7-1":
		t.Errorf("firmware = %q", after.Data.Survey.Firmware)
	case after.Data.Survey.LastProbedAt == nil || *after.Data.Survey.LastProbedAt != formatTime(at):
		t.Errorf("last_probed_at = %v, want %s", after.Data.Survey.LastProbedAt, formatTime(at))
	}

	byID := map[string]*struct {
		Class          string `json:"class"`
		HTTPStatus     int    `json:"http_status"`
		DigestRequired bool   `json:"digest_required"`
		Firmware       string `json:"firmware"`
		ProbedAt       string `json:"probed_at"`
		Fields         []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"fields"`
	}{}
	for _, s := range after.Data.Sources {
		byID[s.ID] = s.Probe
	}

	// The accumulation was identified by path AND query: a survey row of the
	// same path under another DataCollection would not be merged by mistake.
	cumulation := byID["solarapi.inverter.cumulation"]
	if cumulation == nil || cumulation.Class != "ok" || len(cumulation.Fields) != 1 {
		t.Fatalf("the captured accumulation did not receive its survey: %+v", cumulation)
	}
	if cumulation.ProbedAt != formatTime(at) || cumulation.Firmware != "1.19.7-1" {
		t.Errorf("survey stamp: %+v", cumulation)
	}
	if auth := byID["internalapi.common"]; auth == nil || auth.Class != "auth_required" || !auth.DigestRequired {
		t.Errorf("internalapi.common: %+v", auth)
	}
	if absent := byID["internalapi.emrs"]; absent == nil || absent.Class != "not_found" {
		t.Errorf("internalapi.emrs: %+v", absent)
	}
	if called := byID["solarapi.powerflow"]; called != nil {
		t.Errorf("a source without a survey row carries a probe: %+v", called)
	}
}
