package froniuserr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorFormatting(t *testing.T) {
	err := &Error{
		Kind:        KindStatus,
		Endpoint:    "/solar_api/v1/GetPowerFlowRealtimeData.fcgi",
		HTTPStatus:  200,
		Code:        7,
		Reason:      "Device error",
		UserMessage: "Device error",
		Err:         errors.New("cause"),
	}
	msg := err.Error()
	for _, want := range []string{"device_status", "GetPowerFlowRealtimeData", "HTTP 200", "code 7", "Device error", "cause"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q ne contient pas %q", msg, want)
		}
	}
	// UserMessage identique à Reason : pas de doublon.
	if strings.Count(msg, "Device error") != 1 {
		t.Fatalf("message redondant : %q", msg)
	}
}

func TestErrorMinimalFormatting(t *testing.T) {
	err := &Error{Kind: KindTransport}
	if got, want := err.Error(), "fronius: transport"; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
}

func TestKindOfWrapped(t *testing.T) {
	base := &Error{Kind: KindTimeout, Endpoint: "/x"}
	wrapped := fmt.Errorf("collecte: %w", base)

	kind, ok := KindOf(wrapped)
	if !ok || kind != KindTimeout {
		t.Fatalf("KindOf = (%q, %v)", kind, ok)
	}
	if kind, ok := KindOf(errors.New("autre")); ok || kind != "" {
		t.Fatalf("KindOf(erreur banale) = (%q, %v)", kind, ok)
	}
	if kind, ok := KindOf(nil); ok || kind != "" {
		t.Fatalf("KindOf(nil) = (%q, %v)", kind, ok)
	}
}

func TestErrorsIsByKind(t *testing.T) {
	err := fmt.Errorf("wrapper: %w", &Error{Kind: KindNotFound, HTTPStatus: 404})

	if !errors.Is(err, &Error{Kind: KindNotFound}) {
		t.Fatal("errors.Is devait reconnaître KindNotFound")
	}
	if errors.Is(err, &Error{Kind: KindServer}) {
		t.Fatal("errors.Is a confondu deux natures")
	}
	if errors.Is(err, errors.New("x")) {
		t.Fatal("errors.Is a reconnu une erreur non typée")
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("racine")
	err := &Error{Kind: KindDecode, Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap n'expose pas la cause")
	}
}
