package storage

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/team"
)

const ReplayInvariantStateEquivalentWithRequiredSubsetV1 = "StateEquivalentWithRequiredSubsetV1"

type TeamState = team.RunState

type ReplayMismatchType string

const (
	ReplayMismatchMissingEvent          ReplayMismatchType = "MissingEvent"
	ReplayMismatchWrongOrder            ReplayMismatchType = "WrongOrder"
	ReplayMismatchStateMismatch         ReplayMismatchType = "StateMismatch"
	ReplayMismatchSemanticInconsistency ReplayMismatchType = "SemanticInconsistency"
)

var RequiredEventTypes = []EventType{
	EventTeamStarted,
	EventTaskScheduled,
	EventTaskStarted,
	EventTaskCompleted,
	EventTaskOutputsPublished,
	EventTeamCompleted,
}

type EventOrderConstraint struct {
	Before     EventType `json:"before"`
	After      EventType `json:"after"`
	TaskScoped bool      `json:"taskScoped,omitempty"`
	Message    string    `json:"message,omitempty"`
}

var EventOrderConstraints = []EventOrderConstraint{
	{Before: EventTeamStarted, After: EventTeamCompleted, Message: "TeamStarted must occur before TeamCompleted"},
	{Before: EventTaskScheduled, After: EventTaskStarted, TaskScoped: true, Message: "TaskScheduled must occur before TaskStarted for the same task"},
	{Before: EventTaskStarted, After: EventTaskCompleted, TaskScoped: true, Message: "TaskStarted must occur before TaskCompleted for the same task"},
	{Before: EventTaskCompleted, After: EventTaskOutputsPublished, TaskScoped: true, Message: "TaskCompleted must occur before TaskOutputsPublished for the same task"},
}

type ReplayMismatch struct {
	Type     ReplayMismatchType `json:"type"`
	Event    *Event             `json:"event,omitempty"`
	Expected any                `json:"expected,omitempty"`
	Actual   any                `json:"actual,omitempty"`
	Message  string             `json:"message"`
}

type ReplayValidationResult struct {
	Invariant        string           `json:"invariant"`
	Valid            bool             `json:"valid"`
	ReplayConsistent bool             `json:"replayConsistent"`
	ReplayedState    TeamState        `json:"replayedState"`
	MismatchCount    int              `json:"mismatchCount"`
	Mismatches       []ReplayMismatch `json:"mismatches,omitempty"`
}

func ValidateReplay(events []Event, authoritativeState TeamState) ReplayValidationResult {
	result := ReplayValidationResult{
		Invariant:     ReplayInvariantStateEquivalentWithRequiredSubsetV1,
		ReplayedState: ReplayTeam(events),
	}

	result.Mismatches = append(result.Mismatches, validateRequiredEventSubset(events, authoritativeState)...)
	result.Mismatches = append(result.Mismatches, validateEventOrdering(events)...)
	result.Mismatches = append(result.Mismatches, validateStateEquivalence(result.ReplayedState, authoritativeState)...)
	result.MismatchCount = len(result.Mismatches)
	result.Valid = result.MismatchCount == 0
	result.ReplayConsistent = result.Valid
	return result
}

func validateRequiredEventSubset(events []Event, authoritativeState TeamState) []ReplayMismatch {
	present := map[EventType][]Event{}
	for _, event := range events {
		present[event.Type] = append(present[event.Type], event)
	}

	mismatches := make([]ReplayMismatch, 0)
	for _, eventType := range RequiredEventTypes {
		if len(present[eventType]) == 0 {
			mismatches = append(mismatches, ReplayMismatch{
				Type:     ReplayMismatchMissingEvent,
				Expected: string(eventType),
				Actual:   "absent",
				Message:  fmt.Sprintf("required event %s is missing from replay log", eventType),
			})
		}
	}

	for _, task := range authoritativeState.Tasks {
		if task.Status != team.TaskStatusCompleted {
			continue
		}
		for _, eventType := range []EventType{EventTaskScheduled, EventTaskStarted, EventTaskCompleted, EventTaskOutputsPublished} {
			if !hasTaskEvent(events, task.ID, eventType) {
				mismatches = append(mismatches, ReplayMismatch{
					Type:     ReplayMismatchMissingEvent,
					Expected: string(eventType),
					Actual:   "absent",
					Message:  fmt.Sprintf("completed task %s is missing required event %s", task.ID, eventType),
				})
			}
		}
	}
	return mismatches
}

