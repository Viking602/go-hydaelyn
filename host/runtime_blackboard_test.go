package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/team"
)

type verificationProvider struct{}

func (verificationProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "verification"}
}

func (verificationProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	last := request.Messages[len(request.Messages)-1].Text
	if strings.HasSuffix(request.Metadata["taskId"], "-verify") {
		text := "supported"
		if strings.Contains(last, "reject branch") {
			text = "contradicted"
		}
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventTextDelta, Text: text},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: last},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestRuntimePublishesBlackboardAndSynthesizesOnlyVerifiedClaims(t *testing.T) {
	runtime := New(Config{})
	runtime.RegisterProvider("verification", verificationProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{
		Name:     "supervisor",
		Role:     team.RoleSupervisor,
		Provider: "verification",
		Model:    "test",
	})
	runtime.RegisterProfile(team.Profile{
		Name:     "researcher",
		Role:     team.RoleResearcher,
		Provider: "verification",
		Model:    "test",
	})

	state, err := runtime.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher", "researcher"},
		Input: map[string]any{
			"query":               "verification flow",
			"subqueries":          []string{"keep branch", "reject branch"},
			"requireVerification": true,
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Blackboard == nil {
		t.Fatalf("expected blackboard state, got %#v", state)
	}
	if len(state.Blackboard.Claims) != 2 {
		t.Fatalf("expected published claims, got %#v", state.Blackboard)
	}
	if len(state.Blackboard.Verifications) != 2 {
		t.Fatalf("expected verification results, got %#v", state.Blackboard)
	}
	if state.Result == nil || len(state.Result.Findings) != 1 {
		t.Fatalf("expected only supported claim in final result, got %#v", state.Result)
	}
	if state.Result.Findings[0].Summary != "keep branch" {
		t.Fatalf("expected supported claim to survive synthesis, got %#v", state.Result.Findings)
	}
	if strings.Contains(state.Result.Summary, "reject branch") {
		t.Fatalf("expected contradicted claim to be excluded, got %#v", state.Result)
	}
}

func TestVerificationProviderUsesTaskMetadata(t *testing.T) {
	stream, err := (verificationProvider{}).Stream(context.Background(), provider.Request{
		Messages: []message.Message{message.NewText(message.RoleUser, "reject branch")},
		Metadata: map[string]string{"taskId": "task-1-verify"},
	})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if event.Text != "contradicted" {
		t.Fatalf("expected contradicted verification output, got %#v", event)
	}
}

func TestCollaborationBlackboard_RequiresVerifierNamespaces(t *testing.T) {
	runtime := New(Config{})
	board := &blackboard.State{
		Claims: []blackboard.Claim{{
			ID:     "claim-1",
			TaskID: "impl-1",
		}},
		Findings: []blackboard.Finding{{
			ID:       "finding-1",
			TaskID:   "impl-1",
			Summary:  "verified finding",
			ClaimIDs: []string{"claim-1"},
		}},
		Verifications: []blackboard.VerificationResult{{
			ClaimID: "claim-1",
			Status:  blackboard.VerificationStatusSupported,
		}},
	}
	for _, exchange := range []blackboard.Exchange{
		{
			Key:       "design.doc",
			Namespace: "impl.impl-1",
			TaskID:    "impl-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "raw implementation output",
		},
		{
			Key:       "design.doc",
			Namespace: "review.review-1",
			TaskID:    "review-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "review comments",
		},
		{
			Key:       "design.doc",
			Namespace: "verify.verify-1",
			TaskID:    "verify-1",
			Version:   1,
			ValueType: blackboard.ExchangeValueTypeText,
			Text:      "verifier-approved output",
		},
	} {
		if _, err := board.UpsertExchangeCAS(exchange); err != nil {
			t.Fatalf("UpsertExchangeCAS() error = %v", err)
		}
	}

	guardedTask := team.Task{
		ID:               "task-synthesize",
		Kind:             team.TaskKindSynthesize,
		Reads:            []string{"design.doc", "supported_findings"},
		VerifierRequired: true,
	}

	materialized, text := runtime.materializeTaskInputs(team.RunState{Blackboard: board}, guardedTask)
	if len(materialized) != 1 {
		t.Fatalf("expected guarded synthesis to ignore non-verifier namespaces and unsupported fallback, got %#v", materialized)
	}
	if materialized[0].Namespace != "verify.verify-1" || materialized[0].Text != "verifier-approved output" {
		t.Fatalf("expected only verifier namespace exchange to materialize, got %#v", materialized)
	}
	if strings.Contains(text, "raw implementation output") || strings.Contains(text, "review comments") || strings.Contains(text, "verified finding") {
		t.Fatalf("expected guarded synthesis text to exclude unapproved namespaces, got %q", text)
	}

	if _, err := board.UpsertExchangeCAS(blackboard.Exchange{
		Key:        "supported_findings",
		Namespace:  "verify.verify-1",
		TaskID:     "verify-1",
		Version:    1,
		ValueType:  blackboard.ExchangeValueTypeFindingRef,
		Text:       "verified finding",
		ClaimIDs:   []string{"claim-1"},
		FindingIDs: []string{"finding-1"},
	}); err != nil {
		t.Fatalf("UpsertExchangeCAS() verifier finding error = %v", err)
	}

	materialized, text = runtime.materializeTaskInputs(team.RunState{Blackboard: board}, guardedTask)
	if len(materialized) != 2 {
		t.Fatalf("expected guarded synthesis to consume verifier namespaces only, got %#v", materialized)
	}
	for _, item := range materialized {
		if item.Namespace != "verify.verify-1" {
			t.Fatalf("expected only verifier namespaces, got %#v", materialized)
		}
	}
	if strings.Contains(text, "raw implementation output") || strings.Contains(text, "review comments") {
		t.Fatalf("expected guarded synthesis text to exclude implementation/review namespaces, got %q", text)
	}
	if !strings.Contains(text, "verifier-approved output") || !strings.Contains(text, "verified finding") {
		t.Fatalf("expected guarded synthesis text to include verifier-approved exchanges, got %q", text)
	}
}
