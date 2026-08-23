package core

import (
	"github.com/Viking602/venat/api"
	corestate "github.com/Viking602/venat/internal/core/state"
)

func dependencyGate(task api.Task, tasks map[string]api.Task) (ready bool, fatal bool) {
	return corestate.DependencyGate(task, tasks)
}

func taskCanBecomeReady(status api.TaskStatus) bool {
	return corestate.TaskCanBecomeReady(status)
}

func isTerminalTask(status api.TaskStatus) bool {
	return corestate.IsTerminalTask(status)
}

func isTerminalRun(status api.RunStatus) bool {
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
