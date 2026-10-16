package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// secretValue est une valeur reconnaissable, utilisée pour détecter toute fuite.
const secretValue = "s3cr3t-à-ne-pas-divulguer"

func TestRevealReturnsValue(t *testing.T) {
	s := New(secretValue)
	if got := s.Reveal(); got != secretValue {
		t.Fatalf("Reveal() = %q, attendu %q", got, secretValue)
	}
}

func TestFormattingNeverLeaksValue(t *testing.T) {
	s := New(secretValue)

	// Le champ est aussi imbriqué dans une structure : fmt.Formater/reflect
	// parcourt alors les champs un par un, un chemin de fuite classique.
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
			t.Fatalf("%s a divulgué la valeur : %q", name, out)
		}
		if !strings.Contains(out, redactedText) {
			t.Fatalf("%s ne porte pas la mention d'expurgation : %q", name, out)
		}
	}
}

func TestSlogNeverLeaksValue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := New(secretValue)
	logger.Info("connexion", "password", s)
	logger.Info("connexion", slog.Any("password", s))
	logger.With("password", s).Warn("échec")

	out := buf.String()
	if strings.Contains(out, secretValue) {
		t.Fatalf("slog a divulgué la valeur :\n%s", out)
	}
	if !strings.Contains(out, redactedText) {
		t.Fatalf("slog ne porte pas la mention d'expurgation :\n%s", out)
	}
}

func TestMarshalJSONRedacts(t *testing.T) {
	s := New(secretValue)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// encoding/json échappe « < » et « > » : la forme brute est
	// "\u003credacted\u003e". C'est la valeur décodée qui compte.
	var decoded string
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal(%s): %v", b, err)
	}
	if decoded != redactedText {
		t.Fatalf("Marshal = %s (décodé %q)", b, decoded)
	}

	// Sérialisation d'une structure : le champ sensible reste masqué.
	b, err = json.Marshal(struct {
		User     string `json:"user"`
		Password String `json:"password"`
	}{User: "user", Password: s})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), secretValue) {
		t.Fatalf("Marshal a divulgué la valeur : %s", b)
	}
}

func TestIsZero(t *testing.T) {
	if !New("").IsZero() {
		t.Fatal("un secret vide doit être nul")
	}
	if New("x").IsZero() {
		t.Fatal("un secret non vide ne doit pas être nul")
	}
	if !(String("")).IsZero() {
		t.Fatal("la forme zéro du type doit être nulle")
	}
}
