package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ProbeClass is the outcome of one read-only survey attempt.
//
// The distinction carries the whole point of a survey: a path that answers 404
// is absent from this firmware, a path that answers 401 exists but is not
// reachable without credentials, and a path that answers an empty body exists
// and carries nothing. Collapsing the three into "failed" would lose exactly
// the information the survey was run to obtain.
type ProbeClass string

const (
	// ProbeOK: readable JSON, with content.
	ProbeOK ProbeClass = "ok"
	// ProbeEmpty: readable JSON that carries nothing (empty object, empty
	// array): the endpoint exists and describes an uninstalled component.
	ProbeEmpty ProbeClass = "empty"
	// ProbeNotJSON: answered, but not with JSON.
	ProbeNotJSON ProbeClass = "not_json"
	// ProbeAuthRequired: 401 or 403 — the path exists behind authentication.
	ProbeAuthRequired ProbeClass = "auth_required"
	// ProbeNotFound: 404 — the path is not served by this firmware.
	ProbeNotFound ProbeClass = "not_found"
	// ProbeError: anything else (5xx, timeout, connection refused).
	ProbeError ProbeClass = "error"
)

// ProbeField is one value observed in a surveyed answer: its JSON path and its
// type, never its value.
type ProbeField struct {
	Path string `json:"path"`
	Type string `json:"type"`
	// ArrayLen is the length of an array, so that "a list of two components"
	// can be told from "an empty list".
	ArrayLen int `json:"array_len,omitempty"`
}

// SourceProbe is the recorded outcome of one survey attempt on one path.
type SourceProbe struct {
	Path           string
	Query          string
	Class          ProbeClass
	HTTPStatus     int
	DigestRequired bool
	Bytes          int
	Error          string
	// Firmware is the device version observed at survey time, empty when the
	// survey could not establish it.
	Firmware string
	ProbedAt time.Time
	Fields   []ProbeField
}

// SaveSourceProbes records survey outcomes, replacing the row of each
// (path, query): a survey states what the device answers now.
func (s *Store) SaveSourceProbes(ctx context.Context, probes []SourceProbe) error {
	if len(probes) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("recording the survey: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const q = `INSERT INTO source_probe (
			path, query, class, http_status, digest_required, body_bytes,
			error, firmware, probed_at_utc, fields_json)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(path, query) DO UPDATE SET
			class = excluded.class,
			http_status = excluded.http_status,
			digest_required = excluded.digest_required,
			body_bytes = excluded.body_bytes,
			error = excluded.error,
			firmware = excluded.firmware,
			probed_at_utc = excluded.probed_at_utc,
			fields_json = excluded.fields_json`

	for _, p := range probes {
		fields, err := json.Marshal(p.Fields)
		if err != nil {
			return fmt.Errorf("serialising the fields of %s: %w", p.Path, err)
		}
		if _, err := tx.ExecContext(ctx, q,
			p.Path, p.Query, string(p.Class), p.HTTPStatus, boolToInt(p.DigestRequired),
			p.Bytes, p.Error, p.Firmware, formatTS(p.ProbedAt), string(fields),
		); err != nil {
			return fmt.Errorf("recording the survey of %s: %w", p.Path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the survey: %w", err)
	}
	return nil
}

// SourceProbes returns the recorded survey outcomes, ordered by path.
func (s *Store) SourceProbes(ctx context.Context) ([]SourceProbe, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, query, class, http_status,
		digest_required, body_bytes, error, firmware, probed_at_utc, fields_json
		FROM source_probe ORDER BY path, query`)
	if err != nil {
		return nil, fmt.Errorf("reading the survey: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SourceProbe
	for rows.Next() {
		var (
			p          SourceProbe
			class      string
			digest     int
			probedAt   string
			fieldsJSON string
		)
		if err := rows.Scan(&p.Path, &p.Query, &class, &p.HTTPStatus, &digest,
			&p.Bytes, &p.Error, &p.Firmware, &probedAt, &fieldsJSON); err != nil {
			return nil, fmt.Errorf("reading the survey: %w", err)
		}
		// An unreadable stamp stays at the zero time rather than failing the
		// whole read: the outcome of the probe is still true.
		if ts, err := parseTS(probedAt); err == nil {
			p.ProbedAt = ts
		}
		p.Class = ProbeClass(class)
		p.DigestRequired = digest != 0
		if fieldsJSON != "" {
			if err := json.Unmarshal([]byte(fieldsJSON), &p.Fields); err != nil {
				// A row we cannot read must not make the interface lie about
				// the rest: the probe result stays, its field list is dropped.
				p.Fields = nil
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the survey: %w", err)
	}
	return out, nil
}
