package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Detail is one family of the device's diagnostic detail, as last read.
//
// A family is the unit that succeeds or fails together: the detector reads one
// path and either it answered or it did not. `Available` and `Reason` belong to
// the read that produced them — a reader must never have to reconstruct why a
// family is empty.
type Detail struct {
	Family string
	// ReadAtUTC is the instant of the read. These values are read on a slow
	// cycle: the interface must be able to say how recent the answer is.
	ReadAtUTC time.Time
	Available bool
	Reason    string
	// Data is the payload as the device answered it (a JSON document), never a
	// reinterpretation: the mapping to a display shape happens at the API
	// boundary.
	Data json.RawMessage
}

// SaveDetails records the detail families, replacing the row of each: the table
// holds what the device says now, not a history.
func (s *Store) SaveDetails(ctx context.Context, details []Detail) error {
	if len(details) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("recording the device detail: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const q = `INSERT INTO device_detail (family, read_at_utc, available, reason, data_json)
		VALUES (?,?,?,?,?)
		ON CONFLICT(family) DO UPDATE SET
			read_at_utc = excluded.read_at_utc,
			available = excluded.available,
			reason = excluded.reason,
			data_json = excluded.data_json`

	for _, d := range details {
		data := d.Data
		if len(data) == 0 {
			data = json.RawMessage("{}")
		}
		if !json.Valid(data) {
			return fmt.Errorf("family %q: the payload is not valid JSON", d.Family)
		}
		if _, err := tx.ExecContext(ctx, q,
			d.Family, formatTS(d.ReadAtUTC), boolToInt(d.Available), d.Reason, string(data),
		); err != nil {
			return fmt.Errorf("recording the device detail of %q: %w", d.Family, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing the device detail: %w", err)
	}
	return nil
}

// Details returns the recorded detail families, ordered by name. A family
// absent from the result was never read: that is not the same as one recorded
// as unavailable.
func (s *Store) Details(ctx context.Context) ([]Detail, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT family, read_at_utc, available, reason, data_json FROM device_detail ORDER BY family`)
	if err != nil {
		return nil, fmt.Errorf("reading the device detail: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Detail
	for rows.Next() {
		var (
			d        Detail
			readAt   string
			avail    int
			dataJSON string
		)
		if err := rows.Scan(&d.Family, &readAt, &avail, &d.Reason, &dataJSON); err != nil {
			return nil, fmt.Errorf("reading the device detail: %w", err)
		}
		if ts, err := parseTS(readAt); err == nil {
			d.ReadAtUTC = ts
		}
		d.Available = avail != 0
		d.Data = json.RawMessage(dataJSON)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the device detail: %w", err)
	}
	return out, nil
}
