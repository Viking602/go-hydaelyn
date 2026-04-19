package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type AgentOptions struct {
	MaxIterations        int                     `json:"maxIterations,omitempty"`
	StopSequences        []string                `json:"stopSequences,omitempty"`
	ThinkingBudget       int                     `json:"thinkingBudget,omitempty"`
	OutputGuardrails     []agent.OutputGuardrail `json:"-"`
	OutputGuardrailNames []string                `json:"outputGuardrailNames,omitempty"`
	TeamOutputGuardrails []string                `json:"teamOutputGuardrails,omitempty"`
}

func (o AgentOptions) maxIterationsOrDefault(fallback int) int {
	if o.MaxIterations > 0 {
		return o.MaxIterations
	}
	return fallback
}

func mergeAgentOptions(base, override AgentOptions) AgentOptions {
	merged := base
	if override.MaxIterations > 0 {
		merged.MaxIterations = override.MaxIterations
	}
	if len(override.StopSequences) > 0 {
		merged.StopSequences = append([]string{}, override.StopSequences...)
	} else if len(merged.StopSequences) > 0 {
		merged.StopSequences = append([]string{}, merged.StopSequences...)
	}
	if override.ThinkingBudget > 0 {
		merged.ThinkingBudget = override.ThinkingBudget
	}
	if len(override.OutputGuardrails) > 0 {
		merged.OutputGuardrails = append([]agent.OutputGuardrail{}, override.OutputGuardrails...)
	} else if len(merged.OutputGuardrails) > 0 {
		merged.OutputGuardrails = append([]agent.OutputGuardrail{}, merged.OutputGuardrails...)
	}
	if len(override.OutputGuardrailNames) > 0 {
		merged.OutputGuardrailNames = append([]string{}, override.OutputGuardrailNames...)
	} else if len(merged.OutputGuardrailNames) > 0 {
		merged.OutputGuardrailNames = append([]string{}, merged.OutputGuardrailNames...)
	}
	if len(override.TeamOutputGuardrails) > 0 {
		merged.TeamOutputGuardrails = append([]string{}, override.TeamOutputGuardrails...)
	} else if len(merged.TeamOutputGuardrails) > 0 {
		merged.TeamOutputGuardrails = append([]string{}, merged.TeamOutputGuardrails...)
	}
	return merged
}

func toTeamAgentOptions(options AgentOptions) team.AgentOptions {
	return team.AgentOptions{
		MaxIterations:        options.MaxIterations,
		StopSequences:        append([]string{}, options.StopSequences...),
		ThinkingBudget:       options.ThinkingBudget,
		OutputGuardrails:     append([]string{}, options.OutputGuardrailNames...),
		TeamOutputGuardrails: append([]string{}, options.TeamOutputGuardrails...),
	}
}

func fromTeamAgentOptions(options team.AgentOptions) AgentOptions {
	return AgentOptions{
		MaxIterations:        options.MaxIterations,
		StopSequences:        append([]string{}, options.StopSequences...),
		ThinkingBudget:       options.ThinkingBudget,
		OutputGuardrailNames: append([]string{}, options.OutputGuardrails...),
		TeamOutputGuardrails: append([]string{}, options.TeamOutputGuardrails...),
	}
}

func mergeTeamAgentOptions(base, override team.AgentOptions) team.AgentOptions {
	return toTeamAgentOptions(mergeAgentOptions(fromTeamAgentOptions(base), fromTeamAgentOptions(override)))
}

type TeamOutputBoundary string

const (
	TeamOutputBoundaryBlackboard TeamOutputBoundary = "blackboard_publish"
	TeamOutputBoundaryTaskOutput TeamOutputBoundary = "task_output_publish"
	TeamOutputBoundaryFinal      TeamOutputBoundary = "team_final_result"
)

type TeamOutputGuardrail interface {
	Name() string
	Check(ctx context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error)
}

type TeamOutputGuardrailFunc func(ctx context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error)

type teamOutputGuardrail struct {
	name string
	fn   TeamOutputGuardrailFunc
}

func NewTeamOutputGuardrail(name string, fn TeamOutputGuardrailFunc) TeamOutputGuardrail {
	if fn == nil {
		return nil
	}
	return teamOutputGuardrail{name: name, fn: fn}
}

func (g teamOutputGuardrail) Name() string {
	return g.name
}

func (g teamOutputGuardrail) Check(ctx context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error) {
	return g.fn(ctx, input)
}

type TeamOutputGuardrailInput struct {
	TeamID   string
	TaskID   string
	Boundary TeamOutputBoundary
	Output   team.Result
	Metadata map[string]string
}

