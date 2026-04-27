package runtime

import (
	"context"
	"time"
)

type TransitionRunCommand struct {
	RunID string
	To    RunStatus
}

type TransitionTaskCommand struct {
	RunID  string
	TaskID string
	To     TaskStatus
}

type AdvanceRunCommand struct {
	RunID string
}

var allowedRunTransitions = map[RunStatus]map[RunStatus]bool{
	RunStatusCreated: {
		RunStatusPlanning:          true,
		RunStatusWaitingUserInput:  true,
		RunStatusWaitingApproval:   true,
		RunStatusReconcileRequired: true,
		RunStatusBlocked:           true,
		RunStatusCancelled:         true,
		RunStatusFailed:            true,
	},
	RunStatusPlanning: {
		RunStatusValidating: true,
		RunStatusBlocked:    true,
		RunStatusFailed:     true,
		RunStatusCancelled:  true,
	},
	RunStatusValidating: {
		RunStatusRouting:   true,
		RunStatusBlocked:   true,
		RunStatusFailed:    true,
		RunStatusCancelled: true,
	},
	RunStatusRouting: {
		RunStatusDispatching: true,
		RunStatusBlocked:     true,
		RunStatusFailed:      true,
		RunStatusCancelled:   true,
	},
	RunStatusDispatching: {
		RunStatusRunning:   true,
		RunStatusBlocked:   true,
		RunStatusFailed:    true,
		RunStatusCancelled: true,
	},
	RunStatusRunning: {
		RunStatusSynthesizing:      true,
		RunStatusWaitingUserInput:  true,
		RunStatusWaitingApproval:   true,
		RunStatusReconcileRequired: true,
		RunStatusBlocked:           true,
		RunStatusFailed:            true,
		RunStatusCancelled:         true,
	},
	RunStatusSynthesizing: {
		RunStatusReviewing:         true,
		RunStatusComposingResponse: true,
		RunStatusBlocked:           true,
		RunStatusFailed:            true,
		RunStatusCancelled:         true,
	},
	RunStatusReviewing: {
		RunStatusExecuting:         true,
		RunStatusComposingResponse: true,
		RunStatusBlocked:           true,
		RunStatusFailed:            true,
		RunStatusCancelled:         true,
	},
	RunStatusWaitingApproval: {
		RunStatusExecuting:         true,
		RunStatusRunning:           true,
		RunStatusReconcileRequired: true,
		RunStatusBlocked:           true,
		RunStatusCancelled:         true,
	},
	RunStatusWaitingUserInput: {
		RunStatusRunning:   true,
		RunStatusBlocked:   true,
		RunStatusCancelled: true,
		RunStatusFailed:    true,
	},
	RunStatusReconcileRequired: {
		RunStatusExecuting: true,
		RunStatusRunning:   true,
		RunStatusBlocked:   true,
		RunStatusCancelled: true,
		RunStatusFailed:    true,
	},
	RunStatusExecuting: {
		RunStatusComposingResponse: true,
		RunStatusWaitingApproval:   true,
		RunStatusReconcileRequired: true,
		RunStatusBlocked:           true,
		RunStatusFailed:            true,
		RunStatusCancelled:         true,
	},
	RunStatusComposingResponse: {
		RunStatusCompleted: true,
		RunStatusBlocked:   true,
		RunStatusFailed:    true,
		RunStatusCancelled: true,
	},
	RunStatusBlocked: {
		RunStatusComposingResponse: true,
		RunStatusRunning:           true,
		RunStatusCancelled:         true,
		RunStatusFailed:            true,
	},
}

var allowedTaskTransitions = map[TaskStatus]map[TaskStatus]bool{
	TaskStatusCreated: {
		TaskStatusPlanned:    true,
		TaskStatusPaused:     true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
		TaskStatusDispatched: true,
	},
	TaskStatusPlanned: {
		TaskStatusValidated: true,
		TaskStatusPaused:    true,
		TaskStatusBlocked:   true,
		TaskStatusCancelled: true,
		TaskStatusFailed:    true,
	},
	TaskStatusValidated: {
		TaskStatusRouted:            true,
		TaskStatusWaitingDependency: true,
		TaskStatusPaused:            true,
		TaskStatusBlocked:           true,
		TaskStatusCancelled:         true,
		TaskStatusFailed:            true,
	},
	TaskStatusRouted: {
		TaskStatusWaitingDependency: true,
		TaskStatusDispatched:        true,
		TaskStatusPaused:            true,
		TaskStatusBlocked:           true,
		TaskStatusCancelled:         true,
		TaskStatusFailed:            true,
	},
	TaskStatusWaitingDependency: {
		TaskStatusDispatched: true,
		TaskStatusPaused:     true,
		TaskStatusBlocked:    true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
	},
	TaskStatusDispatched: {
		TaskStatusRunning:   true,
		TaskStatusPaused:    true,
		TaskStatusBlocked:   true,
		TaskStatusCancelled: true,
		TaskStatusFailed:    true,
	},
	TaskStatusRunning: {
		TaskStatusDispatched:        true,
		TaskStatusPaused:            true,
		TaskStatusWaitingUserInput:  true,
		TaskStatusReconcileRequired: true,
		TaskStatusBlocked:           true,
		TaskStatusCompleted:         true,
		TaskStatusFailed:            true,
		TaskStatusCancelled:         true,
	},
	TaskStatusPaused: {
		TaskStatusDispatched:       true,
		TaskStatusRunning:          true,
		TaskStatusWaitingUserInput: true,
		TaskStatusBlocked:          true,
		TaskStatusCancelled:        true,
		TaskStatusFailed:           true,
	},
	TaskStatusWaitingUserInput: {
		TaskStatusDispatched: true,
		TaskStatusRunning:    true,
		TaskStatusBlocked:    true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
	},
	TaskStatusReconcileRequired: {
		TaskStatusDispatched: true,
		TaskStatusRunning:    true,
		TaskStatusBlocked:    true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
	},
	TaskStatusBlocked: {
		TaskStatusDispatched: true,
		TaskStatusRunning:    true,
		TaskStatusPaused:     true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
	},
}

