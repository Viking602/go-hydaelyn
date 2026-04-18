package collab

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/team"
)

type Pattern struct{}

type branchSpec struct {
	ID               string
	Title            string
	Input            string
	RequiredRole     team.Role
	VerifierRequired bool
}

func New() Pattern {
	return Pattern{}
}

func (Pattern) Name() string {
	return "collab"
}

func (Pattern) PlanTemplate(request team.StartRequest) (planner.Template, error) {
	if len(request.WorkerProfiles) == 0 {
		return planner.Template{}, fmt.Errorf("collab requires at least one worker profile")
	}
	branches := parseBranches(request.Input)
	if len(branches) == 0 {
		return planner.Template{}, fmt.Errorf("collab requires at least one branch task")
	}
	tasks := make([]planner.TaskSpec, 0, len(branches))
	verificationRequired := boolValue(request.Input["requireVerification"])
	for _, branch := range branches {
		verifierRequired := verificationRequired || branch.VerifierRequired
		tasks = append(tasks, planner.TaskSpec{
			ID:               branch.ID,
			Kind:             string(team.TaskKindResearch),
			Stage:            team.TaskStageImplement,
			Title:            branch.Title,
			Input:            branch.Input,
			RequiredRole:     branch.RequiredRole,
			Writes:           []string{implementWriteKey(branch.ID)},
			Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			Namespace:        implementNamespace(branch.ID),
			VerifierRequired: verifierRequired,
			FailurePolicy:    team.FailurePolicyFailFast,
		})
		verificationRequired = verificationRequired || branch.VerifierRequired
	}
	return planner.Template{
		Name: request.Pattern,
		Goal: goalForRequest(request),
		VerificationPolicy: planner.VerificationPolicy{
			Required: verificationRequired,
			Mode:     "collaboration_verification",
		},
		Tasks: tasks,
	}, nil
}

