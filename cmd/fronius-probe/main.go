// Command fronius-probe effectue une requête de diagnostic en LECTURE SEULE
// vers un onduleur Fronius, et écrit le corps expurgé sur demande.
//
// Ce binaire est destiné à être exécuté par l'utilisateur, sur son propre
// matériel, après lecture de docs/PROBE-READONLY.md. Il n'est jamais lancé par
// l'agent ni par la CI.
//
// Garanties :
//   - liste blanche de cinq endpoints, en GET uniquement, sélectionnables par
//     --endpoint ; tout autre chemin est refusé avant construction d'URL ;
//   - refus d'une adresse non locale sans --allow-non-loopback explicite ;
//   - aucune écriture vers l'appareil : aucune méthode non-GET n'est construite,
//     et la garde fronius.ReadOnly rejette toute requête qui ne serait pas un GET ;
//   - expurgation du corps et des en-têtes avant toute écriture sur disque ;
//   - aucun mot de passe en ligne de commande : les identifiants viennent de
//     l'environnement (FRONIUS_DEVICE_USER / FRONIUS_DEVICE_PASSWORD) ou, pour
//     le mot de passe, de l'entrée standard (--password-stdin) — un argument
//     finirait dans l'historique du shell et dans la table des processus.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fronius-supervisor/internal/config"
	"fronius-supervisor/internal/fronius"
	"fronius-supervisor/internal/fronius/digest"
	"fronius-supervisor/internal/fronius/internalapi"
	"fronius-supervisor/internal/fronius/solarapi"
	"fronius-supervisor/internal/secret"
)

// errNotWhitelisted : la sonde ne peut pas viser une URL arbitraire.
var errNotWhitelisted = errors.New("endpoint hors liste blanche")

// maxBodyBytes borne la lecture : une réponse Solar API réelle fait quelques
// kilo-octets.
const maxBodyBytes = 1 << 20

// endpoint décrit une entrée de la liste blanche.
type endpoint struct {
	method         string
	path           string
	classification string
	note           string
	// digest : le chemin est atteint par le client officiel avec un GET
	// authentifié (il n'implique pas que l'appareil l'exige : il peut répondre
	// sans en-tête).
	digest bool
}

// whitelist est la liste blanche de la phase 2 : les quatre chemins internes
// (identité, présence d'un compteur et d'une batterie, événements) et l'endpoint
// Solar API de la phase 1. Chaque ajout à cette liste est une décision
// documentée, testée et relue (docs/PROBE-READONLY.md, docs/ENDPOINTS.md).
var whitelist = []endpoint{
	{
		method:         http.MethodGet,
		path:           solarapi.PowerFlowPath,
		classification: "READ",
		note:           "Mesure de production du site (Body.Data.Site) et des onduleurs (Body.Data.Inverters)",
	},
	{
		method:         http.MethodGet,
		path:           internalapi.VersionPath,
		classification: "READ",
		digest:         true,
		note:           "Identité de l'appareil : numéro de série (clé établie), nombre de phases, groupe d'appareil",
	},
	{
		method:         http.MethodGet,
		path:           internalapi.ActiveEventsPath,
		classification: "READ",
		digest:         true,
		note:           "Événements actifs : forme du corps NON établie, c'est la capture attendue",
	},
	{
		method:         http.MethodGet,
		path:           internalapi.PowerMeterPath,
		classification: "READ",
		digest:         true,
		note:           "Présence d'un Fronius Smart Meter (Body.Data indexé par identifiant de composant)",
	},
	{
		method:         http.MethodGet,
		path:           internalapi.BatteryPath,
		classification: "READ",
		digest:         true,
		note:           "Présence d'une batterie (Body.Data indexé par identifiant de composant)",
	},
}

// allowAllEndpoints est la valeur de --endpoint qui sélectionne toute la liste.
const allowAllEndpoints = "all"

type options struct {
	baseURL       string
	allowNonLocal bool
	saveDir       string
	timeout       time.Duration
	list          bool
	endpoint      string
	passwordStdin bool
	out           io.Writer
	// stdin alimente --password-stdin (injectable en test).
	stdin io.Reader
}