func (r *Runtime) TransitionRun(_ context.Context, cmd TransitionRunCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return ErrNotFound
	}
	next, err := r.transitionRunLocked(run, cmd.To)
	if err != nil {
		return err
	}
	r.runs[next.ID] = next
	return nil
}

func (r *Runtime) TransitionTask(_ context.Context, cmd TransitionTaskCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return ErrNotFound
	}
	next, err := r.transitionTaskLocked(task, cmd.To)
	if err != nil {
		return err
	}
	r.tasks[next.RunID][next.ID] = next
	return nil
}

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return Run{}, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return Run{}, ErrTerminalState
	}
	span := r.startTraceSpanLocked(StartTraceSpanCommand{
		RunID:     run.ID,
		TaskID:    run.RootTaskID,
		Name:      "runtime.pipeline",
		Component: "orchestrator",
	})
	var err error
	defer func() {
		r.finishTraceSpanLocked(span, err)
	}()
	run, plan, err := r.createPipelinePlanLocked(ctx, run)
	if err != nil {
		return Run{}, err
	}
	run, err = r.validatePipelinePlanLocked(ctx, run, plan)
	if err != nil {
		return Run{}, err
	}
	run, routing, err := r.routePipelinePlanLocked(ctx, run, plan)
	if err != nil {
		return Run{}, err
	}
	run, err = r.transitionRunLocked(run, RunStatusDispatching)
	if err != nil {
		return Run{}, err
	}
	if err = r.dispatchRoutingLocked(ctx, run, routing); err != nil {
		return Run{}, err
	}
	run, err = r.transitionRunLocked(run, RunStatusRunning)
	if err != nil {
		return Run{}, err
	}
	if err = r.pipeline.TaskMonitor.Advance(ctx, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (r *Runtime) createPipelinePlanLocked(ctx context.Context, run Run) (Run, TodoPlan, error) {
	run, err := r.transitionRunLocked(run, RunStatusPlanning)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	intent, err := r.pipeline.IntentAnalyzer.AnalyzeIntent(ctx, run)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	r.appendEventLocked(run.ID, run.RootTaskID, EventIntentAnalyzed, map[string]any{
		"summary": intent.Summary,
	})
	plan, err := r.pipeline.Planner.CreatePlan(ctx, intent)
	if err != nil {
		return Run{}, TodoPlan{}, err
	}
	if plan.RunID == "" {
		plan.RunID = run.ID
	}
	if len(plan.Tasks) == 0 {
		plan.Tasks = []Task{r.tasks[run.ID][run.RootTaskID]}
	}
	r.preparePlanTasksLocked(run, plan)
	r.appendEventLocked(run.ID, run.RootTaskID, EventPlanCreated, map[string]any{
		"taskCount": len(plan.Tasks),
	})
	return run, plan, nil
}

func (r *Runtime) preparePlanTasksLocked(run Run, plan TodoPlan) {
	for _, planned := range plan.Tasks {
		if planned.ID == "" || planned.ID == run.RootTaskID {
			continue
		}
		planned = normalizePlannedTask(run.ID, planned)
		if r.tasks[run.ID] == nil {
			r.tasks[run.ID] = map[string]Task{}
		}
		if _, exists := r.tasks[run.ID][planned.ID]; exists {
			continue
		}
		r.tasks[run.ID][planned.ID] = planned
		r.appendEventLocked(run.ID, planned.ID, EventTaskCreated, taskEventPayload(planned))
	}
}

func normalizePlannedTask(runID string, task Task) Task {
	if task.RunID == "" {
		task.RunID = runID
	}
	if task.Status == "" {
		task.Status = TaskStatusPlanned
	}
	if task.Version == 0 {
		task.Version = 1
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.UpdatedAt = task.CreatedAt
	return task
}

func (r *Runtime) validatePipelinePlanLocked(ctx context.Context, run Run, plan TodoPlan) (Run, error) {
	var err error
	run, err = r.transitionRunLocked(run, RunStatusValidating)
	if err != nil {
		return Run{}, err
	}
	if err = r.pipeline.Validator.ValidatePlan(ctx, plan); err != nil {
		return Run{}, err
	}
	r.appendEventLocked(run.ID, run.RootTaskID, EventPlanValidated, map[string]any{
		"valid": true,
	})
	for _, planned := range plan.Tasks {
		task := r.tasks[run.ID][planned.ID]
		if task.ID != "" && task.Status == TaskStatusPlanned {
			if _, err = r.transitionTaskLocked(task, TaskStatusValidated); err != nil {
				return Run{}, err
			}
		}
	}
	return run, nil
}

func (r *Runtime) routePipelinePlanLocked(ctx context.Context, run Run, plan TodoPlan) (Run, RoutingPlan, error) {
	var err error
	run, err = r.transitionRunLocked(run, RunStatusRouting)
	if err != nil {
		return Run{}, RoutingPlan{}, err
	}
	routing, err := r.pipeline.Router.RouteTasks(ctx, plan)
	if err != nil {
		return Run{}, RoutingPlan{}, err
	}
	if routing.RunID == "" {
		routing.RunID = run.ID
	}
	if len(routing.Routes) == 0 {
		for _, task := range plan.Tasks {
			routing.Routes = append(routing.Routes, TaskRoute{
				TaskID:          task.ID,
				TargetAgentID:   task.OwnerAgentID,
				TargetComponent: task.OwnerComponent,
			})
		}
	}
	r.appendEventLocked(run.ID, run.RootTaskID, EventRoutingPlanCreated, map[string]any{
		"routeCount": len(routing.Routes),
	})
	for _, route := range routing.Routes {
		task := r.tasks[run.ID][route.TaskID]
		if task.ID != "" && task.Status == TaskStatusValidated {
			if _, err = r.transitionTaskLocked(task, TaskStatusRouted); err != nil {
				return Run{}, RoutingPlan{}, err
			}
		}
	}
	return run, routing, nil
}

func (r *Runtime) dispatchRoutingLocked(ctx context.Context, run Run, routing RoutingPlan) error {
	envelopes, err := r.pipeline.Dispatcher.Dispatch(ctx, routing)
	if err != nil {
		return err
	}
	for _, env := range envelopes {
		task := r.tasks[run.ID][env.TaskID]
		if task.ID == "" || isTerminalTask(task.Status) {
			continue
		}
		if len(task.DependsOn) > 0 && !r.dependenciesCompletedLocked(run.ID, task.DependsOn) {
			continue
		}
		if _, err = r.authorizeLocked(ctx, PolicyRequest{
			Operation: PolicyOperationDispatch,
			RunID:     run.ID,
			TaskID:    task.ID,
			Actor:     SourceIdentity{Type: SourceComponent, ID: "dispatcher"},
		}); err != nil {
			return err
		}
		task, err = r.transitionTaskPreserveVersionLocked(task, TaskStatusDispatched)
		if err != nil {
			return err
		}
		env.RunID = run.ID
		env.TaskID = task.ID
		env.TargetAgentID = firstNonEmpty(env.TargetAgentID, task.OwnerAgentID)
		env.TargetComponent = firstNonEmpty(env.TargetComponent, task.OwnerComponent)
		env.TaskVersion = task.Version
		env.CreatedAt = time.Now().UTC()
		r.recordTraceLocked(run.ID, task.ID, "mailbox.dispatch", "mailbox")
		r.writeEnvelopeLocked(env)
	}
	return nil
}

func (r *Runtime) transitionRunLocked(run Run, to RunStatus) (Run, error) {
	if run.Status == to {
		return run, nil
	}
	if isTerminalRun(run.Status) {
		return Run{}, ErrTerminalState
	}
	if !allowedRunTransitions[run.Status][to] {
		return Run{}, ErrInvalidTransition
	}
	from := run.Status
	run.Status = to
	run.UpdatedAt = time.Now().UTC()
	r.runs[run.ID] = run
	r.appendEventLocked(run.ID, run.RootTaskID, EventRunStatusChanged, map[string]any{
		"from": string(from),
		"to":   string(to),
		"run":  runPayload(run),
	})
	return run, nil
}

func (r *Runtime) transitionTaskLocked(task Task, to TaskStatus) (Task, error) {
	return r.transitionTaskStatusLocked(task, to, true)
}

func (r *Runtime) transitionTaskPreserveVersionLocked(task Task, to TaskStatus) (Task, error) {
	return r.transitionTaskStatusLocked(task, to, false)
}

func (r *Runtime) transitionTaskStatusLocked(task Task, to TaskStatus, bumpVersion bool) (Task, error) {
	if task.Status == to {
		return task, nil
	}
	if isTerminalTask(task.Status) {
		return Task{}, ErrTerminalState
	}
	if !allowedTaskTransitions[task.Status][to] {
		return Task{}, ErrInvalidTransition
	}
	task.Status = to
	if bumpVersion {
		task.Version++
	}
	task.UpdatedAt = time.Now().UTC()
	r.tasks[task.RunID][task.ID] = task
	return task, nil
}
