package internalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CacheChannel is one register of a cache node.
//
// `Key` is the device's own key. Most are named — `PV_VOLTAGE_MEAN_01_F32` — and
// carry their type in a suffix; the rest are a bare numeric identifier the
// device writes as `<1090584584>`, which nothing observed so far explains: they
// are kept and shown as they are, never guessed.
type CacheChannel struct {
	Key string
	// Name is the key without its type suffix, empty for a bare identifier.
	Name string
	// Kind is the type suffix the device itself encodes (`F32`, `U64`, `U16`, …),
	// empty when the key carries none.
	Kind string
	// Type is the JSON type of the value as the device answered it.
	Type string
	// Value is the value as the device wrote it, kept verbatim: a 64-bit energy
	// counter does not survive a float64, and rounding a device's own number
	// would be inventing one.
	Value json.RawMessage
}

// CacheAttribute is one attribute of a node: the device labels it, so the value
// is kept as the text it answered.
type CacheAttribute struct {
	Key   string
	Value string
}

// CacheNode is one node of the cache, keyed by the device's node identifier.
type CacheNode struct {
	ID         string
	Attributes []CacheAttribute
	Channels   []CacheChannel
}

// kindSuffix matches the type marker the device appends to a named channel.
var kindSuffix = regexp.MustCompile(`_(F32|F64|U16|U32|U64|I16|I32|I64|STR|BOOL)$`)

// Cache reads the readable cache.
//
// The answer is an envelope whose Body.Data is keyed by node identifier; this
// reads every node the device reports rather than a hard-coded one, and sorts
// the keys so a display does not shuffle between two reads.
func (c *Client) Cache(ctx context.Context) ([]CacheNode, error) {
	ep, ok := Lookup(CachePath)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotAllowed, CachePath)
	}
	body, err := c.get(ctx, ep)
	if err != nil {
		return nil, err
	}
	nodes, err := decodeData(ep.Path, body)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]CacheNode, 0, len(ids))
	for _, id := range ids {
		var raw struct {
			Attributes map[string]json.RawMessage `json:"attributes"`
			Channels   map[string]json.RawMessage `json:"channels"`
		}
		if err := json.Unmarshal(nodes[id], &raw); err != nil {
			// A node we cannot read is reported as an empty one rather than
			// dropped: a hole in an inventory reads as "nothing there".
			out = append(out, CacheNode{ID: id})
			continue
		}

		node := CacheNode{ID: id, Attributes: cacheAttributes(raw.Attributes), Channels: cacheChannels(raw.Channels)}
		out = append(out, node)
	}
	return out, nil
}

// cacheAttributes renders the attribute map in a stable order.
func cacheAttributes(in map[string]json.RawMessage) []CacheAttribute {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]CacheAttribute, 0, len(keys))
	for _, key := range keys {
		out = append(out, CacheAttribute{Key: key, Value: attributeText(in[key])})
	}
	return out
}

// attributeText renders an attribute as text.
//
// The observed attributes are strings; anything else keeps its JSON form, so a
// number or an object is shown as the device wrote it rather than dropped.
func attributeText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// cacheChannels reads the channel map: the value as the device wrote it, with
// the name and the type suffix the key carries.
func cacheChannels(in map[string]json.RawMessage) []CacheChannel {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	// Named registers first, alphabetical, then the bare identifiers: the
	// readable half of the cache is the half a device named, and a display that
	// opened on `<1090584584>` would bury it (a `<` sorts before any letter).
	sort.Slice(keys, func(i, j int) bool {
		bi, bj := strings.HasPrefix(keys[i], "<"), strings.HasPrefix(keys[j], "<")
		if bi != bj {
			return !bi
		}
		return keys[i] < keys[j]
	})

	out := make([]CacheChannel, 0, len(keys))
	for _, key := range keys {
		ch := CacheChannel{Key: key, Value: in[key], Type: jsonType(in[key])}

		// The suffix is only stripped when the key is a name: a bare `<id>`
		// identifier is shown as it is.
		if !strings.HasPrefix(key, "<") {
			if m := kindSuffix.FindStringSubmatch(key); m != nil {
				ch.Kind = m[1]
				ch.Name = strings.TrimSuffix(key, "_"+m[1])
			} else {
				ch.Name = key
			}
		}
		out = append(out, ch)
	}
	return out
}

// jsonType names the JSON type of a raw value, for a display that must tell a
// counter from a flag.
func jsonType(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case trimmed == "" || trimmed == "null":
		return "null"
	case trimmed == "true" || trimmed == "false":
		return "bool"
	case strings.HasPrefix(trimmed, `"`):
		return "string"
	case strings.ContainsAny(trimmed, ".eE"):
		return "float"
	default:
		return "int"
	}
}
