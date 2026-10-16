package solarapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

const validBody = `{
  "Body": {"Data": {
    "Site": {"Mode":"produce-only","P_PV":1902.4,"P_Grid":null,"P_Load":null,"P_Akku":null,
             "E_Day":12345.6,"E_Year":4800000.0,"E_Total":12000000.0,
             "rel_Autonomy":null,"rel_SelfConsumption":null,"Meter_Location":"unknown"},
    "Inverters": {"1": {"DT":1,"P":1902.4,"E_Day":12345.6,"E_Year":4800000.0,"E_Total":12000000.0}}
  }},
  "Head": {"RequestArguments":{},"Status":{"Code":0,"Reason":"","UserMessage":""},"Timestamp":"2026-10-16T09:17:43+02:00"}
}`

type recorded struct {
	method, path, rawQuery, accept string
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newServer starts a test server that records the received request.
func newServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.rawQuery = r.URL.RawQuery
		rec.accept = r.Header.Get("Accept")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func newClient(t *testing.T, baseURL string, timeout time.Duration) *Client {
	t.Helper()
	c, err := New(baseURL, fronius.ReadOnly{Inner: &http.Client{}}, timeout, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func staticHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func TestGetPowerFlowRealtimeDataNominal(t *testing.T) {
	srv, rec := newServer(t, staticHandler(http.StatusOK, validBody))
	c := newClient(t, srv.URL, 2*time.Second)

	env, err := c.GetPowerFlowRealtimeData(context.Background())
	if err != nil {
		t.Fatalf("GetPowerFlowRealtimeData: %v", err)
	}

	if rec.method != http.MethodGet {
		t.Fatalf("method = %q, want GET", rec.method)
	}
	if rec.path != PowerFlowPath {
		t.Fatalf("path = %q, want %q", rec.path, PowerFlowPath)
	}
	if rec.rawQuery != "" {
		t.Fatalf("no parameter expected, got %q", rec.rawQuery)
	}
	if rec.accept != "application/json" {
		t.Fatalf("Accept = %q", rec.accept)
	}

	site := env.Body.Data.Site
	if site.Mode != "produce-only" {
		t.Fatalf("Mode = %q", site.Mode)
	}
	if site.P_PV == nil || *site.P_PV != 1902.4 {
		t.Fatalf("P_PV = %v", site.P_PV)
	}
	if site.E_Day == nil || *site.E_Day != 12345.6 {
		t.Fatalf("E_Day = %v", site.E_Day)
	}
	if site.E_Year == nil || *site.E_Year != 4800000 {
		t.Fatalf("E_Year = %v", site.E_Year)
	}
	if site.Meter_Location == nil || *site.Meter_Location != "unknown" {
		t.Fatalf("Meter_Location = %v", site.Meter_Location)
	}
	for name, v := range map[string]*float64{
		"P_Grid": site.P_Grid, "P_Load": site.P_Load, "P_Akku": site.P_Akku,
		"rel_Autonomy": site.Rel_Autonomy, "rel_SelfConsumption": site.Rel_SelfConsumption,
	} {
		if v != nil {
			t.Fatalf("%s = %v, want nil (JSON field null)", name, *v)
		}
	}

	inv, ok := env.Body.Data.Inverters["1"]
	if !ok {
		t.Fatalf("inverter \"1\" absent: %v", env.Body.Data.Inverters)
	}
	if inv.DT != 1 || inv.P == nil || *inv.P != 1902.4 {
		t.Fatalf("inverter 1 = %+v", inv)
	}
	if env.Head.Status.Code != 0 {
		t.Fatalf("Head.Status = %+v", env.Head.Status)
	}
	if env.Head.Timestamp != "2026-10-16T09:17:43+02:00" {
		t.Fatalf("Head.Timestamp = %q", env.Head.Timestamp)
	}
}

// TestUnknownFieldsTolerated: the client does not refuse unknown fields
// (a more recent firmware adds some, and the fixtures carry _provenance).
func TestUnknownFieldsTolerated(t *testing.T) {
	body := `{
	  "_provenance": {"synthetic": true, "source": "fronius-sim"},
	  "Body": {"Data": {
	    "Site": {"Mode":"produce-only","P_PV":100,"E_Day":1,"E_Year":2,"E_Total":3,"Future":42,"PowerFlow":{}},
	    "Inverters": {"1": {"DT":1,"P":100,"E_Day":1,"E_Year":2,"E_Total":3,"SOC":55}}
	  }, "New": true},
	  "Head": {"Status":{"Code":0},"Timestamp":"2026-10-16T09:17:43+02:00","Extra":"x"}
	}`
	srv, _ := newServer(t, staticHandler(http.StatusOK, body))
	c := newClient(t, srv.URL, 2*time.Second)

	env, err := c.GetPowerFlowRealtimeData(context.Background())
	if err != nil {
		t.Fatalf("a body with unknown fields must be accepted: %v", err)
	}
	if env.Body.Data.Site.P_PV == nil || *env.Body.Data.Site.P_PV != 100 {
		t.Fatalf("P_PV = %v", env.Body.Data.Site.P_PV)
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantKind froniuserr.Kind
	}{
		{"404", http.StatusNotFound, froniuserr.KindNotFound},
		{"401", http.StatusUnauthorized, froniuserr.KindUnauthorized},
		{"403", http.StatusForbidden, froniuserr.KindUnauthorized},
		{"429", http.StatusTooManyRequests, froniuserr.KindServer},
		{"500", http.StatusInternalServerError, froniuserr.KindServer},
		{"503", http.StatusServiceUnavailable, froniuserr.KindServer},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newServer(t, staticHandler(tc.status, "error"))
			c := newClient(t, srv.URL, 2*time.Second)

			_, err := c.GetPowerFlowRealtimeData(context.Background())
			if err == nil {
				t.Fatal("an error was expected")
			}
			kind, ok := froniuserr.KindOf(err)
			if !ok || kind != tc.wantKind {
				t.Fatalf("KindOf = (%q, %v), want %q", kind, ok, tc.wantKind)
			}
			var typed *froniuserr.Error
			if !errors.As(err, &typed) {
				t.Fatal("errors.As did not find *froniuserr.Error")
			}
			if typed.HTTPStatus != tc.status {
				t.Fatalf("HTTPStatus = %d, want %d", typed.HTTPStatus, tc.status)
			}
			if typed.Endpoint != PowerFlowPath {
				t.Fatalf("Endpoint = %q", typed.Endpoint)
			}
			if !errors.Is(err, &froniuserr.Error{Kind: tc.wantKind}) {
				t.Fatalf("errors.Is(%q) false", tc.wantKind)
			}
		})
	}
}

func TestDecodeFailures(t *testing.T) {
	cases := []struct {
		name string
		body string
		hdr  string
	}{
		{"empty body", "", ""},
		{"truncated JSON", `{"Body":{"Data":{"Site":{"P_PV":`, "application/json"},
		{"HTML page", `<!doctype html><html><body>Error</body></html>`, "text/html"},
		{"Body absent", `{"Head":{"Status":{"Code":0}}}`, "application/json"},
		{"Data absent", `{"Body":{},"Head":{"Status":{"Code":0}}}`, "application/json"},
		{"root array", `[1,2,3]`, "application/json"},
		{"non-numeric P_PV", `{"Body":{"Data":{"Site":{"P_PV":"much"}}}}`, "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := tc.hdr
			srv, _ := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				if hdr != "" {
					w.Header().Set("Content-Type", hdr)
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			})
			c := newClient(t, srv.URL, 2*time.Second)

			_, err := c.GetPowerFlowRealtimeData(context.Background())
			if err == nil {
				t.Fatal("a decode error was expected")
			}
			if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
				t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindDecode, err)
			}
		})
	}
}

