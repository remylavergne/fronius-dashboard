// Package secret carries a sensitive value without ever exposing it.
//
// A secret leaves in clear only through Reveal(). Any other formatting —
// fmt (%v, %s, %q, %+v, %#v, including for a field nested in a struct),
// slog and JSON — produces "<redacted>". A leak into a log record or into an
// HTTP response therefore becomes impossible by construction, and not just by
// vigilance: forgetting to filter a field is no longer enough to disclose the
// value.
//
// Invariant: no device credential circulates as a bare string in the
// application. The only call to Reveal() is the HA1 computation of the Digest
// (internal/fronius/digest).
package secret

import "log/slog"

// redactedText is the only un-revealed representation of a secret.
const redactedText = "<redacted>"

// String is a sensitive string.
//
// The type is comparable (like string), so it can be used directly in a struct
// comparison: the configuration keeps its comparability.
type String string

// New builds a secret from a clear value.
func New(v string) String { return String(v) }

// Reveal returns the clear value. It is the only unmasked output.
func (s String) Reveal() string { return string(s) }

// String implements fmt.Stringer: any formatting displays <redacted>.
func (s String) String() string { return redactedText }

// GoString implements fmt.GoStringer: "%#v" must not bypass String.
func (s String) GoString() string { return redactedText }

// LogValue implements slog.LogValuer: a log record cannot contain the value.
func (s String) LogValue() slog.Value { return slog.StringValue(redactedText) }

// MarshalJSON masks the value: a secret cannot be serialized in clear.
func (s String) MarshalJSON() ([]byte, error) { return []byte(`"` + redactedText + `"`), nil }

// IsZero reports whether the secret is empty. Use it to test the absence of
// credentials: comparing must never require revealing the value.
func (s String) IsZero() bool { return s == "" }
