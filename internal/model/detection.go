// Détection de la présence d'un composant ou d'une ressource.
//
// Trois états, et trois seulement : « absent » est une conclusion (l'appareil a
// répondu et le composant n'y est pas), « non déterminé » est l'absence de
// conclusion. Les confondre reviendrait à affirmer une absence sans preuve.
package model

// DetectionState est l'état d'une détection.
type DetectionState string

const (
	// StateUnknown : la lecture n'a pas abouti (identifiants absents, 401, 404,
	// panne réseau, corps illisible). Ce n'est PAS une absence.
	StateUnknown DetectionState = "unknown"
	// StateAbsent : l'appareil a répondu et ne signale pas ce composant.
	StateAbsent DetectionState = "absent"
	// StatePresent : l'appareil signale ce composant.
	StatePresent DetectionState = "present"
)

// ResourceDetection décrit l'état d'une détection, avec sa provenance.
type ResourceDetection struct {
	State DetectionState
	// Source : endpoint ou mécanisme ayant produit l'état, vide si inconnu.
	Source string
	// Reason : cause de l'état lorsque celui-ci n'est pas StatePresent.
	Reason string
}

// Detection regroupe les quatre détections de la phase 2.
type Detection struct {
	Identity     ResourceDetection
	Meter        ResourceDetection
	Battery      ResourceDetection
	DeviceEvents ResourceDetection
}
