package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// secretValue is a recognizable value, used to detect any leak.
const secretValue = "s3cr3t-not-to-be-disclosed"

func TestRevealReturnsValue(t *testing.T) {
	s := New(secretValue)
	if got := s.Reveal(); got != secretValue {
		t.Fatalf("Reveal() = %q, want %q", got, secretValue)
	}
}

func TestFormattingNeverLeaksValue(t *testing.T) {
	s := New(secretValue)

	// The field is also nested in a struct: fmt.Formater/reflect then walks the
	// fields one by one, a classic leak path.
	type holder struct {
		User     string
		Password String
	}
	h := holder{User: "user", Password: s}

	formats := map[string]string{
		"%v":         fmt.Sprintf("%v", s),
		"%s":         fmt.Sprintf("%s", s),
		"%q":         fmt.Sprintf("%q", s),
		"%+v":        fmt.Sprintf("%+v", s),
		"%#v":        fmt.Sprintf("%#v", s),
		"struct %v":  fmt.Sprintf("%v", h),
		"struct %+v": fmt.Sprintf("%+v", h),
		"struct %#v": fmt.Sprintf("%#v", h),
	}
	for name, out := range formats {
		if strings.Contains(out, secretValue) {
			t.Fatalf("%s leaked the value: %q", name, out)
		}
		if !strings.Contains(out, redactedText) {
			t.Fatalf("%s does not carry the redaction marker: %q", name, out)
		}
	}
}

func TestSlogNeverLeaksValue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := New(secretValue)
	logger.Info("connection", "password", s)
	logger.Info("connection", slog.Any("password", s))
	logger.With("password", s).Warn("failure")

	out := buf.String()
	if strings.Contains(out, secretValue) {
		t.Fatalf("slog leaked the value:\n%s", out)
	}
	if !strings.Contains(out, redactedText) {
		t.Fatalf("slog does not carry the redaction marker:\n%s", out)
	}
}

func TestMarshalJSONRedacts(t *testing.T) {
	s := New(secretValue)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// encoding/json escapes "<" and ">": the raw form is
	// "\u003credacted\u003e". The decoded value is what matters.
	var decoded string
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", b, err)
	}
	if decoded != redactedText {
		t.Fatalf("Marshal = %s (decoded %q)", b, decoded)
	}

	// Serialization of a struct: the sensitive field stays masked.
	b, err = json.Marshal(struct {
		User     string `json:"user"`
		Password String `json:"password"`
	}{User: "user", Password: s})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), secretValue) {
		t.Fatalf("Marshal leaked the value: %s", b)
	}
}

func TestIsZero(t *testing.T) {
	if !New("").IsZero() {
		t.Fatal("an empty secret must be zero")
	}
	if New("x").IsZero() {
		t.Fatal("a non-empty secret must not be zero")
	}
	if !(String("")).IsZero() {
		t.Fatal("the zero form of the type must be zero")
	}
}
