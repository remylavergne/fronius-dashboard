package api

import (
	"encoding/json"
	"net/http"

	"fronius-supervisor/internal/model"
)

// The detail endpoint publishes the diagnostic families the collector and the
// detector read on their own cycles: what the device says about itself, beyond
// the measurements.
//
// Two rules shape it:
//
//   - a family is published as the device answered it. A family that could not
//     be read carries `available: false` and its reason, and a family never read
//     is absent from the list — three distinct facts a reader must not have to
//     guess from an empty table;
//   - the snapshot is not live. Each family carries the instant of its own read,
//     because they are read on different cycles (the inverter detail with the
//     measurements, the system and network families with the detection).

// detailMeasurementDTO is one measurement of the inverter family.
//
// `label` is a fallback in English: the interface knows these keys and has its
// own dictionary, and uses this one only for a key the device added and nobody
// catalogued. `value` is null when the device reported nothing — never zero.
type detailMeasurementDTO struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Unit  string   `json:"unit"`
	Value *float64 `json:"value"`
}

// inverterDetailDTO is the published inverter family.
type inverterDetailDTO struct {
	State        model.InverterStateDetail `json:"state"`
	Measurements []detailMeasurementDTO    `json:"measurements"`
	Energies     model.InverterEnergies    `json:"energies"`
}

// detailFamilyDTO is one family of the payload. `data` keeps the shape the
// family defines (see internal/model/detail.go): it is passed through, so the
// endpoint cannot introduce a field the reader did not store.
type detailFamilyDTO struct {
	Family    string          `json:"family"`
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	ReadAt    string          `json:"read_at"`
	Data      json.RawMessage `json:"data"`
}

type detailDTO struct {
	// Source is the device the collector read from.
	//
	// It is published with the families because a reason is only actionable next
	// to the source it came from: a page of `404` is a wrong source far more
	// often than five missing endpoints, and the development default points at
	// the simulator.
	Source string `json:"source"`
	// ReadAt is the most recent family read: the snapshot's age.
	ReadAt   *string           `json:"read_at,omitempty"`
	Families []detailFamilyDTO `json:"families"`
}

// measurementLabels names the measurements observed on the reference device.
// A key that is not here is published with its own name as the label: the
// device's set is not closed.
var measurementLabels = map[string]string{
	"PAC":          "AC power",
	"SAC":          "Apparent power",
	"UAC":          "AC voltage",
	"IAC":          "AC current",
	"UDC":          "DC voltage, input 1",
	"IDC":          "DC current, input 1",
	"UDC_2":        "DC voltage, input 2",
	"IDC_2":        "DC current, input 2",
	"UDC_3":        "DC voltage, input 3",
	"IDC_3":        "DC current, input 3",
	"DAY_ENERGY":   "Energy of the day",
	"YEAR_ENERGY":  "Energy of the year",
	"TOTAL_ENERGY": "Total energy",
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	details, err := s.store.Details(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, CodeInternal, "reading the device detail")
		return
	}

	out := detailDTO{Source: s.cfg.SolarAPIURL, Families: make([]detailFamilyDTO, 0, len(details))}
	var newest string
	for _, d := range details {
		family := detailFamilyDTO{
			Family:    d.Family,
			Available: d.Available,
			Reason:    d.Reason,
			ReadAt:    formatTime(d.ReadAtUTC),
			Data:      d.Data,
		}
		if d.Family == model.FamilyInverter && d.Available {
			family.Data = inverterDetailPayload(d.Data)
		}
		if family.ReadAt > newest {
			newest = family.ReadAt
		}
		out.Families = append(out.Families, family)
	}
	if newest != "" {
		out.ReadAt = &newest
	}
	s.writeData(w, http.StatusOK, out, s.baseMeta())
}

// inverterDetailPayload turns the stored inverter snapshot into what the
// interface consumes: the measurements as an ORDERED list, each with its label.
//
// The stored form is a map because that is how the device answers; the display
// order and the labels belong to the boundary, not to the storage. A payload
// that cannot be read is passed through untouched: the reader then sees what was
// stored instead of an invented shape.
func inverterDetailPayload(stored json.RawMessage) json.RawMessage {
	var detail model.InverterDetail
	if err := json.Unmarshal(stored, &detail); err != nil {
		return stored
	}

	measurements := make([]detailMeasurementDTO, 0, len(detail.Measurements))
	for _, key := range detail.MeasuredKeys() {
		m := detail.Measurements[key]
		label, ok := measurementLabels[key]
		if !ok {
			label = key
		}
		measurements = append(measurements, detailMeasurementDTO{
			Key: key, Label: label, Unit: m.Unit, Value: m.Value,
		})
	}

	body, err := json.Marshal(inverterDetailDTO{
		State:        detail.State,
		Measurements: measurements,
		Energies:     detail.Energies,
	})
	if err != nil {
		return stored
	}
	return body
}
