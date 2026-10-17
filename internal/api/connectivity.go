package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/digest"
	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
)

// connectivityCacheWindow: a second test pressed inside this window reuses the
// previous result. The rationale is the device's authentication counter — a
// wrong password pressed repeatedly would hammer it — not pacing.
const connectivityCacheWindow = 10 * time.Second

// Probe names, as the interface shows them.
const (
	probeNamePowerFlow = "solar_api_powerflow"
	probeNameVersion   = "internal_version"
)

// Probe states.
const (
	probeStateOK           = "ok"
	probeStateSkipped      = "skipped"
	probeStateUnauthorized = "unauthorized"
	probeStateAbsent       = "absent"
	probeStateTimeout      = "timeout"
	probeStateUnreachable  = "unreachable"
	probeStateError        = "error"
)

// Overall verdicts.
const (
	overallOK      = "ok"
	overallPartial = "partial"
	overallFailed  = "failed"
)

// reasons that are not a froniuserr.Kind.
const (
	reasonNoCredentials = "credentials_not_configured"
	reasonUnclassified  = "unclassified"
	reasonClientGuard   = "client_construction_failed"
)

// handleConnectivity runs the two read-only probes and reports their states.
//
// It proves two things only: the device answers GET on the Solar API path, and
// the configured credentials are accepted for /status/version. It proves
// nothing about write access: no request other than GET can leave this
// process.
func (s *Server) handleConnectivity(w http.ResponseWriter, r *http.Request) {
	s.writeData(w, http.StatusOK, s.connectivity(r.Context()), s.baseMeta())
}

// connectivity returns the cached result when it is still inside its window,
// otherwise runs the probes.
//
// The mutex covers the probes themselves: the server is shared, and two
// simultaneous presses must not multiply the authentication attempts. The
// first one runs the test, the second one finds its result cached.
func (s *Server) connectivity(ctx context.Context) connectivityDTO {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	now := s.now()
	if s.connResult != nil && now.Sub(s.connTestedAt) < connectivityCacheWindow {
		out := *s.connResult
		out.Cached = true
		return out
	}

	out := s.runConnectivity(ctx, now)
	s.connResult = &out
	s.connTestedAt = now
	return out
}

// runConnectivity executes the probes in sequence, in the order the screen
// shows them.
func (s *Server) runConnectivity(ctx context.Context, testedAt time.Time) connectivityDTO {
	solarClient, internalClient, err := s.connectivityClients()
	if err != nil {
		// The daemon validates the configuration at startup, so this is a
		// programming guard, not an operator mistake. The reason names the
		// class, never the URL.
		s.log.Error("connectivity: building the clients failed", "error", err)
		return connectivityDTO{
			TestedAt:              formatTime(testedAt),
			CredentialsConfigured: s.credentialsConfigured(),
			Overall:               overallFailed,
			Probes: []connectivityProbeDTO{
				failedProbe(probeNamePowerFlow, solarapi.PowerFlowPath, reasonClientGuard),
				failedProbe(probeNameVersion, internalapi.VersionPath, reasonClientGuard),
			},
		}
	}

	probes := []connectivityProbeDTO{
		s.probePowerFlow(ctx, solarClient),
		s.probeVersion(ctx, internalClient),
	}
	return connectivityDTO{
		TestedAt:              formatTime(testedAt),
		CredentialsConfigured: s.credentialsConfigured(),
		Overall:               overallOf(probes),
		Probes:                probes,
	}
}

// connectivityClients builds the two clients exactly as the daemon does:
// Digest transport, then the read-only guard, then the typed client bounded by
// the configured HTTP timeout.
func (s *Server) connectivityClients() (*solarapi.Client, *internalapi.Client, error) {
	transport, err := digest.New(&http.Client{}, s.cfg.SolarAPIURL,
		digest.Credentials{Username: s.cfg.DeviceUser, Password: s.cfg.DevicePassword}, s.log)
	if err != nil {
		return nil, nil, err
	}
	guarded := fronius.ReadOnly{Inner: transport}

	solarClient, err := solarapi.New(s.cfg.SolarAPIURL, guarded, s.cfg.HTTPTimeout, s.log)
	if err != nil {
		return nil, nil, err
	}
	internalClient, err := internalapi.New(s.cfg.SolarAPIURL, guarded, s.cfg.HTTPTimeout, s.log)
	if err != nil {
		return nil, nil, err
	}
	return solarClient, internalClient, nil
}

