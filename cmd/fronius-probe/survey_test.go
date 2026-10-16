// Tests of the read-only survey.
//
// Two things matter and are pinned here: the classes must keep distinct facts
// distinct (absent ≠ refused ≠ empty), and the inventory must describe an answer
// without ever carrying one of its values.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/secret"
	"fronius-supervisor/internal/storage"
)

func TestClassifyKeepsTheOutcomesDistinct(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		class  string
		fields int
	}{
		{"a measurement", 200, `{"Body":{"Data":{"Site":{"P_PV":3200.5,"Mode":"produce-only"}}}}`, classOK, 2},
		{"an absent path", 404, ``, classNotFound, 0},
		{"a refused path", 401, ``, classAuthRequired, 0},
		{"a forbidden path", 403, ``, classAuthRequired, 0},
		{"a failing device", 500, `internal error`, classError, 0},
		{"an HTML page", 200, `<!doctype html><title>nope</title>`, classNotJSON, 0},
		{"an empty object", 200, `{}`, classEmpty, 0},
		{"an empty list", 200, `[]`, classEmpty, 0},
		{"no component installed", 200, `{"Body":{"Data":{}}}`, classEmpty, 0},
		{"a null component", 200, `{"Body":{"Data":null}}`, classEmpty, 0},
		{"one event", 200, `[{"uuid":"x","severity":3}]`, classOK, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			class, fields := classify(tc.status, []byte(tc.body))
			if class != tc.class {
				t.Fatalf("class = %q, want %q", class, tc.class)
			}
			if len(fields) != tc.fields {
				t.Fatalf("fields = %d (%+v), want %d", len(fields), fields, tc.fields)
			}
		})
	}
}

func TestInventoryDescribesPathsAndTypesOnly(t *testing.T) {
	body := []byte(`{
	  "Body": {"Data": {
	    "Site": {"P_PV": 3200.5, "E_Total": 15089627.58, "Mode": "produce-only",
	             "BackupMode": false, "P_Grid": null},
	    "Inverters": {"1": {"DT": 1, "P": 1902}},
	    "Smartloads": {"Ohmpilots": {}}
	  }},
	  "Head": {"Timestamp": "2026-10-16T15:17:23+00:00", "Status": {"Code": 0}}
	}`)

	class, fields := classify(200, body)
	if class != classOK {
		t.Fatalf("class = %q", class)
	}

	byPath := map[string]surveyField{}
	for _, f := range fields {
		byPath[f.Path] = f
	}
	for path, want := range map[string]string{
		"Body.Data.Site.P_PV":       "float",
		"Body.Data.Site.E_Total":    "float",
		"Body.Data.Site.Mode":       "string",
		"Body.Data.Site.BackupMode": "bool",
		"Body.Data.Site.P_Grid":     "null",
		"Body.Data.Inverters.1.DT":  "int",
		"Body.Data.Inverters.1.P":   "int",
		"Head.Status.Code":          "int",
		"Head.Timestamp":            "string",
	} {
		got, ok := byPath[path]
		if !ok {
			t.Errorf("%s is missing from the inventory", path)
			continue
		}
		if got.Type != want {
			t.Errorf("%s: type %q, want %q", path, got.Type, want)
		}
	}

	// No value of the answer appears anywhere in the inventory: a survey is not
	// a copy of the device's data.
	rendered, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leaked := range []string{"3200.5", "15089627.58", "produce-only", "2026-10-16T15:17:23"} {
		if strings.Contains(string(rendered), leaked) {
			t.Errorf("the inventory leaked the value %q:\n%s", leaked, rendered)
		}
	}
}

func TestInventoryWalksArraysThroughTheirFirstElement(t *testing.T) {
	class, fields := classify(200, []byte(`[{"uuid":"a","severity":3},{"uuid":"b"}]`))
	if class != classOK {
		t.Fatalf("class = %q", class)
	}
	byPath := map[string]surveyField{}
	for _, f := range fields {
		byPath[f.Path] = f
	}
	root, ok := byPath[""]
	if !ok || root.Type != "array" || root.ArrayLen != 2 {
		t.Fatalf("root array not described: %+v", fields)
	}
	if _, ok := byPath["[].uuid"]; !ok {
		t.Errorf("the element keys are not suffixed with []: %+v", fields)
	}
	if _, ok := byPath["[].severity"]; !ok {
		t.Errorf("the second element key is missing: %+v", fields)
	}
}

