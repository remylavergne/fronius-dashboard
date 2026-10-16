// Package api exposes the REST read API.
//
// Serialisation conventions (mirror to keep with apps/web/src/lib/schema.ts):
//   - snake_case keys;
//   - base SI units (W, Wh, %): the conversion to kW/kWh is done by
//     the interface, never here;
//   - an absent value is serialised as `null`, never as 0;
//   - timestamps are in UTC, to the millisecond, suffixed `Z`.
//
// No secret appears in these structures: the device credentials
// live in the secret.String type on the froniusd side, are never
// serialised, and none of these responses can return them (dedicated test).
package api

import (
	"time"

	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// apiTimeLayout: UTC timestamp to the millisecond, readable by JavaScript.
const apiTimeLayout = "2006-01-02T15:04:05.000Z"

// Error codes exposed by the API.
const (
	CodeInvalidRange     = "invalid_range"
	CodeInvalidStep      = "invalid_step"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeInternal         = "internal"
	CodeNoData           = "no_data"
)

// envelope is the envelope of any response.
type envelope struct {
	Data  any       `json:"data"`
	Meta  *meta     `json:"meta,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

// meta accompanies every response; the measurement endpoints add the freshness
// state to it.
type meta struct {
	GeneratedAt string   `json:"generated_at"`
	SampledAt   *string  `json:"sampled_at,omitempty"`
	Stale       *bool    `json:"stale,omitempty"`
	StaleAfterS *float64 `json:"stale_after_s,omitempty"`
	// StepS is the step actually applied by /api/v1/timeseries, in
	// seconds. It is indispensable to the client: without it, deducing the step from
	// the gap between two returned buckets gives a wrong result as soon as a
	// bucket is missing, and the interface would then draw the curve across the
	// hole — exactly the interpolation it must avoid.
	StepS *float64 `json:"step_s,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorEnvelope is the shape of an error response.
//
// It carries no "data" field: the contract is exactly
// {"error":{"code":"…","message":"…"}}. The success envelope, on the other
// hand, always exposes "data" (possibly null, the no_data case of /live).
type errorEnvelope struct {
	Error apiError `json:"error"`
}

// ---------------------------------------------------------------------------
// Measurements
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
	// EnergyDerived is filled by the endpoints that derive the day and year
	// energies; the other endpoints leave it nil.
	EnergyDerived *derivedEnergyDTO `json:"energy_derived,omitempty"`
}

type inverterDTO struct {
	ID          string   `json:"id"`
	DT          int      `json:"dt"`
	PowerW      *float64 `json:"power_w"`
	EnergyDayWh *float64 `json:"energy_day_wh"`
}

// derivedEnergyDTO carries the day and year energies COMPUTED from our own stored
// total energy, for the installations whose firmware does not publish the day and
// year counters (docs/UNKNOWNS.md #13).
//
// It is deliberately separate from the measured fields: a computed value never
// takes the place of a measurement. Each derivation carries its baseline and, when
// it could not be established, the reason — an absent value with a reason beats a
// wrong value.
type derivedEnergyDTO struct {
	From            string   `json:"from"`
	DayWh           *float64 `json:"day_wh"`
	DayBaselineUTC  string   `json:"day_baseline_utc"`
	DayReason       string   `json:"day_reason"`
	YearWh          *float64 `json:"year_wh"`
	YearBaselineUTC string   `json:"year_baseline_utc"`
	YearReason      string   `json:"year_reason"`
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
// Capabilities, health, system
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

// systemDTO describes the supervision application. `device` stays null as long
// as no identity read has succeeded: the Solar API provides neither model,
// nor firmware version, nor serial number (see docs/UNKNOWNS.md).
type systemDTO struct {
	Version       string        `json:"version"`
	GoVersion     string        `json:"go_version"`
	SolarAPIURL   string        `json:"solarapi_url"`
	PollIntervalS float64       `json:"poll_interval_s"`
	TZ            string        `json:"tz"`
	DBPath        string        `json:"db_path"`
	RetentionDays int           `json:"retention_days"`
	APIAddr       string        `json:"api_addr"`
	StaleAfterS   float64       `json:"stale_after_s"`
	Device        *deviceDTO    `json:"device"`
	Detection     *detectionDTO `json:"detection"`
}

// deviceDTO is the device identity as it was read.
//
// The three identity fields are nullable: an empty string in the database means
// "not found in the response" and becomes `null`, never "" nor "unknown".
type deviceDTO struct {
	Model         *string `json:"model"`
	DeviceName    *string `json:"device_name"`
	ArticleNumber *string `json:"article_number"`
	Firmware      *string `json:"firmware"`
	SerialNumber  *string `json:"serial_number"`
	Source        string  `json:"source"`
	ReadAtUTC     string  `json:"read_at_utc"`
}

// detectionDTO exposes the state of the four internal detections.
type detectionDTO struct {
	ReadAtUTC    *string              `json:"read_at_utc"`
	Meter        detectionResourceDTO `json:"meter"`
	Battery      detectionResourceDTO `json:"battery"`
	InternalAPI  detectionResourceDTO `json:"internal_api"`
	DeviceEvents detectionResourceDTO `json:"device_events"`
	LastError    string               `json:"last_error"`
}

// detectionResourceDTO is the state of one detection: unknown, absent or present.
type detectionResourceDTO struct {
	State  string `json:"state"`
	Source string `json:"source"`
	Reason string `json:"reason"`
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
