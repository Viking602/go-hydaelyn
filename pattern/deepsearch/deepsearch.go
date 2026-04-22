package deepsearch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

type Pattern struct{}

func New() Pattern {
	return Pattern{}
}

func (Pattern) Name() string {
	return "deepsearch"
}

func (Pattern) PlanTemplate(request team.StartRequest) (planner.Template, error) {
	workerProfiles := append([]string{}, request.WorkerProfiles...)
	if len(workerProfiles) == 0 {
		return planner.Template{}, fmt.Errorf("deepsearch requires at least one worker profile")
	}
	subqueries := parseSubqueries(request.Input)
	if len(subqueries) == 0 {
		query, _ := request.Input["query"].(string)
		subqueries = []string{query}
	}
	taskHints := make([]planner.TaskSpec, 0, len(subqueries))
	for idx, query := range subqueries {
		taskHints = append(taskHints, planner.TaskSpec{
			ID:            fmt.Sprintf("task-%d", idx+1),
			Kind:          string(team.TaskKindResearch),
			Title:         query,
			Input:         query,
			RequiredRole:  team.RoleResearcher,
			Writes:        []string{researchWriteKey(fmt.Sprintf("task-%d", idx+1))},
			Publish:       []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			FailurePolicy: team.FailurePolicyFailFast,
		})
	}
	query, _ := request.Input["query"].(string)
	return planner.Template{
		Name: request.Pattern,
		Goal: query,
		VerificationPolicy: planner.VerificationPolicy{
			Required: boolValue(request.Input["requireVerification"]),
			Mode:     "research_verification",
		},
		Tasks: taskHints,
	}, nil
}

func (Pattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	workerProfiles := append([]string{}, request.WorkerProfiles...)
	if len(workerProfiles) == 0 {
		return team.RunState{}, fmt.Errorf("deepsearch requires at least one worker profile")
	}
	subqueries := parseSubqueries(request.Input)
	if len(subqueries) == 0 {
		query, _ := request.Input["query"].(string)
		subqueries = []string{query}
	}
	workers := make([]team.Member, 0, len(workerProfiles))
	for idx, profile := range workerProfiles {
		workers = append(workers, team.Member{
			ID:          fmt.Sprintf("worker-%d", idx+1),
			Role:        team.RoleResearcher,
			ProfileName: profile,
		})
	}
	tasks := make([]team.Task, 0, len(subqueries))
	for idx, query := range subqueries {
		assignee := workers[idx%len(workers)]
		tasks = append(tasks, team.Task{
			ID:              fmt.Sprintf("task-%d", idx+1),
			Kind:            team.TaskKindResearch,
			Title:           query,
			Input:           query,
			RequiredRole:    team.RoleResearcher,
			AssigneeAgentID: assignee.ID,
			Writes:          []string{researchWriteKey(fmt.Sprintf("task-%d", idx+1))},
			Publish:         []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			FailurePolicy:   team.FailurePolicyFailFast,
			Status:          team.TaskStatusPending,
		})
	}
	requireVerification, _ := request.Input["requireVerification"].(bool)
	state := team.RunState{
		ID:      request.TeamID,
		Pattern: "deepsearch",
		Status:  team.StatusRunning,
		Phase:   team.PhaseResearch,
		Supervisor: team.Member{
			ID:          "supervisor",
			Role:        team.RoleSupervisor,
			ProfileName: request.SupervisorProfile,
		},
		Workers:             workers,
		Tasks:               tasks,
		Input:               request.Input,
		Metadata:            request.Metadata,
		RequireVerification: requireVerification,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
	return state, nil
}

func (Pattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	state.UpdatedAt = time.Now().UTC()
	switch state.Phase {
	case team.PhaseResearch:
		if hasPendingPhaseTasks(state, team.TaskKindResearch) {
			return state, nil
		}
		if state.RequireVerification {
			verifyTasks := make([]team.Task, 0, len(state.Tasks))
			for _, task := range state.Tasks {
				if task.Kind != team.TaskKindResearch || task.Result == nil {
					continue
				}
				verifyTasks = append(verifyTasks, team.Task{
					ID:              fmt.Sprintf("%s-verify", task.ID),
					Kind:            team.TaskKindVerify,
					Title:           "verify " + task.Title,
					Input:           fmt.Sprintf("Verify the published research output for %s.", task.Title),
					RequiredRole:    team.RoleResearcher,
					AssigneeAgentID: task.EffectiveAssigneeAgentID(),
					Reads:           []string{researchWriteKey(task.ID)},
					Writes:          []string{verificationWriteKey(task.ID)},
					Publish:         []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
					FailurePolicy:   team.FailurePolicyFailFast,
					DependsOn: []string{
						task.ID,
					},
					Status: team.TaskStatusPending,
				})
			}
			state.Tasks = append(state.Tasks, verifyTasks...)
			state.Phase = team.PhaseVerify
			return state, nil
		}
		state.Tasks = append(state.Tasks, synthesizeTask(state))
		state.Phase = team.PhaseSynthesize
		return state, nil
	case team.PhaseVerify:
		if hasPendingPhaseTasks(state, team.TaskKindVerify) {
			return state, nil
		}
		state.Tasks = append(state.Tasks, synthesizeTask(state))
		state.Phase = team.PhaseSynthesize
		return state, nil
	case team.PhaseSynthesize:
		if hasPendingPhaseTasks(state, team.TaskKindSynthesize) {
			return state, nil
		}
		state.Result = synthesizedResult(state)
		state.Status = team.StatusCompleted
		return state, nil
	default:
		return state, nil
	}
}

func hasPendingPhaseTasks(state team.RunState, kind team.TaskKind) bool {
	for _, task := range state.Tasks {
		if task.Kind != kind {
			continue
		}
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return true
		}
	}
	return false
}

