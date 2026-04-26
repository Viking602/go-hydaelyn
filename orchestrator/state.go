package orchestrator

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
		RunStatusPlanning:  true,
		RunStatusCancelled: true,
		RunStatusFailed:    true,
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
		RunStatusSynthesizing:    true,
		RunStatusWaitingApproval: true,
		RunStatusBlocked:         true,
		RunStatusFailed:          true,
		RunStatusCancelled:       true,
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
		RunStatusExecuting: true,
		RunStatusRunning:   true,
		RunStatusBlocked:   true,
		RunStatusCancelled: true,
	},
	RunStatusExecuting: {
		RunStatusComposingResponse: true,
		RunStatusWaitingApproval:   true,
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
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
		TaskStatusDispatched: true,
	},
	TaskStatusPlanned: {
		TaskStatusValidated: true,
		TaskStatusBlocked:   true,
		TaskStatusCancelled: true,
		TaskStatusFailed:    true,
	},
	TaskStatusValidated: {
		TaskStatusRouted:            true,
		TaskStatusWaitingDependency: true,
		TaskStatusBlocked:           true,
		TaskStatusCancelled:         true,
		TaskStatusFailed:            true,
	},
	TaskStatusRouted: {
		TaskStatusWaitingDependency: true,
		TaskStatusDispatched:        true,
		TaskStatusBlocked:           true,
		TaskStatusCancelled:         true,
		TaskStatusFailed:            true,
	},
	TaskStatusWaitingDependency: {
		TaskStatusDispatched: true,
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
		TaskStatusDispatched: true,
		TaskStatusPaused:     true,
		TaskStatusBlocked:    true,
		TaskStatusCompleted:  true,
		TaskStatusFailed:     true,
		TaskStatusCancelled:  true,
	},
	TaskStatusPaused: {
		TaskStatusDispatched: true,
		TaskStatusRunning:    true,
		TaskStatusBlocked:    true,
		TaskStatusCancelled:  true,
		TaskStatusFailed:     true,
	},
	TaskStatusBlocked: {
		TaskStatusDispatched: true,
		TaskStatusRunning:    true,
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

func (r *Runtime) AdvanceRun(_ context.Context, cmd AdvanceRunCommand) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return Run{}, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return Run{}, ErrTerminalState
	}
	var err error
	for _, status := range []RunStatus{
		RunStatusPlanning,
		RunStatusValidating,
		RunStatusRouting,
		RunStatusDispatching,
		RunStatusRunning,
	} {
		run, err = r.transitionRunLocked(run, status)
		if err != nil {
			return Run{}, err
		}
	}
	r.appendEventLocked(run.ID, run.RootTaskID, EventIntentAnalyzed, map[string]any{
		"summary": run.Request,
	})
	plan := TodoPlan{RunID: run.ID, Tasks: []Task{r.tasks[run.ID][run.RootTaskID]}}
	r.appendEventLocked(run.ID, run.RootTaskID, EventPlanCreated, map[string]any{
		"taskCount": len(plan.Tasks),
	})
	r.appendEventLocked(run.ID, run.RootTaskID, EventPlanValidated, map[string]any{
		"valid": true,
	})
	r.appendEventLocked(run.ID, run.RootTaskID, EventRoutingPlanCreated, map[string]any{
		"routeCount": len(plan.Tasks),
	})
	root := r.tasks[run.ID][run.RootTaskID]
	if !isTerminalTask(root.Status) {
		root.Status = TaskStatusDispatched
		root.UpdatedAt = time.Now().UTC()
		r.tasks[root.RunID][root.ID] = root
		r.writeEnvelopeLocked(TaskEnvelope{
			ID:              r.newID("env"),
			RunID:           root.RunID,
			TaskID:          root.ID,
			TargetComponent: root.OwnerComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			TaskVersion:     root.Version,
			CreatedAt:       time.Now().UTC(),
		})
	}
	return run, nil
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
	task.Version++
	task.UpdatedAt = time.Now().UTC()
	r.tasks[task.RunID][task.ID] = task
	return task, nil
}
