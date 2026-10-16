// Simulateur local de la Fronius Solar API v1.
//
// La forme du JSON produite ici est une [HYPOTHÈSE] : le bundle de l'interface
// d'origine ne contient aucun chemin Solar API (voir docs/SOURCES.md), donc
// aucun schéma n'a pu être extrait du fabricant. Les fixtures générées doivent
// être réconciliées avec une capture réelle produite par cmd/fronius-probe.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

const (
	// Production nulle hors de cette plage horaire locale.
	dayStartHour = 7.0
	dayEndHour   = 19.0

	// Périodes du bruit de nébulosité lissé.
	cloudPeriod1 = 37 * time.Minute
	cloudPeriod2 = 113 * time.Minute

	// Pas d'intégration de la courbe de puissance.
	integrationStep = time.Minute

	// Date (locale) à partir de laquelle E_Total est cumulé. Constante du
	// simulateur : la date d'installation réelle de l'onduleur n'est exposée
	// par aucun endpoint confirmé.
	installYear = 2025
)

// generator produit une courbe de production déterministe.
//
//	p(t) = max(0, sin(π·(h−7)/12)) · nominal · cloud(t),  h = heure locale décimale
//
// cloud(t) ∈ [0.75, 1.0] est la somme de deux sinusoïdes de périodes 37 min et
// 113 min, dont les phases dérivent du seed. Aucun appel à math/rand : la même
// entrée produit toujours le même octet de sortie.
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
		// Deux périodes premières distinctes pour décorréler les phases.
		phase1: 2 * math.Pi * float64(seed%977) / 977,
		phase2: 2 * math.Pi * float64((seed/977)%991) / 991,
		// Énergie fictive produite avant installDate, pour que E_Total ne
		// démarre pas à zéro : 1 000 kWh + seed%100 · 1 kWh.
		baseTotalWh: 1_000_000 + float64(seed%100)*1_000,
		installDate: time.Date(installYear, 1, 1, 0, 0, 0, 0, loc),
		dayCache:    map[string]float64{},
	}
}

// cloudAt retourne le facteur de nébulosité, dans [0.75, 1.0].
func (g *generator) cloudAt(t time.Time) float64 {
	u := float64(t.UTC().UnixNano()) / 1e9
	a := 2*math.Pi*u/cloudPeriod1.Seconds() + g.phase1
	b := 2*math.Pi*u/cloudPeriod2.Seconds() + g.phase2
	return 0.875 + 0.125*(0.5*math.Sin(a)+0.5*math.Sin(b))
}

// instantaneousW retourne la puissance PV en W à l'instant t.
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

// solarSecondsBetween intègre la puissance sur [from, to] par la méthode du
// point milieu (pas de 60 s, dernier pas tronqué) et retourne des Wh.
func (g *generator) solarSecondsBetween(from, to time.Time) float64 {
	if !to.After(from) {
		return 0
	}
	var ws float64 // watts-seconde
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

// dayEnergyWh retourne la production d'une journée locale complète (23 h, 24 h
// ou 25 h selon le changement d'heure).
func (g *generator) dayEnergyWh(date time.Time) float64 {
	key := date.Format("2006-01-02")
	if v, ok := g.dayCache[key]; ok {
		return v
	}
	v := g.solarSecondsBetween(date, g.nextDay(date))
	g.dayCache[key] = v
	return v
}

// sumDaysWh somme les journées locales de [from, to[.
func (g *generator) sumDaysWh(from, to time.Time) float64 {
	var total float64
	for d := from; d.Before(to); d = g.nextDay(d) {
		total += g.dayEnergyWh(d)
	}
	return total
}

// snapshot est l'ensemble des valeurs dérivées d'un instant.
type snapshot struct {
	PowerW   float64
	EDayWh   float64
	EYearWh  float64
	ETotalWh float64
}

// snapshotAt calcule les compteurs d'énergie cohérents avec l'instant t.
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
// Forme JSON supposée de GetPowerFlowRealtimeData.fcgi
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

type siteJSON struct {
	Mode                 string   `json:"Mode"`
	PPV                  *float64 `json:"P_PV"`
	PGrid                *float64 `json:"P_Grid"`
	PLoad                *float64 `json:"P_Load"`
	PAkku                *float64 `json:"P_Akku"`
	EDay                 *float64 `json:"E_Day"`
	EYear                *float64 `json:"E_Year"`
	ETotal               *float64 `json:"E_Total"`
	RelAutonomy          *float64 `json:"rel_Autonomy"`
	RelSelfConsumption   *float64 `json:"rel_SelfConsumption"`
	MeterLocationCurrent *int     `json:"Meter_Location_Current"`
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

func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

// loadW modélise une consommation de base déterministe, utilisée uniquement
// lorsque --with-meter est actif.
func (g *generator) loadW(t time.Time) float64 {
	u := float64(t.UTC().UnixNano()) / 1e9
	// 400 W de veille + deux profils lents (17 min et 41 min), plage ~[180, 1100] W.
	v := 640 + 300*math.Sin(2*math.Pi*u/1020+g.phase2) + 160*math.Sin(2*math.Pi*u/2460+g.phase1)
	return round1(math.Max(120, v))
}

// buildEnvelope construit la réponse pour l'instant t.
func (g *generator) buildEnvelope(t time.Time, withMeter bool, statusCode int, reason string) envelope {
	snap := g.snapshotAt(t)

	site := siteJSON{
		Mode:                 "produce-only",
		PPV:                  fptr(snap.PowerW),
		EDay:                 fptr(snap.EDayWh),
		EYear:                fptr(snap.EYearWh),
		ETotal:               fptr(snap.ETotalWh),
		MeterLocationCurrent: iptr(0),
	}

	if withMeter {
		pv := snap.PowerW
		load := g.loadW(t)
		grid := round1(pv - load)

		selfConsumed := math.Min(pv, load)
		site.PLoad = fptr(load)
		site.PGrid = fptr(grid)
		// Un ratio dont le dénominateur est nul est indéfini : on émet null
		// plutôt qu'un zéro inventé, conformément à l'invariant du modèle.
		if pv > 0 {
			site.RelSelfConsumption = fptr(round1(100 * selfConsumed / pv))
		}
		if load > 0 {
			site.RelAutonomy = fptr(round1(100 * selfConsumed / load))
		}
		// P_Akku reste null : le simulateur ne modélise aucune batterie.
	}

	inv := inverterJSON{
		DT:     1,
		P:      fptr(snap.PowerW),
		EDay:   fptr(snap.EDayWh),
		EYear:  fptr(snap.EYearWh),
		ETotal: fptr(snap.ETotalWh),
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

// marshalEnvelope sérialise l'enveloppe de façon stable (indentation fixe,
// ordre des champs fixe, une seule clé d'onduleur).
func marshalEnvelope(env envelope) ([]byte, error) {
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}
	return append(b, '\n'), nil
}
