package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/pattern/deepsearch"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type exampleProvider struct{}

func (exampleProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "example"}
}

func (exampleProvider) Stream(_ context.Context, request provider.Request) (provider.Stream, error) {
	lastText := ""
	var lastTool *message.ToolResult
	for idx := len(request.Messages) - 1; idx >= 0; idx-- {
		msg := request.Messages[idx]
		if lastTool == nil && msg.ToolResult != nil {
			lastTool = msg.ToolResult
		}
		if lastText == "" && strings.TrimSpace(msg.Text) != "" {
			lastText = msg.Text
		}
	}
	if request.Metadata["taskId"] == "research-1" && lastTool == nil {
		args, _ := json.Marshal(map[string]any{
			"key":     "research.branch-1",
			"summary": lastText,
		})
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

type exampleTool struct {
	artifacts storage.ArtifactStore
}

func (t exampleTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "artifact_tool",
		Description: "persist one artifact and return a structured reference",
		InputSchema: tool.Schema{Type: "object"},
	}
}

func (t exampleTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
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
	payload, _ := json.Marshal(map[string]any{
		"key":         input.Key,
		"summary":     input.Summary,
		"artifactIds": []string{artifactID},
	})
	return tool.Result{
		ToolCallID: call.ID,
		Name:       "artifact_tool",
		Content:    input.Summary,
		Structured: payload,
	}, nil
}

type examplePlanner struct{}

func (examplePlanner) Plan(_ context.Context, _ planner.PlanRequest) (planner.Plan, error) {
	return planner.Plan{
		Goal: "dataflow example",
		Tasks: []planner.TaskSpec{
			{
				ID:            "research-1",
				Kind:          string(team.TaskKindResearch),
				Title:         "branch",
				Input:         "map the current runtime gap",
				RequiredRole:  team.RoleResearcher,
				Writes:        []string{"research.branch-1"},
				Publish:       []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
				FailurePolicy: team.FailurePolicyFailFast,
			},
			{
				ID:              "synth-1",
				Kind:            string(team.TaskKindSynthesize),
				Title:           "synthesize",
				Input:           "compose the final answer",
				AssigneeAgentID: "supervisor",
				Reads:           []string{"research.branch-1"},
				Publish:         []team.OutputVisibility{team.OutputVisibilityShared},
				DependsOn:       []string{"research-1"},
				FailurePolicy:   team.FailurePolicyFailFast,
			},
		},
	}, nil
}

func (examplePlanner) Review(_ context.Context, _ planner.ReviewInput) (planner.ReviewDecision, error) {
	return planner.ReviewDecision{Action: planner.ReviewActionContinue}, nil
}

func (examplePlanner) Replan(_ context.Context, _ planner.ReplanInput) (planner.Plan, error) {
	return planner.Plan{}, fmt.Errorf("replan not implemented in example")
}

func main() {
	driver := storage.NewMemoryDriver()
	runner := host.New(host.Config{Storage: driver})
	runner.RegisterProvider("example", exampleProvider{})
	runner.RegisterTool(exampleTool{artifacts: driver.Artifacts()})
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "example", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "example", Model: "test", ToolNames: []string{"artifact_tool"}})
	if err := runner.RegisterPlugin(plugin.Spec{Type: plugin.TypePlanner, Name: "dataflow", Component: examplePlanner{}}); err != nil {
		panic(err)
	}

	state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		Planner:           "dataflow",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "dataflow example"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("summary:", state.Result.Summary)
	fmt.Println("exchanges:", len(state.Blackboard.Exchanges))
	fmt.Println("artifact ids:", state.Tasks[0].Result.ArtifactIDs)
}
