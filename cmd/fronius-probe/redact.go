package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// redactedValue remplace toute information potentiellement identifiante.
const redactedValue = "<redacted>"

// sensitiveKeyParts : un nom de champ contenant l'un de ces fragments voit sa
// valeur remplacée. Les noms sont comparés en minuscules.
var sensitiveKeyParts = []string{
	"password", "passwd", "secret", "nonce", "token", "authorization",
	"credential", "serial", "mac", "ssid", "hostname", "ipaddress", "sessionid",
}

// sensitiveValuePatterns : motifs détectés dans une valeur textuelle, quelle
// que soit la clé. Volontairement restreints pour ne jamais masquer une mesure :
// une puissance, une énergie ou un horodatage ne sont pas touchés.
var sensitiveValuePatterns = []*regexp.Regexp{
	// Adresse IPv4.
	regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`),
	// Adresse MAC (séparateurs « : » ou « - »).
	regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:-][0-9A-Fa-f]{2}){5}\b`),
	// Nom d'hôte ou nom pleinement qualifié : commence par une lettre, au moins
	// un point, se termine par une extension alphabétique.
	regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*\.[A-Za-z]{2,}$`),
}

// sensitiveHeaderNames : en-têtes dont la valeur est masquée intégralement.
var sensitiveHeaderNames = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"www-authenticate":    true,
	"x-www-authenticate":  true,
	"x-csrf-token":        true,
	"x-auth-token":        true,
}

// assignmentPattern repère les affectations de secret dans un corps non JSON
// (page HTML, texte brut) : `password="…"`, `nonce: '…'`.
var assignmentPattern = regexp.MustCompile(
	`(?i)\b(password|passwd|secret|nonce|token|authorization|serial|mac|ssid)\b(\s*[:=]\s*)(["']?)([^"',\s<>&]+)`)

// ipv6Candidate restreint les valeurs examinées par looksLikeIPv6 aux chaînes
// composées uniquement de chiffres hexadécimaux et de deux-points.
var ipv6Candidate = regexp.MustCompile(`^[0-9A-Fa-f:]{2,45}$`)

// looksLikeIPv6 reconnaît une adresse IPv6.
//
// La détection est volontairement une fonction plutôt qu'une expression
// régulière permissive : « 12:00:00 » ne doit pas être confondu avec une
// adresse, sous peine d'expurger les horodatages et de rendre les fixtures
// inutilisables. Une heure locale compte au plus trois groupes, une adresse
// IPv6 en compte au moins quatre (ou contient « :: »).
func looksLikeIPv6(s string) bool {
	if !ipv6Candidate.MatchString(s) || !strings.Contains(s, ":") {
		return false
	}
	if strings.Contains(s, "::") {
		return true
	}
	groups := strings.Split(s, ":")
	if len(groups) < 4 {
		return false
	}
	for _, g := range groups {
		if len(g) > 4 {
			return false
		}
	}
	return true
}

// redactBody expurge un corps de réponse.
//
// Si le corps est du JSON, l'expurgation est structurelle : les clés sensibles
// masquent leur valeur, et les valeurs ressemblant à une adresse ou à un nom
// d'hôte sont masquées. Le corps est ensuite réécrit avec une indentation
// stable — ce qui peut réordonner les clés d'objet : c'est le prix d'une
// expurgation fiable, et le résultat reste un JSON valide exploitable comme
// fixture.
//
// Sinon (HTML, texte), une expurgation textuelle est appliquée.
func redactBody(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // conserve la forme littérale des nombres
	var parsed any
	if err := dec.Decode(&parsed); err == nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		// Sans cela, « <redacted> » serait sérialisé « \u003credacted\u003e ».
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(redactTree(parsed, "")); err == nil {
			return buf.Bytes()
		}
	}
	return []byte(redactText(string(body)))
}

func redactTree(node any, key string) any {
	if key != "" && isSensitiveKey(key) {
		return redactedValue
	}
	switch typed := node.(type) {
	case map[string]any:
		for k, v := range typed {
			typed[k] = redactTree(v, k)
		}
		return typed
	case []any:
		for i, v := range typed {
			typed[i] = redactTree(v, "")
		}
		return typed
	case string:
		return redactString(typed)
	default:
		return node
	}
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// redactString masque une valeur textuelle si elle ressemble à une adresse ou
// à un nom d'hôte.
func redactString(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s
	}
	if looksLikeIPv6(trimmed) {
		return redactedValue
	}
	for _, re := range sensitiveValuePatterns {
		if re.MatchString(trimmed) {
			return redactedValue
		}
	}
	return s
}

// redactText applique l'expurgation à un corps non JSON.
func redactText(s string) string {
	out := assignmentPattern.ReplaceAllString(s, "${1}${2}${3}"+redactedValue)
	for _, token := range strings.FieldsFunc(out, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF:", r)
	}) {
		if looksLikeIPv6(token) {
			out = strings.ReplaceAll(out, token, redactedValue)
		}
	}
	for _, re := range sensitiveValuePatterns {
		out = re.ReplaceAllString(out, redactedValue)
	}
	return out
}

// maskHeaderValue masque la valeur d'un en-tête si son nom est sensible, sinon
// y applique la détection d'adresses.
func maskHeaderValue(name, value string) string {
	if sensitiveHeaderNames[strings.ToLower(strings.TrimSpace(name))] {
		return redactedValue
	}
	if value == "" {
		return value
	}
	if looksLikeIPv6(strings.TrimSpace(value)) {
		return redactedValue
	}
	for _, re := range sensitiveValuePatterns {
		if re.MatchString(strings.TrimSpace(value)) {
			return redactedValue
		}
	}
	return value
}
