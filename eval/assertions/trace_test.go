package assertions_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval/assertions"
)

func TestAssertion_BlackboardHasItem(t *testing.T) {
	const runID = "run-bb-has"
	run, h := runToTerminal(t, runID, "x")
	if err := h.Runner().WriteItem(context.Background(), api.BlackboardItem{
		RunID:      runID,
		Type:       api.BlackboardItemFinding,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: "agent"},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Key:        "verdict",
		Payload:    "confirmed",
	}); err != nil {
		t.Fatalf("WriteItem error = %v", err)
	}
	a := assertions.BlackboardHasItem{Selector: api.BlackboardSelector{Keys: []string{"verdict"}}}
	if err := a.Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected matching item to pass, got %v", err)
	}
	miss := assertions.BlackboardHasItem{Selector: api.BlackboardSelector{Keys: []string{"absent"}}}
	if err := miss.Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected non-matching selector to fail")
	}
}

func TestAssertion_ApprovalRequested(t *testing.T) {
	const runID = "run-approval"
	run, h := runToTerminal(t, runID, "x")
	if _, _, err := h.Runner().RequestApproval(context.Background(), api.RequestApprovalCommand{
		RunID:           runID,
		TaskID:          runID + "-task",
		RequestedAction: "delete-record",
		Reason:          "destructive write requires sign-off",
	}); err != nil {
		t.Fatalf("RequestApproval error = %v", err)
	}
	if err := (assertions.ApprovalRequested{}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected any-approval assertion to pass, got %v", err)
	}
	if err := (assertions.ApprovalRequested{Reason: "sign-off"}).Check(context.Background(), run, h); err != nil {
		t.Fatalf("expected reason-substring assertion to pass, got %v", err)
	}
	if err := (assertions.ApprovalRequested{Reason: "unrelated"}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected mismatched reason to fail")
	}
}

func TestAssertion_ApprovalRequested_NoneFails(t *testing.T) {
	run, h := runToTerminal(t, "run-no-approval", "x")
	if err := (assertions.ApprovalRequested{}).Check(context.Background(), run, h); err == nil {
		t.Fatalf("expected failure when no approval was requested")
	}
}
