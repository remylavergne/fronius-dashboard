// Package detect periodically polls the device's internal paths
// to establish its identity, the presence of a meter and of a battery, and
// its active events.
//
// Invariants:
//   - three states, never two: `absent` is a conclusion, `unknown`
//     is the absence of a conclusion. A read error NEVER produces an
//     absence (see docs/UNKNOWNS.md);
//   - the only remote operation is a GET, via internalapi.Client;
//   - no credential is written or traced: the detector holds only a
//     boolean "credentials configured";
//   - no useless traffic: a 401 received without configured credentials makes
//     the path concerned be abandoned until restart.
package detect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// Persisted state keys.
const (
	// StateLastReadUTC: UTC timestamp of the last successful detection.
	StateLastReadUTC = "detect_last_read_utc"
	// StateLastError: first error message of the last cycle, "" otherwise.
	StateLastError = "detect_last_error"
)

// Unknown-state reasons, reused as-is by the API and the interface.
const (
	ReasonNoCredentials  = "credentials not configured (FRONIUS_DEVICE_USER / FRONIUS_DEVICE_PASSWORD)"
	ReasonUnauthorized   = "access denied (401): credentials absent or invalid"
	ReasonEndpointAbsent = "endpoint absent on this firmware (404)"
	// ReasonRefusedAfterChallenge: the device issued a challenge and then refused
	// the authenticated request (a 404 or a 403 after a 401). The path therefore
	// exists — a bare 404 challenges nobody — so the cause is the credentials or
	// the role they carry, never an absent endpoint.
	ReasonRefusedAfterChallenge = "challenged, then refused: check the credentials and the role they carry"
	// ReasonNoReadYet: no identity read has succeeded yet.
	ReasonNoReadYet = "no identity read has succeeded yet"
	// deviceEventMessage is the fallback message of an event with no text.
	deviceEventMessage = "Device event"
	// maxReasonLength bounds the cause taken from an error in a state.
	maxReasonLength = 200
	// maxDetailBytes bounds the raw JSON kept for an event.
	maxDetailBytes = 2048
)

// Reader is the detection source. *internalapi.Client satisfies it.
type Reader interface {
	Version(ctx context.Context) (internalapi.Version, error)
	ComponentPresent(ctx context.Context, path string) (bool, error)
	ActiveEvents(ctx context.Context) ([]json.RawMessage, error)
}

// Sink persists the effects of a detection. *storage.Store satisfies it.
type Sink interface {
	SaveDevice(ctx context.Context, d storage.DeviceRecord) error
	MarkDeviceEventSeen(ctx context.Context, key string, seenAt time.Time) (bool, error)
	InsertEvent(ctx context.Context, e model.Event) error
	SetState(ctx context.Context, k, v string) error
}

// Detector polls the internal paths and persists the result.
type Detector struct {
	Source Reader
	Sink   Sink
	Log    *slog.Logger
	// Interval is the period between two detections. The official client paces
	// these paths at 5,000 ms; the value retained is much longer, because an
	// identity and a presence rarely change.
	Interval time.Duration
	// CredentialsConfigured indicates that credentials are available. It
	// is only used to distinguish "the device requires authentication"
	// from "no credential has been provided".
	CredentialsConfigured bool
	// Now allows injecting a clock (tests).
	Now func() time.Time

	mu sync.Mutex
	// abandoned remembers the paths on which a 401 was received while
	// no credential is configured: they are no longer polled until
	// restart, and their state remains `unknown`.
	abandoned map[string]bool
}

