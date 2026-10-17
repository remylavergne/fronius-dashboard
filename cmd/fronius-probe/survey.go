// The survey: one read-only GET per catalogued path, and what came back.
//
// A single-probe run answers "what does this path say right now". A survey
// answers the question the source catalogue poses and cannot answer by itself:
// **which of the catalogued paths does this firmware actually serve, with what,
// and behind what authentication**. It walks the whole catalogue sequentially,
// records one outcome per path — including the paths that answer nothing, which
// is a fact and not a failure — and never aborts on a 404 or a 401: those are
// results.
//
// Nothing here writes to the device: every request is a GET, built from the
// allowlist derived from the catalogue, and the read-only guard sits in front of
// the transport in both directions (bare and authenticated).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/digest"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/storage"
)

// versionPath is the identity path of the allowlist: its answer carries the
// firmware version the survey is attributed to.
var versionPath = internalapi.VersionPath

// surveyLocation is the timezone the database is opened with (the store groups
// by local day; a survey does not, but the store must be opened with the same
// installation timezone as the application).
func surveyLocation() (*time.Location, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return cfg.Location()
}

// maxSurveyFields bounds the inventory of one answer. A path that carries more
// is reported as truncated rather than silently cut.
const maxSurveyFields = 500

// maxSurveyDepth bounds the walk of a nested answer.
const maxSurveyDepth = 8

// Survey classes. They are the vocabulary of the recorded result, and they keep
// distinct facts distinct: "the path is absent from this firmware" is not "we
// were refused", and neither is "it answered, and carries nothing".
const (
	classOK           = "ok"
	classEmpty        = "empty"
	classNotJSON      = "not_json"
	classAuthRequired = "auth_required"
	classNotFound     = "not_found"
	classError        = "error"
)

// surveyField is one value of an answer: its JSON path and its type. Never a
// value — a survey of an identity endpoint must not become a copy of the
// device's serial numbers.
type surveyField struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	ArrayLen int    `json:"array_len,omitempty"`
}

// surveyEntry is the outcome of one path.
type surveyEntry struct {
	Path           string        `json:"path"`
	Query          string        `json:"query,omitempty"`
	Class          string        `json:"class"`
	HTTPStatus     int           `json:"http_status"`
	DigestRequired bool          `json:"digest_required"`
	Bytes          int           `json:"bytes"`
	Error          string        `json:"error,omitempty"`
	Fields         []surveyField `json:"fields,omitempty"`
}

