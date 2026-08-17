package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

func TestNormalizePolicyDecision_EmptyAndUnknownEffectFailClosed(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		effect model.PolicyEffect
	}{
		{name: "empty", effect: ""},
		{name: "unknown", effect: "maybe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := model.PolicyDecision{Effect: tt.effect, ApprovalRequired: true}
			if err := normalizePolicyDecision(model.PolicyRequest{Operation: model.PolicyOperationToolCall}, &decision, now); err != nil {
				t.Fatalf("normalizePolicyDecision() error = %v", err)
			}
			if decision.Effect != model.PolicyEffectDeny {
				t.Fatalf("Effect = %q, want deny", decision.Effect)
			}
			if decision.Reason != "policy returned an unknown effect" {
				t.Fatalf("Reason = %q, want unknown-effect denial", decision.Reason)
			}
		})
	}
}

func TestNormalizePolicyDecision_KnownAllowStillAllows(t *testing.T) {
	decision := model.PolicyDecision{Effect: model.PolicyEffectAllow}
	if err := normalizePolicyDecision(model.PolicyRequest{Operation: model.PolicyOperationAction}, &decision, time.Now().UTC()); err != nil {
		t.Fatalf("normalizePolicyDecision() error = %v", err)
	}
	if decision.Effect != model.PolicyEffectAllow {
		t.Fatalf("Effect = %q, want allow", decision.Effect)
	}
}

func TestAuthorize_EmptyPolicyDecisionDoesNotAuthorize(t *testing.T) {
	ctx := context.Background()
	engine := obligationPolicyFunc(func(_ context.Context, request model.PolicyRequest) (model.PolicyDecision, error) {
		switch request.Operation {
		case model.PolicyOperationToolCall, model.PolicyOperationAction, model.PolicyOperationHandoff:
			return model.PolicyDecision{}, nil
		default:
			return model.PolicyDecision{Effect: model.PolicyEffectAllow}, nil
		}
	})

	t.Run("tool", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-tool", "worker")
		rt.RegisterTool(model.Tool{Name: "lookup", EffectType: model.ToolEffectReadOnly})
		if _, err := rt.InvokeTool(ctx, ToolInvocation{RunID: run.ID, TaskID: task.ID, ToolName: "lookup"}); !errors.Is(err, model.ErrPolicyDenied) {
			t.Fatalf("InvokeTool() error = %v, want ErrPolicyDenied", err)
		}
		assertUnknownEffectAudit(t, rt, run.ID, model.PolicyOperationToolCall)
	})

	t.Run("action", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-action", "worker")
		lease := leaseTask(ctx, t, rt, run.ID, task.ID, model.HolderAgent, "agent-a")
		if _, err := rt.StartActionAttempt(ctx, StartActionAttemptCommand{
			RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: model.HolderAgent, HolderID: "agent-a",
			TaskVersion: task.Version, ToolName: "write",
		}); !errors.Is(err, model.ErrPolicyDenied) {
			t.Fatalf("StartActionAttempt() error = %v, want ErrPolicyDenied", err)
		}
		assertUnknownEffectAudit(t, rt, run.ID, model.PolicyOperationAction)
	})

	t.Run("handoff", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-handoff", "worker")
		if err := rt.RequestHandoff(ctx, HandoffCommand{
			RunID: run.ID, TaskID: task.ID, FromAgentID: "agent-a", ToAgentID: "agent-b",
			TaskVersion: task.Version,
		}); !errors.Is(err, model.ErrPolicyDenied) {
			t.Fatalf("RequestHandoff() error = %v, want ErrPolicyDenied", err)
		}
		after := mustLoadTask(ctx, t, rt, run.ID, task.ID)
		if after.OwnerAgentID != "agent-a" {
			t.Fatalf("denied handoff changed owner: %#v", after)
		}
		assertUnknownEffectAudit(t, rt, run.ID, model.PolicyOperationHandoff)
	})

	t.Run("report", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-report", "worker")
		lease := leaseTask(ctx, t, rt, run.ID, task.ID, model.HolderAgent, "agent-a")
		if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
			RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: model.HolderAgent, HolderID: "agent-a",
			TaskVersion: task.Version,
			Report: model.TypedReport{
				Status:        model.ReportStatusSuccess,
				Summary:       "done",
				ActionOutcome: &model.ActionOutcome{AttemptID: "attempt-1", Status: model.ActionAttemptSucceeded, Output: "ok"},
			},
		}); !errors.Is(err, model.ErrPolicyDenied) {
			t.Fatalf("SubmitTypedReport() error = %v, want ErrPolicyDenied", err)
		}
		after := mustLoadTask(ctx, t, rt, run.ID, task.ID)
		if after.Status == model.TaskStatusCompleted {
			t.Fatalf("denied report completed the task: %#v", after)
		}
		assertUnknownEffectAudit(t, rt, run.ID, model.PolicyOperationAction)
	})
}

func mustStartWorker(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) (model.Run, model.Task) {
	t.Helper()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: runID, RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID: run.ID, TaskID: taskID, Type: model.TaskTypeWorker,
		OwnerAgentID: "agent-a", AllowsAction: true,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return run, task
}

func assertUnknownEffectAudit(t *testing.T, rt *Runtime, runID string, operation model.PolicyOperation) {
	t.Helper()
	for _, event := range rt.Events(context.Background(), runID) {
		if event.Type != model.EventPolicyDecisionRecorded {
			continue
		}
		if stringFromPayload(event.Payload["operation"]) != string(operation) {
			continue
		}
		if stringFromPayload(event.Payload["effect"]) != string(model.PolicyEffectDeny) {
			t.Fatalf("audit effect = %#v, want deny", event.Payload)
		}
		if stringFromPayload(event.Payload["reason"]) != "policy returned an unknown effect" {
			t.Fatalf("audit reason = %#v, want unknown-effect denial", event.Payload)
		}
		if stringFromPayload(event.Payload["decisionId"]) == "" {
			t.Fatalf("audit missing decisionId: %#v", event.Payload)
		}
		return
	}
	t.Fatalf("missing PolicyDecisionRecorded for %s: %#v", operation, rt.Events(context.Background(), runID))
}
