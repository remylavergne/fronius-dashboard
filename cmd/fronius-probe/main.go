// Command fronius-probe performs a READ-ONLY diagnostic request
// to a Fronius inverter, and writes the redacted body on demand.
//
// This binary is intended to be run by the user, on their own
// hardware, after reading docs/PROBE-READONLY.md. It is never launched by
// the agent or by CI.
//
// Guarantees:
//   - allowlist of six endpoints, GET only, selectable via
//     --endpoint; any other path is refused before building the URL;
//   - refusal of a non-local address without an explicit --allow-non-loopback;
//   - no write to the device: no non-GET method is built,
//     and the fronius.ReadOnly guard rejects any request that is not a GET;
//   - redaction of the body and headers before any write to disk;
//   - no password on the command line: the credentials come from the
//     environment (FRONIUS_DEVICE_USER / FRONIUS_DEVICE_PASSWORD) or, for
//     the password, from standard input (--password-stdin) — an argument
//     would end up in the shell history and in the process table.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/digest"
	"fronius-supervisor/internal/secret"
	"fronius-supervisor/internal/sources"
)

// errNotWhitelisted: the probe cannot target an arbitrary URL.
var errNotWhitelisted = errors.New("endpoint not in allowlist")

// maxBodyBytes bounds the read: a real Solar API response is a few
// kilobytes.
const maxBodyBytes = 1 << 20

// endpoint describes an allowlist entry.
type endpoint struct {
	method string
	path   string
	// query is the frozen query string of the entry, empty when the path takes
	// none. It is a constant of the binary, like the path: the probe still cannot
	// target an arbitrary URL, it can only reach the entries below.
	query          string
	classification string
	note           string
	// digest: the path is reached by the official client with an authenticated
	// GET (it does not imply that the device requires it: it may respond
	// without a header).
	digest bool
}

// whitelist is the set the probe reads by default: the sources this project
// actually uses (called) plus the ones already captured on the reference
// hardware. It is DERIVED from internal/sources, the register of every known
// source, so a path cannot be probed without being catalogued — and the
// catalogue test forbids a catalogued path from being absent from the registers.
//
// The full catalogue is reached with --survey (see survey.go); --endpoint
// accepts any catalogued path. Every addition to that register is a documented,
// tested and reviewed decision (docs/PROBE-READONLY.md, docs/ENDPOINTS.md).
var whitelist = buildWhitelist(func(s sources.Source) bool {
	return s.State == sources.StateCalled || s.State == sources.StateCaptured
})

// surveyWhitelist is the whole catalogue: what a survey walks.
var surveyWhitelist = buildWhitelist(func(sources.Source) bool { return true })

// buildWhitelist turns the catalogue into probe entries.
//
// A source reached by the manufacturer's own interface is marked digest: the
// transport still sends the bare request first and authenticates only in
// response to a 401 (internal/fronius/digest), so this flag describes what the
// official client does, not what the device demands.
func buildWhitelist(keep func(sources.Source) bool) []endpoint {
	out := make([]endpoint, 0, len(sources.Catalog()))
	for _, src := range sources.Catalog() {
		if !keep(src) {
			continue
		}
		out = append(out, endpoint{
			method:         http.MethodGet,
			path:           src.Path,
			query:          src.Query,
			classification: "READ",
			digest:         src.Transport == sources.InternalAPI,
			note:           src.Provides,
		})
	}
	return out
}

// allowAllEndpoints is the value of --endpoint that selects the whole list.
const allowAllEndpoints = "all"

type options struct {
	baseURL       string
	allowNonLocal bool
	saveDir       string
	timeout       time.Duration
	list          bool
	endpoint      string
	passwordStdin bool
	// survey walks the whole catalogued allowlist instead of one path.
	survey bool
	// delay separates two paths of a survey: a survey is 27 requests, and an
	// inverter's web interface is not asked to serve them in a burst.
	delay time.Duration
	// dbPath records the survey where the application reads it. Empty disables
	// the recording (the report file remains the operator's copy).
	dbPath string
	// endpointSet records that --endpoint was given explicitly: the flag has a
	// default value ("all"), which must not be mistaken for a choice.
	endpointSet bool
	out         io.Writer
	// stdin feeds --password-stdin (injectable in tests).
	stdin io.Reader
}

