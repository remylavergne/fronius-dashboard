package solarapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// TestGetPowerFlowRealtimeDataAtSendsRFC3339At pins the simulator extension: the
// "at" parameter must reach the server, URL-encoded, and its value must be
// exactly the RFC3339 form the simulator parses back.
func TestGetPowerFlowRealtimeDataAtSendsRFC3339At(t *testing.T) {
	at := time.Date(2026, 10, 16, 15, 17, 23, 0, time.UTC)
	var received string
	srv, rec := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		received = r.URL.Query().Get("at")
		_, _ = io.WriteString(w, validBody)
	})
	c := newClient(t, srv.URL, 2*time.Second)

	if _, err := c.GetPowerFlowRealtimeDataAt(context.Background(), at); err != nil {
		t.Fatalf("GetPowerFlowRealtimeDataAt: %v", err)
	}
	if rec.path != PowerFlowPath {
		t.Fatalf("path = %q, want %q", rec.path, PowerFlowPath)
	}
	if rec.rawQuery != "at=2026-10-16T15%3A17%3A23Z" {
		t.Fatalf("raw query = %q", rec.rawQuery)
	}
	parsed, err := time.Parse(time.RFC3339, received)
	if err != nil {
		t.Fatalf("the server cannot parse the at value %q: %v", received, err)
	}
	if !parsed.Equal(at) {
		t.Fatalf("at = %v, want %v", parsed, at)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("at carries %v, want UTC (a Z suffix)", parsed.Location())
	}
}

// TestGetPowerFlowRealtimeDataSendsNoAt: the plain read must not carry the
// simulator extension at all.
func TestGetPowerFlowRealtimeDataSendsNoAt(t *testing.T) {
	srv, rec := newServer(t, staticHandler(http.StatusOK, validBody))
	c := newClient(t, srv.URL, 2*time.Second)

	if _, err := c.GetPowerFlowRealtimeData(context.Background()); err != nil {
		t.Fatalf("GetPowerFlowRealtimeData: %v", err)
	}
	q, err := url.ParseQuery(rec.rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", rec.rawQuery, err)
	}
	if _, ok := q["at"]; ok {
		t.Fatalf("an at parameter was sent: %q", rec.rawQuery)
	}
}

// TestGetPowerFlowRealtimeDataAtTimeout: the variant shares the client timeout,
// and its expiry is classified as a timeout.
func TestGetPowerFlowRealtimeDataAtTimeout(t *testing.T) {
	srv, _ := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := newClient(t, srv.URL, 50*time.Millisecond)

	_, err := c.GetPowerFlowRealtimeDataAt(context.Background(), time.Now())
	if err == nil {
		t.Fatal("a timeout was expected")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindTimeout {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindTimeout, err)
	}
}

// TestGetPowerFlowRealtimeDataAtClassifiesStatus: a non-200 goes through
// ClassifyStatus on the same endpoint path.
func TestGetPowerFlowRealtimeDataAtClassifiesStatus(t *testing.T) {
	srv, _ := newServer(t, staticHandler(http.StatusNotFound, "not found"))
	c := newClient(t, srv.URL, 2*time.Second)

	_, err := c.GetPowerFlowRealtimeDataAt(context.Background(), time.Now())
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindNotFound {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindNotFound, err)
	}
	var typed *froniuserr.Error
	if !errors.As(err, &typed) {
		t.Fatal("errors.As did not find *froniuserr.Error")
	}
	if typed.HTTPStatus != http.StatusNotFound {
		t.Fatalf("HTTPStatus = %d, want %d", typed.HTTPStatus, http.StatusNotFound)
	}
	if typed.Endpoint != PowerFlowPath {
		t.Fatalf("Endpoint = %q, want %q", typed.Endpoint, PowerFlowPath)
	}
}

// TestGetPowerFlowRealtimeDataAtMalformedBody: a truncated body yields a decode
// error rather than a panic.
func TestGetPowerFlowRealtimeDataAtMalformedBody(t *testing.T) {
	srv, _ := newServer(t, staticHandler(http.StatusOK, `{"Body":{"Data":{"Site":{"P_PV":`))
	c := newClient(t, srv.URL, 2*time.Second)

	_, err := c.GetPowerFlowRealtimeDataAt(context.Background(), time.Now())
	if err == nil {
		t.Fatal("a decode error was expected")
	}
	if kind, _ := froniuserr.KindOf(err); kind != froniuserr.KindDecode {
		t.Fatalf("KindOf = %q, want %q (%v)", kind, froniuserr.KindDecode, err)
	}
}