func TestDeviceStatusError(t *testing.T) {
	body := `{"Body":{"Data":{"Site":{"P_PV":0},"Inverters":{}}},
	          "Head":{"Status":{"Code":7,"Reason":"Simulated device error","UserMessage":"Simulated device error"}}}`
	srv, _ := newServer(t, staticHandler(http.StatusOK, body))
	c := newClient(t, srv.URL, 2*time.Second)

	env, err := c.GetPowerFlowRealtimeData(context.Background())
	if err == nil {
		t.Fatal("Head.Status.Code=7 should have produced an error")
	}
	kind, _ := froniuserr.KindOf(err)
	if kind != froniuserr.KindStatus {
		t.Fatalf("KindOf = %q, want %q", kind, froniuserr.KindStatus)
	}
	var typed *froniuserr.Error
	if !errors.As(err, &typed) {
		t.Fatal("errors.As failed")
	}
	if typed.Code != 7 || typed.Reason != "Simulated device error" || typed.UserMessage != "Simulated device error" {
		t.Fatalf("incomplete error: %+v", typed)
	}
	// The body stays usable: the caller may decide to ignore the error.
	if env.Body.Data.Site.P_PV == nil || *env.Body.Data.Site.P_PV != 0 {
		t.Fatalf("envelope not returned: %+v", env.Body.Data.Site)
	}
}