func (Pattern) Start(_ context.Context, request team.StartRequest) (team.RunState, error) {
	if len(request.WorkerProfiles) == 0 {
		return team.RunState{}, fmt.Errorf("collab requires at least one worker profile")
	}
	branches := parseBranches(request.Input)
	if len(branches) == 0 {
		return team.RunState{}, fmt.Errorf("collab requires at least one branch task")
	}
	workers := make([]team.Member, 0, len(request.WorkerProfiles))
	for idx, profile := range request.WorkerProfiles {
		workers = append(workers, team.Member{
			ID:          fmt.Sprintf("worker-%d", idx+1),
			Role:        team.RoleResearcher,
			ProfileName: profile,
		})
	}
	verificationRequired := boolValue(request.Input["requireVerification"])
	tasks := make([]team.Task, 0, len(branches))
	for idx, branch := range branches {
		assignee := workers[idx%len(workers)]
		verifierRequired := verificationRequired || branch.VerifierRequired
		task := team.Task{
			ID:               branch.ID,
			Kind:             team.TaskKindResearch,
			Stage:            team.TaskStageImplement,
			Title:            branch.Title,
			Input:            branch.Input,
			RequiredRole:     branch.RequiredRole,
			AssigneeAgentID:  assignee.ID,
			Writes:           []string{implementWriteKey(branch.ID)},
			Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			Namespace:        implementNamespace(branch.ID),
			VerifierRequired: verifierRequired,
			FailurePolicy:    team.FailurePolicyFailFast,
			Status:           team.TaskStatusPending,
		}
		task.Normalize()
		tasks = append(tasks, task)
		verificationRequired = verificationRequired || branch.VerifierRequired
	}
	now := time.Now().UTC()
	return team.RunState{
		ID:      request.TeamID,
		Pattern: "collab",
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
		RequireVerification: verificationRequired,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func (Pattern) Advance(_ context.Context, state team.RunState) (team.RunState, error) {
	state.UpdatedAt = time.Now().UTC()
	switch state.Phase {
	case team.PhaseResearch:
		if hasPendingStageTasks(state, team.TaskStageImplement) {
			return state, nil
		}
		if !hasStageTasks(state, team.TaskStageReview) {
			state.Tasks = append(state.Tasks, reviewTasks(state)...)
			return state, nil
		}
		if hasPendingStageTasks(state, team.TaskStageReview) {
			return state, nil
		}
		if requiresVerifier(state) {
			if !hasStageTasks(state, team.TaskStageVerify) {
				state.Tasks = append(state.Tasks, verifierTasks(state)...)
			}
			state.Phase = team.PhaseVerify
			return state, nil
		}
		if !hasStageTasks(state, team.TaskStageSynthesize) {
			state.Tasks = append(state.Tasks, synthesizeTask(state))
		}
		state.Phase = team.PhaseSynthesize
		return state, nil
	case team.PhaseVerify:
		if hasPendingStageTasks(state, team.TaskStageVerify) {
			return state, nil
		}
		if !hasStageTasks(state, team.TaskStageSynthesize) {
			state.Tasks = append(state.Tasks, synthesizeTask(state))
		}
		state.Phase = team.PhaseSynthesize
		return state, nil
	case team.PhaseSynthesize:
		if hasPendingStageTasks(state, team.TaskStageSynthesize) {
			return state, nil
		}
		state.Result = synthesizedResult(state)
		state.Status = team.StatusCompleted
		return state, nil
	default:
		return state, nil
	}
}

func parseBranches(input map[string]any) []branchSpec {
	if input == nil {
		return nil
	}
	for _, key := range []string{"branches", "tasks", "subqueries"} {
		if branches := branchesFromValue(input[key]); len(branches) > 0 {
			return branches
		}
	}
	goal, _ := input["query"].(string)
	if strings.TrimSpace(goal) == "" {
		goal, _ = input["goal"].(string)
	}
	if strings.TrimSpace(goal) == "" {
		return nil
	}
	return []branchSpec{{
		ID:           "task-1",
		Title:        goal,
		Input:        goal,
		RequiredRole: team.RoleResearcher,
	}}
}

func branchesFromValue(value any) []branchSpec {
	switch current := value.(type) {
	case []string:
		items := make([]branchSpec, 0, len(current))
		for idx, item := range current {
			items = append(items, newBranchSpec(idx, map[string]any{"title": item, "input": item}))
		}
		return items
	case []any:
		items := make([]branchSpec, 0, len(current))
		for idx, item := range current {
			switch cast := item.(type) {
			case string:
				items = append(items, newBranchSpec(idx, map[string]any{"title": cast, "input": cast}))
			case map[string]any:
				items = append(items, newBranchSpec(idx, cast))
			}
		}
		return items
	default:
		return nil
	}
}

func newBranchSpec(idx int, raw map[string]any) branchSpec {
	id, _ := raw["id"].(string)
	if strings.TrimSpace(id) == "" {
		id = fmt.Sprintf("task-%d", idx+1)
	}
	title, _ := raw["title"].(string)
	input, _ := raw["input"].(string)
	if strings.TrimSpace(title) == "" {
		title = input
	}
	if strings.TrimSpace(input) == "" {
		input = title
	}
	requiredRole, _ := raw["requiredRole"].(team.Role)
	if requiredRole == "" {
		if text, ok := raw["requiredRole"].(string); ok {
			requiredRole = team.Role(text)
		}
	}
	if requiredRole == "" {
		requiredRole = team.RoleResearcher
	}
	verifierRequired := boolValue(raw["verifierRequired"])
	return branchSpec{
		ID:               id,
		Title:            title,
		Input:            input,
		RequiredRole:     requiredRole,
		VerifierRequired: verifierRequired,
	}
}

func reviewTasks(state team.RunState) []team.Task {
	items := make([]team.Task, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage != team.TaskStageImplement || task.Result == nil {
			continue
		}
		reviewer := nextAssignee(state, task.EffectiveAssigneeAgentID())
		review := team.Task{
			ID:               fmt.Sprintf("%s-review", task.ID),
			Kind:             team.TaskKindResearch,
			Stage:            team.TaskStageReview,
			Title:            "review " + task.Title,
			Input:            task.Result.Summary,
			RequiredRole:     task.RequiredRole,
			AssigneeAgentID:  reviewer,
			DependsOn:        []string{task.ID},
			Reads:            []string{implementWriteKey(task.ID)},
			Writes:           []string{reviewWriteKey(task.ID)},
			Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			Namespace:        reviewNamespace(task.ID),
			VerifierRequired: task.VerifierRequired,
			FailurePolicy:    task.FailurePolicy,
			Status:           team.TaskStatusPending,
		}
		review.Normalize()
		items = append(items, review)
	}
	return items
}

func verifierTasks(state team.RunState) []team.Task {
	items := make([]team.Task, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage != team.TaskStageReview || task.Result == nil {
			continue
		}
		verify := team.Task{
			ID:               fmt.Sprintf("%s-verify", baseTaskID(task.ID)),
			Kind:             team.TaskKindVerify,
			Stage:            team.TaskStageVerify,
			Title:            "verify " + strings.TrimPrefix(task.Title, "review "),
			Input:            task.Result.Summary,
			RequiredRole:     task.RequiredRole,
			AssigneeAgentID:  task.EffectiveAssigneeAgentID(),
			DependsOn:        []string{task.ID},
			Reads:            []string{reviewWriteKey(baseTaskID(task.ID))},
			Writes:           []string{verifyWriteKey(baseTaskID(task.ID))},
			Publish:          []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
			Namespace:        verifyNamespace(baseTaskID(task.ID)),
			VerifierRequired: true,
			FailurePolicy:    task.FailurePolicy,
			Status:           team.TaskStatusPending,
		}
		verify.Normalize()
		items = append(items, verify)
	}
	return items
}

func synthesizeTask(state team.RunState) team.Task {
	reads := reviewReadKeys(state)
	dependsOn := stageDependencies(state, team.TaskStageReview)
	verifierRequired := requiresVerifier(state)
	if verifierRequired {
		reads = verifyReadKeys(state)
		dependsOn = stageDependencies(state, team.TaskStageVerify)
	}
	task := team.Task{
		ID:               "task-synthesize",
		Kind:             team.TaskKindSynthesize,
		Stage:            team.TaskStageSynthesize,
		Title:            "synthesize final collaboration output",
		Input:            "Synthesize the collaboration outputs into the final answer.",
		RequiredRole:     team.RoleSupervisor,
		AssigneeAgentID:  state.Supervisor.ID,
		Reads:            reads,
		Publish:          []team.OutputVisibility{team.OutputVisibilityShared},
		Namespace:        "synthesize.final",
		VerifierRequired: verifierRequired,
		FailurePolicy:    team.FailurePolicyFailFast,
		DependsOn:        dependsOn,
		Status:           team.TaskStatusPending,
	}
	task.Normalize()
	return task
}

func synthesizedResult(state team.RunState) *team.Result {
	task := latestCompletedStageTask(state, team.TaskStageSynthesize)
	if task == nil || task.Result == nil {
		return &team.Result{}
	}
	result := cloneResult(*task.Result)
	findings := stageFindings(state)
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
	return &result
}

func stageFindings(state team.RunState) []team.Finding {
	stage := team.TaskStageReview
	if requiresVerifier(state) {
		stage = team.TaskStageVerify
	}
	items := make([]team.Finding, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage != stage || task.Result == nil {
			continue
		}
		items = append(items, team.Finding{
			Summary:    task.Result.Summary,
			Evidence:   append([]team.Evidence{}, task.Result.Evidence...),
			Confidence: task.Result.Confidence,
		})
	}
	return items
}

func hasPendingStageTasks(state team.RunState, stage team.TaskStage) bool {
	for _, task := range state.Tasks {
		if task.Stage != stage {
			continue
		}
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			return true
		}
	}
	return false
}

func hasStageTasks(state team.RunState, stage team.TaskStage) bool {
	for _, task := range state.Tasks {
		if task.Stage == stage {
			return true
		}
	}
	return false
}

func requiresVerifier(state team.RunState) bool {
	if state.RequireVerification {
		return true
	}
	for _, task := range state.Tasks {
		if task.VerifierRequired {
			return true
		}
	}
	return false
}

func nextAssignee(state team.RunState, current string) string {
	if len(state.Workers) == 0 {
		return current
	}
	for idx, worker := range state.Workers {
		if worker.ID != current {
			continue
		}
		if len(state.Workers) == 1 {
			return worker.ID
		}
		return state.Workers[(idx+1)%len(state.Workers)].ID
	}
	return state.Workers[0].ID
}

func stageDependencies(state team.RunState, stage team.TaskStage) []string {
	items := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage == stage {
			items = append(items, task.ID)
		}
	}
	return items
}

