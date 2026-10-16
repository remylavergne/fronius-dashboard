package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// redactedValue replaces any potentially identifying information.
const redactedValue = "<redacted>"

// sensitiveKeyParts: a field name containing one of these fragments has its
// value replaced. Names are compared in lowercase.
var sensitiveKeyParts = []string{
	"password", "passwd", "secret", "nonce", "token", "authorization",
	"credential", "serial", "mac", "ssid", "hostname", "ipaddress", "sessionid",
}

// sensitiveValuePatterns: patterns detected in a textual value, whatever
// the key. Deliberately restricted so as never to mask a measurement:
// a power, an energy or a timestamp is not touched.
var sensitiveValuePatterns = []*regexp.Regexp{
	// IPv4 address.
	regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`),
	// MAC address (":" or "-" separators).
	regexp.MustCompile(`\b[0-9A-Fa-f]{2}(?:[:-][0-9A-Fa-f]{2}){5}\b`),
	// Hostname or fully qualified name: starts with a letter, at least
	// one dot, ends with an alphabetic extension.
	regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*\.[A-Za-z]{2,}$`),
}

// sensitiveHeaderNames: headers whose value is masked entirely.
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

// assignmentPattern detects secret assignments in a non-JSON body
// (HTML page, plain text): `password="…"`, `nonce: '…'`.
var assignmentPattern = regexp.MustCompile(
	`(?i)\b(password|passwd|secret|nonce|token|authorization|serial|mac|ssid)\b(\s*[:=]\s*)(["']?)([^"',\s<>&]+)`)

// ipv6Candidate restricts the values examined by looksLikeIPv6 to strings
// composed only of hexadecimal digits and colons.
var ipv6Candidate = regexp.MustCompile(`^[0-9A-Fa-f:]{2,45}$`)

// looksLikeIPv6 recognises an IPv6 address.
//
// Detection is deliberately a function rather than a permissive regular
// expression: "12:00:00" must not be confused with an
// address, at the risk of redacting timestamps and rendering the fixtures
// unusable. A local time has at most three groups, an IPv6 address has
// at least four (or contains "::").
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

// redactBody redacts a response body.
//
// If the body is JSON, the redaction is structural: sensitive keys
// mask their value, and values resembling an address or a host name
// are masked. The body is then rewritten with a stable indentation
// — which may reorder object keys: that is the price of a
// reliable redaction, and the result remains valid JSON usable as a
// fixture.
//
// Otherwise (HTML, text), a textual redaction is applied.
func redactBody(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keeps the literal form of numbers
	var parsed any
	if err := dec.Decode(&parsed); err == nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		// Without this, "<redacted>" would be serialized "\u003credacted\u003e".
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

// redactString masks a textual value if it resembles an address or
// a host name.
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

// redactText applies the redaction to a non-JSON body.
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

// maskHeaderValue masks the value of a header if its name is sensitive,
// otherwise applies address detection to it.
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