func TestTimeout(t *testing.T) {
	srv, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, validBody)
	})
	c := newClient(t, srv.URL, 50*time.Millisecond)

	start := time.Now()
	_, err := c.GetPowerFlowRealtimeData(context.Background())
	if err == nil {
		t.Fatal("a timeout was expected")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindTimeout {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindTimeout, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the delay was not respected: %v", elapsed)
	}
}

func TestContextDeadlineShorterThanTimeout(t *testing.T) {
	srv, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	})
	c := newClient(t, srv.URL, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := c.GetPowerFlowRealtimeData(ctx)
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindTimeout {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindTimeout, err)
	}
}

func TestServerUnreachable(t *testing.T) {
	srv, _ := newServer(t, staticHandler(http.StatusOK, validBody))
	baseURL := srv.URL
	srv.Close() // the server no longer accepts connections

	c := newClient(t, baseURL, 500*time.Millisecond)
	_, err := c.GetPowerFlowRealtimeData(context.Background())
	if err == nil {
		t.Fatal("a network failure was expected")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindTransport {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindTransport, err)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	huge := strings.Repeat("x", maxBodyBytes+16)
	srv, _ := newServer(t, staticHandler(http.StatusOK, huge))
	c := newClient(t, srv.URL, 5*time.Second)

	_, err := c.GetPowerFlowRealtimeData(context.Background())
	if err == nil {
		t.Fatal("an out-of-limit body was expected")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("KindOf = %q, want %q", kind, froniuserr.KindDecode)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	for _, base := range []string{"", "127.0.0.1:8081", "ftp://inverter/", "http://"} {
		t.Run(base, func(t *testing.T) {
			if _, err := New(base, nil, time.Second, testLogger()); err == nil {
				t.Fatalf("New(%q) should have failed", base)
			}
		})
	}
}

// TestNilDoerDefaultsToHTTPClient checks that a nil transport remains usable.
func TestNilDoerDefaultsToHTTPClient(t *testing.T) {
	srv, _ := newServer(t, staticHandler(http.StatusOK, validBody))
	c, err := New(srv.URL, nil, 2*time.Second, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.GetPowerFlowRealtimeData(context.Background()); err != nil {
		t.Fatalf("GetPowerFlowRealtimeData: %v", err)
	}
}

// TestReadOnlyGuardIntegration: the client as built in production cannot emit
// anything other than a GET.
func TestReadOnlyGuardIntegration(t *testing.T) {
	srv, rec := newServer(t, staticHandler(http.StatusOK, validBody))
	c := newClient(t, srv.URL, 2*time.Second)

	// A direct POST through the guard must be refused without reaching the server.
	ro := fronius.ReadOnly{Inner: &http.Client{}}
	req, err := http.NewRequest(http.MethodPost, srv.URL+PowerFlowPath, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := ro.Do(req); err == nil {
		t.Fatal("the read-only guard should have refused the POST")
	}
	if rec.method != "" {
		t.Fatalf("the server received a request: %+v", rec)
	}

	// And the nominal client, for its part, passes.
	if _, err := c.GetPowerFlowRealtimeData(context.Background()); err != nil {
		t.Fatalf("GetPowerFlowRealtimeData: %v", err)
	}
	if rec.method != http.MethodGet {
		t.Fatalf("method = %q", rec.method)
	}
}

// TestBaseURLWithPathPrefix: a base path is preserved.
func TestBaseURLWithPathPrefix(t *testing.T) {
	srv, rec := newServer(t, staticHandler(http.StatusOK, validBody))
	c := newClient(t, srv.URL+"/prefix/", 2*time.Second)

	if _, err := c.GetPowerFlowRealtimeData(context.Background()); err != nil {
		t.Fatalf("GetPowerFlowRealtimeData: %v", err)
	}
	if rec.path != "/prefix"+PowerFlowPath {
		t.Fatalf("path = %q, want %q", rec.path, "/prefix"+PowerFlowPath)
	}
}
