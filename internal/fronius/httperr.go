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
func ClassifyTransport(ctx context.Context, endpoint string, err error) error {
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