func validateEventOrdering(events []Event) []ReplayMismatch {
	mismatches := make([]ReplayMismatch, 0)
	for _, constraint := range EventOrderConstraints {
		if constraint.TaskScoped {
			mismatches = append(mismatches, validateTaskScopedOrder(events, constraint)...)
			continue
		}
		mismatches = append(mismatches, validateGlobalOrder(events, constraint)...)
	}
	return mismatches
}

func validateTaskScopedOrder(events []Event, constraint EventOrderConstraint) []ReplayMismatch {
	mismatches := make([]ReplayMismatch, 0)
	beforeIndex := map[string]int{}
	for idx, event := range events {
		if event.Type == constraint.Before && strings.TrimSpace(event.TaskID) == "" {
			current := event
			mismatches = append(mismatches, ReplayMismatch{
				Type:    ReplayMismatchSemanticInconsistency,
				Event:   &current,
				Actual:  "missing task id",
				Message: fmt.Sprintf("%s must include taskId for task-scoped validation", event.Type),
			})
			continue
		}
		if event.Type == constraint.Before {
			beforeIndex[event.TaskID] = idx
			continue
		}
		if event.Type != constraint.After {
			continue
		}
		current := event
		if strings.TrimSpace(event.TaskID) == "" {
			mismatches = append(mismatches, ReplayMismatch{
				Type:    ReplayMismatchSemanticInconsistency,
				Event:   &current,
				Actual:  "missing task id",
				Message: fmt.Sprintf("%s must include taskId for task-scoped validation", event.Type),
			})
			continue
		}
		before, ok := beforeIndex[event.TaskID]
		if !ok {
			mismatches = append(mismatches, ReplayMismatch{
				Type:     ReplayMismatchWrongOrder,
				Event:    &current,
				Expected: string(constraint.Before),
				Actual:   string(constraint.After),
				Message:  fmt.Sprintf("task %s has %s before %s", event.TaskID, constraint.After, constraint.Before),
			})
			continue
		}
		if before >= idx {
			mismatches = append(mismatches, ReplayMismatch{
				Type:     ReplayMismatchWrongOrder,
				Event:    &current,
				Expected: string(constraint.Before),
				Actual:   string(constraint.After),
				Message:  fmt.Sprintf("task %s violates ordering constraint: %s", event.TaskID, constraint.Message),
			})
		}
	}
	return mismatches
}

func validateGlobalOrder(events []Event, constraint EventOrderConstraint) []ReplayMismatch {
	beforeIndex := -1
	for idx, event := range events {
		if event.Type == constraint.Before && beforeIndex == -1 {
			beforeIndex = idx
		}
		if event.Type != constraint.After {
			continue
		}
		current := event
		if beforeIndex == -1 || beforeIndex >= idx {
			return []ReplayMismatch{{
				Type:     ReplayMismatchWrongOrder,
				Event:    &current,
				Expected: string(constraint.Before),
				Actual:   string(constraint.After),
				Message:  constraint.Message,
			}}
		}
	}
	return nil
}

func validateStateEquivalence(replayedState, authoritativeState TeamState) []ReplayMismatch {
	expected := normalizeReplayComparableState(authoritativeState)
	actual := normalizeReplayComparableState(replayedState)
	if reflect.DeepEqual(actual, expected) {
		return nil
	}
	return []ReplayMismatch{{
		Type:     ReplayMismatchStateMismatch,
		Expected: expected,
		Actual:   actual,
		Message:  "replayed state does not match authoritative state after replay normalization",
	}}
}

