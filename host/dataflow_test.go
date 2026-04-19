package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type dataflowProvider struct{}

func (dataflowProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "dataflow"}
}

func (dataflowProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	lastText := ""
	var lastTool *message.ToolResult
	for idx := len(request.Messages) - 1; idx >= 0; idx-- {
		current := request.Messages[idx]
		if lastTool == nil && current.ToolResult != nil {
			lastTool = current.ToolResult
		}
		if lastText == "" && strings.TrimSpace(current.Text) != "" {
			lastText = current.Text
		}
	}
	if request.Metadata["taskId"] == "research-1" && lastTool == nil {
		args, err := json.Marshal(map[string]any{
			"key":     "branch.report",
			"summary": lastText,
		})
		if err != nil {
			return nil, err
		}
		return provider.NewSliceStream([]provider.Event{
			{
				Kind: provider.EventToolCall,
				ToolCall: &message.ToolCall{
					ID:        "call-1",
					Name:      "artifact_tool",
					Arguments: args,
				},
			},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	if lastTool != nil {
		lastText = lastTool.Content
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: lastText},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

type artifactTool struct {
	artifacts storage.ArtifactStore
}

func (t artifactTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "artifact_tool",
		Description: "create an artifact-backed structured task output",
		InputSchema: tool.Schema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"key":     {Type: "string"},
				"summary": {Type: "string"},
			},
			Required: []string{"key", "summary"},
		},
	}
}

func (t artifactTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var input struct {
		Key     string `json:"key"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return tool.Result{}, err
	}
	artifactID := "artifact-" + strings.ReplaceAll(input.Key, ".", "-")
	if err := t.artifacts.Save(ctx, storage.Artifact{
		ID:        artifactID,
		Name:      input.Key + ".txt",
		Data:      []byte(input.Summary),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return tool.Result{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"key":         input.Key,
		"summary":     input.Summary,
		"artifactIds": []string{artifactID},
	})
	if err != nil {
		return tool.Result{}, err
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       "artifact_tool",
		Content:    input.Summary,
		Structured: payload,
	}, nil
}

func TestPublishesStructuredOutputsToSessionsBlackboardAndReplay(t *testing.T) {
	driver := storage.NewMemoryDriver()
	runner := New(Config{Storage: driver})
	runner.RegisterProvider("dataflow", dataflowProvider{})
	runner.RegisterTool(artifactTool{artifacts: driver.Artifacts()})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "dataflow", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "dataflow", Model: "test", ToolNames: []string{"artifact_tool"}})

	if err := runner.RegisterPlugin(plugin.Spec{
		Type: plugin.TypePlanner,
		Name: "dataflow-planner",
		Component: fakePlanner{
			planFn: func(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
				return planner.Plan{
					Goal: "dataflow",
					Tasks: []planner.TaskSpec{
						{
							ID:            "research-1",
							Kind:          string(team.TaskKindResearch),
							Title:         "branch",
							Input:         "branch payload",
							RequiredRole:  team.RoleResearcher,
							Writes:        []string{"branch.report"},
							Publish:       []team.OutputVisibility{team.OutputVisibilityPrivate, team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
							FailurePolicy: team.FailurePolicyFailFast,
						},
						{
							ID:              "synth-1",
							Kind:            string(team.TaskKindSynthesize),
							Title:           "synthesize",
							Input:           "compose final answer",
							AssigneeAgentID: "supervisor",
							Reads:           []string{"branch.report"},
							Publish:         []team.OutputVisibility{team.OutputVisibilityShared},
							DependsOn:       []string{"research-1"},
							FailurePolicy:   team.FailurePolicyFailFast,
						},
					},
				}, nil
			},
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "dataflow-planner",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "dataflow"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed state, got %#v", state)
	}
	if state.Blackboard == nil {
		t.Fatalf("expected blackboard state, got %#v", state)
	}

	research := state.Tasks[0]
	synth := state.Tasks[1]
	if research.Result == nil || research.Result.Structured["key"] != "branch.report" {
		t.Fatalf("expected structured output on research task, got %#v", research.Result)
	}
	if len(research.Result.ArtifactIDs) != 1 {
		t.Fatalf("expected artifact ids on research result, got %#v", research.Result)
	}
	if len(state.Blackboard.ExchangesForKey("branch.report")) != 1 {
		t.Fatalf("expected named blackboard exchange, got %#v", state.Blackboard.Exchanges)
	}
	if synth.Result == nil || !strings.Contains(synth.Result.Summary, "branch payload") {
		t.Fatalf("expected synth task to consume explicit read inputs, got %#v", synth.Result)
	}

	teamSnapshot, err := runner.GetSession(context.Background(), state.SessionID)
	if err != nil {
		t.Fatalf("GetSession(team) error = %v", err)
	}
	foundSharedOutput := false
	for _, msg := range teamSnapshot.Messages {
		if msg.Metadata["taskId"] == "research-1" {
			foundSharedOutput = true
			break
		}
	}
	if !foundSharedOutput {
		t.Fatalf("expected shared task output message, got %#v", teamSnapshot.Messages)
	}

	workerSnapshot, err := runner.GetSession(context.Background(), research.SessionID)
	if err != nil {
		t.Fatalf("GetSession(worker) error = %v", err)
	}
	foundPrivateOutput := false
	for _, msg := range workerSnapshot.Messages {
		if msg.Metadata["taskOutput"] == "true" {
			foundPrivateOutput = true
			break
		}
	}
	if !foundPrivateOutput {
		t.Fatalf("expected private task output publication, got %#v", workerSnapshot.Messages)
	}

	events, err := runner.TeamEvents(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("TeamEvents() error = %v", err)
	}
	foundInputsEvent := false
	foundOutputsEvent := false
	for _, event := range events {
		switch event.Type {
		case storage.EventTaskInputsMaterialized:
			foundInputsEvent = true
		case storage.EventTaskOutputsPublished:
			foundOutputsEvent = true
		}
	}
	if !foundInputsEvent || !foundOutputsEvent {
		t.Fatalf("expected task dataflow events, got %#v", events)
	}

	replayed, err := runner.ReplayTeamState(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("ReplayTeamState() error = %v", err)
	}
	if replayed.Blackboard == nil || len(replayed.Blackboard.ExchangesForKey("branch.report")) != 1 {
		t.Fatalf("expected replay to restore exchanges, got %#v", replayed.Blackboard)
	}
	if replayed.Tasks[0].Result == nil || len(replayed.Tasks[0].Result.ArtifactIDs) != 1 {
		t.Fatalf("expected replay to preserve task artifact refs, got %#v", replayed.Tasks)
	}
}