func TestInventoryIsBounded(t *testing.T) {
	// A wide answer must be truncated explicitly, never silently cut.
	var b strings.Builder
	b.WriteString("{")
	for i := range maxSurveyFields + 50 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"k%d":%d`, i, i)
	}
	b.WriteString("}")

	_, fields := classify(200, []byte(b.String()))
	if len(fields) > maxSurveyFields+1 {
		t.Fatalf("inventory holds %d entries, want at most %d plus the marker", len(fields), maxSurveyFields)
	}
	if last := fields[len(fields)-1]; last.Path != "(truncated)" {
		t.Errorf("the truncation is not signalled: last = %+v", last)
	}
}

func TestFirmwareFromReadsBothObservedShapes(t *testing.T) {
	cases := map[string]string{
		`{"swrevisions":{"GEN24":"1.19.7-1"},"serialNumber":"x"}`: "1.19.7-1",
		`{"Body":{"Data":{"swrevisions":{"GEN24":"2.0.0-1"}}}}`:   "2.0.0-1",
		`{"firmwareVersion":"1.24.3-sim"}`:                        "1.24.3-sim",
		`{"Body":{"Data":{"SerialNumber":"x"}}}`:                  "",
		`not json`:                                                "",
	}
	for body, want := range cases {
		if got := firmwareFrom([]byte(body)); got != want {
			t.Errorf("firmwareFrom(%s) = %q, want %q", body, got, want)
		}
	}
}

// TestSurveyWalksTheWhitelistAndRecordsIt drives the whole survey against a stub
// device: one path that answers, one that is absent, one that is empty, one that
// refuses without credentials.
func TestSurveyWalksTheWhitelistAndRecordsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/solar_api/v1/GetPowerFlowRealtimeData.fcgi":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Body":{"Data":{"Site":{"P_PV":1.5}}},"Head":{"Timestamp":"2026-10-16T15:17:23+00:00"}}`)
		case internalapi.VersionPath:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"swrevisions":{"GEN24":"1.19.7-1"},"serialNumber":"redacted"}`)
		case "/components/PowerMeter/readable":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, `{"Body":{"Data":{}}}`)
		case "/status/emrs":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)

	// A small allowlist for the test, restored afterwards: runSurvey walks the
	// package's list, which in production is the whole catalogue.
	original := surveyWhitelist
	surveyWhitelist = []endpoint{
		{method: http.MethodGet, path: "/solar_api/v1/GetPowerFlowRealtimeData.fcgi", classification: "READ"},
		{method: http.MethodGet, path: internalapi.VersionPath, classification: "READ", digest: true},
		{method: http.MethodGet, path: "/components/PowerMeter/readable", classification: "READ", digest: true},
		{method: http.MethodGet, path: "/status/emrs", classification: "READ", digest: true},
		{method: http.MethodGet, path: "/status/network", classification: "READ", digest: true},
	}
	t.Cleanup(func() { surveyWhitelist = original })

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fronius.db")
	var out strings.Builder
	opt := options{
		baseURL: srv.URL,
		out:     &out,
		delay:   0,
		saveDir: dir,
		dbPath:  dbPath,
	}
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if err := runSurvey(context.Background(), opt, base, credentials{}, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("runSurvey: %v", err)
	}

	text := out.String()
	// The operator sees one line per path, with the class that matters.
	for _, want := range []string{
		"Survey of 5 catalogued paths",
		"ok            200",
		"empty         200",
		"not_found     404",
		"auth_required 401",
		"1.19.7-1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the survey output does not contain %q:\n%s", want, text)
		}
	}

	// The report is written, and holds the shape rather than the values.
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var reportPath string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "survey_") {
			reportPath = filepath.Join(dir, f.Name())
		}
	}
	if reportPath == "" {
		t.Fatal("no survey report was written")
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var report surveyReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("the report is not readable JSON: %v", err)
	}
	if len(report.Entries) != 5 {
		t.Errorf("the report holds %d entries, want 5", len(report.Entries))
	}
	if report.Firmware != "1.19.7-1" {
		t.Errorf("firmware = %q, want the version the identity path answered", report.Firmware)
	}
	for _, leaked := range []string{"1.5", "redacted"} {
		if strings.Contains(string(body), leaked) {
			t.Errorf("the report carries the value %q:\n%s", leaked, body)
		}
	}

	// The database holds the same outcome, where the application reads it.
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	store, err := storage.Open(dbPath, loc)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	probes, err := store.SourceProbes(context.Background())
	if err != nil {
		t.Fatalf("SourceProbes: %v", err)
	}
	if len(probes) != 5 {
		t.Fatalf("recorded %d probes, want 5", len(probes))
	}
	byPath := map[string]storage.SourceProbe{}
	for _, p := range probes {
		byPath[p.Path] = p
	}
	if p := byPath["/status/emrs"]; p.Class != storage.ProbeNotFound {
		t.Errorf("emrs: %+v", p)
	}
	// The empty component answered without a challenge: it is reachable, and it
	// carries nothing — two facts the recorded row keeps apart.
	if p := byPath["/components/PowerMeter/readable"]; p.Class != storage.ProbeEmpty || p.DigestRequired {
		t.Errorf("powermeter: %+v", p)
	}
	if p := byPath["/status/network"]; p.Class != storage.ProbeAuthRequired || !p.DigestRequired {
		t.Errorf("network: %+v", p)
	}
	if p := byPath[internalapi.VersionPath]; p.Class != storage.ProbeOK || p.Firmware != "1.19.7-1" {
		t.Errorf("version: %+v", p)
	}
	flow := byPath["/solar_api/v1/GetPowerFlowRealtimeData.fcgi"]
	if flow.Class != storage.ProbeOK || flow.DigestRequired {
		t.Errorf("flow: %+v", flow)
	}
	if len(flow.Fields) == 0 {
		t.Error("the flow answer was probed without keeping its shape")
	}
}

// TestSurveyReadsAnAuthenticatedPathOnceMore makes sure the second attempt is
// made — and only when credentials exist — and that the outcome records that the
// path is protected.
func TestSurveyReadsAnAuthenticatedPathOnceMore(t *testing.T) {
	var bare, authenticated int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			bare++
			w.Header().Set("x-www-authenticate", `Digest realm="Webinterface area", nonce="abc", qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authenticated++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Body":{"Data":{"updateRunning":false}}}`)
	}))
	t.Cleanup(srv.Close)

	ep := endpoint{method: http.MethodGet, path: "/status/common", classification: "READ", digest: true}
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Without credentials: one bare attempt, recorded as auth_required.
	opt := options{baseURL: srv.URL, out: io.Discard, timeout: 5 * time.Second}
	bare, authenticated = 0, 0
	entry, _ := surveyWithCreds(t, opt, base, ep, credentials{}, logger)
	if entry.Class != classAuthRequired || bare != 1 || authenticated != 0 {
		t.Fatalf("without credentials: %+v (bare=%d, authenticated=%d)", entry, bare, authenticated)
	}
	if !entry.DigestRequired {
		t.Error("the survey did not record that the path is protected")
	}

	// With credentials: two bare attempts and one authenticated. The survey
	// sends the bare request itself — that is what tells it the path is
	// protected — and the Digest transport sends one more, because it never
	// authenticates preemptively (internal/fronius/digest): it answers the
	// challenge it receives. One extra GET per protected path is the price of
	// recording the fact; without credentials the survey stops after the first.
	bare, authenticated = 0, 0
	cred := credentials{user: "customer", password: secret.New("pw")}
	entry, _ = surveyWithCreds(t, opt, base, ep, cred, logger)
	if entry.Class != classOK || bare != 2 || authenticated != 1 {
		t.Fatalf("with credentials: %+v (bare=%d, authenticated=%d)", entry, bare, authenticated)
	}
	if !entry.DigestRequired {
		t.Error("digest_required was not recorded although the path answered 401 first")
	}
	if len(entry.Fields) != 1 {
		t.Errorf("the authenticated answer was not inventoried: %+v", entry.Fields)
	}
}

// surveyWithCreds runs one entry of a survey with the given credentials.
func surveyWithCreds(t *testing.T, opt options, base *url.URL, ep endpoint,
	cred credentials, logger *slog.Logger) (surveyEntry, string) {
	t.Helper()
	transport, err := newDigestTransport(opt.baseURL, cred, logger)
	if err != nil {
		t.Fatalf("digest transport: %v", err)
	}
	return surveyOne(context.Background(), opt, base, ep, cred, transport)
}
