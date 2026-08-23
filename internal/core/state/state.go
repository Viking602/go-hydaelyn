package state

import (
	"time"

	"github.com/Viking602/venat/api"
)

var allowedRunTransitions = map[api.RunStatus]map[api.RunStatus]bool{
	api.RunStatusCreated: {
		api.RunStatusPlanning:          true,
		api.RunStatusWaitingUserInput:  true,
		api.RunStatusWaitingApproval:   true,
		api.RunStatusReconcileRequired: true,
		api.RunStatusBlocked:           true,
		api.RunStatusCancelled:         true,
		api.RunStatusFailed:            true,
	},
	api.RunStatusPlanning: {
		api.RunStatusValidating: true,
		api.RunStatusBlocked:    true,
		api.RunStatusFailed:     true,
		api.RunStatusCancelled:  true,
	},
	api.RunStatusValidating: {
		api.RunStatusRouting:   true,
		api.RunStatusBlocked:   true,
		api.RunStatusFailed:    true,
		api.RunStatusCancelled: true,
	},
	api.RunStatusRouting: {
		api.RunStatusDispatching: true,
		api.RunStatusBlocked:     true,
		api.RunStatusFailed:      true,
		api.RunStatusCancelled:   true,
	},
	api.RunStatusDispatching: {
		api.RunStatusRunning:   true,
		api.RunStatusBlocked:   true,
		api.RunStatusFailed:    true,
		api.RunStatusCancelled: true,
	},
	api.RunStatusRunning: {
		api.RunStatusExecuting:         true,
		api.RunStatusComposingResponse: true,
		api.RunStatusWaitingUserInput:  true,
		api.RunStatusWaitingApproval:   true,
		api.RunStatusReconcileRequired: true,
		api.RunStatusBlocked:           true,
		api.RunStatusFailed:            true,
		api.RunStatusCancelled:         true,
	},
	api.RunStatusWaitingApproval: {
		api.RunStatusExecuting:         true,
		api.RunStatusRunning:           true,
		api.RunStatusReconcileRequired: true,
		api.RunStatusBlocked:           true,
		api.RunStatusCancelled:         true,
	},
	api.RunStatusWaitingUserInput: {
		api.RunStatusRunning:   true,
		api.RunStatusBlocked:   true,
		api.RunStatusCancelled: true,
		api.RunStatusFailed:    true,
	},
	api.RunStatusReconcileRequired: {
		api.RunStatusExecuting: true,
		api.RunStatusRunning:   true,
		api.RunStatusBlocked:   true,
		api.RunStatusCancelled: true,
		api.RunStatusFailed:    true,
	},
	api.RunStatusExecuting: {
		api.RunStatusComposingResponse: true,
		api.RunStatusWaitingApproval:   true,
		api.RunStatusReconcileRequired: true,
		api.RunStatusBlocked:           true,
		api.RunStatusFailed:            true,
		api.RunStatusCancelled:         true,
	},
	api.RunStatusComposingResponse: {
		api.RunStatusCompleted: true,
		api.RunStatusBlocked:   true,
		api.RunStatusFailed:    true,
		api.RunStatusCancelled: true,
	},
	api.RunStatusBlocked: {
		api.RunStatusComposingResponse: true,
		api.RunStatusRunning:           true,
		api.RunStatusCancelled:         true,
		api.RunStatusFailed:            true,
	},
}

