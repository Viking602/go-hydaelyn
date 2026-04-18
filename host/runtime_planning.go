package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/middleware"
	"github.com/Viking602/go-hydaelyn/planner"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/team"
)

func (r *Runtime) lookupPlanner(name string) (planner.Planner, error) {
	spec, ok := r.plugins.Lookup(plugin.TypePlanner, name)
	if !ok {
		return nil, fmt.Errorf("planner not found: %s", name)
	}
	driver, ok := spec.Component.(planner.Planner)
	if !ok {
		return nil, fmt.Errorf("plugin %s/%s does not implement planner.Planner", spec.Type, spec.Name)
	}
	return driver, nil
}

func (r *Runtime) startPlannedTeam(ctx context.Context, pattern team.Pattern, request team.StartRequest) (team.RunState, error) {
	driver, err := r.lookupPlanner(request.Planner)
	if err != nil {
		return team.RunState{}, err
	}
	template, err := r.plannerTemplate(pattern, request)
	if err != nil {
		return team.RunState{}, err
	}
	goal, _ := request.Input["query"].(string)
	if strings.TrimSpace(goal) == "" {
		goal = template.Goal
	}
	plan, err := driver.Plan(ctx, planner.PlanRequest{
		TeamID:            request.TeamID,
		Pattern:           request.Pattern,
		Planner:           request.Planner,
		Goal:              goal,
		Input:             request.Input,
		SupervisorProfile: request.SupervisorProfile,
		WorkerProfiles:    request.WorkerProfiles,
		Metadata:          request.Metadata,
		Template:          template,
	})
	if err != nil {
		return team.RunState{}, err
	}
	return r.buildPlannedState(request, plan)
}

func (r *Runtime) plannerTemplate(pattern team.Pattern, request team.StartRequest) (planner.Template, error) {
	provider, ok := pattern.(planner.TemplateProvider)
	if !ok {
		return planner.Template{}, nil
	}
	return provider.PlanTemplate(request)
}

func (r *Runtime) buildPlannedState(request team.StartRequest, plan planner.Plan) (team.RunState, error) {
	supervisor, workers, err := r.instantiateAgents(request)
	if err != nil {
		return team.RunState{}, err
	}
	tasks, err := r.planTasksToRuntimeTasks(plan.Tasks, workers, nil)
	if err != nil {
		return team.RunState{}, err
	}
	requireVerification := plan.VerificationPolicy.Required
	if !requireVerification {
		requireVerification, _ = request.Input["requireVerification"].(bool)
	}
	return team.RunState{
		ID:         request.TeamID,
		Pattern:    request.Pattern,
		Status:     team.StatusRunning,
		Phase:      team.PhaseResearch,
		Supervisor: supervisor,
		Workers:    workers,
		Tasks:      tasks,
		Planning: &team.PlanningState{
			PlannerName:     request.Planner,
			Goal:            plan.Goal,
			SuccessCriteria: append([]string{}, plan.SuccessCriteria...),
			PlanVersion:     1,
		},
		Input:               request.Input,
		Metadata:            request.Metadata,
		RequireVerification: requireVerification,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}, nil
}

func (r *Runtime) instantiateAgents(request team.StartRequest) (team.AgentInstance, []team.AgentInstance, error) {
	supervisorProfile, err := r.lookupProfile(request.SupervisorProfile)
	if err != nil {
		return team.AgentInstance{}, nil, err
	}
	supervisor := team.AgentInstance{
		ID:          "supervisor",
		Role:        supervisorProfile.Role,
		ProfileName: supervisorProfile.Name,
		Budget:      supervisorProfile.DefaultBudget,
	}
	workers := make([]team.AgentInstance, 0, len(request.WorkerProfiles))
	for idx, name := range request.WorkerProfiles {
		profile, err := r.lookupProfile(name)
		if err != nil {
			return team.AgentInstance{}, nil, err
		}
		workers = append(workers, team.AgentInstance{
			ID:          fmt.Sprintf("worker-%d", idx+1),
			Role:        profile.Role,
			ProfileName: profile.Name,
			Budget:      profile.DefaultBudget,
		})
	}
	return supervisor, workers, nil
}

