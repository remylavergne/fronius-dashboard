package api

import (
	"net/http"

	"fronius-supervisor/internal/sources"
)

// sourcesDTO is the payload of /api/v1/sources: the catalogue of what the
// device exposes and what this application does with it.
//
// The catalogue is static data (internal/sources) — it describes endpoints, not
// measurements — so the answer carries no freshness, only the envelope's
// generated_at. The runtime half (is the device answering, which capability is
// available and why) stays in /api/v1/system, /api/v1/health and
// /api/v1/capabilities: the interface joins them, the API does not duplicate
// them.
type sourcesDTO struct {
	Summary sources.Summary  `json:"summary"`
	Sources []sources.Source `json:"sources"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	list := sources.Catalog()
	s.writeData(w, http.StatusOK, sourcesDTO{
		Summary: sources.Summarize(list),
		Sources: list,
	}, s.baseMeta())
}
