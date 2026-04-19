package capability

import (
	"context"
	"errors"
	"testing"
)

func TestApprovalBypass(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(RequireApproval())
	handlerCalls := 0
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		handlerCalls++
		return Result{Output: "approved"}, nil
	})

	t.Run("metadata cannot self approve", func(t *testing.T) {
		recorder := &recordingPolicyOutcomeRecorder{}
		_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), Call{
			Type:     TypeTool,
			Name:     "deploy",
			Metadata: map[string]string{"approved": "true", "approvedBy": "model"},
		})
		if err == nil {
			t.Fatal("expected approval denial")
		}
		var capErr *Error
		if !errors.As(err, &capErr) || capErr.Kind != ErrorKindApproval {
			t.Fatalf("expected approval error, got %v", err)
		}
		if handlerCalls != 0 {
			t.Fatalf("expected metadata bypass to skip handler, got %d", handlerCalls)
		}
		if len(recorder.events) != 1 || recorder.events[0].Outcome != "paused" {
			t.Fatalf("expected paused policy outcome, got %#v", recorder.events)
		}
	})

	t.Run("pause then resume with explicit approval", func(t *testing.T) {
		recorder := &recordingPolicyOutcomeRecorder{}
		_, err := invoker.Invoke(WithPolicyOutcomeRecorder(context.Background(), recorder), Call{Type: TypeTool, Name: "deploy"})
		if err == nil {
			t.Fatal("expected initial pause")
		}
		if handlerCalls != 0 {
			t.Fatalf("expected paused call to skip handler, got %d", handlerCalls)
		}
		result, err := invoker.Invoke(WithApproval(context.Background(), TypeTool, "deploy"), Call{Type: TypeTool, Name: "deploy"})
		if err != nil {
			t.Fatalf("approved invoke error = %v", err)
		}
		if result.Output != "approved" || handlerCalls != 1 {
			t.Fatalf("expected approved call to run once, result=%#v handlerCalls=%d", result, handlerCalls)
		}
	})
}