type TeamOutputGuardrailResult struct {
	Action      agent.OutputGuardrailAction
	Replacement *team.Result
	Reason      string
	Metadata    map[string]string
}

func AllowTeamOutput() TeamOutputGuardrailResult {
	return TeamOutputGuardrailResult{Action: agent.OutputGuardrailActionAllow}
}

func ReplaceTeamOutput(result team.Result) TeamOutputGuardrailResult {
	return TeamOutputGuardrailResult{
		Action:      agent.OutputGuardrailActionReplace,
		Replacement: cloneTeamResult(result),
	}
}

func BlockTeamOutput(reason string) TeamOutputGuardrailResult {
	return TeamOutputGuardrailResult{
		Action: agent.OutputGuardrailActionBlock,
		Reason: reason,
	}
}

func cloneTeamResult(result team.Result) *team.Result {
	cloned := result
	if len(result.Structured) > 0 {
		cloned.Structured = cloneAnyMap(result.Structured)
	}
	if len(result.ArtifactIDs) > 0 {
		cloned.ArtifactIDs = append([]string{}, result.ArtifactIDs...)
	}
	if len(result.Findings) > 0 {
		cloned.Findings = append([]team.Finding{}, result.Findings...)
	}
	if len(result.Evidence) > 0 {
		cloned.Evidence = append([]team.Evidence{}, result.Evidence...)
	}
	return &cloned
}

func (r *Runtime) resolvedAgentOptionsForTask(state team.RunState, task team.Task) (AgentOptions, error) {
	options := fromTeamAgentOptions(state.AgentOptions)
	options.OutputGuardrails = append(options.OutputGuardrails, r.inlineTeamOutputGuardrailsForTeam(state.ID)...)
	if strings.TrimSpace(task.EffectiveAssigneeAgentID()) == "" {
		return options, nil
	}
	agentInstance, ok := state.Agent(task.EffectiveAssigneeAgentID())
	if !ok {
		return options, nil
	}
	if strings.TrimSpace(agentInstance.EffectiveProfileName()) == "" {
		return options, nil
	}
	profile, err := r.lookupProfile(agentInstance.EffectiveProfileName())
	if err != nil {
		return AgentOptions{}, err
	}
	options = mergeAgentOptions(fromTeamAgentOptions(profile.AgentOptions), options)
	if options.MaxIterations <= 0 && profile.MaxTurns > 0 {
		options.MaxIterations = profile.MaxTurns
	}
	return options, nil
}

func (r *Runtime) applyTeamOutputGuardrails(ctx context.Context, teamID, taskID string, boundary TeamOutputBoundary, result *team.Result, names []string, metadata map[string]string) (*team.Result, bool, error) {
	if result == nil || len(names) == 0 {
		return result, true, nil
	}
	guardrails, err := r.resolveTeamOutputGuardrails(names)
	if err != nil {
		return nil, false, err
	}
	candidate := cloneTeamResult(*result)
	allowed := true
	for _, guardrail := range guardrails {
		if guardrail == nil {
			continue
		}
		decision, err := guardrail.Check(ctx, TeamOutputGuardrailInput{
			TeamID:   teamID,
			TaskID:   taskID,
			Boundary: boundary,
			Output:   *candidate,
			Metadata: cloneStringMap(metadata),
		})
		if err != nil {
			return nil, false, err
		}
		switch decision.Action {
		case "", agent.OutputGuardrailActionAllow:
			continue
		case agent.OutputGuardrailActionReplace:
			r.recordTeamOutputGuardrailDecision(ctx, guardrail.Name(), boundary, decision.Action, decision.Reason, metadata)
			if decision.Replacement != nil {
				candidate = cloneTeamResult(*decision.Replacement)
			}
		case agent.OutputGuardrailActionBlock:
			r.recordTeamOutputGuardrailDecision(ctx, guardrail.Name(), boundary, decision.Action, decision.Reason, metadata)
			allowed = false
		default:
			return nil, false, fmt.Errorf("unsupported team output guardrail action %q", decision.Action)
		}
		if !allowed {
			return candidate, false, nil
		}
	}
	return candidate, true, nil
}

func (r *Runtime) resolveOutputGuardrails(options AgentOptions) ([]agent.OutputGuardrail, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	guardrails := make([]agent.OutputGuardrail, 0, len(r.defaultOutputGuardrails)+len(options.OutputGuardrails)+len(options.OutputGuardrailNames))
	guardrails = append(guardrails, r.defaultOutputGuardrails...)
	guardrails = append(guardrails, options.OutputGuardrails...)
	for _, name := range options.OutputGuardrailNames {
		item, ok := r.outputGuardrails[name]
		if !ok {
			return nil, fmt.Errorf("output guardrail not found: %s", name)
		}
		guardrails = append(guardrails, item)
	}
	return guardrails, nil
}

