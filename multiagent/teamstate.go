package multiagent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
)

// The reference Schedulers (SequentialScheduler, RouterScheduler,
// SupervisorScheduler) are pure, stateless functions of TeamState: every
// decision is derived from the AgentInstances and Tasks in the snapshot,
// never from fields on the Scheduler. This keeps them safe to call from
// the runner's replay/recovery path (spec hard rule 4: stateless across
// ticks).

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
func (s TeamState) reportForClass(className string) *api.TypedReport {
	var taskID string
	for _, instance := range s.Instances {
		if instance.ClassName == className && instance.State == InstanceStateFinished {
			taskID = instance.TaskID
			break
		}
	}
	if taskID == "" {
		return nil
	}
	for _, task := range s.Tasks {
		if task.ID == taskID {
			return task.Result
		}
	}
	return nil
}

// reportInput marshals a finished class's report into the Input payload for
// the next dispatched agent, threading one step's output into the next.
func (s TeamState) reportInput(className string) json.RawMessage {
	report := s.reportForClass(className)
	if report == nil {
		return nil
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil
	}
	return raw
}

// buildDispatch assembles a Dispatch for class as the step-th agent in a
// run. The TaskID is deterministic per (run, class); the instance ID is
// deterministic per (class, run, task, step) via ComputeInstanceID, so
// reconstruction from the event stream reproduces the same identities.
// taskIDForClass derives the deterministic TaskID for a class within a run.
// classNameFromTaskID is its inverse; Drive uses it to recover the class a
// finished Dispatch belonged to (the Dispatch carries only the hashed
// instance ID, not the class name).
func taskIDForClass(runID, className string) string {
	return runID + "-" + className
}

func classNameFromTaskID(runID, taskID string) string {
	return strings.TrimPrefix(taskID, runID+"-")
}

func buildDispatch(runID string, class AgentClass, step int, input json.RawMessage) Dispatch {
	taskID := taskIDForClass(runID, class.Name)
	goal := class.Instructions
	if goal == "" {
		goal = class.Description
	}
	return Dispatch{
		To: ComputeInstanceID(class.Name, runID, taskID, strconv.Itoa(step)),
		Task: api.Task{
			ID:           taskID,
			RunID:        runID,
			Type:         api.TaskTypeWorker,
			Goal:         goal,
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

// discriminatorValue reads field from a report's Structured payload and
// renders it as a routing key. It supports a plain top-level field name
// (not a full JSON path); unknown fields yield "".
func discriminatorValue(structured map[string]any, field string) string {
	if structured == nil {
		return ""
	}
	value, ok := structured[field]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}