// Run detects until the context is cancelled, then returns nil.
//
// An error in a cycle never stops the loop: a failed read is a
// state, not a service failure.
func (d *Detector) Run(ctx context.Context) error {
	d.log().Info("detector started",
		"interval", d.Interval,
		"credentials", d.CredentialsConfigured)

	for {
		if _, err := d.DetectOnce(ctx); err != nil {
			d.log().Warn("detection failed", "error", err)
		}
		if ctx.Err() != nil {
			return nil
		}

		timer := time.NewTimer(d.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// DetectOnce runs a complete detection cycle.
//
// The returned error concerns persistence only: reads that fail
// are translated into `unknown` states, never into a cycle error.
func (d *Detector) DetectOnce(ctx context.Context) (model.Detection, error) {
	now := d.now()
	var detection model.Detection

	identity, version := d.readIdentity(ctx)
	detection.Identity = identity

	detection.Meter = d.readComponent(ctx, internalapi.PowerMeterPath)
	detection.Battery = d.readComponent(ctx, internalapi.BatteryPath)

	eventsDetection, eventsErr := d.readEvents(ctx, now)
	detection.DeviceEvents = eventsDetection

	record := storage.DeviceRecord{
		ReadAtUTC:          now,
		Source:             internalapi.VersionPath,
		IdentityState:      detection.Identity.State,
		IdentityReason:     detection.Identity.Reason,
		MeterState:         detection.Meter.State,
		MeterSource:        internalapi.PowerMeterPath,
		MeterReason:        detection.Meter.Reason,
		BatteryState:       detection.Battery.State,
		BatterySource:      internalapi.BatteryPath,
		BatteryReason:      detection.Battery.Reason,
		DeviceEventsState:  detection.DeviceEvents.State,
		DeviceEventsReason: detection.DeviceEvents.Reason,
	}
	if version != nil {
		record.Model = deref(version.Model)
		record.Firmware = deref(version.Firmware)
		record.SerialNumber = deref(version.SerialNumber)
		record.DeviceName = deref(version.DeviceName)
		record.ArticleNumber = deref(version.ArticleNumber)
	}

	var errs []error
	if eventsErr != nil {
		errs = append(errs, eventsErr)
	}
	if err := d.Sink.SaveDevice(ctx, record); err != nil {
		errs = append(errs, err)
	}
	if err := d.Sink.SetState(ctx, StateLastReadUTC, now.UTC().Format(time.RFC3339)); err != nil {
		errs = append(errs, err)
	}

	lastError := firstCause(detection)
	if err := d.Sink.SetState(ctx, StateLastError, lastError); err != nil {
		errs = append(errs, err)
	}

	return detection, errors.Join(errs...)
}

// readIdentity reads /status/version.
func (d *Detector) readIdentity(ctx context.Context) (model.ResourceDetection, *internalapi.Version) {
	const path = internalapi.VersionPath

	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}, nil
	}

	version, err := d.Source.Version(ctx)
	if err != nil {
		return d.failedDetection(ctx, path, err), nil
	}

	return model.ResourceDetection{State: model.StatePresent, Source: path}, &version
}

// readComponent reads the presence of a component.
func (d *Detector) readComponent(ctx context.Context, path string) model.ResourceDetection {
	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}
	}

	present, err := d.Source.ComponentPresent(ctx, path)
	if err != nil {
		return d.failedDetection(ctx, path, err)
	}
	if present {
		return model.ResourceDetection{State: model.StatePresent, Source: path}
	}
	// The device has answered and does not report this component: this is an
	// established absence, not a doubt.
	return model.ResourceDetection{
		State:  model.StateAbsent,
		Source: path,
		Reason: "Body.Data empty: no indexed component",
	}
}

