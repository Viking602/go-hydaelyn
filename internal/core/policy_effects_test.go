package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func TestNormalizePolicyDecision_EmptyAndUnknownEffectFailClosed(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		effect api.PolicyEffect
	}{
		{name: "empty", effect: ""},
		{name: "unknown", effect: "maybe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := api.PolicyDecision{Effect: tt.effect, ApprovalRequired: true}
			if err := normalizePolicyDecision(api.PolicyRequest{Operation: api.PolicyOperationToolCall}, &decision, now); err != nil {
				t.Fatalf("normalizePolicyDecision() error = %v", err)
			}
			if decision.Effect != api.PolicyEffectDeny {
				t.Fatalf("Effect = %q, want deny", decision.Effect)
			}
			if decision.Reason != "policy returned an unknown effect" {
				t.Fatalf("Reason = %q, want unknown-effect denial", decision.Reason)
			}
		})
	}
}

func TestNormalizePolicyDecision_KnownAllowStillAllows(t *testing.T) {
	decision := api.PolicyDecision{Effect: api.PolicyEffectAllow}
	if err := normalizePolicyDecision(api.PolicyRequest{Operation: api.PolicyOperationAction}, &decision, time.Now().UTC()); err != nil {
		t.Fatalf("normalizePolicyDecision() error = %v", err)
	}
	if decision.Effect != api.PolicyEffectAllow {
		t.Fatalf("Effect = %q, want allow", decision.Effect)
	}
}

func TestAuthorize_EmptyPolicyDecisionDoesNotAuthorize(t *testing.T) {
	ctx := context.Background()
	engine := obligationPolicyFunc(func(_ context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
		switch request.Operation {
		case api.PolicyOperationToolCall, api.PolicyOperationAction, api.PolicyOperationHandoff:
			return api.PolicyDecision{}, nil
		default:
			return api.PolicyDecision{Effect: api.PolicyEffectAllow}, nil
		}
	})

	t.Run("tool", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-tool", "worker")
		rt.RegisterTool(api.Tool{Name: "lookup", EffectType: api.ToolEffectReadOnly})
		if _, err := rt.InvokeTool(ctx, ToolInvocation{RunID: run.ID, TaskID: task.ID, ToolName: "lookup"}); !errors.Is(err, api.ErrPolicyDenied) {
			t.Fatalf("InvokeTool() error = %v, want ErrPolicyDenied", err)
		}
		assertUnknownEffectAudit(t, rt, run.ID, api.PolicyOperationToolCall)
	})

	t.Run("action", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-action", "worker")
		lease := leaseTask(ctx, t, rt, run.ID, task.ID, api.HolderAgent, "agent-a")
		if _, err := rt.StartActionAttempt(ctx, StartActionAttemptCommand{
			RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: api.HolderAgent, HolderID: "agent-a",
			TaskVersion: task.Version, ToolName: "write",
		}); !errors.Is(err, api.ErrPolicyDenied) {
			t.Fatalf("StartActionAttempt() error = %v, want ErrPolicyDenied", err)
		}
		assertUnknownEffectAudit(t, rt, run.ID, api.PolicyOperationAction)
	})

	t.Run("handoff", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-handoff", "worker")
		if err := rt.RequestHandoff(ctx, HandoffCommand{
			RunID: run.ID, TaskID: task.ID, FromAgentID: "agent-a", ToAgentID: "agent-b",
			TaskVersion: task.Version,
		}); !errors.Is(err, api.ErrPolicyDenied) {
			t.Fatalf("RequestHandoff() error = %v, want ErrPolicyDenied", err)
		}
		after := mustLoadTask(ctx, t, rt, run.ID, task.ID)
		if after.OwnerAgentID != "agent-a" {
			t.Fatalf("denied handoff changed owner: %#v", after)
		}
		assertUnknownEffectAudit(t, rt, run.ID, api.PolicyOperationHandoff)
	})

	t.Run("report", func(t *testing.T) {
		rt := NewRuntime(Config{PolicyEngine: engine})
		run, task := mustStartWorker(ctx, t, rt, "run-empty-report", "worker")
		lease := leaseTask(ctx, t, rt, run.ID, task.ID, api.HolderAgent, "agent-a")
		if err := rt.SubmitTypedReport(ctx, SubmitTypedReportCommand{
			RunID: run.ID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: api.HolderAgent, HolderID: "agent-a",
			TaskVersion: task.Version,
			Report: api.TypedReport{
				Status:        api.ReportStatusSuccess,
				Summary:       "done",
				ActionOutcome: &api.ActionOutcome{AttemptID: "attempt-1", Status: api.ActionAttemptSucceeded, Output: "ok"},
			},
		}); !errors.Is(err, api.ErrPolicyDenied) {
			t.Fatalf("SubmitTypedReport() error = %v, want ErrPolicyDenied", err)
		}
		after := mustLoadTask(ctx, t, rt, run.ID, task.ID)
		if after.Status == api.TaskStatusCompleted {
			t.Fatalf("denied report completed the task: %#v", after)
		}
		assertUnknownEffectAudit(t, rt, run.ID, api.PolicyOperationAction)
	})
}

func mustStartWorker(ctx context.Context, t *testing.T, rt *Runtime, runID, taskID string) (api.Run, api.Task) {
	t.Helper()
	run, _, err := rt.StartRun(ctx, StartRunCommand{RunID: runID, RootTaskID: "root"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	task, err := rt.CreateTask(ctx, CreateTaskCommand{
		RunID: run.ID, TaskID: taskID, Type: api.TaskTypeWorker,
		OwnerAgentID: "agent-a", AllowsAction: true,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return run, task
}

func assertUnknownEffectAudit(t *testing.T, rt *Runtime, runID string, operation api.PolicyOperation) {
	t.Helper()
	for _, event := range rt.Events(context.Background(), runID) {
		if event.Type != api.EventPolicyDecisionRecorded {
			continue
		}
		if stringFromPayload(event.Payload["operation"]) != string(operation) {
			continue
		}
		if stringFromPayload(event.Payload["effect"]) != string(api.PolicyEffectDeny) {
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
