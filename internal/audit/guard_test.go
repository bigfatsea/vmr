// Ver 2026-09-13, by Sonnet 5

package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRecord_GuardNilOmitted confirms a Record with no Guard set — every
// record on the wire today, since nothing in this change populates it —
// marshals with no "guard" key at all, not "guard":null. ADR-12's
// backward-compat requirement (report's existing golden output must stay
// byte-identical when Guard is nil) depends on this.
func TestRecord_GuardNilOmitted(t *testing.T) {
	r := Record{Model: "coding", Protocol: "anthropic-messages"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"guard"`) {
		t.Errorf("Record with nil Guard should omit the field entirely, got: %s", data)
	}
}

// TestRecord_GuardRoundTrip confirms a populated GuardRecord survives a
// marshal/unmarshal round trip byte-for-byte in content (field values).
func TestRecord_GuardRoundTrip(t *testing.T) {
	r := Record{
		Model: "coding",
		Guard: &GuardRecord{
			Ver:     1,
			OutMode: "audit_only",
			Hits: []Hit{
				{Rule: "gcp-api-key", Tier: 1, Count: 3, FP: "abc123"},
			},
			SanitizedRunes: map[string]int{"bidi": 2},
		},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Record
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Guard == nil {
		t.Fatal("Guard is nil after round trip")
	}
	if got.Guard.Ver != 1 || got.Guard.OutMode != "audit_only" {
		t.Errorf("Guard scalar fields did not survive round trip: %+v", got.Guard)
	}
	if len(got.Guard.Hits) != 1 || got.Guard.Hits[0].Rule != "gcp-api-key" || got.Guard.Hits[0].Count != 3 {
		t.Errorf("Guard.Hits did not survive round trip: %+v", got.Guard.Hits)
	}
	if got.Guard.SanitizedRunes["bidi"] != 2 {
		t.Errorf("Guard.SanitizedRunes did not survive round trip: %+v", got.Guard.SanitizedRunes)
	}
}

// TestRecord_GuardAbsentField confirms a historical record with no "guard"
// key at all — every audit line ever written before this change — decodes
// to Guard == nil rather than erroring or defaulting to a non-nil zero
// value.
func TestRecord_GuardAbsentField(t *testing.T) {
	var r Record
	if err := json.Unmarshal([]byte(`{"model":"coding","protocol":"anthropic-messages"}`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Guard != nil {
		t.Errorf("Guard = %+v, want nil for a record with no guard field", r.Guard)
	}
}
