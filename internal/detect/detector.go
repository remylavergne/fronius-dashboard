// Package detect interroge périodiquement les chemins internes de l'appareil
// pour établir son identité, la présence d'un compteur et d'une batterie, et
// ses événements actifs.
//
// Invariants :
//   - trois états, jamais deux : « absent » est une conclusion, « non déterminé »
//     est l'absence de conclusion. Une erreur de lecture ne produit JAMAIS une
//     absence (voir docs/UNKNOWNS.md) ;
//   - la seule opération distante est un GET, via internalapi.Client ;
//   - aucun identifiant n'est écrit ni tracé : le détecteur ne détient qu'un
//     booléen « identifiants configurés » ;
//   - aucun trafic inutile : un 401 reçu sans identifiants configurés fait
//     abandonner le chemin concerné jusqu'au redémarrage.
package detect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fronius-supervisor/internal/fronius/froniuserr"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/model"
	"fronius-supervisor/internal/storage"
)

// Clés d'état persistées.
const (
	// StateLastReadUTC : horodatage UTC de la dernière détection aboutie.
	StateLastReadUTC = "detect_last_read_utc"
	// StateLastError : premier message d'erreur du dernier cycle, "" sinon.
	StateLastError = "detect_last_error"
)

// Motifs d'état inconnu, réutilisés tels quels par l'API et l'interface.
const (
	ReasonNoCredentials  = "identifiants non configurés (FRONIUS_DEVICE_USER / FRONIUS_DEVICE_PASSWORD)"
	ReasonUnauthorized   = "accès refusé (401) : identifiants absents ou invalides"
	ReasonEndpointAbsent = "endpoint absent sur ce firmware (404)"
	// ReasonNoReadYet : aucune lecture d'identité n'a encore abouti.
	ReasonNoReadYet = "aucune lecture d'identité n'a encore abouti"
	// deviceEventMessage est le message de repli d'un événement sans texte.
	deviceEventMessage = "Événement de l'appareil"
	// maxReasonLength borne la cause reprise d'une erreur dans un état.
	maxReasonLength = 200
	// maxDetailBytes borne le JSON brut conservé pour un événement.
	maxDetailBytes = 2048
)

// Reader est la source de détection. *internalapi.Client la satisfait.
type Reader interface {
	Version(ctx context.Context) (internalapi.Version, error)
	ComponentPresent(ctx context.Context, path string) (bool, error)
	ActiveEvents(ctx context.Context) ([]json.RawMessage, error)
}

// Sink persiste les effets d'une détection. *storage.Store la satisfait.
type Sink interface {
	SaveDevice(ctx context.Context, d storage.DeviceRecord) error
	MarkDeviceEventSeen(ctx context.Context, key string, seenAt time.Time) (bool, error)
	InsertEvent(ctx context.Context, e model.Event) error
	SetState(ctx context.Context, k, v string) error
}

// Detector interroge les chemins internes et persiste le résultat.
type Detector struct {
	Source Reader
	Sink   Sink
	Log    *slog.Logger
	// Interval est la période entre deux détections. Le client officiel cadence
	// ces chemins à 5 000 ms ; la valeur retenue est bien plus longue, car une
	// identité et une présence changent rarement.
	Interval time.Duration
	// CredentialsConfigured indique que des identifiants sont disponibles. Il
	// sert uniquement à distinguer « l'appareil exige une authentification »
	// de « aucun identifiant n'a été fourni ».
	CredentialsConfigured bool
	// Now permet d'injecter une horloge (tests).
	Now func() time.Time

	mu sync.Mutex
	// abandonded mémorise les chemins sur lesquels un 401 a été reçu alors
	// qu'aucun identifiant n'est configuré : ils ne sont plus interrogés jusqu'au
	// redémarrage, et leur état reste « non déterminé ».
	abandoned map[string]bool
}

