package capability

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/storage"
)

type recordingPolicyOutcomeRecorder struct {
	events []storage.PolicyOutcomeEvent
}

func (r *recordingPolicyOutcomeRecorder) RecordPolicyOutcome(_ context.Context, event storage.PolicyOutcomeEvent) {
	r.events = append(r.events, event)
}

func TestDeniedCapabilityHasNoSideEffect(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	sideEffects := 0
	invoker.Use(RequirePermissions())
	invoker.Register(TypeTool, "delete", func(context.Context, Call) (Result, error) {
		sideEffects++
		return Result{Output: "ok"}, nil
	})

	recorder := &recordingPolicyOutcomeRecorder{}
	_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), Call{
		Type:        TypeTool,
		Name:        "delete",
		Permissions: []Permission{{Name: "tool:delete", Granted: false}},
	})
	if err == nil {
		t.Fatal("expected permission denial")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindPermission {
		t.Fatalf("expected permission error, got %v", err)
	}
	if sideEffects != 0 {
		t.Fatalf("expected denied capability to skip handler, got %d side effects", sideEffects)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected 1 policy outcome, got %#v", recorder.events)
	}
	if recorder.events[0].Policy != policyCapabilityPermission || recorder.events[0].Outcome != "denied" {
		t.Fatalf("unexpected policy outcome %#v", recorder.events[0])
	}
	if recorder.events[0].Severity != "error" || !recorder.events[0].Blocking {
		t.Fatalf("expected blocking error policy outcome, got %#v", recorder.events[0])
	}
}

func TestApprovalRequiredPausesFlow(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	handlerCalls := 0
	invoker.Use(RequireApproval())
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		handlerCalls++
		return Result{Output: "ok"}, nil
	})

	recorder := &recordingPolicyOutcomeRecorder{}
	_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), Call{Type: TypeTool, Name: "deploy"})
	if err == nil {
		t.Fatal("expected approval pause")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindApproval {
		t.Fatalf("expected approval error, got %v", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("expected approval middleware to pause before handler, got %d calls", handlerCalls)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected 1 policy outcome, got %#v", recorder.events)
	}
	if recorder.events[0].Policy != policyCapabilityApproval || recorder.events[0].Outcome != "paused" {
		t.Fatalf("unexpected policy outcome %#v", recorder.events[0])
	}
	if recorder.events[0].Severity != "warning" || !recorder.events[0].Blocking {
		t.Fatalf("expected blocking warning policy outcome, got %#v", recorder.events[0])
	}
	if got := recorder.events[0].Evidence.Metadata["capabilityName"]; got != "deploy" {
		t.Fatalf("expected capability metadata, got %#v", recorder.events[0].Evidence)
	}
	approved, err := invoker.Invoke(WithApproval(context.Background(), TypeTool, "deploy"), Call{Type: TypeTool, Name: "deploy"})
	if err != nil {
		t.Fatalf("approved invoke error = %v", err)
	}
	if approved.Output != "ok" || handlerCalls != 1 {
		t.Fatalf("expected approved call to run once, result=%#v handlerCalls=%d", approved, handlerCalls)
	}
}
