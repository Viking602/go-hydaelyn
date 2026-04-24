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
	MaxIterations        int                      `json:"maxIterations,omitempty"`
	StopSequences        []string                 `json:"stopSequences,omitempty"`
	ThinkingBudget       int                      `json:"thinkingBudget,omitempty"`
	ExtraBody            map[string]any           `json:"extraBody,omitempty"`
	OutputGuardrails     []agent.OutputGuardrail  `json:"-"`
	OutputGuardrailNames []string                 `json:"outputGuardrailNames,omitempty"`
	TeamOutputGuardrails []string                 `json:"teamOutputGuardrails,omitempty"`
	AssistantOutputMode  team.AssistantOutputMode `json:"assistantOutputMode,omitempty"`
}

func (o AgentOptions) maxIterationsOrDefault(fallback int) int {
	if o.MaxIterations > 0 {
		return o.MaxIterations
	}
	return fallback
}

func mergeAgentOptions(base, override AgentOptions) AgentOptions {
	merged := base
	merged.MaxIterations = mergePositiveInt(base.MaxIterations, override.MaxIterations)
	merged.StopSequences = mergeStringSlice(base.StopSequences, override.StopSequences)
	merged.ThinkingBudget = mergePositiveInt(base.ThinkingBudget, override.ThinkingBudget)
	merged.ExtraBody = mergeAnyMap(base.ExtraBody, override.ExtraBody)
	merged.OutputGuardrails = mergeOutputGuardrails(base.OutputGuardrails, override.OutputGuardrails)
	merged.OutputGuardrailNames = mergeStringSlice(base.OutputGuardrailNames, override.OutputGuardrailNames)
	merged.TeamOutputGuardrails = mergeStringSlice(base.TeamOutputGuardrails, override.TeamOutputGuardrails)
	merged.AssistantOutputMode = mergeAssistantOutputMode(base.AssistantOutputMode, override.AssistantOutputMode)
	return merged
}

func mergePositiveInt(base, override int) int {
	if override > 0 {
		return override
	}
	return base
}

func mergeStringSlice(base, override []string) []string {
	if len(override) > 0 {
		return cloneStringSlice(override)
	}
	return cloneStringSlice(base)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string{}, values...)
}

func mergeAnyMap(base, override map[string]any) map[string]any {
	if len(override) > 0 {
		return cloneAnyMap(override)
	}
	return cloneAnyMap(base)
}

func mergeOutputGuardrails(base, override []agent.OutputGuardrail) []agent.OutputGuardrail {
	if len(override) > 0 {
		return cloneOutputGuardrails(override)
	}
	return cloneOutputGuardrails(base)
}

func cloneOutputGuardrails(values []agent.OutputGuardrail) []agent.OutputGuardrail {
	if len(values) == 0 {
		return nil
	}
	return append([]agent.OutputGuardrail{}, values...)
}

func mergeAssistantOutputMode(base, override team.AssistantOutputMode) team.AssistantOutputMode {
	if override != "" {
		return override
	}
	return base
}

func toTeamAgentOptions(options AgentOptions) team.AgentOptions {
	return team.AgentOptions{
		MaxIterations:        options.MaxIterations,
		StopSequences:        append([]string{}, options.StopSequences...),
		ThinkingBudget:       options.ThinkingBudget,
		ExtraBody:            cloneAnyMap(options.ExtraBody),
		OutputGuardrails:     append([]string{}, options.OutputGuardrailNames...),
		TeamOutputGuardrails: append([]string{}, options.TeamOutputGuardrails...),
		AssistantOutputMode:  options.AssistantOutputMode,
	}
}

