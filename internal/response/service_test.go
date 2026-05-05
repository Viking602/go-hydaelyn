package response_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	"github.com/Viking602/go-hydaelyn/internal/response"
)

func TestApplyObligationsRedactsEmailAndInternalTrace(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	message := model.UserMessage{
		RunID:   "run-1",
		TaskID:  "task-1",
		Payload: "send to owner@example.com\ninternal trace: span-1\nkeep this",
	}
	out, err := response.ApplyObligations(ctx, uow, message, model.PolicyDecision{
		Obligations: []model.PolicyObligation{
			{Kind: model.ObligationRedactFields},
			{Kind: model.ObligationHideInternalTrace},
		},
	})
	if err != nil {
		t.Fatalf("ApplyObligations() error = %v", err)
	}
	if out.Payload != "send to [redacted-email]\nkeep this" {
		t.Fatalf("redacted payload = %q", out.Payload)
	}
}

func TestApplyObligationsRecordsUnsupportedObligation(t *testing.T) {
	ctx := context.Background()
	provider := memory.NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	_, err = response.ApplyObligations(ctx, uow, model.UserMessage{RunID: "run-1", TaskID: "task-1"}, model.PolicyDecision{
		DecisionID: "decision-1",
		Obligations: []model.PolicyObligation{{
			Kind:   model.ObligationKind("unsupported"),
			Target: "payload",
		}},
	})
	if !errors.Is(err, model.ErrPolicyObligationFailed) {
		t.Fatalf("ApplyObligations() error = %v, want ErrPolicyObligationFailed", err)
	}
	events, err := uow.Events().ListEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Type != model.EventPolicyObligationFailed {
		t.Fatalf("unsupported obligation events = %#v", events)
	}
}
