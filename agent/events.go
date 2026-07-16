package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// EventStepCompleted identifies an event containing one finalized agent step.
const EventStepCompleted api.EventType = "StepCompleted"

// ErrInvalidStepEvent marks a malformed StepCompleted event or step trace.
var ErrInvalidStepEvent = errors.New("agent: invalid step event")

// StepRecord binds a finalized Step to the run, task, agent, and execution that
// produced it. ExecutionID keeps retries in separate index streams.
type StepRecord struct {
	RunID       string `json:"runId"`
	TaskID      string `json:"taskId"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`
	Step        Step   `json:"step"`
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