var allowedTaskTransitions = map[api.TaskStatus]map[api.TaskStatus]bool{
	api.TaskStatusCreated: {
		api.TaskStatusPlanned:    true,
		api.TaskStatusPaused:     true,
		api.TaskStatusCancelled:  true,
		api.TaskStatusFailed:     true,
		api.TaskStatusDispatched: true,
	},
	api.TaskStatusPlanned: {
		api.TaskStatusValidated: true,
		api.TaskStatusPaused:    true,
		api.TaskStatusBlocked:   true,
		api.TaskStatusCancelled: true,
		api.TaskStatusFailed:    true,
	},
	api.TaskStatusValidated: {
		api.TaskStatusRouted:            true,
		api.TaskStatusWaitingDependency: true,
		api.TaskStatusPaused:            true,
		api.TaskStatusBlocked:           true,
		api.TaskStatusCancelled:         true,
		api.TaskStatusFailed:            true,
	},
	api.TaskStatusRouted: {
		api.TaskStatusWaitingDependency: true,
		api.TaskStatusDispatched:        true,
		api.TaskStatusPaused:            true,
		api.TaskStatusBlocked:           true,
		api.TaskStatusCancelled:         true,
		api.TaskStatusFailed:            true,
	},
	api.TaskStatusWaitingDependency: {
		api.TaskStatusDispatched: true,
		api.TaskStatusPaused:     true,
		api.TaskStatusBlocked:    true,
		api.TaskStatusCancelled:  true,
		api.TaskStatusFailed:     true,
	},
	api.TaskStatusDispatched: {
		api.TaskStatusRunning:   true,
		api.TaskStatusPaused:    true,
		api.TaskStatusBlocked:   true,
		api.TaskStatusCancelled: true,
		api.TaskStatusFailed:    true,
	},
	api.TaskStatusRunning: {
		api.TaskStatusDispatched:        true,
		api.TaskStatusPaused:            true,
		api.TaskStatusWaitingUserInput:  true,
		api.TaskStatusReconcileRequired: true,
		api.TaskStatusBlocked:           true,
		api.TaskStatusCompleted:         true,
		api.TaskStatusFailed:            true,
		api.TaskStatusCancelled:         true,
	},
	api.TaskStatusPaused: {
		api.TaskStatusDispatched:       true,
		api.TaskStatusRunning:          true,
		api.TaskStatusWaitingUserInput: true,
		api.TaskStatusBlocked:          true,
		api.TaskStatusCancelled:        true,
		api.TaskStatusFailed:           true,
	},
	api.TaskStatusWaitingUserInput: {
		api.TaskStatusDispatched: true,
		api.TaskStatusRunning:    true,
		api.TaskStatusBlocked:    true,
		api.TaskStatusCancelled:  true,
		api.TaskStatusFailed:     true,
	},
	api.TaskStatusReconcileRequired: {
		api.TaskStatusDispatched: true,
		api.TaskStatusRunning:    true,
		api.TaskStatusBlocked:    true,
		api.TaskStatusCancelled:  true,
		api.TaskStatusFailed:     true,
	},
	api.TaskStatusBlocked: {
		api.TaskStatusDispatched: true,
		api.TaskStatusRunning:    true,
		api.TaskStatusPaused:     true,
		api.TaskStatusCancelled:  true,
		api.TaskStatusFailed:     true,
	},
}

func TransitionRun(run api.Run, to api.RunStatus) (api.Run, error) {
	if run.Status == to {
		return run, nil
	}
	if IsTerminalRun(run.Status) {
		return api.Run{}, api.ErrTerminalState
	}
	if !allowedRunTransitions[run.Status][to] {
		return api.Run{}, api.ErrInvalidTransition
	}
	run.Status = to
	run.UpdatedAt = time.Now().UTC()
	return run, nil
}

func TransitionTask(task api.Task, to api.TaskStatus, bumpVersion bool) (api.Task, error) {
	if task.Status == to {
		return task, nil
	}
	if IsTerminalTask(task.Status) {
		return api.Task{}, api.ErrTerminalState
	}
	if !allowedTaskTransitions[task.Status][to] {
		return api.Task{}, api.ErrInvalidTransition
	}
	task.Status = to
	if bumpVersion {
		task.Version++
	}
	task.UpdatedAt = time.Now().UTC()
	return task, nil
}

func DependencyGate(task api.Task, tasks map[string]api.Task) (ready bool, fatal bool) {
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
		case api.TaskStatusCompleted:
			completed++
		case api.TaskStatusFailed, api.TaskStatusCancelled:
			failed++
		}
	}
	if failed > 0 {
		switch task.OnDependencyFailed {
		case api.OnDependencyFailedFail:
			return false, true
		case api.OnDependencyFailedSkip:
			completed += failed
		}
	}
	switch task.AwaitMode {
	case api.AwaitModeAny:
		return completed >= 1, false
	case api.AwaitModeQuorum:
		threshold := task.AwaitQuorum
		if threshold <= 0 {
			threshold = 1
		}
		return completed >= threshold, false
	default:
		return completed == len(task.DependsOn), false
	}
}

func TaskCanBecomeReady(status api.TaskStatus) bool {
	switch status {
	case api.TaskStatusCreated, api.TaskStatusPlanned, api.TaskStatusValidated, api.TaskStatusRouted, api.TaskStatusWaitingDependency:
		return true
	default:
		return false
	}
}

func IsTerminalTask(status api.TaskStatus) bool {
	return status == api.TaskStatusCompleted || status == api.TaskStatusFailed || status == api.TaskStatusCancelled
}

func IsTerminalRun(status api.RunStatus) bool {
	return status == api.RunStatusCompleted || status == api.RunStatusFailed || status == api.RunStatusCancelled
}