// Run détecte jusqu'à l'annulation du contexte, puis retourne nil.
//
// Une erreur d'un cycle n'arrête jamais la boucle : l'échec d'une lecture est un
// état, pas une panne du service.
func (d *Detector) Run(ctx context.Context) error {
	d.log().Info("détecteur démarré",
		"interval", d.Interval,
		"credentials", d.CredentialsConfigured)

	for {
		if _, err := d.DetectOnce(ctx); err != nil {
			d.log().Warn("détection en erreur", "error", err)
		}
		if ctx.Err() != nil {
			return nil
		}

		timer := time.NewTimer(d.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// DetectOnce exécute un cycle de détection complet.
//
// L'erreur retournée ne concerne que la persistance : les lectures qui échouent
// sont traduites en états « non déterminé », jamais en erreur de cycle.
func (d *Detector) DetectOnce(ctx context.Context) (model.Detection, error) {
	now := d.now()
	var detection model.Detection

	identity, version := d.readIdentity(ctx)
	detection.Identity = identity

	detection.Meter = d.readComponent(ctx, internalapi.PowerMeterPath)
	detection.Battery = d.readComponent(ctx, internalapi.BatteryPath)

	eventsDetection, eventsErr := d.readEvents(ctx, now)
	detection.DeviceEvents = eventsDetection

	record := storage.DeviceRecord{
		ReadAtUTC:          now,
		Source:             internalapi.VersionPath,
		IdentityState:      detection.Identity.State,
		IdentityReason:     detection.Identity.Reason,
		MeterState:         detection.Meter.State,
		MeterSource:        internalapi.PowerMeterPath,
		MeterReason:        detection.Meter.Reason,
		BatteryState:       detection.Battery.State,
		BatterySource:      internalapi.BatteryPath,
		BatteryReason:      detection.Battery.Reason,
		DeviceEventsState:  detection.DeviceEvents.State,
		DeviceEventsReason: detection.DeviceEvents.Reason,
	}
	if version != nil {
		record.Model = deref(version.Model)
		record.Firmware = deref(version.Firmware)
		record.SerialNumber = deref(version.SerialNumber)
	}

	var errs []error
	if eventsErr != nil {
		errs = append(errs, eventsErr)
	}
	if err := d.Sink.SaveDevice(ctx, record); err != nil {
		errs = append(errs, err)
	}
	if err := d.Sink.SetState(ctx, StateLastReadUTC, now.UTC().Format(time.RFC3339)); err != nil {
		errs = append(errs, err)
	}

	lastError := firstCause(detection)
	if err := d.Sink.SetState(ctx, StateLastError, lastError); err != nil {
		errs = append(errs, err)
	}

	return detection, errors.Join(errs...)
}

// readIdentity lit /status/version.
func (d *Detector) readIdentity(ctx context.Context) (model.ResourceDetection, *internalapi.Version) {
	const path = internalapi.VersionPath

	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}, nil
	}

	version, err := d.Source.Version(ctx)
	if err != nil {
		return d.failedDetection(ctx, path, err), nil
	}

	return model.ResourceDetection{State: model.StatePresent, Source: path}, &version
}

// readComponent lit la présence d'un composant.
func (d *Detector) readComponent(ctx context.Context, path string) model.ResourceDetection {
	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}
	}

	present, err := d.Source.ComponentPresent(ctx, path)
	if err != nil {
		return d.failedDetection(ctx, path, err)
	}
	if present {
		return model.ResourceDetection{State: model.StatePresent, Source: path}
	}
	// L'appareil a répondu et ne signale pas ce composant : c'est une absence
	// établie, et non un doute.
	return model.ResourceDetection{
		State:  model.StateAbsent,
		Source: path,
		Reason: "Body.Data vide : aucun composant indexé",
	}
}

// readEvents lit /status/activeEvents, déduplique et insère les événements neufs.
func (d *Detector) readEvents(ctx context.Context, now time.Time) (model.ResourceDetection, error) {
	const path = internalapi.ActiveEventsPath

	if reason, skip := d.abandonedReason(path); skip {
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}, nil
	}

	events, err := d.Source.ActiveEvents(ctx)
	if err != nil {
		return d.failedDetection(ctx, path, err), nil
	}

	detection := model.ResourceDetection{State: model.StatePresent, Source: path}

	var errs []error
	for _, raw := range events {
		key, err := eventKey(raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fresh, err := d.Sink.MarkDeviceEventSeen(ctx, key, now)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !fresh {
			// Déjà vu : aucune écriture, aucune duplication.
			continue
		}
		if err := d.Sink.InsertEvent(ctx, deviceEvent(now, raw)); err != nil {
			errs = append(errs, err)
		}
	}
	return detection, errors.Join(errs...)
}

