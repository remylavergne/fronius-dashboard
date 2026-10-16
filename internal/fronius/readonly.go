// Package fronius contains the primitives shared by the inverter clients.
//
// Invariant: the application never modifies the device configuration.
// All clients are built behind ReadOnly, which refuses any method other than
// GET before even opening a connection. The composition order is deliberately
// ReadOnly{Inner: digest.Transport{…}}: the guard is on the outside, so
// authentication cannot turn a forbidden request into an acceptable one.
package fronius

import (
	"fmt"
	"net/http"

	"fronius-supervisor/internal/fronius/froniuserr"
)

// Doer is the subset of *http.Client used by the Fronius clients.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ReadOnly is a Doer that only allows GET requests.
//
// The refusal is structural: it depends neither on the configuration, nor on a
// flag, nor on the caller's goodwill. The only possible remote operation is a
// read.
type ReadOnly struct {
	Inner Doer
}

// Do executes req if and only if its method is GET.
func (r ReadOnly) Do(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, &froniuserr.Error{
			Kind: froniuserr.KindReadOnly,
			Err:  fmt.Errorf("nil request"),
		}
	}
	if req.Method != http.MethodGet {
		endpoint := ""
		if req.URL != nil {
			endpoint = req.URL.Path
		}
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindReadOnly,
			Endpoint: endpoint,
			Err:      fmt.Errorf("method %s forbidden: only GET is allowed towards the inverter", req.Method),
		}
	}
	if r.Inner == nil {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindTransport,
			Endpoint: req.URL.Path,
			Err:      fmt.Errorf("no transport configured (ReadOnly.Inner is nil)"),
		}
	}
	return r.Inner.Do(req)
}
