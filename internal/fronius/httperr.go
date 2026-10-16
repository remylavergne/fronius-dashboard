// Classification of HTTP/transport errors shared by all Fronius clients
// (Solar API and internal paths).
//
// A single place decides which kind of error an HTTP code maps to: both
// clients therefore classify identically, and the collector as well as the
// detector can react through froniuserr.KindOf without inspecting strings.
package fronius

import (
	"context"
	"errors"
	"net"
	"net/http"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// ClassifyStatus translates non-200 HTTP codes.
//
// 429 is classified with the server errors: the device explicitly asks to
// slow down, and the collector then applies its backoff.
func ClassifyStatus(endpoint string, status int) error {
	kind := froniuserr.KindServer
	switch status {
	case http.StatusNotFound:
		kind = froniuserr.KindNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = froniuserr.KindUnauthorized
	}
	return &froniuserr.Error{Kind: kind, Endpoint: endpoint, HTTPStatus: status}
}

// ClassifyTransport distinguishes timeout expiry from other network failures.
//
// A failure that already carries a decided nature keeps it. The Digest
// transport answers with the very status the device returned — a refusal after
// a challenge, an HTTP 401 — and re-wrapping that as a transport failure would
// hide an authentication problem behind a network excuse: the collector, the
// detector and the interface all read the kind of the outermost error. Only a
// plain failure, or one already classified as a transport failure, is
// (re)classified here — so a context that expired can still upgrade a generic
// transport error to a timeout.
func ClassifyTransport(ctx context.Context, endpoint string, err error) error {
	var typed *froniuserr.Error
	if errors.As(err, &typed) && typed.Kind != froniuserr.KindTransport {
		return err
	}
	kind := froniuserr.KindTransport
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		kind = froniuserr.KindTimeout
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		kind = froniuserr.KindTimeout
	}
	return &froniuserr.Error{Kind: kind, Endpoint: endpoint, Err: err}
}