func main() {
	var opt options
	flag.StringVar(&opt.baseURL, "base-url", "http://127.0.0.1:8081", "Base URL of the device (simulator by default)")
	flag.BoolVar(&opt.allowNonLocal, "allow-non-loopback", false, "allows a non-local address (use only after reading docs/PROBE-READONLY.md)")
	flag.StringVar(&opt.saveDir, "save", "", "directory where the redacted body is written")
	flag.DurationVar(&opt.timeout, "timeout", 5*time.Second, "per-request timeout")
	flag.BoolVar(&opt.list, "list", false, "prints the allowlist of endpoints and exits")
	flag.StringVar(&opt.endpoint, "endpoint", allowAllEndpoints, "path to probe ("+allowAllEndpoints+" or a path from the allowlist)")
	flag.BoolVar(&opt.passwordStdin, "password-stdin", false, "reads the password on the first line of standard input")
	flag.BoolVar(&opt.survey, "survey", false, "walks the whole catalogued allowlist, one GET per path, and records what each one answers")
	flag.DurationVar(&opt.delay, "delay", time.Second, "delay between two paths of a survey")
	flag.StringVar(&opt.dbPath, "db", os.Getenv("FRONIUS_DB_PATH"), "database that records the survey (empty = write the report only)")
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "endpoint" {
			opt.endpointSet = true
		}
	})
	opt.out = os.Stdout
	opt.stdin = os.Stdin

	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, "fronius-probe:", err)
		os.Exit(1)
	}
}

