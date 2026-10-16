package model

// Unit conversions for the API and the interface.
//
// The database and the domain model stay in base SI units (W, Wh): the
// conversions are only used at the display boundary. No conversion is applied
// during acquisition, so as not to introduce a cumulative rounding error in
// the history.

// WhToKWh converts watt-hours to kilowatt-hours.
func WhToKWh(v float64) float64 { return v / 1000 }

// WToKW converts watts to kilowatts.
func WToKW(v float64) float64 { return v / 1000 }
