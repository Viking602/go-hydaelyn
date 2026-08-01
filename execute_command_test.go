package venat

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/model"
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

func TestCommandResultFromCore_ResolveActionAttemptReturnsPublicType(t *testing.T) {
	result := commandResultFromCore(
		api.ResolveActionAttemptCommand{},
		model.ActionAttempt{AttemptID: "attempt-1", Status: model.ActionAttemptSucceeded},
	)
	attempt, ok := result.(api.ActionAttempt)
	if !ok {
		t.Fatalf("ResolveActionAttemptCommand result type = %T, want api.ActionAttempt", result)
	}
	if attempt.AttemptID != "attempt-1" || attempt.Status != api.ActionAttemptSucceeded {
		t.Fatalf("ResolveActionAttemptCommand result = %#v", attempt)
	}
}

func TestPendingResumeTokens_ListsUnconsumedToken(t *testing.T) {
	r := New()
	ctx := context.Background()
	run, task, err := r.StartRun(ctx, api.StartRunCommand{Request: "bulk recovery"})
	if err != nil {
		t.Fatalf("StartRun error = %v", err)
	}
	_, token, err := r.RequestApproval(ctx, api.RequestApprovalCommand{
		RunID:           run.ID,
		TaskID:          task.ID,
		RequestedAction: "deploy",
		Reason:          "needs human sign-off",
	})
	if err != nil {
		t.Fatalf("RequestApproval error = %v", err)
	}

	pending, err := r.PendingResumeTokens(ctx, api.ResumeTokenSelector{RunID: run.ID})
	if err != nil {
		t.Fatalf("PendingResumeTokens error = %v", err)
	}
	if len(pending) != 1 || pending[0].TokenID != token.TokenID {
		t.Fatalf("PendingResumeTokens = %+v, want exactly the approval's token %s", pending, token.TokenID)
	}
}
