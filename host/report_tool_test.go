package host

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestReportKindForTaskUsesExpectedReportKindForPanelTasks(t *testing.T) {
	task := team.Task{Kind: team.TaskKindResearch, ExpectedReportKind: team.ReportKindResearch}
	kind, ok := reportKindForTask(task)
	if !ok || kind != team.ReportKindResearch {
		t.Fatalf("expected research report kind from task contract, got %s ok=%v", kind, ok)
	}

	legacy := team.Task{Kind: team.TaskKindResearch}
	if _, ok := reportKindForTask(legacy); ok {
		t.Fatalf("expected legacy research task to keep prose fallback")
	}
}

func TestValidTypedReportAcceptsResearchAndVerification(t *testing.T) {
	research := mustJSON(t, map[string]any{
		team.ReportKey: map[string]any{
			"kind": string(team.ReportKindResearch),
			"claims": []map[string]any{{
				"id":      "claim-1",
				"summary": "panel supports typed research reports",
			}},
		},
	})
	if !validTypedReport(team.ReportKindResearch, research) {
		t.Fatalf("expected valid research report")
	}

	verify := mustJSON(t, map[string]any{
		team.ReportKey: map[string]any{
			"kind":   string(team.ReportKindVerification),
			"status": string(team.VerificationStatusSupported),
			"perClaim": []map[string]any{{
				"claimId": "claim-1",
				"status":  string(team.VerificationStatusSupported),
			}},
		},
	})
	if !validTypedReport(team.ReportKindVerification, verify) {
		t.Fatalf("expected valid verification report")
	}
}

func TestValidTypedReportRejectsFindingsOnlyResearch(t *testing.T) {
	research := mustJSON(t, map[string]any{
		team.ReportKey: map[string]any{
			"kind": string(team.ReportKindResearch),
			"findings": []map[string]any{{
				"id":      "finding-1",
				"summary": "finding without claim",
			}},
		},
	})
	if validTypedReport(team.ReportKindResearch, research) {
		t.Fatalf("expected findings-only research report to be rejected")
	}
}

func TestValidTypedReportRejectsVerificationWithoutPerClaim(t *testing.T) {
	verify := mustJSON(t, map[string]any{
		team.ReportKey: map[string]any{
			"kind":       string(team.ReportKindVerification),
			"status":     string(team.VerificationStatusSupported),
			"confidence": 0.9,
		},
	})
	if validTypedReport(team.ReportKindVerification, verify) {
		t.Fatalf("expected overall-only verification report to be rejected")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(payload)
}
