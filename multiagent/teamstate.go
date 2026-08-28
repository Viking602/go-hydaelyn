package multiagent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
)

// Schedulers must be pure, stateless functions of TeamState: every decision is
// derived from the AgentInstances and Tasks in the snapshot, never from hidden
// mutable scheduler state. This keeps replay and recovery deterministic.

// hasActiveInstance reports whether any instance is still pending or
// running, in which case a Scheduler should wait rather than dispatch more
// work this tick.
func (s TeamState) hasActiveInstance() bool {
	for _, instance := range s.Instances {
		if instance.State == InstanceStatePending || instance.State == InstanceStateRunning {
			return true
		}
	}
	return false
}

// hasFailedInstance reports whether any instance has failed. The reference
// Schedulers treat a failure as terminal because AgentInstance carries no
// retryable signal (that lives on agent.Result.Failure, off the snapshot).
func (s TeamState) hasFailedInstance() bool {
	for _, instance := range s.Instances {
		if instance.State == InstanceStateFailed {
			return true
		}
	}
	return false
}

// finishedClasses returns the set of AgentClass names that have a finished
// instance in this snapshot.
func (s TeamState) finishedClasses() map[string]bool {
	out := make(map[string]bool, len(s.Instances))
	for _, instance := range s.Instances {
		if instance.State == InstanceStateFinished {
			out[instance.ClassName] = true
		}
	}
	return out
}

// reportForClass returns the TypedReport produced by the finished instance
// of className, resolved through that instance's TaskID, or nil when no
// finished instance or report exists.
//
// When multiple finished instances share a ClassName (e.g. a scheduler
// re-dispatches the same class in a retry loop), the LATEST finished
// instance wins: Drive appends instances in dispatch order, so the last
// match is the most recent run. Reading the earliest match would freeze the
// scheduler on the original decision and loop until MaxTicks — see
// TestDriveReportForClassUsesLatestFinishedInstance. Iterating in reverse
// keeps Next a pure function of the snapshot (the snapshot order is fixed by
// the append order, not by completion timing), so replay determinism is
// preserved.
func (s TeamState) reportForClass(className string) *api.TypedReport {
	var taskID string
	for i := len(s.Instances) - 1; i >= 0; i-- {
		instance := s.Instances[i]
		if instance.ClassName == className && instance.State == InstanceStateFinished {
			taskID = instance.TaskID
			break
		}
	}
	if taskID == "" {
		return nil
	}
	for i := len(s.Tasks) - 1; i >= 0; i-- {
		task := s.Tasks[i]
		if task.ID == taskID {
			return task.Result
		}
	}
	return nil
}

// reportInput marshals a finished class's report into the Input payload for
// the next dispatched agent, threading one step's output into the next.
func (s TeamState) reportInput(className string) (json.RawMessage, error) {
	report := s.reportForClass(className)
	if report == nil {
		return nil, nil
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("marshal report for class %q: %w", className, err)
	}
	return raw, nil
}

// buildDispatch assembles a Dispatch for class as the step-th agent in a run.
func taskIDForClass(runID, className string) string {
	return runID + "-" + className
}

func classNameFromTaskID(runID, taskID string) string {
	className := strings.TrimPrefix(taskID, runID+"-")
	if marker := strings.LastIndex(className, "-attempt-"); marker >= 0 {
		if _, err := strconv.Atoi(className[marker+len("-attempt-"):]); err == nil {
			return className[:marker]
		}
	}
	return className
}

func buildDispatch(runID string, class AgentClass, step int, input json.RawMessage) Dispatch {
	taskID := taskIDForClass(runID, class.Name)
	goal := class.Instructions
	if goal == "" {
		goal = class.Description
	}
	return Dispatch{
		To:             ComputeInstanceID(class.Name, runID, taskID, strconv.Itoa(step)),
		ClassName:      class.Name,
		AgentClassName: class.Name,
		Task: api.Task{
			ID:           taskID,
			RunID:        runID,
			Type:         api.TaskTypeWorker,
			Goal:         goal,
			Input:        input,
			Status:       api.TaskStatusCreated,
			InputSchema:  class.InputSchema,
			OutputSchema: class.OutputSchema,
		},
		Input: input,
		OutputPolicy: agent.OutputPolicy{
			Schema:   class.OutputSchema,
			Validate: len(class.OutputSchema) > 0,
		},
	}
}

func (s TeamState) buildDispatch(class AgentClass, input json.RawMessage) Dispatch {
	dispatch := buildDispatch(s.RunID, class, len(s.Instances), input)
	attempt := 0
	for _, instance := range s.Instances {
		if instance.ClassName == class.Name {
			attempt++
		}
	}
	if attempt > 0 {
		dispatch.Task.ID = fmt.Sprintf("%s-%s-attempt-%d", s.RunID, class.Name, attempt+1)
		dispatch.To = ComputeInstanceID(class.Name, s.RunID, dispatch.Task.ID, strconv.Itoa(len(s.Instances)))
	}
	return dispatch
}
