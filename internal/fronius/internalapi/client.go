// Client for the internal Fronius Webinterface paths, read-only.
//
// The only request builder is Get: the verb is the constant http.MethodGet, no
// method parameter exists in this API. Every path first goes through the
// allowlist (allowlist.go), which contains exactly five entries; an absent path
// is refused before any URL construction.
package internalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

// maxBodyBytes bounds the read: these responses are a few kilobytes.
const maxBodyBytes = 1 << 20 // 1 MiB

// Client queries the allowed internal paths.
type Client struct {
	base    *url.URL
	doer    fronius.Doer
	timeout time.Duration
	log     *slog.Logger
}

// New builds a client.
//
// doer carries the caller's security guarantees: in practice
// fronius.ReadOnly{Inner: digest.Transport{…}}, hence GET only, and
// authentication only in response to a 401.
func New(baseURL string, doer fronius.Doer, timeout time.Duration, log *slog.Logger) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("base URL %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL %q: http(s) scheme expected", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("base URL %q: missing host", baseURL)
	}
	if doer == nil {
		doer = &http.Client{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{base: u, doer: doer, timeout: timeout, log: log}, nil
}

// Get reads a path from the allowlist and returns the raw body.
//
// No decoding is done here: the envelope depends on the endpoint
// (EnvelopeDefault or EnvelopeCgi) and is handled by the typed methods.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	ep, ok := Lookup(path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	return c.get(ctx, ep)
}

