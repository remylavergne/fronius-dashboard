package model

import "time"

// Severity classe un événement local du collecteur.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Natures d'événements produites par la phase 1.
const (
	// EventCollectorError : échec de collecte (appareil injoignable, réponse
	// invalide, erreur applicative signalée par l'appareil).
	EventCollectorError = "collector_error"
	// EventCollectorRecovered : reprise après une série d'échecs.
	EventCollectorRecovered = "collector_recovered"
	// EventStartup : démarrage du collecteur.
	EventStartup = "collector_started"
	// EventPurge : purge de rétention exécutée.
	EventPurge = "retention_purge"
)

// Event est un événement local, conservé dans la base et exposé par
// /api/v1/events. Aucun événement ne provient de l'onduleur en phase 1 :
// les événements matériels exigent l'api interne (phase 2).
type Event struct {
	TS       time.Time
	Kind     string
	Severity Severity
	Message  string
	Detail   string
}
