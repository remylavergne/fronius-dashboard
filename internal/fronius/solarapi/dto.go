// Package solarapi implémente le client lecture seule de la Fronius Solar API v1.
//
// ATTENTION — provenance de la forme décrite ici. Le bundle JavaScript de
// l'interface d'origine ne contient AUCUN chemin Solar API : la chaîne
// « solar_api » n'y apparaît que pour l'endpoint de configuration
// « /config/solar_api ». La structure ci-dessous est donc une [HYPOTHÈSE]
// fondée sur la documentation publique de la Solar API v1 et sur des
// observations rapportées, pas sur une capture du firmware 1.19.7-1.
//
// Conséquences :
//   - le décodage est tolérant (aucun DisallowUnknownFields) : un firmware plus
//     récent peut ajouter des champs sans casser le client ;
//   - toute valeur absente reste nil, jamais 0 (voir internal/model) ;
//   - les fixtures de test/fixtures/solarapi sont synthétiques jusqu'à ce
//     qu'une capture réelle expurgée (cmd/fronius-probe) les remplace.
//
// Ce fichier est le SEUL endroit où la forme du JSON Fronius est encodée.
package solarapi

// PowerFlowEnvelope est la réponse de GetPowerFlowRealtimeData.fcgi.
type PowerFlowEnvelope struct {
	Body PowerFlowBody `json:"Body"`
	Head Head          `json:"Head"`
}

// PowerFlowBody enveloppe les données de mesure.
type PowerFlowBody struct {
	Data PowerFlowData `json:"Data"`
}

// PowerFlowData contient le site puis les onduleurs.
type PowerFlowData struct {
	Site      SiteData              `json:"Site"`
	Inverters map[string]InverterPF `json:"Inverters"`
}

// SiteData agrège le point de mesure du site.
//
// Noms de champs alignés sur les noms du protocole (P_PV, E_Day…) pour que
// toute divergence de tag saute aux yeux à la relecture.
type SiteData struct {
	Mode                   string   `json:"Mode"`
	P_PV                   *float64 `json:"P_PV"`
	P_Grid                 *float64 `json:"P_Grid"`
	P_Load                 *float64 `json:"P_Load"`
	P_Akku                 *float64 `json:"P_Akku"`
	E_Day                  *float64 `json:"E_Day"`
	E_Year                 *float64 `json:"E_Year"`
	E_Total                *float64 `json:"E_Total"`
	Rel_Autonomy           *float64 `json:"rel_Autonomy"`
	Rel_SelfConsumption    *float64 `json:"rel_SelfConsumption"`
	Meter_Location_Current *int     `json:"Meter_Location_Current"`
}

// InverterPF est la mesure d'un onduleur, indexée par son numéro (« 1 », « 2 »…).
type InverterPF struct {
	DT      int      `json:"DT"`
	P       *float64 `json:"P"`
	E_Day   *float64 `json:"E_Day"`
	E_Year  *float64 `json:"E_Year"`
	E_Total *float64 `json:"E_Total"`
}

// Head porte les métadonnées de la réponse.
type Head struct {
	RequestArguments map[string]any `json:"RequestArguments"`
	Status           HeadStatus     `json:"Status"`
	Timestamp        string         `json:"Timestamp"`
}

// HeadStatus est l'état applicatif signalé par l'appareil.
type HeadStatus struct {
	Code        int    `json:"Code"`
	Reason      string `json:"Reason"`
	UserMessage string `json:"UserMessage"`
}

// rawEnvelope sert uniquement à détecter l'absence de Body ou de Body.Data,
// indiscernable d'un objet vide après décodage dans une structure valeur.
type rawEnvelope struct {
	Body *struct {
		Data *PowerFlowData `json:"Data"`
	} `json:"Body"`
	Head Head `json:"Head"`
}
