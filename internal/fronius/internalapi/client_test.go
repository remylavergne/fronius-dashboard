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

// countingDoer counts the requests actually passed through.
type countingDoer struct {
	count   atomic.Int64
	inner   fronius.Doer
	lastURL string
}

func (d *countingDoer) Do(r *http.Request) (*http.Response, error) {
	d.count.Add(1)
	return d.inner.Do(r)
}

func TestAllowlistIsReadOnlyGetEndpoints(t *testing.T) {
	list := Allowlist()
	if len(list) != 5 {
		t.Fatalf("allowlist = %d entries, want 5", len(list))
	}
	want := map[string]Envelope{
		VersionPath:      EnvelopeDefault,
		ActiveEventsPath: EnvelopeDefault,
		PowerMeterPath:   EnvelopeCgi,
		BatteryPath:      EnvelopeCgi,
		PowerFlowPath:    EnvelopeDefault,
	}
	for _, ep := range list {
		if ep.Class != ClassRead {
			t.Fatalf("%s: class %q, only READ is allowed", ep.Path, ep.Class)
		}
		if !ep.Digest {
			t.Fatalf("%s: the polled registry path is authenticated", ep.Path)
		}
		if ep.Purpose == "" || ep.Evidence == "" {
			t.Fatalf("%s: purpose and evidence are mandatory", ep.Path)
		}
		expected, ok := want[ep.Path]
		if !ok {
			t.Fatalf("unexpected path in the allowlist: %s", ep.Path)
		}
		if ep.Envelope != expected {
			t.Fatalf("%s: envelope %q, want %q", ep.Path, ep.Envelope, expected)
		}
		delete(want, ep.Path)
	}
	if len(want) != 0 {
		t.Fatalf("missing paths: %v", want)
	}

	// The list is a copy: corrupting it must not modify the table.
	Allowlist()[0].Path = "/corrupted"
	if _, ok := Lookup(VersionPath); !ok {
		t.Fatal("the allowlist was corrupted by its caller")
	}
}

// TestAllowlistRejectsEverythingElse: a path outside the list is refused before
// any request construction.
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
			t.Fatalf("%q: error = %v, want ErrNotAllowed", path, err)
		}
	}
	if got := doer.count.Load(); got != 0 {
		t.Fatalf("requests emitted = %d, want 0", got)
	}
	if _, err := c.ComponentPresent(context.Background(), "/config/meter"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("ComponentPresent outside the list: %v", err)
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
		t.Fatalf("request = %s %s", method, path)
	}
}

