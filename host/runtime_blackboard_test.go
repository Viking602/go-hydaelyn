package host

import (
	"context"
	"strings"
	"testing"

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
