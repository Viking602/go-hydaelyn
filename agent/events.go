package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
)

// EventStepCompleted identifies an event containing one finalized agent step.
const EventStepCompleted api.EventType = "StepCompleted"

// EventExecutionCheckpointed identifies a provider-neutral completed-turn
// checkpoint that can be replayed by a later worker execution.
const EventExecutionCheckpointed = api.EventExecutionCheckpointed

var (
	// ErrInvalidStepEvent marks a malformed StepCompleted event or step trace.
	ErrInvalidStepEvent = errors.New("agent: invalid step event")
	// ErrInvalidCheckpointEvent marks malformed durable turn state.
	ErrInvalidCheckpointEvent = errors.New("agent: invalid checkpoint event")
)

// StepRecord binds a finalized Step to the run, task, agent, and execution that
// produced it. ExecutionID keeps retries in separate index streams.
type StepRecord struct {
	RunID       string `json:"runId"`
	TaskID      string `json:"taskId"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`
	Step        Step   `json:"step"`
}

// ExecutionCheckpointRecord binds a completed turn checkpoint to the durable
// execution that produced it.
type ExecutionCheckpointRecord struct {
	RunID       string         `json:"runId"`
	TaskID      string         `json:"taskId"`
	AgentID     string         `json:"agentId"`
	ExecutionID string         `json:"executionId"`
	Checkpoint  TurnCheckpoint `json:"checkpoint"`
}

// StepSelector filters reconstructed step records. All non-empty fields are
// combined with AND semantics.
type StepSelector struct {
	RunID       string
	TaskID      string
	AgentID     string
	ExecutionID string
}

// NewStepCompletedEvent builds a typed event for one finalized step.
func NewStepCompletedEvent(record StepRecord) (api.Event, error) {
	if err := validateStepRecord(record); err != nil {
		return api.Event{}, err
	}
	return api.Event{
		RunID:      record.RunID,
		TaskID:     record.TaskID,
		Type:       EventStepCompleted,
		Payload:    map[string]any{"record": record},
		RecordedAt: time.Now().UTC(),
	}, nil
}

// NewExecutionCheckpointedEvent builds a typed event for one completed turn.
func NewExecutionCheckpointedEvent(record ExecutionCheckpointRecord) (api.Event, error) {
	if err := validateExecutionCheckpointRecord(record); err != nil {
		return api.Event{}, err
	}
	return api.Event{
		RunID:      record.RunID,
		TaskID:     record.TaskID,
		Type:       EventExecutionCheckpointed,
		Payload:    map[string]any{"record": record},
		RecordedAt: time.Now().UTC(),
	}, nil
}

// ReconstructStepTrace decodes selected StepCompleted events in event-store
// sequence order and validates a zero-based contiguous index stream for each
// execution.
func ReconstructStepTrace(events []api.Event, selector StepSelector) ([]StepRecord, error) {
	records := make([]StepRecord, 0)
	nextIndex := make(map[string]int)
	for eventIndex, event := range events {
		if event.Type != EventStepCompleted {
			continue
		}
		if selector.RunID != "" && event.RunID != selector.RunID {
			continue
		}
		if selector.TaskID != "" && event.TaskID != selector.TaskID {
			continue
		}

		record, err := stepRecordFromPayload(event.Payload)
		if err != nil {
			return nil, fmt.Errorf("%w: event %d record: %v", ErrInvalidStepEvent, eventIndex, err)
		}
		if err := validateStepRecord(record); err != nil {
			return nil, fmt.Errorf("%w: event %d: %v", ErrInvalidStepEvent, eventIndex, err)
		}
		if event.RunID != record.RunID {
			return nil, fmt.Errorf("%w: event %d run ID %q does not match record %q", ErrInvalidStepEvent, eventIndex, event.RunID, record.RunID)
		}
		if event.TaskID != record.TaskID {
			return nil, fmt.Errorf("%w: event %d task ID %q does not match record %q", ErrInvalidStepEvent, eventIndex, event.TaskID, record.TaskID)
		}
		if selector.AgentID != "" && record.AgentID != selector.AgentID {
			continue
		}
		if selector.ExecutionID != "" && record.ExecutionID != selector.ExecutionID {
			continue
		}

		expected := nextIndex[record.ExecutionID]
		if record.Step.Index != expected {
			return nil, fmt.Errorf("%w: execution %q step index %d, want %d", ErrInvalidStepEvent, record.ExecutionID, record.Step.Index, expected)
		}
		nextIndex[record.ExecutionID] = expected + 1
		records = append(records, record)
	}
	return records, nil
}

