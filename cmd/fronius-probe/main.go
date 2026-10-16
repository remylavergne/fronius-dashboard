// Command fronius-probe effectue une requête de diagnostic en LECTURE SEULE
// vers un onduleur Fronius, et écrit le corps expurgé sur demande.
//
// Ce binaire est destiné à être exécuté par l'utilisateur, sur son propre
// matériel, après lecture de docs/PROBE-READONLY.md. Il n'est jamais lancé par
// l'agent ni par la CI.
//
// Garanties :
//   - liste blanche d'un seul endpoint, en GET uniquement ;
//   - refus d'une adresse non locale sans --allow-non-loopback explicite ;
//   - aucune écriture vers l'appareil : aucune méthode non-GET n'est construite,
//     et la garde fronius.ReadOnly rejette toute requête qui ne serait pas un GET ;
//   - expurgation du corps et des en-têtes avant toute écriture sur disque.
package main

import (
	"context"
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
	"fronius-supervisor/internal/fronius/solarapi"
)

// maxBodyBytes borne la lecture : une réponse Solar API réelle fait quelques
// kilo-octets.
const maxBodyBytes = 1 << 20

// endpoint décrit une entrée de la liste blanche.
type endpoint struct {
	method         string
	path           string
	classification string
	note           string
}

// whitelist est la liste blanche de la phase 1. Elle ne contient qu'un
// endpoint : son ajout à cette liste est une décision documentée, testée et
// relue (docs/PROBE-READONLY.md).
var whitelist = []endpoint{{
	method:         http.MethodGet,
	path:           solarapi.PowerFlowPath,
	classification: "READ",
	note:           "Mesure de production du site (Body.Data.Site) et des onduleurs (Body.Data.Inverters)",
}}

type options struct {
	baseURL       string
	allowNonLocal bool
	saveDir       string
	timeout       time.Duration
	list          bool
	out           io.Writer
}

func main() {
	var opt options
	flag.StringVar(&opt.baseURL, "base-url", "http://127.0.0.1:8081", "URL de base de l'appareil (simulateur par défaut)")
	flag.BoolVar(&opt.allowNonLocal, "allow-non-loopback", false, "autorise une adresse non locale (à n'utiliser qu'après lecture de docs/PROBE-READONLY.md)")
	flag.StringVar(&opt.saveDir, "save", "", "répertoire où écrire le corps expurgé")
	flag.DurationVar(&opt.timeout, "timeout", 5*time.Second, "délai par requête")
	flag.BoolVar(&opt.list, "list", false, "affiche la liste blanche des endpoints et quitte")
	flag.Parse()
	opt.out = os.Stdout

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
	var doer fronius.Doer = fronius.ReadOnly{Inner: &http.Client{}}

	ctx, cancel := context.WithTimeout(context.Background(), opt.timeout)
	defer cancel()

	for _, ep := range whitelist {
		if err := probe(ctx, doer, base, ep, opt, logger); err != nil {
			return err
		}
	}
	return nil
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
		fmt.Fprintf(w, "  %-4s %-60s %s\n", ep.method, ep.path, ep.classification)
		fmt.Fprintf(w, "       %s\n", ep.note)
	}
	fmt.Fprintf(w, "\nAucune autre requête n'est possible : la liste est figée dans le binaire.\n")
	fmt.Fprintf(w, "Aucune méthode POST, PUT, PATCH ou DELETE n'est construite ni autorisée.\n")
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
	name := fmt.Sprintf("probe_%s_%s.json",
		strings.TrimSuffix(filepath.Base(ep.path), filepath.Ext(ep.path)),
		time.Now().UTC().Format("20060102T150405Z"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", fmt.Errorf("écriture de %s: %w", path, err)
	}
	return path, nil
}

// sameBodyNote signale les octets retirés, sans jamais les afficher.
func sameBodyNote(original, redacted []byte) string {
	if len(original) == len(redacted) {
		return ""
	}
	return "Note : des valeurs ont été remplacées par <redacted> avant affichage et écriture."
}
