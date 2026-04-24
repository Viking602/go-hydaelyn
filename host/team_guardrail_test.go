package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type sharedOutputPattern struct{}

func (sharedOutputPattern) Name() string { return "shared-output" }

func (sharedOutputPattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	input, _ := request.Input["task"].(string)
	return team.RunState{
		ID:         request.TeamID,
		Pattern:    "shared-output",
		Status:     team.StatusRunning,
		Phase:      team.PhaseResearch,
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: request.SupervisorProfile},
		Workers:    []team.AgentInstance{{ID: "worker-1", Role: team.RoleResearcher, ProfileName: request.WorkerProfiles[0]}},
		Tasks: []team.Task{{
			ID:              "task-1",
			Kind:            team.TaskKindResearch,
			Input:           input,
			RequiredRole:    team.RoleResearcher,
			AssigneeAgentID: "worker-1",
			FailurePolicy:   team.FailurePolicyFailFast,
			Namespace:       "impl.task-1",
			Writes:          []string{"result"},
			Publish:         []team.OutputVisibility{team.OutputVisibilityShared},
			Status:          team.TaskStatusPending,
		}},
		Input: request.Input,
	}, nil
}

func (sharedOutputPattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return state, nil
		}
	}
	state.Status = team.StatusCompleted
	state.Phase = team.PhaseComplete
	state.Result = &team.Result{Summary: "done"}
	return state, nil
}

func TestStartTeamForwardsAgentOptionsAndOutputGuardrails(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe task answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("team-capture", providerDriver)
	runner.RegisterPattern(singleTaskPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-capture", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-capture", Model: "test"})
	runner.RegisterOutputGuardrail("safe-task", agent.NewOutputGuardrail("safe-task", func(_ context.Context, input agent.OutputGuardrailInput) (agent.OutputGuardrailResult, error) {
		if input.Output.Text == "unsafe task answer" {
			return agent.ReplaceOutput(message.NewText(message.RoleAssistant, "safe task answer")), nil
		}
		return agent.AllowOutput(), nil
	}))

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "do it"},
		Agent: AgentOptions{
			StopSequences:        []string{"STOP"},
			ThinkingBudget:       9,
			ExtraBody:            map[string]any{"chat_template_kwargs": map[string]any{"thinking": true}},
			OutputGuardrailNames: []string{"safe-task"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if len(providerDriver.requests) != 1 {
		t.Fatalf("expected one provider request, got %d", len(providerDriver.requests))
	}
	request := providerDriver.requests[0]
	if len(request.StopSequences) != 1 || request.StopSequences[0] != "STOP" {
		t.Fatalf("expected stop sequences to be forwarded, got %#v", request.StopSequences)
	}
	if request.ThinkingBudget != 9 {
		t.Fatalf("expected thinking budget to be forwarded, got %d", request.ThinkingBudget)
	}
	requireProviderExtraBodyThinkingEnabled(t, request.ExtraBody)
	if state.Tasks[0].Result == nil || state.Tasks[0].Result.Summary != "safe task answer" {
		t.Fatalf("expected output guardrail replacement on task result, got %#v", state.Tasks[0].Result)
	}
}

func TestTeamOutputGuardrailBlocksBlackboardPublishAndReplacesFinalResult(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "unsafe task answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("team-capture", providerDriver)
	runner.RegisterPattern(singleTaskPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-capture", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-capture", Model: "test"})
	runner.RegisterTeamOutputGuardrail("team-sanitize", NewTeamOutputGuardrail("team-sanitize", func(_ context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error) {
		switch input.Boundary {
		case TeamOutputBoundaryBlackboard:
			return BlockTeamOutput("block blackboard"), nil
		case TeamOutputBoundaryFinal:
			return ReplaceTeamOutput(team.Result{Summary: "safe final"}), nil
		default:
			return AllowTeamOutput(), nil
		}
	}))

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "single-task",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "do it"},
		Agent: AgentOptions{
			TeamOutputGuardrails: []string{"team-sanitize"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Blackboard != nil && len(state.Blackboard.Exchanges) > 0 {
		t.Fatalf("expected blackboard publish to be blocked, got %#v", state.Blackboard.Exchanges)
	}
	if state.Result == nil || state.Result.Summary != "safe final" {
		t.Fatalf("expected final team result replacement, got %#v", state.Result)
	}

	events, err := runner.storage.Events().List(context.Background(), state.ID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	blocked := false
	replaced := false
	for _, event := range events {
		if event.Type != storage.EventPolicyOutcome {
			continue
		}
		if event.Payload["policy"] != "output_guardrail.team-sanitize" {
			continue
		}
		outcome, _ := event.Payload["outcome"].(string)
		switch outcome {
		case "blocked":
			blocked = true
		case "replaced":
			replaced = true
		}
	}
	if !blocked || !replaced {
		t.Fatalf("expected blocked and replaced policy outcomes, got %#v", events)
	}
}

func TestTeamOutputGuardrailBlocksTaskOutputPublish(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: "shared answer"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("team-capture", providerDriver)
	runner.RegisterPattern(sharedOutputPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-capture", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-capture", Model: "test"})
	runner.RegisterTeamOutputGuardrail("task-output-block", NewTeamOutputGuardrail("task-output-block", func(_ context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error) {
		if input.Boundary == TeamOutputBoundaryTaskOutput {
			return BlockTeamOutput("no task output publish"), nil
		}
		return AllowTeamOutput(), nil
	}))

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "shared-output",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "do it"},
		Agent: AgentOptions{
			TeamOutputGuardrails: []string{"task-output-block"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	snapshot, err := runner.GetSession(context.Background(), state.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(snapshot.Messages) != 0 {
		t.Fatalf("expected shared task output publish to be blocked, got %#v", snapshot.Messages)
	}
}

func TestDefaultNoJSONToUserGuardrailBlocksSharedJSONSummary(t *testing.T) {
	runner := New(Config{})
	providerDriver := &capturePromptProvider{
		turns: [][]provider.Event{{
			{Kind: provider.EventTextDelta, Text: `{"report":{"kind":"research"}}`},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}},
	}
	runner.RegisterProvider("team-capture", providerDriver)
	runner.RegisterPattern(sharedOutputPattern{})
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-capture", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-capture", Model: "test"})

	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "shared-output",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"task": "do it"},
		Agent: AgentOptions{
			AssistantOutputMode: team.AssistantOutputModeShared,
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	snapshot, err := runner.GetSession(context.Background(), state.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(snapshot.Messages) != 0 {
		t.Fatalf("expected default no-json guardrail to block shared publish, got %#v", snapshot.Messages)
	}
}