// ReconstructExecutionCheckpoints decodes selected ExecutionCheckpointed
// events in event-store sequence order. It validates every matching event
// instead of silently skipping corrupted durable state.
func ReconstructExecutionCheckpoints(events []api.Event, selector StepSelector) ([]ExecutionCheckpointRecord, error) {
	records := make([]ExecutionCheckpointRecord, 0)
	for eventIndex, event := range events {
		if event.Type != EventExecutionCheckpointed {
			continue
		}
		if selector.RunID != "" && event.RunID != selector.RunID {
			continue
		}
		if selector.TaskID != "" && event.TaskID != selector.TaskID {
			continue
		}
		record, err := executionCheckpointRecordFromPayload(event.Payload)
		if err != nil {
			return nil, fmt.Errorf("%w: event %d record: %v", ErrInvalidCheckpointEvent, eventIndex, err)
		}
		if err := validateExecutionCheckpointRecord(record); err != nil {
			return nil, fmt.Errorf("%w: event %d: %v", ErrInvalidCheckpointEvent, eventIndex, err)
		}
		if event.RunID != record.RunID || event.TaskID != record.TaskID {
			return nil, fmt.Errorf("%w: event %d identity mismatch", ErrInvalidCheckpointEvent, eventIndex)
		}
		if selector.AgentID != "" && record.AgentID != selector.AgentID {
			continue
		}
		if selector.ExecutionID != "" && record.ExecutionID != selector.ExecutionID {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

// LatestExecutionCheckpoint returns the last matching checkpoint in event-store
// sequence order. It validates every matching event instead of silently
// skipping corrupted durable state.
func LatestExecutionCheckpoint(events []api.Event, selector StepSelector) (ExecutionCheckpointRecord, bool, error) {
	records, err := ReconstructExecutionCheckpoints(events, selector)
	if err != nil {
		return ExecutionCheckpointRecord{}, false, err
	}
	if len(records) == 0 {
		return ExecutionCheckpointRecord{}, false, nil
	}
	return records[len(records)-1], true, nil
}

func validateExecutionCheckpointRecord(record ExecutionCheckpointRecord) error {
	switch {
	case strings.TrimSpace(record.RunID) == "":
		return errors.New("blank run ID")
	case strings.TrimSpace(record.TaskID) == "":
		return errors.New("blank task ID")
	case strings.TrimSpace(record.AgentID) == "":
		return errors.New("blank agent ID")
	case strings.TrimSpace(record.ExecutionID) == "":
		return errors.New("blank execution ID")
	case len(record.Checkpoint.Messages) == 0:
		return errors.New("empty message transcript")
	case record.Checkpoint.Step.Index < 0:
		return fmt.Errorf("negative step index %d", record.Checkpoint.Step.Index)
	case record.Checkpoint.ToolCallsUsed < 0:
		return fmt.Errorf("negative tool-call count %d", record.Checkpoint.ToolCallsUsed)
	case record.Checkpoint.Usage.InputTokens < 0 ||
		record.Checkpoint.Usage.CachedInputTokens < 0 ||
		record.Checkpoint.Usage.CacheWriteInputTokens < 0 ||
		record.Checkpoint.Usage.OutputTokens < 0 ||
		record.Checkpoint.Usage.TotalTokens < 0:
		return errors.New("negative token usage")
	}
	return validateCheckpointHistory(record.Checkpoint)
}

func validateCheckpointHistory(checkpoint TurnCheckpoint) error {
	if !checkpoint.PendingToolCalls {
		if err := message.ValidateCompleteTurns(checkpoint.Messages); err != nil {
			return fmt.Errorf("invalid completed transcript: %w", err)
		}
		return nil
	}
	assistantIndex := -1
	for index := len(checkpoint.Messages) - 1; index >= 0; index-- {
		if len(checkpoint.Messages[index].ToolCalls) > 0 {
			assistantIndex = index
			break
		}
	}
	if assistantIndex < 0 || checkpoint.Messages[assistantIndex].Role != message.RoleAssistant {
		return errors.New("pending checkpoint has no assistant tool-call turn")
	}
	if err := message.ValidateCompleteTurns(checkpoint.Messages[:assistantIndex]); err != nil {
		return fmt.Errorf("invalid transcript before pending turn: %w", err)
	}
	calls := checkpoint.Messages[assistantIndex].ToolCalls
	callIDs := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return errors.New("pending tool call has blank ID")
		}
		if _, duplicate := callIDs[call.ID]; duplicate {
			return fmt.Errorf("pending tool call ID %q is duplicated", call.ID)
		}
		callIDs[call.ID] = struct{}{}
	}
	completed := make(map[string]struct{}, len(calls))
	for _, current := range checkpoint.Messages[assistantIndex+1:] {
		if current.Role != message.RoleTool || current.ToolResult == nil {
			return errors.New("non-tool message follows pending tool-call turn")
		}
		callID := current.ToolResult.ToolCallID
		if _, known := callIDs[callID]; !known {
			return fmt.Errorf("pending turn has unmatched tool result %q", callID)
		}
		if _, duplicate := completed[callID]; duplicate {
			return fmt.Errorf("pending turn has duplicate tool result %q", callID)
		}
		completed[callID] = struct{}{}
	}
	if len(completed) >= len(calls) {
		return errors.New("pending checkpoint has no unresolved tool calls")
	}
	return nil
}

func executionCheckpointRecordFromPayload(payload map[string]any) (ExecutionCheckpointRecord, error) {
	value, ok := payload["record"]
	if !ok {
		return ExecutionCheckpointRecord{}, errors.New("missing payload key record")
	}
	if record, ok := value.(ExecutionCheckpointRecord); ok {
		return record, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ExecutionCheckpointRecord{}, fmt.Errorf("encode payload: %w", err)
	}
	var record ExecutionCheckpointRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ExecutionCheckpointRecord{}, fmt.Errorf("decode payload: %w", err)
	}
	return record, nil
}

func validateStepRecord(record StepRecord) error {
	switch {
	case strings.TrimSpace(record.RunID) == "":
		return fmt.Errorf("%w: blank run ID", ErrInvalidStepEvent)
	case strings.TrimSpace(record.TaskID) == "":
		return fmt.Errorf("%w: blank task ID", ErrInvalidStepEvent)
	case strings.TrimSpace(record.AgentID) == "":
		return fmt.Errorf("%w: blank agent ID", ErrInvalidStepEvent)
	case strings.TrimSpace(record.ExecutionID) == "":
		return fmt.Errorf("%w: blank execution ID", ErrInvalidStepEvent)
	case record.Step.Index < 0:
		return fmt.Errorf("%w: negative step index %d", ErrInvalidStepEvent, record.Step.Index)
	default:
		return nil
	}
}

func stepRecordFromPayload(payload map[string]any) (StepRecord, error) {
	value, ok := payload["record"]
	if !ok {
		return StepRecord{}, errors.New("missing payload key record")
	}
	if record, ok := value.(StepRecord); ok {
		return record, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return StepRecord{}, fmt.Errorf("encode payload: %w", err)
	}
	var record StepRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return StepRecord{}, fmt.Errorf("decode payload: %w", err)
	}
	return record, nil
}