func run(opt options) error {
	if opt.list {
		printWhitelist(opt.out, opt.baseURL)
		return nil
	}

	if opt.survey && opt.endpointSet {
		return errors.New("--survey walks the whole catalogue: do not combine it with --endpoint")
	}
	selected := []endpoint(nil)
	if !opt.survey {
		var err error
		selected, err = selectEndpoints(opt.endpoint)
		if err != nil {
			return err
		}
	}
	if opt.stdin == nil {
		opt.stdin = os.Stdin
	}
	cred, err := readCredentials(opt)
	if err != nil {
		return err
	}

	base, err := url.Parse(opt.baseURL)
	if err != nil {
		return fmt.Errorf("URL %q: %w", opt.baseURL, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return fmt.Errorf("URL %q: expected http(s) scheme", opt.baseURL)
	}
	if base.Host == "" {
		return fmt.Errorf("URL %q: missing host", opt.baseURL)
	}
	if !config.IsLoopbackHost(base.Hostname()) && !opt.allowNonLocal {
		return fmt.Errorf(
			"refusal: %s is not a local address; re-run with --allow-non-loopback "+
				"after reading docs/PROBE-READONLY.md and obtaining the user's explicit consent",
			opt.baseURL)
	}
	if opt.timeout <= 0 {
		return fmt.Errorf("--timeout must be strictly positive")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if opt.survey {
		if opt.delay < 0 {
			return errors.New("--delay cannot be negative")
		}
		return runSurvey(context.Background(), opt, base, cred, logger)
	}

	// The read-only guard is interposed in front of the transport: a non-GET
	// request cannot reach the network, even by programming mistake.
	// The Digest transport is composed beneath it, and only for the paths
	// that request it; without credentials it sends the bare request.
	digestTransport, err := digest.New(&http.Client{}, opt.baseURL,
		digest.Credentials{Username: cred.user, Password: cred.password}, logger)
	if err != nil {
		return err
	}

	for _, ep := range selected {
		ctx, cancel := context.WithTimeout(context.Background(), opt.timeout)
		err := probe(ctx, doerFor(ep, digestTransport), base, ep, opt, logger)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// doerFor returns the transport of an endpoint, the read-only guard remaining
// outside.
func doerFor(ep endpoint, digestTransport *digest.Transport) fronius.Doer {
	if ep.digest {
		return fronius.ReadOnly{Inner: digestTransport}
	}
	return fronius.ReadOnly{Inner: &http.Client{}}
}

// selectEndpoints resolves --endpoint.
//
// Any path absent from the allowlist is refused: the probe cannot
// target an arbitrary URL, even if the user knows it.
func selectEndpoints(name string) ([]endpoint, error) {
	trimmed := strings.TrimSpace(name)
	switch trimmed {
	case "", allowAllEndpoints:
		return slices.Clone(whitelist), nil
	}
	for _, ep := range surveyWhitelist {
		if ep.path == trimmed {
			return []endpoint{ep}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", errNotWhitelisted, trimmed)
}

// credentials gathers the credentials, without ever logging them.
type credentials struct {
	user     string
	password secret.String
}

// complete reports whether a full pair is available: a user without a password
// cannot answer a Digest challenge, so it counts as no credentials at all.
func (c credentials) complete() bool {
	return c.user != "" && !c.password.IsZero()
}

// readCredentials reads the environment, and the password on standard input
// if --password-stdin is active.
//
// No --password flag exists: a password passed as an argument would appear
// in the shell history and in the process table.
func readCredentials(opt options) (credentials, error) {
	out := credentials{user: strings.TrimSpace(os.Getenv("FRONIUS_DEVICE_USER"))}
	if v := strings.TrimSpace(os.Getenv("FRONIUS_DEVICE_PASSWORD")); v != "" {
		out.password = secret.New(v)
	}
	if opt.passwordStdin {
		line, err := bufio.NewReader(opt.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return credentials{}, fmt.Errorf("reading the password from standard input: %w", err)
		}
		out.password = secret.New(strings.TrimRight(line, "\r\n"))
	}
	if out.user == "" && !out.password.IsZero() {
		return credentials{}, errors.New("FRONIUS_DEVICE_USER is empty: both credentials go together")
	}
	if out.user != "" && out.password.IsZero() {
		return credentials{}, errors.New("FRONIUS_DEVICE_PASSWORD is empty: both credentials go together")
	}
	return out, nil
}

func probe(ctx context.Context, doer fronius.Doer, base *url.URL, ep endpoint, opt options, logger *slog.Logger) error {
	endpointURL := base.JoinPath(ep.path)
	if ep.query != "" {
		endpointURL.RawQuery = ep.query
	}

	req, err := http.NewRequestWithContext(ctx, ep.method, endpointURL.String(), nil)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	fmt.Fprintf(opt.out, "Request\n  %s %s\n", ep.method, endpointURL)
	fmt.Fprintf(opt.out, "  classification: %s\n  allowlist: %s\n\n", ep.classification, ep.note)

	start := time.Now()
	resp, err := doer.Do(req)
	duration := time.Since(start)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpointURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("reading the body: %w", err)
	}
	truncated := len(body) > maxBodyBytes
	if truncated {
		body = body[:maxBodyBytes]
	}

	fmt.Fprintf(opt.out, "Response\n  status   : %s\n  duration : %s\n  bytes    : %d%s\n",
		resp.Status, duration.Round(time.Millisecond), len(body), map[bool]string{true: " (truncated)"}[truncated])

	fmt.Fprintf(opt.out, "  headers (sensitive values masked)\n")
	for _, name := range sortedHeaderNames(resp.Header) {
		fmt.Fprintf(opt.out, "    %s: %s\n", name, maskHeaderValue(name, strings.Join(resp.Header.Values(name), ", ")))
	}

	redacted := redactBody(body)
	fmt.Fprintf(opt.out, "\nRedacted body\n%s\n", redacted)
	if note := sameBodyNote(body, redacted); note != "" {
		fmt.Fprintf(opt.out, "\n%s\n", note)
	}

	if opt.saveDir != "" {
		path, err := saveRedacted(opt.saveDir, ep, redacted)
		if err != nil {
			return err
		}
		fmt.Fprintf(opt.out, "\nRedacted body written to %s\n", path)
	}

	logger.Debug("probe executed", "url", endpointURL.String(), "status", resp.StatusCode, "bytes", len(body))
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("the device responded %s", resp.Status)
	}
	return nil
}

func printWhitelist(w io.Writer, baseURL string) {
	fmt.Fprintf(w, "fronius-probe allowlist (read-only mode, GET only)\n")
	fmt.Fprintf(w, "Base: %s\n\n", baseURL)
	for _, ep := range whitelist {
		fmt.Fprintf(w, "  %-4s %-60s %-5s digest=%v\n", ep.method, ep.displayPath(), ep.classification, ep.digest)
		fmt.Fprintf(w, "       %s\n", ep.note)
	}
	fmt.Fprintf(w, "\nNo other request is possible: the list is frozen in the binary.\n")
	fmt.Fprintf(w, "No POST, PUT, PATCH or DELETE method is built or allowed.\n")
	fmt.Fprintf(w, "\nCredentials: FRONIUS_DEVICE_USER and FRONIUS_DEVICE_PASSWORD (environment),\n")
	fmt.Fprintf(w, "or --password-stdin for the password. The Authorization header is sent\n")
	fmt.Fprintf(w, "only in response to a 401, never preemptively.\n")
}

func sortedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func saveRedacted(dir string, ep endpoint, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	name := fmt.Sprintf("probe_%s_%s.json", saveName(ep.path),
		time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// saveName derives a file name from the PATH of the endpoint, and not from its
// single last segment: "/components/PowerMeter/readable" and
// "/components/BatteryManagementSystem/readable" both end with
// "readable" and would overwrite each other.
//
// The shape obtained (status_version, components_powermeter_readable) is that
// of the promoted fixtures described in docs/PROBE-READONLY.md.
// displayPath is the path as the probe will request it, query included.
func (e endpoint) displayPath() string {
	if e.query == "" {
		return e.path
	}
	return e.path + "?" + e.query
}

func saveName(path string) string {
	withoutExt := strings.TrimSuffix(path, filepath.Ext(path))
	trimmed := strings.Trim(withoutExt, "/")
	if trimmed == "" {
		return "root"
	}
	replacer := strings.NewReplacer("/", "_", "-", "_", " ", "_")
	return strings.ToLower(replacer.Replace(trimmed))
}

// sameBodyNote signals the removed bytes, without ever displaying them.
func sameBodyNote(original, redacted []byte) string {
	if len(original) == len(redacted) {
		return ""
	}
	return "Note: some values were replaced by <redacted> before display and write."
}
