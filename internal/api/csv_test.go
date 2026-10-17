package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// exportCSV performs the request, checks the export's own response headers and
// decodes the body with encoding/csv: the round trip through csv.Reader is what
// proves the quoting of commas, quotes and newlines.
func exportCSV(t *testing.T, srv *httptest.Server, path string) (string, http.Header, [][]string) {
	t.Helper()
	code, header, body := doRequest(t, srv, http.MethodGet, path)
	if code != http.StatusOK {
		t.Fatalf("%s: status = %d (%s)", path, code, body)
	}
	if got := header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("%s: Content-Type = %q", path, got)
	}
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("%s: Cache-Control = %q, wanted no-store", path, got)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("%s: reading the CSV: %v\n%s", path, err, body)
	}
	return string(body), header, rows
}

// plainFloat mirrors the contract's number cell: '.' separator, no exponent,
// no thousands separator.
func plainFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func insertSamplePtr(t *testing.T, store *storage.Store, ts time.Time,
	pv, eDay, total *float64) {
	t.Helper()
	sm := model.Sample{
		TS: ts, TSSource: model.TSSourceDevice, Source: model.SourceSolarAPI,
		Quality: model.QualityMeasured, Mode: "produce-only",
		PowerPVW: pv, EnergyDayWh: eDay, EnergyTotalWh: total,
	}
	if err := store.InsertSample(context.Background(), sm); err != nil {
		t.Fatalf("InsertSample: %v", err)
	}
}