func (c *Client) get(ctx context.Context, ep Endpoint) ([]byte, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	endpointURL := c.base.JoinPath(ep.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: ep.Path, Err: err}
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, ep.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, ep.Path, err)
	}

	c.log.Debug("internal api",
		"internal_path", ep.Path,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"bytes", len(body),
		"duration_ms", time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return nil, fronius.ClassifyStatus(ep.Path, resp.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: ep.Path,
			Err:      fmt.Errorf("body of %d bytes: exceeds the limit of %d MiB", len(body), maxBodyBytes>>20),
		}
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// Identity
// ---------------------------------------------------------------------------

// Version is the device identity, as extracted from /status/version.
//
// Each field is a pointer: an absent or empty key stays nil, never "".
// A nil pointer means "not found in the response", not "absent on the device".
type Version struct {
	SerialNumber *string
	// DeviceName and ArticleNumber are the two identity fields the capture of
	// 2026-10-16 found in /status/version besides the serial number. DeviceName is
	// the human name of the unit ("Primo GEN24 3.6 Plus"), NOT its model: no model
	// key exists in that payload, so Model stays nil rather than carrying a name.
	DeviceName     *string
	ArticleNumber  *string
	Model          *string
	Firmware       *string
	DeviceGroup    *string
	NumberOfPhases *int
}

// Candidate keys, in priority order. The first non-empty key wins.
//
// Established by the bundle (docs/SOURCES.md): `serialNumber`, `numberOfPhases`,
// `devicegroup`, and the software version under `swrevisions.GEN24` (l. 128882
// and 135137: it is the key the official client reads, and it displays it as the
// device's current version). The others are assumed candidates, to be confirmed
// by a hardware capture (cmd/fronius-probe): adding an observed key at the head
// of the corresponding list is then enough to take it into account.
//
// `devicegroup` is NOT a model fallback: it is the device group (`GEN24PRIMO`,
// cf. l. 183035 which infers the number of phases from it), a notion distinct
// from the model, attested as a sibling field in the payload. The proven version
// path is therefore read first, and the model stays nil as long as no model key
// is observed: an unknown value is never replaced by the value of another field.
var (
	serialNumberKeys = []string{"serialNumber", "serial", "serials"}
	deviceNameKeys   = []string{"devicename", "deviceName", "name"}
	articleKeys      = []string{"articleNumber", "article"}
	modelKeys        = []string{"modelName", "model", "deviceModel", "inverterModel"}
	firmwareKeys     = []string{"firmwareVersion", "firmwareversion", "firmware", "swVersion", "version"}
	deviceGroupKeys  = []string{"devicegroup", "deviceGroup"}
	phasesKeys       = []string{"numberOfPhases", "phases"}
)

// Version reads /status/version and extracts the known identity from it.
func (c *Client) Version(ctx context.Context) (Version, error) {
	ep, ok := Lookup(VersionPath)
	if !ok {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAllowed, VersionPath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return Version{}, err
	}

	fields, err := decodeObject(ep.Path, body)
	if err != nil {
		return Version{}, err
	}

	return Version{
		SerialNumber:   stringField(fields, serialNumberKeys...),
		DeviceName:     stringField(fields, deviceNameKeys...),
		ArticleNumber:  stringField(fields, articleKeys...),
		Model:          stringField(fields, modelKeys...),
		Firmware:       firmwareOf(fields),
		DeviceGroup:    stringField(fields, deviceGroupKeys...),
		NumberOfPhases: intField(fields, phasesKeys...),
	}, nil
}

// firmwareOf extracts the device software version.
//
// The proven path comes first: the official client reads `swrevisions.GEN24`
// (l. 128882, 135137). The flat keys remain fallback candidates, so as not to
// depend on a single shape.
func firmwareOf(fields map[string]json.RawMessage) *string {
	if v := nestedStringField(fields, "swrevisions", "GEN24"); v != nil {
		return v
	}
	return stringField(fields, firmwareKeys...)
}

// ---------------------------------------------------------------------------
// Component presence
// ---------------------------------------------------------------------------

// ComponentPresent reports whether a component responds.
//
// `Cgi` envelope rule: non-empty Body.Data ⇒ present, empty or absent Body.Data
// ⇒ absent. An unreadable body is an ERROR (KindDecode), never an absence: a
// response we cannot read proves nothing.
func (c *Client) ComponentPresent(ctx context.Context, path string) (bool, error) {
	ep, ok := Lookup(path)
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	if ep.Envelope != EnvelopeCgi {
		return false, fmt.Errorf("internalapi: %q is not a component (envelope %q)", path, ep.Envelope)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return false, err
	}

	data, err := decodeData(ep.Path, body)
	if err != nil {
		return false, err
	}
	return len(data) > 0, nil
}

// ---------------------------------------------------------------------------
// Active events
// ---------------------------------------------------------------------------

// ActiveEvents reads /status/activeEvents.
//
// The body shape is not established (docs/UNKNOWNS.md): the events are rendered
// as raw JSON, in a stable order (sorted keys for an indexed object, array order
// for a root list). The caller decides what to make of them, and the raw detail
// is kept.
func (c *Client) ActiveEvents(ctx context.Context) ([]json.RawMessage, error) {
	ep, ok := Lookup(ActiveEventsPath)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, ActiveEventsPath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return nil, err
	}

	// Object indexed by event id (expected shape). An empty or nil Data means
	// "no event", this is not an error.
	if data, err := decodeData(ep.Path, body); err == nil {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]json.RawMessage, 0, len(keys))
		for _, k := range keys {
			out = append(out, data[k])
		}
		return out, nil
	}

	// Fallback: root array.
	var list []json.RawMessage
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}

	return nil, &froniuserr.Error{
		Kind:     froniuserr.KindDecode,
		Endpoint: ep.Path,
		Err:      fmt.Errorf("unexpected shape: neither indexed Body.Data nor root array"),
	}
}

// ---------------------------------------------------------------------------
// Internal flow measurement (fallback for the day and year energies)
// ---------------------------------------------------------------------------

// PowerFlow is the internal flow measurement of /status/powerflow.
//
// Shape provenance: the vendor interface reads exactly these keys to draw its
// flow view (bundle l. 129034, 129043-129063), but this path has NOT been
// captured on hardware yet, so every field is a pointer and the DTO stays
// deliberately small: only what the application consumes is modelled, and an
// unknown key is ignored. Field names follow the protocol (`P_PV`, `E_Day`) so
// that a tag divergence stands out on rereading.
type PowerFlow struct {
	Site      PowerFlowSite                `json:"site"`
	Inverters map[string]PowerFlowInverter `json:"inverters"`
}

// PowerFlowSite is the site measurement point of the internal flow view.
type PowerFlowSite struct {
	P_PV    *float64 `json:"P_PV"`
	P_Grid  *float64 `json:"P_Grid"`
	P_Load  *float64 `json:"P_Load"`
	P_Akku  *float64 `json:"P_Akku"`
	E_Day   *float64 `json:"E_Day"`
	E_Year  *float64 `json:"E_Year"`
	E_Total *float64 `json:"E_Total"`
}

