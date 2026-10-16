package model

// Conversions d'unités pour l'API et l'interface.
//
// La base et le modèle domaine restent en unités SI de base (W, Wh) : les
// conversions ne servent qu'à la frontière d'affichage. Aucune conversion
// n'est appliquée lors de l'acquisition, pour ne pas introduire d'erreur
// d'arrondi cumulative dans l'historique.

// WhToKWh convertit des watts-heure en kilowatts-heure.
func WhToKWh(v float64) float64 { return v / 1000 }

// WToKW convertit des watts en kilowatts.
func WToKW(v float64) float64 { return v / 1000 }