// TestCSVExportMatchesJSONPerRoute: one export per route, with the exact header
// line, the exact column order, and cells that carry the same values as the JSON
// route for the same query.
func TestCSVExportMatchesJSONPerRoute(t *testing.T) {
	t.Run("timeseries", func(t *testing.T) {
		srv, httpSrv, store := newTestServer(t, nil)
		srv.now = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

		base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
		insertSample(t, store, base, 100, 1)
		insertSample(t, store, base.Add(30*time.Minute), 300, 2)

		q := "/api/v1/timeseries?from=2026-10-14T10:00:00Z&to=2026-10-14T11:00:00Z&step=1h"
		_, _, jsonBody := doRequest(t, httpSrv, http.MethodGet, q)
		var buckets []bucketDTO
		if err := json.Unmarshal(decodeEnvelope(t, jsonBody).Data, &buckets); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(buckets) != 1 {
			t.Fatalf("buckets = %d, wanted 1", len(buckets))
		}

		_, header, rows := exportCSV(t, httpSrv, q+"&format=csv")
		wantName := `attachment; filename="fronius-timeseries_20261014T100000Z_20261014T110000Z.csv"`
		if got := header.Get("Content-Disposition"); got != wantName {
			t.Fatalf("Content-Disposition = %q, wanted %q", got, wantName)
		}
		wantHeader := []string{"start", "count", "missing", "power_pv_avg_w", "power_pv_min_w", "power_pv_max_w"}
		if !slices.Equal(rows[0], wantHeader) {
			t.Fatalf("header = %v, wanted %v", rows[0], wantHeader)
		}
		if len(rows) != len(buckets)+1 {
			t.Fatalf("%d rows for %d buckets", len(rows), len(buckets))
		}
		for i, b := range buckets {
			want := []string{
				b.Start,
				strconv.Itoa(b.Count),
				strconv.Itoa(b.Missing),
				plainFloat(b.PowerPVAvgW),
				plainFloat(b.PowerPVMinW),
				plainFloat(b.PowerPVMaxW),
			}
			if !slices.Equal(rows[i+1], want) {
				t.Fatalf("row %d = %v, wanted %v", i+1, rows[i+1], want)
			}
		}
		// The JSON route is unchanged and still answers meta.
		decodeMeta(t, decodeEnvelope(t, jsonBody))
	})

	t.Run("daily energy", func(t *testing.T) {
		srv, httpSrv, store := newTestServer(t, nil)

		// 16 October is CEST (UTC+2): its local day runs from 21:59Z on the 15th
		// to 21:59Z on the 16th. The device reports an E_Day on the 16th, the
		// derivation measures it too, and the 17th has nothing at all.
		total := func(v float64) *float64 { return new(v) }
		insertSamplePtr(t, store, time.Date(2026, 10, 15, 21, 59, 0, 0, time.UTC),
			new(0.0), nil, total(1_000_000))
		insertSamplePtr(t, store, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC),
			new(800.0), new(500.0), total(1_001_000))
		insertSamplePtr(t, store, time.Date(2026, 10, 16, 21, 59, 0, 0, time.UTC),
			new(0.0), new(500.0), total(1_002_000))
		srv.now = func() time.Time { return time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC) }

		q := "/api/v1/energy/daily?from=2026-10-15T22:00:00Z&to=2026-10-17T22:00:00Z"
		_, _, jsonBody := doRequest(t, httpSrv, http.MethodGet, q)
		var days []dailyEnergyDTO
		if err := json.Unmarshal(decodeEnvelope(t, jsonBody).Data, &days); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if len(days) != 2 {
			t.Fatalf("days = %+v, wanted 2", days)
		}

		_, header, rows := exportCSV(t, httpSrv, q+"&format=csv")
		wantName := `attachment; filename="fronius-energy-daily_20261015T220000Z_20261017T220000Z.csv"`
		if got := header.Get("Content-Disposition"); got != wantName {
			t.Fatalf("Content-Disposition = %q, wanted %q", got, wantName)
		}
		wantHeader := []string{"date", "energy_wh", "derived_wh", "derived_reason", "partial"}
		if !slices.Equal(rows[0], wantHeader) {
			t.Fatalf("header = %v, wanted %v", rows[0], wantHeader)
		}
		if len(rows) != len(days)+1 {
			t.Fatalf("%d rows for %d days", len(rows), len(days))
		}
		for i, d := range days {
			want := []string{
				d.Date,
				plainFloat(d.EnergyWh),
				plainFloat(d.DerivedWh),
				d.DerivedReason,
				strconv.FormatBool(d.Partial),
			}
			if !slices.Equal(rows[i+1], want) {
				t.Fatalf("row %d = %v, wanted %v", i+1, rows[i+1], want)
			}
		}
		// The measured counter and the derived value both made it to the export.
		if rows[1][0] != "2026-10-16" || rows[1][1] != "500" || rows[1][2] != "2000" {
			t.Fatalf("16/10 = %v", rows[1])
		}
	})

	t.Run("events", func(t *testing.T) {
		srv, httpSrv, store := newTestServer(t, nil)
		srv.now = func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) }

		for i := range 3 {
			if err := store.InsertEvent(context.Background(), model.Event{
				TS:       time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute),
				Kind:     model.EventCollectorError,
				Severity: model.SeverityWarning,
				Message:  "collection failure",
				Detail:   "fronius: server | HTTP 503",
			}); err != nil {
				t.Fatalf("InsertEvent: %v", err)
			}
		}

		q := "/api/v1/events?from=2026-10-16T00:00:00Z&to=2026-10-17T00:00:00Z&limit=10"
		_, _, jsonBody := doRequest(t, httpSrv, http.MethodGet, q)
		var events []eventDTO
		if err := json.Unmarshal(decodeEnvelope(t, jsonBody).Data, &events); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		_, header, rows := exportCSV(t, httpSrv, q+"&format=csv")
		wantName := `attachment; filename="fronius-events_20261016T000000Z_20261017T000000Z.csv"`
		if got := header.Get("Content-Disposition"); got != wantName {
			t.Fatalf("Content-Disposition = %q, wanted %q", got, wantName)
		}
		wantHeader := []string{"ts", "kind", "severity", "message", "detail"}
		if !slices.Equal(rows[0], wantHeader) {
			t.Fatalf("header = %v, wanted %v", rows[0], wantHeader)
		}
		if len(rows) != len(events)+1 {
			t.Fatalf("%d rows for %d events", len(rows), len(events))
		}
		for i, e := range events {
			want := []string{e.TS, e.Kind, e.Severity, e.Message, e.Detail}
			if !slices.Equal(rows[i+1], want) {
				t.Fatalf("row %d = %v, wanted %v", i+1, rows[i+1], want)
			}
		}
		// Newest first, like the API's own order.
		if events[0].TS != "2026-10-16T08:02:00.000Z" || rows[1][0] != "2026-10-16T08:02:00.000Z" {
			t.Fatalf("order: first event %q, first row %q", events[0].TS, rows[1][0])
		}
	})
}

