package evaluation

import (
	"testing"
	"time"
)

func TestConflictEvidenceLatestWins(t *testing.T) {
	t.Parallel()

	corpus := Corpus{Documents: []CorpusDocument{
		{ID: "policy-old", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Text: "Policy snapshot: retain customer data for 30 days after account closure."},
		{ID: "policy-new", Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Text: "Policy snapshot: retain customer data for 60 days after account closure."},
	}}

	claims := []Claim{{Text: "Customer data is retained for 60 days after account closure."}}
	resolved := ResolveTemporalConflicts(claims, corpus)
	if len(resolved) != 1 {
		t.Fatalf("unexpected resolved claim count: %d", len(resolved))
	}
	if resolved[0].Status != ClaimSupported {
		t.Fatalf("expected supported claim, got %+v", resolved[0])
	}
	if !resolved[0].Conflict {
		t.Fatalf("expected conflict flag, got %+v", resolved[0])
	}
	if resolved[0].SupportDocIDs[0] != "policy-new" {
		t.Fatalf("expected latest supporting doc, got %+v", resolved[0].SupportDocIDs)
	}
}
