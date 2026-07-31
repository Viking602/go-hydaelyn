package state

import (
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

var allowedRunTransitions = map[model.RunStatus]map[model.RunStatus]bool{
	model.RunStatusCreated: {
		model.RunStatusPlanning:          true,
		model.RunStatusWaitingUserInput:  true,
		model.RunStatusWaitingApproval:   true,
		model.RunStatusReconcileRequired: true,
		model.RunStatusBlocked:           true,
		model.RunStatusCancelled:         true,
		model.RunStatusFailed:            true,
	},
	model.RunStatusPlanning: {
		model.RunStatusValidating: true,
		model.RunStatusBlocked:    true,
		model.RunStatusFailed:     true,
		model.RunStatusCancelled:  true,
	},
	model.RunStatusValidating: {
		model.RunStatusRouting:   true,
		model.RunStatusBlocked:   true,
		model.RunStatusFailed:    true,
		model.RunStatusCancelled: true,
	},
	model.RunStatusRouting: {
		model.RunStatusDispatching: true,
		model.RunStatusBlocked:     true,
		model.RunStatusFailed:      true,
		model.RunStatusCancelled:   true,
	},
	model.RunStatusDispatching: {
		model.RunStatusRunning:   true,
		model.RunStatusBlocked:   true,
		model.RunStatusFailed:    true,
		model.RunStatusCancelled: true,
	},
	model.RunStatusRunning: {
		model.RunStatusExecuting:         true,
		model.RunStatusComposingResponse: true,
		model.RunStatusWaitingUserInput:  true,
		model.RunStatusWaitingApproval:   true,
		model.RunStatusReconcileRequired: true,
		model.RunStatusBlocked:           true,
		model.RunStatusFailed:            true,
		model.RunStatusCancelled:         true,
	},
	model.RunStatusWaitingApproval: {
		model.RunStatusExecuting:         true,
		model.RunStatusRunning:           true,
		model.RunStatusReconcileRequired: true,
		model.RunStatusBlocked:           true,
		model.RunStatusCancelled:         true,
	},
	model.RunStatusWaitingUserInput: {
		model.RunStatusRunning:   true,
		model.RunStatusBlocked:   true,
		model.RunStatusCancelled: true,
		model.RunStatusFailed:    true,
	},
	model.RunStatusReconcileRequired: {
		model.RunStatusExecuting: true,
		model.RunStatusRunning:   true,
		model.RunStatusBlocked:   true,
		model.RunStatusCancelled: true,
		model.RunStatusFailed:    true,
	},
	model.RunStatusExecuting: {
		model.RunStatusComposingResponse: true,
		model.RunStatusWaitingApproval:   true,
		model.RunStatusReconcileRequired: true,
		model.RunStatusBlocked:           true,
		model.RunStatusFailed:            true,
		model.RunStatusCancelled:         true,
	},
	model.RunStatusComposingResponse: {
		model.RunStatusCompleted: true,
		model.RunStatusBlocked:   true,
		model.RunStatusFailed:    true,
		model.RunStatusCancelled: true,
	},
	model.RunStatusBlocked: {
		model.RunStatusComposingResponse: true,
		model.RunStatusRunning:           true,
		model.RunStatusCancelled:         true,
		model.RunStatusFailed:            true,
	},
}

var allowedTaskTransitions = map[model.TaskStatus]map[model.TaskStatus]bool{
	model.TaskStatusCreated: {
		model.TaskStatusPlanned:    true,
		model.TaskStatusPaused:     true,
		model.TaskStatusCancelled:  true,
		model.TaskStatusFailed:     true,
		model.TaskStatusDispatched: true,
	},
	model.TaskStatusPlanned: {
		model.TaskStatusValidated: true,
		model.TaskStatusPaused:    true,
		model.TaskStatusBlocked:   true,
		model.TaskStatusCancelled: true,
		model.TaskStatusFailed:    true,
	},
	model.TaskStatusValidated: {
		model.TaskStatusRouted:            true,
		model.TaskStatusWaitingDependency: true,
		model.TaskStatusPaused:            true,
		model.TaskStatusBlocked:           true,
		model.TaskStatusCancelled:         true,
		model.TaskStatusFailed:            true,
	},
	model.TaskStatusRouted: {
		model.TaskStatusWaitingDependency: true,
		model.TaskStatusDispatched:        true,
		model.TaskStatusPaused:            true,
		model.TaskStatusBlocked:           true,
		model.TaskStatusCancelled:         true,
		model.TaskStatusFailed:            true,
	},
	model.TaskStatusWaitingDependency: {
		model.TaskStatusDispatched: true,
		model.TaskStatusPaused:     true,
		model.TaskStatusBlocked:    true,
		model.TaskStatusCancelled:  true,
		model.TaskStatusFailed:     true,
	},
	model.TaskStatusDispatched: {
		model.TaskStatusRunning:   true,
		model.TaskStatusPaused:    true,
		model.TaskStatusBlocked:   true,
		model.TaskStatusCancelled: true,
		model.TaskStatusFailed:    true,
	},
	model.TaskStatusRunning: {
		model.TaskStatusDispatched:        true,
		model.TaskStatusPaused:            true,
		model.TaskStatusWaitingUserInput:  true,
		model.TaskStatusReconcileRequired: true,
		model.TaskStatusBlocked:           true,
		model.TaskStatusCompleted:         true,
		model.TaskStatusFailed:            true,
		model.TaskStatusCancelled:         true,
	},
	model.TaskStatusPaused: {
		model.TaskStatusDispatched:       true,
		model.TaskStatusRunning:          true,
		model.TaskStatusWaitingUserInput: true,
		model.TaskStatusBlocked:          true,
		model.TaskStatusCancelled:        true,
		model.TaskStatusFailed:           true,
	},
	model.TaskStatusWaitingUserInput: {
		model.TaskStatusDispatched: true,
		model.TaskStatusRunning:    true,
		model.TaskStatusBlocked:    true,
		model.TaskStatusCancelled:  true,
		model.TaskStatusFailed:     true,
	},
	model.TaskStatusReconcileRequired: {
		model.TaskStatusDispatched: true,
		model.TaskStatusRunning:    true,
		model.TaskStatusBlocked:    true,
		model.TaskStatusCancelled:  true,
		model.TaskStatusFailed:     true,
	},
	model.TaskStatusBlocked: {
		model.TaskStatusDispatched: true,
		model.TaskStatusRunning:    true,
		model.TaskStatusPaused:     true,
		model.TaskStatusCancelled:  true,
		model.TaskStatusFailed:     true,
	},
}

func TransitionRun(run model.Run, to model.RunStatus) (model.Run, error) {
	if run.Status == to {
		return run, nil
	}
	if IsTerminalRun(run.Status) {
		return model.Run{}, model.ErrTerminalState
	}
	if !allowedRunTransitions[run.Status][to] {
		return model.Run{}, model.ErrInvalidTransition
	}
	run.Status = to
	run.UpdatedAt = time.Now().UTC()
	return run, nil
}

func TransitionTask(task model.Task, to model.TaskStatus, bumpVersion bool) (model.Task, error) {
	if task.Status == to {
		return task, nil
	}
	if IsTerminalTask(task.Status) {
		return model.Task{}, model.ErrTerminalState
	}
	if !allowedTaskTransitions[task.Status][to] {
		return model.Task{}, model.ErrInvalidTransition
	}
	task.Status = to
	if bumpVersion {
		task.Version++
	}
	task.UpdatedAt = time.Now().UTC()
	return task, nil
}

func DependencyGate(task model.Task, tasks map[string]model.Task) (ready bool, fatal bool) {
	if len(task.DependsOn) == 0 {
		return true, false
	}
	completed, failed := 0, 0
	for _, dep := range task.DependsOn {
		depTask, ok := tasks[dep]
		if !ok {
			continue
		}
		switch depTask.Status {
		case model.TaskStatusCompleted:
			completed++
		case model.TaskStatusFailed, model.TaskStatusCancelled:
			failed++
		}
	}
	if failed > 0 {
		switch task.OnDependencyFailed {
		case model.OnDependencyFailedFail:
			return false, true
		case model.OnDependencyFailedSkip:
			completed += failed
		}
	}
	switch task.AwaitMode {
	case model.AwaitModeAny:
		return completed >= 1, false
	case model.AwaitModeQuorum:
		threshold := task.AwaitQuorum
		if threshold <= 0 {
			threshold = 1
		}
		return completed >= threshold, false
	default:
		return completed == len(task.DependsOn), false
	}
}

func TaskCanBecomeReady(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusCreated, model.TaskStatusPlanned, model.TaskStatusValidated, model.TaskStatusRouted, model.TaskStatusWaitingDependency:
		return true
	default:
		return false
	}
}

func IsTerminalTask(status model.TaskStatus) bool {
	return status == model.TaskStatusCompleted || status == model.TaskStatusFailed || status == model.TaskStatusCancelled
}

func IsTerminalRun(status model.RunStatus) bool {
	return status == model.RunStatusCompleted || status == model.RunStatusFailed || status == model.RunStatusCancelled
}