func TestVersionExtractsKnownAndCandidateKeys(t *testing.T) {
	cases := []struct {
		name string
		body string
		want Version
	}{
		{
			name: "proven keys only",
			body: `{"serialNumber":"S1","devicegroup":"GEN24PRIMO","numberOfPhases":3}`,
			want: Version{
				SerialNumber: new("S1"),
				DeviceGroup:  new("GEN24PRIMO"),
				// `devicegroup` is not a model: it is the device group, a sibling
				// field. The model stays nil, never inferred from another field.
				Model:          nil,
				NumberOfPhases: new(3),
			},
		},
		{
			name: "captured identity: name and article number beside the serial",
			body: `{"serialNumber":"S","devicename":"Primo GEN24 3.6 Plus","articleNumber":"4,210,141,002"}`,
			want: Version{
				SerialNumber:  new("S"),
				DeviceName:    new("Primo GEN24 3.6 Plus"),
				ArticleNumber: new("4,210,141,002"),
			},
		},
		{
			name: "no name and no article number: both stay nil",
			body: `{"serialNumber":"S"}`,
			want: Version{SerialNumber: new("S")},
		},
		{
			name: "device name is not a model: devicename never fills model",
			body: `{"serialNumber":"S","devicename":"Primo GEN24 3.6 Plus"}`,
			want: Version{SerialNumber: new("S"), DeviceName: new("Primo GEN24 3.6 Plus")},
		},
		{
			name: "version read on the proven path swrevisions.GEN24",
			body: `{"serialNumber":"S","swrevisions":{"GEN24":"1.19.7-1","GEN24PRIMO":"1.19.7-1"},"firmwareVersion":"0.0-unknown"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.19.7-1")},
		},
		{
			name: "proven path case-insensitive, nested object under another name ignored",
			body: `{"serialNumber":"S","swrevisions":{"gen24":"1.19.7-1"},"SWRevisions":{"GEN24":"9.9.9"}}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.19.7-1")},
		},
		{
			name: "swrevisions without GEN24: fallback to the flat candidate",
			body: `{"serialNumber":"S","swrevisions":{"OTHER":"x"},"firmwareVersion":"1.2"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.2")},
		},
		{
			name: "swrevisions of another type: neither panic nor invented value",
			body: `{"serialNumber":"S","swrevisions":"1.19.7-1"}`,
			want: Version{SerialNumber: new("S")},
		},
		{
			name: "simulator candidates",
			body: `{"serialNumber":"SIMULATED-0001","devicegroup":"GEN24PRIMO","numberOfPhases":3,"firmwareVersion":"1.24.3-sim","modelName":"Fronius Primo (simulated)"}`,
			want: Version{
				SerialNumber:   new("SIMULATED-0001"),
				DeviceGroup:    new("GEN24PRIMO"),
				Model:          new("Fronius Primo (simulated)"),
				Firmware:       new("1.24.3-sim"),
				NumberOfPhases: new(3),
			},
		},
		{
			name: "priority respected: modelName before model",
			body: `{"serialNumber":"S","model":"generic","modelName":"specific"}`,
			want: Version{SerialNumber: new("S"), Model: new("specific")},
		},
		{
			name: "priority respected: firmwareVersion before firmware",
			body: `{"serialNumber":"S","firmware":"old","firmwareVersion":"1.24.3"}`,
			want: Version{SerialNumber: new("S"), Firmware: new("1.24.3")},
		},
		{
			name: "empty key ignored: move to the next",
			body: `{"serialNumber":"","serial":"S2"}`,
			want: Version{SerialNumber: new("S2")},
		},
		{
			name: "key case tolerated",
			body: `{"SerialNumber":"S3","Numberofphases":1}`,
			want: Version{SerialNumber: new("S3"), NumberOfPhases: new(1)},
		},
		{
			name: "number of phases as a string",
			body: `{"serialNumber":"S","numberOfPhases":"1"}`,
			want: Version{SerialNumber: new("S"), NumberOfPhases: new(1)},
		},
		{
			name: "unknown keys tolerated, absent ones stay nil",
			body: `{"serialNumber":"S","unknownField":{"a":1}}`,
			want: Version{SerialNumber: new("S")},
		},
		{
			name: "number of phases absent",
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
		A, B, C, D, E, F *string
		G                *int
	}{got.SerialNumber, got.DeviceName, got.ArticleNumber, got.Model, got.Firmware, got.DeviceGroup, got.NumberOfPhases})
	wantJSON, _ := json.Marshal(struct {
		A, B, C, D, E, F *string
		G                *int
	}{want.SerialNumber, want.DeviceName, want.ArticleNumber, want.Model, want.Firmware, want.DeviceGroup, want.NumberOfPhases})
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("Version = %s, want %s", gotJSON, wantJSON)
	}
}

func TestVersionDecodeErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"truncated body", `{"serialNumber":"S`},
		{"root array", `[1,2,3]`},
		{"root string", `"text"`},
		{"root null", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := newClient(t, srv.URL, &http.Client{}).Version(context.Background())
			if err == nil {
				t.Fatal("a decode error was expected")
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
		{"empty Data: absent", `{"Body":{"Data":{}}}`, false},
		{"nil Data: absent", `{"Body":{"Data":null}}`, false},
		{"Body without Data: absent", `{"Body":{}}`, false},
		{"one component: present", `{"Body":{"Data":{"0":{"Serial":"SIM-METER-0"}}}}`, true},
		{"two components: present", `{"Body":{"Data":{"0":{},"1":{}}}}`, true},
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
				t.Fatalf("presence = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestComponentPresentNeverConfusesErrorWithAbsence: an unreadable body is a
// decode error, never "absent". A response we cannot read proves no absence.
func TestComponentPresentNeverConfusesErrorWithAbsence(t *testing.T) {
	for _, body := range []string{`{"Body":{"Data":`, `{"Body":{"Data":`, ``, `{"Body":{"Data":[1,2]}}`, `{"other":"shape"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		_, err := newClient(t, srv.URL, &http.Client{}).ComponentPresent(context.Background(), PowerMeterPath)
		srv.Close()

		if err == nil {
			t.Fatalf("body %q: an error was expected, not an absence", body)
		}
		if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
			t.Fatalf("body %q: nature = %q", body, kind)
		}
	}
}

func TestComponentPresentRejectsNonCgiPath(t *testing.T) {
	c := newClient(t, "http://127.0.0.1:1", &http.Client{})
	_, err := c.ComponentPresent(context.Background(), VersionPath)
	if err == nil {
		t.Fatal("a path with a Default envelope is not a component")
	}
}

func TestPowerFlowDecodesTheInternalShape(t *testing.T) {
	// Keys as the vendor client reads them (bundle l. 129034, 129043-129063); a
	// real capture is still missing, so the values below are illustrative.
	const body = `{
	  "site": {"P_PV": 228.35, "P_Grid": null, "P_Load": null, "P_Akku": null,
	           "E_Day": 4123.4, "E_Year": 4215488.4, "E_Total": 15089488.9},
	  "inverters": {"1": {"DT": 1, "P": 185.58, "SOC": null}},
	  "Smartloads": {"Ohmpilots": {}}
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	pf, err := newClient(t, srv.URL, &http.Client{}).PowerFlow(context.Background())
	if err != nil {
		t.Fatalf("PowerFlow: %v", err)
	}
	if pf.Site.E_Day == nil || *pf.Site.E_Day != 4123.4 {
		t.Fatalf("site.E_Day = %v", pf.Site.E_Day)
	}
	if pf.Site.E_Year == nil || *pf.Site.E_Year != 4215488.4 {
		t.Fatalf("site.E_Year = %v", pf.Site.E_Year)
	}
	// A null stays nil: no invented zero.
	if pf.Site.P_Grid != nil || pf.Site.P_Akku != nil {
		t.Fatalf("null powers must stay nil: %+v", pf.Site)
	}
	if len(pf.Inverters) != 1 || pf.Inverters["1"].P == nil || *pf.Inverters["1"].P != 185.58 {
		t.Fatalf("inverters = %+v", pf.Inverters)
	}
	if pf.Inverters["1"].SOC != nil {
		t.Fatalf("SOC must stay nil: %v", pf.Inverters["1"].SOC)
	}
}

func TestPowerFlowRejectsAnUnexpectedShape(t *testing.T) {
	// An empty object is not an empty measurement: it is a body we cannot read.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL, &http.Client{}).PowerFlow(context.Background())
	if err == nil {
		t.Fatal("an empty body must be a decode error")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("KindOf = %q, want %q", kind, froniuserr.KindDecode)
	}
}

func TestActiveEventsForms(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "indexed object, sorted keys",
			body: `{"Body":{"Data":{"1":{"code":"B"},"0":{"code":"A"}}}}`,
			want: []string{`{"code":"A"}`, `{"code":"B"}`},
		},
		{
			name: "no event",
			body: `{"Body":{"Data":{}}}`,
			want: []string{},
		},
		{
			name: "nil Data: no event",
			body: `{"Body":{"Data":null}}`,
			want: []string{},
		},
		{
			name: "fallback to root array",
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
				t.Fatalf("events = %d, want %d", len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if compactJSON(t, got[i]) != want {
					t.Fatalf("event %d = %s, want %s", i, got[i], want)
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
		t.Fatal("a decode error was expected")
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
			t.Fatalf("status %d: error expected", tc.status)
		}
		if kind, _ := froniuserr.KindOf(err); kind != tc.want {
			t.Fatalf("status %d: nature = %q, want %q", tc.status, kind, tc.want)
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
		t.Fatal("an out-of-limit body must be refused")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("nature = %q", kind)
	}
}

func TestNewValidatesBaseURL(t *testing.T) {
	for _, url := range []string{"ftp://x/y", "127.0.0.1:8081", ""} {
		if _, err := New(url, &http.Client{}, time.Second, nil); err == nil {
			t.Fatalf("URL %q should have been refused", url)
		}
	}
}