func (r *Runtime) resolveTeamOutputGuardrails(names []string) ([]TeamOutputGuardrail, error) {
	if len(names) == 0 {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	guardrails := make([]TeamOutputGuardrail, 0, len(names))
	for _, name := range names {
		item, ok := r.teamGuardrails[name]
		if !ok {
			return nil, fmt.Errorf("team output guardrail not found: %s", name)
		}
		guardrails = append(guardrails, item)
	}
	return guardrails, nil
}

func (r *Runtime) RecordOutputGuardrailDecision(ctx context.Context, decision agent.OutputGuardrailDecision) {
	metadata := cloneStringMap(decision.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	runID := strings.TrimSpace(metadata["runId"])
	teamID := strings.TrimSpace(metadata["teamId"])
	taskID := strings.TrimSpace(metadata["taskId"])
	agentID := strings.TrimSpace(metadata["agentId"])
	if runID == "" {
		runID = teamID
	}
	if runID == "" {
		return
	}
	outcome := outputGuardrailOutcome(decision.Action)
	severity := "info"
	blocking := false
	if decision.Action == agent.OutputGuardrailActionRetry {
		severity = "warning"
	}
	if decision.Action == agent.OutputGuardrailActionBlock {
		severity = "error"
		blocking = true
	}
	metadata["runId"] = runID
	if teamID != "" {
		metadata["teamId"] = teamID
	}
	if taskID != "" {
		metadata["taskId"] = taskID
	}
	if agentID != "" {
		metadata["agentId"] = agentID
	}
	if decision.GuardrailName != "" {
		metadata["guardrail"] = decision.GuardrailName
	}
	r.RecordPolicyOutcome(ctx, storage.PolicyOutcomeEvent{
		SchemaVersion: storage.PolicyOutcomeEventSchemaVersion,
		Layer:         "output_guardrail",
		Stage:         "final_output",
		Operation:     "check",
		Action:        string(decision.Action),
		Policy:        "output_guardrail." + decision.GuardrailName,
		Outcome:       outcome,
		Severity:      severity,
		Message:       decision.Reason,
		Blocking:      blocking,
		RunID:         runID,
		TeamID:        teamID,
		TaskID:        taskID,
		AgentID:       agentID,
		Reference:     outputGuardrailReference(runID, teamID, taskID, agentID),
		Attempt:       decision.Iteration,
		Timestamp:     time.Now().UTC(),
		Evidence: &storage.PolicyOutcomeEvidence{
			Metadata: metadata,
		},
	})
}

func (r *Runtime) recordTeamOutputGuardrailDecision(ctx context.Context, guardrailName string, boundary TeamOutputBoundary, action agent.OutputGuardrailAction, reason string, metadata map[string]string) {
	cloned := cloneStringMap(metadata)
	if cloned == nil {
		cloned = map[string]string{}
	}
	cloned["boundary"] = string(boundary)
	r.RecordOutputGuardrailDecision(ctx, agent.OutputGuardrailDecision{
		GuardrailName: guardrailName,
		Action:        action,
		Reason:        reason,
		Metadata:      cloned,
	})
}

func outputGuardrailOutcome(action agent.OutputGuardrailAction) string {
	switch action {
	case agent.OutputGuardrailActionReplace:
		return "replaced"
	case agent.OutputGuardrailActionRetry:
		return "retrying"
	case agent.OutputGuardrailActionBlock:
		return "blocked"
	default:
		return "allowed"
	}
}

func outputGuardrailReference(runID, teamID, taskID, agentID string) string {
	switch {
	case teamID != "" && taskID != "":
		return teamID + ":" + taskID
	case runID != "":
		return runID
	case agentID != "":
		return agentID
	default:
		return "output_guardrail"
	}
}

func (r *Runtime) storeInlineTeamOutputGuardrails(teamID string, guardrails []agent.OutputGuardrail) {
	if teamID == "" || len(guardrails) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inlineTeamOutputGuardrails[teamID] = append([]agent.OutputGuardrail{}, guardrails...)
}

func (r *Runtime) inlineTeamOutputGuardrailsForTeam(teamID string) []agent.OutputGuardrail {
	if teamID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]agent.OutputGuardrail{}, r.inlineTeamOutputGuardrails[teamID]...)
}