// surveyReport is what --save writes and what the database records, one row per
// entry.
type surveyReport struct {
	BaseURL    string         `json:"base_url"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Firmware   string         `json:"firmware,omitempty"`
	Counts     map[string]int `json:"counts"`
	Entries    []surveyEntry  `json:"entries"`
}

// runSurvey walks the whole catalogue with --delay between paths.
//
// A failing path is recorded, never fatal: the survey's value is precisely the
// paths that answer nothing or refuse to answer.
func runSurvey(ctx context.Context, opt options, base *url.URL, cred credentials, logger *slog.Logger) error {
	// A placeholder is never sent: the survey has no target to attach a value
	// to, so the entry is skipped with one notice and stays in neither the
	// report nor the database. Its classification remains in the catalogue.
	targets := make([]endpoint, 0, len(surveyWhitelist))
	skipped := make([]endpoint, 0, 1)
	for _, ep := range surveyWhitelist {
		if ep.hasPlaceholderQuery() {
			skipped = append(skipped, ep)
			continue
		}
		targets = append(targets, ep)
	}
	fmt.Fprintf(opt.out, "Survey of %d catalogued paths, one GET each, %s apart\n", len(targets), opt.delay)
	for _, ep := range skipped {
		fmt.Fprintf(opt.out,
			"Skipping %s: its query %q is a documentation placeholder; a survey never sends one (use --endpoint with --query).\n",
			ep.displayPath(), ep.query)
	}
	fmt.Fprintf(opt.out, "Base: %s\n", opt.baseURL)
	fmt.Fprintf(opt.out, "Credentials: %s\n\n", map[bool]string{
		true:  "given (a path behind authentication is read once more with them)",
		false: "none (a protected path is recorded as auth_required)",
	}[cred.complete()])

	// The authenticated transport exists whether or not credentials are given:
	// without them it sends the bare request and answers 401, which is the fact
	// we want to record.
	digestTransport, err := newDigestTransport(opt.baseURL, cred, logger)
	if err != nil {
		return err
	}

	report := surveyReport{
		BaseURL:   opt.baseURL,
		StartedAt: time.Now().UTC(),
		Counts:    map[string]int{},
	}
	for i, ep := range targets {
		entry, firmware := surveyOne(ctx, opt, base, ep, cred, digestTransport)
		report.Counts[entry.Class]++
		report.Entries = append(report.Entries, entry)
		if report.Firmware == "" && firmware != "" {
			report.Firmware = firmware
		}
		fields := ""
		if len(entry.Fields) > 0 {
			fields = fmt.Sprintf("%d fields", len(entry.Fields))
		}
		fmt.Fprintf(opt.out, "  %-13s %-3d %8d B  %-10s %s\n",
			entry.Class, entry.HTTPStatus, entry.Bytes, fields, ep.displayPath())
		if entry.Error != "" {
			fmt.Fprintf(opt.out, "      error: %s\n", entry.Error)
		}
		if i < len(targets)-1 && opt.delay > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(opt.delay):
			}
		}
	}
	report.FinishedAt = time.Now().UTC()

	fmt.Fprintf(opt.out, "\nOutcome: %s\n", summarizeClasses(report.Counts))
	if report.Firmware != "" {
		fmt.Fprintf(opt.out, "Firmware: %s\n", report.Firmware)
	}

	if opt.saveDir != "" {
		path, err := saveSurvey(opt.saveDir, report)
		if err != nil {
			return err
		}
		fmt.Fprintf(opt.out, "Report written to %s\n", path)
	}
	if opt.dbPath != "" {
		if err := recordSurvey(ctx, opt.dbPath, report); err != nil {
			return err
		}
		fmt.Fprintf(opt.out, "Recorded in %s (%d paths)\n", opt.dbPath, len(report.Entries))
	}
	return nil
}

// newDigestTransport builds the transport a protected path is read through. It
// sends the bare request first and authenticates only in response to a 401.
func newDigestTransport(baseURL string, cred credentials, logger *slog.Logger) (*digest.Transport, error) {
	return digest.New(&http.Client{}, baseURL,
		digest.Credentials{Username: cred.user, Password: cred.password}, logger)
}

// surveyOne performs the survey of one path.
//
// The bare request comes first, always: whether a path is reachable WITHOUT
// credentials is itself a fact worth recording. If it answers 401 and
// credentials are available, the path is read once more through the Digest
// transport to obtain the shape, and digest_required is set.
func surveyOne(ctx context.Context, opt options, base *url.URL, ep endpoint,
	cred credentials, digestTransport *digest.Transport) (surveyEntry, string) {

	entry := surveyEntry{Path: ep.path, Query: ep.query}
	firmware := ""

	// Bare attempt: the read-only guard is the only layer in front of the
	// transport.
	status, body, err := surveyGet(ctx, opt, base, ep, fronius.ReadOnly{Inner: &http.Client{}})
	entry.HTTPStatus, entry.Bytes = status, len(body)
	if err != nil {
		entry.Class, entry.Error = classError, err.Error()
		return entry, ""
	}
	if ep.path == versionPath {
		firmware = firmwareFrom(body)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		entry.DigestRequired = true
		if !cred.complete() {
			entry.Class = classAuthRequired
			return entry, firmware
		}
		// Read it once more, authenticated, to obtain the shape.
		status, body, err = surveyGet(ctx, opt, base, ep, doerFor(ep, digestTransport))
		entry.HTTPStatus, entry.Bytes = status, len(body)
		if err != nil {
			entry.Class, entry.Error = classError, err.Error()
			return entry, firmware
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			entry.Class = classAuthRequired
			return entry, firmware
		}
		if ep.path == versionPath {
			firmware = firmwareFrom(body)
		}
	}

	entry.Class, entry.Fields = classify(status, body)
	return entry, firmware
}

// firmwareFrom reads the device software version out of the identity answer.
//
// Two shapes are accepted, both observed: the internal payload carries
// `swrevisions` at the top level, an envelope would carry it under Body.Data.
// A flat key list remains a fallback candidate, as in the application's own
// extractor. Nothing is invented: an answer without any of them leaves the
// survey's firmware empty.
func firmwareFrom(body []byte) string {
	var top map[string]any
	if err := json.Unmarshal(body, &top); err != nil {
		return ""
	}
	candidates := []any{top}
	if inner, ok := top["Body"].(map[string]any); ok {
		if data, ok := inner["Data"]; ok {
			candidates = append(candidates, data)
		}
	}
	for _, c := range candidates {
		obj, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if rev, ok := obj["swrevisions"].(map[string]any); ok {
			if v, ok := rev["GEN24"].(string); ok && v != "" {
				return v
			}
		}
		for _, key := range []string{"firmwareVersion", "firmwareversion", "firmware", "version"} {
			if v, ok := obj[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return ""
}

// readBounded reads a response body, bounded like the single-probe path.
func readBounded(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the body: %w", err)
	}
	if len(body) > maxBodyBytes {
		body = body[:maxBodyBytes]
	}
	return body, nil
}

// surveyGet performs one GET and returns the status with the body, bounded.
func surveyGet(ctx context.Context, opt options, base *url.URL, ep endpoint, doer fronius.Doer) (int, []byte, error) {
	// Last-resort guard, as in probe(): the placeholder filter runs before this,
	// but a placeholder must never reach the network by any route.
	if ep.hasPlaceholderQuery() {
		return 0, nil, fmt.Errorf("refusal: %s declares the documentation placeholder %q",
			ep.displayPath(), ep.query)
	}
	endpointURL := base.JoinPath(ep.path)
	if ep.query != "" {
		endpointURL.RawQuery = ep.query
	}
	req, err := http.NewRequestWithContext(ctx, ep.method, endpointURL.String(), nil)
	if err != nil {
		return 0, nil, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := doer.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := readBounded(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// classify turns one answer into a class and, when there is something to
// describe, its inventory.
func classify(status int, body []byte) (string, []surveyField) {
	switch {
	case status == http.StatusNotFound:
		return classNotFound, nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return classAuthRequired, nil
	case status < 200 || status >= 300:
		return classError, nil
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return classNotJSON, nil
	}
	if carriesNothing(decoded) {
		return classEmpty, nil
	}

	fields := make([]surveyField, 0, 32)
	walkJSON("", decoded, &fields, 0)
	return classOK, fields
}

// carriesNothing tells an endpoint that answered an empty body from one that
// answered measurements. `{"Body":{"Data":{}}}` is the manufacturer's way of
// saying "this component is not installed": it is a fact, and a different one
// from "no answer".
func carriesNothing(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case map[string]any:
		if len(t) == 0 {
			return true
		}
		if body, ok := t["Body"].(map[string]any); ok {
			if data, ok := body["Data"]; ok {
				return carriesNothing(data)
			}
		}
		return false
	case []any:
		return len(t) == 0
	}
	return false
}

// walkJSON fills the inventory with the paths and types of a decoded answer.
// Arrays are described by their length and walked through their first element,
// whose paths are suffixed with [].
func walkJSON(prefix string, v any, out *[]surveyField, depth int) {
	if len(*out) >= maxSurveyFields {
		markTruncated(out)
		return
	}
	if depth > maxSurveyDepth {
		*out = append(*out, surveyField{Path: prefix, Type: "deeper"})
		return
	}

	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic report, whatever the encoder did
		for _, k := range keys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			walkJSON(path, t[k], out, depth+1)
		}
	case []any:
		*out = append(*out, surveyField{Path: prefix, Type: "array", ArrayLen: len(t)})
		if len(t) > 0 {
			walkJSON(prefix+"[]", t[0], out, depth+1)
		}
	case bool:
		*out = append(*out, surveyField{Path: prefix, Type: "bool"})
	case float64:
		if t == float64(int64(t)) {
			*out = append(*out, surveyField{Path: prefix, Type: "int"})
		} else {
			*out = append(*out, surveyField{Path: prefix, Type: "float"})
		}
	case string:
		*out = append(*out, surveyField{Path: prefix, Type: "string"})
	case nil:
		*out = append(*out, surveyField{Path: prefix, Type: "null"})
	default:
		*out = append(*out, surveyField{Path: prefix, Type: "unknown"})
	}
}

// markTruncated records, once, that an answer carried more than the survey
// keeps. A silently cut inventory would read as a complete one.
func markTruncated(out *[]surveyField) {
	if n := len(*out); n > 0 && (*out)[n-1].Path == "(truncated)" {
		return
	}
	*out = append(*out, surveyField{Path: "(truncated)", Type: fmt.Sprintf("more than %d values", maxSurveyFields)})
}

// summarizeClasses renders the outcome counts for the operator.
func summarizeClasses(counts map[string]int) string {
	order := []string{classOK, classEmpty, classNotJSON, classAuthRequired, classNotFound, classError}
	parts := make([]string, 0, len(order))
	for _, class := range order {
		if n := counts[class]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, class))
		}
	}
	if len(parts) == 0 {
		return "nothing probed"
	}
	return strings.Join(parts, ", ")
}

// saveSurvey writes the report, redacted by construction: it holds paths and
// types, never a value.
func saveSurvey(dir string, report surveyReport) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	name := fmt.Sprintf("survey_%s.json", report.StartedAt.Format("20060102T150405Z"))
	path := filepath.Join(dir, name)

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serialising the survey: %w", err)
	}
	body = append(body, '\n')
	// 0600: the survey carries paths, and a path can name a device.
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// recordSurvey writes one row per path into the application's database, which is
// where the interface reads it.
func recordSurvey(ctx context.Context, dbPath string, report surveyReport) error {
	loc, err := surveyLocation()
	if err != nil {
		return err
	}
	s, err := storage.Open(dbPath, loc)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	probes := make([]storage.SourceProbe, 0, len(report.Entries))
	at := report.FinishedAt
	for _, e := range report.Entries {
		fields := make([]storage.ProbeField, 0, len(e.Fields))
		for _, f := range e.Fields {
			fields = append(fields, storage.ProbeField{Path: f.Path, Type: f.Type, ArrayLen: f.ArrayLen})
		}
		probes = append(probes, storage.SourceProbe{
			Path:           e.Path,
			Query:          e.Query,
			Class:          storage.ProbeClass(e.Class),
			HTTPStatus:     e.HTTPStatus,
			DigestRequired: e.DigestRequired,
			Bytes:          e.Bytes,
			Error:          e.Error,
			Firmware:       report.Firmware,
			ProbedAt:       at,
			Fields:         fields,
		})
	}
	return s.SaveSourceProbes(ctx, probes)
}
