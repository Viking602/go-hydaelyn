package eval

import (
	"reflect"
	"testing"
	"time"
)

func TestCitationPrecisionRecall(t *testing.T) {
	t.Parallel()

	corpus := Corpus{Documents: []CorpusDocument{
		{ID: "policy-001", Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), Text: "Policy 001: retain customer data for 30 days after account closure unless a legal hold is active."},
		{ID: "evidence-001", Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Text: "Audit log excerpt: account 42 closure initiated on 2026-01-01 and legal hold flag remained false."},
	}}

	answer := "Customer data is retained for 30 days after closure [policy-001]. Legal hold remained false [missing-999]."
	result := ValidateCitations(answer, corpus)

	if result.Precision != 0.5 {
		t.Fatalf("unexpected precision: %v", result.Precision)
	}
	if result.Recall != 0.5 {
		t.Fatalf("unexpected recall: %v", result.Recall)
	}
	if !reflect.DeepEqual(result.InvalidCitations, []string{"missing-999"}) {
		t.Fatalf("unexpected invalid citations: %v", result.InvalidCitations)
	}
}
