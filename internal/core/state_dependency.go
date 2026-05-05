package core

import (
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
)

func dependencyGate(task model.Task, tasks map[string]model.Task) (ready bool, fatal bool) {
	return corestate.DependencyGate(task, tasks)
}

func taskCanBecomeReady(status model.TaskStatus) bool {
	return corestate.TaskCanBecomeReady(status)
}

func isTerminalTask(status model.TaskStatus) bool {
	return corestate.IsTerminalTask(status)
}

func isTerminalRun(status model.RunStatus) bool {
	return corestate.IsTerminalRun(status)
}

func stringsCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
