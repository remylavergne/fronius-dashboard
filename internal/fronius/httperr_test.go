package fronius

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// timeoutNetError is a net.Error whose Timeout() reports true: the shape a
// transport-layer deadline takes when the transport itself reports it.
type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

func assertKind(t *testing.T, err error, want froniuserr.Kind) {
	t.Helper()
	kind, ok := froniuserr.KindOf(err)
	if !ok || kind != want {
		t.Fatalf("KindOf = (%q, %v), want %q (%v)", kind, ok, want, err)
	}
}

// TestClassifyStatus pins one case per branch of ClassifyStatus, by kind and
// not by message: 429 belongs to the server errors on purpose, so the collector
// backs off.
func TestClassifyStatus(t *testing.T) {
	const endpoint = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"

	cases := []struct {
		name   string
		status int
		want   froniuserr.Kind
	}{
		{"404 endpoint absent", http.StatusNotFound, froniuserr.KindNotFound},
		{"401 authentication required", http.StatusUnauthorized, froniuserr.KindUnauthorized},
		{"403 access refused", http.StatusForbidden, froniuserr.KindUnauthorized},
		{"429 slow down (grouped with 5xx so the collector backs off)", http.StatusTooManyRequests, froniuserr.KindServer},
		{"500 internal", http.StatusInternalServerError, froniuserr.KindServer},
		{"502 bad gateway", http.StatusBadGateway, froniuserr.KindServer},
		{"503 unavailable", http.StatusServiceUnavailable, froniuserr.KindServer},
		{"418 falls through to the default branch", http.StatusTeapot, froniuserr.KindServer},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ClassifyStatus(endpoint, tc.status)
			assertKind(t, err, tc.want)

			var typed *froniuserr.Error
			if !errors.As(err, &typed) {
				t.Fatal("errors.As did not find *froniuserr.Error")
			}
			if typed.Endpoint != endpoint {
				t.Fatalf("Endpoint = %q, want %q", typed.Endpoint, endpoint)
			}
			if typed.HTTPStatus != tc.status {
				t.Fatalf("HTTPStatus = %d, want %d", typed.HTTPStatus, tc.status)
			}
		})
	}
}

// TestClassifyTransport pins each branch: an explicit deadline, a net.Error
// whose Timeout() is true, a plain error, and an expired context that did not
// carry the deadline error itself.
func TestClassifyTransport(t *testing.T) {
	const endpoint = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"
	sentinel := errors.New("connection reset by peer")

	t.Run("context.DeadlineExceeded", func(t *testing.T) {
		err := ClassifyTransport(context.Background(), endpoint, context.DeadlineExceeded)
		assertKind(t, err, froniuserr.KindTimeout)
	})

	t.Run("net.Error reporting Timeout", func(t *testing.T) {
		err := ClassifyTransport(context.Background(), endpoint, timeoutNetError{})
		assertKind(t, err, froniuserr.KindTimeout)
	})

	t.Run("arbitrary error is a transport failure", func(t *testing.T) {
		err := ClassifyTransport(context.Background(), endpoint, sentinel)
		assertKind(t, err, froniuserr.KindTransport)
	})

	t.Run("expired context with a non-deadline error", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
		}
		err := ClassifyTransport(ctx, endpoint, sentinel)
		assertKind(t, err, froniuserr.KindTimeout)
	})

	t.Run("the cause stays reachable and the endpoint is set", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()

		err := ClassifyTransport(ctx, endpoint, sentinel)
		if !errors.Is(err, sentinel) {
			t.Fatalf("errors.Is(err, sentinel) = false: %v", err)
		}
		var typed *froniuserr.Error
		if !errors.As(err, &typed) {
			t.Fatal("errors.As did not find *froniuserr.Error")
		}
		if typed.Endpoint != endpoint {
			t.Fatalf("Endpoint = %q, want %q", typed.Endpoint, endpoint)
		}
		if typed.Err != sentinel {
			t.Fatalf("Err = %v, want the original sentinel", typed.Err)
		}
	})
}

// TestClassifyTransportKeepsADecidedNature: the Digest transport answers with
// the very status the device returned, and the collector and the detector read
// the nature of the *outermost* error. Re-classifying such a failure as a
// transport one would hide an authentication problem — or the read-only guard's
// own refusal — behind a network excuse.
func TestClassifyTransportKeepsADecidedNature(t *testing.T) {
	const endpoint = "/status/activeEvents"

	for _, kind := range []froniuserr.Kind{
		froniuserr.KindUnauthorized,
		froniuserr.KindReadOnly,
		froniuserr.KindNotFound,
		froniuserr.KindDecode,
		froniuserr.KindServer,
	} {
		refusal := &froniuserr.Error{
			Kind:       kind,
			Endpoint:   endpoint,
			HTTPStatus: http.StatusUnauthorized,
			Err:        errors.New("the device refused the authenticated request"),
		}

		t.Run(string(kind), func(t *testing.T) {
			// The already-decided error is returned untouched, so nothing about
			// it is lost: neither the status nor the cause.
			got := ClassifyTransport(context.Background(), endpoint, refusal)
			if got != error(refusal) {
				t.Fatalf("the error was rebuilt: %v", got)
			}
			assertKind(t, got, kind)

			// The same holds when the transport wrapped it on the way out.
			wrapped := fmt.Errorf("digest round-trip: %w", refusal)
			assertKind(t, ClassifyTransport(context.Background(), endpoint, wrapped), kind)
		})
	}
}

// TestClassifyTransportStillUpgradesAGenericFailure: only a failure that was
// already a transport failure can be re-classified, which is what lets an
// expired context turn it into a timeout.
func TestClassifyTransportStillUpgradesAGenericFailure(t *testing.T) {
	const endpoint = "/solar_api/v1/GetPowerFlowRealtimeData.fcgi"
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	inner := &froniuserr.Error{
		Kind:     froniuserr.KindTransport,
		Endpoint: endpoint,
		Err:      errors.New("connection reset by peer"),
	}
	assertKind(t, ClassifyTransport(ctx, endpoint, inner), froniuserr.KindTimeout)

	// Without an expired context it stays a transport failure, and the original
	// cause is still reachable.
	got := ClassifyTransport(context.Background(), endpoint, inner)
	assertKind(t, got, froniuserr.KindTransport)
	if !errors.Is(got, inner.Err) {
		t.Fatalf("the cause was lost: %v", got)
	}
}
