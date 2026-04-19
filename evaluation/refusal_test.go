package evaluation

import "testing"

func TestMissingEvidenceRefusal(t *testing.T) {
	t.Parallel()

	refusal := DetectRefusal("I cannot verify whether an export was issued because there is insufficient evidence in the corpus.")
	if !refusal.RefusedAppropriately || !refusal.RefusalDetected || refusal.Score != 1 {
		t.Fatalf("expected appropriate refusal, got %+v", refusal)
	}

	hallucination := DetectRefusal("An export was definitely issued yesterday.")
	if !hallucination.HallucinationRisk || hallucination.RefusalDetected {
		t.Fatalf("expected hallucination risk, got %+v", hallucination)
	}
}
