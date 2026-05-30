package adapter

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func TestTaskRoundTripPreservesPublicFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	original := api.Task{
		ID:              "task-1",
		RunID:           "run-1",
		ParentTaskID:    "root",
		Type:            api.TaskTypeWorker,
		Goal:            "investigate",
		AssignedAgentID: "agent-a",
		OwnerAgentID:    "agent-a",
		OwnerComponent:  "planner",
		Status:          api.TaskStatusRunning,
		Version:         3,
		Attempts:        2,
		HandoffCount:    1,
		OwnerHistory:    []string{"planner", "agent-a"},
		AllowsAction:    true,
		Tags:            []string{"incident", "p1"},
		CompletionCriteria: []string{
			"summary written",
		},
		DependsOn:          []string{"dep-1", "dep-2"},
		AwaitMode:          api.AwaitModeQuorum,
		AwaitQuorum:        2,
		OnDependencyFailed: api.OnDependencyFailedSkip,
		ReadSelectors: []api.BlackboardSelector{{
			RunID:       "run-1",
			ItemTypes:   []api.BlackboardItemType{api.BlackboardItemEvidence},
			SourceTypes: []api.SourceType{api.SourceAgent},
			SourceIDs:   []string{"agent-a"},
			Visibility:  api.BlackboardVisibilityAgentVisible,
			Limit:       5,
		}},
		WriteTargets: []string{"summary"},
		RetryPolicy:  api.RetryPolicy{MaxAttempts: 4, Backoff: 3 * time.Second},
		PolicyDecisions: []api.PolicyDecision{{
			DecisionID: "dec-1",
			Effect:     api.PolicyEffectRequireApproval,
			Reason:     "high-risk tool",
			Obligations: []api.PolicyObligation{{
				Kind:   api.ObligationRequireHumanApproval,
				Target: "tool",
			}},
			ApprovalRequired: true,
			ExpiresAt:        now.Add(5 * time.Minute),
			Metadata:         map[string]string{"source": "policy"},
		}},
		Result: &api.TypedReport{
			Status:  api.ReportStatusNeedsApproval,
			Summary: "waiting",
			Structured: map[string]any{
				"step": "rollback",
			},
		},
		Error:     "blocked",
		CreatedAt: now,
		UpdatedAt: now.Add(time.Minute),
	}

	roundTrip := TaskFromModel(TaskToModel(original))
	if !reflect.DeepEqual(roundTrip, original) {
		t.Fatalf("task round-trip mismatch\nwant: %#v\n got: %#v", original, roundTrip)
	}
}

func TestTaskAdapterRoundTripsV08Fields(t *testing.T) {
	original := api.Task{
		ID:     "task-v08",
		RunID:  "run-v08",
		Type:   api.TaskTypeWorker,
		Status: api.TaskStatusCreated,
		Budget: &api.TaskBudget{
			MaxTokens:    12_000,
			MaxWallClock: 2 * time.Minute,
			MaxToolCalls: 3,
			MaxSteps:     8,
		},
		InputSchema:  json.RawMessage(`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	}

	roundTrip := TaskFromModel(TaskToModel(original))

	if !reflect.DeepEqual(roundTrip.Budget, original.Budget) {
		t.Fatalf("Budget mismatch\nwant: %#v\n got: %#v", original.Budget, roundTrip.Budget)
	}
	if string(roundTrip.InputSchema) != string(original.InputSchema) {
		t.Fatalf("InputSchema mismatch\nwant: %s\n got: %s", original.InputSchema, roundTrip.InputSchema)
	}
	if string(roundTrip.OutputSchema) != string(original.OutputSchema) {
		t.Fatalf("OutputSchema mismatch\nwant: %s\n got: %s", original.OutputSchema, roundTrip.OutputSchema)
	}
}

func TestTypedReportAdapterRoundTripsFailureFields(t *testing.T) {
	original := api.TypedReport{
		Status:      api.ReportStatusFailed,
		Summary:     "budget ran out",
		Kind:        "budget_exhausted",
		Retryable:   true,
		Escalatable: false,
		Structured:  map[string]any{"dimension": "max tokens"},
	}

	roundTrip := TypedReportFromModel(TypedReportToModel(original))

	if !reflect.DeepEqual(roundTrip, original) {
		t.Fatalf("TypedReport round-trip mismatch\nwant: %#v\n got: %#v", original, roundTrip)
	}
	// Distinct true/false values guard against the two booleans being swapped
	// in the adapter.
	if roundTrip.Kind != "budget_exhausted" || !roundTrip.Retryable || roundTrip.Escalatable {
		t.Fatalf("failure fields not preserved: %#v", roundTrip)
	}
}

func TestErrorBridgingPreservesErrorsIs(t *testing.T) {
	apiErr := ErrorToAPI(model.ErrPolicyDenied)
	if !errors.Is(apiErr, api.ErrPolicyDenied) {
		t.Fatalf("bridged error should match api sentinel")
	}
	if !errors.Is(apiErr, model.ErrPolicyDenied) {
		t.Fatalf("bridged error should still match original model sentinel")
	}

	coreErr := ErrorToCore(api.ErrWaitTimeout)
	if !errors.Is(coreErr, core.ErrWaitTimeout) {
		t.Fatalf("bridged error should match core sentinel")
	}
	if !errors.Is(coreErr, api.ErrWaitTimeout) {
		t.Fatalf("bridged error should still match original api sentinel")
	}
}
