package model

import "time"

// Severity classe un événement local du collecteur.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Natures d'événements : les unes produites localement par le collecteur, les
// autres republiées depuis l'appareil.
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
	// EventDevice : événement signalé par l'appareil lui-même (/status/activeEvents).
	// Sa déduplication repose sur device_events_seen (internal/storage) : le flux
	// est republié en entier à chaque interrogation.
	EventDevice = "device_event"
)

// Event est un événement local, conservé dans la base et exposé par
// /api/v1/events. Les événements matériels (EventDevice) proviennent de
// /status/activeEvents ; les autres sont produits localement par le collecteur.
type Event struct {
	TS       time.Time
	Kind     string
	Severity Severity
	Message  string
	Detail   string
}