// TestCSVNullCellsAreEmpty: an unmeasured value is an empty cell, never 0 and
// never "null".
func TestCSVNullCellsAreEmpty(t *testing.T) {
	t.Run("timeseries bucket without PV", func(t *testing.T) {
		srv, httpSrv, store := newTestServer(t, nil)
		srv.now = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
		insertSamplePtr(t, store, time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC), nil, nil, nil)

		_, _, rows := exportCSV(t, httpSrv,
			"/api/v1/timeseries?from=2026-10-14T10:00:00Z&to=2026-10-14T11:00:00Z&step=1h&format=csv")
		if len(rows) != 2 {
			t.Fatalf("rows = %v", rows)
		}
		want := []string{"2026-10-14T10:00:00.000Z", "1", "1", "", "", ""}
		if !slices.Equal(rows[1], want) {
			t.Fatalf("row = %v, wanted %v", rows[1], want)
		}
	})

	t.Run("daily day without energy", func(t *testing.T) {
		srv, httpSrv, store := newTestServer(t, nil)
		total := func(v float64) *float64 { return new(v) }
		insertSamplePtr(t, store, time.Date(2026, 10, 15, 21, 59, 0, 0, time.UTC),
			new(0.0), nil, total(1_000_000))
		insertSamplePtr(t, store, time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC),
			new(800.0), new(500.0), total(1_001_000))
		insertSamplePtr(t, store, time.Date(2026, 10, 16, 21, 59, 0, 0, time.UTC),
			new(0.0), new(500.0), total(1_002_000))
		srv.now = func() time.Time { return time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC) }

		_, _, rows := exportCSV(t, httpSrv,
			"/api/v1/energy/daily?from=2026-10-15T22:00:00Z&to=2026-10-17T22:00:00Z&format=csv")
		if len(rows) != 3 {
			t.Fatalf("rows = %v", rows)
		}
		if rows[2][0] != "2026-10-17" {
			t.Fatalf("second day = %v", rows[2])
		}
		if rows[2][1] != "" || rows[2][2] != "" {
			t.Fatalf("a day with neither value must have empty cells, got %v", rows[2])
		}
	})
}