func main() {
	var opt options
	flag.StringVar(&opt.baseURL, "base-url", "http://127.0.0.1:8081", "URL de base de l'appareil (simulateur par défaut)")
	flag.BoolVar(&opt.allowNonLocal, "allow-non-loopback", false, "autorise une adresse non locale (à n'utiliser qu'après lecture de docs/PROBE-READONLY.md)")
	flag.StringVar(&opt.saveDir, "save", "", "répertoire où écrire le corps expurgé")
	flag.DurationVar(&opt.timeout, "timeout", 5*time.Second, "délai par requête")
	flag.BoolVar(&opt.list, "list", false, "affiche la liste blanche des endpoints et quitte")
	flag.StringVar(&opt.endpoint, "endpoint", allowAllEndpoints, "chemin à sonder ("+allowAllEndpoints+" ou un chemin de la liste blanche)")
	flag.BoolVar(&opt.passwordStdin, "password-stdin", false, "lit le mot de passe sur la première ligne de l'entrée standard")
	flag.Parse()
	opt.out = os.Stdout
	opt.stdin = os.Stdin

	if err := run(opt); err != nil {
		fmt.Fprintln(os.Stderr, "fronius-probe:", err)
		os.Exit(1)
	}
}

func run(opt options) error {
	if opt.list {
		printWhitelist(opt.out, opt.baseURL)
		return nil
	}

	selected, err := selectEndpoints(opt.endpoint)
	if err != nil {
		return err
	}
	if opt.stdin == nil {
		opt.stdin = os.Stdin
	}
	cred, err := readCredentials(opt)
	if err != nil {
		return err
	}

	base, err := url.Parse(opt.baseURL)
	if err != nil {
		return fmt.Errorf("URL %q: %w", opt.baseURL, err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return fmt.Errorf("URL %q : schéma http(s) attendu", opt.baseURL)
	}
	if base.Host == "" {
		return fmt.Errorf("URL %q : hôte manquant", opt.baseURL)
	}
	if !config.IsLoopbackHost(base.Hostname()) && !opt.allowNonLocal {
		return fmt.Errorf(
			"refus: %s n'est pas une adresse locale ; relancer avec --allow-non-loopback "+
				"après avoir lu docs/PROBE-READONLY.md et obtenu l'accord explicite de l'utilisateur",
			opt.baseURL)
	}
	if opt.timeout <= 0 {
		return fmt.Errorf("--timeout doit être strictement positif")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// La garde lecture seule est interposée devant le transport : une requête
	// non-GET ne peut pas atteindre le réseau, même par erreur de programmation.
	// Le transport Digest est composé sous elle, et seulement pour les chemins
	// qui le demandent ; sans identifiants il émet la requête nue.
	digestTransport, err := digest.New(&http.Client{}, opt.baseURL,
		digest.Credentials{Username: cred.user, Password: cred.password}, logger)
	if err != nil {
		return err
	}

	for _, ep := range selected {
		ctx, cancel := context.WithTimeout(context.Background(), opt.timeout)
		err := probe(ctx, doerFor(ep, digestTransport), base, ep, opt, logger)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// doerFor retourne le transport d'un endpoint, la garde lecture seule restant
// à l'extérieur.
func doerFor(ep endpoint, digestTransport *digest.Transport) fronius.Doer {
	if ep.digest {
		return fronius.ReadOnly{Inner: digestTransport}
	}
	return fronius.ReadOnly{Inner: &http.Client{}}
}

// selectEndpoints résout --endpoint.
//
// Tout chemin absent de la liste blanche est refusé : la sonde ne peut pas
// viser une URL arbitraire, même si l'utilisateur la connaît.
func selectEndpoints(name string) ([]endpoint, error) {
	trimmed := strings.TrimSpace(name)
	switch trimmed {
	case "", allowAllEndpoints:
		return slices.Clone(whitelist), nil
	}
	for _, ep := range whitelist {
		if ep.path == trimmed {
			return []endpoint{ep}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", errNotWhitelisted, trimmed)
}

// credentials rassemble les identifiants, sans jamais les journaliser.
type credentials struct {
	user     string
	password secret.String
}

// readCredentials lit l'environnement, et le mot de passe sur l'entrée standard
// si --password-stdin est actif.
//
// Aucun flag --password n'existe : un mot de passe en argument apparaîtrait
// dans l'historique du shell et dans la table des processus.
func readCredentials(opt options) (credentials, error) {
	out := credentials{user: strings.TrimSpace(os.Getenv("FRONIUS_DEVICE_USER"))}
	if v := strings.TrimSpace(os.Getenv("FRONIUS_DEVICE_PASSWORD")); v != "" {
		out.password = secret.New(v)
	}
	if opt.passwordStdin {
		line, err := bufio.NewReader(opt.stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return credentials{}, fmt.Errorf("lecture du mot de passe sur l'entrée standard: %w", err)
		}
		out.password = secret.New(strings.TrimRight(line, "\r\n"))
	}
	if out.user == "" && !out.password.IsZero() {
		return credentials{}, errors.New("FRONIUS_DEVICE_USER est vide : les deux identifiants vont ensemble")
	}
	if out.user != "" && out.password.IsZero() {
		return credentials{}, errors.New("FRONIUS_DEVICE_PASSWORD est vide : les deux identifiants vont ensemble")
	}
	return out, nil
}

func probe(ctx context.Context, doer fronius.Doer, base *url.URL, ep endpoint, opt options, logger *slog.Logger) error {
	endpointURL := base.JoinPath(ep.path)

	req, err := http.NewRequestWithContext(ctx, ep.method, endpointURL.String(), nil)
	if err != nil {
		return fmt.Errorf("construction de la requête: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	fmt.Fprintf(opt.out, "Requête\n  %s %s\n", ep.method, endpointURL)
	fmt.Fprintf(opt.out, "  classification : %s\n  liste blanche : %s\n\n", ep.classification, ep.note)

	start := time.Now()
	resp, err := doer.Do(req)
	duration := time.Since(start)
	if err != nil {
		return fmt.Errorf("requête %s: %w", endpointURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("lecture du corps: %w", err)
	}
	truncated := len(body) > maxBodyBytes
	if truncated {
		body = body[:maxBodyBytes]
	}

	fmt.Fprintf(opt.out, "Réponse\n  statut   : %s\n  durée    : %s\n  octets   : %d%s\n",
		resp.Status, duration.Round(time.Millisecond), len(body), map[bool]string{true: " (tronqué)"}[truncated])

	fmt.Fprintf(opt.out, "  en-têtes (valeurs sensibles masquées)\n")
	for _, name := range sortedHeaderNames(resp.Header) {
		fmt.Fprintf(opt.out, "    %s: %s\n", name, maskHeaderValue(name, strings.Join(resp.Header.Values(name), ", ")))
	}

	redacted := redactBody(body)
	fmt.Fprintf(opt.out, "\nCorps expurgé\n%s\n", redacted)
	if note := sameBodyNote(body, redacted); note != "" {
		fmt.Fprintf(opt.out, "\n%s\n", note)
	}

	if opt.saveDir != "" {
		path, err := saveRedacted(opt.saveDir, ep, redacted)
		if err != nil {
			return err
		}
		fmt.Fprintf(opt.out, "\nCorps expurgé écrit dans %s\n", path)
	}

	logger.Debug("sonde exécutée", "url", endpointURL.String(), "status", resp.StatusCode, "bytes", len(body))
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("l'appareil a répondu %s", resp.Status)
	}
	return nil
}

func printWhitelist(w io.Writer, baseURL string) {
	fmt.Fprintf(w, "Liste blanche de fronius-probe (mode lecture seule, GET uniquement)\n")
	fmt.Fprintf(w, "Base : %s\n\n", baseURL)
	for _, ep := range whitelist {
		fmt.Fprintf(w, "  %-4s %-60s %-5s digest=%v\n", ep.method, ep.path, ep.classification, ep.digest)
		fmt.Fprintf(w, "       %s\n", ep.note)
	}
	fmt.Fprintf(w, "\nAucune autre requête n'est possible : la liste est figée dans le binaire.\n")
	fmt.Fprintf(w, "Aucune méthode POST, PUT, PATCH ou DELETE n'est construite ni autorisée.\n")
	fmt.Fprintf(w, "\nIdentifiants : FRONIUS_DEVICE_USER et FRONIUS_DEVICE_PASSWORD (environnement),\n")
	fmt.Fprintf(w, "ou --password-stdin pour le mot de passe. L'en-tête Authorization n'est envoyé\n")
	fmt.Fprintf(w, "qu'en réponse à un 401, jamais préemptivement.\n")
}

func sortedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func saveRedacted(dir string, ep endpoint, body []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("création de %s: %w", dir, err)
	}
	name := fmt.Sprintf("probe_%s_%s.json", saveName(ep.path),
		time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("écriture de %s: %w", path, err)
	}
	return path, nil
}

// saveName dérive un nom de fichier du CHEMIN de l'endpoint, et non de son seul
// dernier segment : « /components/PowerMeter/readable » et
// « /components/BatteryManagementSystem/readable » finissent tous deux par
// « readable » et s'écraseraient mutuellement.
//
// La forme obtenue (status_version, components_powermeter_readable) est celle
// des fixtures promues décrites dans docs/PROBE-READONLY.md.
func saveName(path string) string {
	withoutExt := strings.TrimSuffix(path, filepath.Ext(path))
	trimmed := strings.Trim(withoutExt, "/")
	if trimmed == "" {
		return "root"
	}
	replacer := strings.NewReplacer("/", "_", "-", "_", " ", "_")
	return strings.ToLower(replacer.Replace(trimmed))
}

// sameBodyNote signale les octets retirés, sans jamais les afficher.
func sameBodyNote(original, redacted []byte) string {
	if len(original) == len(redacted) {
		return ""
	}
	return "Note : des valeurs ont été remplacées par <redacted> avant affichage et écriture."
}
