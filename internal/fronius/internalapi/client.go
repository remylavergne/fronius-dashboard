// Client des chemins internes du Webinterface Fronius, en lecture seule.
//
// Le seul constructeur de requête est Get : le verbe est la constante
// http.MethodGet, aucun paramètre de méthode n'existe dans cette API. Tout
// chemin passe d'abord par la liste blanche (allowlist.go), qui contient
// exactement quatre entrées ; un chemin absent est refusé avant toute
// construction d'URL.
package internalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/froniuserr"
)

// maxBodyBytes borne la lecture : ces réponses font quelques kilo-octets.
const maxBodyBytes = 1 << 20 // 1 MiB

// Client interroge les chemins internes autorisés.
type Client struct {
	base    *url.URL
	doer    fronius.Doer
	timeout time.Duration
	log     *slog.Logger
}

// New construit un client.
//
// doer porte les garanties de sécurité de l'appelant : en pratique
// fronius.ReadOnly{Inner: digest.Transport{…}}, donc GET uniquement, et
// authentification seulement en réponse à un 401.
func New(baseURL string, doer fronius.Doer, timeout time.Duration, log *slog.Logger) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("URL de base %q: %w", baseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL de base %q : schéma http(s) attendu", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL de base %q : hôte manquant", baseURL)
	}
	if doer == nil {
		doer = &http.Client{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{base: u, doer: doer, timeout: timeout, log: log}, nil
}

// Get lit un chemin de la liste blanche et retourne le corps brut.
//
// Aucun décodage n'est fait ici : l'enveloppe dépend de l'endpoint
// (EnvelopeDefault ou EnvelopeCgi) et est traitée par les méthodes typées.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	ep, ok := Lookup(path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	return c.get(ctx, ep)
}

func (c *Client) get(ctx context.Context, ep Endpoint) ([]byte, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	endpointURL := c.base.JoinPath(ep.Path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL.String(), nil)
	if err != nil {
		return nil, &froniuserr.Error{Kind: froniuserr.KindTransport, Endpoint: ep.Path, Err: err}
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, ep.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fronius.ClassifyTransport(ctx, ep.Path, err)
	}

	c.log.Debug("api interne",
		"internal_path", ep.Path,
		"status", resp.StatusCode,
		"bytes", len(body),
		"duration_ms", time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return nil, fronius.ClassifyStatus(ep.Path, resp.StatusCode)
	}
	if len(body) > maxBodyBytes {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: ep.Path,
			Err:      fmt.Errorf("corps de %d octets : dépasse la limite de %d MiB", len(body), maxBodyBytes>>20),
		}
	}
	return body, nil
}

// ---------------------------------------------------------------------------
// Identité
// ---------------------------------------------------------------------------

// Version est l'identité de l'appareil, telle qu'extraite de /status/version.
//
// Chaque champ est un pointeur : une clé absente ou vide reste nil, jamais "".
// Un pointeur nil signifie « non trouvé dans la réponse », pas « inexistant sur
// l'appareil ».
type Version struct {
	SerialNumber   *string
	Model          *string
	Firmware       *string
	DeviceGroup    *string
	NumberOfPhases *int
}

// Clés candidates, par ordre de priorité. La première clé non vide gagne.
//
// Seuls `serialNumber`, `numberOfPhases` et `devicegroup` sont établis par le
// bundle (docs/SOURCES.md) ; les autres sont des candidats assumés, à confirmer
// par une capture matérielle (cmd/fronius-probe). Ajouter une clé observée en
// tête de la liste correspondante suffit alors à la prendre en compte.
var (
	serialNumberKeys = []string{"serialNumber", "serial", "serials"}
	modelKeys        = []string{"modelName", "model", "deviceModel", "inverterModel", "devicegroup"}
	firmwareKeys     = []string{"firmwareVersion", "firmwareversion", "firmware", "swVersion", "version"}
	deviceGroupKeys  = []string{"devicegroup", "deviceGroup"}
	phasesKeys       = []string{"numberOfPhases", "phases"}
)

// Version lit /status/version et en extrait l'identité connue.
func (c *Client) Version(ctx context.Context) (Version, error) {
	ep, ok := Lookup(VersionPath)
	if !ok {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAllowed, VersionPath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return Version{}, err
	}

	fields, err := decodeObject(ep.Path, body)
	if err != nil {
		return Version{}, err
	}

	return Version{
		SerialNumber:   stringField(fields, serialNumberKeys...),
		Model:          stringField(fields, modelKeys...),
		Firmware:       stringField(fields, firmwareKeys...),
		DeviceGroup:    stringField(fields, deviceGroupKeys...),
		NumberOfPhases: intField(fields, phasesKeys...),
	}, nil
}

// ---------------------------------------------------------------------------
// Présence d'un composant
// ---------------------------------------------------------------------------

