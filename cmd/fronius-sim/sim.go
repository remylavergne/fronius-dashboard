// Local simulator of the Fronius Solar API v1.
//
// The shape of the JSON produced here is a [HYPOTHESIS]: the bundle of the
// original interface contains no Solar API path (see docs/SOURCES.md), so
// no schema could be extracted from the manufacturer. The generated fixtures
// must be reconciled with a real capture produced by cmd/fronius-probe.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

const (
	// Zero production outside this local time range.
	dayStartHour = 7.0
	dayEndHour   = 19.0

	// Periods of the smoothed cloudiness noise.
	cloudPeriod1 = 37 * time.Minute
	cloudPeriod2 = 113 * time.Minute

	// Integration step of the power curve.
	integrationStep = time.Minute

	// Date (local) from which E_Total is accumulated. Simulator constant:
	// the real installation date of the inverter is exposed
	// by no confirmed endpoint.
	installYear = 2025
)

// generator produces a deterministic production curve.
//
//	p(t) = max(0, sin(π·(h−7)/12)) · nominal · cloud(t),  h = local decimal hour
//
// cloud(t) ∈ [0.75, 1.0] is the sum of two sinusoids with periods 37 min and
// 113 min, whose phases derive from the seed. No call to math/rand: the same
// input always produces the same output byte.
type generator struct {
	nominal float64
	seed    uint64
	loc     *time.Location

	phase1, phase2 float64
	baseTotalWh    float64
	installDate    time.Time

	dayCache map[string]float64
}

func newGenerator(seed uint64, nominalW float64, loc *time.Location) *generator {
	return &generator{
		nominal: nominalW,
		seed:    seed,
		loc:     loc,
		// Two distinct prime periods to decorrelate the phases.
		phase1: 2 * math.Pi * float64(seed%977) / 977,
		phase2: 2 * math.Pi * float64((seed/977)%991) / 991,
		// Fictitious energy produced before installDate, so that E_Total does
		// not start at zero: 1,000 kWh + seed%100 · 1 kWh.
		baseTotalWh: 1_000_000 + float64(seed%100)*1_000,
		installDate: time.Date(installYear, 1, 1, 0, 0, 0, 0, loc),
		dayCache:    map[string]float64{},
	}
}

// cloudAt returns the cloudiness factor, in [0.75, 1.0].
func (g *generator) cloudAt(t time.Time) float64 {
	u := float64(t.UTC().UnixNano()) / 1e9
	a := 2*math.Pi*u/cloudPeriod1.Seconds() + g.phase1
	b := 2*math.Pi*u/cloudPeriod2.Seconds() + g.phase2
	return 0.875 + 0.125*(0.5*math.Sin(a)+0.5*math.Sin(b))
}

// instantaneousW returns the PV power in W at instant t.
func (g *generator) instantaneousW(t time.Time) float64 {
	lt := t.In(g.loc)
	h := float64(lt.Hour()) + float64(lt.Minute())/60 + float64(lt.Second())/3600
	if h < dayStartHour || h > dayEndHour {
		return 0
	}
	shape := math.Sin(math.Pi * (h - dayStartHour) / 12)
	if shape <= 0 {
		return 0
	}
	return shape * g.nominal * g.cloudAt(t)
}

// solarSecondsBetween integrates the power over [from, to] by the midpoint
// method (step of 60 s, last step truncated) and returns Wh.
func (g *generator) solarSecondsBetween(from, to time.Time) float64 {
	if !to.After(from) {
		return 0
	}
	var ws float64 // watt-seconds
	cur := from
	for {
		next := cur.Add(integrationStep)
		if next.After(to) {
			rem := to.Sub(cur)
			if rem > 0 {
				ws += g.instantaneousW(cur.Add(rem/2)) * rem.Seconds()
			}
			break
		}
		ws += g.instantaneousW(cur.Add(integrationStep/2)) * integrationStep.Seconds()
		cur = next
	}
	return ws / 3600
}

func (g *generator) dateOnly(t time.Time) time.Time {
	lt := t.In(g.loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, g.loc)
}

func (g *generator) nextDay(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, g.loc)
}

// dayEnergyWh returns the production of a complete local day (23 h, 24 h
// or 25 h depending on the daylight-saving change).
func (g *generator) dayEnergyWh(date time.Time) float64 {
	key := date.Format("2006-01-02")
	if v, ok := g.dayCache[key]; ok {
		return v
	}
	v := g.solarSecondsBetween(date, g.nextDay(date))
	g.dayCache[key] = v
	return v
}

// sumDaysWh sums the local days of [from, to[.
func (g *generator) sumDaysWh(from, to time.Time) float64 {
	var total float64
	for d := from; d.Before(to); d = g.nextDay(d) {
		total += g.dayEnergyWh(d)
	}
	return total
}

// snapshot is the set of values derived from an instant.
type snapshot struct {
	PowerW   float64
	EDayWh   float64
	EYearWh  float64
	ETotalWh float64
}