func (r *Runtime) planTasksToRuntimeTasks(specs []planner.TaskSpec, workers []team.AgentInstance, existing []team.Task) ([]team.Task, error) {
	assignments := assignmentCounts(existing)
	tasks := make([]team.Task, 0, len(specs))
	for _, spec := range specs {
		assignee := spec.AssigneeAgentID
		if assignee == "" {
			selected, err := r.selectAgentForPlanTask(spec, workers, assignments)
			if err != nil {
				return nil, err
			}
			assignee = selected
		}
		assignments[assignee]++
		kind := team.TaskKind(spec.Kind)
		if kind == "" {
			kind = team.TaskKindResearch
		}
		task := team.Task{
			ID:                   spec.ID,
			Kind:                 kind,
			Stage:                spec.Stage,
			Title:                spec.Title,
			Input:                spec.Input,
			RequiredRole:         spec.RequiredRole,
			RequiredCapabilities: append([]string{}, spec.RequiredCapabilities...),
			Budget:               spec.Budget,
			AssigneeAgentID:      assignee,
			DependsOn:            append([]string{}, spec.DependsOn...),
			Reads:                append([]string{}, spec.Reads...),
			Writes:               append([]string{}, spec.Writes...),
			Publish:              append([]team.OutputVisibility{}, spec.Publish...),
			Namespace:            spec.Namespace,
			VerifierRequired:     spec.VerifierRequired,
			FailurePolicy:        spec.FailurePolicy,
			Status:               team.TaskStatusPending,
		}
		task.Normalize()
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func assignmentCounts(tasks []team.Task) map[string]int {
	counts := map[string]int{}
	for _, task := range tasks {
		if task.EffectiveAssigneeAgentID() == "" {
			continue
		}
		counts[task.EffectiveAssigneeAgentID()]++
	}
	return counts
}

func (r *Runtime) selectAgentForPlanTask(spec planner.TaskSpec, workers []team.AgentInstance, assignments map[string]int) (string, error) {
	type candidate struct {
		id        string
		load      int
		saturated bool
	}
	candidates := make([]candidate, 0, len(workers))
	for _, worker := range workers {
		if spec.RequiredRole != "" && worker.Role != spec.RequiredRole {
			continue
		}
		profile, err := r.lookupProfile(worker.EffectiveProfileName())
		if err != nil {
			return "", err
		}
		if !hasCapabilities(profile.ToolNames, spec.RequiredCapabilities) {
			continue
		}
		if !effectiveBudget(worker, profile).Covers(spec.Budget) {
			continue
		}
		load := assignments[worker.ID]
		saturated := profile.MaxConcurrency > 0 && load >= profile.MaxConcurrency
		candidates = append(candidates, candidate{
			id:        worker.ID,
			load:      load,
			saturated: saturated,
		})
	}
	bestID := ""
	bestLoad := int(^uint(0) >> 1)
	for _, current := range candidates {
		if current.saturated {
			continue
		}
		if current.load < bestLoad {
			bestID = current.id
			bestLoad = current.load
		}
	}
	if bestID != "" {
		return bestID, nil
	}
	for _, current := range candidates {
		if current.load < bestLoad {
			bestID = current.id
			bestLoad = current.load
		}
	}
	if bestID == "" {
		return "", fmt.Errorf("no worker matches role %s, capabilities %v, and budget %+v", spec.RequiredRole, spec.RequiredCapabilities, spec.Budget)
	}
	return bestID, nil
}

func effectiveBudget(worker team.AgentInstance, profile team.Profile) team.Budget {
	if worker.Budget != (team.Budget{}) {
		return worker.Budget
	}
	return profile.DefaultBudget
}

func hasCapabilities(granted, required []string) bool {
	if len(required) == 0 {
		return true
	}
	available := map[string]struct{}{}
	for _, item := range granted {
		available[item] = struct{}{}
	}
	for _, item := range required {
		if _, ok := available[item]; !ok {
			return false
		}
	}
	return true
}

func (r *Runtime) reviewPlannedTeam(ctx context.Context, current team.RunState) (team.RunState, bool, bool, error) {
	if current.Planning == nil || current.Planning.PlannerName == "" {
		return current, false, false, nil
	}
	driver, err := r.lookupPlanner(current.Planning.PlannerName)
	if err != nil {
		return team.RunState{}, false, false, err
	}
	var decision planner.ReviewDecision
	err = r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StagePlanner,
		Operation: "review",
		TeamID:    current.ID,
		Request:   current,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		item, runErr := driver.Review(ctx, planner.ReviewInput{State: current})
		if runErr != nil {
			return runErr
		}
		decision = item
		envelope.Response = item
		return nil
	})
	if err != nil {
		return team.RunState{}, false, false, err
	}
	next := current
	next.UpdatedAt = time.Now().UTC()
	next.Planning.ReviewCount++
	next.Planning.LastAction = string(decision.Action)
	next.Planning.LastActionReason = decision.Reason
	switch decision.Action {
	case planner.ReviewActionContinue, planner.ReviewActionComplete:
		return next, false, false, nil
	case planner.ReviewActionReplan:
		updated, err := r.replanTeam(ctx, driver, next)
		if err != nil {
			return team.RunState{}, false, false, err
		}
		persisted, err := r.persistTeamProgress(ctx, updated)
		if err != nil {
			return team.RunState{}, false, false, err
		}
		return persisted, true, persisted.IsTerminal(), nil
	case planner.ReviewActionAbort:
		return r.finishReviewedTeam(ctx, next, team.StatusAborted, decision.Reason)
	case planner.ReviewActionAskHuman:
		return r.finishReviewedTeam(ctx, next, team.StatusPaused, decision.Reason)
	case planner.ReviewActionEscalate:
		return r.finishReviewedTeam(ctx, next, team.StatusFailed, decision.Reason)
	default:
		return next, false, false, nil
	}
}

func (r *Runtime) replanTeam(ctx context.Context, driver planner.Planner, current team.RunState) (team.RunState, error) {
	var plan planner.Plan
	err := r.runStage(ctx, &middleware.Envelope{
		Stage:     middleware.StagePlanner,
		Operation: "replan",
		TeamID:    current.ID,
		Request:   current,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		item, runErr := driver.Replan(ctx, planner.ReplanInput{State: current})
		if runErr != nil {
			return runErr
		}
		plan = item
		envelope.Response = item
		return nil
	})
	if err != nil {
		return team.RunState{}, err
	}
	tasks, err := r.planTasksToRuntimeTasks(plan.Tasks, current.Workers, current.Tasks)
	if err != nil {
		return team.RunState{}, err
	}
	next := current
	next.Tasks = append(next.Tasks, tasks...)
	next.UpdatedAt = time.Now().UTC()
	next.Planning.PlanVersion++
	return next, nil
}

func (r *Runtime) finishReviewedTeam(ctx context.Context, state team.RunState, status team.Status, reason string) (team.RunState, bool, bool, error) {
	state.Status = status
	if state.Result == nil {
		state.Result = &team.Result{}
	}
	state.Result.Error = reason
	state.UpdatedAt = time.Now().UTC()
	if err := r.saveTeam(ctx, &state); err != nil {
		return team.RunState{}, false, false, err
	}
	return state, true, true, nil
}
