package blackboard

import "testing"

func TestLegacyReadKeyToSelector_PreservesPermissivePayload(t *testing.T) {
	sel := LegacyReadKeyToSelector("research.notes")
	if len(sel.Keys) != 1 || sel.Keys[0] != "research.notes" {
		t.Fatalf("expected key research.notes, got %v", sel.Keys)
	}
	if !sel.IncludeText || !sel.IncludeStructured || !sel.IncludeArtifacts {
		t.Fatalf("expected legacy selector to permit all payload types, got %#v", sel)
	}
	if !sel.Required {
		t.Fatalf("expected legacy Reads entries to map to required selectors, got Required=false")
	}
}

func TestSelectExchanges_RequireVerifiedFiltersUnsupportedClaims(t *testing.T) {
	state := State{
		Claims: []Claim{{ID: "claim-1"}, {ID: "claim-2"}},
		Verifications: []VerificationResult{
			{ClaimID: "claim-1", Status: VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev-1"}},
			{ClaimID: "claim-2", Status: VerificationStatusContradicted, Confidence: 0.9, EvidenceIDs: []string{"ev-2"}},
		},
		Exchanges: []Exchange{
			{Key: "findings", Namespace: "research", ClaimIDs: []string{"claim-1"}},
			{Key: "findings", Namespace: "research", ClaimIDs: []string{"claim-2"}},
			{Key: "findings", Namespace: "research", ClaimIDs: []string{"claim-1", "claim-2"}},
		},
	}
	got := state.SelectExchanges(ExchangeSelector{Keys: []string{"findings"}, RequireVerified: true})
	if len(got) != 1 {
		t.Fatalf("expected only the fully supported exchange, got %d entries: %#v", len(got), got)
	}
	if len(got[0].ClaimIDs) != 1 || got[0].ClaimIDs[0] != "claim-1" {
		t.Fatalf("expected supported claim-1 exchange, got %#v", got[0])
	}
}

func TestSelectExchanges_MinConfidenceGatesStructuredConfidence(t *testing.T) {
	state := State{
		Exchanges: []Exchange{
			{Key: "summary", Structured: map[string]any{"confidence": 0.3}},
			{Key: "summary", Structured: map[string]any{"confidence": 0.9}},
		},
	}
	got := state.SelectExchanges(ExchangeSelector{Keys: []string{"summary"}, MinConfidence: 0.7})
	if len(got) != 1 {
		t.Fatalf("expected only high-confidence exchange, got %d: %#v", len(got), got)
	}
	if conf, _ := got[0].Structured["confidence"].(float64); conf < 0.9 {
		t.Fatalf("expected confidence 0.9 entry, got %v", conf)
	}
}

func TestSelectFindings_ContradictedClaimPoisonsFinding(t *testing.T) {
	// A finding backed by two claims — one supported, one contradicted — must
	// not be returned under RequireVerified because partial support cannot
	// protect downstream synthesis from the contradicted claim.
	state := State{
		Findings: []Finding{
			{ID: "finding-mixed", TaskID: "task-1", ClaimIDs: []string{"claim-1", "claim-2"}, Confidence: 0.9},
			{ID: "finding-clean", TaskID: "task-1", ClaimIDs: []string{"claim-1"}, Confidence: 0.9},
		},
		Verifications: []VerificationResult{
			{ClaimID: "claim-1", Status: VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev-1"}},
			{ClaimID: "claim-2", Status: VerificationStatusContradicted, Confidence: 0.9, EvidenceIDs: []string{"ev-2"}},
		},
	}
	got := state.SelectFindings(ExchangeSelector{RequireVerified: true})
	if len(got) != 1 || got[0].ID != "finding-clean" {
		t.Fatalf("expected only finding-clean to pass RequireVerified, got %#v", got)
	}
}

func TestSelectFindings_MinConfidenceDropsWeakClaims(t *testing.T) {
	state := State{
		Findings: []Finding{
			{ID: "finding-weak", Confidence: 0.4},
			{ID: "finding-strong", Confidence: 0.92},
		},
	}
	got := state.SelectFindings(ExchangeSelector{MinConfidence: 0.8})
	if len(got) != 1 || got[0].ID != "finding-strong" {
		t.Fatalf("expected only finding-strong to clear threshold, got %#v", got)
	}
}

func TestSelectExchanges_ValueTypeAndLimitHonored(t *testing.T) {
	state := State{
		Exchanges: []Exchange{
			{Key: "notes", ValueType: ExchangeValueTypeText},
			{Key: "notes", ValueType: ExchangeValueTypeJSON},
			{Key: "notes", ValueType: ExchangeValueTypeText},
		},
	}
	got := state.SelectExchanges(ExchangeSelector{Keys: []string{"notes"}, ValueTypes: []ExchangeValueType{ExchangeValueTypeText}, Limit: 1})
	if len(got) != 1 {
		t.Fatalf("expected limit=1 to cap results, got %d", len(got))
	}
	if got[0].ValueType != ExchangeValueTypeText {
		t.Fatalf("expected text value type, got %s", got[0].ValueType)
	}
}
