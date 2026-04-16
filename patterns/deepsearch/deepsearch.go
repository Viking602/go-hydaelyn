package deepsearch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
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
					Input:           task.Result.Summary,
					RequiredRole:    team.RoleResearcher,
					AssigneeAgentID: task.EffectiveAssigneeAgentID(),
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
		fallthrough
	case team.PhaseVerify:
		if state.Phase == team.PhaseVerify && hasPendingPhaseTasks(state, team.TaskKindVerify) {
			return state, nil
		}
		state.Result = aggregate(state)
		state.Phase = team.PhaseSynthesize
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

func aggregate(state team.RunState) *team.Result {
	if state.RequireVerification && state.Blackboard != nil {
		if result := aggregateVerified(state.Blackboard); result != nil {
			return result
		}
	}
	findings := make([]team.Finding, 0, len(state.Tasks))
	summaries := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Kind != team.TaskKindResearch || task.Result == nil {
			continue
		}
		findings = append(findings, team.Finding{
			Summary:    task.Result.Summary,
			Evidence:   task.Result.Evidence,
			Confidence: task.Result.Confidence,
		})
		summaries = append(summaries, task.Result.Summary)
	}
	return &team.Result{
		Summary:    strings.Join(summaries, "\n"),
		Findings:   findings,
		Confidence: 0.75,
	}
}

func aggregateVerified(board *blackboard.State) *team.Result {
	findings := board.SupportedFindings()
	if len(findings) == 0 {
		return &team.Result{}
	}
	items := make([]team.Finding, 0, len(findings))
	summaries := make([]string, 0, len(findings))
	totalConfidence := 0.0
	for _, finding := range findings {
		items = append(items, team.Finding{
			Summary:    finding.Summary,
			Confidence: finding.Confidence,
		})
		summaries = append(summaries, finding.Summary)
		totalConfidence += finding.Confidence
	}
	return &team.Result{
		Summary:    strings.Join(summaries, "\n"),
		Findings:   items,
		Confidence: totalConfidence / float64(len(items)),
	}
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