// readEvents reads /status/activeEvents, deduplicates and inserts new events.
func (d *Detector) readEvents(ctx context.Context, now time.Time) (model.ResourceDetection, error) {
	const path = internalapi.ActiveEventsPath

	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}, nil
	}

	events, err := d.Source.ActiveEvents(ctx)
	if err != nil {
		return d.failedDetection(ctx, path, err), nil
	}

	detection := model.ResourceDetection{State: model.StatePresent, Source: path}

	var errs []error
	for _, raw := range events {
		key, err := eventKey(raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fresh, err := d.Sink.MarkDeviceEventSeen(ctx, key, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !fresh {
			// Already seen: no write, no duplication.
			continue
		}
		if err := d.Sink.InsertEvent(ctx, deviceEvent(now, raw)); err != nil {
			errs = append(errs, err)
		}
	}
	return detection, errors.Join(errs...)
}

// failedDetection translates a read error into an `unknown` state.
func (d *Detector) failedDetection(ctx context.Context, path string, err error) model.ResourceDetection {
	kind, _ := froniuserr.KindOf(err)

	// The challenge-then-refusal sequence is more specific than the generic rules
	// below: the challenge proves the path exists, and the sequence proves
	// credentials were presented. Reporting it first keeps the cause visible.
	if errors.Is(err, froniuserr.ErrRefusedAfterChallenge) {
		status := 0
		var typed *froniuserr.Error
		if errors.As(err, &typed) {
			status = typed.HTTPStatus
		}
		return model.ResourceDetection{
			State:  model.StateUnknown,
			Source: path,
			Reason: fmt.Sprintf("%s (HTTP %d on %s)", ReasonRefusedAfterChallenge, status, path),
		}
	}

	if kind == froniuserr.KindUnauthorized && !d.CredentialsConfigured {
		// No credential: no point re-emitting a 401 on every cycle.
		d.abandon(path)
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: ReasonNoCredentials}
	}

	var reason string
	switch kind {
	case froniuserr.KindUnauthorized:
		reason = ReasonUnauthorized
	case froniuserr.KindNotFound:
		reason = ReasonEndpointAbsent
	default:
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			reason = "stop requested"
		} else {
			reason = truncate(err.Error(), maxReasonLength)
		}
	}
	return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}
}

// abandonedReason indicates whether a path has been abandoned, and why.
func (d *Detector) abandonedReason(path string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.abandoned[path] {
		return ReasonNoCredentials, true
	}
	return "", false
}

func (d *Detector) abandon(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.abandoned == nil {
		d.abandoned = map[string]bool{}
	}
	d.abandoned[path] = true
}

