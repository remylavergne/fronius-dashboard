package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The CSV export is the only non-JSON body of the API. It carries exactly the
// data of the JSON route — same parameters, same validation — and changes only
// the representation (see the frozen contract,
// local://fronius-csv-export-contract.md).

const (
	// csvTimeLayout renders an instant in a filename: UTC, no colon, no space.
	csvTimeLayout = "20060102T150405Z"
	// csvContentType is the media type of every export.
	csvContentType = "text/csv; charset=utf-8"
	// formulaPrefixes are the first characters a spreadsheet reads as the
	// start of a formula. A text cell beginning with one is prefixed with an
	// apostrophe, which every spreadsheet treats as "this is text".
	formulaPrefixes = "=+-@\t\r"
	// invalidFormatMessage is the 400 body when `format` names an unknown
	// representation.
	invalidFormatMessage = "format must be json or csv"
)

// csvCell is one cell of an export, text or not.
//
// The distinction is not cosmetic: the formula-injection prefix applies to
// device- and user-originated strings only, because a number legitimately
// starts with '-' and prefixing it would corrupt the value.
type csvCell struct {
	value string
	text  bool
}

// textCell is a device- or user-originated string; it is neutralised.
func textCell(s string) csvCell { return csvCell{value: s, text: true} }

// nullCell is an absent value: an empty cell, never 0 and never "null".
func nullCell() csvCell { return csvCell{} }

// intCell is an integer written as is.
func intCell(v int) csvCell { return csvCell{value: strconv.Itoa(v)} }

// floatCell is a nullable number written with a '.' decimal separator and no
// exponent, so a spreadsheet reads it as a number.
func floatCell(v *float64) csvCell {
	if v == nil {
		return nullCell()
	}
	return csvCell{value: strconv.FormatFloat(*v, 'f', -1, 64)}
}

// boolCell is written true or false, as in the JSON body.
func boolCell(v bool) csvCell {
	if v {
		return csvCell{value: "true"}
	}
	return csvCell{value: "false"}
}

// exported returns the value that encoding/csv must quote and write.
func (c csvCell) exported() string {
	if !c.text || c.value == "" {
		return c.value
	}
	if strings.IndexByte(formulaPrefixes, c.value[0]) >= 0 {
		return "'" + c.value
	}
	return c.value
}

// exportFormat reads the requested representation. An absent parameter keeps
// today's JSON body. The caller runs this after the range and step validation:
// an invalid range must stay a JSON 400, never a CSV header.
func exportFormat(q url.Values) (string, bool) {
	switch q.Get("format") {
	case "", "json":
		return "json", true
	case "csv":
		return "csv", true
	default:
		return "", false
	}
}

// exportFilename names the downloaded file after the route and the PARSED
// instants, never after the raw query text.
//
// An absent bound is rendered `all`, not as the zero instant: the events route
// accepts an unbounded query (the Events page's "all" preset sends exactly
// that), and `00010101T000000Z` would read as a year rather than as the absence
// of a bound. `all` states the request; it invents no range.
func exportFilename(route string, from, to time.Time) string {
	return fmt.Sprintf("fronius-%s_%s_%s.csv", route,
		filenameInstant(from), filenameInstant(to))
}

// filenameInstant renders one bound, or `all` when the query had none.
func filenameInstant(t time.Time) string {
	if t.IsZero() {
		return "all"
	}
	return t.UTC().Format(csvTimeLayout)
}

// writeCSV streams the export: the header line, then one row per record.
//
// The status line is sent before the first row, so a failure afterwards can no
// longer change it; the error is logged, as the streaming contract allows. The
// rows are never buffered: encoding/csv writes through to the response.
func (s *Server) writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]csvCell) {
	w.Header().Set("Content-Type", csvContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	if err := cw.Write(header); err != nil {
		s.log.Error("writing the CSV export failed", "file", filename, "error", err)
		return
	}
	record := make([]string, len(header))
	for _, row := range rows {
		for i, cell := range row {
			record[i] = cell.exported()
		}
		if err := cw.Write(record[:len(row)]); err != nil {
			s.log.Error("writing the CSV export failed", "file", filename, "error", err)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		s.log.Error("writing the CSV export failed", "file", filename, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Rows, one builder per route (columns are the JSON field names, in order)
// ---------------------------------------------------------------------------

var timeSeriesCSVHeader = []string{
	"start", "count", "missing", "power_pv_avg_w", "power_pv_min_w", "power_pv_max_w",
}

func timeSeriesCSVRows(buckets []bucketDTO) [][]csvCell {
	out := make([][]csvCell, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, []csvCell{
			textCell(b.Start),
			intCell(b.Count),
			intCell(b.Missing),
			floatCell(b.PowerPVAvgW),
			floatCell(b.PowerPVMinW),
			floatCell(b.PowerPVMaxW),
		})
	}
	return out
}

var dailyEnergyCSVHeader = []string{
	"date", "energy_wh", "derived_wh", "derived_reason", "partial",
}

func dailyEnergyCSVRows(days []dailyEnergyDTO) [][]csvCell {
	out := make([][]csvCell, 0, len(days))
	for _, d := range days {
		out = append(out, []csvCell{
			textCell(d.Date),
			floatCell(d.EnergyWh),
			floatCell(d.DerivedWh),
			textCell(d.DerivedReason),
			boolCell(d.Partial),
		})
	}
	return out
}

var eventsCSVHeader = []string{"ts", "kind", "severity", "message", "detail"}

func eventsCSVRows(events []eventDTO) [][]csvCell {
	out := make([][]csvCell, 0, len(events))
	for _, e := range events {
		out = append(out, []csvCell{
			textCell(e.TS),
			textCell(e.Kind),
			textCell(e.Severity),
			textCell(e.Message),
			textCell(e.Detail),
		})
	}
	return out
}
