// Tests of the device detail snapshot.
//
// The table holds what the device says about itself, one row per family. What
// these tests pin is the distinction the interface depends on: a family
// recorded as unavailable is not a family that was never read.
package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestDetailsRecordPerFamilyAndReplace(t *testing.T) {
	s := openStore(t, brussels(t))
	ctx := context.Background()
	first := time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC)
	second := time.Date(2026, 10, 16, 19, 30, 0, 0, time.UTC)

	// Nothing read yet: no row at all, which is not "unavailable".
	details, err := s.Details(ctx)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	if len(details) != 0 {
		t.Fatalf("%d families before any read, want none", len(details))
	}

	if err := s.SaveDetails(ctx, []Detail{
		{Family: "inverter", ReadAtUTC: first, Available: true,
			Data: json.RawMessage(`{"state":{"inverter_state":"Sleeping"}}`)},
		{Family: "network", ReadAtUTC: first, Available: false,
			Reason: "the device answered 401"},
	}); err != nil {
		t.Fatalf("SaveDetails: %v", err)
	}

	// A second read of one family replaces its row and leaves the other alone.
	if err := s.SaveDetails(ctx, []Detail{
		{Family: "inverter", ReadAtUTC: second, Available: true,
			Data: json.RawMessage(`{"state":{"inverter_state":"Running"}}`)},
	}); err != nil {
		t.Fatalf("SaveDetails: %v", err)
	}

	details, err = s.Details(ctx)
	if err != nil {
		t.Fatalf("Details: %v", err)
	}
	if len(details) != 2 {
		t.Fatalf("%d families, want 2", len(details))
	}
	byFamily := map[string]Detail{}
	for _, d := range details {
		byFamily[d.Family] = d
	}

	inverter := byFamily["inverter"]
	if !inverter.Available || !inverter.ReadAtUTC.Equal(second) {
		t.Errorf("inverter: %+v", inverter)
	}
	if string(inverter.Data) != `{"state":{"inverter_state":"Running"}}` {
		t.Errorf("inverter data = %s", inverter.Data)
	}
	network := byFamily["network"]
	if network.Available || network.Reason == "" || network.Data == nil {
		t.Errorf("network: %+v", network)
	}
	if string(network.Data) != "{}" {
		t.Errorf("an unavailable family must still carry a payload: %s", network.Data)
	}
}

func TestDetailsRefuseAPayloadThatIsNotJSON(t *testing.T) {
	s := openStore(t, brussels(t))
	err := s.SaveDetails(context.Background(), []Detail{
		{Family: "inverter", ReadAtUTC: time.Now().UTC(), Available: true,
			Data: json.RawMessage(`{"state":`)},
	})
	if err == nil {
		t.Fatal("a truncated payload was recorded")
	}
	if details, err := s.Details(context.Background()); err != nil || len(details) != 0 {
		t.Errorf("after the refusal: %d families (err=%v)", len(details), err)
	}
}
