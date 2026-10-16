// Package api expose l'API REST de consultation.
//
// Conventions de sérialisation (miroir à tenir avec apps/web/src/lib/schema.ts) :
//   - clés en snake_case ;
//   - unités SI de base (W, Wh, %) : la conversion en kW/kWh est faite par
//     l'interface, jamais ici ;
//   - une valeur absente est sérialisée en `null`, jamais en 0 ;
//   - les horodatages sont en UTC, à la milliseconde, suffixés `Z`.
//
// Aucun secret ne figure dans ces structures : la phase 1 n'en détient aucun.
package api

import (
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// apiTimeLayout : horodatage UTC à la milliseconde, lisible par JavaScript.
const apiTimeLayout = "2006-01-02T15:04:05.000Z"

// Codes d'erreur exposés par l'API.
const (
	CodeInvalidRange     = "invalid_range"
	CodeInvalidStep      = "invalid_step"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeInternal         = "internal"
	CodeNoData           = "no_data"
)

// envelope est l'enveloppe de toute réponse.
type envelope struct {
	Data  any       `json:"data"`
	Meta  *meta     `json:"meta,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

// meta accompagne chaque réponse ; les endpoints de mesure y ajoutent l'état de
// fraîcheur.
type meta struct {
	GeneratedAt string   `json:"generated_at"`
	SampledAt   *string  `json:"sampled_at,omitempty"`
	Stale       *bool    `json:"stale,omitempty"`
	StaleAfterS *float64 `json:"stale_after_s,omitempty"`
	// StepS est le pas effectivement appliqué par /api/v1/timeseries, en
	// secondes. Il est indispensable au client : sans lui, déduire le pas de
	// l'écart entre deux buckets renvoyés donne un résultat faux dès qu'un
	// bucket manque, et l'interface tracerait alors la courbe en travers du
	// trou — exactement l'interpolation qu'elle doit éviter.
	StepS *float64 `json:"step_s,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorEnvelope est la forme d'une réponse d'erreur.
//
// Elle ne porte pas de champ « data » : le contrat est exactement
// {"error":{"code":"…","message":"…"}}. L'enveloppe de succès, elle, expose
// toujours « data » (éventuellement null, cas no_data de /live).
type errorEnvelope struct {
	Error apiError `json:"error"`
}

// ---------------------------------------------------------------------------
// Mesures
// ---------------------------------------------------------------------------

type sampleDTO struct {
	TS                    string        `json:"ts"`
	TSSource              string        `json:"ts_source"`
	Source                string        `json:"source"`
	Quality               string        `json:"quality"`
	Mode                  string        `json:"mode"`
	PowerPVW              *float64      `json:"power_pv_w"`
	EnergyDayWh           *float64      `json:"energy_day_wh"`
	EnergyYearWh          *float64      `json:"energy_year_wh"`
	EnergyTotalWh         *float64      `json:"energy_total_wh"`
	PowerGridW            *float64      `json:"power_grid_w"`
	PowerLoadW            *float64      `json:"power_load_w"`
	PowerBatteryW         *float64      `json:"power_battery_w"`
	RelAutonomyPct        *float64      `json:"rel_autonomy_pct"`
	RelSelfConsumptionPct *float64      `json:"rel_self_consumption_pct"`
	Inverters             []inverterDTO `json:"inverters"`
}

type inverterDTO struct {
	ID          string   `json:"id"`
	DT          int      `json:"dt"`
	PowerW      *float64 `json:"power_w"`
	EnergyDayWh *float64 `json:"energy_day_wh"`
}

func sampleToDTO(sm model.Sample) sampleDTO {
	inverters := make([]inverterDTO, 0, len(sm.Inverters))
	for _, inv := range sm.Inverters {
		inverters = append(inverters, inverterDTO{
			ID:          inv.ID,
			DT:          inv.DT,
			PowerW:      inv.PowerW,
			EnergyDayWh: inv.EnergyDayWh,
		})
	}
	return sampleDTO{
		TS:                    formatTime(sm.TS),
		TSSource:              sm.TSSource,
		Source:                sm.Source,
		Quality:               string(sm.Quality),
		Mode:                  sm.Mode,
		PowerPVW:              sm.PowerPVW,
		EnergyDayWh:           sm.EnergyDayWh,
		EnergyYearWh:          sm.EnergyYearWh,
		EnergyTotalWh:         sm.EnergyTotalWh,
		PowerGridW:            sm.PowerGridW,
		PowerLoadW:            sm.PowerLoadW,
		PowerBatteryW:         sm.PowerBatteryW,
		RelAutonomyPct:        sm.RelAutonomyPct,
		RelSelfConsumptionPct: sm.RelSelfConsumptionPct,
		Inverters:             inverters,
	}
}

type bucketDTO struct {
	Start       string   `json:"start"`
	Count       int      `json:"count"`
	Missing     int      `json:"missing"`
	PowerPVAvgW *float64 `json:"power_pv_avg_w"`
	PowerPVMinW *float64 `json:"power_pv_min_w"`
	PowerPVMaxW *float64 `json:"power_pv_max_w"`
}

func bucketsToDTO(buckets []storage.Bucket) []bucketDTO {
	out := make([]bucketDTO, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, bucketDTO{
			Start:       formatTime(b.Start),
			Count:       b.Count,
			Missing:     b.Missing,
			PowerPVAvgW: b.PowerPVAvgW,
			PowerPVMinW: b.PowerPVMinW,
			PowerPVMaxW: b.PowerPVMaxW,
		})
	}
	return out
}

type dailyEnergyDTO struct {
	Date     string   `json:"date"`
	EnergyWh *float64 `json:"energy_wh"`
}

func dailyToDTO(daily []storage.DailyEnergy) []dailyEnergyDTO {
	out := make([]dailyEnergyDTO, 0, len(daily))
	for _, d := range daily {
		out = append(out, dailyEnergyDTO{Date: d.Date, EnergyWh: d.EnergyWh})
	}
	return out
}

type eventDTO struct {
	TS       string `json:"ts"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Detail   string `json:"detail"`
}

func eventsToDTO(events []model.Event) []eventDTO {
	out := make([]eventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, eventDTO{
			TS:       formatTime(e.TS),
			Kind:     e.Kind,
			Severity: string(e.Severity),
			Message:  e.Message,
			Detail:   e.Detail,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Capacités, santé, système
// ---------------------------------------------------------------------------

type capabilityDTO struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Available bool     `json:"available"`
	Quality   string   `json:"quality"`
	Requires  []string `json:"requires"`
	ReadOnly  bool     `json:"read_only"`
	Verified  bool     `json:"verified"`
	Evidence  []string `json:"evidence"`
	Reason    string   `json:"reason"`
}

func capabilitiesToDTO(caps []model.Capability) []capabilityDTO {
	out := make([]capabilityDTO, 0, len(caps))
	for _, c := range caps {
		out = append(out, capabilityDTO{
			ID:        c.ID,
			Label:     c.Label,
			Available: c.Available,
			Quality:   string(c.Quality),
			Requires:  nonNil(c.Requires),
			ReadOnly:  c.ReadOnly,
			Verified:  c.Verified,
			Evidence:  nonNil(c.Evidence),
			Reason:    c.Reason,
		})
	}
	return out
}

type healthDTO struct {
	Status    string             `json:"status"`
	Version   string             `json:"version"`
	UptimeS   int64              `json:"uptime_s"`
	DB        dbHealthDTO        `json:"db"`
	Collector collectorHealthDTO `json:"collector"`
}

type dbHealthDTO struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type collectorHealthDTO struct {
	LastSuccessUTC string `json:"last_success_utc"`
	LastError      string `json:"last_error"`
	LastRunMS      int64  `json:"last_run_ms"`
	SamplesTotal   int64  `json:"samples_total"`
}

// systemDTO décrit l'application de supervision. `device` restera null tant que
// l'adaptateur d'API interne n'existe pas : la Solar API ne fournit ni modèle,
// ni version de firmware, ni numéro de série (voir docs/UNKNOWNS.md).
type systemDTO struct {
	Version       string     `json:"version"`
	GoVersion     string     `json:"go_version"`
	SolarAPIURL   string     `json:"solarapi_url"`
	PollIntervalS float64    `json:"poll_interval_s"`
	TZ            string     `json:"tz"`
	DBPath        string     `json:"db_path"`
	RetentionDays int        `json:"retention_days"`
	APIAddr       string     `json:"api_addr"`
	StaleAfterS   float64    `json:"stale_after_s"`
	Device        *deviceDTO `json:"device"`
}

// deviceDTO restera absent de la phase 1 (aucune source HTTP confirmée).
type deviceDTO struct {
	Model        string `json:"model"`
	Firmware     string `json:"firmware"`
	SerialNumber string `json:"serial_number"`
	Source       string `json:"source"`
}

func formatTime(t time.Time) string {
	return t.UTC().Format(apiTimeLayout)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
