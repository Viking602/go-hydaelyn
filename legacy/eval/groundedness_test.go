package eval

import (
	"reflect"
	"testing"
	"time"
)

func TestGroundednessChecker(t *testing.T) {
	t.Parallel()

	corpus := Corpus{Documents: []CorpusDocument{
		{ID: "policy-001", Date: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), Text: "Policy 001: retain customer data for 30 days after account closure unless a legal hold is active."},
		{ID: "evidence-001", Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Text: "Audit log excerpt: account 42 closure initiated on 2026-01-01 and legal hold flag remained false."},
	}}

	answer := "Customer data is retained for 30 days after closure [policy-001]. Legal hold remained false [evidence-001]. The account is retained for 60 days. I cannot verify whether an export was issued because there is insufficient evidence."
	result := CheckGroundedness(answer, corpus)

	if result.SupportedClaims != 2 || result.UnsupportedClaims != 1 || result.BlockedClaims != 1 {
		t.Fatalf("unexpected claim counts: %+v", result)
	}
	if result.GroundednessRatio != 2.0/3.0 {
		t.Fatalf("unexpected groundedness ratio: %v", result.GroundednessRatio)
	}
	gotStatuses := []ClaimStatus{result.Claims[0].Status, result.Claims[1].Status, result.Claims[2].Status, result.Claims[3].Status}
	wantStatuses := []ClaimStatus{ClaimSupported, ClaimSupported, ClaimUnsupported, ClaimBlocked}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("unexpected claim statuses: got %v want %v", gotStatuses, wantStatuses)
	}
}