// TestCSVNeutralisesFormulas: only text cells are prefixed, and only once; a
// negative number keeps its sign. Commas, quotes and newlines survive the CSV
// round trip.
func TestCSVNeutralisesFormulas(t *testing.T) {
	_, httpSrv, store := newTestServer(t, nil)
	ctx := context.Background()

	cases := []struct {
		ts      time.Time
		message string
		want    string
	}{
		{time.Date(2026, 10, 16, 8, 4, 0, 0, time.UTC), `=cmd|' /C calc'!A0`, `'=cmd|' /C calc'!A0`},
		{time.Date(2026, 10, 16, 8, 3, 0, 0, time.UTC), `+DDE("cmd";"calc")`, `'+DDE("cmd";"calc")`},
		{time.Date(2026, 10, 16, 8, 2, 0, 0, time.UTC), `-2+3`, `'-2+3`},
		{time.Date(2026, 10, 16, 8, 1, 0, 0, time.UTC), `@SUM(A1:A2)`, `'@SUM(A1:A2)`},
		{time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), "\tTAB first", "'\tTAB first"},
	}
	for _, c := range cases {
		if err := store.InsertEvent(ctx, model.Event{
			TS: c.ts, Kind: model.EventCollectorError, Severity: model.SeverityWarning,
			Message: c.message,
			// The detail carries the characters encoding/csv must quote.
			Detail: "comma, quote \" and\nnewline",
		}); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	_, _, rows := exportCSV(t, httpSrv,
		"/api/v1/events?from=2026-10-16T00:00:00Z&to=2026-10-17T00:00:00Z&format=csv")
	byTS := map[string][]string{}
	for _, row := range rows[1:] {
		byTS[row[0]] = row
	}
	for _, c := range cases {
		ts := formatTime(c.ts)
		row, ok := byTS[ts]
		if !ok {
			t.Fatalf("no row for %s", ts)
		}
		if row[3] != c.want {
			t.Fatalf("message %q exported as %q, wanted %q", c.message, row[3], c.want)
		}
		if row[4] != "comma, quote \" and\nnewline" {
			t.Fatalf("detail round trip = %q", row[4])
		}
	}

	// A number that legitimately starts with '-' is not a formula: its cell is
	// written as is, without the apostrophe.
	_, httpSrv2, store2 := newTestServer(t, nil)
	insertSample(t, store2, time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC), -50, 0)
	_, _, rows = exportCSV(t, httpSrv2,
		"/api/v1/timeseries?from=2026-10-14T10:00:00Z&to=2026-10-14T11:00:00Z&step=1h&format=csv")
	want := []string{"2026-10-14T10:00:00.000Z", "1", "0", "-50", "-50", "-50"}
	if !slices.Equal(rows[1], want) {
		t.Fatalf("negative numeric row = %v, wanted %v", rows[1], want)
	}
}

// TestCSVErrorsBeforeTheFirstByte: the format branch comes after the range
// validation, so a bad range stays a JSON 400 and no CSV header is written.
func TestCSVErrorsBeforeTheFirstByte(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	cases := []struct {
		name   string
		path   string
		header string
	}{
		{
			"timeseries invalid from",
			"/api/v1/timeseries?from=yesterday&to=2026-10-16T00:00:00Z&format=csv",
			"start,count,missing",
		},
		{
			"timeseries reversed range",
			"/api/v1/timeseries?from=2026-10-16T00:00:00Z&to=2026-10-15T00:00:00Z&format=csv",
			"start,count,missing",
		},
		{
			"daily invalid from",
			"/api/v1/energy/daily?from=yesterday&format=csv",
			"date,energy_wh",
		},
		{
			"daily reversed range",
			"/api/v1/energy/daily?from=2026-10-16T00:00:00Z&to=2026-10-15T00:00:00Z&format=csv",
			"date,energy_wh",
		},
		{
			"events invalid from",
			"/api/v1/events?from=yesterday&format=csv",
			"ts,kind,severity",
		},
		{
			"events reversed range",
			"/api/v1/events?from=2026-10-16T00:00:00Z&to=2026-10-15T00:00:00Z&format=csv",
			"ts,kind,severity",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, header, body := doRequest(t, httpSrv, http.MethodGet, tc.path)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", code, body)
			}
			if got := header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := header.Get("Content-Disposition"); got != "" {
				t.Fatalf("a rejected export must not be named: %q", got)
			}
			if len(body) == 0 || body[0] != '{' {
				t.Fatalf("body is not a JSON envelope: %s", body)
			}
			env := decodeEnvelope(t, body)
			if env.Error == nil || env.Error.Code != CodeInvalidRange {
				t.Fatalf("error = %+v", env.Error)
			}
			if strings.Contains(string(body), tc.header) {
				t.Fatalf("a CSV header leaked into the error body: %s", body)
			}
		})
	}
}

