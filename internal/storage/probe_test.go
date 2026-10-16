// Tests of the recorded survey: what a read-only probe observed on a path.
//
// The table exists so that the interface can answer "does this firmware serve
// this path, and does it carry anything" without holding a capture. The tests
// pin the two things that would make it lie: a class that collapses distinct
// outcomes, and a field list that a value could leak through.
package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSourceProbesRecordTheShapeNotTheValues(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	at := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)

	probes := []SourceProbe{
		{
			Path: "/solar_api/v1/GetInverterRealtimeData.cgi", Query: "DataCollection=CumulationInverterData",
			Class: ProbeOK, HTTPStatus: 200, Bytes: 883, Firmware: "1.19.7-1", ProbedAt: at,
			Fields: []ProbeField{
				{Path: "Body.Data.CumulationInverterData.TOTAL_ENERGY", Type: "float"},
				{Path: "Body.Data.DeviceStatus.InverterState", Type: "string"},
			},
		},
		{
			Path:  "/components/Ohmpilot/readable",
			Class: ProbeEmpty, HTTPStatus: 200, Bytes: 42, DigestRequired: true,
			ProbedAt: at, Fields: []ProbeField{},
		},
		{
			Path: "/status/emrs", Class: ProbeNotFound, HTTPStatus: 404, ProbedAt: at,
		},
		{
			Path: "/solar_api/v1/GetStorageRealtimeData.fcgi", Class: ProbeAuthRequired,
			HTTPStatus: 401, ProbedAt: at,
		},
		{
			Path: "/solar_api/v1/GetSensorRealtimeData.fcgi", Class: ProbeError,
			Error: "connection refused", ProbedAt: at,
		},
	}
	if err := s.SaveSourceProbes(ctx, probes); err != nil {
		t.Fatalf("SaveSourceProbes: %v", err)
	}

	got, err := s.SourceProbes(ctx)
	if err != nil {
		t.Fatalf("SourceProbes: %v", err)
	}
	if len(got) != len(probes) {
		t.Fatalf("recorded %d probes, want %d", len(got), len(probes))
	}

	byPath := map[string]SourceProbe{}
	for _, p := range got {
		byPath[p.Path] = p
		if strings.Contains(p.Firmware, "1.19") && p.Class != ProbeOK {
			t.Errorf("%s: firmware recorded on a %s outcome", p.Path, p.Class)
		}
	}

	// The five classes stay distinct: an empty answer, an absent path, a path
	// behind authentication and a failed request are four different facts.
	cumulation := byPath["/solar_api/v1/GetInverterRealtimeData.cgi"]
	if cumulation.Class != ProbeOK || len(cumulation.Fields) != 2 {
		t.Errorf("cumulation: %+v", cumulation)
	}
	if cumulation.Query != "DataCollection=CumulationInverterData" {
		t.Errorf("the query is not part of the key: %q", cumulation.Query)
	}
	if cumulation.Fields[0].Path != "Body.Data.CumulationInverterData.TOTAL_ENERGY" {
		t.Errorf("fields: %+v", cumulation.Fields)
	}
	if empty := byPath["/components/Ohmpilot/readable"]; empty.Class != ProbeEmpty || !empty.DigestRequired {
		t.Errorf("ohmpilot: %+v", empty)
	}
	if absent := byPath["/status/emrs"]; absent.Class != ProbeNotFound || absent.HTTPStatus != 404 {
		t.Errorf("emrs: %+v", absent)
	}
	if auth := byPath["/solar_api/v1/GetStorageRealtimeData.fcgi"]; auth.Class != ProbeAuthRequired {
		t.Errorf("storage: %+v", auth)
	}
	if failed := byPath["/solar_api/v1/GetSensorRealtimeData.fcgi"]; failed.Class != ProbeError || failed.Error == "" {
		t.Errorf("sensor: %+v", failed)
	}
}

func TestSourceProbesReplaceThePreviousSurvey(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	first := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	second := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)

	if err := s.SaveSourceProbes(ctx, []SourceProbe{{
		Path: "/status/emrs", Class: ProbeNotFound, HTTPStatus: 404, ProbedAt: first,
	}}); err != nil {
		t.Fatalf("SaveSourceProbes: %v", err)
	}
	// A second survey after a firmware update: the same path now answers.
	if err := s.SaveSourceProbes(ctx, []SourceProbe{{
		Path: "/status/emrs", Class: ProbeOK, HTTPStatus: 200, Bytes: 512,
		Firmware: "2.0.0-1", ProbedAt: second,
		Fields: []ProbeField{{Path: "Body.Data.Foo", Type: "string"}},
	}}); err != nil {
		t.Fatalf("SaveSourceProbes: %v", err)
	}

	got, err := s.SourceProbes(ctx)
	if err != nil {
		t.Fatalf("SourceProbes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d rows after two surveys of the same path, want one", len(got))
	}
	if got[0].Class != ProbeOK || !got[0].ProbedAt.Equal(second) || got[0].Firmware != "2.0.0-1" {
		t.Errorf("the row was not replaced: %+v", got[0])
	}
}

func TestPurgeRemovesTheSurveysItOutlived(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)

	if err := s.SaveSourceProbes(ctx, []SourceProbe{
		{Path: "/status/emrs", Class: ProbeNotFound, HTTPStatus: 404, ProbedAt: old},
		{Path: "/status/network", Class: ProbeNotFound, HTTPStatus: 404, ProbedAt: recent},
	}); err != nil {
		t.Fatalf("SaveSourceProbes: %v", err)
	}
	if _, err := s.PurgeBefore(ctx, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	got, err := s.SourceProbes(ctx)
	if err != nil {
		t.Fatalf("SourceProbes: %v", err)
	}
	if len(got) != 1 || got[0].Path != "/status/network" {
		t.Errorf("after the purge: %+v", got)
	}
}