// firstCause returns the first unknown-state reason encountered, in
// the order of the reads. This is the message kept in detect_last_error.
func firstCause(d model.Detection) string {
	for _, r := range []model.ResourceDetection{d.Identity, d.Meter, d.Battery, d.DeviceEvents} {
		if r.State == model.StateUnknown && r.Reason != "" {
			return r.Source + ": " + r.Reason
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Device events
// ---------------------------------------------------------------------------

// eventKey builds the anti-duplication key of a raw event.
//
// Priority, established by the bundle extraction (docs/ENDPOINTS.md): `uuid` —
// the instance identity of the official client, which it compares to detect a
// state change (l. 135742) and sends back to `/commands/ConfirmEvent`
// (l. 135760) —, then `prefix`-`eventID` (same identity, concatenated form,
// l. 164699), then `code`, then `state`, then the SHA-256 fingerprint of the raw JSON.
//
// `code` is a *type* code: alone, it makes two active events of the same type
// collide, the second of which would never be notified. Hence the priority
// given to instance identifiers. Assumed consequence of this change of
// order: an event already seen under a `code` key is notified once more
// after an update, the first time it carries a `uuid`.
//
// The "device:" prefix avoids any collision with another family of keys.
func eventKey(raw json.RawMessage) (string, error) {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("unreadable device event: %w", err)
	}
	if uuid := identifierField(fields, "uuid"); uuid != "" {
		return "device:" + uuid, nil
	}
	prefix, eventID := identifierField(fields, "prefix"), identifierField(fields, "eventID")
	if prefix != "" && eventID != "" {
		return "device:" + prefix + "-" + eventID, nil
	}
	for _, key := range []string{"code", "state"} {
		if v := identifierField(fields, key); v != "" {
			return "device:" + v, nil
		}
	}
	sum := sha256.Sum256(raw)
	return "device:" + hex.EncodeToString(sum[:])[:16], nil
}

// identifierField returns an identity field as a string, or "".
//
// `uuid`, `code` and `state` are strings, but `eventID` may be a number:
// the official client concatenates the two forms without distinction
// (`${t.prefix}-${t.eventID}`, l. 164699). An integer is therefore rendered
// as is; any other form is ignored rather than guessed.
func identifierField(fields map[string]any, key string) string {
	switch v := fields[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

// deviceEvent builds the persisted event from the raw JSON.
//
// The timestamp is the **collector's**, not the device's: `timestamp`
// and `activeUntil` of the payload are device clock seconds
// (l. 135694-135695), and that clock is precisely the unknown that justifies
// `/commands/GetUTCTime`; `TS` is the sort and retention axis of the database, it
// therefore remains local. The fields observed without a consumer here (`category`,
// `viewer`, `confirmable`, `activeUntil`, `sourceID`) remain entirely in
// `Detail`, which keeps the raw JSON. No filtering on `confirmable` is
// applied: hiding events would require an observed rule, not a
// guess.
func deviceEvent(now time.Time, raw json.RawMessage) model.Event {
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields) // the shape may be unexpected: the raw detail is kept

	return model.Event{
		TS:       now.UTC(),
		Kind:     model.EventDevice,
		Severity: severityOf(fields),
		Message:  messageOf(fields),
		Detail:   truncate(string(raw), maxDetailBytes),
	}
}

// severityOf maps the announced severity of an event.
//
// The bundle extraction establishes the real shape: `severity` is an **integer**,
// `getEventLevel` (l. 135728-135733) mapping 1 → error, 2 → warning,
// 3 → information. Before this extraction, only strings were read: a
// real hardware event was therefore **always** classified "information". The
// text labels remain accepted by tolerance (simulator, fixtures) as well as
// `level` as a fallback key, and the first interpretable key wins. A
// missing or unknown value remains "information": no severity is
// invented.
func severityOf(fields map[string]any) model.Severity {
	for _, key := range []string{"severity", "level"} {
		if s, ok := severityValue(fields[key]); ok {
			return s
		}
	}
	return model.SeverityInfo
}

// severityValue interprets a severity value: device integer, integer
// serialized as a string, or text label.
func severityValue(v any) (model.Severity, bool) {
	var text string
	switch t := v.(type) {
	case float64:
		if t != float64(int64(t)) {
			return "", false
		}
		switch int64(t) {
		case 1:
			return model.SeverityError, true
		case 2:
			return model.SeverityWarning, true
		case 3:
			return model.SeverityInfo, true
		}
		text = strconv.FormatInt(int64(t), 10)
	case string:
		text = strings.TrimSpace(t)
	default:
		return "", false
	}
	switch text {
	case "":
		return "", false
	case "1":
		return model.SeverityError, true
	case "2":
		return model.SeverityWarning, true
	case "3":
		return model.SeverityInfo, true
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "warn"):
		return model.SeverityWarning, true
	case strings.Contains(lower, "error"), strings.Contains(lower, "fault"), strings.Contains(lower, "alert"):
		return model.SeverityError, true
	case strings.Contains(lower, "info"), strings.Contains(lower, "notice"):
		return model.SeverityInfo, true
	}
	return "", false
}

// messageOf returns the event text, or a neutral label.
//
// `label` comes first: the official client displays a text translated from
// a dictionary key (`STATECODETRANSLATIONS.StateCodes.<prefix>-<eventID>`,
// l. 164702) and falls back on `label` when that key is missing (l. 135654) — it is
// therefore the only readable text present in the payload. The historical
// candidates remain as a fallback.
func messageOf(fields map[string]any) string {
	for _, key := range []string{"label", "message", "text", "description"} {
		if v, ok := fields[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return deviceEventMessage
}

// truncate cuts a string to n bytes without breaking the last UTF-8 character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (d *Detector) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Detector) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}