// TestCSVFormatParameter: format=json is today's behaviour byte for byte,
// format=xml is a 400 invalid_format, and the CSV responses are not cacheable.
func TestCSVFormatParameter(t *testing.T) {
	srv, httpSrv, store := newTestServer(t, nil)
	srv.now = func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) }

	insertSample(t, store, time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), 100, 1)
	insertSamplePtr(t, store, time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC),
		new(0.0), nil, new(float64(1000)))

	paths := []string{
		"/api/v1/timeseries?from=2026-10-16T08:00:00Z&to=2026-10-16T09:00:00Z&step=1h",
		"/api/v1/energy/daily?from=2026-10-15T22:00:00Z&to=2026-10-16T22:00:00Z",
		"/api/v1/events?limit=5",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			code, header, plain := doRequest(t, httpSrv, http.MethodGet, p)
			if code != http.StatusOK {
				t.Fatalf("status = %d (%s)", code, plain)
			}
			if got := header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("JSON Cache-Control = %q", got)
			}
			_, _, withJSON := doRequest(t, httpSrv, http.MethodGet, p+"&format=json")
			if string(plain) != string(withJSON) {
				t.Fatalf("format=json changed the body:\n%s\n%s", plain, withJSON)
			}

			code, _, body := doRequest(t, httpSrv, http.MethodGet, p+"&format=xml")
			if code != http.StatusBadRequest {
				t.Fatalf("format=xml: status = %d (%s)", code, body)
			}
			env := decodeEnvelope(t, body)
			if env.Error == nil || env.Error.Code != CodeInvalidFormat {
				t.Fatalf("format=xml: error = %+v", env.Error)
			}
			if strings.Contains(string(body), ",") && strings.Contains(string(body), "\n") &&
				strings.HasPrefix(string(body), "start,") {
				t.Fatalf("format=xml produced a CSV body: %s", body)
			}
		})
	}
}

// TestCSVFilenameIsPerRouteAndParsed: the name carries the route and the parsed
// instants, and no query text can reach it; an unbounded events export states the
// zero instant instead of inventing a bound.
func TestCSVFilenameIsPerRouteAndParsed(t *testing.T) {
	_, httpSrv, _ := newTestServer(t, nil)

	cases := []struct {
		path string
		want string
	}{
		{
			"/api/v1/timeseries?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&step=1h&format=csv",
			`attachment; filename="fronius-timeseries_20261001T000000Z_20261002T000000Z.csv"`,
		},
		{
			"/api/v1/energy/daily?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&format=csv",
			`attachment; filename="fronius-energy-daily_20261001T000000Z_20261002T000000Z.csv"`,
		},
		{
			"/api/v1/events?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&format=csv",
			`attachment; filename="fronius-events_20261001T000000Z_20261002T000000Z.csv"`,
		},
		{
			// The Events page's "all" preset sends no bounds at all.
			"/api/v1/events?format=csv",
			`attachment; filename="fronius-events_all_all.csv"`,
		},
		{
			"/api/v1/events?from=2026-10-01T00:00:00Z&format=csv",
			`attachment; filename="fronius-events_20261001T000000Z_all.csv"`,
		},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		_, header, _ := exportCSV(t, httpSrv, tc.path)
		got := header.Get("Content-Disposition")
		if got != tc.want {
			t.Fatalf("%s: Content-Disposition = %q, wanted %q", tc.path, got, tc.want)
		}
		if strings.Contains(got, "00010101") {
			t.Fatalf("%s: an absent bound rendered as the zero instant: %q", tc.path, got)
		}
		if seen[got] {
			t.Fatalf("%s: duplicate filename %q", tc.path, got)
		}
		seen[got] = true
	}

	// A raw query value that does not parse as an instant is rejected before any
	// byte is written, so its text has no way into a filename.
	code, header, body := doRequest(t, httpSrv, http.MethodGet,
		"/api/v1/timeseries?from=2026-10-01T00:00:00Z%22;evil&to=2026-10-02T00:00:00Z&format=csv")
	if code != http.StatusBadRequest {
		t.Fatalf("malicious from: status = %d (%s)", code, body)
	}
	if got := header.Get("Content-Disposition"); got != "" {
		t.Fatalf("malicious from: Content-Disposition = %q", got)
	}
	if strings.Contains(header.Get("Content-Disposition"), "evil") {
		t.Fatal("raw query text leaked into the filename")
	}
}