// PowerFlowInverter is one inverter of the internal flow view.
type PowerFlowInverter struct {
	P   *float64 `json:"P"`
	SOC *float64 `json:"SOC"`
}

// PowerFlow reads /status/powerflow (Default envelope: the body is the payload).
//
// A body that carries none of the expected keys is a decode error, not an empty
// measurement: a shape we cannot read proves nothing, as for the components.
func (c *Client) PowerFlow(ctx context.Context) (PowerFlow, error) {
	ep, ok := Lookup(PowerFlowPath)
	if !ok {
		return PowerFlow{}, fmt.Errorf("%w: %q", ErrNotAllowed, PowerFlowPath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return PowerFlow{}, err
	}

	var pf PowerFlow
	if err := json.Unmarshal(body, &pf); err != nil {
		return PowerFlow{}, jsonDecodeError(ep.Path, err)
	}
	if pf.Site.P_PV == nil && pf.Site.E_Day == nil && pf.Site.E_Year == nil &&
		pf.Site.E_Total == nil && len(pf.Inverters) == 0 {
		return PowerFlow{}, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: ep.Path,
			Err:      fmt.Errorf("no expected key found: neither site.P_* nor inverters"),
		}
	}
	return pf, nil
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

// decodeObject decodes a body into a JSON object.
//
// Unknown keys are kept: it is the extraction by candidate key list that
// decides, never a single assumed name.
func decodeObject(endpoint string, body []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, jsonDecodeError(endpoint, err)
	}
	if fields == nil {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: endpoint,
			Err:      fmt.Errorf("JSON object expected, got null"),
		}
	}
	return fields, nil
}

// decodeData returns Body.Data of a `Cgi` envelope.
func decodeData(endpoint string, body []byte) (map[string]json.RawMessage, error) {
	fields, err := decodeObject(endpoint, body)
	if err != nil {
		return nil, err
	}
	rawBody, ok := fields["Body"]
	if !ok {
		// An envelope without Body is unreadable for us: it is not an absence of
		// component.
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: endpoint,
			Err:      fmt.Errorf("Body field absent from the response"),
		}
	}
	var wrapper struct {
		Data map[string]json.RawMessage `json:"Data"`
	}
	if err := json.Unmarshal(rawBody, &wrapper); err != nil {
		return nil, jsonDecodeError(endpoint, err)
	}
	return wrapper.Data, nil
}

func jsonDecodeError(endpoint string, err error) error {
	return &froniuserr.Error{Kind: froniuserr.KindDecode, Endpoint: endpoint, Err: err}
}

// stringField returns the first candidate key whose value is a non-empty string,
// case-insensitive on keys.
func stringField(fields map[string]json.RawMessage, keys ...string) *string {
	for _, key := range keys {
		raw, ok := lookupFold(fields, key)
		if !ok {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		return new(v)
	}
	return nil
}

// nestedStringField returns the first non-empty candidate key of a nested
// object, for example `swrevisions.GEN24`.
//
// An absent object, of another type, or without a matching key returns nil: the
// nested shape is never assumed, only searched for.
func nestedStringField(fields map[string]json.RawMessage, object string, keys ...string) *string {
	raw, ok := lookupFold(fields, object)
	if !ok {
		return nil
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil || inner == nil {
		return nil
	}
	return stringField(inner, keys...)
}

// intField returns the first candidate key whose value is an integer.
//
// A number serialized as a string ("3") is accepted: the real shape is not
// established for numberOfPhases on all firmwares.
func intField(fields map[string]json.RawMessage, keys ...string) *int {
	for _, key := range keys {
		raw, ok := lookupFold(fields, key)
		if !ok {
			continue
		}
		var n int
		if err := json.Unmarshal(raw, &n); err == nil {
			return new(n)
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if parsed, err := parseLenientInt(s); err == nil {
				return new(parsed)
			}
		}
	}
	return nil
}

func parseLenientInt(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

func lookupFold(fields map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if raw, ok := fields[key]; ok {
		return raw, true
	}
	for k, raw := range fields {
		if strings.EqualFold(k, key) {
			return raw, true
		}
	}
	return nil, false
}