func reviewReadKeys(state team.RunState) []string {
	items := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage == team.TaskStageReview {
			items = append(items, reviewWriteKey(baseTaskID(task.ID)))
		}
	}
	return items
}

func verifyReadKeys(state team.RunState) []string {
	items := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.Stage == team.TaskStageVerify {
			items = append(items, verifyWriteKey(baseTaskID(task.ID)))
		}
	}
	return items
}

func latestCompletedStageTask(state team.RunState, stage team.TaskStage) *team.Task {
	for idx := len(state.Tasks) - 1; idx >= 0; idx-- {
		if state.Tasks[idx].Stage == stage && state.Tasks[idx].Status == team.TaskStatusCompleted {
			return &state.Tasks[idx]
		}
	}
	return nil
}

func cloneResult(result team.Result) team.Result {
	clone := result
	clone.Structured = cloneStructured(result.Structured)
	clone.ArtifactIDs = append([]string{}, result.ArtifactIDs...)
	clone.Findings = append([]team.Finding{}, result.Findings...)
	clone.Evidence = append([]team.Evidence{}, result.Evidence...)
	return clone
}

func cloneStructured(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	maps.Copy(clone, values)
	return clone
}

func goalForRequest(request team.StartRequest) string {
	if request.Input == nil {
		return ""
	}
	if goal, ok := request.Input["query"].(string); ok && strings.TrimSpace(goal) != "" {
		return goal
	}
	goal, _ := request.Input["goal"].(string)
	return goal
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func implementNamespace(taskID string) string {
	return "impl." + taskID
}

func reviewNamespace(taskID string) string {
	return "review." + taskID
}

func verifyNamespace(taskID string) string {
	return "verify." + taskID
}

func implementWriteKey(taskID string) string {
	return "implement." + taskID
}

func reviewWriteKey(taskID string) string {
	return "review." + taskID
}

func verifyWriteKey(taskID string) string {
	return "verify." + taskID
}

func baseTaskID(taskID string) string {
	for _, suffix := range []string{"-review", "-verify"} {
		if trimmed, ok := strings.CutSuffix(taskID, suffix); ok {
			return trimmed
		}
	}
	return taskID
}
