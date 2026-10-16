package api

import (
	"net/http"
	"time"

	"fronius-supervisor/internal/sources"
	"fronius-supervisor/internal/storage"
)

// probeDTO is what the last read-only survey observed on one path.
//
// It is deliberately not a capture: the class says what the device answered
// (and "it answered nothing" is an answer), the field list gives the shape, and
// no value of the answer is ever carried here.
type probeDTO struct {
	Class          string               `json:"class"`
	HTTPStatus     int                  `json:"http_status"`
	DigestRequired bool                 `json:"digest_required"`
	Bytes          int                  `json:"bytes"`
	Error          string               `json:"error,omitempty"`
	Firmware       string               `json:"firmware,omitempty"`
	ProbedAt       string               `json:"probed_at"`
	Fields         []storage.ProbeField `json:"fields,omitempty"`
}

// sourceDTO is a catalogued source plus what the survey saw on it.
//
// The catalogue is static (internal/sources); the probe is the observation.
// Keeping them on one row is what lets the interface tell `available and
// unmapped` from `documented but never observed` without holding a second list.
type sourceDTO struct {
	sources.Source
	Probe *probeDTO `json:"probe,omitempty"`
}

// surveyDTO counts the recorded survey. It is empty (all zero, no timestamp)
// until a survey has been run and recorded: an interface must be able to say
// "never surveyed" instead of showing zeros as a result.
type surveyDTO struct {
	Probed       int     `json:"probed"`
	OK           int     `json:"ok"`
	Empty        int     `json:"empty"`
	NotJSON      int     `json:"not_json"`
	AuthRequired int     `json:"auth_required"`
	NotFound     int     `json:"not_found"`
	Error        int     `json:"error"`
	LastProbedAt *string `json:"last_probed_at,omitempty"`
	Firmware     string  `json:"firmware,omitempty"`
}

// sourcesDTO is the payload of /api/v1/sources: the catalogue of what the
// device exposes, what this application does with it, and what the last survey
// observed.
//
// The catalogue is static data, so the envelope carries no freshness; the
// runtime half (is the device answering, which capability is available and why)
// stays in /api/v1/system, /api/v1/health and /api/v1/capabilities: the
// interface joins them, the API does not duplicate them.
type sourcesDTO struct {
	Summary sources.Summary `json:"summary"`
	Survey  surveyDTO       `json:"survey"`
	Sources []sourceDTO     `json:"sources"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	recorded, err := s.store.SourceProbes(ctx)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, CodeInternal, "reading the survey")
		return
	}

	byKey := make(map[string]storage.SourceProbe, len(recorded))
	for _, p := range recorded {
		byKey[probeKey(p.Path, p.Query)] = p
	}

	list := sources.Catalog()
	out := make([]sourceDTO, 0, len(list))
	survey := surveyDTO{}
	for _, src := range list {
		entry := sourceDTO{Source: src}
		if p, ok := byKey[probeKey(src.Path, src.Query)]; ok {
			entry.Probe = probeToDTO(p)
			survey.Probed++
			switch storage.ProbeClass(p.Class) {
			case storage.ProbeOK:
				survey.OK++
			case storage.ProbeEmpty:
				survey.Empty++
			case storage.ProbeNotJSON:
				survey.NotJSON++
			case storage.ProbeAuthRequired:
				survey.AuthRequired++
			case storage.ProbeNotFound:
				survey.NotFound++
			case storage.ProbeError:
				survey.Error++
			}
			if survey.LastProbedAt == nil || p.ProbedAt.After(probedAtOf(*survey.LastProbedAt)) {
				at := formatTime(p.ProbedAt)
				survey.LastProbedAt = &at
			}
			if survey.Firmware == "" && p.Firmware != "" {
				survey.Firmware = p.Firmware
			}
		}
		out = append(out, entry)
	}

	s.writeData(w, http.StatusOK, sourcesDTO{
		Summary: sources.Summarize(list),
		Survey:  survey,
		Sources: out,
	}, s.baseMeta())
}

// probeKey identifies a source the way the survey does: the frozen query is
// part of the identity of a path, since two DataCollections of the same endpoint
// answer different things.
func probeKey(path, query string) string {
	if query == "" {
		return path
	}
	return path + "?" + query
}

func probeToDTO(p storage.SourceProbe) *probeDTO {
	return &probeDTO{
		Class:          string(p.Class),
		HTTPStatus:     p.HTTPStatus,
		DigestRequired: p.DigestRequired,
		Bytes:          p.Bytes,
		Error:          p.Error,
		Firmware:       p.Firmware,
		ProbedAt:       formatTime(p.ProbedAt),
		Fields:         p.Fields,
	}
}

// probedAtOf reads back an instant this package formatted itself. The layout is
// the API's own, so a value that does not parse cannot come from here; the zero
// time is then the honest answer (it compares as "older than everything").
func probedAtOf(formatted string) time.Time {
	ts, err := time.Parse(apiTimeLayout, formatted)
	if err != nil {
		return time.Time{}
	}
	return ts
}
