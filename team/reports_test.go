package team

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractResearchReport_RoundTripsThroughStructured(t *testing.T) {
	report := ResearchReport{
		Kind: ReportKindResearch,
		Claims: []ReportClaim{
			{ID: "c1", Summary: "sky is blue", Confidence: 0.9},
		},
		Findings: []ReportFinding{
			{ID: "f1", Summary: "blue sky confirmed", ClaimIDs: []string{"c1"}},
		},
		Confidence: 0.8,
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	structured := map[string]any{ReportKey: decoded}

	got, ok := ExtractResearchReport(structured)
	if !ok {
		t.Fatalf("ExtractResearchReport returned !ok")
	}
	if len(got.Claims) != 1 || got.Claims[0].ID != "c1" {
		t.Fatalf("unexpected claims: %#v", got.Claims)
	}
	if len(got.Findings) != 1 || got.Findings[0].ClaimIDs[0] != "c1" {
		t.Fatalf("unexpected findings: %#v", got.Findings)
	}
}

func TestExtractResearchReport_RejectsWrongKind(t *testing.T) {
	structured := map[string]any{
		ReportKey: map[string]any{
			"kind":   "verification",
			"claims": []any{},
		},
	}
	if _, ok := ExtractResearchReport(structured); ok {
		t.Fatalf("expected research extraction to reject verification kind")
	}
}

func TestExtractResearchReport_AbsentReport(t *testing.T) {
	if _, ok := ExtractResearchReport(nil); ok {
		t.Fatalf("nil structured must not yield report")
	}
	if _, ok := ExtractResearchReport(map[string]any{"other": 1}); ok {
		t.Fatalf("structured without report key must not yield report")
	}
}

func TestExtractVerificationReport_LegacyTopLevelStatus(t *testing.T) {
	structured := map[string]any{"status": "supported"}
	report, ok := ExtractVerificationReport(structured)
	if !ok {
		t.Fatalf("legacy status must yield report")
	}
	if report.Kind != ReportKindVerification || report.Status != VerificationStatusSupported {
		t.Fatalf("legacy status produced wrong report: %#v", report)
	}
}

func TestExtractVerificationReport_LegacyRejectsUnknown(t *testing.T) {
	structured := map[string]any{"status": "maybe-ish"}
	if _, ok := ExtractVerificationReport(structured); ok {
		t.Fatalf("unknown legacy status must be rejected")
	}
}

func TestExtractSynthesisReport_AbsentReport(t *testing.T) {
	if _, ok := ExtractSynthesisReport(map[string]any{}); ok {
		t.Fatalf("empty structured must not yield synthesis report")
	}
}

func TestValidateResearchReport_RequiresClaim(t *testing.T) {
	err := ValidateResearchReport(ResearchReport{Kind: ReportKindResearch})
	if err == nil || !strings.Contains(err.Error(), "at least one claim") {
		t.Fatalf("expected missing-claim error, got %v", err)
	}

	err = ValidateResearchReport(ResearchReport{
		Kind:     ReportKindResearch,
		Findings: []ReportFinding{{Summary: "finding without claim"}},
	})
	if err == nil || !strings.Contains(err.Error(), "at least one claim") {
		t.Fatalf("expected findings-only report to be rejected, got %v", err)
	}
}

func TestValidateResearchReport_RejectsWrongKind(t *testing.T) {
	err := ValidateResearchReport(ResearchReport{Kind: ReportKindVerification})
	if err == nil {
		t.Fatalf("expected kind mismatch error")
	}
}

func TestValidateResearchReport_RejectsDanglingClaimID(t *testing.T) {
	report := ResearchReport{
		Kind:   ReportKindResearch,
		Claims: []ReportClaim{{ID: "c1", Summary: "one"}},
		Findings: []ReportFinding{
			{Summary: "f", ClaimIDs: []string{"does-not-exist"}},
		},
	}
	err := ValidateResearchReport(report)
	if err == nil || !strings.Contains(err.Error(), "unknown claim") {
		t.Fatalf("expected dangling claim error, got %v", err)
	}
}

func TestValidateResearchReport_RejectsDuplicateLocalIDs(t *testing.T) {
	cases := []struct {
		name   string
		report ResearchReport
		want   string
	}{
		{
			name: "duplicate claims",
			report: ResearchReport{
				Kind: ReportKindResearch,
				Claims: []ReportClaim{
					{ID: "claim-1", Summary: "one"},
					{ID: "claim-1", Summary: "two"},
				},
			},
			want: "duplicates id claim-1",
		},
		{
			name: "duplicate evidence",
			report: ResearchReport{
				Kind:     ReportKindResearch,
				Claims:   []ReportClaim{{ID: "claim-1", Summary: "one"}},
				Evidence: []ReportEvidence{{ID: "evidence-1", Snippet: "one"}, {ID: "evidence-1", Snippet: "two"}},
			},
			want: "duplicates id evidence-1",
		},
		{
			name: "duplicate findings",
			report: ResearchReport{
				Kind:     ReportKindResearch,
				Claims:   []ReportClaim{{ID: "claim-1", Summary: "one"}},
				Findings: []ReportFinding{{ID: "finding-1", Summary: "one"}, {ID: "finding-1", Summary: "two"}},
			},
			want: "duplicates id finding-1",
		},
		{
			name: "canonical claim collision",
			report: ResearchReport{
				Kind: ReportKindResearch,
				Claims: []ReportClaim{
					{ID: "claim 1", Summary: "one"},
					{ID: "claim-1", Summary: "two"},
				},
			},
			want: "duplicates id claim-1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResearchReport(tc.report)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected duplicate id error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateResearchReport_HappyPath(t *testing.T) {
	report := ResearchReport{
		Kind:   ReportKindResearch,
		Claims: []ReportClaim{{ID: "c1", Summary: "one"}},
		Findings: []ReportFinding{
			{Summary: "f", ClaimIDs: []string{"c1"}},
		},
	}
	if err := ValidateResearchReport(report); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestValidateVerificationReport_StatusMustBeKnown(t *testing.T) {
	err := ValidateVerificationReport(VerificationReport{
		Kind:   ReportKindVerification,
		Status: VerificationStatus("bogus"),
	})
	if err == nil {
		t.Fatalf("expected unknown status error")
	}
}

func TestValidateVerificationReport_PerClaimStatusRequired(t *testing.T) {
	err := ValidateVerificationReport(VerificationReport{
		Kind:   ReportKindVerification,
		Status: VerificationStatusSupported,
		PerClaim: []VerificationClaim{
			{ClaimID: "c1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "perClaim[0]") {
		t.Fatalf("expected per-claim status error, got %v", err)
	}
}

func TestValidateVerificationReport_HappyPath(t *testing.T) {
	report := VerificationReport{
		Kind:   ReportKindVerification,
		Status: VerificationStatusSupported,
		PerClaim: []VerificationClaim{
			{ClaimID: "c1", Status: VerificationStatusContradicted},
		},
	}
	if err := ValidateVerificationReport(report); err != nil {
		t.Fatalf("valid verification report rejected: %v", err)
	}
}

func TestValidateSynthesisReport_RequiresAnswer(t *testing.T) {
	err := ValidateSynthesisReport(SynthesisReport{Kind: ReportKindSynthesis})
	if err == nil || !strings.Contains(err.Error(), "answer") {
		t.Fatalf("expected missing-answer error, got %v", err)
	}
}

func TestValidateSynthesisReport_HappyPath(t *testing.T) {
	report := SynthesisReport{Kind: ReportKindSynthesis, Answer: "done"}
	if err := ValidateSynthesisReport(report); err != nil {
		t.Fatalf("valid synthesis report rejected: %v", err)
	}
}