// failedDetection traduit une erreur de lecture en état « non déterminé ».
func (d *Detector) failedDetection(ctx context.Context, path string, err error) model.ResourceDetection {
	kind, _ := froniuserr.KindOf(err)

	if kind == froniuserr.KindUnauthorized && !d.CredentialsConfigured {
		// Aucun identifiant : inutile de réémettre un 401 à chaque cycle.
		d.abandon(path)
		return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: ReasonNoCredentials}
	}

	var reason string
	switch kind {
	case froniuserr.KindUnauthorized:
		reason = ReasonUnauthorized
	case froniuserr.KindNotFound:
		reason = ReasonEndpointAbsent
	default:
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			reason = "arrêt demandé"
		} else {
			reason = truncate(err.Error(), maxReasonLength)
		}
	}
	return model.ResourceDetection{State: model.StateUnknown, Source: path, Reason: reason}
}

// abandonedReason indique si un chemin a été abandonné, et pourquoi.
func (d *Detector) abandonedReason(path string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.abandoned[path] {
		return ReasonNoCredentials, true
	}
	return "", false
}

func (d *Detector) abandon(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.abandoned == nil {
		d.abandoned = map[string]bool{}
	}
	d.abandoned[path] = true
}

// firstCause retourne le premier motif d'état non déterminé rencontré, dans
// l'ordre des lectures. C'est le message conservé dans detect_last_error.
func firstCause(d model.Detection) string {
	for _, r := range []model.ResourceDetection{d.Identity, d.Meter, d.Battery, d.DeviceEvents} {
		if r.State == model.StateUnknown && r.Reason != "" {
			return r.Source + " : " + r.Reason
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Événements de l'appareil
// ---------------------------------------------------------------------------

// eventKey fabrique la clé d'anti-duplication d'un événement brut.
//
// Priorité, établie par l'extraction du bundle (docs/ENDPOINTS.md) : `uuid` —
// l'identité d'instance du client officiel, qu'il compare pour détecter un
// changement d'état (l. 135742) et qu'il renvoie à `/commands/ConfirmEvent`
// (l. 135760) —, puis `prefix`-`eventID` (même identité, forme concaténée,
// l. 164699), puis `code`, puis `state`, puis l'empreinte SHA-256 du JSON brut.
//
// `code` est un code de *type* : seul, il fait collisionner deux événements
// actifs du même type, dont le second ne serait jamais notifié. D'où la priorité
// donnée aux identifiants d'instance. Conséquence assumée de ce changement
// d'ordre : un événement déjà vu sous une clé `code` est notifié une fois de plus
// après mise à jour, la première fois qu'il porte un `uuid`.
//
// Le préfixe « device: » évite toute collision avec une autre famille de clés.
func eventKey(raw json.RawMessage) (string, error) {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("événement de l'appareil illisible: %w", err)
	}
	if uuid := identifierField(fields, "uuid"); uuid != "" {
		return "device:" + uuid, nil
	}
	prefix, eventID := identifierField(fields, "prefix"), identifierField(fields, "eventID")
	if prefix != "" && eventID != "" {
		return "device:" + prefix + "-" + eventID, nil
	}
	for _, key := range []string{"code", "state"} {
		if v := identifierField(fields, key); v != "" {
			return "device:" + v, nil
		}
	}
	sum := sha256.Sum256(raw)
	return "device:" + hex.EncodeToString(sum[:])[:16], nil
}

// identifierField retourne un champ d'identité sous forme de chaîne, ou "".
//
// `uuid`, `code` et `state` sont des chaînes, mais `eventID` peut être un nombre :
// le client officiel concatène les deux formes sans distinction
// (`${t.prefix}-${t.eventID}`, l. 164699). Un nombre entier est donc rendu tel
// quel ; toute autre forme est ignorée plutôt que devinée.
func identifierField(fields map[string]any, key string) string {
	switch v := fields[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

// deviceEvent construit l'événement persisté à partir du JSON brut.
//
// L'horodatage est celui du **collecteur**, pas celui de l'appareil : `timestamp`
// et `activeUntil` de la charge utile sont des secondes d'horloge appareil
// (l. 135694-135695), et cette horloge est précisément l'inconnue qui justifie
// `/commands/GetUTCTime` ; `TS` est l'axe de tri et de rétention de la base, il
// reste donc local. Les champs observés sans consommateur ici (`category`,
// `viewer`, `confirmable`, `activeUntil`, `sourceID`) restent intégralement dans
// `Detail`, qui conserve le JSON brut. Aucun filtrage sur `confirmable` n'est
// appliqué : masquer des événements demanderait une règle observée, pas une
// supposition.
func deviceEvent(now time.Time, raw json.RawMessage) model.Event {
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields) // la forme peut être inattendue : le détail brut reste conservé

	return model.Event{
		TS:       now.UTC(),
		Kind:     model.EventDevice,
		Severity: severityOf(fields),
		Message:  messageOf(fields),
		Detail:   truncate(string(raw), maxDetailBytes),
	}
}

// severityOf mappe la sévérité annoncée d'un événement.
//
// L'extraction du bundle établit la forme réelle : `severity` est un **entier**,
// `getEventLevel` (l. 135728-135733) mappant 1 → erreur, 2 → avertissement,
// 3 → information. Avant cette extraction, seules des chaînes étaient lues : un
// événement matériel réel était donc **toujours** classé « information ». Les
// libellés texte restent acceptés par tolérance (simulateur, fixtures) ainsi que
// `level` comme clé de secours, et la première clé interprétable gagne. Une
// valeur absente ou inconnue reste « information » : aucune gravité n'est
// inventée.
func severityOf(fields map[string]any) model.Severity {
	for _, key := range []string{"severity", "level"} {
		if s, ok := severityValue(fields[key]); ok {
			return s
		}
	}
	return model.SeverityInfo
}

// severityValue interprète une valeur de sévérité : entier de l'appareil, entier
// sérialisé en chaîne, ou libellé texte.
func severityValue(v any) (model.Severity, bool) {
	var text string
	switch t := v.(type) {
	case float64:
		if t != float64(int64(t)) {
			return "", false
		}
		switch int64(t) {
		case 1:
			return model.SeverityError, true
		case 2:
			return model.SeverityWarning, true
		case 3:
			return model.SeverityInfo, true
		}
		text = strconv.FormatInt(int64(t), 10)
	case string:
		text = strings.TrimSpace(t)
	default:
		return "", false
	}
	switch text {
	case "":
		return "", false
	case "1":
		return model.SeverityError, true
	case "2":
		return model.SeverityWarning, true
	case "3":
		return model.SeverityInfo, true
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "warn"):
		return model.SeverityWarning, true
	case strings.Contains(lower, "error"), strings.Contains(lower, "fault"), strings.Contains(lower, "alert"):
		return model.SeverityError, true
	case strings.Contains(lower, "info"), strings.Contains(lower, "notice"):
		return model.SeverityInfo, true
	}
	return "", false
}

// messageOf retourne le texte de l'événement, ou un libellé neutre.
//
// `label` vient en tête : le client officiel affiche un texte traduit à partir
// d'une clé de dictionnaire (`STATECODETRANSLATIONS.StateCodes.<prefix>-<eventID>`,
// l. 164702) et retombe sur `label` quand cette clé manque (l. 135654) — c'est
// donc le seul texte lisible présent dans la charge utile. Les candidats
// historiques restent en secours.
func messageOf(fields map[string]any) string {
	for _, key := range []string{"label", "message", "text", "description"} {
		if v, ok := fields[key].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return deviceEventMessage
}

// truncate coupe une chaîne à n octets sans casser le dernier caractère UTF-8.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (d *Detector) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Detector) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}