// ComponentPresent indique si un composant répond.
//
// Règle d'enveloppe `Cgi` : Body.Data non vide ⇒ présent, Body.Data vide ou
// absent ⇒ absent. Un corps illisible est une ERREUR (KindDecode), jamais une
// absence : une réponse qu'on ne sait pas lire ne prouve rien.
func (c *Client) ComponentPresent(ctx context.Context, path string) (bool, error) {
	ep, ok := Lookup(path)
	if !ok {
		return false, fmt.Errorf("%w: %q", ErrNotAllowed, path)
	}
	if ep.Envelope != EnvelopeCgi {
		return false, fmt.Errorf("internalapi: %q n'est pas un composant (enveloppe %q)", path, ep.Envelope)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return false, err
	}

	data, err := decodeData(ep.Path, body)
	if err != nil {
		return false, err
	}
	return len(data) > 0, nil
}

// ---------------------------------------------------------------------------
// Événements actifs
// ---------------------------------------------------------------------------

// ActiveEvents lit /status/activeEvents.
//
// La forme du corps n'est pas établie (docs/UNKNOWNS.md) : les événements sont
// rendus en JSON brut, dans un ordre stable (clés triées pour un objet indexé,
// ordre du tableau pour une liste racine). L'appelant décide de ce qu'il en
// tire, et le détail brut est conservé.
func (c *Client) ActiveEvents(ctx context.Context) ([]json.RawMessage, error) {
	ep, ok := Lookup(ActiveEventsPath)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, ActiveEventsPath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return nil, err
	}

	// Objet indexé par identifiant d'événement (forme attendue). Un Data vide
	// ou nul signifie « aucun événement », ce n'est pas une erreur.
	if data, err := decodeData(ep.Path, body); err == nil {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]json.RawMessage, 0, len(keys))
		for _, k := range keys {
			out = append(out, data[k])
		}
		return out, nil
	}

	// Repli : tableau racine.
	var list []json.RawMessage
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}

	return nil, &froniuserr.Error{
		Kind:     froniuserr.KindDecode,
		Endpoint: ep.Path,
		Err:      fmt.Errorf("forme inattendue : ni Body.Data indexé, ni tableau racine"),
	}
}

// ---------------------------------------------------------------------------
// Décodage
// ---------------------------------------------------------------------------

// decodeObject décode un corps en objet JSON.
//
// Les clés inconnues sont conservées : c'est l'extraction par liste de clés
// candidates qui décide, jamais un nom unique supposé.
func decodeObject(endpoint string, body []byte) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, jsonDecodeError(endpoint, err)
	}
	if fields == nil {
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: endpoint,
			Err:      fmt.Errorf("objet JSON attendu, reçu null"),
		}
	}
	return fields, nil
}

// decodeData retourne Body.Data d'une enveloppe `Cgi`.
func decodeData(endpoint string, body []byte) (map[string]json.RawMessage, error) {
	fields, err := decodeObject(endpoint, body)
	if err != nil {
		return nil, err
	}
	rawBody, ok := fields["Body"]
	if !ok {
		// Une enveloppe sans Body est illisible pour nous : ce n'est pas une
		// absence de composant.
		return nil, &froniuserr.Error{
			Kind:     froniuserr.KindDecode,
			Endpoint: endpoint,
			Err:      fmt.Errorf("champ Body absent de la réponse"),
		}
	}
	var wrapper struct {
		Data map[string]json.RawMessage `json:"Data"`
	}
	if err := json.Unmarshal(rawBody, &wrapper); err != nil {
		return nil, jsonDecodeError(endpoint, err)
	}
	return wrapper.Data, nil
}

func jsonDecodeError(endpoint string, err error) error {
	return &froniuserr.Error{Kind: froniuserr.KindDecode, Endpoint: endpoint, Err: err}
}

// stringField retourne la première clé candidate dont la valeur est une chaîne
// non vide, insensible à la casse des clés.
func stringField(fields map[string]json.RawMessage, keys ...string) *string {
	for _, key := range keys {
		raw, ok := lookupFold(fields, key)
		if !ok {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if strings.TrimSpace(v) == "" {
			continue
		}
		return new(v)
	}
	return nil
}

// intField retourne la première clé candidate dont la valeur est un entier.
//
// Un nombre sérialisé en chaîne (« 3 ») est accepté : la forme réelle n'est pas
// établie pour numberOfPhases sur tous les firmwares.
func intField(fields map[string]json.RawMessage, keys ...string) *int {
	for _, key := range keys {
		raw, ok := lookupFold(fields, key)
		if !ok {
			continue
		}
		var n int
		if err := json.Unmarshal(raw, &n); err == nil {
			return new(n)
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if parsed, err := parseLenientInt(s); err == nil {
				return new(parsed)
			}
		}
	}
	return nil
}

func parseLenientInt(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

func lookupFold(fields map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if raw, ok := fields[key]; ok {
		return raw, true
	}
	for k, raw := range fields {
		if strings.EqualFold(k, key) {
			return raw, true
		}
	}
	return nil, false
}