func normalizeReplayComparableState(state TeamState) TeamState {
	state.Version = 0
	state.SessionID = ""
	state.CreatedAt = time.Time{}
	state.UpdatedAt = time.Time{}
	state.Input = nil
	state.Metadata = nil
	state.RequireVerification = false
	if state.Planning != nil && isEmptyPlanningState(*state.Planning) {
		state.Planning = nil
	}
	state.Supervisor.Profile = ""
	state.Supervisor.SessionID = ""
	state.Supervisor.Budget = team.Budget{}
	state.Supervisor.Metadata = nil
	for idx := range state.Workers {
		state.Workers[idx].Profile = ""
		state.Workers[idx].SessionID = ""
		state.Workers[idx].Budget = team.Budget{}
		state.Workers[idx].Metadata = nil
	}
	if state.Result != nil {
		state.Result.Findings = nil
		state.Result.Evidence = nil
		state.Result.Confidence = 0
		if len(state.Result.Structured) == 0 {
			state.Result.Structured = nil
		}
		if len(state.Result.ArtifactIDs) == 0 {
			state.Result.ArtifactIDs = nil
		}
		if isEmptyResult(*state.Result) {
			state.Result = nil
		}
	}
	for idx := range state.Tasks {
		state.Tasks[idx].Stage = ""
		state.Tasks[idx].RequiredCapabilities = nil
		state.Tasks[idx].Assignee = ""
		state.Tasks[idx].Namespace = ""
		state.Tasks[idx].VerifierRequired = false
		state.Tasks[idx].IdempotencyKey = ""
		state.Tasks[idx].Version = 0
		state.Tasks[idx].MaxAttempts = 0
		state.Tasks[idx].SessionID = ""
		state.Tasks[idx].Error = ""
		state.Tasks[idx].StartedAt = time.Time{}
		state.Tasks[idx].CompletedAt = time.Time{}
		state.Tasks[idx].CompletedBy = ""
		state.Tasks[idx].FinishedAt = time.Time{}
		if len(state.Tasks[idx].DependsOn) == 0 {
			state.Tasks[idx].DependsOn = nil
		}
		if len(state.Tasks[idx].Reads) == 0 {
			state.Tasks[idx].Reads = nil
		}
		if len(state.Tasks[idx].Writes) == 0 {
			state.Tasks[idx].Writes = nil
		}
		if len(state.Tasks[idx].Publish) == 0 {
			state.Tasks[idx].Publish = nil
		}
		if state.Tasks[idx].Result != nil {
			state.Tasks[idx].Result.Findings = nil
			state.Tasks[idx].Result.Evidence = nil
			state.Tasks[idx].Result.Confidence = 0
			if len(state.Tasks[idx].Result.Structured) == 0 {
				state.Tasks[idx].Result.Structured = nil
			}
			if len(state.Tasks[idx].Result.ArtifactIDs) == 0 {
				state.Tasks[idx].Result.ArtifactIDs = nil
			}
			if isEmptyResult(*state.Tasks[idx].Result) {
				state.Tasks[idx].Result = nil
			}
		}
	}
	if state.Blackboard != nil {
		if len(state.Blackboard.Sources) == 0 {
			state.Blackboard.Sources = nil
		}
		if len(state.Blackboard.Artifacts) == 0 {
			state.Blackboard.Artifacts = nil
		}
		if len(state.Blackboard.Evidence) == 0 {
			state.Blackboard.Evidence = nil
		}
		if len(state.Blackboard.Claims) == 0 {
			state.Blackboard.Claims = nil
		}
		if len(state.Blackboard.Findings) == 0 {
			state.Blackboard.Findings = nil
		}
		if len(state.Blackboard.Verifications) == 0 {
			state.Blackboard.Verifications = nil
		}
		if len(state.Blackboard.Exchanges) == 0 {
			state.Blackboard.Exchanges = nil
		}
	}
	if len(state.Workers) == 0 {
		state.Workers = nil
	}
	if len(state.Tasks) == 0 {
		state.Tasks = nil
	}
	return state
}

func hasTaskEvent(events []Event, taskID string, eventType EventType) bool {
	for _, event := range events {
		if event.TaskID == taskID && event.Type == eventType {
			return true
		}
	}
	return false
}

func isEmptyPlanningState(state team.PlanningState) bool {
	return state.PlannerName == "" &&
		state.Goal == "" &&
		len(state.SuccessCriteria) == 0 &&
		state.ReviewCount == 0 &&
		state.LastAction == "" &&
		state.LastActionReason == "" &&
		state.PlanVersion == 0
}

func isEmptyResult(result team.Result) bool {
	return result.Summary == "" &&
		len(result.Structured) == 0 &&
		len(result.ArtifactIDs) == 0 &&
		len(result.Findings) == 0 &&
		len(result.Evidence) == 0 &&
		result.Confidence == 0 &&
		result.Usage == (team.Result{}).Usage &&
		result.ToolCallCount == 0 &&
		result.Error == ""
}