func parseSubqueries(input map[string]any) []string {
	if input == nil {
		return nil
	}
	raw, ok := input["subqueries"]
	if !ok {
		return nil
	}
	switch current := raw.(type) {
	case []string:
		return append([]string{}, current...)
	case []any:
		items := make([]string, 0, len(current))
		for _, item := range current {
			text, ok := item.(string)
			if ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func synthesizeTask(state team.RunState) team.Task {
	reads := researchReadKeys(state)
	dependencies := researchDependencies(state)
	if state.RequireVerification {
		reads = []string{"supported_findings"}
		dependencies = verifyDependencies(state)
	}
	return team.Task{
		ID:              "task-synthesize",
		Kind:            team.TaskKindSynthesize,
		Title:           "synthesize final answer",
		Input:           "Synthesize the collected findings into the final answer.",
		RequiredRole:    team.RoleSupervisor,
		AssigneeAgentID: state.Supervisor.ID,
		Reads:           reads,
		Publish:         []team.OutputVisibility{team.OutputVisibilityShared},
		FailurePolicy:   team.FailurePolicyFailFast,
		DependsOn:       dependencies,
		Status:          team.TaskStatusPending,
	}
}

func synthesizedResult(state team.RunState) *team.Result {
	synthTask := latestCompletedTask(state, team.TaskKindSynthesize)
	if synthTask == nil || synthTask.Result == nil {
		return &team.Result{}
	}
	result := cloneResult(*synthTask.Result)
	findings := synthesisFindings(state)
	if len(findings) == 0 {
		return &result
	}
	result.Findings = findings
	if strings.TrimSpace(result.Summary) == "" {
		summaries := make([]string, 0, len(findings))
		for _, finding := range findings {
			summaries = append(summaries, finding.Summary)
		}
		result.Summary = strings.Join(summaries, "\n")
	}
	if result.Confidence == 0 {
		total := 0.0
		for _, finding := range findings {
			total += finding.Confidence
		}
		result.Confidence = total / float64(len(findings))
	}
	return &result
}

func synthesisFindings(state team.RunState) []team.Finding {
	if state.RequireVerification && state.Blackboard != nil {
		supported := state.Blackboard.SupportedFindings()
		if len(supported) > 0 {
			items := make([]team.Finding, 0, len(supported))
			for _, finding := range supported {
				items = append(items, team.Finding{
					Summary:    finding.Summary,
					Confidence: finding.Confidence,
				})
			}
			return items
		}
	}
	items := make([]team.Finding, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Kind != team.TaskKindResearch || task.Result == nil {
			continue
		}
		items = append(items, team.Finding{
			Summary:    task.Result.Summary,
			Evidence:   task.Result.Evidence,
			Confidence: task.Result.Confidence,
		})
	}
	return items
}

func latestCompletedTask(state team.RunState, kind team.TaskKind) *team.Task {
	for idx := len(state.Tasks) - 1; idx >= 0; idx-- {
		if state.Tasks[idx].Kind == kind && state.Tasks[idx].Status == team.TaskStatusCompleted {
			return &state.Tasks[idx]
		}
	}
	return nil
}

func researchWriteKey(taskID string) string {
	return "research." + taskID
}

func verificationWriteKey(taskID string) string {
	return "verification." + taskID
}

func researchReadKeys(state team.RunState) []string {
	keys := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Kind == team.TaskKindResearch {
			keys = append(keys, researchWriteKey(task.ID))
		}
	}
	return keys
}

func researchDependencies(state team.RunState) []string {
	dependencies := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Kind == team.TaskKindResearch {
			dependencies = append(dependencies, task.ID)
		}
	}
	return dependencies
}

func verifyDependencies(state team.RunState) []string {
	dependencies := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Kind == team.TaskKindVerify {
			dependencies = append(dependencies, task.ID)
		}
	}
	return dependencies
}

func cloneResult(result team.Result) team.Result {
	result.Structured = cloneStructured(result.Structured)
	result.ArtifactIDs = append([]string{}, result.ArtifactIDs...)
	result.Findings = append([]team.Finding{}, result.Findings...)
	result.Evidence = append([]team.Evidence{}, result.Evidence...)
	return result
}

func cloneStructured(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