func fromTeamAgentOptions(options team.AgentOptions) AgentOptions {
	return AgentOptions{
		MaxIterations:        options.MaxIterations,
		StopSequences:        append([]string{}, options.StopSequences...),
		ThinkingBudget:       options.ThinkingBudget,
		ExtraBody:            cloneAnyMap(options.ExtraBody),
		OutputGuardrailNames: append([]string{}, options.OutputGuardrails...),
		TeamOutputGuardrails: append([]string{}, options.TeamOutputGuardrails...),
		AssistantOutputMode:  options.AssistantOutputMode,
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
	cloned.Structured = cloneAnyMap(result.Structured)
	cloned.ArtifactIDs = cloneStringSlice(result.ArtifactIDs)
	cloned.Findings = cloneFindings(result.Findings)
	cloned.Evidence = cloneEvidence(result.Evidence)
	return &cloned
}

func cloneFindings(values []team.Finding) []team.Finding {
	if len(values) == 0 {
		return nil
	}
	return append([]team.Finding{}, values...)
}

func cloneEvidence(values []team.Evidence) []team.Evidence {
	if len(values) == 0 {
		return nil
	}
	return append([]team.Evidence{}, values...)
}

func (r *Runtime) resolvedAgentOptionsForTask(state team.RunState, task team.Task) (AgentOptions, error) {
	options := fromTeamAgentOptions(state.AgentOptions)
	options.OutputGuardrails = append(options.OutputGuardrails, r.inlineTeamOutputGuardrailsForTeam(state.ID)...)
	profile, ok, err := r.profileForTask(state, task)
	if err != nil {
		return AgentOptions{}, err
	}
	if !ok {
		return options, nil
	}
	return applyProfileAgentOptions(options, profile), nil
}

func (r *Runtime) profileForTask(state team.RunState, task team.Task) (team.Profile, bool, error) {
	profileName, ok := profileNameForTask(state, task)
	if !ok {
		return team.Profile{}, false, nil
	}
	profile, err := r.lookupProfile(profileName)
	if err != nil {
		return team.Profile{}, false, err
	}
	return profile, true, nil
}

func profileNameForTask(state team.RunState, task team.Task) (string, bool) {
	agentID := strings.TrimSpace(task.EffectiveAssigneeAgentID())
	if agentID == "" {
		return "", false
	}
	agentInstance, ok := state.Agent(agentID)
	if !ok {
		return "", false
	}
	profileName := strings.TrimSpace(agentInstance.EffectiveProfileName())
	if profileName == "" {
		return "", false
	}
	return profileName, true
}

func applyProfileAgentOptions(options AgentOptions, profile team.Profile) AgentOptions {
	merged := mergeAgentOptions(fromTeamAgentOptions(profile.AgentOptions), options)
	if merged.MaxIterations <= 0 && profile.MaxTurns > 0 {
		merged.MaxIterations = profile.MaxTurns
	}
	return merged
}

func (r *Runtime) resolvedAssistantOutputModeForTask(state team.RunState, task team.Task) (team.AssistantOutputMode, error) {
	options, err := r.resolvedAgentOptionsForTask(state, task)
	if err != nil {
		return team.AssistantOutputModeOff, err
	}
	return task.EffectiveAssistantOutputMode(options.AssistantOutputMode), nil
}

func (r *Runtime) applyTeamOutputGuardrails(ctx context.Context, teamID, taskID string, boundary TeamOutputBoundary, result *team.Result, names []string, metadata map[string]string) (*team.Result, bool, error) {
	if result == nil || len(names) == 0 {
		return result, true, nil
	}
	guardrails, err := r.resolveTeamOutputGuardrails(names)
	if err != nil {
		return nil, false, err
	}
	return r.applyTeamOutputGuardrailChain(ctx, teamID, taskID, boundary, cloneTeamResult(*result), guardrails, metadata)
}

func (r *Runtime) applyTeamOutputGuardrailChain(ctx context.Context, teamID, taskID string, boundary TeamOutputBoundary, candidate *team.Result, guardrails []TeamOutputGuardrail, metadata map[string]string) (*team.Result, bool, error) {
	for _, guardrail := range guardrails {
		if guardrail == nil {
			continue
		}
		next, allowed, err := r.applySingleTeamOutputGuardrail(ctx, teamID, taskID, boundary, candidate, guardrail, metadata)
		if err != nil {
			return nil, false, err
		}
		candidate = next
		if !allowed {
			return candidate, false, nil
		}
	}
	return candidate, true, nil
}

func (r *Runtime) applySingleTeamOutputGuardrail(ctx context.Context, teamID, taskID string, boundary TeamOutputBoundary, candidate *team.Result, guardrail TeamOutputGuardrail, metadata map[string]string) (*team.Result, bool, error) {
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
	return r.applyTeamOutputGuardrailDecision(ctx, boundary, candidate, guardrail.Name(), decision, metadata)
}

func (r *Runtime) applyTeamOutputGuardrailDecision(ctx context.Context, boundary TeamOutputBoundary, candidate *team.Result, guardrailName string, decision TeamOutputGuardrailResult, metadata map[string]string) (*team.Result, bool, error) {
	switch decision.Action {
	case "", agent.OutputGuardrailActionAllow:
		return candidate, true, nil
	case agent.OutputGuardrailActionReplace:
		r.recordTeamOutputGuardrailDecision(ctx, guardrailName, boundary, decision.Action, decision.Reason, metadata)
		return replacementTeamResult(candidate, decision.Replacement), true, nil
	case agent.OutputGuardrailActionBlock:
		r.recordTeamOutputGuardrailDecision(ctx, guardrailName, boundary, decision.Action, decision.Reason, metadata)
		return candidate, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported team output guardrail action %q", decision.Action)
	}
}

func replacementTeamResult(current, replacement *team.Result) *team.Result {
	if replacement == nil {
		return current
	}
	return cloneTeamResult(*replacement)
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	guardrails := make([]TeamOutputGuardrail, 0, len(names)+1)
	if item, ok := r.teamGuardrails["no_json_to_user"]; ok {
		guardrails = append(guardrails, item)
	}
	if len(names) == 0 {
		return guardrails, nil
	}
	for _, name := range names {
		item, ok := r.teamGuardrails[name]
		if !ok {
			return nil, fmt.Errorf("team output guardrail not found: %s", name)
		}
		if name == "no_json_to_user" {
			continue
		}
		guardrails = append(guardrails, item)
	}
	return guardrails, nil
}

func noJSONToUserGuardrail() TeamOutputGuardrail {
	return NewTeamOutputGuardrail("no_json_to_user", func(_ context.Context, input TeamOutputGuardrailInput) (TeamOutputGuardrailResult, error) {
		if input.Boundary == TeamOutputBoundaryBlackboard {
			return AllowTeamOutput(), nil
		}
		summary := strings.TrimSpace(input.Output.Summary)
		if summary == "" || !looksLikeJSON(summary) {
			return AllowTeamOutput(), nil
		}
		if report, ok := team.ExtractSynthesisReport(input.Output.Structured); ok {
			answer := strings.TrimSpace(report.Answer)
			if answer != "" {
				replacement := input.Output
				replacement.Summary = answer
				return ReplaceTeamOutput(replacement), nil
			}
		}
		if input.Boundary == TeamOutputBoundaryTaskOutput {
			return BlockTeamOutput("json summary is not display-safe"), nil
		}
		replacement := input.Output
		replacement.Summary = ""
		replacement.Findings = nil
		replacement.Evidence = nil
		return ReplaceTeamOutput(replacement), nil
	})
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