// probePowerFlow proves the device is reachable through the application's own
// Solar API client path. It carries no detail: no measurement is invented here.
func (s *Server) probePowerFlow(ctx context.Context, client *solarapi.Client) connectivityProbeDTO {
	start := time.Now()
	_, err := client.GetPowerFlowRealtimeData(ctx)
	out := connectivityProbeDTO{
		Name:       probeNamePowerFlow,
		Path:       solarapi.PowerFlowPath,
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		classifyProbe(&out, err)
		return out
	}
	out.State = probeStateOK
	status := http.StatusOK
	out.HTTPStatus = &status
	return out
}

// probeVersion proves the configured credentials are accepted for
// /status/version, and reports the firmware the device answered with.
//
// Without credentials the probe is skipped, not attempted: sending an
// unauthenticated request would only record a refusal that says nothing about
// the credentials.
func (s *Server) probeVersion(ctx context.Context, client *internalapi.Client) connectivityProbeDTO {
	out := connectivityProbeDTO{Name: probeNameVersion, Path: internalapi.VersionPath}
	if !s.credentialsConfigured() {
		out.State = probeStateSkipped
		out.Reason = reasonNoCredentials
		return out
	}

	start := time.Now()
	version, err := client.Version(ctx)
	out.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		classifyProbe(&out, err)
		return out
	}
	out.State = probeStateOK
	status := http.StatusOK
	out.HTTPStatus = &status
	if version.Firmware != nil && *version.Firmware != "" {
		out.Detail = "firmware " + *version.Firmware
	}
	return out
}

// failedProbe is a probe that could not even be attempted.
func failedProbe(name, path, reason string) connectivityProbeDTO {
	return connectivityProbeDTO{Name: name, Path: path, State: probeStateError, Reason: reason}
}

// classifyProbe maps a classified error onto a state and a reason.
//
// The classification is read from the error itself (froniuserr.KindOf): an
// already-decided kind survives the transport, and no string is inspected.
// `reason` carries that class, never a body and never a URL.
func classifyProbe(out *connectivityProbeDTO, err error) {
	var typed *froniuserr.Error
	if errors.As(err, &typed) && typed.HTTPStatus != 0 {
		status := typed.HTTPStatus
		out.HTTPStatus = &status
	}

	kind, ok := froniuserr.KindOf(err)
	if !ok {
		out.State = probeStateError
		out.Reason = reasonUnclassified
		return
	}

	out.Reason = string(kind)
	switch kind {
	case froniuserr.KindNotFound:
		out.State = probeStateAbsent
	case froniuserr.KindUnauthorized:
		out.State = probeStateUnauthorized
	case froniuserr.KindTimeout:
		out.State = probeStateTimeout
	case froniuserr.KindTransport:
		out.State = probeStateUnreachable
	default:
		out.State = probeStateError
	}
}

// overallOf derives the verdict: the device not answering the powerflow probe
// is a failure; reachable but the internal route or the credentials not
// accepted is partial.
func overallOf(probes []connectivityProbeDTO) string {
	if len(probes) == 0 || probes[0].State != probeStateOK {
		return overallFailed
	}
	for _, p := range probes {
		if p.State != probeStateOK && p.State != probeStateSkipped {
			return overallPartial
		}
	}
	return overallOK
}

// credentialsConfigured reports whether both halves of the pair are set.
// An incomplete pair authenticates nothing (digest.Credentials.IsZero).
func (s *Server) credentialsConfigured() bool {
	return !digest.Credentials{Username: s.cfg.DeviceUser, Password: s.cfg.DevicePassword}.IsZero()
}
