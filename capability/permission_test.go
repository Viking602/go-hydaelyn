package capability

import (
	"context"
	"errors"
	"testing"
)

func TestPermissionDenied(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(RequirePermissions())
	handlerCalls := 0
	invoker.Register(TypeTool, "dangerous", func(context.Context, Call) (Result, error) {
		handlerCalls++
		return Result{Output: "should-not-run"}, nil
	})

	recorder := &recordingPolicyOutcomeRecorder{}
	_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), Call{
		Type: TypeTool,
		Name: "dangerous",
		Permissions: []Permission{{
			Name:    "tool:dangerous",
			Granted: false,
		}},
	})
	if err == nil {
		t.Fatal("expected permission denial")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindPermission {
		t.Fatalf("expected permission error, got %v", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("expected denied call to skip handler, got %d", handlerCalls)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("expected 1 policy outcome, got %#v", recorder.events)
	}
	event := recorder.events[0]
	if event.Policy != policyCapabilityPermission || event.Outcome != "denied" {
		t.Fatalf("unexpected permission policy outcome %#v", event)
	}
	if event.Evidence == nil || event.Evidence.Metadata["permission"] != "tool:dangerous" {
		t.Fatalf("expected permission evidence, got %#v", event.Evidence)
	}
}