// snapshotAt computes the energy counters consistent with instant t.
func (g *generator) snapshotAt(t time.Time) snapshot {
	day := g.dateOnly(t)
	yearStart := time.Date(day.Year(), 1, 1, 0, 0, 0, 0, g.loc)

	eDay := g.solarSecondsBetween(day, t)
	eYear := g.sumDaysWh(yearStart, day) + eDay
	eTotal := g.baseTotalWh + g.sumDaysWh(g.installDate, day) + eDay

	return snapshot{
		PowerW:   round1(g.instantaneousW(t)),
		EDayWh:   round1(eDay),
		EYearWh:  round1(eYear),
		ETotalWh: round1(eTotal),
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// ---------------------------------------------------------------------------
// Assumed JSON shape of GetPowerFlowRealtimeData.fcgi
// ---------------------------------------------------------------------------

type envelope struct {
	Body bodyJSON `json:"Body"`
	Head headJSON `json:"Head"`
}

type bodyJSON struct {
	Data dataJSON `json:"Data"`
}

type dataJSON struct {
	Site      siteJSON                `json:"Site"`
	Inverters map[string]inverterJSON `json:"Inverters"`
}

// unknownMeterLocation is the value firmware 1.19.7-1 reports in
// `Meter_Location` when no meter is configured (captured 2026-10-16).
const unknownMeterLocation = "unknown"

type siteJSON struct {
	Mode               string   `json:"Mode"`
	PPV                *float64 `json:"P_PV"`
	PGrid              *float64 `json:"P_Grid"`
	PLoad              *float64 `json:"P_Load"`
	PAkku              *float64 `json:"P_Akku"`
	EDay               *float64 `json:"E_Day"`
	EYear              *float64 `json:"E_Year"`
	ETotal             *float64 `json:"E_Total"`
	RelAutonomy        *float64 `json:"rel_Autonomy"`
	RelSelfConsumption *float64 `json:"rel_SelfConsumption"`
	MeterLocation      *string  `json:"Meter_Location"`
}

type inverterJSON struct {
	DT     int      `json:"DT"`
	P      *float64 `json:"P"`
	EDay   *float64 `json:"E_Day"`
	EYear  *float64 `json:"E_Year"`
	ETotal *float64 `json:"E_Total"`
}

type headJSON struct {
	RequestArguments map[string]any `json:"RequestArguments"`
	Status           statusJSON     `json:"Status"`
	Timestamp        string         `json:"Timestamp"`
}

type statusJSON struct {
	Code        int    `json:"Code"`
	Reason      string `json:"Reason"`
	UserMessage string `json:"UserMessage"`
}

// loadW models a deterministic base consumption, used only
// when --with-meter is active.
func (g *generator) loadW(t time.Time) float64 {
	u := float64(t.UTC().UnixNano()) / 1e9
	// 400 W of standby + two slow profiles (17 min and 41 min), range ~[180, 1100] W.
	v := 640 + 300*math.Sin(2*math.Pi*u/1020+g.phase2) + 160*math.Sin(2*math.Pi*u/2460+g.phase1)
	return round1(math.Max(120, v))
}

// buildEnvelope builds the response for instant t.
func (g *generator) buildEnvelope(t time.Time, withMeter bool, statusCode int, reason string) envelope {
	snap := g.snapshotAt(t)

	site := siteJSON{
		Mode:          "produce-only",
		PPV:           new(snap.PowerW),
		EDay:          new(snap.EDayWh),
		EYear:         new(snap.EYearWh),
		ETotal:        new(snap.ETotalWh),
		MeterLocation: new(unknownMeterLocation),
	}

	if withMeter {
		pv := snap.PowerW
		load := g.loadW(t)
		grid := round1(pv - load)

		selfConsumed := math.Min(pv, load)
		site.PLoad = new(load)
		site.PGrid = new(grid)
		// A ratio whose denominator is zero is undefined: we emit null
		// rather than an invented zero, in accordance with the model invariant.
		if pv > 0 {
			site.RelSelfConsumption = new(round1(100 * selfConsumed / pv))
		}
		if load > 0 {
			site.RelAutonomy = new(round1(100 * selfConsumed / load))
		}
		// P_Akku stays null: the simulator models no battery.
	}

	inv := inverterJSON{
		DT:     1,
		P:      new(snap.PowerW),
		EDay:   new(snap.EDayWh),
		EYear:  new(snap.EYearWh),
		ETotal: new(snap.ETotalWh),
	}

	return envelope{
		Body: bodyJSON{Data: dataJSON{
			Site:      site,
			Inverters: map[string]inverterJSON{"1": inv},
		}},
		Head: headJSON{
			RequestArguments: map[string]any{},
			Status:           statusJSON{Code: statusCode, Reason: reason, UserMessage: reason},
			Timestamp:        t.In(g.loc).Format(time.RFC3339),
		},
	}
}

// marshalEnvelope serializes the envelope in a stable way (fixed indentation,
// fixed field order, a single inverter key).
func marshalEnvelope(env envelope) ([]byte, error) {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return append(b, '\n'), nil
}
