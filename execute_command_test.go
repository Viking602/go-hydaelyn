package hydaelyn

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

func TestExecuteCommand_StartRunReturnsTypedResult(t *testing.T) {
	r := New()
	ctx := context.Background()
	result, err := r.ExecuteCommand(ctx, api.StartRunCommand{Request: "execute-command-typed-start"})
	if err != nil {
		t.Fatalf("ExecuteCommand(StartRunCommand) error = %v", err)
	}
	started, ok := result.(api.StartRunResult)
	if !ok {
		t.Fatalf("ExecuteCommand(StartRunCommand) returned %T, want api.StartRunResult", result)
	}
	if started.Run.ID == "" {
		t.Fatalf("expected non-empty Run.ID, got %#v", started.Run)
	}
	if started.RootTask.ID == "" {
		t.Fatalf("expected non-empty RootTask.ID, got %#v", started.RootTask)
	}
	if started.RootTask.RunID != started.Run.ID {
		t.Fatalf("RootTask.RunID %q != Run.ID %q", started.RootTask.RunID, started.Run.ID)
	}
}

func TestExecuteCommand_RequestApprovalReturnsTypedResult(t *testing.T) {
	r := New()
	ctx := context.Background()
	run, task, err := r.StartRun(ctx, api.StartRunCommand{Request: "execute-command-typed-approval"})
	if err != nil {
		t.Fatalf("StartRun error = %v", err)
	}
	result, err := r.ExecuteCommand(ctx, api.RequestApprovalCommand{
		RunID:           run.ID,
		TaskID:          task.ID,
		RequestedAction: "smoke",
		Reason:          "smoke",
	})
	if err != nil {
		t.Fatalf("ExecuteCommand(RequestApprovalCommand) error = %v", err)
	}
	requested, ok := result.(api.RequestApprovalResult)
	if !ok {
		t.Fatalf("ExecuteCommand(RequestApprovalCommand) returned %T, want api.RequestApprovalResult", result)
	}
	if requested.Approval.ApprovalID == "" {
		t.Fatalf("expected non-empty Approval.ApprovalID, got %#v", requested.Approval)
	}
	if requested.Token.TokenID == "" {
		t.Fatalf("expected non-empty Token.TokenID, got %#v", requested.Token)
	}
	if requested.Approval.RunID != run.ID {
		t.Fatalf("Approval.RunID %q != Run.ID %q", requested.Approval.RunID, run.ID)
	}
}
